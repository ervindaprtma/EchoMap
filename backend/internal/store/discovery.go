package store

import "context"

// UpsertSubnet returns the id of the subnet row for cidr, creating it if absent.
// A discovery sweep groups the devices it finds under this subnet so they inherit
// its site (a device's site is read via its subnet — Doc 2 §1).
func (s *Store) UpsertSubnet(ctx context.Context, cidr, site string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO subnets (cidr, site) VALUES ($1::cidr, $2)
		ON CONFLICT (cidr) DO UPDATE SET site = COALESCE(EXCLUDED.site, subnets.site)
		RETURNING id`,
		cidr, nilIfZero(site),
	).Scan(&id)
	return id, err
}

// UpsertDiscoveredDevice inserts an alive discovered IP as an UP device (Doc 3 §1).
// Returns created=false when the IP is already monitored (ON CONFLICT DO NOTHING),
// so the sweep can report it without creating a duplicate row.
func (s *Store) UpsertDiscoveredDevice(ctx context.Context, ip string, subnetID *int64, snmp bool) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO devices (ip_address, name, name_source, source_type, status, subnet_id, snmp_enabled, last_seen_at)
		VALUES ($1::inet, $1, 'IP', 'DISCOVERY', 'UP', $2, $3, now())
		ON CONFLICT (ip_address) DO NOTHING`,
		ip, subnetID, snmp,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
