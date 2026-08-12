package recorder

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"camstation/internal/store"
)

func TestRecoverInterruptedSegmentsQuarantinesTempAndMarksFailed(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "camstation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	tempPath := filepath.Join(root, "temp", "cam1", "2026-06-30", "2026-06-30_21-00.mp4")
	if err := os.MkdirAll(filepath.Dir(tempPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempPath, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.OpenRecordingSegment(ctx, store.RecordingSegment{
		CameraID:   1,
		StreamName: "cam1",
		Filename:   filepath.Base(tempPath),
		TempPath:   tempPath,
		TSStart:    1782820800,
		Status:     "recording",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := RecoverInterruptedSegments(ctx, db, filepath.Join(root, "quarantine"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Recovered != 1 || result.Quarantined != 1 || result.FailedMoves != 0 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("temp file still present or unexpected stat error: %v", err)
	}
	quarantined := filepath.Join(root, "quarantine", "temp", "2026-06-30", "cam1", filepath.Base(tempPath))
	if _, err := os.Stat(quarantined); err != nil {
		t.Fatalf("quarantined file missing: %v", err)
	}
	segments, err := db.ListRecordingSegmentsByStatus(ctx, "failed")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 || segments[0].Status != "failed" || segments[0].Error == "" {
		t.Fatalf("failed segments = %#v", segments)
	}
}
