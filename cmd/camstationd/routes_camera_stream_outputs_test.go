package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"camstation/internal/camera"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type policyApplyFunc func(context.Context) stream.PolicyApplyResult

func (f policyApplyFunc) Apply(ctx context.Context) stream.PolicyApplyResult { return f(ctx) }

type recordingPolicyProber struct {
	urls []string
}

func (p *recordingPolicyProber) Probe(_ context.Context, rawURL string, _ time.Duration) (camera.ProbeResult, error) {
	p.urls = append(p.urls, rawURL)
	result := camera.ProbeResult{Reachable: true, CheckedAt: time.Now().UTC()}
	if strings.HasPrefix(rawURL, "http://") {
		result.Format = "flv"
		result.Streams = []camera.Stream{{Type: "video", Codec: "hevc", Profile: "Main 10", Level: "5.1", PixelFormat: "yuv420p10le", BitDepth: 10, Width: 3840, Height: 2160, FPS: 20}, {Type: "audio", Codec: "aac"}}
	} else {
		result.Streams = []camera.Stream{{Type: "video", Codec: "h264", Profile: "High", PixelFormat: "yuv420p", BitDepth: 8, Width: 1920, Height: 1080, FPS: 20}}
	}
	return result, nil
}

func TestPublicCameraStreamPolicyDTOHasStableKeysAndNoPrivateInputIdentity(t *testing.T) {
	now := time.Date(2026, 7, 11, 1, 2, 3, 0, time.UTC)
	camera := store.Camera{
		ID: 91, Name: "Station", StreamName: "station", State: "streaming",
		RecordingStreamName: "station-recording", LiveStreamName: "station-live", FocusStreamName: "station-focus",
		Streams: []store.CameraStream{{
			ID: 41, CameraID: 91, SourceKey: "recording", Role: store.CameraStreamRoleRecording,
			Label: "main", Source: "onvif", URL: "rtsp://admin:secret@10.0.0.5/main",
			RedactedURL: "rtsp://redacted:redacted@10.0.0.5/main", Go2RTCStreamName: "private-source-91-recording",
			Codec: "h264", Width: 1920, Height: 1080, FPS: 20,
			DetectedVideoCodec: "hevc", DetectedAudioCodec: "aac", DetectedProfile: "Main 10",
			DetectedLevel: "5.1", DetectedPixelFormat: "yuv420p10le", DetectedBitDepth: 10,
			DetectedWidth: 3840, DetectedHeight: 2160, DetectedFPS: 20, DetectedCheckedAt: now,
		}},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, StreamName: "station-recording", SourceStreamID: 41, SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand, AppliedPolicy: store.CameraOutputPolicySnapshot{SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand}, Verification: store.CameraOutputVerification{VideoCodec: "hevc", AudioCodec: "aac", Width: 3840, Height: 2160, FPS: 20, CheckedAt: now}},
			{Purpose: store.CameraOutputLive, StreamName: "station-live", SourceStreamID: 41, SourceKey: "recording", VideoMode: store.CameraVideoH264, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputFocus, StreamName: "station-focus", SourceStreamID: 41, SourceKey: "recording", VideoMode: store.CameraVideoH264, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		},
		PolicyState: store.CameraPolicyState{DesiredRevision: 3, AppliedRevision: 2, ApplyState: store.CameraApplyPending, ApplyStateAt: now, AppliedAt: now.Add(-time.Hour)},
	}
	status := stream.Status{Streams: map[string]stream.StreamRuntime{"station-recording": {State: "running", ProducerCount: 1, ConsumerCount: 2, ViewerCount: 1}}}

	public := publicCameraFromStore(camera, status)
	if len(public.Streams) != 1 || public.Streams[0].SourceKey != "recording" {
		t.Fatalf("public sources = %#v", public.Streams)
	}
	if len(public.StreamOutputs) != 3 || public.StreamOutputs[0].Desired.SourceKey != "recording" {
		t.Fatalf("public outputs = %#v", public.StreamOutputs)
	}
	if public.StreamOutputs[0].Runtime.ProducerCount != 1 || public.StreamOutputs[0].Runtime.ViewerCount != 1 {
		t.Fatalf("runtime = %#v", public.StreamOutputs[0].Runtime)
	}
	if public.StreamApplyState.DesiredRevision != 3 || public.StreamApplyState.State != store.CameraApplyPending {
		t.Fatalf("apply state = %#v", public.StreamApplyState)
	}
	if public.StreamApplyState.AppliedAt != now.Add(-time.Hour).Format(time.RFC3339) {
		t.Fatalf("appliedAt = %q", public.StreamApplyState.AppliedAt)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"id":`, "sourceStreamId", "camera_id", "go2rtcStreamName", "redactedUrl", "rtsp://", "10.0.0.5", "private-source"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public DTO leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestPublicCameraFiltersLegacySourceKeysAndNormalizesOutputReferences(t *testing.T) {
	cameraRow := store.Camera{
		StreamName: "legacy", RecordingStreamName: "legacy-recording", LiveStreamName: "legacy-live", FocusStreamName: "legacy-focus",
		Streams: []store.CameraStream{
			{SourceKey: "recording", Role: store.CameraStreamRoleRecording, Label: "main"},
			{SourceKey: "snapshot", Role: store.CameraStreamRoleSnapshot, Label: "snapshot"},
			{SourceKey: "recording-44", Role: store.CameraStreamRoleRecording, Label: "duplicate"},
		},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, StreamName: "legacy-recording", SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputLive, StreamName: "legacy-live", SourceKey: "snapshot", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand, AppliedPolicy: store.CameraOutputPolicySnapshot{SourceKey: "recording-44", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand}},
			{Purpose: store.CameraOutputFocus, StreamName: "legacy-focus", SourceKey: "recording-44", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		},
		PolicyState: store.CameraPolicyState{DesiredRevision: 2, AppliedRevision: 1, ApplyState: store.CameraApplyPending},
	}
	public := publicCameraFromStore(cameraRow)
	if len(public.Streams) != 1 || public.Streams[0].SourceKey != "recording" {
		t.Fatalf("public legacy sources = %#v", public.Streams)
	}
	for _, output := range public.StreamOutputs {
		if output.SourceKey != "recording" || output.Desired.SourceKey != "recording" || (output.Applied != nil && output.Applied.SourceKey != "recording") {
			t.Fatalf("unsupported source key escaped: %#v", output)
		}
	}
}

func TestPublicCameraRedactsPolicyErrorsAndSecretJSONKeys(t *testing.T) {
	internal := "rtsp://admin:secret@127.0.0.1:8554/private?token=querysecret"
	cameraRow := store.Camera{
		StreamName: "redaction", LastProbeJSON: map[string]any{
			"username": "admin", "profileToken": "PROFILE_000", "nested": map[string]any{"password": "secret", "token": "abc", "safe": internal},
		},
		Streams: []store.CameraStream{{SourceKey: "recording", Role: store.CameraStreamRoleRecording, DetectedError: "probe " + internal}},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand, Verification: store.CameraOutputVerification{Error: "verify " + internal}},
			{Purpose: store.CameraOutputLive, SourceKey: "recording", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputFocus, SourceKey: "recording", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		},
		PolicyState: store.CameraPolicyState{DesiredRevision: 2, AppliedRevision: 1, ApplyState: store.CameraApplyFailed, ApplyError: "apply " + internal},
	}
	encoded := mustMarshalString(t, publicCameraFromStore(cameraRow))
	if !strings.Contains(encoded, `"profileToken":"PROFILE_000"`) {
		t.Fatalf("public camera removed non-secret profile token: %s", encoded)
	}
	for _, forbidden := range []string{"admin", "secret", "querysecret", "127.0.0.1", "8554", "username", "password", `"token"`} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("public camera leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestUpdateCameraStreamOutputsValidatesExactSetAndRevision(t *testing.T) {
	server, camera := newPolicyRouteServer(t, nil)
	badBody := `{"expectedDesiredRevision":1,"outputs":[{"purpose":"recording","sourceKey":"recording","videoMode":"copy","maxWidth":null,"maxHeight":null,"maxFps":null,"audioMode":"source","activation":"on_demand"}]}`
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPut, "/api/cameras/"+camera.StreamName+"/stream-outputs", badBody, trustedConsoleHeaders())
	if status != http.StatusBadRequest {
		t.Fatalf("invalid exact-set status = %d", status)
	}

	validBody := streamOutputRequestBody(t, camera.PolicyState.DesiredRevision+1, store.CameraVideoH264)
	status, _ = requestJSONWithHeaders(t, server.handler, http.MethodPut, "/api/cameras/"+camera.StreamName+"/stream-outputs", validBody, trustedConsoleHeaders())
	if status != http.StatusConflict {
		t.Fatalf("revision mismatch status = %d, want 409", status)
	}
	got, err := server.db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyState.DesiredRevision != camera.PolicyState.DesiredRevision {
		t.Fatalf("failed requests changed revision to %d", got.PolicyState.DesiredRevision)
	}
}

func TestUpdateCameraStreamOutputsReturnsFinalAppliedDTO(t *testing.T) {
	var db *store.DB
	server, camera := newPolicyRouteServer(t, policyApplyFunc(func(ctx context.Context) stream.PolicyApplyResult {
		fresh, err := db.GetCameraByStream(ctx, "policy-camera")
		if err != nil {
			return stream.PolicyApplyResult{Pending: true, Error: err.Error()}
		}
		results := make([]store.CameraOutputApplyResult, 0, 3)
		for _, output := range fresh.Outputs {
			results = append(results, store.CameraOutputApplyResult{Purpose: output.Purpose, Policy: store.CameraOutputPolicySnapshot{SourceKey: output.SourceKey, VideoMode: output.VideoMode, MaxWidth: output.MaxWidth, MaxHeight: output.MaxHeight, MaxFPS: output.MaxFPS, AudioMode: output.AudioMode, Activation: output.Activation}, Verification: store.CameraOutputVerification{VideoCodec: "h264", Width: 1280, Height: 720, CheckedAt: time.Now().UTC()}})
		}
		if err := db.MarkCameraPolicyApplied(ctx, fresh.ID, fresh.PolicyState.DesiredRevision, results); err != nil {
			return stream.PolicyApplyResult{Pending: true, Error: err.Error()}
		}
		return stream.PolicyApplyResult{Applied: true}
	}))
	db = server.db

	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPut, "/api/cameras/"+camera.StreamName+"/stream-outputs", streamOutputRequestBody(t, camera.PolicyState.DesiredRevision, store.CameraVideoH264), trustedConsoleHeaders())
	if status != http.StatusOK || payload["saved"] != true || payload["applied"] != true {
		t.Fatalf("apply response = %d %#v", status, payload)
	}
	publicCamera := requirePayloadObject(t, payload, "camera")
	state := requirePayloadObject(t, publicCamera, "streamApplyState")
	if state["state"] != "applied" || state["desiredRevision"] != state["appliedRevision"] {
		t.Fatalf("final state = %#v", state)
	}
	outputs, ok := publicCamera["streamOutputs"].([]any)
	if !ok || len(outputs) != 3 {
		t.Fatalf("stream outputs = %#v", publicCamera["streamOutputs"])
	}
}

func TestUpdateCameraStreamOutputsReturns202WithRollbackFinalState(t *testing.T) {
	var db *store.DB
	server, camera := newPolicyRouteServer(t, policyApplyFunc(func(ctx context.Context) stream.PolicyApplyResult {
		fresh, _ := db.GetCameraByStream(ctx, "policy-camera")
		_ = db.MarkCameraPolicyFailed(ctx, fresh.ID, fresh.PolicyState.DesiredRevision, "failed rtsp://admin:secret@10.0.0.8/live")
		return stream.PolicyApplyResult{Pending: true, Error: "failed rtsp://admin:secret@10.0.0.8/live"}
	}))
	db = server.db

	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPut, "/api/cameras/"+camera.StreamName+"/stream-outputs", streamOutputRequestBody(t, camera.PolicyState.DesiredRevision, store.CameraVideoH264), trustedConsoleHeaders())
	if status != http.StatusAccepted || payload["saved"] != true || payload["applied"] != false {
		t.Fatalf("pending response = %d %#v", status, payload)
	}
	encoded := mustMarshalString(t, payload)
	for _, forbidden := range []string{"admin", "secret", "10.0.0.8", "rtsp://"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("pending response leaked %q: %s", forbidden, encoded)
		}
	}
	state := requirePayloadObject(t, requirePayloadObject(t, payload, "camera"), "streamApplyState")
	if state["state"] != "apply_failed" {
		t.Fatalf("final rollback state = %#v", state)
	}
}

func TestProbeCameraStreamOutputsUsesStoredHTTPFLVAndLocalRTSPWithoutChangingDesired(t *testing.T) {
	prober := &recordingPolicyProber{}
	applyCalls := 0
	server, camera := newPolicyRouteServerWithProber(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		applyCalls++
		return stream.PolicyApplyResult{Applied: true}
	}), prober)
	camera.Streams[0].URL = "http://10.0.0.8/flv?port=1935&app=bcs&user=admin&password=secret"
	if err := server.db.ReplaceCameraStreams(t.Context(), camera.ID, camera.Streams); err != nil {
		t.Fatal(err)
	}
	before, _ := server.db.GetCameraByStream(t.Context(), camera.StreamName)

	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/"+camera.StreamName+"/stream-outputs/probe", "", trustedConsoleHeaders())
	if status != http.StatusOK {
		t.Fatalf("probe status = %d payload=%#v", status, payload)
	}
	if applyCalls != 1 {
		t.Fatalf("apply calls = %d", applyCalls)
	}
	if len(prober.urls) != 4 || prober.urls[0] != camera.Streams[0].URL {
		t.Fatalf("probe URLs = %#v", prober.urls)
	}
	for _, outputURL := range prober.urls[1:] {
		if !strings.HasPrefix(outputURL, "rtsp://127.0.0.1:8554/") {
			t.Fatalf("output verification did not use local RTSP: %q", outputURL)
		}
	}
	after, err := server.db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	if after.Outputs[1].VideoMode != before.Outputs[1].VideoMode || after.Outputs[1].SourceKey != before.Outputs[1].SourceKey {
		t.Fatalf("manual desired changed: before=%#v after=%#v", before.Outputs[1], after.Outputs[1])
	}
	if after.Streams[0].DetectedVideoCodec != "hevc" || after.Streams[0].DetectedBitDepth != 10 || after.Outputs[0].Verification.VideoCodec != "h264" {
		t.Fatalf("persisted probe metadata = %#v / %#v", after.Streams[0], after.Outputs[0].Verification)
	}
	encoded := mustMarshalString(t, payload)
	for _, forbidden := range []string{"password", "secret", "127.0.0.1", "8554", "http://10.0.0.8"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("probe response leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestReapplyKeepsDesiredRevision(t *testing.T) {
	applyCalls := 0
	server, camera := newPolicyRouteServer(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		applyCalls++
		return stream.PolicyApplyResult{Applied: true}
	}))
	before := camera.PolicyState.DesiredRevision
	body := fmt.Sprintf(`{"expectedDesiredRevision":%d}`, before)
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/"+camera.StreamName+"/stream-outputs/reapply", body, trustedConsoleHeaders())
	if status != http.StatusOK || applyCalls != 1 {
		t.Fatalf("reapply = status %d calls %d", status, applyCalls)
	}
	after, _ := server.db.GetCameraByStream(t.Context(), camera.StreamName)
	if after.PolicyState.DesiredRevision != before {
		t.Fatalf("reapply revision = %d, want %d", after.PolicyState.DesiredRevision, before)
	}
}

func TestReapplyRejectsMissingAndStaleDesiredRevision(t *testing.T) {
	applyCalls := 0
	server, camera := newPolicyRouteServer(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		applyCalls++
		return stream.PolicyApplyResult{Applied: true}
	}))
	for _, body := range []string{"{}", fmt.Sprintf(`{"expectedDesiredRevision":%d}`, camera.PolicyState.DesiredRevision-1), fmt.Sprintf(`{"expectedDesiredRevision":%d,"unknown":true}`, camera.PolicyState.DesiredRevision)} {
		status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/"+camera.StreamName+"/stream-outputs/reapply", body, trustedConsoleHeaders())
		if body == "{}" || strings.Contains(body, "unknown") {
			if status != http.StatusBadRequest {
				t.Fatalf("body %s status = %d", body, status)
			}
		} else if status != http.StatusConflict {
			t.Fatalf("stale reapply status = %d", status)
		}
	}
	if applyCalls != 0 {
		t.Fatalf("stale reapply called apply %d times", applyCalls)
	}
}

func TestBulkProbeStoresAllInputsBeforeOneApply(t *testing.T) {
	prober := &recordingPolicyProber{}
	applyCalls := 0
	var server testRouteServer
	var first store.Camera
	server, first = newPolicyRouteServerWithProber(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		applyCalls++
		cameras, _ := server.db.ListCameras(context.Background(), true)
		for _, cameraRow := range cameras {
			if cameraRow.Streams[0].DetectedVideoCodec == "" {
				t.Fatalf("apply called before detected metadata was saved for %s", cameraRow.StreamName)
			}
		}
		return stream.PolicyApplyResult{Applied: true}
	}), prober)
	_ = first
	second, err := server.db.SaveCameraConfiguration(t.Context(), store.Camera{
		Name: "Second", StreamName: "second", State: "streaming",
		Streams: []store.CameraStream{{SourceKey: "recording", Role: store.CameraStreamRoleRecording, URL: "rtsp://u:p@10.0.0.9/main", Go2RTCStreamName: "second-input"}},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputLive, SourceKey: "recording", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputFocus, SourceKey: "recording", VideoMode: store.CameraVideoAuto, MaxWidth: intTestPtr(1920), MaxHeight: intTestPtr(1080), AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == 0 {
		t.Fatal("second camera not created")
	}

	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/stream-outputs/probe", "", trustedConsoleHeaders())
	if status != http.StatusOK {
		t.Fatalf("bulk status = %d payload=%#v", status, payload)
	}
	if applyCalls != 1 {
		t.Fatalf("bulk apply calls = %d", applyCalls)
	}
	for _, name := range []string{"policy-camera", "second"} {
		cameraRow, _ := server.db.GetCameraByStream(t.Context(), name)
		if cameraRow.Streams[0].DetectedVideoCodec != "h264" {
			t.Fatalf("%s detected = %#v", name, cameraRow.Streams[0])
		}
	}
}

func TestStreamOutputMutationsRequireManagementGuard(t *testing.T) {
	server, camera := newPolicyRouteServer(t, nil)
	requests := []struct{ method, path, body string }{
		{http.MethodPut, "/api/cameras/" + camera.StreamName + "/stream-outputs", streamOutputRequestBody(t, camera.PolicyState.DesiredRevision, store.CameraVideoH264)},
		{http.MethodPost, "/api/cameras/" + camera.StreamName + "/stream-outputs/probe", ""},
		{http.MethodPost, "/api/cameras/" + camera.StreamName + "/stream-outputs/reapply", fmt.Sprintf(`{"expectedDesiredRevision":%d}`, camera.PolicyState.DesiredRevision)},
		{http.MethodPost, "/api/cameras/stream-outputs/probe", ""},
	}
	for _, request := range requests {
		status, _ := requestJSONWithHeaders(t, server.handler, request.method, request.path, request.body, http.Header{})
		if status != http.StatusForbidden {
			t.Fatalf("%s %s status = %d", request.method, request.path, status)
		}
	}
}

func TestPolicyApplyHTTPStatusDistinguishesWarningsAndUnsafeRecovery(t *testing.T) {
	if status, warning := policyApplyHTTPStatus(stream.PolicyApplyResult{Applied: true, Error: "secret detail"}, nil); status != http.StatusOK || warning == "" || strings.Contains(warning, "secret") {
		t.Fatalf("safe active warning = %d %q", status, warning)
	}
	if status, _ := policyApplyHTTPStatus(stream.PolicyApplyResult{Pending: true, Error: "failed"}, []store.Camera{{PolicyState: store.CameraPolicyState{AppliedRevision: 0}}}); status != http.StatusServiceUnavailable {
		t.Fatalf("unsafe initial apply status = %d", status)
	}
	if status, _ := policyApplyHTTPStatus(stream.PolicyApplyResult{Pending: true, Error: "failed"}, []store.Camera{{PolicyState: store.CameraPolicyState{AppliedRevision: 2}}}); status != http.StatusAccepted {
		t.Fatalf("restored previous apply status = %d", status)
	}
	if status, warning := policyApplyHTTPStatus(stream.PolicyApplyResult{Pending: true, RecoveryFailed: true, Error: "secret rollback failure"}, []store.Camera{{PolicyState: store.CameraPolicyState{AppliedRevision: 2}}}); status != http.StatusServiceUnavailable || strings.Contains(warning, "secret") {
		t.Fatalf("recovery failure status = %d warning=%q", status, warning)
	}
	if status, _ := policyApplyHTTPStatus(stream.PolicyApplyResult{Pending: true}, nil); status != http.StatusServiceUnavailable {
		t.Fatalf("deleted final camera rollback status = %d", status)
	}
}

func TestDeleteReturns503WhenApplyFailsEvenWithOtherAppliedCamera(t *testing.T) {
	server, camera := newPolicyRouteServer(t, policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		return stream.PolicyApplyResult{Pending: true, Error: "runtime apply failed"}
	}))
	second, err := server.db.SaveCameraConfiguration(t.Context(), store.Camera{
		Name: "Second", StreamName: "delete-second", State: "streaming",
		Streams: []store.CameraStream{{SourceKey: "recording", Role: store.CameraStreamRoleRecording, URL: "rtsp://u:p@10.0.0.9/main", Go2RTCStreamName: "delete-second-input"}},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputLive, SourceKey: "recording", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputFocus, SourceKey: "recording", VideoMode: store.CameraVideoAuto, MaxWidth: intTestPtr(1920), MaxHeight: intTestPtr(1080), AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := make([]store.CameraOutputApplyResult, 0, 3)
	for _, output := range second.Outputs {
		results = append(results, store.CameraOutputApplyResult{Purpose: output.Purpose, Policy: store.CameraOutputPolicySnapshot{SourceKey: output.SourceKey, VideoMode: output.VideoMode, MaxWidth: output.MaxWidth, MaxHeight: output.MaxHeight, MaxFPS: output.MaxFPS, AudioMode: output.AudioMode, Activation: output.Activation}})
	}
	if err := server.db.MarkCameraPolicyApplied(t.Context(), second.ID, second.PolicyState.DesiredRevision, results); err != nil {
		t.Fatal(err)
	}

	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodDelete, "/api/cameras/"+camera.StreamName, "", trustedConsoleHeaders())
	if status != http.StatusServiceUnavailable {
		t.Fatalf("delete failed apply status = %d, want 503", status)
	}
}

func TestCameraRegistrationSavesIDLessSourceKeyPoliciesAtomically(t *testing.T) {
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	base := map[string]any{
		"name": "Atomic Policy", "streamName": "atomic-policy", "url": routeSyntheticRTSPURL("atomic-main"),
		"streams": []map[string]any{
			{"roleHint": "recording", "label": "main", "url": routeSyntheticRTSPURL("atomic-main"), "profileToken": "main"},
			{"roleHint": "live", "label": "sub", "url": routeSyntheticRTSPURL("atomic-live"), "profileToken": "sub"},
		},
	}
	base["streamOutputs"] = []map[string]any{{"purpose": "recording", "sourceKey": "recording", "videoMode": "copy", "audioMode": "source", "activation": "on_demand"}}
	bad, _ := json.Marshal(base)
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", string(bad), trustedConsoleHeaders())
	if status != http.StatusBadRequest {
		t.Fatalf("invalid atomic create status = %d", status)
	}
	if _, err := server.db.GetCameraByStream(t.Context(), "atomic-policy"); err == nil {
		t.Fatal("invalid policy left an orphan camera")
	}
	base["streamOutputs"] = []map[string]any{
		{"purpose": "recording", "sourceKey": "recording", "videoMode": "copy", "maxWidth": nil, "maxHeight": nil, "maxFps": nil, "audioMode": "source", "activation": "on_demand"},
		{"purpose": "live", "sourceKey": "live", "videoMode": "h264", "maxWidth": nil, "maxHeight": nil, "maxFps": nil, "audioMode": "none", "activation": "on_demand"},
		{"purpose": "focus", "sourceKey": "recording", "videoMode": "h264", "maxWidth": 1280, "maxHeight": 720, "maxFps": 15, "audioMode": "none", "activation": "always"},
	}
	good, _ := json.Marshal(base)
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", string(good), trustedConsoleHeaders())
	if status != http.StatusOK {
		t.Fatalf("valid atomic create status = %d payload=%#v", status, payload)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "atomic-policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Outputs) != 3 || stored.Outputs[1].SourceKey != "live" || stored.Outputs[1].VideoMode != store.CameraVideoH264 || stored.Outputs[2].MaxFPS == nil || *stored.Outputs[2].MaxFPS != 15 || stored.Outputs[2].Activation != store.CameraActivationAlways {
		t.Fatalf("stored registration policies = %#v", stored.Outputs)
	}
}

func newPolicyRouteServer(t *testing.T, applier policyApplyFunc) (testRouteServer, store.Camera) {
	return newPolicyRouteServerWithProber(t, applier, &fakeRouteCameraProber{})
}

func newPolicyRouteServerWithProber(t *testing.T, applier policyApplyFunc, prober camera.Prober) (testRouteServer, store.Camera) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "camstation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "recordings"), 0o755); err != nil {
		t.Fatal(err)
	}
	camera, err := db.SaveCameraConfiguration(t.Context(), store.Camera{
		Name: "Policy", StreamName: "policy-camera", URL: "rtsp://u:p@10.0.0.8/main", State: "streaming",
		Streams: []store.CameraStream{{SourceKey: "recording", Role: store.CameraStreamRoleRecording, Label: "main", URL: "rtsp://u:p@10.0.0.8/main", Go2RTCStreamName: "policy-camera-input"}},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputLive, SourceKey: "recording", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
			{Purpose: store.CameraOutputFocus, SourceKey: "recording", VideoMode: store.CameraVideoAuto, MaxWidth: intTestPtr(1920), MaxHeight: intTestPtr(1080), AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	initialResults := make([]store.CameraOutputApplyResult, 0, 3)
	for _, output := range camera.Outputs {
		initialResults = append(initialResults, store.CameraOutputApplyResult{Purpose: output.Purpose, Policy: store.CameraOutputPolicySnapshot{SourceKey: output.SourceKey, VideoMode: output.VideoMode, MaxWidth: output.MaxWidth, MaxHeight: output.MaxHeight, MaxFPS: output.MaxFPS, AudioMode: output.AudioMode, Activation: output.Activation}})
	}
	if err := db.MarkCameraPolicyApplied(t.Context(), camera.ID, camera.PolicyState.DesiredRevision, initialResults); err != nil {
		t.Fatal(err)
	}
	camera, err = db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil {
		t.Fatal(err)
	}
	if applier == nil {
		applier = func(context.Context) stream.PolicyApplyResult { return stream.PolicyApplyResult{Applied: true} }
	}
	streamer := &fakeStreamController{status: stream.Status{Installed: true, Running: true}}
	handler, err := (routeDeps{db: db, prober: prober, streamer: streamer, policyApplier: applier}).handler()
	if err != nil {
		t.Fatal(err)
	}
	return testRouteServer{handler: handler, db: db}, camera
}

func streamOutputRequestBody(t *testing.T, revision int64, liveMode store.CameraVideoMode) string {
	t.Helper()
	body := map[string]any{"expectedDesiredRevision": revision, "outputs": []map[string]any{
		{"purpose": "recording", "sourceKey": "recording", "videoMode": "copy", "maxWidth": nil, "maxHeight": nil, "maxFps": nil, "audioMode": "source", "activation": "on_demand"},
		{"purpose": "live", "sourceKey": "recording", "videoMode": liveMode, "maxWidth": nil, "maxHeight": nil, "maxFps": nil, "audioMode": "none", "activation": "on_demand"},
		{"purpose": "focus", "sourceKey": "recording", "videoMode": "h264", "maxWidth": 1920, "maxHeight": 1080, "maxFps": nil, "audioMode": "none", "activation": "on_demand"},
	}}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func intTestPtr(value int) *int { return &value }
