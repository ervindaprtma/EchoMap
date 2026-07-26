// Package nbsync holds the Netbox sync diff (Doc 3 §3), shared by the hourly
// worker loop and the on-demand "Sync now" API handler. It lives in its own
// package because store imports netbox (so netbox can't import store) — nbsync is
// the neutral home that can import store + bus + netbox together.
package nbsync

import (
	"context"
	"log"
	"time"

	"echomap/internal/bus"
	"echomap/internal/netbox"
	"echomap/internal/store"
)

// Run performs one Netbox↔DB diff. The rule that matters: an IP gone from Netbox
// is NOT hard-deleted — it becomes ORPHANED and waits for a human decision.
// Returns the sync log it persisted.
func Run(ctx context.Context, st *store.Store, b *bus.Bus, nb *netbox.Client) (store.SyncLog, error) {
	start := time.Now()
	l := store.SyncLog{RunAt: start, Status: "SUCCESS"}

	ips, err := nb.FetchAllIPs(ctx)
	if err != nil {
		ms := int(time.Since(start).Milliseconds())
		e := err.Error()
		failed := store.SyncLog{RunAt: start, Status: "FAILED", DurationMs: &ms, Error: &e}
		_ = st.SaveSyncLog(ctx, failed)
		return failed, err
	}

	current, err := st.ListNetboxDevices(ctx)
	if err != nil {
		return l, err
	}

	seen := make(map[string]bool, len(ips))
	for _, obj := range ips {
		seen[obj.IP] = true
		dev, exists := current[obj.IP]
		if !exists {
			if inserted, err := st.InsertNetboxDevice(ctx, obj); err == nil && inserted {
				l.IPsAdded++
			}
			continue
		}
		_ = st.RefreshNetboxDevice(ctx, dev.ID, obj)
		l.IPsUpdated++
		if dev.Status == "ORPHANED" { // reappeared in Netbox -> restore to monitoring
			publishTransition(ctx, st, b, dev.ID, "UNKNOWN")
			l.IPsRestored++
		}
	}

	// Present in the DB as NETBOX but gone from Netbox -> ORPHANED (retained).
	for ip, dev := range current {
		if seen[ip] || dev.Status == "ORPHANED" {
			continue
		}
		publishTransition(ctx, st, b, dev.ID, "ORPHANED")
		l.IPsOrphaned++
		log.Printf("netbox-sync: %s missing from Netbox -> ORPHANED (retained)", ip)
	}

	ms := int(time.Since(start).Milliseconds())
	l.DurationMs = &ms
	return l, st.SaveSyncLog(ctx, l)
}

// publishTransition flips status and pushes a device.status frame so the map/table
// recolor live (amber on ORPHANED).
func publishTransition(ctx context.Context, st *store.Store, b *bus.Bus, id int64, to string) {
	c, err := st.SetDeviceStatus(ctx, id, to)
	if err != nil {
		log.Printf("netbox-sync: set status device=%d: %v", id, err)
		return
	}
	if b != nil {
		_ = b.PublishStatus(ctx, bus.StatusEvent{
			DeviceID: id, IPAddress: c.IP, Name: c.Name,
			FromStatus: c.From, ToStatus: to, ChangedAt: time.Now(),
		})
	}
}
