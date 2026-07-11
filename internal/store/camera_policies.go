package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrDesiredRevisionMismatch = errors.New("camera desired revision mismatch")

func (d *DB) SaveCameraConfiguration(ctx context.Context, camera Camera, expectedDesiredRevision *int64) (Camera, error) {
	if err := validateCameraOutputs(camera.Outputs); err != nil {
		return Camera{}, err
	}
	if err := validateCameraStreams(camera.Streams); err != nil {
		return Camera{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Camera{}, err
	}
	defer tx.Rollback()

	var currentRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT desired_revision FROM camera_policy_states WHERE camera_id = ?`, camera.ID).Scan(&currentRevision); err != nil {
		return Camera{}, err
	}
	if expectedDesiredRevision != nil && *expectedDesiredRevision != currentRevision {
		return Camera{}, fmt.Errorf("%w: expected %d, current %d", ErrDesiredRevisionMismatch, *expectedDesiredRevision, currentRevision)
	}
	now := time.Now().UTC()
	probe, err := json.Marshal(camera.LastProbeJSON)
	if err != nil {
		return Camera{}, err
	}
	scan, err := json.Marshal(camera.LastScanJSON)
	if err != nil {
		return Camera{}, err
	}
	controls, err := json.Marshal(normalizeControlCapabilities(camera.ControlCapabilities))
	if err != nil {
		return Camera{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE cameras SET
		name=?, url=?, layout_key=?, state=?, profile_template_id=?, manufacturer=?, model=?, profile_adapter=?,
		host=?, rtsp_port=?, http_port=?, onvif_port=?, channel_index=?, last_probe_json=?, last_scan_json=?,
		control_capabilities_json=?, updated_at=? WHERE id=?`,
		camera.Name, camera.URL, camera.LayoutKey, camera.State, camera.ProfileTemplateID, camera.Manufacturer,
		camera.Model, camera.ProfileAdapter, camera.Host, camera.RTSPPort, camera.HTTPPort, camera.ONVIFPort,
		camera.ChannelIndex, string(probe), string(scan), string(controls), now.Format(time.RFC3339Nano), camera.ID)
	if err != nil {
		return Camera{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Camera{}, sql.ErrNoRows
	}
	if err := upsertCameraStreamsTx(ctx, tx, camera.ID, camera.Streams, now); err != nil {
		return Camera{}, err
	}

	wantedKeys := map[string]bool{}
	for i := range camera.Streams {
		normalizeCameraStream(&camera.Streams[i])
		wantedKeys[camera.Streams[i].SourceKey] = true
	}
	streamIDs := map[string]int64{}
	allowedIDs := map[int64]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT id, source_key FROM camera_streams WHERE camera_id=?`, camera.ID)
	if err != nil {
		return Camera{}, err
	}
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			return Camera{}, err
		}
		if wantedKeys[key] {
			streamIDs[key] = id
			allowedIDs[id] = true
		}
	}
	if err := rows.Close(); err != nil {
		return Camera{}, err
	}
	for i := range camera.Outputs {
		output := &camera.Outputs[i]
		if output.SourceKey != "" {
			output.SourceStreamID = streamIDs[output.SourceKey]
		}
		if output.SourceStreamID == 0 || !allowedIDs[output.SourceStreamID] {
			return Camera{}, fmt.Errorf("output %s source does not belong to camera", output.Purpose)
		}
		var belongs int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM camera_streams WHERE id=? AND camera_id=?`, output.SourceStreamID, camera.ID).Scan(&belongs); err != nil || belongs != 1 {
			return Camera{}, fmt.Errorf("output %s source does not belong to camera", output.Purpose)
		}
		if output.StreamName == "" {
			return Camera{}, fmt.Errorf("output %s stream name is required", output.Purpose)
		}
		applied, err := json.Marshal(output.AppliedPolicy)
		if err != nil {
			return Camera{}, err
		}
		var verifiedAt any
		if !output.Verification.CheckedAt.IsZero() {
			verifiedAt = output.Verification.CheckedAt.Format(time.RFC3339Nano)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO camera_outputs(
			camera_id,purpose,stream_name,source_stream_id,video_mode,max_width,max_height,max_fps,audio_mode,activation,
			applied_policy_json,verified_video_codec,verified_audio_codec,verified_width,verified_height,verified_fps,
			verified_at,verification_error,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(camera_id,purpose) DO UPDATE SET
			stream_name=excluded.stream_name,source_stream_id=excluded.source_stream_id,video_mode=excluded.video_mode,
			max_width=excluded.max_width,max_height=excluded.max_height,max_fps=excluded.max_fps,audio_mode=excluded.audio_mode,
			activation=excluded.activation,applied_policy_json=excluded.applied_policy_json,
			verified_video_codec=excluded.verified_video_codec,verified_audio_codec=excluded.verified_audio_codec,
			verified_width=excluded.verified_width,verified_height=excluded.verified_height,verified_fps=excluded.verified_fps,
			verified_at=excluded.verified_at,verification_error=excluded.verification_error,updated_at=excluded.updated_at`,
			camera.ID, output.Purpose, output.StreamName, output.SourceStreamID, output.VideoMode, output.MaxWidth, output.MaxHeight,
			output.MaxFPS, output.AudioMode, output.Activation, string(applied), output.Verification.VideoCodec,
			output.Verification.AudioCodec, output.Verification.Width, output.Verification.Height, output.Verification.FPS,
			verifiedAt, redactString(output.Verification.Error), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return Camera{}, err
		}
	}
	if err := deleteMissingCameraStreamsTx(ctx, tx, camera.ID, camera.Streams); err != nil {
		return Camera{}, err
	}
	applyAt := camera.PolicyState.ApplyStateAt
	if applyAt.IsZero() {
		applyAt = now
	}
	applyState := camera.PolicyState.ApplyState
	if applyState == "" {
		applyState = CameraApplyPending
	}
	_, err = tx.ExecContext(ctx, `UPDATE camera_policy_states SET desired_revision=?, applied_revision=?, apply_state=?, apply_state_at=?, apply_error=? WHERE camera_id=?`,
		currentRevision+1, camera.PolicyState.AppliedRevision, applyState, applyAt.Format(time.RFC3339Nano), redactString(camera.PolicyState.ApplyError), camera.ID)
	if err != nil {
		return Camera{}, err
	}
	if err := tx.Commit(); err != nil {
		return Camera{}, err
	}
	return d.GetCameraByStream(ctx, camera.StreamName)
}

func validateCameraStreams(streams []CameraStream) error {
	if len(streams) == 0 {
		return fmt.Errorf("at least one camera input is required")
	}
	seen := map[string]bool{}
	for i := range streams {
		stream := streams[i]
		normalizeCameraStream(&stream)
		if stream.SourceKey == "" || stream.URL == "" || stream.Go2RTCStreamName == "" {
			return fmt.Errorf("camera input source key, URL, and stream name are required")
		}
		if seen[stream.SourceKey] {
			return fmt.Errorf("duplicate input source key %q", stream.SourceKey)
		}
		seen[stream.SourceKey] = true
	}
	return nil
}

func validateCameraOutputs(outputs []CameraOutput) error {
	if len(outputs) != 3 {
		return fmt.Errorf("exactly three camera outputs are required")
	}
	seen := map[CameraOutputPurpose]bool{}
	for _, output := range outputs {
		if output.Purpose != CameraOutputRecording && output.Purpose != CameraOutputLive && output.Purpose != CameraOutputFocus {
			return fmt.Errorf("invalid output purpose %q", output.Purpose)
		}
		if seen[output.Purpose] {
			return fmt.Errorf("duplicate output purpose %q", output.Purpose)
		}
		seen[output.Purpose] = true
		if output.VideoMode != CameraVideoAuto && output.VideoMode != CameraVideoCopy && output.VideoMode != CameraVideoH264 {
			return fmt.Errorf("invalid video mode %q", output.VideoMode)
		}
		if output.AudioMode != CameraAudioSource && output.AudioMode != CameraAudioNone && output.AudioMode != CameraAudioAAC {
			return fmt.Errorf("invalid audio mode %q", output.AudioMode)
		}
		if output.Activation != CameraActivationOnDemand && output.Activation != CameraActivationAlways {
			return fmt.Errorf("invalid activation %q", output.Activation)
		}
		if (output.MaxWidth == nil) != (output.MaxHeight == nil) {
			return fmt.Errorf("output %s width and height must both be set", output.Purpose)
		}
		if output.MaxWidth != nil && (*output.MaxWidth < 2 || *output.MaxWidth > 7680 || *output.MaxWidth%2 != 0 || *output.MaxHeight < 2 || *output.MaxHeight > 4320 || *output.MaxHeight%2 != 0) {
			return fmt.Errorf("output %s dimensions are invalid", output.Purpose)
		}
		if output.MaxFPS != nil && (*output.MaxFPS < 1 || *output.MaxFPS > 60) {
			return fmt.Errorf("output %s fps is invalid", output.Purpose)
		}
		if output.VideoMode == CameraVideoCopy && (output.MaxWidth != nil || output.MaxFPS != nil) {
			return fmt.Errorf("copy output %s cannot resize or limit fps", output.Purpose)
		}
	}
	return nil
}

func (d *DB) listCameraOutputs(ctx context.Context, cameraID int64, includeSecrets bool) ([]CameraOutput, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT o.id,o.camera_id,o.purpose,o.stream_name,o.source_stream_id,s.source_key,
		o.video_mode,o.max_width,o.max_height,o.max_fps,o.audio_mode,o.activation,o.applied_policy_json,
		o.verified_video_codec,o.verified_audio_codec,o.verified_width,o.verified_height,o.verified_fps,o.verified_at,
		o.verification_error,o.created_at,o.updated_at
		FROM camera_outputs o JOIN camera_streams s ON s.id=o.source_stream_id WHERE o.camera_id=?
		ORDER BY CASE o.purpose WHEN 'recording' THEN 0 WHEN 'live' THEN 1 ELSE 2 END`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var outputs []CameraOutput
	for rows.Next() {
		var output CameraOutput
		var maxWidth, maxHeight sql.NullInt64
		var maxFPS sql.NullFloat64
		var applied, verifiedAt, createdAt, updatedAt string
		var nullableVerifiedAt sql.NullString
		if err := rows.Scan(&output.ID, &output.CameraID, &output.Purpose, &output.StreamName, &output.SourceStreamID, &output.SourceKey,
			&output.VideoMode, &maxWidth, &maxHeight, &maxFPS, &output.AudioMode, &output.Activation, &applied,
			&output.Verification.VideoCodec, &output.Verification.AudioCodec, &output.Verification.Width, &output.Verification.Height,
			&output.Verification.FPS, &nullableVerifiedAt, &output.Verification.Error, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if maxWidth.Valid {
			v := int(maxWidth.Int64)
			output.MaxWidth = &v
		}
		if maxHeight.Valid {
			v := int(maxHeight.Int64)
			output.MaxHeight = &v
		}
		if maxFPS.Valid {
			v := maxFPS.Float64
			output.MaxFPS = &v
		}
		_ = json.Unmarshal([]byte(applied), &output.AppliedPolicy)
		verifiedAt = nullableVerifiedAt.String
		output.Verification.CheckedAt, _ = time.Parse(time.RFC3339Nano, verifiedAt)
		output.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		output.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		if !includeSecrets {
			output.Verification.Error = redactString(output.Verification.Error)
		}
		outputs = append(outputs, output)
	}
	return outputs, rows.Err()
}

func (d *DB) getCameraPolicyState(ctx context.Context, cameraID int64, includeSecrets bool) (CameraPolicyState, error) {
	var state CameraPolicyState
	var at string
	err := d.db.QueryRowContext(ctx, `SELECT camera_id,desired_revision,applied_revision,apply_state,apply_state_at,apply_error FROM camera_policy_states WHERE camera_id=?`, cameraID).
		Scan(&state.CameraID, &state.DesiredRevision, &state.AppliedRevision, &state.ApplyState, &at, &state.ApplyError)
	state.ApplyStateAt, _ = time.Parse(time.RFC3339Nano, at)
	if !includeSecrets {
		state.ApplyError = redactString(state.ApplyError)
	}
	return state, err
}
