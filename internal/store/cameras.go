package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

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
	encodedControlCapabilities, err := json.Marshal(normalizeControlCapabilities(camera.ControlCapabilities))
	if err != nil {
		return Camera{}, err
	}
	var channelIndex any
	if camera.ChannelIndex != nil {
		channelIndex = *camera.ChannelIndex
	}
	var profileTemplateID any
	if camera.ProfileTemplateID != nil {
		profileTemplateID = *camera.ProfileTemplateID
	}

	_, err = d.db.ExecContext(ctx,
		`INSERT INTO cameras(
			name, url, stream_name, layout_key, recording_stream_name, live_stream_name, state,
			profile_template_id, manufacturer, model, profile_adapter, host, rtsp_port, http_port, onvif_port, channel_index,
			last_probe_json, last_scan_json, control_capabilities_json, created_at, updated_at
		 )
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(stream_name) DO UPDATE SET
			name=excluded.name,
			url=excluded.url,
			layout_key=excluded.layout_key,
			recording_stream_name=excluded.recording_stream_name,
			live_stream_name=excluded.live_stream_name,
			state=excluded.state,
			profile_template_id=excluded.profile_template_id,
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
			control_capabilities_json=excluded.control_capabilities_json,
			updated_at=excluded.updated_at`,
		camera.Name,
		camera.URL,
		camera.StreamName,
		camera.LayoutKey,
		camera.RecordingStreamName,
		camera.LiveStreamName,
		camera.State,
		profileTemplateID,
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
		string(encodedControlCapabilities),
		camera.CreatedAt.Format(time.RFC3339Nano),
		camera.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Camera{}, err
	}
	if err := d.ensureCameraPolicyDefaults(ctx); err != nil {
		return Camera{}, err
	}
	return d.GetCameraByStream(ctx, camera.StreamName)
}

func (d *DB) ListCameras(ctx context.Context, includeSecrets bool) ([]Camera, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, name, url, stream_name, layout_key, recording_stream_name, live_stream_name, state,
		        profile_template_id, manufacturer, model, profile_adapter, host, rtsp_port, http_port, onvif_port, channel_index,
		        last_probe_json, last_scan_json, control_capabilities_json, created_at, updated_at
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
		cameras[i].Outputs, err = d.listCameraOutputs(ctx, cameras[i].ID, includeSecrets)
		if err != nil {
			return nil, err
		}
		cameras[i].PolicyState, err = d.getCameraPolicyState(ctx, cameras[i].ID, includeSecrets)
		if err != nil {
			return nil, err
		}
		applyRoleStreamNames(&cameras[i])
	}
	return cameras, nil
}

func (d *DB) GetCameraByStream(ctx context.Context, streamName string) (Camera, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, name, url, stream_name, layout_key, recording_stream_name, live_stream_name, state,
		        profile_template_id, manufacturer, model, profile_adapter, host, rtsp_port, http_port, onvif_port, channel_index,
		        last_probe_json, last_scan_json, control_capabilities_json, created_at, updated_at
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
	camera.Outputs, err = d.listCameraOutputs(ctx, camera.ID, true)
	if err != nil {
		return Camera{}, err
	}
	camera.PolicyState, err = d.getCameraPolicyState(ctx, camera.ID, true)
	if err != nil {
		return Camera{}, err
	}
	applyRoleStreamNames(&camera)
	return camera, nil
}

func (d *DB) UpdateCameraControlCapabilities(ctx context.Context, streamName string, capabilities CameraControlCapabilities) error {
	payload, err := json.Marshal(normalizeControlCapabilities(capabilities))
	if err != nil {
		return err
	}
	result, err := d.db.ExecContext(ctx,
		`UPDATE cameras SET control_capabilities_json = ?, updated_at = ? WHERE stream_name = ?`,
		string(payload), time.Now().UTC().Format(time.RFC3339Nano), streamName,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
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
