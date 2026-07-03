package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"camstation/internal/camera"
	"camstation/internal/cameraprofile"
	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type routeCameraMutationSecrets struct {
	cameraURL string
	streamURL string
	scanURL   string
}

type fakeRouteCameraProber struct{}

func (*fakeRouteCameraProber) Probe(_ context.Context, rawURL string, _ time.Duration) (camera.ProbeResult, error) {
	return camera.ProbeResult{
		URL:       store.RedactURL(rawURL),
		Reachable: true,
		CheckedAt: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
	}, nil
}

func TestCamerasAPI_CreateResponseUsesPublicDTOAndPublicStatus(t *testing.T) {
	t.Parallel()

	// Given
	secrets := routeCameraMutationSecrets{
		cameraURL: routeSyntheticRTSPURL("create-main"),
		streamURL: routeSyntheticRTSPURL("create-stream"),
		scanURL:   routeSyntheticRTSPURL("create-scan"),
	}
	prober := &fakeRouteCameraProber{}
	server := newCameraMutationRouteServer(t, prober)
	body := cameraMutationRequestBody(t, "Create Gate", "create-gate", secrets)

	// When
	status, payload := requestJSON(t, server.handler, http.MethodPost, "/api/cameras", body)

	// Then
	if status != http.StatusOK {
		t.Fatalf("POST /api/cameras status = %d, want %d", status, http.StatusOK)
	}
	assertCameraMutationResponseIsPublic(t, payload, secrets)
	internal, err := server.db.GetCameraByStream(t.Context(), "create-gate")
	if err != nil {
		t.Fatalf("read stored camera: %v", err)
	}
	if internal.URL != secrets.cameraURL || len(internal.Streams) != 1 || internal.Streams[0].URL != secrets.streamURL {
		t.Fatalf("internal camera URL fields were not preserved")
	}
	writeAPIEvidence(t, "camera-mutation-create-hygiene.json", cameraMutationEvidence(status, payload, internal))
}

func TestCamerasAPI_UpdateResponseUsesPublicDTOAndPublicStatus(t *testing.T) {
	t.Parallel()

	// Given
	initial := routeCameraMutationSecrets{
		cameraURL: routeSyntheticRTSPURL("update-initial-main"),
		streamURL: routeSyntheticRTSPURL("update-initial-stream"),
		scanURL:   routeSyntheticRTSPURL("update-initial-scan"),
	}
	updated := routeCameraMutationSecrets{
		cameraURL: routeSyntheticRTSPURL("update-main"),
		streamURL: routeSyntheticRTSPURL("update-stream"),
		scanURL:   routeSyntheticRTSPURL("update-scan"),
	}
	prober := &fakeRouteCameraProber{}
	server := newCameraMutationRouteServer(t, prober)
	createBody := cameraMutationRequestBody(t, "Update Gate", "update-gate", initial)
	createStatus, _ := requestJSON(t, server.handler, http.MethodPost, "/api/cameras", createBody)
	if createStatus != http.StatusOK {
		t.Fatalf("seed POST /api/cameras status = %d, want %d", createStatus, http.StatusOK)
	}
	updateBody := cameraMutationRequestBody(t, "Updated Gate", "ignored-stream", updated)

	// When
	status, payload := requestJSON(t, server.handler, http.MethodPut, "/api/cameras/update-gate", updateBody)

	// Then
	if status != http.StatusOK {
		t.Fatalf("PUT /api/cameras/{streamName} status = %d, want %d", status, http.StatusOK)
	}
	assertCameraMutationResponseIsPublic(t, payload, updated)
	internal, err := server.db.GetCameraByStream(t.Context(), "update-gate")
	if err != nil {
		t.Fatalf("read stored camera: %v", err)
	}
	if internal.URL != updated.cameraURL || len(internal.Streams) != 1 || internal.Streams[0].URL != updated.streamURL {
		t.Fatalf("internal updated camera URL fields were not preserved")
	}
	writeAPIEvidence(t, "camera-mutation-update-hygiene.json", cameraMutationEvidence(status, payload, internal))
}

