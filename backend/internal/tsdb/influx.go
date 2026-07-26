// Package tsdb writes ping metrics to InfluxDB (Doc 2 §3).
package tsdb

import (
	"strconv"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"

	"echomap/internal/sysmon"
)

type Writer struct {
	client influxdb2.Client
	write  api.WriteAPI
}

// New returns an async metrics writer. With an empty url or token it returns an
// inert writer whose WritePing is a no-op (lets the worker run without InfluxDB).
func New(url, token, org, bucket string) *Writer {
	if url == "" || token == "" {
		return &Writer{}
	}
	c := influxdb2.NewClient(url, token)
	return &Writer{client: c, write: c.WriteAPI(org, bucket)}
}

// WritePing records one probe. Every probe is written (history stays continuous),
// independent of whether it produced a state change.
func (w *Writer) WritePing(deviceID int64, ip, site string, alive bool, rttMs float64) {
	if w.write == nil {
		return
	}
	reachable, loss := 0, 100.0
	if alive {
		reachable, loss = 1, 0.0
	}
	p := influxdb2.NewPointWithMeasurement("ping_metrics").
		AddTag("device_id", strconv.FormatInt(deviceID, 10)).
		AddTag("ip_address", ip).
		AddTag("site", site).
		AddField("latency_ms", rttMs).
		AddField("packet_loss", loss).
		AddField("reachable", reachable).
		SetTime(time.Now())
	w.write.WritePoint(p)
}

// WriteMonitor records one custom-monitor check to the `monitor_metrics`
// measurement (Doc 2 §4.6). httpStatus/certDaysLeft are nil for TCP checks.
func (w *Writer) WriteMonitor(monitorID, deviceID int64, kind string, up bool, durationMs float64, httpStatus, certDaysLeft *int) {
	if w.write == nil {
		return
	}
	upVal := 0
	if up {
		upVal = 1
	}
	p := influxdb2.NewPointWithMeasurement("monitor_metrics").
		AddTag("monitor_id", strconv.FormatInt(monitorID, 10)).
		AddTag("device_id", strconv.FormatInt(deviceID, 10)).
		AddTag("kind", kind).
		AddField("up", upVal).
		AddField("duration_ms", durationMs)
	if httpStatus != nil {
		p.AddField("http_status", *httpStatus)
	}
	if certDaysLeft != nil {
		p.AddField("cert_days_left", *certDaysLeft)
	}
	p.SetTime(time.Now())
	w.write.WritePoint(p)
}

// WriteSysHost records one host sample (Pillar 16, sys_host). Disk fields are
// omitted when /hostfs isn't mounted — never zero-filled.
func (w *Writer) WriteSysHost(h sysmon.HostStats) {
	if w.write == nil {
		return
	}
	p := influxdb2.NewPointWithMeasurement("sys_host").
		AddField("cpu_pct", h.CPUPct).
		AddField("mem_used_bytes", int64(h.MemUsed)).
		AddField("mem_total_bytes", int64(h.MemTotal)).
		AddField("swap_used_bytes", int64(h.SwapUsed)).
		AddField("swap_total_bytes", int64(h.SwapTotal)).
		AddField("load1", h.Load1).
		AddField("load5", h.Load5).
		AddField("load15", h.Load15).
		AddField("uptime_s", h.UptimeS)
	if h.HasDisk {
		p.AddField("disk_used_bytes", int64(h.DiskUsed)).
			AddField("disk_total_bytes", int64(h.DiskTotal))
	}
	p.SetTime(time.Now())
	w.write.WritePoint(p)
}

// WriteSysService records one service self-point (sys_service, tag=service). self
// is nil for non-Go services; extra carries role-specific fields (ws_clients,
// monitor_backlog, …).
func (w *Writer) WriteSysService(service string, up bool, self *sysmon.SelfStats, extra map[string]float64) {
	if w.write == nil {
		return
	}
	upVal := 0
	if up {
		upVal = 1
	}
	p := influxdb2.NewPointWithMeasurement("sys_service").
		AddTag("service", service).
		AddField("up", upVal)
	if self != nil {
		p.AddField("goroutines", int64(self.Goroutines)).
			AddField("heap_bytes", int64(self.HeapBytes)).
			AddField("uptime_s", self.UptimeS).
			AddField("db_pool_total", int64(self.DBPoolTotal)).
			AddField("db_pool_idle", int64(self.DBPoolIdle)).
			AddField("db_ping_ms", self.DBPingMs).
			AddField("redis_ping_ms", self.RedisPingMs)
	}
	for k, v := range extra {
		p.AddField(k, v)
	}
	p.SetTime(time.Now())
	w.write.WritePoint(p)
}

func (w *Writer) Close() {
	if w.client != nil {
		w.write.Flush()
		w.client.Close()
	}
}
