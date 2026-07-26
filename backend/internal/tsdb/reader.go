// Reader is the read side of tsdb: Flux queries over the same bucket the Writer
// fills (Doc 5 §8.5–8.6). Ping and custom-monitor metrics share one bucket and
// are separated by _measurement. Like Writer, an empty url/token yields an inert
// reader whose queries return empty series — so the History page shows an honest
// "no samples" state instead of erroring when InfluxDB isn't configured.
package tsdb

import (
	"context"
	"fmt"
	"math"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

type Reader struct {
	client influxdb2.Client
	query  api.QueryAPI
	bucket string
}

// NewReader returns a query client. Empty url/token → inert (see package doc).
func NewReader(url, token, org, bucket string) *Reader {
	if url == "" || token == "" {
		return &Reader{}
	}
	c := influxdb2.NewClient(url, token)
	return &Reader{client: c, query: c.QueryAPI(org), bucket: bucket}
}

func (r *Reader) Close() {
	if r.client != nil {
		r.client.Close()
	}
}

// Range is a validated history window and the aggregation grain it reads at.
// The grain is chosen so every range yields ~120–360 points — enough for a
// smooth line, few enough to send and render cheaply.
type Range struct {
	Key    string // "1h" | "6h" | "24h" | "7d" | "30d"
	Start  string // Flux relative duration, e.g. "-24h"
	Window string // aggregateWindow every, e.g. "5m"
}

var ranges = map[string]Range{
	"1h":  {"1h", "-1h", "30s"},
	"6h":  {"6h", "-6h", "2m"},
	"24h": {"24h", "-24h", "5m"},
	"7d":  {"7d", "-7d", "30m"},
	"30d": {"30d", "-30d", "2h"},
}

// ParseRange resolves a ?range= value, defaulting to 24h for empty/unknown input.
func ParseRange(s string) Range {
	if r, ok := ranges[s]; ok {
		return r
	}
	return ranges["24h"]
}

type PingPoint struct {
	T            time.Time `json:"t"`
	LatencyAvg   *float64  `json:"latency_avg"`
	LatencyP95   *float64  `json:"latency_p95"`
	PacketLoss   *float64  `json:"packet_loss"`
	Jitter       *float64  `json:"jitter"`
	Availability *float64  `json:"availability"`
}

// PingSeries returns the device's ICMP history for the range. Two Flux passes
// (mean-family fields, then the p95 quantile) merged on the shared window edge,
// with jitter derived in Go (see deriveJitter).
func (r *Reader) PingSeries(ctx context.Context, deviceID int64, rg Range) ([]PingPoint, error) {
	if r.query == nil {
		return []PingPoint{}, nil
	}
	byT := map[int64]*PingPoint{}
	order := []int64{}
	at := func(t time.Time) *PingPoint {
		k := t.UnixNano()
		p, ok := byT[k]
		if !ok {
			p = &PingPoint{T: t}
			byT[k] = p
			order = append(order, k)
		}
		return p
	}

	// Pass 1: latency mean, packet loss, reachable → availability%.
	meanFlux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s)
  |> filter(fn: (r) => r._measurement == "ping_metrics" and r.device_id == "%d")
  |> filter(fn: (r) => r._field == "latency_ms" or r._field == "packet_loss" or r._field == "reachable")
  |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
  |> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
  |> sort(columns: ["_time"])`, r.bucket, rg.Start, deviceID, rg.Window)
	res, err := r.query.Query(ctx, meanFlux)
	if err != nil {
		return nil, err
	}
	for res.Next() {
		v := res.Record().Values()
		p := at(res.Record().Time())
		p.LatencyAvg = fptr(v["latency_ms"])
		p.PacketLoss = fptr(v["packet_loss"])
		if a := fptr(v["reachable"]); a != nil {
			pct := *a * 100
			p.Availability = &pct
		}
	}
	if res.Err() != nil {
		return nil, res.Err()
	}

	// Pass 2: p95 latency (per-window quantile).
	p95Flux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s)
  |> filter(fn: (r) => r._measurement == "ping_metrics" and r.device_id == "%d" and r._field == "latency_ms")
  |> aggregateWindow(every: %s, fn: (column, tables=<-) => tables |> quantile(q: 0.95, column: column), createEmpty: false)
  |> sort(columns: ["_time"])`, r.bucket, rg.Start, deviceID, rg.Window)
	res, err = r.query.Query(ctx, p95Flux)
	if err != nil {
		return nil, err
	}
	for res.Next() {
		at(res.Record().Time()).LatencyP95 = fptr(res.Record().Value())
	}
	if res.Err() != nil {
		return nil, res.Err()
	}

	points := make([]PingPoint, 0, len(order))
	for _, k := range order {
		points = append(points, *byT[k])
	}
	sortPoints(points)
	deriveJitter(points)
	return points, nil
}

type MonitorPoint struct {
	T            time.Time `json:"t"`
	DurationMs   *float64  `json:"duration_ms"`
	Availability *float64  `json:"availability"`
}

