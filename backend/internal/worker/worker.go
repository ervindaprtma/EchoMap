// Package worker runs the background jobs (the `worker` role). Slice 2 lights up
// the ping loop: probe every device, write metrics, run debounce/flap, fan out
// confirmed changes. discovery / snmp / netbox-sync / notifier attach in later slices.
package worker

import (
	"context"
	"log"
	"sync"
	"time"

	"echomap/internal/bus"
	"echomap/internal/config"
	"echomap/internal/monitor"
	"echomap/internal/notify"
	"echomap/internal/ping"
	"echomap/internal/store"
	"echomap/internal/tsdb"
)

const pingTimeout = time.Second

// Run boots the ping loop and blocks until ctx is cancelled. b may be nil (no Redis).
func Run(ctx context.Context, cfg config.Config, st *store.Store, b *bus.Bus, tw *tsdb.Writer) error {
	var pub monitor.Publisher
	if b != nil {
		pub = b // avoid a typed-nil interface: only assign when non-nil
	}
	// Fan alerts out to every channel; each no-ops until it's configured.
	alerter := notify.NewMulti(notify.NewTelegram(st), notify.NewEmail(st))
	mon := monitor.New(st, pub, tw)
	mon.Alerter = alerter
	mon.Events = st // INFO-log confirmed transitions to event_logs (Doc 3 §10)

	// Custom port/URL monitors run on their own cursor-driven loop (Doc 3 §9),
	// beside the ping loop and the resolver, sharing the same alerter/store/tsdb.
	go runMonitorScheduler(ctx, st, b, tw, alerter, cfg.DiscoveryConcurrency)

	interval := time.Duration(cfg.PingIntervalSeconds) * time.Second

	// Hostname re-resolution runs on its own cadence (Doc 3 §6). ponytail: the
	// cadence is read once at boot — there is no settings UI to change it live yet,
	// so re-reading it every tick would buy nothing.
	dnsEvery := defaultDNSRecheck
	if n, err := st.DNSRecheckSeconds(ctx); err != nil {
		log.Printf("worker: dns_recheck_seconds unreadable (%v), using %s", err, dnsEvery)
	} else if n > 0 {
		dnsEvery = time.Duration(n) * time.Second
	}
	go runResolver(ctx, st, b, dnsEvery)

	// Hourly retention sweep (Doc 3 §4) — keeps the append-only + session tables
	// bounded. Boot sweep clears backlog immediately.
	go runHousekeeping(ctx, st)

	log.Printf("worker started (ping interval %s, concurrency %d, dns recheck %s)",
		interval, cfg.DiscoveryConcurrency, dnsEvery)

	sweep := func() {
		devs, err := st.ListForMonitoring(ctx)
		if err != nil {
			log.Printf("worker: list devices: %v", err)
			return
		}
		var errOnce sync.Once
		sem := make(chan struct{}, cfg.DiscoveryConcurrency)
		var wg sync.WaitGroup
		for _, d := range devs {
			wg.Add(1)
			sem <- struct{}{}
			go func(d store.MonDevice) {
				defer wg.Done()
				defer func() { <-sem }()
				alive, rtt, err := ping.Ping(d.IPAddress, pingTimeout)
				if err != nil {
					errOnce.Do(func() {
						log.Printf("worker: ping failed (need CAP_NET_RAW? run via docker or sudo): %v", err)
					})
				}
				mon.Evaluate(ctx, monitor.Device{
					ID: d.ID, IP: d.IPAddress, Name: d.Name, Site: d.Site, Status: d.Status,
				}, alive, rtt)
			}(d)
		}
		wg.Wait()
	}

	sweep() // don't wait a full interval for the first sweep
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			sweep()
		}
	}
}
