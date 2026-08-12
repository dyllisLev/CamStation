package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type restartWarningStreamer struct {
	err error
}

func (s *restartWarningStreamer) Status(context.Context) stream.Status {
	return stream.Status{Installed: true, Running: false, Error: "restart failed"}
}

func (s *restartWarningStreamer) Restart(context.Context, []store.Camera) error {
	return s.err
}

func TestCamerasAPI_CreateUpdateDeleteRestartWarningsArePublic_whenGo2RTCRestartFails(t *testing.T) {
	t.Parallel()

	// Given
	secrets := routeCameraMutationSecrets{
		cameraURL: routeSyntheticRTSPURL("warning-main"),
		streamURL: routeSyntheticRTSPURL("warning-stream"),
		scanURL:   routeSyntheticRTSPURL("warning-scan"),
	}
	restartErr := errors.New("restart failed at http://127.0.0.1:1984/api/streams?src=rtsp://operator:warning-secret@camera.internal:554/main")
	server := newCameraMutationWarningRouteServer(t, restartErr)
	createBody := cameraMutationRequestBody(t, "Warning Gate", "warning-gate", secrets)

	// When
	createStatus, createPayload := requestJSON(t, server.handler, http.MethodPost, "/api/cameras", createBody)
	updateBody := cameraMutationRequestBody(t, "Warning Gate Updated", "ignored-warning-gate", secrets)
	updateStatus, updatePayload := requestJSON(t, server.handler, http.MethodPut, "/api/cameras/warning-gate", updateBody)
	deleteStatus, deletePayload := requestJSON(t, server.handler, http.MethodDelete, "/api/cameras/warning-gate", "")
	eventsStatus, eventsPayload := requestJSON(t, server.handler, http.MethodGet, "/api/events?source=go2rtc&limit=10", "")

	// Then
	responses := map[string]struct {
		status  int
		payload map[string]any
	}{
		"create": {status: createStatus, payload: createPayload},
		"update": {status: updateStatus, payload: updatePayload},
		"delete": {status: deleteStatus, payload: deletePayload},
	}
	for name, response := range responses {
		if response.status != http.StatusAccepted {
			t.Fatalf("%s status = %d, want %d; body=%#v", name, response.status, http.StatusAccepted, response.payload)
		}
		warning, ok := response.payload["warning"].(string)
		if !ok || warning == "" {
			t.Fatalf("%s warning missing: %#v", name, response.payload)
		}
		assertCameraRestartWarningIsPublic(t, response.payload)
	}
	if eventsStatus != http.StatusOK {
		t.Fatalf("events status = %d, want %d; body=%#v", eventsStatus, http.StatusOK, eventsPayload)
	}
	assertPublicPayloadHasNoRestartRuntimeMarkers(t, eventsPayload)
}

func newCameraMutationWarningRouteServer(t *testing.T, restartErr error) testRouteServer {
	t.Helper()

	tempDir := t.TempDir()
	db, err := store.Open(filepath.Join(tempDir, "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	recordingsDir := filepath.Join(tempDir, "recordings")
	tempRecordingDir := filepath.Join(tempDir, "temp")
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		t.Fatalf("create recordings dir: %v", err)
	}
	if err := os.MkdirAll(tempRecordingDir, 0o755); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	handler, err := routeDeps{
		db:              db,
		prober:          &fakeRouteCameraProber{},
		streamer:        &restartWarningStreamer{err: restartErr},
		recorderManager: recorder.New(db, recordingsDir, tempRecordingDir, 5),
		cleaner:         cleanup.New(db, recordingsDir),
		recordingsDir:   recordingsDir,
		tempDir:         tempRecordingDir,
	}.handler()
	if err != nil {
		t.Fatalf("build routes: %v", err)
	}
	return testRouteServer{handler: handler, db: db, recordingsDir: recordingsDir, tempDir: tempRecordingDir}
}

func assertCameraRestartWarningIsPublic(t *testing.T, payload map[string]any) {
	t.Helper()

	warning, ok := payload["warning"].(string)
	if !ok {
		t.Fatalf("restart warning missing: %#v", payload)
	}
	for _, forbidden := range []string{"127.0.0.1:1984", "/api/streams", "rtsp://", "warning-secret"} {
		if strings.Contains(warning, forbidden) {
			t.Fatalf("restart warning leaked forbidden runtime marker")
		}
	}
	assertPublicPayloadHasNoRestartRuntimeMarkers(t, payload)
}

func assertPublicPayloadHasNoRestartRuntimeMarkers(t *testing.T, payload map[string]any) {
	t.Helper()

	encoded := mustMarshalString(t, payload)
	for _, forbidden := range []string{"127.0.0.1:1984", "/api/streams", "warning-secret"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("restart warning response leaked forbidden runtime marker")
		}
	}
}
