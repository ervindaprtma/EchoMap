package store

import (
	"context"
	"fmt"
	"strings"

	"echomap/internal/snmp"
)

// --- SNMP defaults (settings row) ---------------------------------------------

// SNMPSettings is the API-safe view: reports whether a community exists, never it.
type SNMPSettings struct {
	DefaultVersion string
	CommunitySet   bool
}

func (s *Store) GetSNMPSettings(ctx context.Context) (SNMPSettings, error) {
	var version, community *string
	err := s.pool.QueryRow(ctx,
		`SELECT snmp_default_version, snmp_default_community FROM settings WHERE id = 1`).
		Scan(&version, &community)
	if err != nil {
		return SNMPSettings{}, err
	}
	out := SNMPSettings{DefaultVersion: "v2c"}
	if version != nil && *version != "" {
		out.DefaultVersion = *version
	}
	out.CommunitySet = community != nil && *community != ""
	return out, nil
}

// SNMPUpdate is a write-only update; a nil field is left unchanged. Community: ""
// clears, otherwise the new plaintext (sealed before it lands).
type SNMPUpdate struct {
	DefaultVersion *string
	Community      *string
}

func (s *Store) UpdateSNMPSettings(ctx context.Context, u SNMPUpdate) error {
	sets, args := []string{}, []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if u.DefaultVersion != nil {
		add("snmp_default_version", nilIfZero(strings.TrimSpace(*u.DefaultVersion)))
	}
	if u.Community != nil {
		val, err := s.sealOrNil(*u.Community)
		if err != nil {
			return err
		}
		add("snmp_default_community", val)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = now()")
	_, err := s.pool.Exec(ctx, "UPDATE settings SET "+strings.Join(sets, ", ")+" WHERE id = 1", args...)
	return err
}

// SNMPCredentials returns the decrypted default community + version for the
// scanner. Empty community ⇒ SNMP is unconfigured (the scanner no-ops).
func (s *Store) SNMPCredentials(ctx context.Context) (version, community string, err error) {
	var ver, comm *string
	err = s.pool.QueryRow(ctx,
		`SELECT snmp_default_version, snmp_default_community FROM settings WHERE id = 1`).
		Scan(&ver, &comm)
	if err != nil {
		return "", "", err
	}
	version = "v2c"
	if ver != nil && *ver != "" {
		version = *ver
	}
	if comm != nil && *comm != "" {
		community, err = s.secrets.Decrypt(*comm)
		if err != nil {
			return "", "", err
		}
	}
	return version, community, nil
}

// --- fingerprint persistence --------------------------------------------------

// FingerprintTarget is a device awaiting its first SNMP fingerprint.
type FingerprintTarget struct {
	ID int64
	IP string
}

// DevicesNeedingFingerprint returns SNMP-enabled devices that have never been
// fingerprinted (sys_descr IS NULL). Re-fingerprinting is on-demand via the API.
func (s *Store) DevicesNeedingFingerprint(ctx context.Context, limit int) ([]FingerprintTarget, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, host(ip_address) FROM devices
		WHERE snmp_enabled = true AND sys_descr IS NULL
		ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FingerprintTarget
	for rows.Next() {
		var t FingerprintTarget
		if err := rows.Scan(&t.ID, &t.IP); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveFingerprint persists identity + interfaces in one transaction and applies the
// naming fallback: an SNMP sysName wins only over an IP-derived name — never over a
// MANUAL or NETBOX name (Doc 2 §1: NETBOX → SNMP_SYSNAME → MANUAL → IP).
func (s *Store) SaveFingerprint(ctx context.Context, deviceID int64, r *snmp.Result) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE devices SET
			sys_descr     = $2,
			sys_object_id = NULLIF($3, ''),
			sys_name      = NULLIF($4, ''),
			vendor        = NULLIF($5, ''),
			model         = NULLIF($6, ''),
			icon          = CASE WHEN icon_id IS NULL AND $7 <> '' THEN $7 ELSE icon END,
			name          = CASE WHEN name_source = 'IP' AND $4 <> '' THEN $4 ELSE name END,
			name_source   = CASE WHEN name_source = 'IP' AND $4 <> '' THEN 'SNMP_SYSNAME'::name_source ELSE name_source END,
			updated_at    = now()
		WHERE id = $1`,
		deviceID, r.SysDescr, r.SysObjectID, r.SysName, r.Vendor, r.Model, r.Icon)
	if err != nil {
		return err
	}

	// Whole-object refresh: drop then re-insert the device's interfaces.
	if _, err = tx.Exec(ctx, `DELETE FROM interfaces WHERE device_id = $1`, deviceID); err != nil {
		return err
	}
	for _, iface := range r.Interfaces {
		if _, err = tx.Exec(ctx, `
			INSERT INTO interfaces (device_id, if_index, if_name, mac_address)
			VALUES ($1, $2, NULLIF($3, ''), $4::macaddr)`,
			deviceID, iface.Index, iface.Name, nilIfZero(iface.MAC)); err != nil {
			// A malformed MAC from odd gear shouldn't abort the whole fingerprint.
			return err
		}
	}
	return tx.Commit(ctx)
}

// InterfaceRow is an API-facing interface record.
type InterfaceRow struct {
	IfIndex   *int    `json:"if_index"`
	IfName    *string `json:"if_name"`
	MAC       *string `json:"mac_address"`
	IPAddress *string `json:"ip_address"`
}

func (s *Store) ListInterfaces(ctx context.Context, deviceID int64) ([]InterfaceRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT if_index, if_name, mac_address::text, host(ip_address)
		FROM interfaces WHERE device_id = $1 ORDER BY if_index`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InterfaceRow{}
	for rows.Next() {
		var r InterfaceRow
		if err := rows.Scan(&r.IfIndex, &r.IfName, &r.MAC, &r.IPAddress); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
