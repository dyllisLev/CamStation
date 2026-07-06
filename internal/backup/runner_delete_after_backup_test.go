package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestRunner_Start_deletesLocalRecordingFile_whenBackupSucceeds(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	finalPath := filepath.Join(source, "front", "2026-07-03", "front_2026-07-03_12-00.mp4")
	if err := createBackupFixture(finalPath, []byte("segment")); err != nil {
		t.Fatalf("create backup fixture: %v", err)
	}
	if err := db.UpdateBackupSettings(t.Context(), store.BackupSettings{
		Enabled:       true,
		Target:        "gdrive:/cctv-sample",
		RetentionDays: 7,
	}); err != nil {
		t.Fatalf("update backup settings: %v", err)
	}
	opened, err := db.OpenRecordingSegment(t.Context(), store.RecordingSegment{
		CameraID:   1,
		StreamName: "front",
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join("temp", "front", filepath.Base(finalPath)),
		TSStart:    float64(time.Now().Add(-10 * time.Minute).Unix()),
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open recording segment: %v", err)
	}
	size := int64(7)
	if err := db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+300, finalPath, &size); err != nil {
		t.Fatalf("close recording segment: %v", err)
	}
	runner := NewRunner(db, WithCommandRunner(&fakeCommandRunner{}))

	// When
	job, err := runner.Start(t.Context(), StartRequest{Source: source})

	// Then
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	job = waitForBackupState(t, db, job.ID, store.JobStateSucceeded)
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local segment stat error = %v, want file removed", err)
	}
	segment, err := db.GetRecordingSegment(t.Context(), opened.StreamName, opened.TSStart)
	if err != nil {
		t.Fatalf("reload segment: %v", err)
	}
	if segment.Status != "deleted" {
		t.Fatalf("segment status = %q, want deleted", segment.Status)
	}
	if segment.BackupState != "backed_up" || segment.BackupJobID != job.ID || segment.BackedUpAt == "" {
		t.Fatalf("segment backup marker = state:%q job:%d at:%q", segment.BackupState, segment.BackupJobID, segment.BackedUpAt)
	}
	job = reloadBackupJob(t, db, job.ID)
	backupEvent := requireJobEvent(t, job, "recording_backed_up")
	if backupEvent.Message != "파일 백업 완료" || backupEvent.Details["archivePath"] != filepath.ToSlash("front/2026-07-03/front_2026-07-03_12-00.mp4") {
		t.Fatalf("backup event = %#v", backupEvent)
	}
	deleteEvent := requireJobEvent(t, job, "recording_local_deleted")
	if deleteEvent.Message != "로컬 파일 삭제 완료" || deleteEvent.Details["archivePath"] != backupEvent.Details["archivePath"] {
		t.Fatalf("delete event = %#v", deleteEvent)
	}
	if _, ok := backupEvent.Details["source"]; ok {
		t.Fatalf("backup event leaked source path: %#v", backupEvent.Details)
	}
}

func TestRunner_Start_deletesExistingBackedUpLocalRecordingFile_whenBackupSucceeds(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	finalPath := filepath.Join(source, "front", "2026-07-03", "front_2026-07-03_12-05.mp4")
	if err := createBackupFixture(finalPath, []byte("segment")); err != nil {
		t.Fatalf("create backup fixture: %v", err)
	}
	if err := db.UpdateBackupSettings(t.Context(), store.BackupSettings{
		Enabled:       true,
		Target:        "gdrive:/cctv-sample",
		RetentionDays: 7,
	}); err != nil {
		t.Fatalf("update backup settings: %v", err)
	}
	opened, err := db.OpenRecordingSegment(t.Context(), store.RecordingSegment{
		CameraID:   1,
		StreamName: "front-existing",
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join("temp", "front-existing", filepath.Base(finalPath)),
		TSStart:    float64(time.Now().Add(-10 * time.Minute).Unix()),
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open recording segment: %v", err)
	}
	size := int64(7)
	if err := db.CloseRecordingSegment(t.Context(), opened.StreamName, opened.Filename, opened.TSStart+300, finalPath, &size); err != nil {
		t.Fatalf("close recording segment: %v", err)
	}
	marked, err := db.MarkReadyRecordingSegmentsBackedUp(t.Context(), store.RecordingSegmentsBackupMark{
		JobID:         10,
		UpdatedBefore: time.Now().Unix() + 10,
		SourceDir:     source,
	})
	if err != nil {
		t.Fatalf("pre-mark segment backed up: %v", err)
	}
	if len(marked) != 1 {
		t.Fatalf("pre-marked segments = %#v, want 1", marked)
	}
	runner := NewRunner(db, WithCommandRunner(&fakeCommandRunner{}))

	// When
	job, err := runner.Start(t.Context(), StartRequest{Source: source})

	// Then
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	waitForBackupState(t, db, job.ID, store.JobStateSucceeded)
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("existing backed-up local segment stat error = %v, want file removed", err)
	}
}

func reloadBackupJob(t *testing.T, db *store.DB, id int64) store.Job {
	t.Helper()

	job, err := db.GetJob(t.Context(), id)
	if err != nil {
		t.Fatalf("reload backup job: %v", err)
	}
	return job
}

func requireJobEvent(t *testing.T, job store.Job, eventType string) store.JobEvent {
	t.Helper()

	for _, event := range job.Events {
		if event.Type == eventType {
			return event
		}
	}
	t.Fatalf("job events missing %q: %#v", eventType, job.Events)
	return store.JobEvent{}
}
