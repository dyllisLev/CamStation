package store

import (
	"context"
	"time"
)

func (d *DB) UpsertCameraPresetName(ctx context.Context, cameraID int64, token, name string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.db.ExecContext(ctx, `INSERT INTO camera_preset_names(camera_id,preset_token,name,created_at,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(camera_id,preset_token)
		DO UPDATE SET name=excluded.name,updated_at=excluded.updated_at`, cameraID, token, name, now, now)
	return err
}

func (d *DB) ListCameraPresetNames(ctx context.Context, cameraID int64) (map[string]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT preset_token,name FROM camera_preset_names WHERE camera_id=?`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make(map[string]string)
	for rows.Next() {
		var token, name string
		if err := rows.Scan(&token, &name); err != nil {
			return nil, err
		}
		names[token] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func (d *DB) DeleteCameraPresetName(ctx context.Context, cameraID int64, token string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM camera_preset_names WHERE camera_id=? AND preset_token=?`, cameraID, token)
	return err
}

func (d *DB) ReconcileCameraPresetNames(ctx context.Context, cameraID int64, activeTokens []string) error {
	active := make(map[string]struct{}, len(activeTokens))
	for _, token := range activeTokens {
		active[token] = struct{}{}
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT preset_token FROM camera_preset_names WHERE camera_id=?`, cameraID)
	if err != nil {
		return err
	}
	var existing []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, token)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, token := range existing {
		if _, ok := active[token]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM camera_preset_names WHERE camera_id=? AND preset_token=?`, cameraID, token); err != nil {
			return err
		}
	}
	return tx.Commit()
}
