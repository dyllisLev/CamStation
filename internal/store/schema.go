package store

import (
	"context"
	"database/sql"
	"fmt"
)

func (d *DB) Migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL,
			source TEXT NOT NULL,
			level TEXT NOT NULL,
			message TEXT NOT NULL,
			details_json TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_query
			ON events(level, source, created_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_created_id
			ON events(created_at, id)`,
		`CREATE TABLE IF NOT EXISTS incidents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			source TEXT NOT NULL,
			severity TEXT NOT NULL,
			status TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}',
			acknowledged_at TEXT,
			snoozed_until TEXT,
			resolved_at TEXT,
			auto_key TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_status_severity_source
			ON incidents(status, severity, source, updated_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_incidents_active_auto_key
			ON incidents(auto_key)
			WHERE auto_key != '' AND status != 'resolved'`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			single_flight_key TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL,
			timeout_seconds INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			result_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			updated_at TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_active_single_flight
			ON jobs(single_flight_key)
			WHERE single_flight_key != '' AND state IN ('queued', 'running')`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_state_updated
			ON jobs(state, updated_at)`,
		`CREATE TABLE IF NOT EXISTS job_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			job_id INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			type TEXT NOT NULL,
			message TEXT NOT NULL,
			details_json TEXT NOT NULL DEFAULT '{}',
			FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_job_id
				ON job_events(job_id, id)`,
		`CREATE TABLE IF NOT EXISTS camera_profile_templates (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					profile_name TEXT NOT NULL,
					normalized_profile_name TEXT NOT NULL,
					manufacturer TEXT NOT NULL,
					normalized_manufacturer TEXT NOT NULL,
					model TEXT NOT NULL,
					normalized_model TEXT NOT NULL,
					adapter TEXT NOT NULL,
					normalized_adapter TEXT NOT NULL,
					version INTEGER NOT NULL DEFAULT 1,
					match_rules_json TEXT NOT NULL DEFAULT '[]',
					channels_json TEXT NOT NULL DEFAULT '[]',
					capabilities_json TEXT NOT NULL DEFAULT '{}',
					created_at TEXT NOT NULL,
					updated_at TEXT NOT NULL
				)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_camera_profile_templates_unique_key
				ON camera_profile_templates(
					normalized_adapter,
					normalized_manufacturer,
					normalized_model,
					normalized_profile_name,
					version
				)`,
		`CREATE TABLE IF NOT EXISTS cameras (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL,
					url TEXT NOT NULL,
					stream_name TEXT NOT NULL UNIQUE,
					layout_key TEXT NOT NULL DEFAULT '',
					recording_stream_name TEXT NOT NULL DEFAULT '',
					live_stream_name TEXT NOT NULL DEFAULT '',
					state TEXT NOT NULL,
					enabled INTEGER NOT NULL DEFAULT 1,
					profile_template_id INTEGER,
					manufacturer TEXT NOT NULL DEFAULT '',
					model TEXT NOT NULL DEFAULT '',
					profile_adapter TEXT NOT NULL DEFAULT '',
					host TEXT NOT NULL DEFAULT '',
				rtsp_port INTEGER NOT NULL DEFAULT 0,
				http_port INTEGER NOT NULL DEFAULT 0,
				onvif_port INTEGER NOT NULL DEFAULT 0,
				channel_index INTEGER,
					last_probe_json TEXT NOT NULL DEFAULT '{}',
					last_scan_json TEXT NOT NULL DEFAULT '{}',
					control_capabilities_json TEXT NOT NULL DEFAULT '{}',
					created_at TEXT NOT NULL,
					updated_at TEXT NOT NULL,
					FOREIGN KEY(profile_template_id) REFERENCES camera_profile_templates(id) ON DELETE RESTRICT
				)`,
		`CREATE TABLE IF NOT EXISTS camera_streams (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				camera_id INTEGER NOT NULL,
				role TEXT NOT NULL,
				label TEXT NOT NULL,
				source TEXT NOT NULL,
				url TEXT NOT NULL,
				go2rtc_stream_name TEXT NOT NULL UNIQUE,
				codec TEXT NOT NULL DEFAULT '',
				width INTEGER NOT NULL DEFAULT 0,
				height INTEGER NOT NULL DEFAULT 0,
				fps REAL NOT NULL DEFAULT 0,
				bitrate_kbps INTEGER NOT NULL DEFAULT 0,
				profile_token TEXT NOT NULL DEFAULT '',
				state TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				FOREIGN KEY(camera_id) REFERENCES cameras(id) ON DELETE CASCADE
			)`,
		`CREATE TABLE IF NOT EXISTS camera_preset_names (
			camera_id INTEGER NOT NULL,
			preset_token TEXT NOT NULL,
			name TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (camera_id, preset_token),
			FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_camera_streams_camera_role
				ON camera_streams(camera_id, role)`,
		`CREATE TABLE IF NOT EXISTS layouts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			data TEXT NOT NULL,
			timeline_collapsed INTEGER NOT NULL DEFAULT 0,
			grid_cols INTEGER NOT NULL DEFAULT 48,
			grid_rows INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS recording_segments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			camera_id INTEGER NOT NULL,
			stream_name TEXT NOT NULL,
			filename TEXT NOT NULL,
			temp_path TEXT,
			final_path TEXT,
			ts_start REAL NOT NULL,
			ts_end REAL,
				file_size INTEGER,
				status TEXT NOT NULL,
				backup_state TEXT NOT NULL DEFAULT 'pending',
				backed_up_at TEXT,
				backup_job_id INTEGER NOT NULL DEFAULT 0,
				error TEXT,
				created_at INTEGER NOT NULL,
				updated_at INTEGER NOT NULL,
			UNIQUE(stream_name, ts_start)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recording_segments_stream_ts
			ON recording_segments(stream_name, ts_start)`,
		`CREATE INDEX IF NOT EXISTS idx_recording_segments_status
			ON recording_segments(status, updated_at)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (1, datetime('now'))`,
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration statement failed: %w", err)
		}
	}
	if err := d.ensureCameraProfileSchema(ctx); err != nil {
		return err
	}
	if err := d.ensureCameraPolicySchema(ctx); err != nil {
		return err
	}
	if err := d.ensureViewerSystemSchema(ctx); err != nil {
		return err
	}
	if err := d.ensureRecordingBackupSchema(ctx); err != nil {
		return err
	}
	return nil
}

func (d *DB) ensureRecordingBackupSchema(ctx context.Context) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"backup_state", "TEXT NOT NULL DEFAULT 'pending'"},
		{"backed_up_at", "TEXT"},
		{"backup_job_id", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, column := range columns {
		if err := d.addColumnIfMissing(ctx, "recording_segments", column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) addColumnIfMissing(ctx context.Context, table, name, definition string) error {
	rows, err := d.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			columnName string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return err
		}
		if columnName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, definition))
	return err
}
