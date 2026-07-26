package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"echomap/internal/sysmon"
)

const sysStaleAfter = 90 * time.Second // > 2 collector ticks (30 s) → treat as down

// runSelfReport writes this api role's sys_service{api} point every 30 s (Pillar
// 16). The worker writes sys_host + its own point; both roles self-report because
// they are separate processes.
func (s *server) runSelfReport(ctx context.Context) {
	const interval = 30 * time.Second
	report := func() {
		self := sysmon.ReadSelf(ctx, s.startedAt, s.store, s.redisPinger())
		s.writer.WriteSysService("api", true, &self, map[string]float64{"ws_clients": float64(s.hub.count())})
	}
	report()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			report()
		}
	}
}

// redisPinger returns the bus as a RedisPinger, or nil (avoids a typed-nil that
// would panic on Ping).
func (s *server) redisPinger() sysmon.RedisPinger {
	if s.bus != nil {
		return s.bus
	}
	return nil
}

// systemOverview composes a live health snapshot (Doc 5 §8.8): host + worker from
// the latest Influx points, api from its own live stats, and postgres/redis/
// influx from cheap live probes.
func (s *server) systemOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	host, hostAt, services, err := s.reader.SysLatest(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now()
	collectedAt := hostAt
	overall := "OK"
	degrade := func() { overall = "DEGRADED" }
	out := []map[string]any{}

	// api — this very process, live.
	apiSelf := sysmon.ReadSelf(ctx, s.startedAt, s.store, s.redisPinger())
	out = append(out, map[string]any{
		"service": "api", "up": true, "goroutines": apiSelf.Goroutines, "heap_bytes": apiSelf.HeapBytes,
		"uptime_s": apiSelf.UptimeS, "db_ping_ms": apiSelf.DBPingMs, "redis_ping_ms": apiSelf.RedisPingMs,
		"ws_clients": s.hub.count(),
	})

	// worker — from its latest self-point; a stale point means the collector died.
	if wk, ok := services["worker"]; ok {
		up := now.Sub(wk.At) < sysStaleAfter
		if !up {
			degrade()
		}
		if wk.At.After(collectedAt) {
			collectedAt = wk.At
		}
		out = append(out, map[string]any{
			"service": "worker", "up": up, "goroutines": int(wk.Fields["goroutines"]),
			"heap_bytes": int64(wk.Fields["heap_bytes"]), "uptime_s": wk.Fields["uptime_s"],
			"monitor_backlog": int(wk.Fields["monitor_backlog"]),
		})
	} else {
		degrade()
		out = append(out, map[string]any{"service": "worker", "up": false})
	}

	// postgres / redis — live probes.
	pgMs, pgErr := s.store.PingDB(ctx)
	if pgErr != nil {
		degrade()
	}
	out = append(out, map[string]any{"service": "postgres", "up": pgErr == nil, "ping_ms": pgMs})

	rdUp, rdMs := false, 0.0
	if s.bus != nil {
		if ms, err := s.bus.Ping(ctx); err == nil {
			rdUp, rdMs = true, ms
		}
	}
	if !rdUp {
		degrade()
	}
	out = append(out, map[string]any{"service": "redis", "up": rdUp, "ping_ms": rdMs})

	// influxdb — /health (only degrades the whole system if Influx is configured).
	ixUp := s.influxHealthy(ctx)
	if !ixUp && s.cfg.InfluxToken != "" {
		degrade()
	}
	out = append(out, map[string]any{"service": "influxdb", "up": ixUp})

	if collectedAt.IsZero() {
		collectedAt = now
	}
	_, hasDisk := host["disk_total_bytes"]
	writeJSON(w, http.StatusOK, map[string]any{
		"overall":      overall,
		"collected_at": collectedAt.UTC().Format(time.RFC3339),
		"capabilities": map[string]bool{"docker": false, "hostfs": hasDisk, "nginx": false},
		"host":         host,
		"containers":   []any{},
		"services":     out,
	})
}

// systemHistory reads a ranged series for a target (Doc 5 §8.8).
func (s *server) systemHistory(w http.ResponseWriter, r *http.Request) {
	rg := parseRangeParam(r)
	target := r.URL.Query().Get("target")
	var measurement, tag, tagVal string
	switch {
	case target == "" || target == "host":
		target, measurement = "host", "sys_host"
	case strings.HasPrefix(target, "service:"):
		measurement, tag, tagVal = "sys_service", "service", strings.TrimPrefix(target, "service:")
	case strings.HasPrefix(target, "container:"):
		measurement, tag, tagVal = "sys_container", "container", strings.TrimPrefix(target, "container:")
	default:
		unprocessable(w, "unknown target (want host | service:<name> | container:<name>)")
		return
	}
	points, err := s.reader.SysHistory(r.Context(), measurement, tag, tagVal, rg)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	rows := make([]map[string]any, len(points))
	for i, p := range points {
		m := map[string]any{"t": p.T.UTC().Format(time.RFC3339)}
		for k, v := range p.Fields {
			m[k] = v
		}
		rows[i] = m
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rg.Key, "target": target, "points": rows})
}

func (s *server) influxHealthy(c context.Context) bool {
	if s.cfg.InfluxURL == "" {
		return false
	}
	req, err := http.NewRequestWithContext(c, http.MethodGet, s.cfg.InfluxURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
