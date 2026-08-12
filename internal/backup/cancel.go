package backup

import (
	"context"
	"fmt"
	"time"

	"camstation/internal/store"
)

const cancelWait = 2 * time.Second

type activeRun struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

func (r *Runner) Cancel(ctx context.Context, id int64) (store.Job, error) {
	job, err := r.db.GetJob(ctx, id)
	if err != nil {
		return store.Job{}, err
	}
	if job.Kind != JobKind {
		return store.Job{}, fmt.Errorf("job is not a backup job: %w", store.ErrValidation)
	}
	active, ok := r.activeRun(id)
	if ok {
		active.cancel()
		return r.waitForActiveCancel(ctx, id, active.done)
	}
	if job.State != store.JobStateQueued && job.State != store.JobStateRunning {
		return store.Job{}, store.ErrJobNotFound
	}
	return r.db.CancelJob(ctx, id, "backup cancelled")
}

func (r *Runner) activeRun(id int64) (activeRun, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	active, ok := r.active[id]
	return active, ok
}

func (r *Runner) waitForActiveCancel(ctx context.Context, id int64, done <-chan struct{}) (store.Job, error) {
	timer := time.NewTimer(cancelWait)
	defer timer.Stop()
	select {
	case <-done:
		return r.db.GetJob(ctx, id)
	case <-ctx.Done():
		return store.Job{}, ctx.Err()
	case <-timer.C:
		job, err := r.db.GetJob(ctx, id)
		if err != nil {
			return store.Job{}, err
		}
		if job.State == store.JobStateCancelled {
			return job, nil
		}
		return store.Job{}, fmt.Errorf("backup cancellation did not finish: %w", context.DeadlineExceeded)
	}
}
