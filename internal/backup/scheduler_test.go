package backup

import (
	"testing"
	"time"

	"camstation/internal/store"
)

func TestScheduleStatus_usesCronExpressionForNextRun_whenScheduleEnabled(t *testing.T) {
	t.Parallel()

	// Given
	now := time.Date(2026, 7, 3, 2, 59, 0, 0, kst())
	settings := store.BackupSettings{
		Enabled:         true,
		ScheduleEnabled: true,
		ScheduleCron:    "0 3 * * *",
	}

	// When
	status := scheduleStatus(settings, nil, now)

	// Then
	if status.Due {
		t.Fatalf("due = true, want false before cron time")
	}
	if status.Cron != "0 3 * * *" {
		t.Fatalf("cron = %q, want configured expression", status.Cron)
	}
	wantNext := time.Date(2026, 7, 3, 3, 0, 0, 0, kst())
	if status.NextRunAt == nil || !status.NextRunAt.Equal(wantNext) {
		t.Fatalf("next run = %v, want %v", status.NextRunAt, wantNext)
	}
}

func TestScheduleStatus_isDueOncePerCronSlot_whenNoBackupHasRun(t *testing.T) {
	t.Parallel()

	// Given
	now := time.Date(2026, 7, 3, 3, 0, 5, 0, kst())
	settings := store.BackupSettings{
		Enabled:         true,
		ScheduleEnabled: true,
		ScheduleCron:    "0 3 * * *",
	}

	// When
	status := scheduleStatus(settings, nil, now)

	// Then
	if !status.Due {
		t.Fatalf("due = false, want true at cron time")
	}
}

func TestScheduleStatus_movesToNextCronSlot_afterBackupJobUpdated(t *testing.T) {
	t.Parallel()

	// Given
	now := time.Date(2026, 7, 3, 3, 6, 0, 0, kst())
	settings := store.BackupSettings{
		Enabled:         true,
		ScheduleEnabled: true,
		ScheduleCron:    "0 3 * * *",
	}
	jobs := []store.Job{{
		State:     store.JobStateSucceeded,
		UpdatedAt: time.Date(2026, 7, 3, 3, 5, 0, 0, kst()),
	}}

	// When
	status := scheduleStatus(settings, jobs, now)

	// Then
	if status.Due {
		t.Fatalf("due = true, want false after this cron slot was handled")
	}
	wantNext := time.Date(2026, 7, 4, 3, 0, 0, 0, kst())
	if status.NextRunAt == nil || !status.NextRunAt.Equal(wantNext) {
		t.Fatalf("next run = %v, want %v", status.NextRunAt, wantNext)
	}
}

func TestRunner_StartScheduledDue_doesNotCreateDuplicateJob_whenBackupAlreadyActive(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	if err := db.UpdateBackupSettings(t.Context(), store.BackupSettings{
		Enabled:         true,
		Target:          "gdrive:/cctvTest",
		RetentionDays:   7,
		ScheduleEnabled: true,
		ScheduleCron:    "* * * * *",
		ProtectUnbacked: true,
	}); err != nil {
		t.Fatalf("update backup settings: %v", err)
	}
	exec := &fakeCommandRunner{block: make(chan struct{}), done: make(chan struct{})}
	runner := NewRunner(db, WithCommandRunner(exec))
	now := time.Date(2026, 7, 3, 3, 0, 5, 0, kst())

	// When
	first, started, err := runner.StartScheduledDue(t.Context(), source, now)
	if err != nil {
		t.Fatalf("start scheduled backup: %v", err)
	}
	if !started {
		t.Fatalf("started = false, want first scheduled backup to start")
	}
	<-exec.done
	_, duplicateStarted, duplicateErr := runner.StartScheduledDue(t.Context(), source, now.Add(time.Minute))

	// Then
	if duplicateErr != nil {
		t.Fatalf("duplicate scheduled start error = %v, want nil skip", duplicateErr)
	}
	if duplicateStarted {
		t.Fatalf("duplicate scheduled start = true, want skip while active")
	}
	jobs, err := db.ListJobsByKind(t.Context(), JobKind, 10)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != first.ID {
		t.Fatalf("jobs = %#v, want only first active scheduled job", jobs)
	}
	if _, err := runner.Cancel(t.Context(), first.ID); err != nil {
		t.Fatalf("cancel first scheduled backup: %v", err)
	}
}
