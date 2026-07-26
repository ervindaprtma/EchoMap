package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"echomap/internal/bus"
	"echomap/internal/moncheck"
	"echomap/internal/monitor"
	"echomap/internal/notify"
	"echomap/internal/store"
	"echomap/internal/tsdb"
)

// monitorTick is how often the scheduler looks for due monitors. The cursor
// (next_check_at) is the real cadence source; this is just the polling grain.
const monitorTick = 5 * time.Second

// runMonitorScheduler is the Phase 9 custom-monitor loop (Doc 3 §9): every tick it
// claims due monitors, checks each on a bounded pool, and runs the same debounce
// state machine as the ping loop. A confirmed transition writes an event log,
// fans out a `monitor.status` WS frame, and routes an alert. Monitors are paused
// while their device itself is DOWN (no double-alerting a dead box). b/tw/alerter
// may be nil.
func runMonitorScheduler(ctx context.Context, st *store.Store, b *bus.Bus, tw *tsdb.Writer, alerter *notify.Multi, concurrency int) {
	sem := make(chan struct{}, concurrency)
	tick := func() {
		due, err := st.ClaimDueMonitors(ctx)
		if err != nil {
			log.Printf("monitor: claim due: %v", err)
			return
		}
		var wg sync.WaitGroup
		for _, m := range due {
			wg.Add(1)
			sem <- struct{}{}
			go func(m store.DueMonitor) {
				defer wg.Done()
				defer func() { <-sem }()
				checkMonitor(ctx, st, b, tw, alerter, m)
			}(m)
		}
		wg.Wait()
	}

	tick() // don't wait a full tick for the first pass
	ticker := time.NewTicker(monitorTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

func checkMonitor(ctx context.Context, st *store.Store, b *bus.Bus, tw *tsdb.Writer, alerter *notify.Multi, m store.DueMonitor) {
	if m.DeviceStatus == "DOWN" {
		return // device itself is down: pause its monitors, don't double-alert
	}
	res := runCheck(ctx, m)

	// Metrics: every check is written (history stays continuous), like ping.
	var certDays *int
	if res.CertExpiresAt != nil {
		d := int(time.Until(*res.CertExpiresAt).Hours() / 24)
		certDays = &d
	}
	if tw != nil {
		tw.WriteMonitor(m.ID, m.DeviceID, m.Kind, res.OK, res.DurationMs, res.HTTPStatus, certDays)
	}

	// Debounce → confirmed transition (same 3-consecutive contract as devices).
	newStatus, up, down, changed := stepDebounce(m.Status, m.UpStreak, m.DownStreak, res.OK)
	dur := res.DurationMs
	if err := st.SaveMonitorCheck(ctx, m.ID, newStatus, up, down, changed, &dur, res.HTTPStatus, res.CertExpiresAt); err != nil {
		log.Printf("monitor: save %d: %v", m.ID, err)
		return
	}
	if !changed {
		return
	}

	from := m.Status
	if b != nil {
		_ = b.PublishMonitorStatus(ctx, bus.MonitorStatusEvent{
			MonitorID: m.ID, DeviceID: m.DeviceID, Label: m.Label, Kind: m.Kind,
			FromStatus: from, ToStatus: newStatus, HTTPStatus: res.HTTPStatus, ChangedAt: time.Now(),
		})
	}
	did := m.DeviceID
	st.Info(ctx, "monitor", fmt.Sprintf("%s (%s) %s → %s", m.Label, m.Kind, from, newStatus), &did)
	if alerter != nil && (newStatus == "UP" || newStatus == "DOWN") {
		alerter.MonitorAlert(ctx, m.DeviceID, m.Label, m.Kind, from, newStatus)
	}
}

func runCheck(ctx context.Context, m store.DueMonitor) moncheck.Result {
	port := 0
	if m.Port != nil {
		port = *m.Port
	}
	return moncheck.Check(ctx, moncheck.Spec{
		Kind: m.Kind, Host: m.DeviceIP, Port: port,
		Path: deref(m.Path), URLOverride: deref(m.URLOverride),
		ExpectLo: m.ExpectLo, ExpectHi: m.ExpectHi,
		Timeout: time.Duration(m.TimeoutMs) * time.Millisecond,
	})
}

// stepDebounce advances one monitor's debounce state by one check result and
// reports whether this produced a confirmed transition (Doc 3 §2, shared contract).
func stepDebounce(cur string, up, down int, ok bool) (status string, newUp, newDown int, transitioned bool) {
	if ok {
		up++
		down = 0
	} else {
		down++
		up = 0
	}
	next := cur
	if ok && up >= monitor.DebounceCount && cur != "UP" {
		next = "UP"
	}
	if !ok && down >= monitor.DebounceCount && cur != "DOWN" {
		next = "DOWN"
	}
	return next, up, down, next != cur
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
