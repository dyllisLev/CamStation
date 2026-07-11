package stream

import (
	"os"
	"strings"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestResolveAutoCopiesVerifiedBrowserSafeH264(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	output.AudioMode = store.CameraAudioSource
	resolved, err := resolveOutput(camera, output)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Transcoding {
		t.Fatal("safe H.264 should not transcode")
	}
	if resolved.Result.Verification.Transcoding {
		t.Fatal("safe H.264 persisted as transcoding")
	}
	wantProducer := "rtsp://127.0.0.1:8554/" + resolved.SourceName
	if resolved.Producer != wantProducer {
		t.Fatalf("producer = %q, want local RTSP alias %q", resolved.Producer, wantProducer)
	}
}

func TestResolveAutoTranscodesHEVCAndUnverifiedH264(t *testing.T) {
	for _, tc := range []struct {
		name      string
		codec     string
		checkedAt time.Time
	}{
		{name: "hevc", codec: "hevc", checkedAt: time.Now()},
		{name: "unverified", codec: "h264"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			camera, output := policyFixture(tc.codec, "yuv420p", 8, 3840, 2160, 20)
			camera.Streams[0].DetectedCheckedAt = tc.checkedAt
			resolved, err := resolveOutput(camera, output)
			if err != nil {
				t.Fatal(err)
			}
			if !resolved.Transcoding || !strings.Contains(resolved.Producer, "#video=h264") {
				t.Fatalf("producer = %q, want H.264 transcode", resolved.Producer)
			}
		})
	}
}

func TestResolveUnverifiedInputUsesAspectSafeNoUpscaleRuntimeCap(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 0, 0, 0)
	camera.Streams[0].DetectedCheckedAt = time.Time{}
	width, height := 1920, 1080
	output.MaxWidth, output.MaxHeight = &width, &height
	resolved, err := resolveOutput(camera, output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resolved.Producer, "#width=1920#height=1080") ||
		!strings.Contains(resolved.Producer, "min(iw,1920)") ||
		!strings.Contains(resolved.Producer, "force_original_aspect_ratio=decrease") ||
		!strings.Contains(resolved.Producer, "force_divisible_by=2") {
		t.Fatalf("producer = %q, want aspect-safe dynamic maximum", resolved.Producer)
	}
	if resolved.Result.Verification.Width != 0 || resolved.Result.Verification.Height != 0 {
		t.Fatalf("unknown input reported invented size: %#v", resolved.Result.Verification)
	}
}

func TestResolveAutoTranscodesUnsupportedH264ProfilesAndLevels(t *testing.T) {
	for _, tc := range []struct{ profile, level string }{
		{profile: "High 10", level: "4.1"},
		{profile: "High 4:2:2", level: "4.1"},
		{profile: "High 4:4:4 Predictive", level: "4.1"},
		{profile: "unknown", level: "4.1"},
		{profile: "High", level: "unknown"},
		{profile: "High", level: "6.0"},
	} {
		t.Run(tc.profile+"-"+tc.level, func(t *testing.T) {
			camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
			camera.Streams[0].DetectedProfile = tc.profile
			camera.Streams[0].DetectedLevel = tc.level
			resolved, err := resolveOutput(camera, output)
			if err != nil {
				t.Fatal(err)
			}
			if !resolved.Transcoding {
				t.Fatalf("profile=%q level=%q was copied", tc.profile, tc.level)
			}
		})
	}
}

func TestResolveOutputPreservesAspectRatioWithoutUpscalingAndLimitsFPS(t *testing.T) {
	camera, output := policyFixture("hevc", "yuv420p", 8, 3840, 2160, 30)
	width, height, fps := 1920, 1200, 15.0
	output.MaxWidth, output.MaxHeight, output.MaxFPS = &width, &height, &fps
	resolved, err := resolveOutput(camera, output)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Result.Verification.Width != 1920 || resolved.Result.Verification.Height != 1080 {
		t.Fatalf("size = %dx%d, want 1920x1080", resolved.Result.Verification.Width, resolved.Result.Verification.Height)
	}
	if resolved.Result.Verification.FPS != 15 {
		t.Fatalf("fps = %v, want 15", resolved.Result.Verification.FPS)
	}
	if !strings.Contains(resolved.Producer, "min(iw,1920)") || !strings.Contains(resolved.Producer, "min(ih,1200)") || !strings.Contains(resolved.Producer, "-r 15") {
		t.Fatalf("producer = %q, want bounded dimensions and FPS", resolved.Producer)
	}

	camera, output = policyFixture("hevc", "yuv420p", 8, 640, 360, 10)
	output.MaxWidth, output.MaxHeight = &width, &height
	resolved, err = resolveOutput(camera, output)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Result.Verification.Width != 640 || resolved.Result.Verification.Height != 360 {
		t.Fatalf("upscaled to %dx%d", resolved.Result.Verification.Width, resolved.Result.Verification.Height)
	}
}

