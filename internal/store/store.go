package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	URL           string         `json:"url,omitempty"`
	RedactedURL   string         `json:"redactedUrl"`
	StreamName    string         `json:"streamName"`
	State         string         `json:"state"`
	LastProbeJSON map[string]any `json:"lastProbe,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
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
			state TEXT NOT NULL,
			last_probe_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (1, datetime('now'))`,
	}
	for _, statement := range statements {
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration statement failed: %w", err)
		}
	}
	return nil
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

	_, err = d.db.ExecContext(ctx,
		`INSERT INTO cameras(name, url, stream_name, state, last_probe_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(stream_name) DO UPDATE SET
			name=excluded.name,
			url=excluded.url,
			state=excluded.state,
			last_probe_json=excluded.last_probe_json,
			updated_at=excluded.updated_at`,
		camera.Name,
		camera.URL,
		camera.StreamName,
		camera.State,
		string(encoded),
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
		`SELECT id, name, url, stream_name, state, last_probe_json, created_at, updated_at
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
	return cameras, rows.Err()
}

func (d *DB) GetCameraByStream(ctx context.Context, streamName string) (Camera, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, name, url, stream_name, state, last_probe_json, created_at, updated_at
		 FROM cameras WHERE stream_name = ?`,
		streamName,
	)
	return scanCamera(row, true)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCamera(row scanner, includeSecrets bool) (Camera, error) {
	var camera Camera
	var createdAt, updatedAt, probeJSON string
	if err := row.Scan(
		&camera.ID,
		&camera.Name,
		&camera.URL,
		&camera.StreamName,
		&camera.State,
		&probeJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Camera{}, err
	}
	camera.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	camera.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	_ = json.Unmarshal([]byte(probeJSON), &camera.LastProbeJSON)
	if camera.LastProbeJSON == nil {
		camera.LastProbeJSON = map[string]any{}
	}
	camera.RedactedURL = redactCameraURL(camera.URL)
	if !includeSecrets {
		camera.URL = ""
	}
	return camera, nil
}

func redactCameraURL(rawURL string) string {
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
