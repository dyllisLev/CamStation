package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordingBackupMarkers_marksOnlyReadySegmentsInsideBackupSource(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	source := t.TempDir()
	insidePath := filepath.Join(source, "front", "ready.mp4")
	outsidePath := filepath.Join(t.TempDir(), "side", "ready.mp4")
	inside := createReadySegmentForBackupMarkerTest(t, db, insidePath, 1_783_000_000)
	outside := createReadySegmentForBackupMarkerTest(t, db, outsidePath, 1_783_000_100)

	// When
	marked, err := db.MarkReadyRecordingSegmentsBackedUp(t.Context(), RecordingSegmentsBackupMark{
		JobID:         42,
		UpdatedBefore: inside.UpdatedAt + 10,
		SourceDir:     source,
	})

	// Then
	if err != nil {
		t.Fatalf("mark ready recording segments backed up: %v", err)
	}
	if len(marked) != 1 || marked[0].ID != inside.ID {
		t.Fatalf("marked segments = %#v, want only inside segment %d", marked, inside.ID)
	}
	reloadedInside, err := db.GetRecordingSegmentByID(t.Context(), inside.ID)
	if err != nil {
		t.Fatalf("reload inside segment: %v", err)
	}
	if reloadedInside.BackupState != "backed_up" || reloadedInside.BackupJobID != 42 || reloadedInside.BackedUpAt == "" {
		t.Fatalf("inside backup marker = %#v", reloadedInside)
	}
	reloadedOutside, err := db.GetRecordingSegmentByID(t.Context(), outside.ID)
	if err != nil {
		t.Fatalf("reload outside segment: %v", err)
	}
	if reloadedOutside.BackupState == "backed_up" {
		t.Fatalf("outside segment was marked backed up: %#v", reloadedOutside)
	}
}

func TestRecordingBackupMarkers_listsBackedUpReadySegmentsInsideBackupSource(t *testing.T) {
	t.Parallel()

	// Given
	db := newSettingsJobTestDB(t)
	source := t.TempDir()
	insidePath := filepath.Join(source, "front", "backed-up.mp4")
	outsidePath := filepath.Join(t.TempDir(), "side", "backed-up.mp4")
	inside := createReadySegmentForBackupMarkerTest(t, db, insidePath, 1_783_000_300)
	outside := createReadySegmentForBackupMarkerTest(t, db, outsidePath, 1_783_000_400)
	if _, err := db.db.ExecContext(t.Context(),
		`UPDATE recording_segments
		    SET backup_state = 'backed_up', backed_up_at = ?, backup_job_id = ?
		  WHERE id IN (?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano),
		55,
		inside.ID,
		outside.ID,
	); err != nil {
		t.Fatalf("seed backed up markers: %v", err)
	}

	// When
	segments, err := db.ListReadyBackedUpRecordingSegments(t.Context(), source)

	// Then
	if err != nil {
		t.Fatalf("list ready backed up segments: %v", err)
	}
	if len(segments) != 1 || segments[0].ID != inside.ID {
		t.Fatalf("segments = %#v, want only inside segment %d", segments, inside.ID)
	}
}

func createReadySegmentForBackupMarkerTest(t *testing.T, db *DB, finalPath string, tsStart float64) RecordingSegment {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		t.Fatalf("create segment dir: %v", err)
	}
	if err := os.WriteFile(finalPath, []byte("segment"), 0o644); err != nil {
		t.Fatalf("write segment: %v", err)
	}
	size := int64(7)
	opened, err := db.OpenRecordingSegment(t.Context(), RecordingSegment{
		CameraID:   1,
		StreamName: filepath.Base(filepath.Dir(finalPath)),
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join(t.TempDir(), filepath.Base(finalPath)),
		TSStart:    tsStart,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open recording segment: %v", err)
	}
	if err := db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+30, finalPath, &size); err != nil {
		t.Fatalf("close recording segment: %v", err)
	}
	segment, err := db.GetRecordingSegmentByID(t.Context(), opened.ID)
	if err != nil {
		t.Fatalf("reload created segment: %v", err)
	}
	return segment
}
