package store

import (
	"context"
	"encoding/base64"
	"fmt"
)

// IconRow carries the image inline as a data: URL. <img> tags can't send the
// bearer header a raw-bytes endpoint would require, and custom icons are small
// (upload capped at 512 KB), so inlining beats a separate authenticated route.
type IconRow struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Category *string `json:"category"`
	Mime     string  `json:"mime"`
	DataURL  string  `json:"data_url"`
}

func (s *Store) ListIcons(ctx context.Context) ([]IconRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, category, mime, data FROM icons ORDER BY category NULLS FIRST, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []IconRow{}
	for rows.Next() {
		var (
			ic   IconRow
			data []byte
		)
		if err := rows.Scan(&ic.ID, &ic.Name, &ic.Category, &ic.Mime, &data); err != nil {
			return nil, err
		}
		ic.DataURL = fmt.Sprintf("data:%s;base64,%s", ic.Mime, base64.StdEncoding.EncodeToString(data))
		out = append(out, ic)
	}
	return out, rows.Err()
}

func (s *Store) CreateIcon(ctx context.Context, name, category, mime string, data []byte) (int64, error) {
	var cat *string
	if category != "" {
		cat = &category
	}
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO icons (name, category, mime, data) VALUES ($1, $2, $3, $4) RETURNING id`,
		name, cat, mime, data).Scan(&id)
	return id, err
}

func (s *Store) DeleteIcon(ctx context.Context, id int64) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM icons WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