type StatusBandPoint struct {
	T    time.Time `json:"t"`
	C2xx int       `json:"c2xx"`
	C3xx int       `json:"c3xx"`
	C4xx int       `json:"c4xx"`
	C5xx int       `json:"c5xx"`
}

type MonitorSeries struct {
	Kind         string            `json:"kind"`
	Points       []MonitorPoint    `json:"points"`
	StatusBand   []StatusBandPoint `json:"status_band,omitempty"`   // HTTP/HTTPS
	CertDaysLeft *int              `json:"cert_days_left,omitempty"` // HTTPS
}

// MonitorSeries returns one custom monitor's history. duration/availability for
// every kind; the HTTP status-code band and (HTTPS) TLS days-to-expiry only for
// the kinds that have them. kind is passed in (from the DB row) so a monitor with
// no samples yet still reports its type to the UI.
func (r *Reader) MonitorSeries(ctx context.Context, monitorID int64, kind string, rg Range) (MonitorSeries, error) {
	out := MonitorSeries{Kind: kind, Points: []MonitorPoint{}}
	if r.query == nil {
		return out, nil
	}

	meanFlux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s)
  |> filter(fn: (r) => r._measurement == "monitor_metrics" and r.monitor_id == "%d")
  |> filter(fn: (r) => r._field == "duration_ms" or r._field == "up")
  |> aggregateWindow(every: %s, fn: mean, createEmpty: false)
  |> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
  |> sort(columns: ["_time"])`, r.bucket, rg.Start, monitorID, rg.Window)
	res, err := r.query.Query(ctx, meanFlux)
	if err != nil {
		return out, err
	}
	for res.Next() {
		v := res.Record().Values()
		mp := MonitorPoint{T: res.Record().Time(), DurationMs: fptr(v["duration_ms"])}
		if a := fptr(v["up"]); a != nil {
			pct := *a * 100
			mp.Availability = &pct
		}
		out.Points = append(out.Points, mp)
	}
	if res.Err() != nil {
		return out, res.Err()
	}

	if kind != "HTTP" && kind != "HTTPS" {
		return out, nil
	}

	// Status-code band: classify each http_status, count per class per window.
	bandFlux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s)
  |> filter(fn: (r) => r._measurement == "monitor_metrics" and r.monitor_id == "%d" and r._field == "http_status")
  |> map(fn: (r) => ({ r with class:
      if r._value >= 500 then "c5xx"
      else if r._value >= 400 then "c4xx"
      else if r._value >= 300 then "c3xx"
      else if r._value >= 200 then "c2xx"
      else "other" }))
  |> group(columns: ["class"])
  |> aggregateWindow(every: %s, fn: count, createEmpty: false)
  |> pivot(rowKey: ["_time"], columnKey: ["class"], valueColumn: "_value")
  |> sort(columns: ["_time"])`, r.bucket, rg.Start, monitorID, rg.Window)
	res, err = r.query.Query(ctx, bandFlux)
	if err != nil {
		return out, err
	}
	for res.Next() {
		v := res.Record().Values()
		out.StatusBand = append(out.StatusBand, StatusBandPoint{
			T:    res.Record().Time(),
			C2xx: iVal(v["c2xx"]), C3xx: iVal(v["c3xx"]),
			C4xx: iVal(v["c4xx"]), C5xx: iVal(v["c5xx"]),
		})
	}
	if res.Err() != nil {
		return out, res.Err()
	}

	if kind == "HTTPS" {
		certFlux := fmt.Sprintf(`from(bucket: %q)
  |> range(start: %s)
  |> filter(fn: (r) => r._measurement == "monitor_metrics" and r.monitor_id == "%d" and r._field == "cert_days_left")
  |> last()`, r.bucket, rg.Start, monitorID)
		res, err = r.query.Query(ctx, certFlux)
		if err != nil {
			return out, err
		}
		for res.Next() {
			if f := fptr(res.Record().Value()); f != nil {
				d := int(*f)
				out.CertDaysLeft = &d
			}
		}
		if res.Err() != nil {
			return out, res.Err()
		}
	}
	return out, nil
}

// deriveJitter fills each point's Jitter from the RFC 3550 (§A.8) smoothing
// filter over the latency series: J += (|Lᵢ − Lᵢ₋₁| − J) / 16.
// ponytail: derived from per-window mean latency, not raw inter-arrival times —
// so it tracks jitter of the smoothed signal. Upgrade to a raw-sample pass if
// sub-window jitter ever matters.
func deriveJitter(points []PingPoint) {
	var j float64
	var prev *float64
	for i := range points {
		l := points[i].LatencyAvg
		if l == nil {
			prev = nil // gap breaks the running estimate
			continue
		}
		if prev != nil {
			j += (math.Abs(*l-*prev) - j) / 16
			v := j
			points[i].Jitter = &v
		}
		prev = l
	}
}

func sortPoints(p []PingPoint) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j].T.Before(p[j-1].T); j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

// fptr coerces an InfluxDB numeric cell (float64/int64) to *float64, nil if absent.
func fptr(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case int64:
		f := float64(n)
		return &f
	}
	return nil
}

func iVal(v any) int {
	if f := fptr(v); f != nil {
		return int(*f)
	}
	return 0
}
