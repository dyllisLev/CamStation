package main

import (
	"context"
	"errors"
	"time"

	"camstation/internal/backup"
	"camstation/internal/store"
)

func startBackupScheduler(ctx context.Context, db *store.DB, runner *backup.Runner, source string) {
	ticker := time.NewTicker(time.Minute)
	go func() {
		defer ticker.Stop()
		runBackupScheduleTick(ctx, db, runner, source)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runBackupScheduleTick(ctx, db, runner, source)
			}
		}
	}()
}

func runBackupScheduleTick(ctx context.Context, db *store.DB, runner *backup.Runner, source string) {
	job, started, err := runner.StartScheduledDue(ctx, source, time.Now().UTC())
	if err != nil {
		level := "error"
		message := "scheduled backup start failed"
		if errors.Is(err, store.ErrJobAlreadyActive) {
			level = "info"
			message = "scheduled backup skipped because another backup is active"
		}
		_ = db.AppendEvent(context.Background(), store.Event{
			Source:  "backup.scheduler",
			Level:   level,
			Message: message,
			Details: map[string]any{"scheduled": true},
		})
		return
	}
	if !started {
		return
	}
	_ = db.AppendEvent(context.Background(), store.Event{
		Source:  "backup.scheduler",
		Level:   "info",
		Message: "scheduled backup started",
		Details: map[string]any{"jobId": job.ID, "scheduled": true},
	})
}
