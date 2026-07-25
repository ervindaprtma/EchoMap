package api

import (
	"net/http"
	"strings"
	"time"

	"echomap/internal/moncheck"
	"echomap/internal/store"
)

var monitorKinds = map[string]bool{"TCP": true, "HTTP": true, "HTTPS": true}

func (s *server) listMonitors(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListMonitors(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type monitorBody struct {
	Label       string  `json:"label"`
	Kind        string  `json:"kind"`
	Port        *int    `json:"port"`
	Path        *string `json:"path"`
	URLOverride *string `json:"url_override"`
	ExpectLo    *int    `json:"expect_status_lo"`
	ExpectHi    *int    `json:"expect_status_hi"`
	Interval    *int    `json:"interval_seconds"`
	TimeoutMs   *int    `json:"timeout_ms"`
}

func (s *server) createMonitor(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := idParam(w, r)
	if !ok {
		return
	}
	var b monitorBody
	if !readJSON(w, r, &b) {
		return
	}
	b.Kind = strings.ToUpper(strings.TrimSpace(b.Kind))
	b.Label = strings.TrimSpace(b.Label)
	if b.Label == "" || !monitorKinds[b.Kind] {
		unprocessable(w, "label is required and kind must be TCP, HTTP or HTTPS")
		return
	}
	if b.Kind == "TCP" && (b.Port == nil || *b.Port < 1 || *b.Port > 65535) {
		unprocessable(w, "a TCP monitor requires a port between 1 and 65535")
		return
	}
	interval := valueOr(b.Interval, 60)
	if interval < 10 || interval > 86400 {
		unprocessable(w, "interval_seconds must be between 10 and 86400")
		return
	}
	in := store.CreateMonitorInput{
		DeviceID: deviceID, Label: b.Label, Kind: b.Kind, Port: b.Port,
		Path: b.Path, URLOverride: b.URLOverride,
		ExpectLo: valueOr(b.ExpectLo, 200), ExpectHi: valueOr(b.ExpectHi, 399),
		IntervalSecs: interval, TimeoutMs: valueOr(b.TimeoutMs, 5000),
	}
	m, err := s.store.CreateMonitor(r.Context(), in)
	if err != nil {
		writeAuthErr(w, err) // maps ErrConflict → 409
		return
	}
	s.logEvent(r, "NOTICE", "config", "added monitor "+m.Label+" ("+m.Kind+")", &deviceID, nil)
	writeJSON(w, http.StatusCreated, m)
}

func (s *server) patchMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	var b monitorBody
	// Reuse the create body but treat every field as optional (nil = unchanged).
	var raw struct {
		monitorBody
		Enabled *bool `json:"enabled"`
	}
	if !readJSON(w, r, &raw) {
		return
	}
	b = raw.monitorBody
	if b.Kind != "" {
		unprocessable(w, "kind cannot be changed; delete and re-create to change kind")
		return
	}
	if b.Interval != nil && (*b.Interval < 10 || *b.Interval > 86400) {
		unprocessable(w, "interval_seconds must be between 10 and 86400")
		return
	}
	var label *string
	if b.Label != "" {
		l := strings.TrimSpace(b.Label)
		label = &l
	}
	m, err := s.store.UpdateMonitor(r.Context(), id, store.PatchMonitorInput{
		Label: label, Port: b.Port, Path: b.Path, URLOverride: b.URLOverride,
		ExpectLo: b.ExpectLo, ExpectHi: b.ExpectHi, IntervalSecs: b.Interval,
		TimeoutMs: b.TimeoutMs, Enabled: raw.Enabled,
	})
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) deleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteMonitor(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}

// testMonitor runs one check now and returns its result (Doc 5 §8.4), without
// touching the monitor's cursor or debounce state.
func (s *server) testMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	m, err := s.store.MonitorForCheck(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	port := 0
	if m.Port != nil {
		port = *m.Port
	}
	res := moncheck.Check(r.Context(), moncheck.Spec{
		Kind: m.Kind, Host: m.DeviceIP, Port: port,
		Path: strDeref(m.Path), URLOverride: strDeref(m.URLOverride),
		ExpectLo: m.ExpectLo, ExpectHi: m.ExpectHi,
		Timeout: time.Duration(m.TimeoutMs) * time.Millisecond,
	})
	out := map[string]any{"ok": res.OK, "duration_ms": res.DurationMs}
	if res.HTTPStatus != nil {
		out["http_status"] = *res.HTTPStatus
	}
	if res.CertExpiresAt != nil {
		out["cert_days_left"] = int(time.Until(*res.CertExpiresAt).Hours() / 24)
	}
	writeJSON(w, http.StatusOK, out)
}

func unprocessable(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": msg})
}

func valueOr(p *int, def int) int {
	if p != nil {
		return *p
	}
	return def
}

func strDeref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
