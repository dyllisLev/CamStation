package store

import (
	"context"
	"fmt"
)

func (d *DB) ensureCameraPolicySchema(ctx context.Context) error {
	columns := []struct{ name, definition string }{
		{"source_key", "TEXT NOT NULL DEFAULT ''"},
		{"detected_video_codec", "TEXT NOT NULL DEFAULT ''"},
		{"detected_audio_codec", "TEXT NOT NULL DEFAULT ''"},
		{"detected_profile", "TEXT NOT NULL DEFAULT ''"},
		{"detected_level", "TEXT NOT NULL DEFAULT ''"},
		{"detected_pixel_format", "TEXT NOT NULL DEFAULT ''"},
		{"detected_bit_depth", "INTEGER NOT NULL DEFAULT 0"},
		{"detected_width", "INTEGER NOT NULL DEFAULT 0"},
		{"detected_height", "INTEGER NOT NULL DEFAULT 0"},
		{"detected_fps", "REAL NOT NULL DEFAULT 0"},
		{"detected_checked_at", "TEXT"},
		{"detected_error", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		if err := d.addColumnIfMissing(ctx, "camera_streams", column.name, column.definition); err != nil {
			return err
		}
	}
	statements := []string{
		`UPDATE camera_streams AS stream SET source_key = CASE
			WHEN (SELECT count(*) FROM camera_streams AS earlier
				WHERE earlier.camera_id = stream.camera_id AND earlier.role = stream.role AND earlier.id <= stream.id) = 1
			THEN CASE WHEN stream.role = '' THEN 'recording' ELSE stream.role END
			ELSE (CASE WHEN stream.role = '' THEN 'recording' ELSE stream.role END) || '-' || stream.id
		END WHERE source_key = ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_camera_streams_camera_source_key ON camera_streams(camera_id, source_key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_camera_streams_id_camera ON camera_streams(id, camera_id)`,
		`CREATE TABLE IF NOT EXISTS camera_outputs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			camera_id INTEGER NOT NULL,
			purpose TEXT NOT NULL CHECK (purpose IN ('recording','live','focus')),
			stream_name TEXT NOT NULL UNIQUE,
			source_stream_id INTEGER NOT NULL,
			video_mode TEXT NOT NULL CHECK (video_mode IN ('auto','copy','h264')),
			max_width INTEGER,
			max_height INTEGER,
			max_fps REAL,
			audio_mode TEXT NOT NULL CHECK (audio_mode IN ('source','none','aac')),
			activation TEXT NOT NULL CHECK (activation IN ('on_demand','always')),
			applied_policy_json TEXT NOT NULL DEFAULT '{}',
			verified_video_codec TEXT NOT NULL DEFAULT '',
			verified_audio_codec TEXT NOT NULL DEFAULT '',
			verified_width INTEGER NOT NULL DEFAULT 0,
			verified_height INTEGER NOT NULL DEFAULT 0,
			verified_fps REAL NOT NULL DEFAULT 0,
			verified_transcoding INTEGER NOT NULL DEFAULT 0,
			verified_at TEXT,
			verification_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(camera_id, purpose),
			CHECK ((max_width IS NULL) = (max_height IS NULL)),
			CHECK (max_width IS NULL OR (max_width BETWEEN 2 AND 7680 AND max_width % 2 = 0)),
			CHECK (max_height IS NULL OR (max_height BETWEEN 2 AND 4320 AND max_height % 2 = 0)),
			CHECK (max_fps IS NULL OR max_fps BETWEEN 1 AND 60),
			CHECK (video_mode != 'copy' OR (max_width IS NULL AND max_height IS NULL AND max_fps IS NULL)),
			FOREIGN KEY(camera_id) REFERENCES cameras(id) ON DELETE CASCADE,
			FOREIGN KEY(source_stream_id, camera_id) REFERENCES camera_streams(id, camera_id)
		)`,
		`CREATE TABLE IF NOT EXISTS camera_policy_states (
			camera_id INTEGER PRIMARY KEY,
			desired_revision INTEGER NOT NULL DEFAULT 1,
			applied_revision INTEGER NOT NULL DEFAULT 0,
			apply_state TEXT NOT NULL DEFAULT 'pending' CHECK (apply_state IN ('applied','pending','apply_failed')),
			apply_state_at TEXT NOT NULL,
			apply_error TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(camera_id) REFERENCES cameras(id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("camera policy migration failed: %w", err)
		}
	}
	if err := d.addColumnIfMissing(ctx, "camera_outputs", "verified_transcoding", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE camera_outputs SET applied_policy_json=json_set(
		applied_policy_json,'$.sourceKey',(SELECT source_key FROM camera_streams
		WHERE id=CAST(json_extract(applied_policy_json,'$.sourceStreamId') AS INTEGER)))
		WHERE COALESCE(json_extract(applied_policy_json,'$.sourceKey'),'')=''
		AND COALESCE(json_extract(applied_policy_json,'$.sourceStreamId'),0) != 0`); err != nil {
		return fmt.Errorf("camera applied source-key migration failed: %w", err)
	}
	return d.ensureCameraPolicyDefaults(ctx)
}

func (d *DB) ensureCameraPolicyDefaults(ctx context.Context) error {
	statements := []string{
		`INSERT INTO camera_streams(camera_id,role,source_key,label,source,url,go2rtc_stream_name,state,created_at,updated_at)
		 SELECT c.id,'recording','recording','recording','legacy',c.url,
		 CASE WHEN c.recording_stream_name != '' THEN c.recording_stream_name ELSE c.stream_name END,
		 c.state,c.created_at,c.updated_at FROM cameras c
		 WHERE NOT EXISTS (SELECT 1 FROM camera_streams s WHERE s.camera_id=c.id)`,
		`INSERT OR IGNORE INTO camera_policy_states(camera_id,desired_revision,applied_revision,apply_state,apply_state_at)
		 SELECT id,1,0,'pending',updated_at FROM cameras`,
		defaultOutputInsertSQL("recording"), defaultOutputInsertSQL("live"), defaultOutputInsertSQL("focus"),
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var invalid int
	if err := d.db.QueryRowContext(ctx, `SELECT count(*) FROM cameras c
		WHERE (SELECT count(*) FROM camera_outputs o WHERE o.camera_id=c.id) != 3`).Scan(&invalid); err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("%d cameras do not have exactly three outputs", invalid)
	}
	return nil
}

func defaultOutputInsertSQL(purpose string) string {
	sourceOrder := "CASE WHEN s.source_key = 'recording' THEN 0 ELSE 1 END"
	streamName := "CASE WHEN c.recording_stream_name != '' THEN c.recording_stream_name ELSE c.stream_name || '-recording' END"
	video, audio, maxWidth, maxHeight := "'copy'", "'source'", "NULL", "NULL"
	if purpose == "live" {
		sourceOrder = "CASE WHEN s.source_key = 'live' THEN 0 WHEN s.source_key = 'recording' THEN 1 ELSE 2 END"
		streamName, video, audio = "CASE WHEN c.live_stream_name != '' AND c.live_stream_name != c.recording_stream_name THEN c.live_stream_name ELSE c.stream_name || '-live' END", "'auto'", "'none'"
	}
	if purpose == "focus" {
		streamName, video, audio, maxWidth, maxHeight = "c.stream_name || '-focus'", "'auto'", "'none'", "1920", "1080"
	}
	return fmt.Sprintf(`INSERT OR IGNORE INTO camera_outputs(
		camera_id, purpose, stream_name, source_stream_id, video_mode, max_width, max_height,
		audio_mode, activation, created_at, updated_at
	) SELECT c.id, '%s', %s,
		(SELECT s.id FROM camera_streams s WHERE s.camera_id = c.id ORDER BY %s, s.id LIMIT 1),
		%s, %s, %s, %s, 'on_demand', c.created_at, c.updated_at
	FROM cameras c WHERE EXISTS (SELECT 1 FROM camera_streams s WHERE s.camera_id = c.id)`,
		purpose, streamName, sourceOrder, video, maxWidth, maxHeight, audio)
}
