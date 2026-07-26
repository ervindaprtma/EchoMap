// History metrics read endpoints (Doc 5 §8.5–8.6, Phase 10). Both are Operator+
// GETs backed by InfluxDB via the tsdb.Reader. When Influx is unconfigured the
// reader is inert and these return empty series (200), never an error — the
// History page renders its "no samples" state.
package api

import (
	"net/http"

	"echomap/internal/tsdb"
)

func parseRangeParam(r *http.Request) tsdb.Range {
	return tsdb.ParseRange(r.URL.Query().Get("range"))
}

// GET /api/v1/devices/{id}/metrics/ping?range=1h|6h|24h|7d|30d
func (s *server) metricsPing(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetDevice(r.Context(), id); err != nil {
		writeErr(w, err) // 404 for unknown device
		return
	}
	rg := parseRangeParam(r)
	points, err := s.reader.PingSeries(r.Context(), id, rg)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "metrics query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rg.Key, "points": points})
}

// GET /api/v1/monitors/{id}/metrics?range=1h|6h|24h|7d|30d
func (s *server) metricsMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(w, r)
	if !ok {
		return
	}
	mon, err := s.store.GetMonitor(r.Context(), id)
	if err != nil {
		writeErr(w, err) // 404 for unknown monitor
		return
	}
	rg := parseRangeParam(r)
	series, err := s.reader.MonitorSeries(r.Context(), id, mon.Kind, rg)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "metrics query failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rg.Key, "series": series})
}
