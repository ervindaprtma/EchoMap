package store

import "context"

// DeviceServiceRow is a right-click launcher (Doc 2 §1.15). No credential
// columns exist — by design.
type DeviceServiceRow struct {
	ID       int64   `json:"id"`
	DeviceID int64   `json:"device_id"`
	Label    string  `json:"label"`
	Kind     string  `json:"kind"` // HTTP | HTTPS | SSH | TELNET | CUSTOM_URL
	Port     *int    `json:"port"`
	Path     *string `json:"path"`
	URL      *string `json:"url"`
}

func (s *Store) ListServices(ctx context.Context, deviceID int64) ([]DeviceServiceRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, device_id, label, kind::text, port, path, url
		FROM device_services WHERE device_id = $1 ORDER BY id`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DeviceServiceRow{}
	for rows.Next() {
		var r DeviceServiceRow
		if err := rows.Scan(&r.ID, &r.DeviceID, &r.Label, &r.Kind, &r.Port, &r.Path, &r.URL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateService(ctx context.Context, in DeviceServiceRow) (*DeviceServiceRow, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO device_services (device_id, label, kind, port, path, url)
		VALUES ($1, $2, $3::service_kind, $4, $5, $6)
		RETURNING id`,
		in.DeviceID, in.Label, in.Kind, in.Port, in.Path, in.URL).Scan(&in.ID)
	if err != nil {
		return nil, err
	}
	return &in, nil
}

// ponytail: no UPDATE — delete + re-add covers editing a 5-field row; add PATCH
// if service lists ever grow beyond a handful per device.
func (s *Store) DeleteService(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM device_services WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
