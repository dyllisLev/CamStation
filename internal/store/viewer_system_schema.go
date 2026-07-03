package store

import "context"

func (d *DB) ensureViewerSystemSchema(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS viewers (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			app_version TEXT NOT NULL,
			hostname TEXT NOT NULL,
			device_label TEXT NOT NULL,
			route TEXT NOT NULL,
			mode TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			streams_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			last_heartbeat_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_viewers_last_heartbeat
			ON viewers(last_heartbeat_at)`,
		`CREATE TABLE IF NOT EXISTS viewer_commands (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			viewer_id TEXT NOT NULL,
			type TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			route TEXT NOT NULL DEFAULT '',
			mode TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			sent_at TEXT,
			completed_at TEXT,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(viewer_id) REFERENCES viewers(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_viewer_commands_queue
			ON viewer_commands(viewer_id, state, id)`,
		`CREATE TABLE IF NOT EXISTS diagnostic_artifacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			sha256 TEXT NOT NULL,
			created_at TEXT NOT NULL,
			deleted_at TEXT,
			FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_diagnostic_artifacts_created
			ON diagnostic_artifacts(created_at, id)`,
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
