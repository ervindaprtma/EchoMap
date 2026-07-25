package store

import "context"

// HostDevice is the resolver loop's projection: devices identified by a domain
// name rather than a typed IP (Doc 3 §6).
type HostDevice struct {
	ID        int64
	Hostname  string
	IPAddress string
	Name      string
}

func (s *Store) ListWithHostname(ctx context.Context) ([]HostDevice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, hostname, host(ip_address), name
		FROM devices
		WHERE hostname IS NOT NULL AND hostname <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []HostDevice{}
	for rows.Next() {
		var d HostDevice
		if err := rows.Scan(&d.ID, &d.Hostname, &d.IPAddress, &d.Name); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateDeviceIP repoints a device at a freshly resolved address. Reports false
// when another device already monitors that IP (ip_address is UNIQUE): the guard
// lives inside the statement so a concurrent insert can't slip between a check
// and the write, and the caller keeps the last-known IP instead of erroring.
func (s *Store) UpdateDeviceIP(ctx context.Context, id int64, ip string) (bool, error) {
	ct, err := s.pool.Exec(ctx, `
		UPDATE devices SET ip_address = $2::inet, updated_at = now()
		WHERE id = $1
		  AND NOT EXISTS (SELECT 1 FROM devices WHERE ip_address = $2::inet AND id <> $1)`, id, ip)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// DNSRecheckSeconds reads the resolver cadence from the settings singleton.
func (s *Store) DNSRecheckSeconds(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT dns_recheck_seconds FROM settings WHERE id = 1`).Scan(&n)
	return n, err
}
