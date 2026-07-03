package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrJobAlreadyActive = errors.New("job already active")
	ErrJobNotFound      = errors.New("job not found")
)

func (d *DB) CreateJob(ctx context.Context, create JobCreate) (Job, error) {
	create.Kind = strings.TrimSpace(create.Kind)
	create.SingleFlightKey = strings.TrimSpace(create.SingleFlightKey)
	if create.Kind == "" {
		return Job{}, fmt.Errorf("job kind is required: %w", ErrValidation)
	}
	if jobStringContainsSecret(create.Kind) {
		return Job{}, fmt.Errorf("job kind must not contain secret-like value: %w", ErrValidation)
	}
	if create.TimeoutSeconds < 0 {
		return Job{}, fmt.Errorf("timeoutSeconds must be zero or positive: %w", ErrValidation)
	}
	if jobStringContainsSecret(create.SingleFlightKey) {
		return Job{}, fmt.Errorf("singleFlightKey must not contain secret-like value: %w", ErrValidation)
	}
	if create.SingleFlightKey != "" {
		active, err := d.activeJobForKey(ctx, create.SingleFlightKey)
		if err != nil {
			return Job{}, err
		}
		if active {
			return Job{}, ErrJobAlreadyActive
		}
	}
	now := time.Now().UTC()
	result, err := d.db.ExecContext(ctx,
		`INSERT INTO jobs(kind, single_flight_key, state, timeout_seconds, result_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '{}', ?, ?)`,
		create.Kind,
		create.SingleFlightKey,
		JobStateQueued,
		create.TimeoutSeconds,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		if create.SingleFlightKey != "" {
			return Job{}, ErrJobAlreadyActive
		}
		return Job{}, fmt.Errorf("create job: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Job{}, fmt.Errorf("created job id: %w", err)
	}
	if err := d.appendJobEvent(ctx, id, "queued", "job queued", map[string]any{"kind": create.Kind}); err != nil {
		return Job{}, err
	}
	return d.GetJob(ctx, id)
}

func (d *DB) GetJob(ctx context.Context, id int64) (Job, error) {
	job, err := d.getJob(ctx, id)
	if err != nil {
		return Job{}, err
	}
	events, err := d.listJobEvents(ctx, id)
	if err != nil {
		return Job{}, err
	}
	job.Events = events
	return job, nil
}

func (d *DB) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, kind, single_flight_key, state, timeout_seconds, error, result_json,
		        created_at, started_at, completed_at, updated_at
		 FROM jobs
		 ORDER BY id DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]Job, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) StartJob(ctx context.Context, id int64) (Job, error) {
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`UPDATE jobs
		 SET state = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		 WHERE id = ? AND state = ?`,
		JobStateRunning,
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		id,
		JobStateQueued,
	)
	if err != nil {
		return Job{}, fmt.Errorf("start job: %w", err)
	}
	if err := requireChanged(res); err != nil {
		return Job{}, err
	}
	if err := d.appendJobEvent(ctx, id, "running", "job started", nil); err != nil {
		return Job{}, err
	}
	return d.GetJob(ctx, id)
}

func (d *DB) activeJobForKey(ctx context.Context, key string) (bool, error) {
	var id int64
	err := d.db.QueryRowContext(ctx, `SELECT id FROM jobs WHERE single_flight_key = ? AND state IN (?, ?) LIMIT 1`, key, JobStateQueued, JobStateRunning).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query active job: %w", err)
	}
	return true, nil
}

func (d *DB) getJob(ctx context.Context, id int64) (Job, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, kind, single_flight_key, state, timeout_seconds, error, result_json,
		        created_at, started_at, completed_at, updated_at
		 FROM jobs
		 WHERE id = ?`,
		id,
	)
	return scanJob(row)
}
