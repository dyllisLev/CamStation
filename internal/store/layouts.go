package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (d *DB) ListLayouts(ctx context.Context) ([]LayoutProfile, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, name, data, timeline_collapsed, grid_cols, grid_rows, created_at, updated_at
		 FROM layouts ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	layouts := make([]LayoutProfile, 0)
	for rows.Next() {
		layout, err := scanLayout(rows)
		if err != nil {
			return nil, err
		}
		layouts = append(layouts, layout)
	}
	return layouts, rows.Err()
}

func (d *DB) CreateLayout(ctx context.Context, layout LayoutProfile) (LayoutProfile, error) {
	now := time.Now().Unix()
	if layout.ID == "" {
		layout.ID = fmt.Sprintf("%d", now)
	}
	if strings.TrimSpace(layout.Name) == "" {
		layout.Name = "기본"
	}
	if layout.GridCols == 0 {
		layout.GridCols = 48
	}
	layout.CreatedAt = now
	layout.UpdatedAt = now
	encoded, err := json.Marshal(layout.Data)
	if err != nil {
		return LayoutProfile{}, err
	}
	var gridRows any
	if layout.GridRows != nil {
		gridRows = *layout.GridRows
	}
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO layouts(id, name, data, timeline_collapsed, grid_cols, grid_rows, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		layout.ID,
		strings.TrimSpace(layout.Name),
		string(encoded),
		boolInt(layout.TimelineCollapsed),
		layout.GridCols,
		gridRows,
		layout.CreatedAt,
		layout.UpdatedAt,
	)
	if err != nil {
		return LayoutProfile{}, err
	}
	return d.GetLayout(ctx, layout.ID)
}

func (d *DB) UpdateLayout(ctx context.Context, id string, layout LayoutProfile) (LayoutProfile, error) {
	current, err := d.GetLayout(ctx, id)
	if err != nil {
		return LayoutProfile{}, err
	}
	if strings.TrimSpace(layout.Name) != "" {
		current.Name = strings.TrimSpace(layout.Name)
	}
	if layout.Data != nil {
		current.Data = layout.Data
	}
	current.TimelineCollapsed = layout.TimelineCollapsed
	if layout.GridCols != 0 {
		current.GridCols = layout.GridCols
	}
	current.GridRows = layout.GridRows
	current.UpdatedAt = time.Now().Unix()
	encoded, err := json.Marshal(current.Data)
	if err != nil {
		return LayoutProfile{}, err
	}
	var gridRows any
	if current.GridRows != nil {
		gridRows = *current.GridRows
	}
	_, err = d.db.ExecContext(ctx,
		`UPDATE layouts
		 SET name = ?, data = ?, timeline_collapsed = ?, grid_cols = ?, grid_rows = ?, updated_at = ?
		 WHERE id = ?`,
		current.Name,
		string(encoded),
		boolInt(current.TimelineCollapsed),
		current.GridCols,
		gridRows,
		current.UpdatedAt,
		id,
	)
	if err != nil {
		return LayoutProfile{}, err
	}
	return d.GetLayout(ctx, id)
}

func (d *DB) GetLayout(ctx context.Context, id string) (LayoutProfile, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, name, data, timeline_collapsed, grid_cols, grid_rows, created_at, updated_at
		 FROM layouts WHERE id = ?`,
		id,
	)
	return scanLayout(row)
}

func (d *DB) DeleteLayout(ctx context.Context, id string) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM layouts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete layout: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete layout rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("layout %q: %w", id, ErrNotFound)
	}
	return nil
}

func scanLayout(row scanner) (LayoutProfile, error) {
	var layout LayoutProfile
	var dataJSON string
	var timelineCollapsed int
	var gridRows sql.NullInt64
	if err := row.Scan(
		&layout.ID,
		&layout.Name,
		&dataJSON,
		&timelineCollapsed,
		&layout.GridCols,
		&gridRows,
		&layout.CreatedAt,
		&layout.UpdatedAt,
	); err != nil {
		return LayoutProfile{}, err
	}
	if err := json.Unmarshal([]byte(dataJSON), &layout.Data); err != nil {
		return LayoutProfile{}, err
	}
	layout.TimelineCollapsed = timelineCollapsed != 0
	if gridRows.Valid {
		value := int(gridRows.Int64)
		layout.GridRows = &value
	}
	return layout, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
