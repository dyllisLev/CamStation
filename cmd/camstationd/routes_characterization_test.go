package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type testRouteServer struct {
	handler       http.Handler
	db            *store.DB
	recordingsDir string
	tempDir       string
}

func newTestRouteServer(t *testing.T) testRouteServer {
	t.Helper()

	ctx := t.Context()
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
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	if err := db.AppendEvent(ctx, store.Event{
		Source:  "test",
		Level:   "info",
		Message: "baseline event",
		Details: map[string]any{"route": "events"},
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	recordingsDir := filepath.Join(tempDir, "recordings")
	tempRecordingDir := filepath.Join(tempDir, "temp")
	if err := os.MkdirAll(recordingsDir, 0o755); err != nil {
		t.Fatalf("create recordings dir: %v", err)
	}
	if err := os.MkdirAll(tempRecordingDir, 0o755); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	handler, err := routes(
		db,
		nil,
		stream.NewGo2RTC(filepath.Join(tempDir, "go2rtc.yaml")),
		recorder.New(db, recordingsDir, tempRecordingDir, 5),
		cleanup.New(db, recordingsDir),
		recordingsDir,
		tempRecordingDir,
		0,
		false,
	)
	if err != nil {
		t.Fatalf("build routes: %v", err)
	}

	return testRouteServer{
		handler:       handler,
		db:            db,
		recordingsDir: recordingsDir,
		tempDir:       tempRecordingDir,
	}
}

func getJSON(t *testing.T, handler http.Handler, target string) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d; body=%s", target, rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode GET %s response: %v; body=%s", target, err, rec.Body.String())
	}
	return payload
}

func getJSONArray(t *testing.T, handler http.Handler, target string) []map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d; body=%s", target, rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode GET %s response: %v; body=%s", target, err, rec.Body.String())
	}
	return payload
}

func TestRoutesServeConsoleAtRoot(t *testing.T) {
	t.Parallel()

	server := newTestRouteServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	server.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want no redirect", location)
	}
}

func TestRoutesPreserveCoreAPISurface(t *testing.T) {
	t.Parallel()

	server := newTestRouteServer(t)

	health := getJSON(t, server.handler, "/api/health")
	if health["ok"] != true {
		t.Fatalf("health ok = %v, want true", health["ok"])
	}
	if health["mode"] != "development" {
		t.Fatalf("health mode = %v, want development", health["mode"])
	}
	if _, ok := health["startedAt"].(string); !ok {
		t.Fatalf("health startedAt missing or non-string: %#v", health["startedAt"])
	}

	events := getJSONArray(t, server.handler, "/api/events")
	if len(events) == 0 {
		t.Fatalf("events length = 0, want seeded event")
	}
	if events[0]["source"] != "test" || events[0]["message"] != "baseline event" {
		t.Fatalf("first event = %#v, want seeded baseline event", events[0])
	}

	storage := getJSON(t, server.handler, "/api/recordings/storage")
	if storage["recordingsDir"] != publicManagedRecordingsDir {
		t.Fatalf("recordingsDir = %v, want public managed label", storage["recordingsDir"])
	}
	if storage["tempDir"] != publicManagedTempDir {
		t.Fatalf("tempDir = %v, want public managed label", storage["tempDir"])
	}
	if storage["autoCleanupEnabled"] != false {
		t.Fatalf("autoCleanupEnabled = %v, want false", storage["autoCleanupEnabled"])
	}
	for _, field := range []string{"recordingsBytes", "tempBytes", "maxBytes"} {
		if _, ok := storage[field].(float64); !ok {
			t.Fatalf("storage %s missing or non-number: %#v", field, storage[field])
		}
	}

	streamStatus := getJSON(t, server.handler, "/api/streams/status")
	for _, field := range []string{"installed", "running"} {
		if _, ok := streamStatus[field]; !ok {
			t.Fatalf("stream status missing field %s in %#v", field, streamStatus)
		}
	}
	if _, ok := streamStatus["apiUrl"]; ok {
		t.Fatalf("stream status leaked apiUrl: %#v", streamStatus)
	}

	cameras := getJSONArray(t, server.handler, "/api/cameras")
	if len(cameras) != 0 {
		t.Fatalf("cameras length = %d, want empty fixture list", len(cameras))
	}
	layouts := getJSONArray(t, server.handler, "/api/layouts")
	if len(layouts) != 0 {
		t.Fatalf("layouts length = %d, want empty fixture list", len(layouts))
	}

	layoutBody := bytes.NewBufferString(`{"name":"Ops","data":[],"timeline_collapsed":true,"grid_cols":24,"grid_rows":3}`)
	req := httptest.NewRequest(http.MethodPost, "/api/layouts", layoutBody)
	rec := httptest.NewRecorder()
	server.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/layouts status = %d, want %d; body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var layout map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &layout); err != nil {
		t.Fatalf("decode layout response: %v; body=%s", err, rec.Body.String())
	}
	if layout["id"] == "" || layout["name"] != "Ops" {
		t.Fatalf("created layout = %#v, want id and name Ops", layout)
	}
}
