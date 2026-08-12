package backup

import (
	"context"
	"time"

	"camstation/internal/cronexpr"
	"camstation/internal/store"
)

type ScheduleStatus struct {
	Enabled          bool       `json:"enabled"`
	Cron             string     `json:"cron"`
	Due              bool       `json:"due"`
	BlockedReason    string     `json:"blockedReason,omitempty"`
	LastSucceededAt  *time.Time `json:"lastSucceededAt,omitempty"`
	LastJobUpdatedAt *time.Time `json:"lastJobUpdatedAt,omitempty"`
	NextRunAt        *time.Time `json:"nextRunAt,omitempty"`
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
	cron := settings.ScheduleCron
	if cron == "" {
		cron = "0 3 * * *"
	}
	status := ScheduleStatus{
		Enabled: settings.Enabled && settings.ScheduleEnabled,
		Cron:    cron,
	}
	if !settings.Enabled {
		status.BlockedReason = "backup disabled"
		return status
	}
	if !settings.ScheduleEnabled {
		status.BlockedReason = "schedule disabled"
		return status
	}
	schedule, err := cronexpr.Parse(cron)
	if err != nil {
		status.BlockedReason = "schedule invalid"
		return status
	}
	lastSucceeded := latestSucceededBackup(jobs)
	status.LastSucceededAt = lastSucceeded
	base := now.In(kst()).Add(-time.Minute)
	lastActivity := latestBackupActivity(jobs)
	if lastActivity != nil {
		status.LastJobUpdatedAt = lastActivity
		base = lastActivity.In(kst())
	}
	next, ok := schedule.NextAfter(base)
	if !ok {
		status.BlockedReason = "schedule invalid"
		return status
	}
	status.NextRunAt = &next
	if hasActiveBackup(jobs) {
		status.BlockedReason = "backup already queued or running"
		return status
	}
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

func latestBackupActivity(jobs []store.Job) *time.Time {
	var latest *time.Time
	for index := range jobs {
		job := jobs[index]
		if job.State == store.JobStateDeleted {
			continue
		}
		updatedAt := job.UpdatedAt
		if latest == nil || updatedAt.After(*latest) {
			latest = &updatedAt
		}
	}
	return latest
}

func kst() *time.Location {
	return cronexpr.KST()
}
