package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type Event struct {
	ID        int64          `json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	Source    string         `json:"source"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}

type Camera struct {
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	URL                 string         `json:"url,omitempty"`
	RedactedURL         string         `json:"redactedUrl"`
	StreamName          string         `json:"streamName"`
	LayoutKey           string         `json:"layoutKey,omitempty"`
	RecordingStreamName string         `json:"recordingStreamName,omitempty"`
	LiveStreamName      string         `json:"liveStreamName,omitempty"`
	State               string         `json:"state"`
	Manufacturer        string         `json:"manufacturer,omitempty"`
	Model               string         `json:"model,omitempty"`
	ProfileAdapter      string         `json:"profileAdapter,omitempty"`
	Host                string         `json:"host,omitempty"`
	RTSPPort            int            `json:"rtspPort,omitempty"`
	HTTPPort            int            `json:"httpPort,omitempty"`
	ONVIFPort           int            `json:"onvifPort,omitempty"`
	ChannelIndex        *int           `json:"channelIndex,omitempty"`
	LastProbeJSON       map[string]any `json:"lastProbe,omitempty"`
	LastScanJSON        map[string]any `json:"lastScan,omitempty"`
	Streams             []CameraStream `json:"streams,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

type CameraStreamRole string

const (
	CameraStreamRoleRecording CameraStreamRole = "recording"
	CameraStreamRoleLive      CameraStreamRole = "live"
	CameraStreamRoleSnapshot  CameraStreamRole = "snapshot"
)

type CameraStream struct {
	ID               int64            `json:"id"`
	CameraID         int64            `json:"camera_id"`
	Role             CameraStreamRole `json:"role"`
	Label            string           `json:"label"`
	Source           string           `json:"source"`
	URL              string           `json:"url,omitempty"`
	RedactedURL      string           `json:"redactedUrl"`
	Go2RTCStreamName string           `json:"go2rtcStreamName"`
	Codec            string           `json:"codec,omitempty"`
	Width            int              `json:"width,omitempty"`
	Height           int              `json:"height,omitempty"`
	FPS              float64          `json:"fps,omitempty"`
	BitrateKbps      int              `json:"bitrateKbps,omitempty"`
	ProfileToken     string           `json:"profileToken,omitempty"`
	State            string           `json:"state,omitempty"`
	CreatedAt        time.Time        `json:"createdAt,omitempty"`
	UpdatedAt        time.Time        `json:"updatedAt,omitempty"`
}

type LayoutItem struct {
	I         string     `json:"i"`
	X         int        `json:"x"`
	Y         int        `json:"y"`
	W         int        `json:"w"`
	H         int        `json:"h"`
	MinW      int        `json:"minW,omitempty"`
	MinH      int        `json:"minH,omitempty"`
	VideoZoom *VideoZoom `json:"videoZoom,omitempty"`
}

type VideoZoom struct {
	Scale float64 `json:"scale"`
	TX    float64 `json:"tx"`
	TY    float64 `json:"ty"`
}

type LayoutProfile struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Data              []LayoutItem `json:"data"`
	TimelineCollapsed bool         `json:"timeline_collapsed"`
	GridCols          int          `json:"grid_cols"`
	GridRows          *int         `json:"grid_rows"`
	CreatedAt         int64        `json:"created_at"`
	UpdatedAt         int64        `json:"updated_at"`
}

type RecordingSegment struct {
	ID         int64    `json:"id"`
	CameraID   int64    `json:"camera_id"`
	StreamName string   `json:"streamName"`
	Filename   string   `json:"filename"`
	TempPath   string   `json:"tempPath,omitempty"`
	FinalPath  string   `json:"finalPath,omitempty"`
	TSStart    float64  `json:"ts_start"`
	TSEnd      *float64 `json:"ts_end"`
	FileSize   *int64   `json:"file_size"`
	Status     string   `json:"status"`
	Error      string   `json:"error,omitempty"`
	CreatedAt  int64    `json:"created_at"`
	UpdatedAt  int64    `json:"updated_at"`
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

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
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
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
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
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
	return nil
}

func (d *DB) ensureCameraProfileSchema(ctx context.Context) error {
	columns := []struct {
		name       string
		definition string
	}{
		{"layout_key", "TEXT NOT NULL DEFAULT ''"},
		{"recording_stream_name", "TEXT NOT NULL DEFAULT ''"},
		{"live_stream_name", "TEXT NOT NULL DEFAULT ''"},
		{"manufacturer", "TEXT NOT NULL DEFAULT ''"},
		{"model", "TEXT NOT NULL DEFAULT ''"},
		{"profile_adapter", "TEXT NOT NULL DEFAULT ''"},
		{"host", "TEXT NOT NULL DEFAULT ''"},
		{"rtsp_port", "INTEGER NOT NULL DEFAULT 0"},
		{"http_port", "INTEGER NOT NULL DEFAULT 0"},
		{"onvif_port", "INTEGER NOT NULL DEFAULT 0"},
		{"channel_index", "INTEGER"},
		{"last_scan_json", "TEXT NOT NULL DEFAULT '{}'"},
	}
	for _, column := range columns {
		if err := d.addColumnIfMissing(ctx, "cameras", column.name, column.definition); err != nil {
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

func (d *DB) AppendEvent(ctx context.Context, event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.Level == "" {
		event.Level = "info"
	}
	details := event.Details
	if details == nil {
		details = map[string]any{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO events(created_at, source, level, message, details_json) VALUES (?, ?, ?, ?, ?)`,
		event.CreatedAt.Format(time.RFC3339Nano),
		event.Source,
		event.Level,
		event.Message,
		string(encoded),
	)
	return err
}

