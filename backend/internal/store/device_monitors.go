package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Monitor is the API-facing custom-monitor row (Doc 2 §4.4). Runtime state
// (streaks, cursor) is included so the Monitoring tab can show live status.
type Monitor struct {
	ID             int64      `json:"id"`
	DeviceID       int64      `json:"device_id"`
	Label          string     `json:"label"`
	Kind           string     `json:"kind"`
	Port           *int       `json:"port"`
	Path           *string    `json:"path"`
	URLOverride    *string    `json:"url_override"`
	ExpectStatusLo int        `json:"expect_status_lo"`
	ExpectStatusHi int        `json:"expect_status_hi"`
	IntervalSecs   int        `json:"interval_seconds"`
	TimeoutMs      int        `json:"timeout_ms"`
	Enabled        bool       `json:"enabled"`
	Status         string     `json:"status"`
	LastChangeAt   *time.Time `json:"last_change_at"`
	LastCheckAt    *time.Time `json:"last_check_at"`
	LastDurationMs *float64   `json:"last_duration_ms"`
	LastHTTPStatus *int       `json:"last_http_status"`
	CertExpiresAt  *time.Time `json:"cert_expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const monitorCols = `id, device_id, label, kind::text, port, path, url_override,
	expect_status_lo, expect_status_hi, interval_seconds, timeout_ms, enabled,
	status::text, last_change_at, last_check_at, last_duration_ms, last_http_status,
	cert_expires_at, created_at, updated_at`

func scanMonitor(row pgx.Row) (Monitor, error) {
	var m Monitor
	err := row.Scan(&m.ID, &m.DeviceID, &m.Label, &m.Kind, &m.Port, &m.Path, &m.URLOverride,
		&m.ExpectStatusLo, &m.ExpectStatusHi, &m.IntervalSecs, &m.TimeoutMs, &m.Enabled,
		&m.Status, &m.LastChangeAt, &m.LastCheckAt, &m.LastDurationMs, &m.LastHTTPStatus,
		&m.CertExpiresAt, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Monitor{}, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMonitors(ctx context.Context, deviceID int64) ([]Monitor, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+monitorCols+` FROM device_monitors WHERE device_id = $1 ORDER BY label`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMonitor(ctx context.Context, id int64) (Monitor, error) {
	return scanMonitor(s.pool.QueryRow(ctx, `SELECT `+monitorCols+` FROM device_monitors WHERE id = $1`, id))
}

// CreateMonitorInput is the validated create payload (validation lives in the API).
type CreateMonitorInput struct {
	DeviceID     int64
	Label, Kind  string
	Port         *int
	Path         *string
	URLOverride  *string
	ExpectLo     int
	ExpectHi     int
	IntervalSecs int
	TimeoutMs    int
}

// CreateMonitor inserts a monitor due immediately (next_check_at = now()). A
// duplicate (device_id, label) maps to ErrConflict.
func (s *Store) CreateMonitor(ctx context.Context, in CreateMonitorInput) (Monitor, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO device_monitors
			(device_id, label, kind, port, path, url_override, expect_status_lo, expect_status_hi,
			 interval_seconds, timeout_ms, next_check_at)
		VALUES ($1, $2, $3::monitor_kind, $4, $5, $6, $7, $8, $9, $10, now())
		RETURNING id`,
		in.DeviceID, in.Label, in.Kind, in.Port, in.Path, in.URLOverride,
		in.ExpectLo, in.ExpectHi, in.IntervalSecs, in.TimeoutMs).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "device_monitors_device_id_label_key") {
			return Monitor{}, ErrConflict
		}
		return Monitor{}, err
	}
	return s.GetMonitor(ctx, id)
}

// PatchMonitorInput carries the mutable fields; nil = unchanged.
type PatchMonitorInput struct {
	Label        *string
	Port         *int
	Path         *string
	URLOverride  *string
	ExpectLo     *int
	ExpectHi     *int
	IntervalSecs *int
	TimeoutMs    *int
	Enabled      *bool
}

