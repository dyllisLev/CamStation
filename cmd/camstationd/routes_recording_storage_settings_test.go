package main

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

func TestRecordingsStorageAPI_UsesSavedMaxStorageGB_whenStartupLimitDiffers(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServerWithStartupStorageLimit(t, gbToBytes(0.30))
	status, updated := requestJSON(t, server.handler, http.MethodPut, "/api/settings", `{"recording":{"segmentMinutes":5,"retentionDays":30,"maxStorageGB":5}}`)
	if status != http.StatusOK {
		t.Fatalf("PUT /api/settings status = %d, want %d; body=%#v", status, http.StatusOK, updated)
	}

	// When
	status, storage := requestJSON(t, server.handler, http.MethodGet, "/api/recordings/storage", "")

	// Then
	if status != http.StatusOK {
		t.Fatalf("GET /api/recordings/storage status = %d, want %d; body=%#v", status, http.StatusOK, storage)
	}
	if storage["maxBytes"] != float64(5*bytesPerGB) {
		t.Fatalf("storage maxBytes = %v, want %d", storage["maxBytes"], 5*bytesPerGB)
	}
	if storage["autoCleanupEnabled"] != true {
		t.Fatalf("autoCleanupEnabled = %v, want true", storage["autoCleanupEnabled"])
	}
}

func TestRecordingStorageLimitBytes_UsesFallbackOnlyWhenSettingDisabled(t *testing.T) {
	t.Parallel()

	// Given
	ctx := context.Background()
	db := newStorageLimitTestDB(t)
	fallback := gbToBytes(0.30)

	// When
	initialLimit, err := recordingStorageLimitBytes(ctx, db, fallback)
	if err != nil {
		t.Fatalf("initial storage limit: %v", err)
	}
	_, err = db.UpdateSettings(ctx, store.SettingsUpdate{
		Recording: &store.RecordingSettings{
			SegmentMinutes: 5,
			RetentionDays:  30,
			MaxStorageGB:   5,
		},
	})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	savedLimit, err := recordingStorageLimitBytes(ctx, db, fallback)
	if err != nil {
		t.Fatalf("saved storage limit: %v", err)
	}

	// Then
	if initialLimit != fallback {
		t.Fatalf("initial limit = %d, want fallback %d", initialLimit, fallback)
	}
	if savedLimit != 5*bytesPerGB {
		t.Fatalf("saved limit = %d, want %d", savedLimit, 5*bytesPerGB)
	}
}

func newTestRouteServerWithStartupStorageLimit(t *testing.T, maxStorageBytes int64) testRouteServer {
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

	recordingsDir := filepath.Join(tempDir, "recordings")
	tempRecordingDir := filepath.Join(tempDir, "temp")
	handler, err := routes(
		db,
		nil,
		stream.NewGo2RTC(filepath.Join(tempDir, "go2rtc.yaml")),
		recorder.New(db, recordingsDir, tempRecordingDir, 5),
		cleanup.New(db, recordingsDir),
		recordingsDir,
		tempRecordingDir,
		maxStorageBytes,
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

func newStorageLimitTestDB(t *testing.T) *store.DB {
	t.Helper()

	db, err := store.Open(filepath.Join(t.TempDir(), "camstation.db"))
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
	return db
}
