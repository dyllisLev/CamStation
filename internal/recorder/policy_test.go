package recorder

import (
	"strings"
	"testing"

	"camstation/internal/store"
)

func TestRecordingSpecUsesAppliedOutputAndNeverRawCameraURL(t *testing.T) {
	camera := store.Camera{
		ID: 7, Enabled: true, StreamName: "legacy", URL: "rtsp://admin:secret@192.0.2.8/raw",
		Outputs: []store.CameraOutput{{
			Purpose: store.CameraOutputRecording, StreamName: "server-owned-recording",
			VideoMode: store.CameraVideoH264, AudioMode: store.CameraAudioNone,
			AppliedPolicy: store.CameraOutputPolicySnapshot{
				SourceKey: "recording", VideoMode: store.CameraVideoCopy, AudioMode: store.CameraAudioAAC,
			},
		}},
	}
	spec, err := recordingSpec(camera, "rtsp://127.0.0.1:8554")
	if err != nil {
		t.Fatal(err)
	}
	if spec.camera.StreamName != "server-owned-recording" {
		t.Fatalf("stream = %q, want applied recording output", spec.camera.StreamName)
	}
	if spec.input != "rtsp://127.0.0.1:8554/server-owned-recording" {
		t.Fatalf("input = %q, want local applied output", spec.input)
	}
	if strings.Contains(spec.input, "secret") || spec.audioMode != store.CameraAudioAAC {
		t.Fatalf("recording spec leaked raw input or used desired audio: %+v", spec)
	}
}

func TestBuildFFmpegArgsSupportsRecordingAudioModes(t *testing.T) {
	for _, tc := range []struct {
		mode    store.CameraAudioMode
		want    string
		notWant string
	}{
		{mode: store.CameraAudioSource, want: "-c:a aac", notWant: "-an"},
		{mode: store.CameraAudioAAC, want: "-c:a copy", notWant: "-an"},
		{mode: store.CameraAudioNone, want: "-an", notWant: "-c:a"},
	} {
		t.Run(string(tc.mode), func(t *testing.T) {
			args := BuildFFmpegArgsForPolicy("rtsp://127.0.0.1:8554/cam", "/tmp/cam", 5, "cam", tc.mode)
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, "-c:v copy") || !strings.Contains(joined, tc.want) {
				t.Fatalf("args = %s, want video copy and %s", joined, tc.want)
			}
			if strings.Contains(joined, tc.notWant) {
				t.Fatalf("args = %s, did not want %s", joined, tc.notWant)
			}
		})
	}
}

func TestSuspendActiveReturnsOnlyRunningWorkers(t *testing.T) {
	manager := New(nil, t.TempDir(), t.TempDir(), 5)
	done := make(chan struct{})
	close(done)
	manager.workers["active"] = &worker{
		camera: store.Camera{ID: 1, StreamName: "active"}, stop: make(chan struct{}), done: done,
	}

	active := manager.SuspendActive()
	if len(active) != 1 || active[0].StreamName != "active" {
		t.Fatalf("active = %+v", active)
	}
	if len(manager.workers) != 0 {
		t.Fatalf("workers still active: %+v", manager.workers)
	}
}

func TestRestoreActivePrevalidatesAllCamerasBeforeStartingAnyWorker(t *testing.T) {
	manager := New(nil, t.TempDir(), t.TempDir(), 5)
	t.Cleanup(manager.StopAll)
	err := manager.RestoreActive([]store.Camera{
		{ID: 1, Enabled: true, StreamName: "valid"},
		{ID: 2, Enabled: true},
	})
	if err == nil {
		t.Fatal("expected malformed second camera to fail validation")
	}
	if workers := manager.Status().Workers; len(workers) != 0 {
		t.Fatalf("partial workers started: %+v", workers)
	}
}

func TestRecordingSpecRejectsDisabledCamera(t *testing.T) {
	_, err := recordingSpec(store.Camera{Enabled: false, StreamName: "disabled"}, "rtsp://127.0.0.1:8554")
	if err == nil {
		t.Fatal("disabled camera recording was accepted")
	}
}
