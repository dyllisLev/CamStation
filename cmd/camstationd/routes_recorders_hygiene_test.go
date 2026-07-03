package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"camstation/internal/store"
)

func TestRecordersAPI_PublicResponsesHideInternalTransportAndPaths(t *testing.T) {
	// Given
	originalLogWriter := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		log.SetOutput(originalLogWriter)
	})
	server := newTestRouteServer(t)
	if _, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name:       "Front",
		URL:        "rtsp://camera.example.invalid/main",
		StreamName: "front-main",
		State:      "streaming",
	}); err != nil {
		t.Fatalf("seed camera: %v", err)
	}
	t.Cleanup(func() {
		req := httptest.NewRequest(http.MethodPost, "/api/recorders/stop", nil)
		rec := httptest.NewRecorder()
		server.handler.ServeHTTP(rec, req)
	})

	// When
	statusCode, statusBody := requestJSON(t, server.handler, http.MethodGet, "/api/recorders/status", "")
	startCode, startBody := requestJSON(t, server.handler, http.MethodPost, "/api/recorders/start?stream=front-main", "")
	stopCode, stopBody := requestJSON(t, server.handler, http.MethodPost, "/api/recorders/stop?stream=front-main", "")

	// Then
	endpoints := []publicHygieneResponse{
		{name: "status", status: statusCode, body: statusBody},
		{name: "start", status: startCode, body: startBody},
		{name: "stop", status: stopCode, body: stopBody},
	}
	for _, endpoint := range endpoints {
		if endpoint.status != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", endpoint.name, endpoint.status, http.StatusOK)
		}
		assertPublicHygieneCounts(t, endpoint.name, endpoint.body, publicHygieneForbidden{
			recordingsDir: server.recordingsDir,
			tempDir:       server.tempDir,
		})
	}
	writeAPIEvidence(t, "recorders-public-hygiene.json", map[string]any{
		"responses": publicHygieneEvidence(endpoints, publicHygieneForbidden{
			recordingsDir: server.recordingsDir,
			tempDir:       server.tempDir,
		}),
	})
}

func TestRecordingsStorageAPI_PublicResponseHidesRuntimePaths(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)

	// When
	status, body := requestJSON(t, server.handler, http.MethodGet, "/api/recordings/storage", "")

	// Then
	if status != http.StatusOK {
		t.Fatalf("storage status = %d, want %d", status, http.StatusOK)
	}
	assertPublicHygieneCounts(t, "storage", body, publicHygieneForbidden{
		recordingsDir: server.recordingsDir,
		tempDir:       server.tempDir,
	})
	writeAPIEvidence(t, "recordings-storage-public-hygiene.json", map[string]any{
		"status": status,
		"counts": publicHygieneCounts(body, publicHygieneForbidden{
			recordingsDir: server.recordingsDir,
			tempDir:       server.tempDir,
		}),
	})
}

type publicHygieneResponse struct {
	name   string
	status int
	body   map[string]any
}

type publicHygieneForbidden struct {
	recordingsDir string
	tempDir       string
}

func assertPublicHygieneCounts(t *testing.T, name string, body map[string]any, forbidden publicHygieneForbidden) {
	t.Helper()

	counts := publicHygieneCounts(body, forbidden)
	if counts["rawTransport"] != 0 || counts["localGo2RTC"] != 0 || counts["runtimePath"] != 0 {
		t.Fatalf("%s public hygiene counts = %#v, want all zero", name, counts)
	}
}

func publicHygieneEvidence(responses []publicHygieneResponse, forbidden publicHygieneForbidden) []map[string]any {
	out := make([]map[string]any, 0, len(responses))
	for _, response := range responses {
		out = append(out, map[string]any{
			"name":   response.name,
			"status": response.status,
			"counts": publicHygieneCounts(response.body, forbidden),
		})
	}
	return out
}

func publicHygieneCounts(body map[string]any, forbidden publicHygieneForbidden) map[string]int {
	encoded, err := json.Marshal(body)
	if err != nil {
		return map[string]int{"marshalError": 1}
	}
	text := string(encoded)
	return map[string]int{
		"rawTransport":   strings.Count(text, "rtsp://"),
		"localGo2RTC":    strings.Count(text, "127.0.0.1:8554") + strings.Count(text, "127.0.0.1:1984"),
		"runtimePath":    strings.Count(text, forbidden.recordingsDir) + strings.Count(text, forbidden.tempDir),
		"workerFieldKey": countJSONKey(body, "input") + countJSONKey(body, "current"),
	}
}
