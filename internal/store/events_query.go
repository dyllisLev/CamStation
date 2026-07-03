package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type EventQuery struct {
	Level  string
	Source string
	Search string
	From   time.Time
	To     time.Time
	Cursor string
	Limit  int
}

type EventPage struct {
	Events     []Event `json:"events"`
	NextCursor string  `json:"nextCursor,omitempty"`
	Limit      int     `json:"limit"`
}

type EventPrune struct {
	Confirm bool
	Before  time.Time
	Level   string
	Source  string
	Search  string
	Limit   int
}

type EventPruneResult struct {
	Deleted int64 `json:"deleted"`
}

var (
	discordWebhookRE = regexp.MustCompile(`https://(?:discord\.com|discordapp\.com)/api/webhooks/[A-Za-z0-9._~%-]+/[A-Za-z0-9._~%-]+`)
	cameraURLRE      = regexp.MustCompile(`(?i)\b(?:rtsp|rtsps|onvif)://[^\s"']+|\bhttps?://[^/\s"']*(?:camera|cam|nvr|dvr|onvif)[^\s"']*`)
	credentialURLRE  = regexp.MustCompile(`(?i)\bhttps?://[^/\s"']+:[^@\s"']+@[^\s"']+`)
)

func (d *DB) QueryEvents(ctx context.Context, query EventQuery) (EventPage, error) {
	query.Limit = normalizeEventLimit(query.Limit)
	return d.queryEvents(ctx, query)
}

func (d *DB) queryEvents(ctx context.Context, query EventQuery) (EventPage, error) {
	cursorID, err := parseEventCursor(query.Cursor)
	if err != nil {
		return EventPage{}, err
	}
	where, args := eventWhere(query, cursorID)
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, created_at, source, level, message, details_json FROM events `+where+` ORDER BY id DESC LIMIT ?`,
		append(args, query.Limit+1)...,
	)
	if err != nil {
		return EventPage{}, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0, query.Limit)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return EventPage{}, err
		}
		events = append(events, RedactEvent(event))
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, fmt.Errorf("query events rows: %w", err)
	}
	nextCursor := ""
	if len(events) > query.Limit {
		nextCursor = strconv.FormatInt(events[query.Limit-1].ID, 10)
		events = events[:query.Limit]
	}
	return EventPage{Events: events, NextCursor: nextCursor, Limit: query.Limit}, nil
}

func (d *DB) ExportEvents(ctx context.Context, query EventQuery) ([]Event, error) {
	query.Limit = normalizeExportLimit(query.Limit)
	page, err := d.queryEvents(ctx, query)
	if err != nil {
		return nil, err
	}
	return page.Events, nil
}

func (d *DB) PruneEvents(ctx context.Context, prune EventPrune) (EventPruneResult, error) {
	if !prune.Confirm {
		return EventPruneResult{}, fmt.Errorf("confirm=true is required: %w", ErrValidation)
	}
	if prune.Before.IsZero() && strings.TrimSpace(prune.Level) == "" && strings.TrimSpace(prune.Source) == "" && strings.TrimSpace(prune.Search) == "" {
		return EventPruneResult{}, fmt.Errorf("at least one safe prune filter is required: %w", ErrValidation)
	}
	if prune.Limit <= 0 || prune.Limit > 1000 {
		return EventPruneResult{}, fmt.Errorf("limit must be between 1 and 1000: %w", ErrValidation)
	}
	where, args := eventPruneWhere(prune)
	res, err := d.db.ExecContext(ctx, `DELETE FROM events WHERE id IN (SELECT id FROM events `+where+` ORDER BY id ASC LIMIT ?)`, append(args, prune.Limit)...)
	if err != nil {
		return EventPruneResult{}, fmt.Errorf("prune events: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return EventPruneResult{}, fmt.Errorf("prune event count: %w", err)
	}
	return EventPruneResult{Deleted: deleted}, nil
}

func RedactEvent(event Event) Event {
	event.Source = redactString(event.Source)
	event.Level = redactString(event.Level)
	event.Message = redactString(event.Message)
	event.Details = redactDetails(event.Details)
	return event
}

func redactDetails(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}
	redacted := make(map[string]any, len(details))
	for key, value := range details {
		redacted[redactString(key)] = redactValue(value)
	}
	return redacted
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case string:
		return redactString(typed)
	case map[string]any:
		return redactDetails(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, redactValue(item))
		}
		return items
	default:
		return value
	}
}

func redactString(value string) string {
	value = discordWebhookRE.ReplaceAllString(value, "[redacted-discord-webhook]")
	value = credentialURLRE.ReplaceAllString(value, "[redacted-url]")
	return cameraURLRE.ReplaceAllString(value, "[redacted-camera-url]")
}

func scanEvent(scanner interface {
	Scan(dest ...any) error
}) (Event, error) {
	var event Event
	var createdAt, detailsJSON string
	if err := scanner.Scan(&event.ID, &createdAt, &event.Source, &event.Level, &event.Message, &detailsJSON); err != nil {
		return Event{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Event{}, fmt.Errorf("parse event created_at: %w", err)
	}
	event.CreatedAt = parsed
	if err := json.Unmarshal([]byte(detailsJSON), &event.Details); err != nil {
		event.Details = map[string]any{"parseError": err.Error()}
	}
	return event, nil
}

func eventWhere(query EventQuery, cursorID int64) (string, []any) {
	clauses := make([]string, 0)
	args := make([]any, 0)
	if query.Level != "" {
		clauses = append(clauses, "level = ?")
		args = append(args, strings.TrimSpace(query.Level))
	}
	if query.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, strings.TrimSpace(query.Source))
	}
	if query.Search != "" {
		clauses = append(clauses, "(message LIKE ? OR details_json LIKE ?)")
		term := "%" + strings.TrimSpace(query.Search) + "%"
		args = append(args, term, term)
	}
	if !query.From.IsZero() {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, query.From.Format(time.RFC3339Nano))
	}
	if !query.To.IsZero() {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, query.To.Format(time.RFC3339Nano))
	}
	if cursorID > 0 {
		clauses = append(clauses, "id < ?")
		args = append(args, cursorID)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func eventPruneWhere(prune EventPrune) (string, []any) {
	query := EventQuery{Level: prune.Level, Source: prune.Source, Search: prune.Search, To: prune.Before}
	return eventWhere(query, 0)
}

func parseEventCursor(cursor string) (int64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("cursor must be a positive event id: %w", ErrValidation)
	}
	return id, nil
}

func normalizeEventLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func normalizeExportLimit(limit int) int {
	if limit <= 0 || limit > 5000 {
		return 1000
	}
	return limit
}

func eventByID(ctx context.Context, tx *sql.Tx, id int64) (Event, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, created_at, source, level, message, details_json FROM events WHERE id = ?`, id)
	return scanEvent(row)
}
