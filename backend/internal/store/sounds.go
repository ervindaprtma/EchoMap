package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SoundRow is alert-sound metadata (no bytes). The audio itself is fetched
// separately via GetSoundAudio — a .wav can be up to 1 MB, too big to inline in
// a list the way icons do.
type SoundRow struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (s *Store) ListSounds(ctx context.Context) ([]SoundRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM alert_sounds ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SoundRow{}
	for rows.Next() {
		var r SoundRow
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetSoundAudio(ctx context.Context, id int64) ([]byte, error) {
	var data []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM alert_sounds WHERE id = $1`, id).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}

// CreateSound stores a .wav. A duplicate name maps to ErrConflict.
func (s *Store) CreateSound(ctx context.Context, name string, data []byte) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO alert_sounds (name, data) VALUES ($1, $2) RETURNING id`, name, data).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "alert_sounds_name_key") {
			return 0, ErrConflict
		}
		return 0, err
	}
	return id, nil
}

func (s *Store) DeleteSound(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM alert_sounds WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SoundAssignments maps each alert event class to a sound id (nil = built-in
// chime). Deleting a sound sets the referencing columns NULL (FK ON DELETE SET
// NULL), so an assignment is never dangling.
type SoundAssignments struct {
	DeviceDownID  *int64 `json:"device_down_id"`
	DeviceUpID    *int64 `json:"device_up_id"`
	MonitorDownID *int64 `json:"monitor_down_id"`
	MonitorUpID   *int64 `json:"monitor_up_id"`
}

func (s *Store) GetSoundAssignments(ctx context.Context) (SoundAssignments, error) {
	var a SoundAssignments
	err := s.pool.QueryRow(ctx, `
		SELECT sound_device_down_id, sound_device_up_id, sound_monitor_down_id, sound_monitor_up_id
		FROM settings WHERE id = 1`).
		Scan(&a.DeviceDownID, &a.DeviceUpID, &a.MonitorDownID, &a.MonitorUpID)
	return a, err
}

// UpdateSoundAssignments is a whole-object write (four nullable ids). A
// nonexistent id trips the FK; callers map that to a 422.
func (s *Store) UpdateSoundAssignments(ctx context.Context, a SoundAssignments) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE settings SET
			sound_device_down_id  = $1,
			sound_device_up_id    = $2,
			sound_monitor_down_id = $3,
			sound_monitor_up_id   = $4,
			updated_at = now()
		WHERE id = 1`, a.DeviceDownID, a.DeviceUpID, a.MonitorDownID, a.MonitorUpID)
	return err
}
