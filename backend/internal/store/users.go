package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Session lifetime (Doc 2 §4.2 / PRD §4.6): 12 h idle, 7 d absolute.
const (
	SessionIdleTimeout = 12 * time.Hour
	SessionMaxLifetime = 7 * 24 * time.Hour
	sessionTouchEvery  = time.Minute // throttle last_seen_at writes
)

var (
	ErrLastSuperadmin = errors.New("cannot disable or remove the last enabled Superadmin")
	ErrConflict       = errors.New("conflict")
)

// User is the account + RBAC row. PasswordHash never leaves the store toward the API.
type User struct {
	ID                 int64     `json:"id"`
	Username           string    `json:"username"`
	PasswordHash       string    `json:"-"`
	Role               string    `json:"role"`
	Enabled            bool      `json:"enabled"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	LastLoginAt        *time.Time `json:"last_login_at"`
}

// ---- users ----

// EnsureBootstrapSuperadmin seeds a forced-password-change Superadmin when the
// users table is empty. Returns true if it created one. No-op once any user exists,
// so it is safe to call on every api boot.
func (s *Store) EnsureBootstrapSuperadmin(ctx context.Context, username, passwordHash string) (bool, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (username, password_hash, role, must_change_password)
		 VALUES ($1, $2, 'SUPERADMIN', true)`, username, passwordHash)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, userSelect+` WHERE u.username = $1`, username))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, userSelect+` WHERE u.id = $1`, id))
}

const userSelect = `
	SELECT u.id, u.username, u.password_hash, u.role, u.enabled, u.must_change_password, u.created_at,
	       (SELECT max(created_at) FROM sessions WHERE user_id = u.id)
	FROM users u`

func (s *Store) scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Enabled,
		&u.MustChangePassword, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, userSelect+` ORDER BY u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := s.scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateUser inserts an account. A duplicate username maps to ErrConflict.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash, role string) (User, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3) RETURNING id`,
		username, passwordHash, role).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "users_username_key") {
			return User{}, ErrConflict
		}
		return User{}, err
	}
	return s.UserByID(ctx, id)
}

// UpdateUser applies the non-nil fields. Guards the "last enabled Superadmin"
// invariant for both role demotion and disabling, inside one transaction so a
// concurrent second demotion can't slip past the count.
func (s *Store) UpdateUser(ctx context.Context, id int64, role *string, enabled *bool, passwordHash *string, clearMustChange bool) (User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)

	var cur User
	if cur, err = s.scanUser(tx.QueryRow(ctx, userSelect+` WHERE u.id = $1 FOR UPDATE OF u`, id)); err != nil {
		return User{}, err
	}

	demoting := role != nil && *role != "SUPERADMIN" && cur.Role == "SUPERADMIN"
	disabling := enabled != nil && !*enabled && cur.Enabled
	if (demoting || disabling) && cur.Role == "SUPERADMIN" {
		var others int
		if err = tx.QueryRow(ctx,
			`SELECT count(*) FROM users WHERE role = 'SUPERADMIN' AND enabled AND id <> $1`, id).Scan(&others); err != nil {
			return User{}, err
		}
		if others == 0 {
			return User{}, ErrLastSuperadmin
		}
	}

	sets, args := []string{}, []any{}
	add := func(expr string, v any) { args = append(args, v); sets = append(sets, fmt.Sprintf(expr, len(args))) }
	if role != nil {
		add("role = $%d", *role)
	}
	if enabled != nil {
		add("enabled = $%d", *enabled)
	}
	if passwordHash != nil {
		add("password_hash = $%d", *passwordHash)
	}
	if clearMustChange {
		sets = append(sets, "must_change_password = false")
	} else if passwordHash != nil {
		// An admin-set password forces the user to change it on next login.
		sets = append(sets, "must_change_password = true")
	}
	if len(sets) == 0 {
		return cur, nil
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	if _, err = tx.Exec(ctx, "UPDATE users SET "+strings.Join(sets, ", ")+fmt.Sprintf(" WHERE id = $%d", len(args)), args...); err != nil {
		return User{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return User{}, err
	}
	return s.UserByID(ctx, id)
}

// DeleteUser removes an account, refusing to delete the last enabled Superadmin.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var role string
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT role, enabled FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&role, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == "SUPERADMIN" && enabled {
		var others int
		if err = tx.QueryRow(ctx,
			`SELECT count(*) FROM users WHERE role = 'SUPERADMIN' AND enabled AND id <> $1`, id).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return ErrLastSuperadmin
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- sessions ----

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// CreateSession stores only the SHA-256 hash of the opaque token.
func (s *Store) CreateSession(ctx context.Context, rawToken string, userID int64, ip, ua string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, ip, user_agent)
		 VALUES ($1, $2, $3, $4)`, hashToken(rawToken), userID, nilIfZero(ip), nilIfZero(ua))
	return err
}

// Session is a row for the "my/all sessions" tables.
type Session struct {
	ID         int64     `json:"id"`
	UserID     int64     `json:"user_id"`
	Username   string    `json:"username"`
	IP         *string   `json:"ip"`
	UserAgent  *string   `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Current    bool      `json:"current"`
}

// SessionUser resolves a raw cookie token to its user, enforcing enabled + idle +
// absolute-lifetime expiry. An expired/invalid token deletes any matching row and
// returns ErrNotFound. It slides the idle window (throttled) and returns the
// session id so callers can flag the "current" row. Used by both the REST
// middleware and the WS upgrade.
func (s *Store) SessionUser(ctx context.Context, rawToken string) (User, int64, error) {
	var (
		sessID              int64
		created, lastSeen   time.Time
		u                   User
	)
	err := s.pool.QueryRow(ctx, `
		SELECT s.id, s.created_at, s.last_seen_at,
		       u.id, u.username, u.password_hash, u.role, u.enabled, u.must_change_password, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1`, hashToken(rawToken)).
		Scan(&sessID, &created, &lastSeen, &u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.Enabled, &u.MustChangePassword, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, 0, ErrNotFound
	}
	if err != nil {
		return User{}, 0, err
	}

	now := time.Now()
	expired := !u.Enabled ||
		now.Sub(lastSeen) > SessionIdleTimeout ||
		now.Sub(created) > SessionMaxLifetime
	if expired {
		_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessID)
		return User{}, 0, ErrNotFound
	}
	if now.Sub(lastSeen) > sessionTouchEvery {
		_, _ = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, sessID)
	}
	return u, sessID, nil
}

