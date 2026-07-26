package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"echomap/internal/monitor"
	"echomap/internal/store"
)

// ErrSMTPNotConfigured is returned by SendTest when no SMTP host is set.
var ErrSMTPNotConfigured = errors.New("SMTP is not configured")

// Operator-overridable via settings.email_{subject,body}_template (Doc 3 §7).
const (
	defaultEmailSubject = `[EchoMap] {{.Name}} {{.From}} → {{.To}}`
	defaultEmailBody    = `Device {{.Name}} ({{.IP}}) changed {{.From}} → {{.To}} at {{.Time}}.` +
		`{{if .AffectedChildren}}
{{.AffectedChildren}} downstream device(s) are down via this parent: {{.ChildNames}}{{end}}`
)

// Email delivers alerts over SMTP. Like Telegram it re-reads its config per
// alert and no-ops when SMTP isn't configured, so monitoring runs fine without it.
type Email struct {
	store   *store.Store
	timeout time.Duration
}

func NewEmail(st *store.Store) *Email {
	return &Email{store: st, timeout: 15 * time.Second}
}

// StatusAlert implements the device-status branch (one message per recipient
// per confirmed transition; cascaded children never alert themselves).
func (e *Email) StatusAlert(ctx context.Context, d monitor.Device, from, to string, affected []monitor.Child) {
	if to != "DOWN" && to != "UP" {
		return
	}
	cfg, err := e.store.EmailConfigFor(ctx, d.ID)
	if err != nil {
		log.Printf("notify: load email config: %v", err)
		return
	}
	if cfg.SMTP.Host == "" || len(cfg.Rules) == 0 {
		return
	}
	vars := buildVars(d, from, to, affected)
	subject := renderText(cfg.Subject, defaultEmailSubject, vars)
	body := renderText(cfg.Body, defaultEmailBody, vars)

	for _, r := range cfg.Rules {
		if (to == "DOWN" && !r.OnDown) || (to == "UP" && !r.OnUp) {
			continue
		}
		err := e.send(ctx, cfg.SMTP, r.ToAddr, subject, body)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
			log.Printf("notify: email to %s failed: %v", r.ToAddr, err)
		}
		_ = e.store.RecordAlertEvent(ctx, d.ID, from, to, "EMAIL", false, err == nil, errMsg)

		level, msg := "ALERT", fmt.Sprintf("email alert %s → %s for %s (%s)", from, to, d.Name, d.IP)
		if err != nil {
			level, msg = "ERROR", fmt.Sprintf("email alert send failed for %s: %s", d.Name, errMsg)
		}
		id := d.ID
		_ = e.store.InsertEventLog(ctx, level, "alert", msg, &id, nil,
			map[string]any{"channel": "EMAIL", "delivered": err == nil, "to": r.ToAddr})
	}
}

