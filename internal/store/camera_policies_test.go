package store

import (
	"encoding/json"
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
	saved, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(camera.PolicyState.DesiredRevision))
	if err != nil {
		t.Fatal(err)
	}
	results := make([]CameraOutputApplyResult, 0, 3)
	for _, output := range saved.Outputs {
		result := CameraOutputApplyResult{Purpose: output.Purpose, Policy: CameraOutputPolicySnapshot{SourceStreamID: output.SourceStreamID, VideoMode: output.VideoMode, AudioMode: output.AudioMode, Activation: output.Activation}}
		if output.Purpose == CameraOutputRecording {
			result.Verification = CameraOutputVerification{VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080, FPS: 29.97, Transcoding: true, CheckedAt: time.Now().UTC().Truncate(time.Microsecond), Error: "verify rtsp://admin:secret@10.0.0.2/out failed"}
		}
		results = append(results, result)
	}
	if err := db.MarkCameraPolicyApplied(t.Context(), saved.ID, saved.PolicyState.DesiredRevision, results); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkCameraPolicyFailed(t.Context(), saved.ID, saved.PolicyState.DesiredRevision, "apply rtsp://admin:secret@10.0.0.2/out failed"); err != nil {
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
	if private[0].URL == "" || private[0].Streams[0].URL == "" || private[0].Streams[0].DetectedVideoCodec != "h264" || private[0].Outputs[0].AppliedPolicy.VideoMode != CameraVideoCopy || !private[0].Outputs[0].Verification.Transcoding {
		t.Fatalf("private read lost data: %#v", private[0])
	}
}

func TestDesiredSavePreservesCoordinatorOwnedAppliedState(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "applied-owner")
	results := make([]CameraOutputApplyResult, 0, 3)
	for _, output := range camera.Outputs {
		results = append(results, CameraOutputApplyResult{
			Purpose: output.Purpose,
			Policy: CameraOutputPolicySnapshot{
				SourceStreamID: output.SourceStreamID, VideoMode: output.VideoMode,
				AudioMode: output.AudioMode, Activation: output.Activation,
			},
			Verification: CameraOutputVerification{VideoCodec: "h264", Width: 1920, Height: 1080, FPS: 15},
		})
	}
	if err := db.MarkCameraPolicyApplied(t.Context(), camera.ID, camera.PolicyState.DesiredRevision, results); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkCameraPolicyFailed(t.Context(), camera.ID, camera.PolicyState.DesiredRevision, "failed rtsp://admin:secret@10.0.0.2/out"); err != nil {
		t.Fatal(err)
	}
	camera = mustGetCamera(t, db, camera.StreamName)
	wantApplied := camera.Outputs[0].AppliedPolicy
	wantVerification := camera.Outputs[0].Verification
	wantAppliedRevision := camera.PolicyState.AppliedRevision
	wantError := camera.PolicyState.ApplyError
	camera.Outputs[0].AppliedPolicy = CameraOutputPolicySnapshot{VideoMode: CameraVideoH264}
	camera.Outputs[0].Verification = CameraOutputVerification{VideoCodec: "stale"}
	camera.PolicyState.AppliedRevision = 0
	camera.PolicyState.ApplyError = "stale caller overwrite"

	saved, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(camera.PolicyState.DesiredRevision))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Outputs[0].AppliedPolicy, wantApplied) || !reflect.DeepEqual(saved.Outputs[0].Verification, wantVerification) {
		t.Fatalf("desired save overwrote applied output: %#v", saved.Outputs[0])
	}
	if saved.PolicyState.AppliedRevision != wantAppliedRevision || saved.PolicyState.ApplyError != wantError {
		t.Fatalf("desired save overwrote applied state: %#v", saved.PolicyState)
	}
	if saved.PolicyState.ApplyState != CameraApplyPending || saved.PolicyState.DesiredRevision != camera.PolicyState.DesiredRevision+1 {
		t.Fatalf("desired save state = %#v", saved.PolicyState)
	}
}

