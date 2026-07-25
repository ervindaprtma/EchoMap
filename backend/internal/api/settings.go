package api

import (
	"net/http"
	"text/template"
)

// maskedToken is what GET returns when a token exists, and what PUT treats as
// "unchanged" when the frontend echoes the masked value back (write-only field,
// Doc 5 §0/§4). Plaintext tokens never leave the store toward the API.
const maskedToken = "••••••••"

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
	writeJSON(w, http.StatusOK, map[string]string{
		"telegram_bot_token": tok,
		"telegram_template":  cs.TelegramTemplate,
		"webssh_url":         cs.WebsshURL,
	})
}

func (s *server) putChannelSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TelegramBotToken *string `json:"telegram_bot_token"`
		TelegramTemplate *string `json:"telegram_template"`
		WebsshURL        *string `json:"webssh_url"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.TelegramBotToken != nil && *body.TelegramBotToken == maskedToken {
		body.TelegramBotToken = nil // masked echo = leave unchanged
	}
	// A template that doesn't parse must never reach the notifier (Doc 3 §7).
	if body.TelegramTemplate != nil && *body.TelegramTemplate != "" {
		if _, err := template.New("t").Parse(*body.TelegramTemplate); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity,
				map[string]string{"error": "invalid telegram_template: " + err.Error()})
			return
		}
	}
	if err := s.store.UpdateChannelSettings(r.Context(), body.TelegramBotToken, body.TelegramTemplate, body.WebsshURL); err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "updated alert channel settings", nil, nil)
	s.getChannelSettings(w, r) // respond with the masked view
}
