package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCameraStreamsPersistAndRedactWithMetadata(t *testing.T) {
	t.Parallel()

	db, err := Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name:           "염소장",
		URL:            "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0",
		StreamName:     "goat-yard",
		State:          "streaming",
		Manufacturer:   "VStarcam",
		Model:          "VeePai IP Camera",
		ProfileAdapter: "vstarcam",
		Host:           "192.168.0.55",
		RTSPPort:       10554,
		HTTPPort:       10080,
		ONVIFPort:      10080,
		LastScanJSON:   map[string]any{"adapter": "vstarcam"},
	})
	if err != nil {
		t.Fatalf("upsert camera: %v", err)
	}

	streams := []CameraStream{
		{
			Role:             CameraStreamRoleRecording,
			Label:            "PROFILE_000 main",
			Source:           "onvif-vstarcam",
			URL:              "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0",
			Go2RTCStreamName: "goat-yard-recording",
			Codec:            "h264",
			Width:            2304,
			Height:           1296,
			FPS:              12,
			BitrateKbps:      1024,
			ProfileToken:     "PROFILE_000",
			State:            "streaming",
		},
		{
			Role:             CameraStreamRoleLive,
			Label:            "PROFILE_001 sub",
			Source:           "onvif-vstarcam",
			URL:              "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_1",
			Go2RTCStreamName: "goat-yard-live",
			Codec:            "h264",
			Width:            448,
			Height:           256,
			FPS:              12,
			BitrateKbps:      512,
			ProfileToken:     "PROFILE_001",
			State:            "streaming",
		},
	}
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, streams); err != nil {
		t.Fatalf("replace streams: %v", err)
	}

	public, err := db.ListCameras(t.Context(), false)
	if err != nil {
		t.Fatalf("list public cameras: %v", err)
	}
	if len(public) != 1 {
		t.Fatalf("public cameras = %d, want 1", len(public))
	}
	got := public[0]
	if got.URL != "" || got.RedactedURL != "rtsp://redacted:redacted@192.168.0.55:10554/tcp/av0_0" {
		t.Fatalf("public urls = %q / %q", got.URL, got.RedactedURL)
	}
	if got.Manufacturer != "VStarcam" || got.Model != "VeePai IP Camera" || got.ProfileAdapter != "vstarcam" {
		t.Fatalf("metadata = %s/%s/%s", got.Manufacturer, got.Model, got.ProfileAdapter)
	}
	if got.RecordingStreamName != "goat-yard-recording" || got.LiveStreamName != "goat-yard-live" {
		t.Fatalf("role streams = %q/%q", got.RecordingStreamName, got.LiveStreamName)
	}
	if len(got.Streams) != 2 {
		t.Fatalf("streams = %d, want 2", len(got.Streams))
	}
	if got.Streams[0].URL != "" || got.Streams[0].RedactedURL == "" {
		t.Fatalf("public stream leaked secret: %#v", got.Streams[0])
	}

	private, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatalf("list private cameras: %v", err)
	}
	if private[0].Streams[0].URL == "" {
		t.Fatalf("private stream URL should be present")
	}
}

func TestRedactURLMasksQueryCredentials(t *testing.T) {
	t.Parallel()

	rawURL := "http://192.168.0.12/flv?port=1935&app=bcs&stream=channel0_main.bcs&user=admin&password=camera-pass&token=session-token"

	redacted := RedactURL(rawURL)

	if redacted == rawURL {
		t.Fatalf("redacted URL did not change")
	}
	for _, secret := range []string{"admin", "camera-pass", "session-token"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted URL %q leaked %q", redacted, secret)
		}
	}
	if !strings.Contains(redacted, "stream=channel0_main.bcs") {
		t.Fatalf("redacted URL removed non-secret stream query: %s", redacted)
	}
}

func TestDeleteCameraRemovesCameraAndRoleStreams(t *testing.T) {
	t.Parallel()

	db, err := Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name:       "삭제 테스트",
		URL:        "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0",
		StreamName: "delete-test",
		State:      "streaming",
	})
	if err != nil {
		t.Fatalf("upsert camera: %v", err)
	}
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, []CameraStream{{
		Role:             CameraStreamRoleRecording,
		Label:            "main",
		Source:           "test",
		URL:              "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0",
		Go2RTCStreamName: "delete-test-recording",
		State:            "streaming",
	}}); err != nil {
		t.Fatalf("replace streams: %v", err)
	}

	deleted, err := db.DeleteCamera(t.Context(), "delete-test")
	if err != nil {
		t.Fatalf("delete camera: %v", err)
	}
	if deleted.StreamName != "delete-test" || len(deleted.Streams) != 1 {
		t.Fatalf("deleted camera = %#v", deleted)
	}

	cameras, err := db.ListCameras(t.Context(), false)
	if err != nil {
		t.Fatalf("list cameras: %v", err)
	}
	if len(cameras) != 0 {
		t.Fatalf("cameras after delete = %d, want 0", len(cameras))
	}
}
