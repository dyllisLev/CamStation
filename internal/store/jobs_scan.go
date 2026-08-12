package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func scanJob(row scanner) (Job, error) {
	var job Job
	var startedAt, completedAt sql.NullString
	var resultJSON, createdAtText, updatedAtText string
	if err := row.Scan(
		&job.ID,
		&job.Kind,
		&job.SingleFlightKey,
		&job.State,
		&job.TimeoutSeconds,
		&job.Error,
		&resultJSON,
		&createdAtText,
		&startedAt,
		&completedAt,
		&updatedAtText,
	); err != nil {
		if err == sql.ErrNoRows {
			return Job{}, ErrJobNotFound
		}
		return Job{}, err
	}
	result := map[string]any{}
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return Job{}, fmt.Errorf("decode job result: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtText)
	if err != nil {
		return Job{}, fmt.Errorf("parse job created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedAtText)
	if err != nil {
		return Job{}, fmt.Errorf("parse job updated_at: %w", err)
	}
	job.Result = result
	job.CreatedAt = createdAt
	job.UpdatedAt = updatedAt
	if startedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse job started_at: %w", err)
		}
		job.StartedAt = &parsed
	}
	if completedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return Job{}, fmt.Errorf("parse job completed_at: %w", err)
		}
		job.CompletedAt = &parsed
	}
	return sanitizeJob(job), nil
}

func (d *DB) appendJobEvent(ctx context.Context, jobID int64, eventType, message string, details map[string]any) error {
	encoded, err := json.Marshal(sanitizeJobPayload(details))
	if err != nil {
		return fmt.Errorf("encode job event details: %w", err)
	}
	now := time.Now().UTC()
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO job_events(job_id, created_at, type, message, details_json)
		 VALUES (?, ?, ?, ?, ?)`,
		jobID,
		now.Format(time.RFC3339Nano),
		eventType,
		sanitizeJobString(message),
		string(encoded),
	)
	if err != nil {
		return fmt.Errorf("append job event: %w", err)
	}
	return nil
}

func (d *DB) listJobEvents(ctx context.Context, jobID int64) ([]JobEvent, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, job_id, created_at, type, message, details_json
		 FROM job_events
		 WHERE job_id = ?
		 ORDER BY id`,
		jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("list job events: %w", err)
	}
	defer rows.Close()
	events := make([]JobEvent, 0)
	for rows.Next() {
		event, err := scanJobEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanJobEvent(row scanner) (JobEvent, error) {
	var event JobEvent
	var createdAtText, detailsJSON string
	if err := row.Scan(&event.ID, &event.JobID, &createdAtText, &event.Type, &event.Message, &detailsJSON); err != nil {
		return JobEvent{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtText)
	if err != nil {
		return JobEvent{}, fmt.Errorf("parse job event created_at: %w", err)
	}
	event.CreatedAt = createdAt
	event.Details = map[string]any{}
	if err := json.Unmarshal([]byte(detailsJSON), &event.Details); err != nil {
		return JobEvent{}, fmt.Errorf("decode job event details: %w", err)
	}
	return sanitizeJobEvent(event), nil
}
