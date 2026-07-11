package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCameraPolicyFreshDefaults(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name: "yard", URL: "rtsp://admin:secret@10.0.0.2/main", StreamName: "yard", State: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(camera.Streams) != 1 || camera.Streams[0].SourceKey != "recording" || camera.Streams[0].URL == "" {
		t.Fatalf("default inputs = %#v", camera.Streams)
	}
	assertDefaultOutputs(t, camera)
	if camera.PolicyState.DesiredRevision != 1 || camera.PolicyState.AppliedRevision != 0 || camera.PolicyState.ApplyState != CameraApplyPending {
		t.Fatalf("policy state = %#v", camera.PolicyState)
	}
}

func TestCameraPolicyLegacyMigrationIsIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.db.ExecContext(t.Context(), `CREATE TABLE cameras (
		id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, url TEXT NOT NULL,
		stream_name TEXT NOT NULL UNIQUE, recording_stream_name TEXT NOT NULL DEFAULT '',
		live_stream_name TEXT NOT NULL DEFAULT '', state TEXT NOT NULL,
		last_probe_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.ExecContext(t.Context(), `INSERT INTO cameras
		(name,url,stream_name,recording_stream_name,live_stream_name,state,created_at,updated_at)
		VALUES ('legacy','rtsp://user:pass@10.0.0.3/main','legacy','legacy-rec','legacy-live','unknown',?,?)`,
		time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	first, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(first[0].Streams) != 1 || first[0].Streams[0].URL == "" {
		t.Fatalf("legacy camera = %#v", first)
	}
	assertDefaultOutputs(t, first[0])
	if first[0].Outputs[0].StreamName != "legacy-rec" || first[0].Outputs[1].StreamName != "legacy-live" || first[0].Outputs[2].StreamName != "legacy-focus" {
		t.Fatalf("legacy output names = %#v", first[0].Outputs)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	second, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("rerun changed migration\nfirst: %#v\nsecond: %#v", first, second)
	}
}

func TestReplaceCameraStreamsPreservesIDsAndOutputPolicies(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "stable")
	initial := []CameraStream{
		{SourceKey: "recording", Role: CameraStreamRoleRecording, Label: "main", Source: "onvif", URL: "rtsp://u:p@host/main", Go2RTCStreamName: "stable-rec"},
		{SourceKey: "live", Role: CameraStreamRoleLive, Label: "sub", Source: "onvif", URL: "rtsp://u:p@host/sub", Go2RTCStreamName: "stable-live"},
	}
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, initial); err != nil {
		t.Fatal(err)
	}
	camera = mustGetCamera(t, db, "stable")
	ids := map[string]int64{}
	for _, stream := range camera.Streams {
		ids[stream.SourceKey] = stream.ID
	}
	camera.Outputs[1].VideoMode = CameraVideoH264
	camera.Outputs[1].MaxWidth, camera.Outputs[1].MaxHeight = intPtr(1280), intPtr(720)
	camera.Outputs[1].MaxFPS = floatPtr(15)
	updated, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(camera.PolicyState.DesiredRevision))
	if err != nil {
		t.Fatal(err)
	}
	policy := updated.Outputs[1]
	initial[0].Label, initial[0].DetectedVideoCodec = "main updated", "h265"
	initial[1].Label, initial[1].DetectedVideoCodec = "sub updated", "h264"
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, initial); err != nil {
		t.Fatal(err)
	}
	got := mustGetCamera(t, db, "stable")
	for _, stream := range got.Streams {
		if stream.ID != ids[stream.SourceKey] {
			t.Fatalf("%s id = %d, want %d", stream.SourceKey, stream.ID, ids[stream.SourceKey])
		}
	}
	if !reflect.DeepEqual(got.Outputs[1], policy) {
		t.Fatalf("live policy changed\n got %#v\nwant %#v", got.Outputs[1], policy)
	}
}

func TestSaveCameraConfigurationRejectsInvalidPoliciesAtomically(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Camera, *DB)
	}{
		{"invalid video enum", func(c *Camera, _ *DB) { c.Outputs[0].VideoMode = "bogus" }},
		{"missing purpose", func(c *Camera, _ *DB) { c.Outputs = c.Outputs[:2] }},
		{"duplicate purpose", func(c *Camera, _ *DB) { c.Outputs[2].Purpose = CameraOutputLive }},
		{"cross camera source", func(c *Camera, db *DB) {
			other := mustCamera(t, db, "other")
			c.Outputs[0].SourceStreamID = other.Streams[0].ID
			c.Outputs[0].SourceKey = ""
		}},
		{"copy resize", func(c *Camera, _ *DB) { c.Outputs[0].MaxWidth, c.Outputs[0].MaxHeight = intPtr(1280), intPtr(720) }},
		{"half resolution", func(c *Camera, _ *DB) { c.Outputs[1].MaxWidth = intPtr(1280) }},
		{"odd dimensions", func(c *Camera, _ *DB) { c.Outputs[1].MaxWidth, c.Outputs[1].MaxHeight = intPtr(1279), intPtr(720) }},
		{"out of range dimensions", func(c *Camera, _ *DB) { c.Outputs[1].MaxWidth, c.Outputs[1].MaxHeight = intPtr(7682), intPtr(4320) }},
		{"invalid fps", func(c *Camera, _ *DB) { c.Outputs[1].MaxFPS = floatPtr(61) }},
		{"invalid audio enum", func(c *Camera, _ *DB) { c.Outputs[1].AudioMode = "bogus" }},
		{"invalid activation enum", func(c *Camera, _ *DB) { c.Outputs[1].Activation = "bogus" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openMigratedStore(t)
			before := mustCamera(t, db, "atomic")
			candidate := before
			candidate.Name = "must roll back"
			candidate.Streams = append([]CameraStream(nil), before.Streams...)
			candidate.Outputs = append([]CameraOutput(nil), before.Outputs...)
			tt.edit(&candidate, db)
			_, err := db.SaveCameraConfiguration(t.Context(), candidate, int64Ptr(before.PolicyState.DesiredRevision))
			if err == nil {
				t.Fatal("expected validation error")
			}
			after := mustGetCamera(t, db, "atomic")
			if after.Name != before.Name || after.PolicyState.DesiredRevision != before.PolicyState.DesiredRevision || !reflect.DeepEqual(after.Outputs, before.Outputs) {
				t.Fatalf("partial change persisted: before=%#v after=%#v", before, after)
			}
		})
	}
}

func TestSaveCameraConfigurationRevisionMismatch(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "revision")
	_, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(camera.PolicyState.DesiredRevision+1))
	if !errors.Is(err, ErrDesiredRevisionMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestDeleteCameraCascadesPolicyRows(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "cascade")
	if _, err := db.DeleteCamera(t.Context(), camera.StreamName); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"camera_streams", "camera_outputs", "camera_policy_states"} {
		var count int
		if err := db.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table+" WHERE camera_id = ?", camera.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s rows = %d", table, count)
		}
	}
}

func TestCameraPolicyPublicPrivateReadsRedactSecretsAndKeepMetadata(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "redaction")
	camera.Streams[0].DetectedVideoCodec = "h264"
	camera.Streams[0].DetectedAudioCodec = "aac"
	camera.Streams[0].DetectedProfile = "high"
	camera.Streams[0].DetectedLevel = "4.1"
	camera.Streams[0].DetectedPixelFormat = "yuv420p"
	camera.Streams[0].DetectedBitDepth = 8
	camera.Streams[0].DetectedWidth, camera.Streams[0].DetectedHeight = 1920, 1080
	camera.Streams[0].DetectedFPS = 29.97
	camera.Streams[0].DetectedCheckedAt = time.Now().UTC().Truncate(time.Microsecond)
	camera.Streams[0].DetectedError = "probe rtsp://admin:secret@10.0.0.2/main failed"
	camera.Outputs[0].AppliedPolicy = CameraOutputPolicySnapshot{SourceStreamID: camera.Streams[0].ID, VideoMode: CameraVideoCopy, AudioMode: CameraAudioSource, Activation: CameraActivationOnDemand}
	camera.Outputs[0].Verification = CameraOutputVerification{VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 29.97, CheckedAt: time.Now().UTC().Truncate(time.Microsecond), Error: "verify rtsp://admin:secret@10.0.0.2/out failed"}
	camera.PolicyState.ApplyState = CameraApplyFailed
	camera.PolicyState.ApplyError = "apply rtsp://admin:secret@10.0.0.2/out failed"
	if _, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(camera.PolicyState.DesiredRevision)); err != nil {
		t.Fatal(err)
	}
	public, err := db.ListCameras(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	private, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if public[0].URL != "" || public[0].Streams[0].URL != "" || !strings.Contains(public[0].Streams[0].RedactedURL, "redacted:redacted") {
		t.Fatalf("public urls = %#v", public[0])
	}
	for _, value := range []string{public[0].Streams[0].DetectedError, public[0].Outputs[0].Verification.Error, public[0].PolicyState.ApplyError} {
		if strings.Contains(value, "admin") || strings.Contains(value, "secret") {
			t.Fatalf("public error leaked: %q", value)
		}
	}
	if private[0].URL == "" || private[0].Streams[0].URL == "" || private[0].Streams[0].DetectedVideoCodec != "h264" || private[0].Outputs[0].AppliedPolicy.VideoMode != CameraVideoCopy {
		t.Fatalf("private read lost data: %#v", private[0])
	}
}

func assertDefaultOutputs(t *testing.T, camera Camera) {
	t.Helper()
	if len(camera.Outputs) != 3 {
		t.Fatalf("outputs = %d", len(camera.Outputs))
	}
	recording, live, focus := camera.Outputs[0], camera.Outputs[1], camera.Outputs[2]
	if recording.Purpose != CameraOutputRecording || recording.SourceKey != "recording" || recording.VideoMode != CameraVideoCopy || recording.AudioMode != CameraAudioSource || recording.Activation != CameraActivationOnDemand {
		t.Fatalf("recording default = %#v", recording)
	}
	if live.Purpose != CameraOutputLive || live.SourceKey == "" || live.VideoMode != CameraVideoAuto || live.AudioMode != CameraAudioNone || live.Activation != CameraActivationOnDemand {
		t.Fatalf("live default = %#v", live)
	}
	if focus.Purpose != CameraOutputFocus || focus.SourceKey != "recording" || focus.VideoMode != CameraVideoAuto || focus.MaxWidth == nil || *focus.MaxWidth != 1920 || focus.MaxHeight == nil || *focus.MaxHeight != 1080 || focus.AudioMode != CameraAudioNone || focus.Activation != CameraActivationOnDemand {
		t.Fatalf("focus default = %#v", focus)
	}
}

func mustCamera(t *testing.T, db *DB, streamName string) Camera {
	t.Helper()
	camera, err := db.UpsertCamera(t.Context(), Camera{Name: streamName, URL: "rtsp://admin:secret@10.0.0.2/main", StreamName: streamName, State: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	return camera
}

func mustGetCamera(t *testing.T, db *DB, streamName string) Camera {
	t.Helper()
	cameras, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, camera := range cameras {
		if camera.StreamName == streamName {
			return camera
		}
	}
	t.Fatalf("camera %q not found", streamName)
	return Camera{}
}

func intPtr(v int) *int           { return &v }
func int64Ptr(v int64) *int64     { return &v }
func floatPtr(v float64) *float64 { return &v }
