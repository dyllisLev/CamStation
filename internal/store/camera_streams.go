package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) ReplaceCameraStreams(ctx context.Context, cameraID int64, streams []CameraStream) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	before, err := cameraInputGraphSignatureTx(ctx, tx, cameraID)
	if err != nil {
		return err
	}
	if err := upsertCameraStreamsTx(ctx, tx, cameraID, streams, now); err != nil {
		return err
	}
	for i := range streams {
		normalizeCameraStream(&streams[i])
	}
	if err := deleteMissingCameraStreamsTx(ctx, tx, cameraID, streams); err != nil {
		return err
	}
	after, err := cameraInputGraphSignatureTx(ctx, tx, cameraID)
	if err != nil {
		return err
	}
	if before != after {
		if _, err := tx.ExecContext(ctx, `UPDATE camera_policy_states SET desired_revision=desired_revision+1,apply_state='pending',apply_state_at=? WHERE camera_id=?`, now.Format(time.RFC3339Nano), cameraID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cameras SET updated_at=? WHERE id=?`, now.Format(time.RFC3339Nano), cameraID); err != nil {
		return err
	}
	return tx.Commit()
}

func cameraInputGraphSignatureTx(ctx context.Context, tx *sql.Tx, cameraID int64) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT source_key,role,source,url,go2rtc_stream_name FROM camera_streams WHERE camera_id=? ORDER BY source_key`, cameraID)
	if err != nil {
		return "", err
	}
	signature := ""
	for rows.Next() {
		var key, role, source, url, name string
		if err := rows.Scan(&key, &role, &source, &url, &name); err != nil {
			rows.Close()
			return "", err
		}
		signature += fmt.Sprintf("i:%q:%q:%q:%q:%q;", key, role, source, url, name)
	}
	if err := rows.Close(); err != nil {
		return "", err
	}
	rows, err = tx.QueryContext(ctx, `SELECT o.purpose,s.source_key FROM camera_outputs o JOIN camera_streams s ON s.id=o.source_stream_id WHERE o.camera_id=? ORDER BY o.purpose`, cameraID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var purpose, key string
		if err := rows.Scan(&purpose, &key); err != nil {
			return "", err
		}
		signature += fmt.Sprintf("o:%q:%q;", purpose, key)
	}
	return signature, rows.Err()
}

func upsertCameraStreamsTx(ctx context.Context, tx *sql.Tx, cameraID int64, streams []CameraStream, now time.Time) error {
	for _, stream := range streams {
		normalizeCameraStream(&stream)
		if stream.URL == "" || stream.Go2RTCStreamName == "" {
			continue
		}
		var detectedAt any
		if !stream.DetectedCheckedAt.IsZero() {
			detectedAt = stream.DetectedCheckedAt.Format(time.RFC3339Nano)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO camera_streams(
			camera_id,role,source_key,label,source,url,go2rtc_stream_name,codec,width,height,fps,bitrate_kbps,
			profile_token,state,detected_video_codec,detected_audio_codec,detected_profile,detected_level,
			detected_pixel_format,detected_bit_depth,detected_width,detected_height,detected_fps,detected_checked_at,
			detected_error,created_at,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(camera_id,source_key) DO UPDATE SET
			role=excluded.role,label=excluded.label,source=excluded.source,url=excluded.url,
			go2rtc_stream_name=excluded.go2rtc_stream_name,codec=excluded.codec,width=excluded.width,height=excluded.height,
			fps=excluded.fps,bitrate_kbps=excluded.bitrate_kbps,profile_token=excluded.profile_token,state=excluded.state,
			detected_video_codec=excluded.detected_video_codec,detected_audio_codec=excluded.detected_audio_codec,
			detected_profile=excluded.detected_profile,detected_level=excluded.detected_level,
			detected_pixel_format=excluded.detected_pixel_format,detected_bit_depth=excluded.detected_bit_depth,
			detected_width=excluded.detected_width,detected_height=excluded.detected_height,detected_fps=excluded.detected_fps,
			detected_checked_at=excluded.detected_checked_at,detected_error=excluded.detected_error,updated_at=excluded.updated_at`,
			cameraID, stream.Role, stream.SourceKey, stream.Label, stream.Source, stream.URL, stream.Go2RTCStreamName,
			stream.Codec, stream.Width, stream.Height, stream.FPS, stream.BitrateKbps, stream.ProfileToken, stream.State,
			stream.DetectedVideoCodec, stream.DetectedAudioCodec, stream.DetectedProfile, stream.DetectedLevel,
			stream.DetectedPixelFormat, stream.DetectedBitDepth, stream.DetectedWidth, stream.DetectedHeight, stream.DetectedFPS,
			detectedAt, redactString(stream.DetectedError), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	return nil
}

func normalizeCameraStream(stream *CameraStream) {
	if stream.Role == "" {
		stream.Role = CameraStreamRoleRecording
	}
	if stream.SourceKey == "" {
		stream.SourceKey = string(stream.Role)
	}
	if stream.State == "" {
		stream.State = "unknown"
	}
}

func deleteMissingCameraStreamsTx(ctx context.Context, tx *sql.Tx, cameraID int64, streams []CameraStream) error {
	keep := map[string]bool{}
	for i := range streams {
		normalizeCameraStream(&streams[i])
		keep[streams[i].SourceKey] = true
	}
	if len(keep) == 0 {
		return fmt.Errorf("at least one camera input is required")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,source_key FROM camera_streams WHERE camera_id=?`, cameraID)
	if err != nil {
		return err
	}
	var remove []int64
	ids := map[string]int64{}
	var first int64
	for rows.Next() {
		var id int64
		var key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			return err
		}
		if keep[key] {
			ids[key] = id
			if first == 0 {
				first = id
			}
		} else {
			remove = append(remove, id)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	recording := ids["recording"]
	if recording == 0 {
		recording = first
	}
	live := ids["live"]
	if live == 0 {
		live = recording
	}
	for _, id := range remove {
		if _, err := tx.ExecContext(ctx, `UPDATE camera_outputs SET source_stream_id=CASE WHEN purpose='live' THEN ? ELSE ? END WHERE camera_id=? AND source_stream_id=?`, live, recording, cameraID, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM camera_streams WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ListCameraStreams(ctx context.Context, cameraID int64, includeSecrets bool) ([]CameraStream, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,camera_id,role,source_key,label,source,url,go2rtc_stream_name,
		codec,width,height,fps,bitrate_kbps,profile_token,state,detected_video_codec,detected_audio_codec,
		detected_profile,detected_level,detected_pixel_format,detected_bit_depth,detected_width,detected_height,
		detected_fps,detected_checked_at,detected_error,created_at,updated_at
		FROM camera_streams WHERE camera_id=?
		ORDER BY CASE role WHEN 'recording' THEN 0 WHEN 'live' THEN 1 WHEN 'snapshot' THEN 2 ELSE 3 END,id`, cameraID)
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