func TestSaveCameraConfigurationCreatesIDLessCameraAtomically(t *testing.T) {
	db := openMigratedStore(t)
	registration := Camera{
		Name: "registered", URL: "rtsp://admin:secret@10.0.0.8/main", StreamName: "registered", State: "unknown",
		Streams: []CameraStream{
			{SourceKey: "recording", Role: CameraStreamRoleRecording, URL: "rtsp://admin:secret@10.0.0.8/main", Go2RTCStreamName: "producer-main"},
			{SourceKey: "live", Role: CameraStreamRoleLive, URL: "rtsp://admin:secret@10.0.0.8/sub", Go2RTCStreamName: "producer-sub", DetectedVideoCodec: "h264"},
		},
		Outputs: desiredOutputs("recording", "live"),
	}
	created, err := db.SaveCameraConfiguration(t.Context(), registration, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.PolicyState.DesiredRevision != 1 || created.Streams[1].DetectedVideoCodec != "h264" {
		t.Fatalf("created camera = %#v", created)
	}
	if created.RecordingStreamName != "registered-recording" || created.LiveStreamName != "registered-live" || created.FocusStreamName != "registered-focus" {
		t.Fatalf("created output names = %q/%q/%q", created.RecordingStreamName, created.LiveStreamName, created.FocusStreamName)
	}

	broken := registration
	broken.StreamName = "rolled-back"
	broken.Name = "rolled-back"
	broken.Outputs = desiredOutputs("missing", "live")
	if _, err := db.SaveCameraConfiguration(t.Context(), broken, nil); err == nil {
		t.Fatal("expected unresolved source failure")
	}
	cameras, err := db.ListCameras(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, camera := range cameras {
		if camera.StreamName == "rolled-back" {
			t.Fatal("failed registration left a partial camera row")
		}
	}
}

func TestCameraStreamNamesComeFromImmutableOutputRows(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "names")
	if camera.RecordingStreamName != "names-recording" || camera.LiveStreamName != "names-live" || camera.FocusStreamName != "names-focus" {
		t.Fatalf("default names = %q/%q/%q", camera.RecordingStreamName, camera.LiveStreamName, camera.FocusStreamName)
	}
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, []CameraStream{
		{SourceKey: "recording", Role: CameraStreamRoleRecording, URL: "rtsp://u:p@host/main", Go2RTCStreamName: "producer-recording"},
		{SourceKey: "live", Role: CameraStreamRoleLive, URL: "rtsp://u:p@host/sub", Go2RTCStreamName: "producer-live"},
	}); err != nil {
		t.Fatal(err)
	}
	camera = mustGetCamera(t, db, "names")
	for i := range camera.Outputs {
		camera.Outputs[i].StreamName = "caller-controlled-" + string(camera.Outputs[i].Purpose)
	}
	saved, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(camera.PolicyState.DesiredRevision))
	if err != nil {
		t.Fatal(err)
	}
	if saved.RecordingStreamName != "names-recording" || saved.LiveStreamName != "names-live" || saved.FocusStreamName != "names-focus" {
		t.Fatalf("mutable/producer names escaped: %#v", saved)
	}
}

