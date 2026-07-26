package worker

import (
	"context"
	"log"
	"time"

	"echomap/internal/bus"
	"echomap/internal/nbsync"
	"echomap/internal/netbox"
	"echomap/internal/store"
)

const netboxSyncInterval = time.Hour

// runNetboxSync runs the Netbox diff every hour + on boot (Doc 3 §3). No-ops when
// Netbox is unconfigured or sync is disabled. The diff itself lives in nbsync so
// the on-demand "Sync now" API handler shares it.
func runNetboxSync(ctx context.Context, st *store.Store, b *bus.Bus) {
	tick := time.NewTicker(netboxSyncInterval)
	defer tick.Stop()

	sync := func() {
		url, token, enabled, err := st.NetboxCredentials(ctx)
		if err != nil || !enabled || url == "" || token == "" {
			return
		}
		if _, err := nbsync.Run(ctx, st, b, netbox.New(url, token)); err != nil {
			log.Printf("netbox-sync: %v", err)
		}
	}

	sync() // boot sync
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			sync()
		}
	}
}
