package backup

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"camstation/internal/store"
)

const (
	JobKind          = "backup"
	singleFlightKey  = "backup:rclone"
	defaultTarget    = "gdrive:/cctvTest"
	defaultTimeout   = 30 * time.Minute
	validationFailed = "backup request validation failed"
)

type StartRequest struct {
	Source         string `json:"source"`
	Prefix         string `json:"prefix,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type CommandRunnerFunc func(ctx context.Context, name string, args ...string) error

func (f CommandRunnerFunc) Run(ctx context.Context, name string, args ...string) error {
	return f(ctx, name, args...)
}

type Runner struct {
	db       *store.DB
	commands CommandRunner
	mu       sync.Mutex
	active   map[int64]activeRun
	requests map[int64]StartRequest
}

type Option func(*Runner)

func WithCommandRunner(commands CommandRunner) Option {
	return func(r *Runner) {
		r.commands = commands
	}
}

func NewRunner(db *store.DB, opts ...Option) *Runner {
	runner := &Runner{
		db:       db,
		commands: osCommandRunner{},
		active:   map[int64]activeRun{},
		requests: map[int64]StartRequest{},
	}
	for _, opt := range opts {
		opt(runner)
	}
	return runner
}

func (r *Runner) SetCommandRunner(commands CommandRunner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = commands
}

func (r *Runner) Start(ctx context.Context, request StartRequest) (store.Job, error) {
	timeout := request.TimeoutSeconds
	if timeout <= 0 {
		timeout = int(defaultTimeout / time.Second)
	}
	job, err := r.db.CreateJob(ctx, store.JobCreate{Kind: JobKind, SingleFlightKey: singleFlightKey, TimeoutSeconds: timeout})
	if err != nil {
		return store.Job{}, err
	}
	r.rememberRequest(job.ID, request)
	return r.startCreatedJob(ctx, job, request)
}

func (r *Runner) Retry(ctx context.Context, id int64) (store.Job, error) {
	job, err := r.db.GetJob(ctx, id)
	if err != nil {
		return store.Job{}, err
	}
	if job.Kind != JobKind || job.State == store.JobStateQueued || job.State == store.JobStateRunning || job.State == store.JobStateDeleted {
		return store.Job{}, fmt.Errorf("backup job cannot be retried from this state: %w", store.ErrValidation)
	}
	request, ok := r.recalledRequest(id)
	if !ok {
		return store.Job{}, fmt.Errorf("backup retry request is unavailable after daemon restart: %w", store.ErrValidation)
	}
	return r.Start(ctx, request)
}

func (r *Runner) Status(ctx context.Context) (map[string]any, error) {
	settings, err := r.db.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := r.db.ListJobsByKind(ctx, JobKind, 50)
	if err != nil {
		return nil, err
	}
	var active *store.Job
	for index := range jobs {
		if jobs[index].State == store.JobStateQueued || jobs[index].State == store.JobStateRunning {
			active = &jobs[index]
			break
		}
	}
	return map[string]any{
		"config":    settings.Backup,
		"activeJob": active,
		"history":   jobs,
		"schedule":  scheduleStatus(settings.Backup, jobs, time.Now().UTC()),
	}, nil
}

func (r *Runner) startCreatedJob(ctx context.Context, job store.Job, request StartRequest) (store.Job, error) {
	settings, err := r.db.GetSettings(ctx)
	if err != nil {
		return store.Job{}, err
	}
	source, destination, err := validateRequest(request, settings.Backup.Target)
	running, startErr := r.db.StartJob(ctx, job.ID)
	if startErr != nil {
		return store.Job{}, startErr
	}
	if err != nil {
		return r.failValidation(ctx, running.ID)
	}
	backupCutoff := time.Now().Unix()
	runCtx, cancel := context.WithTimeout(context.Background(), time.Duration(running.TimeoutSeconds)*time.Second)
	done := make(chan struct{})
	r.mu.Lock()
	r.active[running.ID] = activeRun{cancel: cancel, done: done}
	commands := r.commands
	r.mu.Unlock()
	go r.run(runCtx, running.ID, cancel, done, commands, source, destination, backupCutoff)
	return running, nil
}

func (r *Runner) run(ctx context.Context, id int64, cancel context.CancelFunc, done chan<- struct{}, commands CommandRunner, source string, destination string, backupCutoff int64) {
	defer cancel()
	defer close(done)
	defer r.clearActive(id)
	err := commands.Run(ctx, "rclone", rcloneCopyArgs(source, destination)...)
	if err == nil {
		backedUp, markErr := r.db.MarkReadyRecordingSegmentsBackedUp(context.Background(), store.RecordingSegmentsBackupMark{
			JobID:         id,
			UpdatedBefore: backupCutoff,
			SourceDir:     source,
		})
		if markErr != nil {
			_, _ = r.db.FailJob(context.Background(), id, "backup marker update failed", map[string]any{"operation": "copy"})
			_ = r.db.AppendEvent(context.Background(), failedBackupEvent(id, "backup marker update failed"))
			return
		}
		cleanupSegments, listErr := r.db.ListReadyBackedUpRecordingSegments(context.Background(), source)
		if listErr != nil {
			_, _ = r.db.FailJob(context.Background(), id, "backup local delete failed", map[string]any{
				"operation":        "copy",
				"segmentsBackedUp": len(backedUp),
				"segmentsDeleted":  0,
			})
			_ = r.db.AppendEvent(context.Background(), failedBackupEvent(id, "backup local delete failed"))
			return
		}
		auditBatch := recordingAuditBatch{JobID: id, Source: source, Segments: cleanupSegments}
		if auditErr := r.appendBackedUpAuditEvents(context.Background(), auditBatch); auditErr != nil {
			_, _ = r.db.FailJob(context.Background(), id, "backup audit update failed", map[string]any{
				"operation":        "copy",
				"segmentsBackedUp": len(backedUp),
				"segmentsDeleted":  0,
			})
			_ = r.db.AppendEvent(context.Background(), failedBackupEvent(id, "backup audit update failed"))
			return
		}
		deleted, deleteErr := r.deleteBackedUpSegments(context.Background(), auditBatch)
		if deleteErr != nil {
			_, _ = r.db.FailJob(context.Background(), id, "backup local delete failed", map[string]any{
				"operation":        "copy",
				"segmentsBackedUp": len(backedUp),
				"segmentsDeleted":  deleted,
			})
			_ = r.db.AppendEvent(context.Background(), failedBackupEvent(id, "backup local delete failed"))
			return
		}
		_, _ = r.db.SucceedJob(context.Background(), id, map[string]any{
			"operation":        "copy",
			"target":           destination,
			"segmentsBackedUp": len(backedUp),
			"segmentsDeleted":  deleted,
		})
		return
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		_, _ = r.db.CancelJob(context.Background(), id, "backup cancelled")
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_, _ = r.db.TimeoutJob(context.Background(), id)
		return
	}
	_, _ = r.db.FailJob(context.Background(), id, "rclone copy failed", map[string]any{"operation": "copy"})
	_ = r.db.AppendEvent(context.Background(), failedBackupEvent(id, "rclone copy failed"))
}

func (r *Runner) failValidation(ctx context.Context, id int64) (store.Job, error) {
	job, err := r.db.FailJob(ctx, id, validationFailed, map[string]any{"validation": "failed"})
	if err != nil {
		return store.Job{}, err
	}
	_ = r.db.AppendEvent(ctx, failedBackupEvent(id, validationFailed))
	return job, nil
}

func failedBackupEvent(id int64, message string) store.Event {
	return store.Event{
		Source:  "backup",
		Level:   "error",
		Message: message,
		Details: map[string]any{"jobId": id},
	}
}

func (r *Runner) rememberRequest(id int64, request StartRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests[id] = request
}

func (r *Runner) recalledRequest(id int64) (StartRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	request, ok := r.requests[id]
	return request, ok
}

func (r *Runner) clearActive(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, id)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}