func TestReplaceCameraStreamsAdvancesRevisionOnlyForGraphChanges(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "replace-revision")
	start := camera.PolicyState.DesiredRevision
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, camera.Streams); err != nil {
		t.Fatal(err)
	}
	if got := mustGetCamera(t, db, camera.StreamName).PolicyState.DesiredRevision; got != start {
		t.Fatalf("no-op revision = %d, want %d", got, start)
	}
	camera.Streams[0].URL = "rtsp://admin:secret@10.0.0.2/changed"
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, camera.Streams); err != nil {
		t.Fatal(err)
	}
	changed := mustGetCamera(t, db, camera.StreamName)
	if changed.PolicyState.DesiredRevision != start+1 || changed.PolicyState.ApplyState != CameraApplyPending {
		t.Fatalf("changed state = %#v", changed.PolicyState)
	}
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, changed.Streams); err != nil {
		t.Fatal(err)
	}
	if got := mustGetCamera(t, db, camera.StreamName).PolicyState.DesiredRevision; got != start+1 {
		t.Fatalf("second no-op revision = %d, want %d", got, start+1)
	}
	changed.Streams = append(changed.Streams, CameraStream{SourceKey: "live", Role: CameraStreamRoleLive, URL: "rtsp://admin:secret@10.0.0.2/sub", Go2RTCStreamName: "producer-live"})
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, changed.Streams); err != nil {
		t.Fatal(err)
	}
	if got := mustGetCamera(t, db, camera.StreamName).PolicyState.DesiredRevision; got != start+2 {
		t.Fatalf("source-FK revision = %d, want %d", got, start+2)
	}
}

func TestCoordinatorResultsRespectNewerDesiredRevision(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "coordinator-race")
	oldRevision := camera.PolicyState.DesiredRevision
	results := applyResults(camera)
	camera.Outputs[1].VideoMode = CameraVideoH264
	newer, err := db.SaveCameraConfiguration(t.Context(), camera, int64Ptr(oldRevision))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkCameraPolicyApplied(t.Context(), camera.ID, oldRevision, results); err != nil {
		t.Fatalf("accept older completed apply: %v", err)
	}
	got := mustGetCamera(t, db, camera.StreamName)
	if got.PolicyState.AppliedRevision != oldRevision || got.PolicyState.DesiredRevision != newer.PolicyState.DesiredRevision || got.PolicyState.ApplyState != CameraApplyPending {
		t.Fatalf("older completion state = %#v", got.PolicyState)
	}
	if got.Outputs[0].AppliedPolicy.SourceKey != "recording" {
		t.Fatalf("older applied snapshot = %#v", got.Outputs[0].AppliedPolicy)
	}
	if err := db.MarkCameraPolicyFailed(t.Context(), camera.ID, oldRevision, "old apply failed"); err != nil {
		t.Fatalf("stale failure: %v", err)
	}
	if state := mustGetCamera(t, db, camera.StreamName).PolicyState; state.ApplyState != CameraApplyPending {
		t.Fatalf("stale failure poisoned newer desired state: %#v", state)
	}
	if err := db.MarkCameraPolicyApplied(t.Context(), camera.ID, newer.PolicyState.DesiredRevision+1, applyResults(newer)); !errors.Is(err, ErrDesiredRevisionMismatch) {
		t.Fatalf("future applied revision error = %v", err)
	}
	if err := db.MarkCameraPolicyApplied(t.Context(), camera.ID, oldRevision-1, results); !errors.Is(err, ErrAppliedRevisionRegression) {
		t.Fatalf("regressed applied revision error = %v", err)
	}
}

func TestMarkCameraPoliciesAppliedRollsBackWholeSnapshot(t *testing.T) {
	db := openMigratedStore(t)
	first := mustCamera(t, db, "bulk-first")
	second := mustCamera(t, db, "bulk-second")
	err := db.MarkCameraPoliciesApplied(t.Context(), []CameraPolicyApplySnapshot{
		{CameraID: first.ID, Revision: first.PolicyState.DesiredRevision, Results: applyResults(first)},
		{CameraID: second.ID + 1000, Revision: second.PolicyState.DesiredRevision, Results: applyResults(second)},
	})
	if err == nil {
		t.Fatal("expected second snapshot to fail")
	}
	got := mustGetCamera(t, db, first.StreamName)
	if got.PolicyState.AppliedRevision != 0 || got.Outputs[0].AppliedPolicy.SourceKey != "" {
		t.Fatalf("first snapshot advanced despite rollback: %#v %#v", got.PolicyState, got.Outputs[0].AppliedPolicy)
	}
}

