package store

import (
	"context"
	"time"
)

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
