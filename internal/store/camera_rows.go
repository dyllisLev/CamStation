package store

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

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

func RedactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("redacted", "redacted")
	}
	query := parsed.Query()
	for key := range query {
		if isCredentialQueryKey(key) {
			query.Set(key, "redacted")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func isCredentialQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "user", "username", "password", "passwd", "pwd", "token":
		return true
	default:
		return false
	}
}
