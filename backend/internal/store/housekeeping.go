package store

import (
	"context"
	"fmt"
)

// SweepResult reports how many rows the retention sweep pruned per table.
type SweepResult struct {
	Transitions int64
	AlertEvents int64
	SyncLogs    int64
	EventLogs   int64
	Sessions    int64
}

func (r SweepResult) Total() int64 {
	return r.Transitions + r.AlertEvents + r.SyncLogs + r.EventLogs + r.Sessions
}

// Housekeep prunes the append-only + session tables to their retention windows
// (Doc 2 §1.12/§4.7, Doc 3 §4). Each is an indexed range delete. The session TTLs
// reuse the auth-time constants so the sweep can never delete a session the
// middleware would still accept, nor keep one it would reject.
func (s *Store) Housekeep(ctx context.Context) (SweepResult, error) {
	var r SweepResult
	del := func(dst *int64, sql string, args ...any) error {
		ct, err := s.pool.Exec(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("housekeep: %w", err)
		}
		*dst = ct.RowsAffected()
		return nil
	}

	if err := del(&r.Transitions,
		`DELETE FROM status_transitions WHERE changed_at < now() - interval '24 hours'`); err != nil {
		return r, err
	}
	if err := del(&r.AlertEvents,
		`DELETE FROM alert_events WHERE fired_at < now() - interval '90 days'`); err != nil {
		return r, err
	}
	if err := del(&r.SyncLogs,
		`DELETE FROM netbox_sync_logs WHERE run_at < now() - interval '90 days'`); err != nil {
		return r, err
	}
	if err := del(&r.EventLogs,
		`DELETE FROM event_logs WHERE ts < now() - interval '90 days'`); err != nil { // event_logs stamps `ts`, not created_at
		return r, err
	}
	// Idle (last_seen_at) OR absolute-age (created_at) expiry — mirrors SessionUser.
	if err := del(&r.Sessions, `
		DELETE FROM sessions
		WHERE last_seen_at < now() - make_interval(secs => $1)
		   OR created_at   < now() - make_interval(secs => $2)`,
		SessionIdleTimeout.Seconds(), SessionMaxLifetime.Seconds()); err != nil {
		return r, err
	}
	return r, nil
}
