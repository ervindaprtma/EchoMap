package store

import (
	"context"
	"time"

	"echomap/internal/monitor"
)

// MonDevice is the slim projection the ping loop needs.
type MonDevice struct {
	ID        int64
	IPAddress string
	Name      string
	Site      string
	Status    string
}

func (s *Store) ListForMonitoring(ctx context.Context) ([]MonDevice, error) {
	// Cascade victims (DOWN via a dead parent) are excluded: they are unreachable
	// through that parent anyway, so probing them wastes the pool (Doc 3 §5).
	// CascadeUp releases them back into the sweep.
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, host(d.ip_address), d.name, COALESCE(s.site, ''), d.status::text
		FROM devices d
		LEFT JOIN subnets s ON s.id = d.subnet_id
		WHERE NOT (d.status = 'DOWN' AND d.down_reason = 'PARENT')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MonDevice{}
	for rows.Next() {
		var d MonDevice
		if err := rows.Scan(&d.ID, &d.IPAddress, &d.Name, &d.Site, &d.Status); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ApplyStatusChange(ctx context.Context, id int64, from, to string, alive bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE devices
		SET previous_status = $2::device_status,
		    status          = $3::device_status,
		    down_reason     = CASE WHEN $3 = 'DOWN' THEN 'PROBE'::down_reason ELSE NULL END,
		    last_change_at  = now(),
		    last_seen_at    = CASE WHEN $4 THEN now() ELSE last_seen_at END,
		    updated_at      = now()
		WHERE id = $1`, id, from, to, alive)
	return err
}

// CascadeDown flips every descendant of parentID to DOWN with down_reason='PARENT'
// (Doc 3 §5) in one statement. UNION (not UNION ALL) so an accidental parent cycle
// terminates instead of looping; the API's walk-up check is the primary guard.
// Children already DOWN on their own probe are left untouched (they keep 'PROBE').
func (s *Store) CascadeDown(ctx context.Context, parentID int64) ([]monitor.Child, error) {
	return s.cascade(ctx, `
		WITH RECURSIVE kids AS (
			SELECT id FROM devices WHERE parent_device_id = $1
			UNION
			SELECT d.id FROM devices d JOIN kids k ON d.parent_device_id = k.id
		)
		UPDATE devices d
		SET previous_status = d.status,
		    status          = 'DOWN',
		    down_reason     = 'PARENT',
		    last_change_at  = now(),
		    updated_at      = now()
		FROM kids
		WHERE d.id = kids.id AND d.status <> 'DOWN'
		RETURNING d.id, host(d.ip_address), d.name, d.previous_status::text`, parentID)
}

// CascadeUp releases descendants that were DOWN only because of parentID back to
// UNKNOWN; their next probe resolves UP or genuinely DOWN. Children DOWN by their
// own probe (down_reason='PROBE') are not touched.
func (s *Store) CascadeUp(ctx context.Context, parentID int64) ([]monitor.Child, error) {
	return s.cascade(ctx, `
		WITH RECURSIVE kids AS (
			SELECT id FROM devices WHERE parent_device_id = $1
			UNION
			SELECT d.id FROM devices d JOIN kids k ON d.parent_device_id = k.id
		)
		UPDATE devices d
		SET previous_status = d.status,
		    status          = 'UNKNOWN',
		    down_reason     = NULL,
		    last_change_at  = now(),
		    updated_at      = now()
		FROM kids
		WHERE d.id = kids.id AND d.status = 'DOWN' AND d.down_reason = 'PARENT'
		RETURNING d.id, host(d.ip_address), d.name, d.previous_status::text`, parentID)
}

func (s *Store) cascade(ctx context.Context, sql string, parentID int64) ([]monitor.Child, error) {
	rows, err := s.pool.Query(ctx, sql, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []monitor.Child{}
	for rows.Next() {
		var c monitor.Child
		if err := rows.Scan(&c.ID, &c.IP, &c.Name, &c.From); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) RecordTransition(ctx context.Context, id int64, to string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO status_transitions (device_id, to_status) VALUES ($1, $2::device_status)`, id, to)
	return err
}

func (s *Store) CountTransitions(ctx context.Context, id int64, window time.Duration) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM status_transitions
		WHERE device_id = $1 AND changed_at > now() - make_interval(secs => $2)`,
		id, window.Seconds()).Scan(&n)
	return n, err
}

func (s *Store) SetFlapping(ctx context.Context, id int64, flapping bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE devices SET is_flapping = $2, updated_at = now() WHERE id = $1`, id, flapping)
	return err
}
