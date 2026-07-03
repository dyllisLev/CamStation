package main

import (
	"net/http"
	"testing"

	"camstation/internal/store"
)

func TestCamerasAPI_ListUsesPublicDTOWithoutRawURLs(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	cameraURL := "rtsp://admin:" + "camera-pass" + "@camera.internal:554/main"
	streamURL := "rtsp://stream:" + "camera-pass" + "@camera.internal:554/sub"
	scanURL := "rtsp://scan:" + "camera-pass" + "@camera.internal:554/scan"
	camera, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name:       "Gate",
		URL:        cameraURL,
		StreamName: "gate-main",
		State:      "streaming",
		LastScanJSON: map[string]any{
			"candidate": map[string]any{
				"url":         scanURL,
				"redactedUrl": store.RedactURL(scanURL),
			},
		},
	})
	if err != nil {
		t.Fatalf("seed camera: %v", err)
	}
	if err := server.db.ReplaceCameraStreams(t.Context(), camera.ID, []store.CameraStream{{
		Role:             store.CameraStreamRoleRecording,
		Label:            "main",
		Source:           "onvif",
		URL:              streamURL,
		Go2RTCStreamName: "gate-main-recording",
	}}); err != nil {
		t.Fatalf("seed camera stream: %v", err)
	}

	// When
	status, cameras := requestJSONArray(t, server.handler, http.MethodGet, "/api/cameras", "")

	// Then
	if status != http.StatusOK {
		t.Fatalf("GET /api/cameras status = %d, want %d", status, http.StatusOK)
	}
	if len(cameras) != 1 {
		t.Fatalf("camera count = %d, want 1", len(cameras))
	}
	assertPublicArrayDoesNotContain(t, cameras, cameraURL)
	assertPublicArrayDoesNotContain(t, cameras, streamURL)
	assertPublicArrayDoesNotContain(t, cameras, scanURL)
	if countJSONKey(cameras, "url") != 0 {
		t.Fatalf("public camera payload included raw url keys")
	}
	if cameras[0]["redactedUrl"] == "" {
		t.Fatalf("public camera payload omitted redactedUrl")
	}

	internal, err := server.db.GetCameraByStream(t.Context(), "gate-main")
	if err != nil {
		t.Fatalf("read internal camera: %v", err)
	}
	if internal.URL != cameraURL {
		t.Fatalf("internal camera URL was not preserved")
	}
	if len(internal.Streams) != 1 || internal.Streams[0].URL != streamURL {
		t.Fatalf("internal camera stream URL was not preserved")
	}
	writeAPIEvidence(t, "cameras-public-dto-secret-hygiene.json", map[string]any{
		"status":                     status,
		"cameraCount":                len(cameras),
		"rawUrlKeyCount":             countJSONKey(cameras, "url"),
		"redactedUrlKeyCount":        countJSONKey(cameras, "redactedUrl"),
		"internalCameraURLPreserved": internal.URL != "",
		"internalStreamURLPreserved": len(internal.Streams) == 1 && internal.Streams[0].URL != "",
	})
}

func countJSONKey(value any, key string) int {
	switch typed := value.(type) {
	case []map[string]any:
		total := 0
		for _, item := range typed {
			total += countJSONKey(item, key)
		}
		return total
	case []any:
		total := 0
		for _, item := range typed {
			total += countJSONKey(item, key)
		}
		return total
	case map[string]any:
		total := 0
		for itemKey, item := range typed {
			if itemKey == key {
				total++
			}
			total += countJSONKey(item, key)
		}
		return total
	default:
		return 0
	}
}
