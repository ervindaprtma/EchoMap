package worker

import (
	"context"
	"time"

	"echomap/internal/bus"
	"echomap/internal/store"
	"echomap/internal/sysmon"
	"echomap/internal/tsdb"
)

// runSysmon collects host + worker self stats to Influx every 30 s (Pillar 16,
// Doc 3 §11). Host CPU% is a jiffy delta, so the reader is primed before the loop.
func runSysmon(ctx context.Context, st *store.Store, b *bus.Bus, tw *tsdb.Writer) {
	const interval = 30 * time.Second
	start := time.Now()
	host := sysmon.NewHostReader()
	host.Read() // prime the CPU delta baseline

	var redis sysmon.RedisPinger
	if b != nil {
		redis = b
	}
	tick := func() {
		tw.WriteSysHost(host.Read())
		self := sysmon.ReadSelf(ctx, start, st, redis)
		backlog, _ := st.MonitorBacklog(ctx)
		tw.WriteSysService("worker", true, &self, map[string]float64{"monitor_backlog": float64(backlog)})
	}
	// Brief settle so the first CPU delta spans a real interval, not ~0.
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Second):
	}
	tick()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
