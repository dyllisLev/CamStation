package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type jobFinish struct {
	ID           int64
	State        JobState
	Error        string
	Result       map[string]any
	EventType    string
	EventMessage string
}

func (d *DB) SucceedJob(ctx context.Context, id int64, result map[string]any) (Job, error) {
	return d.finishJob(ctx, jobFinish{
		ID:           id,
		State:        JobStateSucceeded,
		Result:       result,
		EventType:    "succeeded",
		EventMessage: "job succeeded",
	})
}

func (d *DB) FailJob(ctx context.Context, id int64, message string, result map[string]any) (Job, error) {
	if strings.TrimSpace(message) == "" {
		message = "job failed"
	}
	return d.finishJob(ctx, jobFinish{
		ID:           id,
		State:        JobStateFailed,
		Error:        message,
		Result:       result,
		EventType:    "failed",
		EventMessage: "job failed",
	})
}

func (d *DB) TimeoutJob(ctx context.Context, id int64) (Job, error) {
	return d.finishJob(ctx, jobFinish{
		ID:           id,
		State:        JobStateFailed,
		Error:        "job timed out",
		Result:       map[string]any{"timeout": true},
		EventType:    "timeout",
		EventMessage: "job timed out",
	})
}

func (d *DB) CancelJob(ctx context.Context, id int64, reason string) (Job, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "job cancelled"
	}
	return d.finishJob(ctx, jobFinish{
		ID:           id,
		State:        JobStateCancelled,
		Error:        reason,
		Result:       map[string]any{"cancelled": true},
		EventType:    "cancelled",
		EventMessage: "job cancelled",
	})
}

func (d *DB) DeleteJob(ctx context.Context, id int64) (Job, error) {
	now := time.Now().UTC()
	encodedResult, err := json.Marshal(sanitizeJobPayload(map[string]any{"deleted": true}))
	if err != nil {
		return Job{}, fmt.Errorf("encode deleted job result: %w", err)
	}
	res, err := d.db.ExecContext(ctx,
		`UPDATE jobs
		 SET state = ?, error = '', result_json = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		 WHERE id = ? AND state != ?`,
		JobStateDeleted,
		string(encodedResult),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		id,
		JobStateDeleted,
	)
	if err != nil {
		return Job{}, fmt.Errorf("delete job: %w", err)
	}
	if err := requireChanged(res); err != nil {
		return Job{}, err
	}
	if err := d.appendJobEvent(ctx, id, "deleted", "job deleted", map[string]any{"state": JobStateDeleted}); err != nil {
		return Job{}, err
	}
	return d.GetJob(ctx, id)
}

func (d *DB) RecoverStaleRunningJobs(ctx context.Context, before time.Time) (int, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id FROM jobs WHERE state = ? AND updated_at < ? ORDER BY id`, JobStateRunning, before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("list stale running jobs: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err := d.finishJob(ctx, staleRunningJobFinish(id)); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func staleRunningJobFinish(id int64) jobFinish {
	return jobFinish{
		ID:           id,
		State:        JobStateFailed,
		Error:        "stale running job recovered after daemon restart",
		Result:       map[string]any{"recovered": true},
		EventType:    "crash_recovered",
		EventMessage: "stale running job marked failed",
	}
}

func (d *DB) finishJob(ctx context.Context, finish jobFinish) (Job, error) {
	now := time.Now().UTC()
	encodedResult, err := json.Marshal(sanitizeJobPayload(finish.Result))
	if err != nil {
		return Job{}, fmt.Errorf("encode job result: %w", err)
	}
	res, err := d.db.ExecContext(ctx,
		`UPDATE jobs
		 SET state = ?, error = ?, result_json = ?, completed_at = ?, updated_at = ?
		 WHERE id = ? AND state IN (?, ?)`,
		finish.State,
		sanitizeJobString(finish.Error),
		string(encodedResult),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		finish.ID,
		JobStateQueued,
		JobStateRunning,
	)
	if err != nil {
		return Job{}, fmt.Errorf("finish job: %w", err)
	}
	if err := requireChanged(res); err != nil {
		return Job{}, err
	}
	if err := d.appendJobEvent(ctx, finish.ID, finish.EventType, finish.EventMessage, map[string]any{"state": finish.State}); err != nil {
		return Job{}, err
	}
	return d.GetJob(ctx, finish.ID)
}

func requireChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if changed == 0 {
		return ErrJobNotFound
	}
	return nil
}