// MonitorAlert delivers a custom-monitor transition over email, reusing the
// device's EMAIL rules and the same on_down/on_up gating.
func (e *Email) MonitorAlert(ctx context.Context, deviceID int64, label, kind, from, to string) {
	cfg, err := e.store.EmailConfigFor(ctx, deviceID)
	if err != nil {
		log.Printf("notify: load email config (monitor): %v", err)
		return
	}
	if cfg.SMTP.Host == "" || len(cfg.Rules) == 0 {
		return
	}
	subject := fmt.Sprintf("[EchoMap] monitor %s (%s) %s → %s", label, kind, from, to)
	body := fmt.Sprintf("Monitor %s (%s) changed %s → %s at %s.",
		label, kind, from, to, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	for _, r := range cfg.Rules {
		if (to == "DOWN" && !r.OnDown) || (to == "UP" && !r.OnUp) {
			continue
		}
		err := e.send(ctx, cfg.SMTP, r.ToAddr, subject, body)
		level, msg := "ALERT", fmt.Sprintf("email monitor %s (%s) %s → %s", label, kind, from, to)
		if err != nil {
			level, msg = "ERROR", fmt.Sprintf("email monitor alert failed for %s: %v", label, err)
			log.Printf("notify: email monitor to %s failed: %v", r.ToAddr, err)
		}
		id := deviceID
		_ = e.store.InsertEventLog(ctx, level, "alert", msg, &id, nil,
			map[string]any{"channel": "EMAIL", "delivered": err == nil, "monitor": label, "kind": kind})
	}
}

// FlappingAlert fires once when a device enters the flapping state, gated by
// each rule's on_flapping toggle.
func (e *Email) FlappingAlert(ctx context.Context, d monitor.Device) {
	cfg, err := e.store.EmailConfigFor(ctx, d.ID)
	if err != nil {
		log.Printf("notify: load email config (flapping): %v", err)
		return
	}
	if cfg.SMTP.Host == "" || len(cfg.Rules) == 0 {
		return
	}
	subject := fmt.Sprintf("[EchoMap] %s is flapping", d.Name)
	body := fmt.Sprintf("Device %s (%s) is flapping — repeated up/down transitions. Per-change alerts are muted until it stabilises.", d.Name, d.IP)
	for _, r := range cfg.Rules {
		if !r.OnFlapping {
			continue
		}
		err := e.send(ctx, cfg.SMTP, r.ToAddr, subject, body)
		level, msg := "ALERT", fmt.Sprintf("email flapping alert for %s (%s)", d.Name, d.IP)
		if err != nil {
			level, msg = "ERROR", fmt.Sprintf("email flapping alert failed for %s: %v", d.Name, err)
			log.Printf("notify: email flapping to %s failed: %v", r.ToAddr, err)
		}
		id := d.ID
		_ = e.store.InsertEventLog(ctx, level, "alert", msg, &id, nil,
			map[string]any{"channel": "EMAIL", "delivered": err == nil, "flapping": true})
	}
}

// SendTest sends a one-off message to `to` using the current SMTP config —
// backs the Settings "send test" button. Returns ErrSMTPNotConfigured if no
// host is set, or the transport error on failure.
func (e *Email) SendTest(ctx context.Context, to string) error {
	cfg, err := e.store.EmailConfigFor(ctx, 0)
	if err != nil {
		return err
	}
	if cfg.SMTP.Host == "" {
		return ErrSMTPNotConfigured
	}
	return e.send(ctx, cfg.SMTP, to,
		"[EchoMap] SMTP test",
		"This is a test message from EchoMap. If you received it, your SMTP settings are working.")
}

// send delivers one message. TLS modes: "tls" (implicit, e.g. :465), "starttls"
// (upgrade on a plain connection, e.g. :587), or "none"/"" (plain, e.g. :25 or a
// local relay). ponytail: TLS uses InsecureSkipVerify — internal mail relays run
// self-signed certs (same posture as the HTTPS monitors, PRD §4.5); add a verify
// toggle before sending over an untrusted network.
func (e *Email) send(ctx context.Context, cfg store.SMTPDialConfig, to, subject, body string) error {
	from := cfg.From
	if from == "" {
		from = "echomap@localhost"
	}
	port := cfg.Port
	if port == 0 {
		switch cfg.TLS {
		case "tls":
			port = 465
		case "starttls":
			port = 587
		default:
			port = 25
		}
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	tlsCfg := &tls.Config{ServerName: cfg.Host, InsecureSkipVerify: true} //nolint:gosec // internal relays, see doc comment
	dialer := net.Dialer{Timeout: e.timeout}

	var conn net.Conn
	var err error
	if cfg.TLS == "tls" {
		conn, err = tls.DialWithDialer(&dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}

	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}
	if cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(buildMessage(from, to, subject, body))); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMessage assembles a minimal RFC 5322 plaintext message with CRLF lines.
func buildMessage(from, to, subject, body string) string {
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}