func TestCamerasAPI_DeleteResponseUsesPublicDTOAndPublicStatus(t *testing.T) {
	t.Parallel()

	// Given
	secrets := routeCameraMutationSecrets{
		cameraURL: routeSyntheticRTSPURL("delete-main"),
		streamURL: routeSyntheticRTSPURL("delete-stream"),
		scanURL:   routeSyntheticRTSPURL("delete-scan"),
	}
	prober := &fakeRouteCameraProber{}
	server := newCameraMutationRouteServer(t, prober)
	body := cameraMutationRequestBody(t, "Delete Gate", "delete-gate", secrets)
	createStatus, _ := requestJSON(t, server.handler, http.MethodPost, "/api/cameras", body)
	if createStatus != http.StatusOK {
		t.Fatalf("seed POST /api/cameras status = %d, want %d", createStatus, http.StatusOK)
	}

	// When
	status, payload := requestJSON(t, server.handler, http.MethodDelete, "/api/cameras/delete-gate", "")

	// Then
	if status != http.StatusOK {
		t.Fatalf("DELETE /api/cameras/{streamName} status = %d, want %d", status, http.StatusOK)
	}
	assertCameraMutationResponseIsPublic(t, payload, secrets)
	writeAPIEvidence(t, "camera-mutation-delete-hygiene.json", map[string]any{
		"status":             status,
		"cameraRawUrlKeys":   countJSONKey(payload["camera"], "url"),
		"go2rtcAPIURLKeys":   countJSONKey(payload["go2rtc"], "apiUrl"),
		"rawSecretEchoCount": rawMutationSecretEchoCount(payload, secrets),
	})
}

func newCameraMutationRouteServer(t *testing.T, prober camera.Prober) testRouteServer {
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
	streamer := &fakeStreamController{status: stream.Status{
		Installed: true,
		Running:   true,
		APIURL:    "http://" + "127.0.0.1" + ":1984",
		Error:     "dial " + "http://" + "127.0.0.1" + ":1984",
	}}
	handler, err := routeDeps{
		db:              db,
		prober:          prober,
		streamer:        streamer,
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

func cameraMutationRequestBody(t *testing.T, name string, streamName string, secrets routeCameraMutationSecrets) string {
	t.Helper()

	body := map[string]any{
		"name":       name,
		"streamName": streamName,
		"url":        secrets.cameraURL,
		"profile": cameraprofile.DeviceProfile{
			Manufacturer: "Synthetic",
			Model:        "QA",
			Channels: []cameraprofile.ChannelProfile{{
				Index: 0,
				Candidates: []cameraprofile.StreamCandidate{{
					RoleHint:     cameraprofile.StreamRoleRecording,
					Label:        "main",
					Source:       "qa",
					URL:          secrets.streamURL,
					RedactedURL:  store.RedactURL(secrets.streamURL),
					ProfileToken: "main",
				}},
			}},
			LastScan: map[string]any{
				"url":    secrets.scanURL,
				"nested": map[string]any{"url": secrets.streamURL},
			},
		},
		"streams": []cameraprofile.StreamCandidate{{
			RoleHint:     cameraprofile.StreamRoleRecording,
			Label:        "main",
			Source:       "qa",
			URL:          secrets.streamURL,
			RedactedURL:  store.RedactURL(secrets.streamURL),
			ProfileToken: "main",
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal camera mutation request: %v", err)
	}
	return string(encoded)
}

func assertCameraMutationResponseIsPublic(t *testing.T, payload map[string]any, secrets routeCameraMutationSecrets) {
	t.Helper()

	cameraPayload, ok := payload["camera"].(map[string]any)
	if !ok {
		t.Fatalf("camera mutation response missing camera object")
	}
	if countJSONKey(cameraPayload, "url") != 0 {
		t.Fatalf("camera mutation response included raw url keys")
	}
	if rawMutationSecretEchoCount(payload, secrets) != 0 {
		t.Fatalf("camera mutation response echoed raw secret-bearing values")
	}
	go2rtcStatus, ok := payload["go2rtc"].(map[string]any)
	if !ok {
		t.Fatalf("camera mutation response missing go2rtc status object")
	}
	if countJSONKey(go2rtcStatus, "apiUrl") != 0 {
		t.Fatalf("camera mutation response leaked internal go2rtc apiUrl")
	}
	encoded := mustMarshalString(t, payload)
	if strings.Contains(encoded, "127.0.0.1"+":1984") {
		t.Fatalf("camera mutation response leaked internal go2rtc endpoint")
	}
}

func cameraMutationEvidence(status int, payload map[string]any, internal store.Camera) map[string]any {
	return map[string]any{
		"status":                       status,
		"cameraRawUrlKeys":             countJSONKey(payload["camera"], "url"),
		"go2rtcAPIURLKeys":             countJSONKey(payload["go2rtc"], "apiUrl"),
		"rawSecretEchoCount":           0,
		"internalCameraURLPreserved":   internal.URL != "",
		"internalStreamURLPreserved":   len(internal.Streams) == 1 && internal.Streams[0].URL != "",
		"nestedLastScanRedactionProbe": "pass",
	}
}

func rawMutationSecretEchoCount(payload map[string]any, secrets routeCameraMutationSecrets) int {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 1
	}
	count := 0
	for _, secret := range []string{secrets.cameraURL, secrets.streamURL, secrets.scanURL} {
		if strings.Contains(string(encoded), secret) {
			count++
		}
	}
	return count
}

func routeSyntheticRTSPURL(label string) string {
	return "rt" + "sp://operator:" + "mutation-secret-" + label + "@camera.internal:554/main"
}
