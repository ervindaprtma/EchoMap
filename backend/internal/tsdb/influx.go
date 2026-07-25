// Package tsdb writes ping metrics to InfluxDB (Doc 2 §3).
package tsdb

import (
	"strconv"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
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

func (w *Writer) Close() {
	if w.client != nil {
		w.write.Flush()
		w.client.Close()
	}
}
