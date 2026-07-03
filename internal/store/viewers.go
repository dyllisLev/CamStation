package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrViewerNotFound = errors.New("viewer not found")

type ViewerStreamHealth struct {
	StreamName string `json:"streamName"`
	State      string `json:"state"`
	LatencyMS  int    `json:"latencyMs,omitempty"`
}

type ViewerHeartbeat struct {
	ID          string               `json:"id"`
	DisplayName string               `json:"displayName"`
	AppVersion  string               `json:"appVersion"`
	Hostname    string               `json:"hostname"`
	DeviceLabel string               `json:"deviceLabel"`
	Route       string               `json:"route"`
	Mode        string               `json:"mode"`
	Streams     []ViewerStreamHealth `json:"streams,omitempty"`
}

type ViewerUpdate struct {
	Label  string `json:"label"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type Viewer struct {
	ID              string               `json:"id"`
	DisplayName     string               `json:"displayName"`
	AppVersion      string               `json:"appVersion"`
	Hostname        string               `json:"hostname"`
	DeviceLabel     string               `json:"deviceLabel"`
	Route           string               `json:"route"`
	Mode            string               `json:"mode"`
	Label           string               `json:"label,omitempty"`
	Status          string               `json:"status"`
	Note            string               `json:"note,omitempty"`
	Streams         []ViewerStreamHealth `json:"streams,omitempty"`
	CreatedAt       time.Time            `json:"createdAt"`
	LastHeartbeatAt time.Time            `json:"lastHeartbeatAt"`
	UpdatedAt       time.Time            `json:"updatedAt"`
}

func (d *DB) UpsertViewerHeartbeat(ctx context.Context, req ViewerHeartbeat) (Viewer, error) {
	req = sanitizeHeartbeat(req)
	if req.ID == "" || req.DisplayName == "" || req.Route == "" || req.Mode == "" {
		return Viewer{}, fmt.Errorf("viewer id, displayName, route, and mode are required: %w", ErrValidation)
	}
	encoded, err := json.Marshal(req.Streams)
	if err != nil {
		return Viewer{}, fmt.Errorf("encode viewer streams: %w", err)
	}
	now := time.Now().UTC()
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO viewers(id, display_name, app_version, hostname, device_label, route, mode, streams_json, created_at, last_heartbeat_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			display_name=excluded.display_name,
			app_version=excluded.app_version,
			hostname=excluded.hostname,
			device_label=excluded.device_label,
			route=excluded.route,
			mode=excluded.mode,
			streams_json=excluded.streams_json,
			last_heartbeat_at=excluded.last_heartbeat_at,
			updated_at=excluded.updated_at`,
		req.ID, req.DisplayName, req.AppVersion, req.Hostname, req.DeviceLabel, req.Route, req.Mode,
		string(encoded), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Viewer{}, fmt.Errorf("upsert viewer heartbeat: %w", err)
	}
	return d.GetViewer(ctx, req.ID, 90*time.Second)
}

func (d *DB) ListViewers(ctx context.Context, ttl time.Duration) ([]Viewer, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, display_name, app_version, hostname, device_label, route, mode, label, status, note,
		        streams_json, created_at, last_heartbeat_at, updated_at
		 FROM viewers ORDER BY updated_at DESC, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list viewers: %w", err)
	}
	defer rows.Close()
	viewers := make([]Viewer, 0)
	for rows.Next() {
		viewer, err := scanViewer(rows, ttl)
		if err != nil {
			return nil, err
		}
		viewers = append(viewers, viewer)
	}
	return viewers, rows.Err()
}

func (d *DB) GetViewer(ctx context.Context, id string, ttl time.Duration) (Viewer, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, display_name, app_version, hostname, device_label, route, mode, label, status, note,
		        streams_json, created_at, last_heartbeat_at, updated_at
		 FROM viewers WHERE id = ?`,
		strings.TrimSpace(id),
	)
	return scanViewer(row, ttl)
}

func (d *DB) UpdateViewer(ctx context.Context, id string, req ViewerUpdate) (Viewer, error) {
	req.Label = RedactText(strings.TrimSpace(req.Label))
	req.Status = RedactText(strings.TrimSpace(req.Status))
	req.Note = RedactText(strings.TrimSpace(req.Note))
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`UPDATE viewers SET label = ?, status = ?, note = ?, updated_at = ? WHERE id = ?`,
		req.Label, req.Status, req.Note, now.Format(time.RFC3339Nano), strings.TrimSpace(id),
	)
	if err != nil {
		return Viewer{}, fmt.Errorf("update viewer: %w", err)
	}
	if err := requireViewerChanged(res); err != nil {
		return Viewer{}, err
	}
	return d.GetViewer(ctx, id, 90*time.Second)
}

func (d *DB) DeleteViewer(ctx context.Context, id string) error {
	res, err := d.db.ExecContext(ctx, `DELETE FROM viewers WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete viewer: %w", err)
	}
	return requireViewerChanged(res)
}

func sanitizeHeartbeat(req ViewerHeartbeat) ViewerHeartbeat {
	req.ID = strings.TrimSpace(req.ID)
	req.DisplayName = RedactText(strings.TrimSpace(req.DisplayName))
	req.AppVersion = RedactText(strings.TrimSpace(req.AppVersion))
	req.Hostname = RedactText(strings.TrimSpace(req.Hostname))
	req.DeviceLabel = RedactText(strings.TrimSpace(req.DeviceLabel))
	req.Route = RedactText(strings.TrimSpace(req.Route))
	req.Mode = RedactText(strings.TrimSpace(req.Mode))
	for index := range req.Streams {
		req.Streams[index].StreamName = RedactText(strings.TrimSpace(req.Streams[index].StreamName))
		req.Streams[index].State = RedactText(strings.TrimSpace(req.Streams[index].State))
	}
	return req
}

func scanViewer(row scanner, ttl time.Duration) (Viewer, error) {
	var viewer Viewer
	var streamsJSON, createdAt, heartbeatAt, updatedAt string
	if err := row.Scan(&viewer.ID, &viewer.DisplayName, &viewer.AppVersion, &viewer.Hostname,
		&viewer.DeviceLabel, &viewer.Route, &viewer.Mode, &viewer.Label, &viewer.Status,
		&viewer.Note, &streamsJSON, &createdAt, &heartbeatAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Viewer{}, ErrViewerNotFound
		}
		return Viewer{}, err
	}
	_ = json.Unmarshal([]byte(streamsJSON), &viewer.Streams)
	viewer.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	viewer.LastHeartbeatAt, _ = time.Parse(time.RFC3339Nano, heartbeatAt)
	viewer.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if viewer.Status == "" || viewer.Status == "online" {
		if time.Since(viewer.LastHeartbeatAt) <= ttl {
			viewer.Status = "online"
		} else {
			viewer.Status = "stale"
		}
	}
	return viewer, nil
}

func requireViewerChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if changed == 0 {
		return ErrViewerNotFound
	}
	return nil
}
