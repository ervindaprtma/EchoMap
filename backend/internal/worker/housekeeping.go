package worker

import (
	"context"
	"log"
	"time"

	"echomap/internal/store"
)

// runHousekeeping prunes the retention tables hourly (Doc 3 §4). One ticker, five
// range deletes; the boot sweep clears whatever accumulated while it was off.
func runHousekeeping(ctx context.Context, st *store.Store) {
	const interval = time.Hour
	sweep := func() {
		res, err := st.Housekeep(ctx)
		if err != nil {
			log.Printf("housekeeping sweep failed: %v", err)
			return
		}
		if res.Total() > 0 {
			log.Printf("housekeeping: pruned %d rows (transitions=%d alert_events=%d sync_logs=%d event_logs=%d sessions=%d)",
				res.Total(), res.Transitions, res.AlertEvents, res.SyncLogs, res.EventLogs, res.Sessions)
		}
	}
	sweep() // boot sweep
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}
