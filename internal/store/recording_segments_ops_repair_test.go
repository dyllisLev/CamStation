package store

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRecordingSegmentsOps_DeleteRestoresFile_whenMarkDeletedFailsAfterRemove(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	recordingsDir := t.TempDir()
	finalPath := filepath.Join(recordingsDir, "front", "atomic.mp4")
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatalf("create final dir: %v", err)
	}
	if err := os.WriteFile(finalPath, []byte("atomic"), 0o644); err != nil {
		t.Fatalf("write final file: %v", err)
	}
	size := int64(6)
	opened, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   31,
		StreamName: "front-atomic",
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join(t.TempDir(), "atomic.mp4"),
		TSStart:    1_783_030_100,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if err := db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+30, finalPath, &size); err != nil {
		t.Fatalf("close segment: %v", err)
	}
	if _, err := db.db.ExecContext(t.Context(), `CREATE TRIGGER fail_segment_delete
		BEFORE UPDATE OF status ON recording_segments
		WHEN NEW.status = 'deleted'
		BEGIN SELECT RAISE(FAIL, 'forced delete mark failure'); END`); err != nil {
		t.Fatalf("create failing trigger: %v", err)
	}

	// When
	_, err = db.DeleteReadyRecordingSegmentFile(t.Context(), opened.ID, recordingsDir)

	// Then
	if err == nil {
		t.Fatalf("delete error = nil, want mark failure")
	}
	if _, statErr := os.Stat(finalPath); statErr != nil {
		t.Fatalf("final file should be restored on failure: %v", statErr)
	}
	detail, detailErr := db.GetRecordingSegmentByID(t.Context(), opened.ID)
	if detailErr != nil {
		t.Fatalf("reload segment: %v", detailErr)
	}
	if detail.Status != "ready" {
		t.Fatalf("segment status = %q, want ready", detail.Status)
	}
}

func TestRecordingSegmentsOps_DeleteReadyFile_returnsConflictWithoutPath_whenStagedDeletePathExists(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	recordingsDir := t.TempDir()
	finalPath := filepath.Join(recordingsDir, "front", "collision.mp4")
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatalf("create final dir: %v", err)
	}
	if err := os.WriteFile(finalPath, []byte("collision"), 0o644); err != nil {
		t.Fatalf("write final file: %v", err)
	}
	size := int64(9)
	opened, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   33,
		StreamName: "front-collision",
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join(t.TempDir(), "collision.mp4"),
		TSStart:    1_783_030_300,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if err := db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+30, finalPath, &size); err != nil {
		t.Fatalf("close segment: %v", err)
	}
	stagedPath := finalPath + ".deleting-" + strconv.FormatInt(opened.ID, 10)
	if err := os.WriteFile(stagedPath, []byte("staged"), 0o644); err != nil {
		t.Fatalf("write staged file: %v", err)
	}

	// When
	_, err = db.DeleteReadyRecordingSegmentFile(t.Context(), opened.ID, recordingsDir)

	// Then
	if !errors.Is(err, ErrRecordingSegmentDeleteConflict) {
		t.Fatalf("delete error = %v, want ErrRecordingSegmentDeleteConflict", err)
	}
	if strings.Contains(err.Error(), stagedPath) || strings.Contains(err.Error(), recordingsDir) {
		t.Fatalf("delete collision error exposed an internal path")
	}
}

func TestRecordingSegmentsOps_DeleteRestoresFile_whenMarkDeletedAffectsZeroRows(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	recordingsDir := t.TempDir()
	finalPath := filepath.Join(recordingsDir, "front", "zero-row.mp4")
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatalf("create final dir: %v", err)
	}
	if err := os.WriteFile(finalPath, []byte("zero-row"), 0o644); err != nil {
		t.Fatalf("write final file: %v", err)
	}
	size := int64(8)
	opened, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   32,
		StreamName: "front-zero-row",
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join(t.TempDir(), "zero-row.mp4"),
		TSStart:    1_783_030_200,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	if err := db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+30, finalPath, &size); err != nil {
		t.Fatalf("close segment: %v", err)
	}
	if _, err := db.db.ExecContext(t.Context(), `CREATE TRIGGER ignore_segment_delete
		BEFORE UPDATE OF status ON recording_segments
		WHEN NEW.status = 'deleted'
		BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatalf("create ignore trigger: %v", err)
	}

	// When
	_, err = db.DeleteReadyRecordingSegmentFile(t.Context(), opened.ID, recordingsDir)

	// Then
	if !errors.Is(err, ErrRecordingSegmentNotReady) {
		t.Fatalf("delete error = %v, want ErrRecordingSegmentNotReady", err)
	}
	if _, statErr := os.Stat(finalPath); statErr != nil {
		t.Fatalf("final file should be restored on zero-row mark: %v", statErr)
	}
	detail, detailErr := db.GetRecordingSegmentByID(t.Context(), opened.ID)
	if detailErr != nil {
		t.Fatalf("reload segment: %v", detailErr)
	}
	if detail.Status != "ready" {
		t.Fatalf("segment status = %q, want ready", detail.Status)
	}
}

func TestRecordingSegmentsOps_RejectsNonReadyAndMissingFinalFiles(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	recordingsDir := t.TempDir()
	statuses := []string{"finalizing", "failed", "deleted", "recording"}

	// When / Then
	for index, status := range statuses {
		segment, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
			CameraID:   int64(40 + index),
			StreamName: "front-" + status,
			Filename:   status + ".mp4",
			TempPath:   filepath.Join(t.TempDir(), status+".mp4"),
			FinalPath:  filepath.Join(recordingsDir, status+".mp4"),
			TSStart:    float64(1_783_040_000 + index),
			Status:     status,
		})
		if err != nil {
			t.Fatalf("open %s segment: %v", status, err)
		}
		_, err = db.DeleteReadyRecordingSegmentFile(t.Context(), segment.ID, recordingsDir)
		if !errors.Is(err, ErrRecordingSegmentNotReady) {
			t.Fatalf("%s delete error = %v, want ErrRecordingSegmentNotReady", status, err)
		}
	}
	missing, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   50,
		StreamName: "front-missing-store",
		Filename:   "missing.mp4",
		FinalPath:  filepath.Join(recordingsDir, "missing.mp4"),
		TSStart:    1_783_050_000,
		Status:     "ready",
	})
	if err != nil {
		t.Fatalf("open missing ready segment: %v", err)
	}
	_, err = db.DeleteReadyRecordingSegmentFile(t.Context(), missing.ID, recordingsDir)
	if !errors.Is(err, ErrRecordingSegmentFileMissing) {
		t.Fatalf("missing delete error = %v, want ErrRecordingSegmentFileMissing", err)
	}
}
