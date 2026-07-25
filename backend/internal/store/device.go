// Package store is the Postgres data-access layer (repository pattern).
// The workers (ping/discovery/sync) reuse this same store in later slices.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"echomap/internal/secrets"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrParentNotFound = errors.New("parent device not found")
	ErrParentCycle    = errors.New("parent_device_id would create a cycle")
)

type Store struct {
	pool    *pgxpool.Pool
	secrets *secrets.Box // seals/opens settings credentials (Doc 2 §1.10)
}

func New(pool *pgxpool.Pool, box *secrets.Box) *Store { return &Store{pool: pool, secrets: box} }

// Device holds the columns the API surfaces (Doc 5). latency_ms / packet_loss
// live in InfluxDB and are joined in from Slice 2 onward.
type Device struct {
	ID             int64      `json:"id"`
	IPAddress      string     `json:"ip_address"`
	Hostname       *string    `json:"hostname"`
	Name           string     `json:"name"`
	NameSource     string     `json:"name_source"`
	Status         string     `json:"status"`
	DownReason     *string    `json:"down_reason"` // "PROBE" | "PARENT" while DOWN (Doc 3 §5)
	IsFlapping     bool       `json:"is_flapping"`
	SourceType     string     `json:"source_type"`
	SubnetID       *int64     `json:"subnet_id"`
	Site           *string    `json:"site"`
	Vendor         *string    `json:"vendor"`
	Model          *string    `json:"model"`
	Icon           string     `json:"icon"`
	IconID         *int64     `json:"icon_id"`
	ParentDeviceID *int64     `json:"parent_device_id"`
	MapID          *int64     `json:"map_id"`
	SNMPEnabled    bool       `json:"snmp_enabled"`
	PosX           *float64   `json:"pos_x"`
	PosY           *float64   `json:"pos_y"`
	IsLocked       bool       `json:"is_locked"`
	LastSeenAt     *time.Time `json:"last_seen_at"`
	LastChangeAt   *time.Time `json:"last_change_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Shared SELECT so List and Get always return the same shape. host() strips the
// INET mask; ::text renders enums as their labels; subnets join supplies site.
const deviceSelect = `
SELECT d.id, host(d.ip_address), d.hostname, d.name, d.name_source::text, d.status::text,
       d.down_reason::text, d.is_flapping,
       d.source_type::text, d.subnet_id, s.site, d.vendor, d.model, d.icon, d.icon_id,
       d.parent_device_id, d.map_id,
       d.snmp_enabled, d.pos_x, d.pos_y, d.is_locked, d.last_seen_at, d.last_change_at,
       d.created_at, d.updated_at
FROM devices d
LEFT JOIN subnets s ON s.id = d.subnet_id`

func scanDevice(row pgx.Row, d *Device) error {
	return row.Scan(&d.ID, &d.IPAddress, &d.Hostname, &d.Name, &d.NameSource, &d.Status,
		&d.DownReason, &d.IsFlapping,
		&d.SourceType, &d.SubnetID, &d.Site, &d.Vendor, &d.Model, &d.Icon, &d.IconID,
		&d.ParentDeviceID, &d.MapID,
		&d.SNMPEnabled, &d.PosX, &d.PosY, &d.IsLocked, &d.LastSeenAt, &d.LastChangeAt,
		&d.CreatedAt, &d.UpdatedAt)
}

type ListFilter struct {
	Status     string
	Site       string
	SourceType string
	Search     string
	Page       int
	PageSize   int
}

// Normalize clamps pagination to sane bounds. Idempotent.
func (f *ListFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize <= 0 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
}

func (s *Store) ListDevices(ctx context.Context, f ListFilter) ([]Device, int, error) {
	f.Normalize()

	where := " WHERE 1=1"
	args := []any{}
	if f.Status != "" {
		args = append(args, f.Status)
		where += fmt.Sprintf(" AND d.status::text = $%d", len(args))
	}
	if f.SourceType != "" {
		args = append(args, f.SourceType)
		where += fmt.Sprintf(" AND d.source_type::text = $%d", len(args))
	}
	if f.Site != "" {
		args = append(args, f.Site)
		where += fmt.Sprintf(" AND s.site = $%d", len(args))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		where += fmt.Sprintf(" AND (d.name ILIKE $%d OR host(d.ip_address) ILIKE $%d)", len(args), len(args))
	}

	var total int
	countSQL := "SELECT count(*) FROM devices d LEFT JOIN subnets s ON s.id = d.subnet_id" + where
	if err := s.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	dataArgs := append(append([]any{}, args...), f.PageSize, (f.Page-1)*f.PageSize)
	dataSQL := deviceSelect + where + fmt.Sprintf(" ORDER BY d.id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := s.pool.Query(ctx, dataSQL, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []Device{}
	for rows.Next() {
		var d Device
		if err := scanDevice(rows, &d); err != nil {
			return nil, 0, err
		}
		items = append(items, d)
	}
	return items, total, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, id int64) (*Device, error) {
	var d Device
	err := scanDevice(s.pool.QueryRow(ctx, deviceSelect+" WHERE d.id = $1", id), &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

type CreateDeviceInput struct {
	IPAddress      string
	Hostname       *string // optional FQDN; API resolves it to IPAddress first (Doc 3 §6)
	Name           string
	NameSource     string  // "MANUAL" or "IP"
	ManualName     *string // set only for a MANUAL name
	SubnetID       *int64
	ParentDeviceID *int64 // existence enforced by FK; a new device has no descendants, so no cycle is possible
	MapID          *int64
	IconID         *int64
	SNMPEnabled    bool
}

func (s *Store) CreateDevice(ctx context.Context, in CreateDeviceInput) (*Device, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO devices (ip_address, hostname, name, name_source, manual_name, source_type, status,
		                     subnet_id, parent_device_id, map_id, icon_id, snmp_enabled)
		VALUES ($1::inet, $2, $3, $4::name_source, $5, 'MANUAL', 'UNKNOWN', $6, $7, $8, $9, $10)
		RETURNING id`,
		in.IPAddress, in.Hostname, in.Name, in.NameSource, in.ManualName,
		in.SubnetID, in.ParentDeviceID, in.MapID, in.IconID, in.SNMPEnabled,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetDevice(ctx, id)
}

