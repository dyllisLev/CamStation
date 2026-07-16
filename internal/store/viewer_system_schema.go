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
			agent_state TEXT NOT NULL DEFAULT '',
			agent_version TEXT NOT NULL DEFAULT '',
			control_state TEXT NOT NULL DEFAULT '',
			viewer_state TEXT NOT NULL DEFAULT '',
			viewer_version TEXT NOT NULL DEFAULT '',
			renderer_state TEXT NOT NULL DEFAULT '',
			last_control_success_at TEXT,
			last_viewer_heartbeat_at TEXT,
			last_renderer_heartbeat_at TEXT,
			last_video_progress_at TEXT,
			update_state TEXT NOT NULL DEFAULT '',
			update_target_version TEXT NOT NULL DEFAULT '',
			update_generation INTEGER NOT NULL DEFAULT 0,
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
			stream_name TEXT NOT NULL DEFAULT '',
			desired_version TEXT NOT NULL DEFAULT '',
			artifact_sha256 TEXT NOT NULL DEFAULT '',
			payload_hash TEXT NOT NULL DEFAULT '',
			ttl_seconds INTEGER NOT NULL DEFAULT 300,
			operation_key TEXT NOT NULL DEFAULT '',
			generation INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			sent_at TEXT,
			delivered_at TEXT,
			acknowledged_at TEXT,
			running_at TEXT,
			result_at TEXT,
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
	viewerColumns := []struct {
		name       string
		definition string
	}{
		{"agent_state", "TEXT NOT NULL DEFAULT ''"},
		{"agent_version", "TEXT NOT NULL DEFAULT ''"},
		{"control_state", "TEXT NOT NULL DEFAULT ''"},
		{"viewer_state", "TEXT NOT NULL DEFAULT ''"},
		{"viewer_version", "TEXT NOT NULL DEFAULT ''"},
		{"renderer_state", "TEXT NOT NULL DEFAULT ''"},
		{"last_control_success_at", "TEXT"},
		{"last_viewer_heartbeat_at", "TEXT"},
		{"last_renderer_heartbeat_at", "TEXT"},
		{"last_video_progress_at", "TEXT"},
		{"update_state", "TEXT NOT NULL DEFAULT ''"},
		{"update_target_version", "TEXT NOT NULL DEFAULT ''"},
		{"update_generation", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, column := range viewerColumns {
		if err := d.addColumnIfMissing(ctx, "viewers", column.name, column.definition); err != nil {
			return err
		}
	}
	commandColumns := []struct {
		name       string
		definition string
	}{
		{"stream_name", "TEXT NOT NULL DEFAULT ''"},
		{"desired_version", "TEXT NOT NULL DEFAULT ''"},
		{"artifact_sha256", "TEXT NOT NULL DEFAULT ''"},
		{"payload_hash", "TEXT NOT NULL DEFAULT ''"},
		{"ttl_seconds", "INTEGER NOT NULL DEFAULT 300"},
		{"operation_key", "TEXT NOT NULL DEFAULT ''"},
		{"generation", "INTEGER NOT NULL DEFAULT 0"},
		{"delivered_at", "TEXT"},
		{"acknowledged_at", "TEXT"},
		{"running_at", "TEXT"},
		{"result_at", "TEXT"},
	}
	for _, column := range commandColumns {
		if err := d.addColumnIfMissing(ctx, "viewer_commands", column.name, column.definition); err != nil {
			return err
		}
	}
	if _, err := d.db.ExecContext(ctx,
		`UPDATE viewer_commands SET state = 'delivered',
			sent_at = COALESCE(sent_at, updated_at),
			delivered_at = COALESCE(delivered_at, sent_at, updated_at)
		 WHERE state = 'sent'`,
	); err != nil {
		return err
	}
	return nil
}
