package store

import (
	"context"
	"fmt"
)

func (d *DB) UpdateBackupSettings(ctx context.Context, settings BackupSettings) error {
	_, err := d.UpdateSettings(ctx, SettingsUpdate{Backup: &settings})
	return err
}

func (d *DB) ListJobsByKind(ctx context.Context, kind string, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, kind, single_flight_key, state, timeout_seconds, error, result_json,
		        created_at, started_at, completed_at, updated_at
		 FROM jobs
		 WHERE kind = ?
		 ORDER BY id DESC
		 LIMIT ?`,
		kind,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list jobs by kind: %w", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
