package store

import "context"

// MapRow is one map in the tree (Doc 2 §1.13). The client builds the tree from
// the flat parent_map_id list — no server-side nesting needed at this scale.
type MapRow struct {
	ID          int64    `json:"id"`
	ParentMapID *int64   `json:"parent_map_id"`
	Name        string   `json:"name"`
	PosX        *float64 `json:"pos_x"`
	PosY        *float64 `json:"pos_y"`
}

func (s *Store) ListMaps(ctx context.Context) ([]MapRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, parent_map_id, name, pos_x, pos_y FROM maps ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MapRow{}
	for rows.Next() {
		var m MapRow
		if err := rows.Scan(&m.ID, &m.ParentMapID, &m.Name, &m.PosX, &m.PosY); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateMap(ctx context.Context, name string, parentMapID *int64) (*MapRow, error) {
	var m MapRow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO maps (name, parent_map_id) VALUES ($1, $2)
		RETURNING id, parent_map_id, name, pos_x, pos_y`,
		name, parentMapID).Scan(&m.ID, &m.ParentMapID, &m.Name, &m.PosX, &m.PosY)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