func (s *Store) ListSessions(ctx context.Context, userID *int64, currentID int64) ([]Session, error) {
	q := `SELECT s.id, s.user_id, u.username, host(s.ip), s.user_agent, s.created_at, s.last_seen_at
	      FROM sessions s JOIN users u ON u.id = s.user_id`
	args := []any{}
	if userID != nil {
		args = append(args, *userID)
		q += ` WHERE s.user_id = $1`
	}
	q += ` ORDER BY s.last_seen_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var se Session
		if err := rows.Scan(&se.ID, &se.UserID, &se.Username, &se.IP, &se.UserAgent, &se.CreatedAt, &se.LastSeenAt); err != nil {
			return nil, err
		}
		se.Current = se.ID == currentID
		out = append(out, se)
	}
	return out, rows.Err()
}

// DeleteSession revokes a session. If ownerID is non-nil the delete is scoped to
// that user (so an operator can only revoke their own); nil = unscoped (Superadmin).
// Returns ErrNotFound when nothing matched (wrong id or not the caller's session).
func (s *Store) DeleteSession(ctx context.Context, id int64, ownerID *int64) error {
	q := `DELETE FROM sessions WHERE id = $1`
	args := []any{id}
	if ownerID != nil {
		q += ` AND user_id = $2`
		args = append(args, *ownerID)
	}
	ct, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUserSessions revokes every session of a user ("sign out everywhere" and
// forced-logout after a password change). Returns the number revoked.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64, except int64) (int64, error) {
	ct, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, userID, except)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

// ---- event logs ----

// InsertEventLog appends one row (Doc 3 §10). Callers pass nil device/user ids and
// a nil meta when not applicable.
func (s *Store) InsertEventLog(ctx context.Context, level, category, message string, deviceID, userID *int64, meta map[string]any) error {
	var metaJSON []byte
	if meta != nil {
		metaJSON, _ = json.Marshal(meta)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO event_logs (level, category, message, device_id, user_id, meta)
		 VALUES ($1, $2, $3, $4, $5, $6)`, level, category, message, deviceID, userID, metaJSON)
	return err
}

// Info satisfies monitor.EventLogger: fire-and-forget INFO logging from the worker's
// hot path, so a logging hiccup never fails a status transition (Doc 3 §10).
func (s *Store) Info(ctx context.Context, category, message string, deviceID *int64) {
	if err := s.InsertEventLog(ctx, "INFO", category, message, deviceID, nil, nil); err != nil {
		// degrade to the process log; never propagate
		fmt.Printf("eventlog: %v\n", err)
	}
}

// EventLog is one row returned to the Logs page.
type EventLog struct {
	ID       int64          `json:"id"`
	TS       time.Time      `json:"ts"`
	Level    string         `json:"level"`
	Category string         `json:"category"`
	Message  string         `json:"message"`
	DeviceID *int64         `json:"device_id"`
	UserID   *int64         `json:"user_id"`
	Meta     map[string]any `json:"meta"`
}

// LogFilter drives GET /logs server-side filtering + pagination.
type LogFilter struct {
	Level        string
	Category     string
	DeviceID     *int64
	UserID       *int64
	Search       string
	IncludeAudit bool // false hides AUDIT rows regardless of the level filter
	Page         int
	PageSize     int
}

// ListEventLogs returns a page of logs (newest first) and the total match count.
func (s *Store) ListEventLogs(ctx context.Context, f LogFilter) ([]EventLog, int, error) {
	where, args := []string{"true"}, []any{}
	add := func(expr string, v any) { args = append(args, v); where = append(where, fmt.Sprintf(expr, len(args))) }

	if !f.IncludeAudit {
		where = append(where, "level <> 'AUDIT'")
	}
	if f.Level != "" {
		add("level = $%d", f.Level)
	}
	if f.Category != "" {
		add("category = $%d", f.Category)
	}
	if f.DeviceID != nil {
		add("device_id = $%d", *f.DeviceID)
	}
	if f.UserID != nil {
		add("user_id = $%d", *f.UserID)
	}
	if f.Search != "" {
		add("message ILIKE '%%' || $%d || '%%'", f.Search)
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM event_logs WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if f.PageSize <= 0 {
		f.PageSize = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	q := fmt.Sprintf(`SELECT id, ts, level, category, message, device_id, user_id, meta
		FROM event_logs WHERE %s ORDER BY ts DESC, id DESC LIMIT $%d OFFSET $%d`,
		cond, len(args)-1, len(args))
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []EventLog{}
	for rows.Next() {
		var e EventLog
		var meta []byte
		if err := rows.Scan(&e.ID, &e.TS, &e.Level, &e.Category, &e.Message, &e.DeviceID, &e.UserID, &meta); err != nil {
			return nil, 0, err
		}
		if len(meta) > 0 {
			_ = json.Unmarshal(meta, &e.Meta)
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
