package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordingSegmentsOps_ListDetailAndDeleteReadyFile_whenFinalizedInsideRecordingsDir(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	recordingsDir := t.TempDir()
	finalPath := filepath.Join(recordingsDir, "front", "2026-07-02", "ready.mp4")
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatalf("create segment dir: %v", err)
	}
	content := []byte("ready-segment")
	if err := os.WriteFile(finalPath, content, 0o644); err != nil {
		t.Fatalf("write ready segment: %v", err)
	}
	size := int64(len(content))
	opened, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   7,
		StreamName: "front-record",
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join(t.TempDir(), "ready.mp4"),
		TSStart:    1_783_000_000,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open recording segment: %v", err)
	}
	if err := db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+30, finalPath, &size); err != nil {
		t.Fatalf("close recording segment: %v", err)
	}

	// When
	listed, err := db.ListRecordingSegmentsForConsole(t.Context(), RecordingSegmentFilter{})

	// Then
	if err != nil {
		t.Fatalf("list recording segments: %v", err)
	}
	if len(listed) != 1 || listed[0].Status != "ready" {
		t.Fatalf("listed segments = %#v, want one ready segment", listed)
	}
	detail, err := db.GetRecordingSegmentByID(t.Context(), listed[0].ID)
	if err != nil {
		t.Fatalf("get recording segment by id: %v", err)
	}
	if detail.FinalPath != finalPath || detail.FileSize == nil || *detail.FileSize != size {
		t.Fatalf("detail = %#v, want final path and size", detail)
	}

	deleted, err := db.DeleteReadyRecordingSegmentFile(t.Context(), detail.ID, recordingsDir)
	if err != nil {
		t.Fatalf("delete ready recording segment: %v", err)
	}
	if deleted.Status != "deleted" {
		t.Fatalf("deleted status = %q, want deleted", deleted.Status)
	}
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final file stat error = %v, want not exist", err)
	}
}

func TestRecordingSegmentsOps_DeleteRejectsRecordingAndUnsafeReadyPaths(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	recordingsDir := t.TempDir()
	activePath := filepath.Join(t.TempDir(), "active.mp4")
	if err := os.WriteFile(activePath, []byte("active"), 0o644); err != nil {
		t.Fatalf("write active segment: %v", err)
	}
	active, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   8,
		StreamName: "front-active",
		Filename:   filepath.Base(activePath),
		TempPath:   activePath,
		TSStart:    1_783_000_100,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open active segment: %v", err)
	}
	outsidePath := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outsidePath, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside segment: %v", err)
	}
	unsafeReady, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   9,
		StreamName: "front-unsafe",
		Filename:   filepath.Base(outsidePath),
		FinalPath:  outsidePath,
		TSStart:    1_783_000_200,
		Status:     "ready",
	})
	if err != nil {
		t.Fatalf("open unsafe ready segment: %v", err)
	}

	// When
	_, activeErr := db.DeleteReadyRecordingSegmentFile(t.Context(), active.ID, recordingsDir)
	_, unsafeErr := db.DeleteReadyRecordingSegmentFile(t.Context(), unsafeReady.ID, recordingsDir)

	// Then
	if !errors.Is(activeErr, ErrRecordingSegmentNotReady) {
		t.Fatalf("active delete error = %v, want ErrRecordingSegmentNotReady", activeErr)
	}
	if !errors.Is(unsafeErr, ErrRecordingSegmentUnsafePath) {
		t.Fatalf("unsafe delete error = %v, want ErrRecordingSegmentUnsafePath", unsafeErr)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active file should remain: %v", err)
	}
	if _, err := os.Stat(outsidePath); err != nil {
		t.Fatalf("outside file should remain: %v", err)
	}
}
