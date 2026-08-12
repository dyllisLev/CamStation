package store

import (
	"context"
	"encoding/json"
	"time"
)

func (d *DB) AppendEvent(ctx context.Context, event Event) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.Level == "" {
		event.Level = "info"
	}
	details := event.Details
	if details == nil {
		details = map[string]any{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx,
		`INSERT INTO events(created_at, source, level, message, details_json) VALUES (?, ?, ?, ?, ?)`,
		event.CreatedAt.Format(time.RFC3339Nano),
		event.Source,
		event.Level,
		event.Message,
		string(encoded),
	)
	if err != nil {
		return err
	}
	if err := d.createIncidentFromEvent(ctx, event); err != nil {
		return err
	}
	return nil
}

func (d *DB) ListEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, created_at, source, level, message, details_json FROM events ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var createdAt, detailsJSON string
		if err := rows.Scan(&event.ID, &createdAt, &event.Source, &event.Level, &event.Message, &detailsJSON); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if err := json.Unmarshal([]byte(detailsJSON), &event.Details); err != nil {
			event.Details = map[string]any{"parseError": err.Error()}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
