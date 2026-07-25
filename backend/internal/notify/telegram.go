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
		_ = t.store.RecordAlertEvent(ctx, d.ID, from, to, false, err == nil, errMsg)

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

func render(tmplSrc string, d monitor.Device, from, to string, affected []monitor.Child) string {
	if tmplSrc == "" {
		tmplSrc = defaultTemplate
	}
	names := make([]string, 0, 6)
	for i, c := range affected {
		if i == 5 {
			names = append(names, fmt.Sprintf("+%d more", len(affected)-5))
			break
		}
		names = append(names, c.Name)
	}
	vars := templateVars{
		Name: d.Name, IP: d.IP, From: from, To: to,
		Time:             time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		AffectedChildren: len(affected),
		ChildNames:       strings.Join(names, ", "),
	}

	tmpl, err := template.New("alert").Parse(tmplSrc)
	if err != nil { // a bad operator template must never kill alerting (Doc 3 §7)
		tmpl = template.Must(template.New("alert").Parse(defaultTemplate))
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return fmt.Sprintf("%s (%s) %s → %s", d.Name, d.IP, from, to)
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
