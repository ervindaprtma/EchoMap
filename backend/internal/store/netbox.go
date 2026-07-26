package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"echomap/internal/netbox"
)

// --- Netbox settings (settings row) -------------------------------------------

type NetboxSettings struct {
	URL         string
	TokenSet    bool
	SyncEnabled bool
}

func (s *Store) GetNetboxSettings(ctx context.Context) (NetboxSettings, error) {
	var url, token *string
	var enabled bool
	err := s.pool.QueryRow(ctx,
		`SELECT netbox_url, netbox_token, netbox_sync_enabled FROM settings WHERE id = 1`).
		Scan(&url, &token, &enabled)
	if err != nil {
		return NetboxSettings{}, err
	}
	out := NetboxSettings{SyncEnabled: enabled}
	if url != nil {
		out.URL = *url
	}
	out.TokenSet = token != nil && *token != ""
	return out, nil
}

type NetboxUpdate struct {
	URL         *string
	Token       *string // nil = unchanged, "" = clear, else new plaintext (sealed)
	SyncEnabled *bool
}

func (s *Store) UpdateNetboxSettings(ctx context.Context, u NetboxUpdate) error {
	sets, args := []string{}, []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if u.URL != nil {
		add("netbox_url", nilIfZero(strings.TrimSpace(*u.URL)))
	}
	if u.Token != nil {
		val, err := s.sealOrNil(*u.Token)
		if err != nil {
			return err
		}
		add("netbox_token", val)
	}
	if u.SyncEnabled != nil {
		add("netbox_sync_enabled", *u.SyncEnabled)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	_, err := s.pool.Exec(ctx, "UPDATE settings SET "+strings.Join(sets, ", ")+" WHERE id = 1", args...)
	return err
}

// NetboxCredentials returns the decrypted URL + token (empty when unconfigured).
func (s *Store) NetboxCredentials(ctx context.Context) (url, token string, enabled bool, err error) {
	var u, t *string
	err = s.pool.QueryRow(ctx,
		`SELECT netbox_url, netbox_token, netbox_sync_enabled FROM settings WHERE id = 1`).
		Scan(&u, &t, &enabled)
	if err != nil {
		return "", "", false, err
	}
	if u != nil {
		url = *u
	}
	if t != nil && *t != "" {
		token, err = s.secrets.Decrypt(*t)
		if err != nil {
			return "", "", false, err
		}
	}
	return url, token, enabled, nil
}

// --- sync diff primitives -----------------------------------------------------

// NetboxDev is a lightweight view of a NETBOX-sourced device for the sync diff.
type NetboxDev struct {
	ID         int64
	Status     string
	NameSource string
}

// ListNetboxDevices returns the NETBOX-sourced devices keyed by IP (Doc 3 §3).
func (s *Store) ListNetboxDevices(ctx context.Context) (map[string]*NetboxDev, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, host(ip_address), status::text, name_source::text
		 FROM devices WHERE source_type = 'NETBOX'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*NetboxDev{}
	for rows.Next() {
		var ip string
		d := &NetboxDev{}
		if err := rows.Scan(&d.ID, &ip, &d.Status, &d.NameSource); err != nil {
			return nil, err
		}
		out[ip] = d
	}
	return out, rows.Err()
}

// InsertNetboxDevice creates a NETBOX device. ON CONFLICT DO NOTHING so it never
// clobbers a manually-added device that happens to share the IP; returns whether
// a row was inserted.
func (s *Store) InsertNetboxDevice(ctx context.Context, o netbox.IPObject) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO devices (ip_address, name, name_source, source_type, netbox_id, status)
		VALUES ($1::inet, $2, 'NETBOX', 'NETBOX', $3, 'UNKNOWN')
		ON CONFLICT (ip_address) DO NOTHING`,
		o.IP, o.Name, o.NetboxID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RefreshNetboxDevice updates Netbox-owned fields on an existing NETBOX device,
// preserving a MANUAL name override (Doc 3 §3 naming rule).
func (s *Store) RefreshNetboxDevice(ctx context.Context, id int64, o netbox.IPObject) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE devices SET
			netbox_id = $2,
			name = CASE WHEN name_source = 'MANUAL' THEN name ELSE $3 END,
			updated_at = now()
		WHERE id = $1`, id, o.NetboxID, o.Name)
	return err
}

// StatusChange is the info needed to publish a device.status WS frame.
type StatusChange struct {
	IP   string
	Name string
	From string
}

// SetDeviceStatus flips a device's status and returns the prior status + identity
// so the caller can publish the transition. Used by the sync worker for
// ORPHANED (and back to UNKNOWN on restore).
func (s *Store) SetDeviceStatus(ctx context.Context, id int64, status string) (StatusChange, error) {
	var c StatusChange
	err := s.pool.QueryRow(ctx, `
		UPDATE devices SET previous_status = status, status = $2::device_status, last_change_at = now()
		WHERE id = $1
		RETURNING host(ip_address), name, previous_status::text`,
		id, status).Scan(&c.IP, &c.Name, &c.From)
	return c, err
}

// DetachNetbox converts a device to MANUAL, drops its netbox_id, and lifts it out
// of ORPHANED so it re-enters normal monitoring (Doc 3 §3 resolution).
func (s *Store) DetachNetbox(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE devices SET
			source_type = 'MANUAL',
			netbox_id = NULL,
			status = CASE WHEN status = 'ORPHANED' THEN 'UNKNOWN'::device_status ELSE status END,
			updated_at = now()
		WHERE id = $1`, id)
	return err
}

// --- sync logs ----------------------------------------------------------------

type SyncLog struct {
	ID          int64     `json:"id"`
	RunAt       time.Time `json:"run_at"`
	Status      string    `json:"status"`
	IPsAdded    int       `json:"ips_added"`
	IPsUpdated  int       `json:"ips_updated"`
	IPsOrphaned int       `json:"ips_orphaned"`
	IPsRestored int       `json:"ips_restored"`
	DurationMs  *int      `json:"duration_ms"`
	Error       *string   `json:"error"`
}

func (s *Store) SaveSyncLog(ctx context.Context, l SyncLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO netbox_sync_logs (run_at, status, ips_added, ips_updated, ips_orphaned, ips_restored, duration_ms, error)
		VALUES ($1, $2::sync_status, $3, $4, $5, $6, $7, $8)`,
		l.RunAt, l.Status, l.IPsAdded, l.IPsUpdated, l.IPsOrphaned, l.IPsRestored, l.DurationMs, l.Error)
	return err
}

func (s *Store) ListSyncLogs(ctx context.Context, limit int) ([]SyncLog, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_at, status::text, ips_added, ips_updated, ips_orphaned, ips_restored, duration_ms, error
		FROM netbox_sync_logs ORDER BY run_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SyncLog{}
	for rows.Next() {
		var l SyncLog
		if err := rows.Scan(&l.ID, &l.RunAt, &l.Status, &l.IPsAdded, &l.IPsUpdated,
			&l.IPsOrphaned, &l.IPsRestored, &l.DurationMs, &l.Error); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
