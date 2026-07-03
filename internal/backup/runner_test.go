package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"camstation/internal/store"
)

type commandCall struct {
	name string
	args []string
}

type fakeCommandRunner struct {
	block chan struct{}
	done  chan struct{}
	err   error
	calls []commandCall
}

func (f *fakeCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	f.calls = append(f.calls, commandCall{name: name, args: append([]string(nil), args...)})
	if f.block != nil {
		close(f.done)
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func newBackupTestDB(t *testing.T) *store.DB {
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

func TestRunner_Start_whenConfigValid_usesSafeRcloneCopyAndSucceeds(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	if err := db.UpdateBackupSettings(t.Context(), store.BackupSettings{Enabled: true, Target: "gdrive:/cctvTest", RetentionDays: 7}); err != nil {
		t.Fatalf("update backup settings: %v", err)
	}
	exec := &fakeCommandRunner{}
	runner := NewRunner(db, WithCommandRunner(exec))

	// When
	job, err := runner.Start(t.Context(), StartRequest{Source: source, Prefix: "qa/test"})

	// Then
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	job = waitForBackupState(t, db, job.ID, store.JobStateSucceeded)
	if job.State != store.JobStateSucceeded {
		t.Fatalf("job state = %s, want succeeded", job.State)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("rclone calls = %d, want 1", len(exec.calls))
	}
	call := exec.calls[0]
	if call.name != "rclone" || len(call.args) < 3 || call.args[0] != "copy" {
		t.Fatalf("rclone command = %s %#v", call.name, call.args)
	}
	if call.args[1] != source || call.args[2] != "gdrive:/cctvTest/qa/test" {
		t.Fatalf("rclone args = %#v", call.args)
	}
	for _, arg := range call.args {
		switch arg {
		case "sync", "purge", "delete", "deletefile":
			t.Fatalf("destructive rclone operation present: %#v", call.args)
		}
	}
}

func TestRunner_Start_whenConfiguredRemote_preservesRecordingTreeAndMarksSegmentsBackedUp(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	finalPath := filepath.Join(source, "집-마당", "2026-07-03", "집-마당_2026-07-03_08-35.mp4")
	if err := createBackupFixture(finalPath, []byte("segment")); err != nil {
		t.Fatalf("create backup fixture: %v", err)
	}
	if err := db.UpdateBackupSettings(t.Context(), store.BackupSettings{
		Enabled:                 true,
		Target:                  "gdrive:/cctv-sample",
		RetentionDays:           7,
		ScheduleIntervalMinutes: 1440,
		ProtectUnbacked:         true,
	}); err != nil {
		t.Fatalf("update backup settings: %v", err)
	}
	opened, err := db.OpenRecordingSegment(t.Context(), store.RecordingSegment{
		CameraID:   1,
		StreamName: "yard-recording",
		Filename:   filepath.Base(finalPath),
		TempPath:   filepath.Join("temp", "yard", filepath.Base(finalPath)),
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
	exec := &fakeCommandRunner{}
	runner := NewRunner(db, WithCommandRunner(exec))

	// When
	job, err := runner.Start(t.Context(), StartRequest{Source: source})

	// Then
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	job = waitForBackupState(t, db, job.ID, store.JobStateSucceeded)
	if len(exec.calls) != 1 {
		t.Fatalf("rclone calls = %d, want 1", len(exec.calls))
	}
	call := exec.calls[0]
	if call.args[1] != source || call.args[2] != "gdrive:/cctv-sample" {
		t.Fatalf("rclone copy destination = %#v, want source tree copied under configured target", call.args)
	}
	segment, err := db.GetRecordingSegment(t.Context(), opened.StreamName, opened.TSStart)
	if err != nil {
		t.Fatalf("reload segment: %v", err)
	}
	if segment.BackupState != "backed_up" || segment.BackupJobID != job.ID || segment.BackedUpAt == "" {
		t.Fatalf("segment backup marker = state:%q job:%d at:%q", segment.BackupState, segment.BackupJobID, segment.BackedUpAt)
	}
}

func TestRunner_Start_whenAnotherBackupActive_rejectsDeterministically(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	exec := &fakeCommandRunner{block: make(chan struct{}), done: make(chan struct{})}
	runner := NewRunner(db, WithCommandRunner(exec))

	// When
	first, err := runner.Start(t.Context(), StartRequest{Source: source, Prefix: "qa/lock"})
	if err != nil {
		t.Fatalf("start first backup: %v", err)
	}
	<-exec.done
	_, duplicateErr := runner.Start(t.Context(), StartRequest{Source: source, Prefix: "qa/lock-2"})

	// Then
	if !errors.Is(duplicateErr, store.ErrJobAlreadyActive) {
		t.Fatalf("duplicate error = %v, want ErrJobAlreadyActive", duplicateErr)
	}
	if _, err := runner.Cancel(t.Context(), first.ID); err != nil {
		t.Fatalf("cancel first backup: %v", err)
	}
}

func TestRunner_CancelAndRetry_whenJobFailed_transitionsThroughSharedJobs(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	blockingExec := &fakeCommandRunner{block: make(chan struct{}), done: make(chan struct{})}
	runner := NewRunner(db, WithCommandRunner(blockingExec))
	running, err := runner.Start(t.Context(), StartRequest{Source: source, Prefix: "qa/cancel"})
	if err != nil {
		t.Fatalf("start running backup: %v", err)
	}
	<-blockingExec.done

	// When
	cancelled, err := runner.Cancel(t.Context(), running.ID)

	// Then
	if err != nil {
		t.Fatalf("cancel backup: %v", err)
	}
	if cancelled.State != store.JobStateCancelled {
		t.Fatalf("cancelled state = %s, want cancelled", cancelled.State)
	}

	failingExec := &fakeCommandRunner{err: errors.New("upstream path " + source)}
	failingRunner := NewRunner(db, WithCommandRunner(failingExec))
	failed, err := failingRunner.Start(t.Context(), StartRequest{Source: source, Prefix: "qa/retry"})
	if err != nil {
		t.Fatalf("start failing backup: %v", err)
	}
	failed = waitForBackupState(t, db, failed.ID, store.JobStateFailed)
	if strings.Contains(failed.Error, source) {
		t.Fatalf("failed job leaked source path: %q", failed.Error)
	}

	retryExec := &fakeCommandRunner{}
	failingRunner.SetCommandRunner(retryExec)
	retried, err := failingRunner.Retry(t.Context(), failed.ID)
	if err != nil {
		t.Fatalf("retry backup: %v", err)
	}
	retried = waitForBackupState(t, db, retried.ID, store.JobStateSucceeded)
	if retried.State != store.JobStateSucceeded || len(retryExec.calls) != 1 {
		t.Fatalf("retried job = %#v calls=%d", retried, len(retryExec.calls))
	}
}

func TestRunner_Start_whenInputInvalid_recordsFailedJobAndIncidentWithoutRawInput(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	rawTarget := "../prod"
	if err := db.UpdateBackupSettings(t.Context(), store.BackupSettings{Enabled: true, Target: rawTarget, RetentionDays: 7}); err != nil {
		t.Fatalf("update backup settings: %v", err)
	}
	runner := NewRunner(db, WithCommandRunner(&fakeCommandRunner{}))

	// When
	job, err := runner.Start(t.Context(), StartRequest{Source: source, Prefix: "qa/invalid-target"})

	// Then
	if err != nil {
		t.Fatalf("invalid backup should return failed job, got error: %v", err)
	}
	if job.State != store.JobStateFailed {
		t.Fatalf("job state = %s, want failed", job.State)
	}
	events, err := db.QueryEvents(t.Context(), store.EventQuery{Source: "backup", Limit: 10})
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	encoded := mustMarshalString(t, events)
	if strings.Contains(encoded, rawTarget) || strings.Contains(encoded, "../bad") {
		t.Fatalf("event payload leaked raw invalid input: %s", encoded)
	}
	incidents, err := db.ListIncidents(t.Context(), store.IncidentQuery{Source: "backup", Limit: 10})
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) == 0 {
		t.Fatalf("missing auto incident for failed backup")
	}
}

func TestRunner_Start_whenSourceMissing_recordsFailedJob(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	runner := NewRunner(db, WithCommandRunner(&fakeCommandRunner{}))

	// When
	job, err := runner.Start(t.Context(), StartRequest{Source: "", Prefix: "qa/missing-source"})

	// Then
	if err != nil {
		t.Fatalf("missing source should return failed job, got error: %v", err)
	}
	if job.State != store.JobStateFailed {
		t.Fatalf("job state = %s, want failed", job.State)
	}
	if strings.Contains(mustMarshalString(t, job), "qa/missing-source") {
		t.Fatalf("failed job leaked raw prefix: %#v", job)
	}
}

func waitForBackupState(t *testing.T, db *store.DB, id int64, want store.JobState) store.Job {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := db.GetJob(t.Context(), id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if job.State == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, err := db.GetJob(t.Context(), id)
	if err != nil {
		t.Fatalf("get final job: %v", err)
	}
	t.Fatalf("job %d state = %s, want %s", id, job.State, want)
	return store.Job{}
}

func mustMarshalString(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(encoded)
}

func createBackupFixture(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
