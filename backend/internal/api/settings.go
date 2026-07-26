package api

import (
	"errors"
	"net/http"
	"strings"
	"text/template"

	"echomap/internal/notify"
	"echomap/internal/store"
)

// maskedToken is what GET returns when a secret exists, and what PUT treats as
// "unchanged" when the frontend echoes the masked value back (write-only field,
// Doc 5 §0/§4). Plaintext secrets never leave the store toward the API.
const maskedToken = "••••••••"

var smtpTLSModes = map[string]bool{"none": true, "starttls": true, "tls": true}

func (s *server) getChannelSettings(w http.ResponseWriter, r *http.Request) {
	cs, err := s.store.GetChannelSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	tok := ""
	if cs.TelegramTokenSet {
		tok = maskedToken
	}
	smtpPw := ""
	if cs.SMTPPasswordSet {
		smtpPw = maskedToken
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"telegram_bot_token":     tok,
		"telegram_template":      cs.TelegramTemplate,
		"webssh_url":             cs.WebsshURL,
		"email_subject_template": cs.EmailSubject,
		"email_body_template":    cs.EmailBody,
		"smtp": map[string]any{
			"host":     cs.SMTPHost,
			"port":     cs.SMTPPort,
			"username": cs.SMTPUsername,
			"from":     cs.SMTPFrom,
			"tls":      cs.SMTPTLS,
			"password": smtpPw,
		},
	})
}

func (s *server) putChannelSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TelegramBotToken *string `json:"telegram_bot_token"`
		TelegramTemplate *string `json:"telegram_template"`
		WebsshURL        *string `json:"webssh_url"`
		EmailSubject     *string `json:"email_subject_template"`
		EmailBody        *string `json:"email_body_template"`
		SMTP             *struct {
			Host     string  `json:"host"`
			Port     int     `json:"port"`
			Username string  `json:"username"`
			From     string  `json:"from"`
			TLS      string  `json:"tls"`
			Password *string `json:"password"`
		} `json:"smtp"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	// Masked echoes mean "leave unchanged".
	if body.TelegramBotToken != nil && *body.TelegramBotToken == maskedToken {
		body.TelegramBotToken = nil
	}
	// Operator templates that don't parse must never reach the notifier (Doc 3 §7).
	for _, t := range []*string{body.TelegramTemplate, body.EmailSubject, body.EmailBody} {
		if t != nil && *t != "" {
			if _, err := template.New("t").Parse(*t); err != nil {
				unprocessable(w, "invalid template: "+err.Error())
				return
			}
		}
	}

	upd := store.ChannelUpdate{
		TelegramToken:    body.TelegramBotToken,
		TelegramTemplate: body.TelegramTemplate,
		WebsshURL:        body.WebsshURL,
		EmailSubject:     body.EmailSubject,
		EmailBody:        body.EmailBody,
	}
	if body.SMTP != nil {
		tls := strings.ToLower(strings.TrimSpace(body.SMTP.TLS))
		if tls == "" {
			tls = "none"
		}
		if !smtpTLSModes[tls] {
			unprocessable(w, "smtp.tls must be none, starttls, or tls")
			return
		}
		if body.SMTP.Port < 0 || body.SMTP.Port > 65535 {
			unprocessable(w, "smtp.port must be between 0 and 65535")
			return
		}
		pw := body.SMTP.Password
		if pw != nil && *pw == maskedToken {
			pw = nil // masked echo = keep stored password
		}
		upd.SMTP = &store.SMTPUpdate{
			Host: body.SMTP.Host, Port: body.SMTP.Port, Username: body.SMTP.Username,
			From: body.SMTP.From, TLS: tls, Password: pw,
		}
	}

	if err := s.store.UpdateChannelSettings(r.Context(), upd); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "updated alert channel settings", nil, nil)
	s.getChannelSettings(w, r) // respond with the masked view
}

// testEmail sends a one-off message using the stored SMTP config (Settings
// "send test" button). 422 if SMTP isn't configured, 502 if the send fails.
func (s *server) testEmail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		To string `json:"to"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.To) == "" {
		unprocessable(w, "to (recipient address) is required")
		return
	}
	if err := s.email.SendTest(r.Context(), strings.TrimSpace(body.To)); err != nil {
		if errors.Is(err, notify.ErrSMTPNotConfigured) {
			unprocessable(w, err.Error())
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