func TestRenderPolicyConfigKeepsSourcesPrivateEscapesYAMLAndPreloadsAlways(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Name = "evil:\nstreams: {owned: yes}"
	camera.Streams[0].URL = "rtsp://admin:secret@192.0.2.1/main?token=querysecret"
	output.StreamName = "public-focus"
	output.Activation = store.CameraActivationAlways
	camera.Outputs = []store.CameraOutput{output}

	config, _, err := renderPolicyConfig([]store.Camera{camera}, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	if !strings.Contains(text, "rtsp://admin:secret@192.0.2.1/main?token=querysecret") {
		t.Fatal("private generated config must contain the producer credentials")
	}
	if strings.Contains(text, "\nstreams: {owned: yes}\n") {
		t.Fatalf("camera name injected YAML: %s", text)
	}
	if !strings.Contains(text, "preload:\n  \"public-focus\":") {
		t.Fatalf("missing preload entry: %s", text)
	}
	if !strings.Contains(text, "\"public-focus\": \"video\"") {
		t.Fatalf("video-only output requested unavailable preload tracks: %s", text)
	}
	if !strings.Contains(text, "\"public-focus\":\n") {
		t.Fatalf("output key is not safely quoted: %s", text)
	}
}

func TestRenderPolicyConfigUsesCollisionFreePrivateInputNames(t *testing.T) {
	camera, first := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Streams[0].SourceKey = "a:b"
	camera.Streams = append(camera.Streams, camera.Streams[0])
	camera.Streams[1].ID = 11
	camera.Streams[1].SourceKey = "a?b"
	camera.Streams[1].URL = "rtsp://user:pass@192.0.2.1/sub"
	first.SourceKey, first.SourceStreamID, first.StreamName = "a:b", 10, "output-one"
	second := first
	second.SourceKey, second.SourceStreamID, second.StreamName, second.Purpose = "a?b", 11, "output-two", store.CameraOutputLive
	camera.Outputs = []store.CameraOutput{first, second}
	config, _, err := renderPolicyConfig([]store.Camera{camera}, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	if !strings.Contains(text, privateSourcePrefix+"1_10") || !strings.Contains(text, privateSourcePrefix+"1_11") {
		t.Fatalf("private source names collided: %s", text)
	}
}

func TestResolveOutputPrefersStableSourceKeyOverStaleSourceID(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Streams[0].SourceKey = "live"
	camera.Streams = append(camera.Streams, camera.Streams[0])
	camera.Streams[1].ID, camera.Streams[1].SourceKey = 11, "recording"
	output.SourceStreamID, output.SourceKey = 10, "recording"
	resolved, err := resolveOutput(camera, output)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SourceName != privateSourcePrefix+"1_11" {
		t.Fatalf("source = %q, want stable source-key row", resolved.SourceName)
	}
}

func TestStartupConfigUsesAppliedPolicyInsteadOfPendingDesiredPolicy(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	output.VideoMode = store.CameraVideoH264
	output.AppliedPolicy = store.CameraOutputPolicySnapshot{
		SourceStreamID: output.SourceStreamID, SourceKey: output.SourceKey,
		VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand,
	}
	camera.PolicyState.DesiredRevision, camera.PolicyState.AppliedRevision = 1, 1
	camera.Outputs = []store.CameraOutput{output}
	config, err := renderStartupConfig([]store.Camera{camera})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "#video=h264") {
		t.Fatalf("startup applied pending desired transcode: %s", config)
	}
}

func TestStartupConfigRestoresPersistedAppliedAutoDecisionAndEffectiveBounds(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 640, 360, 20)
	maxWidth, maxHeight, maxFPS := 1920, 1080, 20.0
	output.MaxWidth, output.MaxHeight, output.MaxFPS = &maxWidth, &maxHeight, &maxFPS
	output.AppliedPolicy = store.CameraOutputPolicySnapshot{
		SourceStreamID: output.SourceStreamID, SourceKey: output.SourceKey, VideoMode: store.CameraVideoAuto,
		MaxWidth: &maxWidth, MaxHeight: &maxHeight, MaxFPS: &maxFPS,
		AudioMode: store.CameraAudioNone, Activation: store.CameraActivationOnDemand,
	}
	output.Verification = store.CameraOutputVerification{
		VideoCodec: "h264", Width: 1280, Height: 720, FPS: 15, Transcoding: true, CheckedAt: time.Now(),
	}
	camera.PolicyState.AppliedRevision = 7
	camera.Outputs = []store.CameraOutput{output}
	config, err := renderStartupConfig([]store.Camera{camera})
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	if !strings.Contains(text, "#video=h264") || !strings.Contains(text, "min(iw,1280)") || !strings.Contains(text, "-r 15") {
		t.Fatalf("startup lost applied effective snapshot: %s", text)
	}
}

