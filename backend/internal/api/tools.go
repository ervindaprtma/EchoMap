package api

import (
	"net/http"
	"strings"
	"time"

	"echomap/internal/ping"
	"echomap/internal/tools"
)

// On-demand network tools (Doc 5 §4). Targets may be an IP or a domain — the
// resolver handles both. Nothing here shells out; no injection surface.

func readTarget(w http.ResponseWriter, r *http.Request, field string) (string, bool) {
	var body map[string]any
	if !readJSON(w, r, &body) {
		return "", false
	}
	t, _ := body[field].(string)
	t = strings.TrimSpace(t)
	if t == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": field + " is required"})
		return "", false
	}
	return t, true
}

func (s *server) toolPing(w http.ResponseWriter, r *http.Request) {
	target, ok := readTarget(w, r, "target")
	if !ok {
		return
	}
	alive, rtt, err := ping.Ping(target, 2*time.Second)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "ping failed: " + err.Error()})
		return
	}
	resp := map[string]any{"target": target, "alive": alive}
	if alive {
		resp["rtt_ms"] = rtt
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) toolTraceroute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target  string `json:"target"`
		MaxHops int    `json:"max_hops"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.Target = strings.TrimSpace(body.Target)
	if body.Target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "target is required"})
		return
	}
	if body.MaxHops <= 0 || body.MaxHops > 30 {
		body.MaxHops = 20
	}
	hops, targetIP, err := tools.Traceroute(r.Context(), body.Target, body.MaxHops, time.Second)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "traceroute failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": body.Target, "target_ip": targetIP, "hops": hops})
}

func (s *server) toolDNSLookup(w http.ResponseWriter, r *http.Request) {
	name, ok := readTarget(w, r, "name")
	if !ok {
		return
	}
	res, err := tools.DNSLookup(r.Context(), name)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "lookup failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}
