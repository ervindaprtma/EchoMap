package api

import (
	"errors"
	"net/http"
	"strings"

	"echomap/internal/store"
)

// Alert-rules CRUD (Doc 5 §8, Phase 11). Closes the audit gap where alert_rules
// could only be created via SQL. Read = Operator, writes = Admin (server.go).
var alertChannels = map[string]bool{"TELEGRAM": true, "EMAIL": true}

func (s *server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAlertRules(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type alertRuleBody struct {
	DeviceID   *int64 `json:"device_id"`
	Channel    string `json:"channel"`
	Target     string `json:"target"`
	OnDown     *bool  `json:"on_down"`
	OnUp       *bool  `json:"on_up"`
	OnFlapping *bool  `json:"on_flapping"`
	OnOrphaned *bool  `json:"on_orphaned"`
	Enabled    *bool  `json:"enabled"`
}

func (s *server) createAlertRule(w http.ResponseWriter, r *http.Request) {
	var b alertRuleBody
	if !readJSON(w, r, &b) {
		return
	}
	b.Channel = strings.ToUpper(strings.TrimSpace(b.Channel))
	b.Target = strings.TrimSpace(b.Target)
	if !alertChannels[b.Channel] {
		unprocessable(w, "channel must be TELEGRAM or EMAIL")
		return
	}
	if b.Target == "" {
		unprocessable(w, "target is required (Telegram chat id or email address)")
		return
	}
	// A device-scoped rule must point at a real device; NULL = global. Validate
	// here so a bad id is a clean 422, not a foreign-key 500.
	if b.DeviceID != nil {
		if _, err := s.store.GetDevice(r.Context(), *b.DeviceID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				unprocessable(w, "device_id does not exist")
				return
			}
			writeErr(w, err)
			return
		}
	}
	in := store.AlertRuleRow{
		DeviceID: b.DeviceID, Channel: b.Channel, Target: b.Target,
		OnDown: boolOr(b.OnDown, true), OnUp: boolOr(b.OnUp, true),
		OnFlapping: boolOr(b.OnFlapping, true), OnOrphaned: boolOr(b.OnOrphaned, true),
		Enabled: boolOr(b.Enabled, true),
	}
	rule, err := s.store.CreateAlertRule(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.logEvent(r, "NOTICE", "config", "added "+rule.Channel+" alert rule", rule.DeviceID, nil)
	writeJSON(w, http.StatusCreated, rule)
}

func (s *server) patchAlertRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var b alertRuleBody
	if !readJSON(w, r, &b) {
		return
	}
	in := store.PatchAlertRuleInput{
		OnDown: b.OnDown, OnUp: b.OnUp, OnFlapping: b.OnFlapping,
		OnOrphaned: b.OnOrphaned, Enabled: b.Enabled,
	}
	if b.Channel != "" {
		ch := strings.ToUpper(strings.TrimSpace(b.Channel))
		if !alertChannels[ch] {
			unprocessable(w, "channel must be TELEGRAM or EMAIL")
			return
		}
		in.Channel = &ch
	}
	if t := strings.TrimSpace(b.Target); t != "" {
		in.Target = &t
	}
	rule, err := s.store.UpdateAlertRule(r.Context(), id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *server) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAlertRule(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

func boolOr(p *bool, def bool) bool {
	if p != nil {
		return *p
	}
	return def
}