func TestStartupConfigRejectsMissingAppliedSnapshotAfterAppliedRevision(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.PolicyState.AppliedRevision = 2
	camera.Outputs = []store.CameraOutput{output}
	if _, err := renderStartupConfig([]store.Camera{camera}); err == nil {
		t.Fatal("expected missing applied snapshot to fail closed")
	}
}

func TestWriteConfigUsesAppliedSnapshot(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	output.VideoMode = store.CameraVideoH264
	output.AppliedPolicy = store.CameraOutputPolicySnapshot{
		SourceStreamID: output.SourceStreamID, SourceKey: output.SourceKey,
		VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioSource, Activation: store.CameraActivationOnDemand,
	}
	camera.PolicyState.DesiredRevision, camera.PolicyState.AppliedRevision = 1, 1
	camera.Outputs = []store.CameraOutput{output}
	path := t.TempDir() + "/go2rtc.yaml"
	if err := NewGo2RTC(path).WriteConfig([]store.Camera{camera}); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "#video=h264") {
		t.Fatalf("WriteConfig applied pending desired: %s", config)
	}
}

func TestStartupPendingPolicyUsesLastGoodConfigInsteadOfNewDesiredInput(t *testing.T) {
	path := t.TempDir() + "/go2rtc.yaml"
	if err := os.WriteFile(path, []byte("new-desired-input\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".last-good", []byte("old-applied-input\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGo2RTC(path)
	camera := store.Camera{PolicyState: store.CameraPolicyState{
		DesiredRevision: 2, AppliedRevision: 1, ApplyState: store.CameraApplyPending,
	}}
	config, preserve, err := g.startupConfig([]store.Camera{camera})
	if err != nil {
		t.Fatal(err)
	}
	if !preserve || string(config) != "old-applied-input\n" {
		t.Fatalf("startup config = %q preserve=%v", config, preserve)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".last-good"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.startupConfig([]store.Camera{camera}); err == nil {
		t.Fatal("pending policy without a last-good/current config must fail closed")
	}
}

func policyFixture(codec, pixelFormat string, bitDepth, width, height int, fps float64) (store.Camera, store.CameraOutput) {
	stream := store.CameraStream{
		ID: 10, CameraID: 1, SourceKey: "recording", URL: "rtsp://user:pass@192.0.2.1/main",
		DetectedVideoCodec: codec, DetectedAudioCodec: "aac", DetectedPixelFormat: pixelFormat,
		DetectedProfile: "High", DetectedLevel: "4.1",
		DetectedBitDepth: bitDepth, DetectedWidth: width, DetectedHeight: height, DetectedFPS: fps,
		DetectedCheckedAt: time.Now(),
	}
	output := store.CameraOutput{
		CameraID: 1, Purpose: store.CameraOutputFocus, StreamName: "camera-focus", SourceStreamID: 10,
		SourceKey: "recording", VideoMode: store.CameraVideoAuto, AudioMode: store.CameraAudioNone,
		Activation: store.CameraActivationOnDemand,
	}
	return store.Camera{ID: 1, StreamName: "camera", Streams: []store.CameraStream{stream}}, output
}
