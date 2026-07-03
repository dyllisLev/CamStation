package backup

import (
	"context"
	"testing"
	"time"

	"camstation/internal/store"
)

type heldCancelCommand struct {
	started    chan struct{}
	cancelSeen chan struct{}
	release    chan struct{}
}

func newHeldCancelCommand() *heldCancelCommand {
	return &heldCancelCommand{
		started:    make(chan struct{}),
		cancelSeen: make(chan struct{}),
		release:    make(chan struct{}),
	}
}

func (h *heldCancelCommand) Run(ctx context.Context, name string, args ...string) error {
	close(h.started)
	<-ctx.Done()
	close(h.cancelSeen)
	<-h.release
	return ctx.Err()
}

func TestRunner_Cancel_whenActiveRunnerOwnsTerminalState_waitsForRunnerFinalizer(t *testing.T) {
	t.Parallel()

	// Given
	db := newBackupTestDB(t)
	source := t.TempDir()
	command := newHeldCancelCommand()
	runner := NewRunner(db, WithCommandRunner(command))
	running, err := runner.Start(t.Context(), StartRequest{Source: source, Prefix: "qa/cancel-owner"})
	if err != nil {
		t.Fatalf("start backup: %v", err)
	}
	<-command.started

	// When
	cancelResult := make(chan struct {
		job store.Job
		err error
	}, 1)
	go func() {
		job, err := runner.Cancel(t.Context(), running.ID)
		cancelResult <- struct {
			job store.Job
			err error
		}{job: job, err: err}
	}()
	<-command.cancelSeen

	// Then
	select {
	case result := <-cancelResult:
		t.Fatalf("Cancel returned before active runner finalized: job=%#v err=%v", result.job, result.err)
	case <-time.After(100 * time.Millisecond):
	}
	close(command.release)
	result := <-cancelResult
	if result.err != nil {
		t.Fatalf("cancel backup: %v", result.err)
	}
	if result.job.State != store.JobStateCancelled {
		t.Fatalf("cancelled state = %s, want cancelled", result.job.State)
	}
}
