// Package notify delivers alerts. This is the Slice-7 seed: Telegram only,
// driven directly by monitor state changes. Email + the single "flapping"
// alert land with the full notifier.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"text/template"
	"time"

	"echomap/internal/monitor"
	"echomap/internal/store"
)

// Default message; operators override via settings.telegram_template (Doc 3 §7).
const defaultTemplate = `{{if eq .To "DOWN"}}🔴{{else if eq .To "UP"}}🟢{{else}}🟠{{end}} *{{.Name}}* ({{.IP}}) {{.From}} → {{.To}} at {{.Time}}{{if .AffectedChildren}}
⚠️ {{.AffectedChildren}} downstream device(s) down via parent: {{.ChildNames}}{{end}}`

type templateVars struct {
	Name, IP, From, To, Time string
	AffectedChildren         int
	ChildNames               string // first few names, comma-joined
}

type Telegram struct {
	store  *store.Store
	client *http.Client
	apiURL string // overridable in tests
}

func NewTelegram(st *store.Store) *Telegram {
	return &Telegram{
		store:  st,
		client: &http.Client{Timeout: 10 * time.Second},
		apiURL: "https://api.telegram.org",
	}
}

// StatusAlert implements monitor.Alerter: one aggregated message per confirmed
// transition; cascaded children arrive in `affected` and never alert themselves.
// Config is re-read per alert — alerts are rare, and this picks up settings
// edits with zero cache invalidation.
func (t *Telegram) StatusAlert(ctx context.Context, d monitor.Device, from, to string, affected []monitor.Child) {
	if to != "DOWN" && to != "UP" {
		return // ORPHANED alerts belong to the sync worker (Slice 7)
	}
	cfg, err := t.store.TelegramConfigFor(ctx, d.ID)
	if err != nil {
		log.Printf("notify: load telegram config: %v", err)
		return
	}
	if cfg.BotToken == "" || len(cfg.Rules) == 0 {
		return // not configured — monitoring runs fine without alerting
	}

	text := render(cfg.Template, d, from, to, affected)
	for _, r := range cfg.Rules {
		if (to == "DOWN" && !r.OnDown) || (to == "UP" && !r.OnUp) {
			continue
		}
		err := t.send(ctx, cfg.BotToken, r.ChatID, text)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
			log.Printf("notify: telegram send to %s failed: %v", r.ChatID, err)
		}
		_ = t.store.RecordAlertEvent(ctx, d.ID, from, to, "TELEGRAM", false, err == nil, errMsg)

		// Event log: ALERT on delivery, ERROR on failure (Doc 3 §10).
		level, msg := "ALERT", fmt.Sprintf("telegram alert %s → %s for %s (%s)", from, to, d.Name, d.IP)
		if err != nil {
			level, msg = "ERROR", fmt.Sprintf("telegram alert send failed for %s: %s", d.Name, errMsg)
		}
		id := d.ID
		_ = t.store.InsertEventLog(ctx, level, "alert", msg, &id, nil,
			map[string]any{"channel": "TELEGRAM", "delivered": err == nil})
	}
}

// MonitorAlert delivers a custom-monitor transition (Doc 3 §9). It reuses the
// device's alert rules and the same on_down/on_up gating, and logs ALERT/ERROR
// to the event log per attempt. Message carries the monitor label + kind.
func (t *Telegram) MonitorAlert(ctx context.Context, deviceID int64, label, kind, from, to string) {
	cfg, err := t.store.TelegramConfigFor(ctx, deviceID)
	if err != nil {
		log.Printf("notify: load telegram config (monitor): %v", err)
		return
	}
	if cfg.BotToken == "" || len(cfg.Rules) == 0 {
		return
	}
	icon := "🟢"
	if to == "DOWN" {
		icon = "🔴"
	}
	text := fmt.Sprintf("%s monitor *%s* (%s) %s → %s", icon, label, kind, from, to)
	for _, r := range cfg.Rules {
		if (to == "DOWN" && !r.OnDown) || (to == "UP" && !r.OnUp) {
			continue
		}
		err := t.send(ctx, cfg.BotToken, r.ChatID, text)
		level, msg := "ALERT", fmt.Sprintf("monitor %s (%s) %s → %s", label, kind, from, to)
		if err != nil {
			level, msg = "ERROR", fmt.Sprintf("monitor alert send failed for %s: %v", label, err)
			log.Printf("notify: telegram monitor send to %s failed: %v", r.ChatID, err)
		}
		id := deviceID
		_ = t.store.InsertEventLog(ctx, level, "alert", msg, &id, nil,
			map[string]any{"channel": "TELEGRAM", "delivered": err == nil, "monitor": label, "kind": kind})
	}
}

// FlappingAlert fires once when a device enters the flapping state, gated by
// each rule's on_flapping toggle. Event-logged, not recorded in alert_events
// (which is status-transition-scoped).
func (t *Telegram) FlappingAlert(ctx context.Context, d monitor.Device) {
	cfg, err := t.store.TelegramConfigFor(ctx, d.ID)
	if err != nil {
		log.Printf("notify: load telegram config (flapping): %v", err)
		return
	}
	if cfg.BotToken == "" || len(cfg.Rules) == 0 {
		return
	}
	text := fmt.Sprintf("⚠️ *%s* (%s) is flapping — repeated up/down transitions; per-change alerts are muted until it stabilises.", d.Name, d.IP)
	for _, r := range cfg.Rules {
		if !r.OnFlapping {
			continue
		}
		err := t.send(ctx, cfg.BotToken, r.ChatID, text)
		level, msg := "ALERT", fmt.Sprintf("telegram flapping alert for %s (%s)", d.Name, d.IP)
		if err != nil {
			level, msg = "ERROR", fmt.Sprintf("telegram flapping alert failed for %s: %v", d.Name, err)
			log.Printf("notify: telegram flapping send to %s failed: %v", r.ChatID, err)
		}
		id := d.ID
		_ = t.store.InsertEventLog(ctx, level, "alert", msg, &id, nil,
			map[string]any{"channel": "TELEGRAM", "delivered": err == nil, "flapping": true})
	}
}

func render(tmplSrc string, d monitor.Device, from, to string, affected []monitor.Child) string {
	return renderText(tmplSrc, defaultTemplate, buildVars(d, from, to, affected))
}

// buildVars assembles the template variables shared by every channel.
func buildVars(d monitor.Device, from, to string, affected []monitor.Child) templateVars {
	names := make([]string, 0, 6)
	for i, c := range affected {
		if i == 5 {
			names = append(names, fmt.Sprintf("+%d more", len(affected)-5))
			break
		}
		names = append(names, c.Name)
	}
	return templateVars{
		Name: d.Name, IP: d.IP, From: from, To: to,
		Time:             time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		AffectedChildren: len(affected),
		ChildNames:       strings.Join(names, ", "),
	}
}

// renderText renders src (falling back to fallback on empty or a parse error —
// a bad operator template must never kill alerting, Doc 3 §7).
func renderText(src, fallback string, vars templateVars) string {
	if src == "" {
		src = fallback
	}
	tmpl, err := template.New("alert").Parse(src)
	if err != nil {
		tmpl = template.Must(template.New("alert").Parse(fallback))
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return fmt.Sprintf("%s (%s) %s → %s", vars.Name, vars.IP, vars.From, vars.To)
	}
	return buf.String()
}

func (t *Telegram) send(ctx context.Context, token, chatID, text string) error {
	body, _ := json.Marshal(map[string]string{
		"chat_id": chatID, "text": text, "parse_mode": "Markdown",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", t.apiURL, token), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API status %s", resp.Status)
	}
	return nil
}
