package worker

import (
	"context"
	"log"
	"time"

	"echomap/internal/bus"
	"echomap/internal/ping"
	"echomap/internal/store"
)

const defaultDNSRecheck = 300 * time.Second

// runResolver keeps domain-based devices pointed at the right address (Doc 3 §6):
// every interval it re-resolves each device's hostname and, on change, updates
// ip_address and fans out a `device.updated` frame. A failed lookup keeps the
// last-known IP — a DNS hiccup must not interrupt monitoring. b may be nil.
func runResolver(ctx context.Context, st *store.Store, b *bus.Bus, interval time.Duration) {
	tick := func() {
		devs, err := st.ListWithHostname(ctx)
		if err != nil {
			log.Printf("resolver: list devices: %v", err)
			return
		}
		for _, d := range devs {
			newIP, err := ping.Resolve4(ctx, d.Hostname)
			if err != nil || newIP == d.IPAddress {
				continue // DNS hiccup / v6-only / unchanged: keep the last-known IP
			}
			ok, err := st.UpdateDeviceIP(ctx, d.ID, newIP)
			if err != nil {
				log.Printf("resolver: update %s: %v", d.Hostname, err)
				continue
			}
			if !ok {
				log.Printf("resolver: %s now resolves to %s, already monitored by another device — keeping %s",
					d.Hostname, newIP, d.IPAddress)
				continue
			}
			log.Printf("resolver: %s re-resolved %s -> %s", d.Hostname, d.IPAddress, newIP)
			id := d.ID
			_ = st.InsertEventLog(ctx, "NOTICE", "device",
				d.Hostname+" re-resolved "+d.IPAddress+" → "+newIP, &id, nil,
				map[string]any{"old_ip": d.IPAddress, "new_ip": newIP})
			if b != nil {
				_ = b.PublishUpdated(ctx, bus.UpdatedEvent{
					DeviceID: d.ID, IPAddress: newIP, Hostname: d.Hostname, Name: d.Name,
				})
			}
		}
	}

	tick() // a hostname may have moved while the worker was down
	ticker := time.NewTicker(interval)
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
