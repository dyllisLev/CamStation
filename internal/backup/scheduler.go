package backup

import (
	"context"
	"time"

	"camstation/internal/store"
)

type ScheduleStatus struct {
	Enabled         bool       `json:"enabled"`
	IntervalMinutes int        `json:"intervalMinutes"`
	Due             bool       `json:"due"`
	BlockedReason   string     `json:"blockedReason,omitempty"`
	LastSucceededAt *time.Time `json:"lastSucceededAt,omitempty"`
	NextRunAt       *time.Time `json:"nextRunAt,omitempty"`
}

func (r *Runner) StartScheduledDue(ctx context.Context, source string, now time.Time) (store.Job, bool, error) {
	settings, err := r.db.GetSettings(ctx)
	if err != nil {
		return store.Job{}, false, err
	}
	jobs, err := r.db.ListJobsByKind(ctx, JobKind, 50)
	if err != nil {
		return store.Job{}, false, err
	}
	status := scheduleStatus(settings.Backup, jobs, now.UTC())
	if !status.Due {
		return store.Job{}, false, nil
	}
	job, err := r.Start(ctx, StartRequest{Source: source})
	if err != nil {
		return store.Job{}, false, err
	}
	return job, true, nil
}

func scheduleStatus(settings store.BackupSettings, jobs []store.Job, now time.Time) ScheduleStatus {
	status := ScheduleStatus{
		Enabled:         settings.Enabled && settings.ScheduleEnabled,
		IntervalMinutes: settings.ScheduleIntervalMinutes,
	}
	if status.IntervalMinutes <= 0 {
		status.IntervalMinutes = int(defaultTimeout / time.Minute)
	}
	if !settings.Enabled {
		status.BlockedReason = "backup disabled"
		return status
	}
	if !settings.ScheduleEnabled {
		status.BlockedReason = "schedule disabled"
		return status
	}
	if hasActiveBackup(jobs) {
		status.BlockedReason = "backup already active"
		return status
	}
	lastSucceeded := latestSucceededBackup(jobs)
	if lastSucceeded == nil {
		status.Due = true
		next := now
		status.NextRunAt = &next
		return status
	}
	status.LastSucceededAt = lastSucceeded
	next := lastSucceeded.Add(time.Duration(status.IntervalMinutes) * time.Minute)
	status.NextRunAt = &next
	status.Due = !now.Before(next)
	return status
}

func hasActiveBackup(jobs []store.Job) bool {
	for _, job := range jobs {
		if job.State == store.JobStateQueued || job.State == store.JobStateRunning {
			return true
		}
	}
	return false
}

func latestSucceededBackup(jobs []store.Job) *time.Time {
	var latest *time.Time
	for index := range jobs {
		job := jobs[index]
		if job.State != store.JobStateSucceeded || job.CompletedAt == nil {
			continue
		}
		completedAt := *job.CompletedAt
		if latest == nil || completedAt.After(*latest) {
			latest = &completedAt
		}
	}
	return latest
}