func (d *DB) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, created_at, source, level, message, details_json FROM events ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var createdAt, detailsJSON string
		if err := rows.Scan(&event.ID, &createdAt, &event.Source, &event.Level, &event.Message, &detailsJSON); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if err := json.Unmarshal([]byte(detailsJSON), &event.Details); err != nil {
			event.Details = map[string]any{"parseError": err.Error()}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (d *DB) UpsertCamera(ctx context.Context, camera Camera) (Camera, error) {
	now := time.Now().UTC()
	if camera.Name == "" {
		camera.Name = "Camera"
	}
	if camera.State == "" {
		camera.State = "unknown"
	}
	if camera.StreamName == "" {
		camera.StreamName = "camera-1"
	}
	if camera.LayoutKey == "" {
		camera.LayoutKey = camera.StreamName
	}
	if camera.CreatedAt.IsZero() {
		camera.CreatedAt = now
	}
	camera.UpdatedAt = now
	probe := camera.LastProbeJSON
	if probe == nil {
		probe = map[string]any{}
	}
	encoded, err := json.Marshal(probe)
	if err != nil {
		return Camera{}, err
	}
	scan := camera.LastScanJSON
	if scan == nil {
		scan = map[string]any{}
	}
	encodedScan, err := json.Marshal(scan)
	if err != nil {
		return Camera{}, err
	}
	var channelIndex any
	if camera.ChannelIndex != nil {
		channelIndex = *camera.ChannelIndex
	}

	_, err = d.db.ExecContext(ctx,
		`INSERT INTO cameras(
			name, url, stream_name, layout_key, recording_stream_name, live_stream_name, state,
			manufacturer, model, profile_adapter, host, rtsp_port, http_port, onvif_port, channel_index,
			last_probe_json, last_scan_json, created_at, updated_at
		 )
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(stream_name) DO UPDATE SET
			name=excluded.name,
			url=excluded.url,
			layout_key=excluded.layout_key,
			recording_stream_name=excluded.recording_stream_name,
			live_stream_name=excluded.live_stream_name,
			state=excluded.state,
			manufacturer=excluded.manufacturer,
			model=excluded.model,
			profile_adapter=excluded.profile_adapter,
			host=excluded.host,
			rtsp_port=excluded.rtsp_port,
			http_port=excluded.http_port,
			onvif_port=excluded.onvif_port,
			channel_index=excluded.channel_index,
			last_probe_json=excluded.last_probe_json,
			last_scan_json=excluded.last_scan_json,
			updated_at=excluded.updated_at`,
		camera.Name,
		camera.URL,
		camera.StreamName,
		camera.LayoutKey,
		camera.RecordingStreamName,
		camera.LiveStreamName,
		camera.State,
		camera.Manufacturer,
		camera.Model,
		camera.ProfileAdapter,
		camera.Host,
		camera.RTSPPort,
		camera.HTTPPort,
		camera.ONVIFPort,
		channelIndex,
		string(encoded),
		string(encodedScan),
		camera.CreatedAt.Format(time.RFC3339Nano),
		camera.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Camera{}, err
	}
	return d.GetCameraByStream(ctx, camera.StreamName)
}

func (d *DB) ListCameras(ctx context.Context, includeSecrets bool) ([]Camera, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, name, url, stream_name, layout_key, recording_stream_name, live_stream_name, state,
		        manufacturer, model, profile_adapter, host, rtsp_port, http_port, onvif_port, channel_index,
		        last_probe_json, last_scan_json, created_at, updated_at
		 FROM cameras ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cameras := make([]Camera, 0)
	for rows.Next() {
		camera, err := scanCamera(rows, includeSecrets)
		if err != nil {
			return nil, err
		}
		cameras = append(cameras, camera)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range cameras {
		streams, err := d.ListCameraStreams(ctx, cameras[i].ID, includeSecrets)
		if err != nil {
			return nil, err
		}
		cameras[i].Streams = streams
		applyRoleStreamNames(&cameras[i])
	}
	return cameras, nil
}

func (d *DB) GetCameraByStream(ctx context.Context, streamName string) (Camera, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, name, url, stream_name, layout_key, recording_stream_name, live_stream_name, state,
		        manufacturer, model, profile_adapter, host, rtsp_port, http_port, onvif_port, channel_index,
		        last_probe_json, last_scan_json, created_at, updated_at
		 FROM cameras
		 WHERE stream_name = ? OR recording_stream_name = ? OR live_stream_name = ?`,
		streamName,
		streamName,
		streamName,
	)
	camera, err := scanCamera(row, true)
	if err != nil {
		return Camera{}, err
	}
	streams, err := d.ListCameraStreams(ctx, camera.ID, true)
	if err != nil {
		return Camera{}, err
	}
	camera.Streams = streams
	applyRoleStreamNames(&camera)
	return camera, nil
}

func (d *DB) DeleteCamera(ctx context.Context, streamName string) (Camera, error) {
	camera, err := d.GetCameraByStream(ctx, streamName)
	if err != nil {
		return Camera{}, err
	}
	result, err := d.db.ExecContext(ctx, `DELETE FROM cameras WHERE id = ?`, camera.ID)
	if err != nil {
		return Camera{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Camera{}, err
	}
	if affected == 0 {
		return Camera{}, sql.ErrNoRows
	}
	return camera, nil
}

func (d *DB) ReplaceCameraStreams(ctx context.Context, cameraID int64, streams []CameraStream) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM camera_streams WHERE camera_id = ?`, cameraID); err != nil {
		return err
	}
	now := time.Now().UTC()
	recordingStream := ""
	liveStream := ""
	for _, stream := range streams {
		if stream.Go2RTCStreamName == "" || stream.URL == "" {
			continue
		}
		if stream.Role == "" {
			stream.Role = CameraStreamRoleRecording
		}
		if stream.State == "" {
			stream.State = "unknown"
		}
		if stream.Role == CameraStreamRoleRecording && recordingStream == "" {
			recordingStream = stream.Go2RTCStreamName
		}
		if stream.Role == CameraStreamRoleLive && liveStream == "" {
			liveStream = stream.Go2RTCStreamName
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO camera_streams(
				camera_id, role, label, source, url, go2rtc_stream_name, codec, width, height, fps,
				bitrate_kbps, profile_token, state, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cameraID,
			stream.Role,
			stream.Label,
			stream.Source,
			stream.URL,
			stream.Go2RTCStreamName,
			stream.Codec,
			stream.Width,
			stream.Height,
			stream.FPS,
			stream.BitrateKbps,
			stream.ProfileToken,
			stream.State,
			now.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	if liveStream == "" {
		liveStream = recordingStream
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE cameras
		 SET recording_stream_name = ?, live_stream_name = ?, updated_at = ?
		 WHERE id = ?`,
		recordingStream,
		liveStream,
		now.Format(time.RFC3339Nano),
		cameraID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) ListCameraStreams(ctx context.Context, cameraID int64, includeSecrets bool) ([]CameraStream, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, role, label, source, url, go2rtc_stream_name, codec, width, height, fps,
		        bitrate_kbps, profile_token, state, created_at, updated_at
		 FROM camera_streams
		 WHERE camera_id = ?
		 ORDER BY CASE role WHEN 'recording' THEN 0 WHEN 'live' THEN 1 WHEN 'snapshot' THEN 2 ELSE 3 END, id`,
		cameraID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	streams := make([]CameraStream, 0)
	for rows.Next() {
		stream, err := scanCameraStream(rows, includeSecrets)
		if err != nil {
			return nil, err
		}
		streams = append(streams, stream)
	}
	return streams, rows.Err()
}

func (d *DB) OpenRecordingSegment(ctx context.Context, segment RecordingSegment) (RecordingSegment, error) {
	now := time.Now().Unix()
	if segment.Status == "" {
		segment.Status = "recording"
	}
	segment.CreatedAt = now
	segment.UpdatedAt = now
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO recording_segments(
			camera_id, stream_name, filename, temp_path, final_path, ts_start,
			ts_end, file_size, status, error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(stream_name, ts_start) DO UPDATE SET
			filename=excluded.filename,
			temp_path=excluded.temp_path,
			status=excluded.status,
			error='',
			updated_at=excluded.updated_at`,
		segment.CameraID,
		segment.StreamName,
		segment.Filename,
		nullString(segment.TempPath),
		nullString(segment.FinalPath),
		segment.TSStart,
		segment.TSEnd,
		segment.FileSize,
		segment.Status,
		nullString(segment.Error),
		segment.CreatedAt,
		segment.UpdatedAt,
	)
	if err != nil {
		return RecordingSegment{}, err
	}
	return d.GetRecordingSegment(ctx, segment.StreamName, segment.TSStart)
}

func (d *DB) CloseRecordingSegment(ctx context.Context, streamName, filename string, tsEnd float64, finalPath string, fileSize *int64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET ts_end = ?, final_path = ?, file_size = ?, status = 'ready', error = '', updated_at = ?
		 WHERE stream_name = ? AND filename = ? AND status IN ('recording', 'finalizing', 'failed')`,
		tsEnd,
		nullString(finalPath),
		fileSize,
		time.Now().Unix(),
		streamName,
		filename,
	)
	return err
}

func (d *DB) MarkRecordingSegmentStatus(ctx context.Context, streamName, filename, status, message string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET status = ?, error = ?, updated_at = ?
		 WHERE stream_name = ? AND filename = ?`,
		status,
		nullString(message),
		time.Now().Unix(),
		streamName,
		filename,
	)
	return err
}

func (d *DB) MarkRecordingSegmentStatusByID(ctx context.Context, id int64, status, message string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET status = ?, error = ?, updated_at = ?
		 WHERE id = ?`,
		status,
		nullString(message),
		time.Now().Unix(),
		id,
	)
	return err
}

func (d *DB) GetRecordingSegment(ctx context.Context, streamName string, tsStart float64) (RecordingSegment, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
		        ts_end, file_size, status, error, created_at, updated_at
		 FROM recording_segments WHERE stream_name = ? AND ts_start = ?`,
		streamName,
		tsStart,
	)
	return scanRecordingSegment(row)
}

func (d *DB) ListRecordingSegments(ctx context.Context, streamName string, from, to time.Time, statuses ...string) ([]RecordingSegment, error) {
	args := []any{streamName, float64(from.Unix()), float64(to.Unix())}
	statusClause := ""
	if len(statuses) > 0 {
		placeholders := make([]string, 0, len(statuses))
		for _, status := range statuses {
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		statusClause = " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
		        ts_end, file_size, status, error, created_at, updated_at
		 FROM recording_segments
		 WHERE stream_name = ? AND ts_start >= ? AND ts_start < ?`+statusClause+`
		 ORDER BY ts_start`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func (d *DB) ListRecordingSegmentsByStatus(ctx context.Context, statuses ...string) ([]RecordingSegment, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(statuses))
	placeholders := make([]string, 0, len(statuses))
	for _, status := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, status)
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
		        ts_end, file_size, status, error, created_at, updated_at
		 FROM recording_segments
		 WHERE status IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY ts_start, id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func (d *DB) ListDeletableRecordingSegments(ctx context.Context) ([]RecordingSegment, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
		        ts_end, file_size, status, error, created_at, updated_at
		 FROM recording_segments
		 WHERE status = 'ready' AND final_path IS NOT NULL AND final_path != ''
		 ORDER BY ts_start, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func (d *DB) MarkRecordingSegmentDeleted(ctx context.Context, id int64, reason string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET status = 'deleted', error = ?, updated_at = ?
		 WHERE id = ? AND status = 'ready'`,
		nullString(reason),
		time.Now().Unix(),
		id,
	)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCamera(row scanner, includeSecrets bool) (Camera, error) {
	var camera Camera
	var createdAt, updatedAt, probeJSON, scanJSON string
	var channelIndex sql.NullInt64
	if err := row.Scan(
		&camera.ID,
		&camera.Name,
		&camera.URL,
		&camera.StreamName,
		&camera.LayoutKey,
		&camera.RecordingStreamName,
		&camera.LiveStreamName,
		&camera.State,
		&camera.Manufacturer,
		&camera.Model,
		&camera.ProfileAdapter,
		&camera.Host,
		&camera.RTSPPort,
		&camera.HTTPPort,
		&camera.ONVIFPort,
		&channelIndex,
		&probeJSON,
		&scanJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Camera{}, err
	}
	if camera.LayoutKey == "" {
		camera.LayoutKey = camera.StreamName
	}
	if camera.RecordingStreamName == "" {
		camera.RecordingStreamName = camera.StreamName
	}
	if camera.LiveStreamName == "" {
		camera.LiveStreamName = camera.StreamName
	}
	if channelIndex.Valid {
		value := int(channelIndex.Int64)
		camera.ChannelIndex = &value
	}
	camera.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	camera.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	_ = json.Unmarshal([]byte(probeJSON), &camera.LastProbeJSON)
	if camera.LastProbeJSON == nil {
		camera.LastProbeJSON = map[string]any{}
	}
	_ = json.Unmarshal([]byte(scanJSON), &camera.LastScanJSON)
	if camera.LastScanJSON == nil {
		camera.LastScanJSON = map[string]any{}
	}
	camera.RedactedURL = RedactURL(camera.URL)
	if !includeSecrets {
		camera.URL = ""
	}
	return camera, nil
}

func scanCameraStream(row scanner, includeSecrets bool) (CameraStream, error) {
	var stream CameraStream
	var createdAt, updatedAt string
	if err := row.Scan(
		&stream.ID,
		&stream.CameraID,
		&stream.Role,
		&stream.Label,
		&stream.Source,
		&stream.URL,
		&stream.Go2RTCStreamName,
		&stream.Codec,
		&stream.Width,
		&stream.Height,
		&stream.FPS,
		&stream.BitrateKbps,
		&stream.ProfileToken,
		&stream.State,
		&createdAt,
		&updatedAt,
	); err != nil {
		return CameraStream{}, err
	}
	stream.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	stream.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	stream.RedactedURL = RedactURL(stream.URL)
	if !includeSecrets {
		stream.URL = ""
	}
	return stream, nil
}

func applyRoleStreamNames(camera *Camera) {
	if camera.RecordingStreamName == "" {
		camera.RecordingStreamName = camera.StreamName
	}
	if camera.LiveStreamName == "" {
		camera.LiveStreamName = camera.StreamName
	}
	for _, stream := range camera.Streams {
		switch stream.Role {
		case CameraStreamRoleRecording:
			if stream.Go2RTCStreamName != "" {
				camera.RecordingStreamName = stream.Go2RTCStreamName
			}
		case CameraStreamRoleLive:
			if stream.Go2RTCStreamName != "" {
				camera.LiveStreamName = stream.Go2RTCStreamName
			}
		}
	}
	if camera.LiveStreamName == "" {
		camera.LiveStreamName = camera.RecordingStreamName
	}
}

func scanRecordingSegment(row scanner) (RecordingSegment, error) {
	var segment RecordingSegment
	var tempPath, finalPath, errorText sql.NullString
	var tsEnd sql.NullFloat64
	var fileSize sql.NullInt64
	if err := row.Scan(
		&segment.ID,
		&segment.CameraID,
		&segment.StreamName,
		&segment.Filename,
		&tempPath,
		&finalPath,
		&segment.TSStart,
		&tsEnd,
		&fileSize,
		&segment.Status,
		&errorText,
		&segment.CreatedAt,
		&segment.UpdatedAt,
	); err != nil {
		return RecordingSegment{}, err
	}
	if tempPath.Valid {
		segment.TempPath = tempPath.String
	}
	if finalPath.Valid {
		segment.FinalPath = finalPath.String
	}
	if tsEnd.Valid {
		value := tsEnd.Float64
		segment.TSEnd = &value
	}
	if fileSize.Valid {
		value := fileSize.Int64
		segment.FileSize = &value
	}
	if errorText.Valid {
		segment.Error = errorText.String
	}
	return segment, nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func RedactURL(rawURL string) string {
	for i := 0; i < len(rawURL); i++ {
		if rawURL[i] != '@' {
			continue
		}
		start := 0
		for j := 0; j+3 <= i; j++ {
			if rawURL[j:j+3] == "://" {
				start = j + 3
				break
			}
		}
		return rawURL[:start] + "redacted:redacted" + rawURL[i:]
	}
	return rawURL
}