func TestAppliedSnapshotKeepsSourceKeyAfterInputDeletion(t *testing.T) {
	db := openMigratedStore(t)
	created, err := db.SaveCameraConfiguration(t.Context(), Camera{
		Name: "source-key", URL: "rtsp://u:p@host/main", StreamName: "source-key", State: "unknown",
		Streams: []CameraStream{
			{SourceKey: "recording", Role: CameraStreamRoleRecording, URL: "rtsp://u:p@host/main", Go2RTCStreamName: "producer-main"},
			{SourceKey: "live", Role: CameraStreamRoleLive, URL: "rtsp://u:p@host/sub", Go2RTCStreamName: "producer-live"},
		},
		Outputs: desiredOutputs("recording", "live"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkCameraPolicyApplied(t.Context(), created.ID, created.PolicyState.DesiredRevision, applyResults(created)); err != nil {
		t.Fatal(err)
	}
	created.Streams = created.Streams[:1]
	for i := range created.Outputs {
		created.Outputs[i].SourceKey = "recording"
		created.Outputs[i].SourceStreamID = created.Streams[0].ID
	}
	updated, err := db.SaveCameraConfiguration(t.Context(), created, int64Ptr(created.PolicyState.DesiredRevision))
	if err != nil {
		t.Fatal(err)
	}
	live := updated.Outputs[1].AppliedPolicy
	if live.SourceKey != "live" {
		t.Fatalf("applied source identity = %#v", live)
	}
	encoded, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"sourceKey":"live"`) || strings.Contains(string(encoded), "sourceStreamId") {
		t.Fatalf("public snapshot JSON = %s", encoded)
	}
}

func TestReplaceCameraStreamsDoesNotRetargetLivePolicy(t *testing.T) {
	db := openMigratedStore(t)
	camera := mustCamera(t, db, "no-retarget")
	if camera.Outputs[1].SourceKey != "recording" {
		t.Fatalf("initial live source = %q", camera.Outputs[1].SourceKey)
	}
	streams := append(camera.Streams, CameraStream{SourceKey: "live", Role: CameraStreamRoleLive, URL: "rtsp://u:p@host/sub", Go2RTCStreamName: "producer-live"})
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, streams); err != nil {
		t.Fatal(err)
	}
	got := mustGetCamera(t, db, camera.StreamName)
	if got.Outputs[1].SourceKey != "recording" {
		t.Fatalf("input discovery retargeted live policy to %q", got.Outputs[1].SourceKey)
	}
}

func applyResults(camera Camera) []CameraOutputApplyResult {
	results := make([]CameraOutputApplyResult, 0, len(camera.Outputs))
	for _, output := range camera.Outputs {
		results = append(results, CameraOutputApplyResult{Purpose: output.Purpose, Policy: CameraOutputPolicySnapshot{
			SourceKey: output.SourceKey, SourceStreamID: output.SourceStreamID, VideoMode: output.VideoMode,
			MaxWidth: output.MaxWidth, MaxHeight: output.MaxHeight, MaxFPS: output.MaxFPS,
			AudioMode: output.AudioMode, Activation: output.Activation,
		}})
	}
	return results
}

func desiredOutputs(recordingKey, liveKey string) []CameraOutput {
	return []CameraOutput{
		{Purpose: CameraOutputRecording, SourceKey: recordingKey, VideoMode: CameraVideoCopy, AudioMode: CameraAudioSource, Activation: CameraActivationOnDemand},
		{Purpose: CameraOutputLive, SourceKey: liveKey, VideoMode: CameraVideoAuto, AudioMode: CameraAudioNone, Activation: CameraActivationOnDemand},
		{Purpose: CameraOutputFocus, SourceKey: recordingKey, VideoMode: CameraVideoAuto, MaxWidth: intPtr(1920), MaxHeight: intPtr(1080), AudioMode: CameraAudioNone, Activation: CameraActivationOnDemand},
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
