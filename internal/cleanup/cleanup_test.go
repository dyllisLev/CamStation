package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestEnforceMaxBytesDeletesOldestReadySegments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openTestDB(t, root)
	recordingsDir := filepath.Join(root, "recordings")
	disableUnbackedProtection(t, ctx, db)

	oldPath := addReadySegment(t, ctx, db, recordingsDir, "cam1", "2026-06-30_10-00.mp4", 100, []byte("aaaa"))
	newPath := addReadySegment(t, ctx, db, recordingsDir, "cam1", "2026-06-30_10-05.mp4", 200, []byte("bbbb"))

	result, err := New(db, recordingsDir).EnforceMaxBytes(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	if result.BeforeBytes != 8 || result.AfterBytes != 4 {
		t.Fatalf("bytes = %d -> %d, want 8 -> 4", result.BeforeBytes, result.AfterBytes)
	}
	if len(result.Deleted) != 1 || result.Deleted[0].Filename != filepath.Base(oldPath) {
		t.Fatalf("deleted = %#v, want oldest segment", result.Deleted)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old segment still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new segment should remain: %v", err)
	}

	from := time.Unix(0, 0)
	to := time.Unix(1000, 0)
	ready, err := db.ListRecordingSegments(ctx, "cam1", from, to, "ready")
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].Filename != filepath.Base(newPath) {
		t.Fatalf("ready segments = %#v, want only newest", ready)
	}
}

func TestEnforceMaxBytesDoesNotDeleteRecordingSegments(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openTestDB(t, root)
	recordingsDir := filepath.Join(root, "recordings")
	disableUnbackedProtection(t, ctx, db)

	_ = addReadySegment(t, ctx, db, recordingsDir, "cam1", "2026-06-30_10-00.mp4", 100, []byte("aaaa"))
	activePath := filepath.Join(recordingsDir, "cam1", "2026-06-30", "2026-06-30_10-05.mp4")
	writeFile(t, activePath, []byte("bbbb"))
	if _, err := db.OpenRecordingSegment(ctx, store.RecordingSegment{
		CameraID:   1,
		StreamName: "cam1",
		Filename:   filepath.Base(activePath),
		TempPath:   activePath,
		TSStart:    200,
		Status:     "recording",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := New(db, recordingsDir).EnforceMaxBytes(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("deleted = %#v, want only completed segment", result.Deleted)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("recording segment should remain: %v", err)
	}
}

func TestEnforceMaxBytesProtectsUnbackedSegmentsWhenConfigured(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := openTestDB(t, root)
	recordingsDir := filepath.Join(root, "recordings")
	if err := db.UpdateBackupSettings(ctx, store.BackupSettings{
		Enabled:         true,
		Target:          "gdrive:/cctvTest",
		RetentionDays:   30,
		ScheduleCron:    "0 3 * * *",
		ProtectUnbacked: true,
	}); err != nil {
		t.Fatal(err)
	}

	segmentPath := addReadySegment(t, ctx, db, recordingsDir, "cam1", "2026-06-30_10-00.mp4", 100, []byte("aaaa"))

	result, err := New(db, recordingsDir).EnforceMaxBytes(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deleted) != 0 {
		t.Fatalf("deleted = %#v, want no deletion before backup", result.Deleted)
	}
	if result.AfterBytes != result.BeforeBytes {
		t.Fatalf("bytes changed before backup: %d -> %d", result.BeforeBytes, result.AfterBytes)
	}
	if !result.BackupProtectionActive {
		t.Fatalf("BackupProtectionActive = false, want true")
	}
	if result.ProtectedUnbackedCount != 1 {
		t.Fatalf("ProtectedUnbackedCount = %d, want 1", result.ProtectedUnbackedCount)
	}
	if result.ProtectedUnbackedBytes != 4 {
		t.Fatalf("ProtectedUnbackedBytes = %d, want 4", result.ProtectedUnbackedBytes)
	}
	if _, err := os.Stat(segmentPath); err != nil {
		t.Fatalf("unbacked segment should remain: %v", err)
	}
}

func openTestDB(t *testing.T, root string) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func addReadySegment(t *testing.T, ctx context.Context, db *store.DB, recordingsDir, streamName, filename string, tsStart float64, data []byte) string {
	t.Helper()
	path := filepath.Join(recordingsDir, streamName, "2026-06-30", filename)
	writeFile(t, path, data)
	if _, err := db.OpenRecordingSegment(ctx, store.RecordingSegment{
		CameraID:   1,
		StreamName: streamName,
		Filename:   filename,
		TempPath:   filepath.Join("temp", streamName, "2026-06-30", filename),
		TSStart:    tsStart,
		Status:     "recording",
	}); err != nil {
		t.Fatal(err)
	}
	size := int64(len(data))
	if err := db.CloseRecordingSegment(ctx, streamName, filename, tsStart+300, path, &size); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func disableUnbackedProtection(t *testing.T, ctx context.Context, db *store.DB) {
	t.Helper()
	if err := db.UpdateBackupSettings(ctx, store.BackupSettings{
		Enabled:         true,
		Target:          "gdrive:/cctvTest",
		RetentionDays:   30,
		ScheduleCron:    "0 3 * * *",
		ProtectUnbacked: false,
	}); err != nil {
		t.Fatal(err)
	}
}