// PatchDeviceInput uses nil = "leave unchanged". The nullable references
// (ParentDeviceID/MapID/IconID) treat 0 as "clear", and Hostname treats "" as
// "clear" — JSON null is indistinguishable from an absent key with plain pointers.
type PatchDeviceInput struct {
	Name           *string
	Hostname       *string
	ParentDeviceID *int64
	MapID          *int64
	IconID         *int64
	SNMPEnabled    *bool
}

func (s *Store) PatchDevice(ctx context.Context, id int64, in PatchDeviceInput) (*Device, error) {
	sets := []string{}
	args := []any{}
	if in.Name != nil {
		args = append(args, *in.Name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
		args = append(args, *in.Name)
		sets = append(sets, fmt.Sprintf("manual_name = $%d", len(args)))
		sets = append(sets, "name_source = 'MANUAL'")
	}
	if in.Hostname != nil {
		args = append(args, nilIfZero(*in.Hostname))
		sets = append(sets, fmt.Sprintf("hostname = $%d", len(args)))
	}
	if in.ParentDeviceID != nil {
		args = append(args, nilIfZero(*in.ParentDeviceID))
		sets = append(sets, fmt.Sprintf("parent_device_id = $%d", len(args)))
		// A cascade victim being re-parented or cleared must re-enter the probe
		// sweep (ListForMonitoring skips DOWN+PARENT rows): its DOWN-by-parent
		// state is no longer justified by the new topology. Reset to UNKNOWN;
		// its next confirmed transition re-cascades its own subtree correctly.
		sets = append(sets, "status = CASE WHEN status = 'DOWN' AND down_reason = 'PARENT' THEN 'UNKNOWN'::device_status ELSE status END")
		sets = append(sets, "down_reason = CASE WHEN down_reason = 'PARENT' THEN NULL ELSE down_reason END")
	}
	if in.MapID != nil {
		args = append(args, nilIfZero(*in.MapID))
		sets = append(sets, fmt.Sprintf("map_id = $%d", len(args)))
	}
	if in.IconID != nil {
		args = append(args, nilIfZero(*in.IconID))
		sets = append(sets, fmt.Sprintf("icon_id = $%d", len(args)))
	}
	if in.SNMPEnabled != nil {
		args = append(args, *in.SNMPEnabled)
		sets = append(sets, fmt.Sprintf("snmp_enabled = $%d", len(args)))
	}
	if len(sets) == 0 {
		return s.GetDevice(ctx, id) // nothing to change
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	sql := "UPDATE devices SET " + strings.Join(sets, ", ") + fmt.Sprintf(" WHERE id = $%d", len(args))

	// The cycle check and the UPDATE must see the same graph, so both run in one
	// transaction. ponytail: READ COMMITTED still allows two *concurrent* PATCHes
	// to weave a cycle past each other; re-parenting is a rare admin action and
	// the cascade CTEs use UNION (cycle-tolerant), so full serialization waits
	// until it ever matters.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if in.ParentDeviceID != nil && *in.ParentDeviceID != 0 {
		if err := checkParent(ctx, tx, id, *in.ParentDeviceID); err != nil {
			return nil, err
		}
	}

	ct, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetDevice(ctx, id)
}

// checkParent validates a proposed parent for device id: the parent must exist,
// and walking UP the ancestor chain from the parent must never reach the device
// itself (that would close a loop, Doc 3 §5). Walk-up beats walk-down: the path
// to the root is short even when the device's subtree is huge.
func checkParent(ctx context.Context, tx pgx.Tx, id, parentID int64) error {
	if parentID == id {
		return ErrParentCycle
	}
	var parentExists, cycle bool
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_device_id FROM devices WHERE id = $1
			UNION
			SELECT d.id, d.parent_device_id
			FROM devices d JOIN up u ON d.id = u.parent_device_id
		)
		SELECT EXISTS (SELECT 1 FROM devices WHERE id = $1),
		       EXISTS (SELECT 1 FROM up WHERE id = $2)`,
		parentID, id).Scan(&parentExists, &cycle)
	if err != nil {
		return err
	}
	if !parentExists {
		return ErrParentNotFound
	}
	if cycle {
		return ErrParentCycle
	}
	return nil
}

// nilIfZero maps the "clear this reference" sentinel (0 / "") to SQL NULL.
func nilIfZero[T comparable](v T) any {
	var zero T
	if v == zero {
		return nil
	}
	return v
}

// DeleteDevice hard-deletes (cascades to interfaces/edges/alerts).
// ponytail: archive-then-delete is Slice 7; a plain DELETE is enough now.
func (s *Store) DeleteDevice(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Un-strand cascade victims BEFORE the FK sets their parent to NULL: children
	// DOWN because of this device would otherwise stay DOWN+PARENT forever
	// (ListForMonitoring excludes them, and no parent remains to release them).
	// Reset to UNKNOWN; their own next confirmed transition handles their subtrees.
	if _, err := tx.Exec(ctx, `
		UPDATE devices SET status = 'UNKNOWN', down_reason = NULL, updated_at = now()
		WHERE parent_device_id = $1 AND status = 'DOWN' AND down_reason = 'PARENT'`, id); err != nil {
		return err
	}

	ct, err := tx.Exec(ctx, "DELETE FROM devices WHERE id = $1", id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}
