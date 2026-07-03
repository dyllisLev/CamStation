package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type fakeStreamController struct {
	status       stream.Status
	restartCalls int
}

func (f *fakeStreamController) Status(context.Context) stream.Status {
	return f.status
}

func (f *fakeStreamController) Restart(context.Context, []store.Camera) error {
	f.restartCalls++
	return nil
}

func newTestRouteServerWithStreamer(t *testing.T, streamer streamController) testRouteServer {
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
		Source: "test", Level: "info", Message: "baseline event",
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
	handler, err := routeDeps{
		db: db, prober: nil, streamer: streamer,
		recorderManager: recorder.New(db, recordingsDir, tempRecordingDir, 5),
		cleaner:         cleanup.New(db, recordingsDir),
		recordingsDir:   recordingsDir, tempDir: tempRecordingDir,
	}.handler()
	if err != nil {
		t.Fatalf("build routes: %v", err)
	}
	return testRouteServer{handler: handler, db: db, recordingsDir: recordingsDir, tempDir: tempRecordingDir}
}

func requestJSONArray(t *testing.T, handler http.Handler, method, target, body string) (int, []map[string]any) {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var payload []map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s %s response: %v; body=%s", method, target, err, rec.Body.String())
		}
	}
	return rec.Code, payload
}

func assertPublicArrayDoesNotContain(t *testing.T, payload []map[string]any, forbidden string) {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal array payload: %v", err)
	}
	if strings.Contains(string(encoded), forbidden) {
		t.Fatalf("public array payload leaked forbidden secret")
	}
}

func assertEncodedDoesNotContain(t *testing.T, payload map[string]any, forbidden string) {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if strings.Contains(string(encoded), forbidden) {
		t.Fatalf("public payload leaked forbidden fragment %q", forbidden)
	}
}