func (s *Store) UpdateMonitor(ctx context.Context, id int64, p PatchMonitorInput) (Monitor, error) {
	sets, args := []string{}, []any{}
	add := func(expr string, v any) { args = append(args, v); sets = append(sets, fmt.Sprintf(expr, len(args))) }
	if p.Label != nil {
		add("label = $%d", *p.Label)
	}
	if p.Port != nil {
		add("port = $%d", nilIfZeroInt(*p.Port))
	}
	if p.Path != nil {
		add("path = $%d", nilIfEmpty(*p.Path))
	}
	if p.URLOverride != nil {
		add("url_override = $%d", nilIfEmpty(*p.URLOverride))
	}
	if p.ExpectLo != nil {
		add("expect_status_lo = $%d", *p.ExpectLo)
	}
	if p.ExpectHi != nil {
		add("expect_status_hi = $%d", *p.ExpectHi)
	}
	if p.IntervalSecs != nil {
		add("interval_seconds = $%d", *p.IntervalSecs)
	}
	if p.TimeoutMs != nil {
		add("timeout_ms = $%d", *p.TimeoutMs)
	}
	if p.Enabled != nil {
		add("enabled = $%d", *p.Enabled)
	}
	if len(sets) == 0 {
		return s.GetMonitor(ctx, id)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	ct, err := s.pool.Exec(ctx, "UPDATE device_monitors SET "+strings.Join(sets, ", ")+
		fmt.Sprintf(" WHERE id = $%d", len(args)), args...)
	if err != nil {
		if strings.Contains(err.Error(), "device_monitors_device_id_label_key") {
			return Monitor{}, ErrConflict
		}
		return Monitor{}, err
	}
	if ct.RowsAffected() == 0 {
		return Monitor{}, ErrNotFound
	}
	return s.GetMonitor(ctx, id)
}

func (s *Store) DeleteMonitor(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM device_monitors WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- scheduler side (Doc 3 §9) ----

// DueMonitor is the scheduler projection: the check spec plus the device's IP and
// status (so the loop can pause checks while the device itself is DOWN).
type DueMonitor struct {
	ID             int64
	DeviceID       int64
	Label, Kind    string
	Port           *int
	Path           *string
	URLOverride    *string
	ExpectLo       int
	ExpectHi       int
	TimeoutMs      int
	Status         string
	UpStreak       int
	DownStreak     int
	DeviceIP       string
	DeviceStatus   string
}

const dueCols = `m.id, m.device_id, m.label, m.kind::text, m.port, m.path, m.url_override,
	m.expect_status_lo, m.expect_status_hi, m.timeout_ms, m.status::text, m.up_streak, m.down_streak,
	host(d.ip_address), d.status::text`

func scanDue(rows pgx.Rows) (DueMonitor, error) {
	var m DueMonitor
	err := rows.Scan(&m.ID, &m.DeviceID, &m.Label, &m.Kind, &m.Port, &m.Path, &m.URLOverride,
		&m.ExpectLo, &m.ExpectHi, &m.TimeoutMs, &m.Status, &m.UpStreak, &m.DownStreak,
		&m.DeviceIP, &m.DeviceStatus)
	return m, err
}

// ClaimDueMonitors returns every enabled monitor whose cursor is due and bumps
// each cursor forward by its own interval, so the same monitor isn't re-claimed
// next tick. ponytail: single scheduler process, so a plain SELECT-then-UPDATE
// (no FOR UPDATE SKIP LOCKED) is enough; add locking only if the loop ever forks.
func (s *Store) ClaimDueMonitors(ctx context.Context) ([]DueMonitor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+dueCols+`
		FROM device_monitors m JOIN devices d ON d.id = m.device_id
		WHERE m.enabled AND (m.next_check_at IS NULL OR m.next_check_at <= now())`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var due []DueMonitor
	var ids []int64
	for rows.Next() {
		m, err := scanDue(rows)
		if err != nil {
			return nil, err
		}
		due = append(due, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_, err = s.pool.Exec(ctx, `
			UPDATE device_monitors
			SET next_check_at = now() + make_interval(secs => interval_seconds), updated_at = now()
			WHERE id = ANY($1)`, ids)
		if err != nil {
			return nil, err
		}
	}
	return due, nil
}

// MonitorForCheck loads one monitor as a DueMonitor (spec + device IP) for the
// on-demand POST /monitors/{id}/test endpoint. It does NOT touch the cursor.
func (s *Store) MonitorForCheck(ctx context.Context, id int64) (DueMonitor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+dueCols+`
		FROM device_monitors m JOIN devices d ON d.id = m.device_id
		WHERE m.id = $1`, id)
	if err != nil {
		return DueMonitor{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return DueMonitor{}, ErrNotFound
	}
	return scanDue(rows)
}

// SaveMonitorCheck persists one check's outcome + debounce state.
func (s *Store) SaveMonitorCheck(ctx context.Context, id int64, status string, up, down int, changed bool,
	durationMs *float64, httpStatus *int, certExpiresAt *time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE device_monitors
		SET status = $2::monitor_status, up_streak = $3, down_streak = $4,
		    last_check_at = now(),
		    last_change_at = CASE WHEN $5 THEN now() ELSE last_change_at END,
		    last_duration_ms = $6, last_http_status = $7, cert_expires_at = $8, updated_at = now()
		WHERE id = $1`, id, status, up, down, changed, durationMs, httpStatus, certExpiresAt)
	return err
}

func nilIfZeroInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func nilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// MonitorBacklog counts enabled monitors overdue for a check (Pillar 16 worker self-report).
func (s *Store) MonitorBacklog(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM device_monitors WHERE enabled AND next_check_at < now() - interval '30 seconds'`).Scan(&n)
	return n, err
}
