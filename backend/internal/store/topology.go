package store

import "context"

type EdgeRow struct {
	ID             int64   `json:"id"`
	SourceDeviceID int64   `json:"source_device_id"`
	TargetDeviceID int64   `json:"target_device_id"`
	LinkType       string  `json:"link_type"` // AUTO | MANUAL
	Label          *string `json:"label"`
}

// SubmapSummary powers the folder-style submap node on a parent canvas
// (Doc 4 §4.6): counts aggregate over the child map's WHOLE subtree, so a
// problem three levels down still turns the top-level node red.
type SubmapSummary struct {
	MapID    int64    `json:"map_id"`
	Name     string   `json:"name"`
	PosX     *float64 `json:"pos_x"`
	PosY     *float64 `json:"pos_y"`
	Up       int      `json:"up"`
	Down     int      `json:"down"`
	Orphaned int      `json:"orphaned"`
	Unknown  int      `json:"unknown"`
}

// mapID nil = the root map throughout (devices.map_id IS NULL).

func (s *Store) ListDevicesByMap(ctx context.Context, mapID *int64) ([]Device, error) {
	rows, err := s.pool.Query(ctx,
		deviceSelect+" WHERE d.map_id IS NOT DISTINCT FROM $1 ORDER BY d.id", mapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Device{}
	for rows.Next() {
		var d Device
		if err := scanDevice(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListEdgesByMap returns edges whose BOTH endpoints live on this map — an edge
// across maps has no canvas to render on (link submaps instead).
func (s *Store) ListEdgesByMap(ctx context.Context, mapID *int64) ([]EdgeRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.source_device_id, e.target_device_id, e.link_type::text, e.label
		FROM edges e
		JOIN devices sd ON sd.id = e.source_device_id
		JOIN devices td ON td.id = e.target_device_id
		WHERE sd.map_id IS NOT DISTINCT FROM $1 AND td.map_id IS NOT DISTINCT FROM $1
		ORDER BY e.id`, mapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EdgeRow{}
	for rows.Next() {
		var e EdgeRow
		if err := rows.Scan(&e.ID, &e.SourceDeviceID, &e.TargetDeviceID, &e.LinkType, &e.Label); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SubmapSummaries(ctx context.Context, mapID *int64) ([]SubmapSummary, error) {
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE tree AS (
			SELECT id, id AS top FROM maps WHERE parent_map_id IS NOT DISTINCT FROM $1
			UNION ALL
			SELECT m.id, t.top FROM maps m JOIN tree t ON m.parent_map_id = t.id
		),
		agg AS (
			SELECT t.top,
			       count(d.id) FILTER (WHERE d.status = 'UP')       AS up,
			       count(d.id) FILTER (WHERE d.status = 'DOWN')     AS down,
			       count(d.id) FILTER (WHERE d.status = 'ORPHANED') AS orphaned,
			       count(d.id) FILTER (WHERE d.status = 'UNKNOWN')  AS unknown
			FROM tree t
			LEFT JOIN devices d ON d.map_id = t.id
			GROUP BY t.top
		)
		SELECT m.id, m.name, m.pos_x, m.pos_y, a.up, a.down, a.orphaned, a.unknown
		FROM agg a
		JOIN maps m ON m.id = a.top
		ORDER BY m.name`, mapID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SubmapSummary{}
	for rows.Next() {
		var sm SubmapSummary
		if err := rows.Scan(&sm.MapID, &sm.Name, &sm.PosX, &sm.PosY,
			&sm.Up, &sm.Down, &sm.Orphaned, &sm.Unknown); err != nil {
			return nil, err
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

type NodePosition struct {
	ID   int64   `json:"id"`
	PosX float64 `json:"pos_x"`
	PosY float64 `json:"pos_y"`
}

// SavePositions persists a "Save Layout" in one statement (Doc 4 §4.2).
// Saved coordinates are canonical, so the node is also locked.
func (s *Store) SavePositions(ctx context.Context, positions []NodePosition) (int64, error) {
	ids := make([]int64, len(positions))
	xs := make([]float64, len(positions))
	ys := make([]float64, len(positions))
	for i, p := range positions {
		ids[i], xs[i], ys[i] = p.ID, p.PosX, p.PosY
	}
	ct, err := s.pool.Exec(ctx, `
		UPDATE devices d
		SET pos_x = u.x, pos_y = u.y, is_locked = true, updated_at = now()
		FROM unnest($1::bigint[], $2::float8[], $3::float8[]) AS u(id, x, y)
		WHERE d.id = u.id`, ids, xs, ys)
	if err != nil {
		return 0, err
	}
	return ct.RowsAffected(), nil
}

func (s *Store) CreateManualEdge(ctx context.Context, source, target int64, label *string) (*EdgeRow, error) {
	var e EdgeRow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO edges (source_device_id, target_device_id, link_type, label)
		VALUES ($1, $2, 'MANUAL', $3)
		RETURNING id, source_device_id, target_device_id, link_type::text, label`,
		source, target, label).Scan(&e.ID, &e.SourceDeviceID, &e.TargetDeviceID, &e.LinkType, &e.Label)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// DeleteManualEdge removes a MANUAL edge only — AUTO edges are derived and get
// regenerated, so deleting them by hand is meaningless (Doc 2 §1.5).
func (s *Store) DeleteManualEdge(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM edges WHERE id = $1 AND link_type = 'MANUAL'`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMapPosition moves a submap node on its parent canvas.
func (s *Store) UpdateMapPosition(ctx context.Context, id int64, x, y float64) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE maps SET pos_x = $2, pos_y = $3, updated_at = now() WHERE id = $1`, id, x, y)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
