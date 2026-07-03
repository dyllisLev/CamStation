package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrViewerCommandNotFound = errors.New("viewer command not found")

type ViewerCommandState string

const (
	ViewerCommandPending      ViewerCommandState = "pending"
	ViewerCommandSent         ViewerCommandState = "sent"
	ViewerCommandAcknowledged ViewerCommandState = "acknowledged"
	ViewerCommandFailed       ViewerCommandState = "failed"
	ViewerCommandCancelled    ViewerCommandState = "cancelled"
	ViewerCommandDeleted      ViewerCommandState = "deleted"
)

type ViewerCommandCreate struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Route   string `json:"route"`
	Mode    string `json:"mode"`
}

type ViewerCommandUpdate struct {
	State ViewerCommandState `json:"state"`
	Error string             `json:"error"`
}

type ViewerCommand struct {
	ID          int64              `json:"id"`
	ViewerID    string             `json:"viewerId"`
	Type        string             `json:"type"`
	Message     string             `json:"message,omitempty"`
	Route       string             `json:"route,omitempty"`
	Mode        string             `json:"mode,omitempty"`
	State       ViewerCommandState `json:"state"`
	Error       string             `json:"error,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	SentAt      *time.Time         `json:"sentAt,omitempty"`
	CompletedAt *time.Time         `json:"completedAt,omitempty"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

func (d *DB) CreateViewerCommand(ctx context.Context, viewerID string, req ViewerCommandCreate) (ViewerCommand, error) {
	if _, err := d.GetViewer(ctx, viewerID, 90*time.Second); err != nil {
		return ViewerCommand{}, err
	}
	req.Type = RedactText(strings.TrimSpace(req.Type))
	req.Message = RedactText(strings.TrimSpace(req.Message))
	req.Route = RedactText(strings.TrimSpace(req.Route))
	req.Mode = RedactText(strings.TrimSpace(req.Mode))
	if req.Type == "" {
		return ViewerCommand{}, fmt.Errorf("command type is required: %w", ErrValidation)
	}
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO viewer_commands(viewer_id, type, message, route, mode, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(viewerID), req.Type, req.Message, req.Route, req.Mode, ViewerCommandPending,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return ViewerCommand{}, fmt.Errorf("create viewer command: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ViewerCommand{}, fmt.Errorf("created viewer command id: %w", err)
	}
	return d.GetViewerCommand(ctx, viewerID, id)
}

func (d *DB) DequeueViewerCommands(ctx context.Context, viewerID string) ([]ViewerCommand, error) {
	now := time.Now().UTC()
	if _, err := d.db.ExecContext(ctx,
		`UPDATE viewer_commands SET state = ?, sent_at = COALESCE(sent_at, ?), updated_at = ?
		 WHERE viewer_id = ? AND state = ?`,
		ViewerCommandSent, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		strings.TrimSpace(viewerID), ViewerCommandPending,
	); err != nil {
		return nil, fmt.Errorf("mark viewer commands sent: %w", err)
	}
	return d.ListViewerCommands(ctx, viewerID)
}

func (d *DB) ListViewerCommands(ctx context.Context, viewerID string) ([]ViewerCommand, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, viewer_id, type, message, route, mode, state, error, created_at, sent_at, completed_at, updated_at
		 FROM viewer_commands
		 WHERE viewer_id = ? AND state IN (?, ?)
		 ORDER BY id`,
		strings.TrimSpace(viewerID), ViewerCommandPending, ViewerCommandSent,
	)
	if err != nil {
		return nil, fmt.Errorf("list viewer commands: %w", err)
	}
	defer rows.Close()
	commands := make([]ViewerCommand, 0)
	for rows.Next() {
		command, err := scanViewerCommand(rows)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func (d *DB) GetViewerCommand(ctx context.Context, viewerID string, id int64) (ViewerCommand, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, viewer_id, type, message, route, mode, state, error, created_at, sent_at, completed_at, updated_at
		 FROM viewer_commands WHERE viewer_id = ? AND id = ?`,
		strings.TrimSpace(viewerID), id,
	)
	return scanViewerCommand(row)
}

func (d *DB) UpdateViewerCommand(ctx context.Context, viewerID string, id int64, req ViewerCommandUpdate) (ViewerCommand, error) {
	if req.State != ViewerCommandAcknowledged && req.State != ViewerCommandFailed {
		return ViewerCommand{}, fmt.Errorf("unsupported command state: %w", ErrValidation)
	}
	return d.finishViewerCommand(ctx, viewerID, id, req.State, req.Error)
}

func (d *DB) CancelViewerCommand(ctx context.Context, viewerID string, id int64, reason string) (ViewerCommand, error) {
	return d.finishViewerCommand(ctx, viewerID, id, ViewerCommandCancelled, reason)
}

func (d *DB) DeleteViewerCommand(ctx context.Context, viewerID string, id int64) (ViewerCommand, error) {
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`UPDATE viewer_commands SET state = ?, error = '', completed_at = ?, updated_at = ?
		 WHERE viewer_id = ? AND id = ? AND state IN (?, ?, ?)`,
		ViewerCommandDeleted, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		strings.TrimSpace(viewerID), id, ViewerCommandPending, ViewerCommandSent, ViewerCommandCancelled,
	)
	if err != nil {
		return ViewerCommand{}, fmt.Errorf("delete viewer command: %w", err)
	}
	if err := requireViewerCommandChanged(res); err != nil {
		return ViewerCommand{}, err
	}
	return d.GetViewerCommand(ctx, viewerID, id)
}

func (d *DB) finishViewerCommand(ctx context.Context, viewerID string, id int64, state ViewerCommandState, message string) (ViewerCommand, error) {
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`UPDATE viewer_commands SET state = ?, error = ?, completed_at = ?, updated_at = ?
		 WHERE viewer_id = ? AND id = ? AND state IN (?, ?)`,
		state, RedactText(strings.TrimSpace(message)), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		strings.TrimSpace(viewerID), id, ViewerCommandPending, ViewerCommandSent,
	)
	if err != nil {
		return ViewerCommand{}, fmt.Errorf("finish viewer command: %w", err)
	}
	if err := requireViewerCommandChanged(res); err != nil {
		return ViewerCommand{}, err
	}
	return d.GetViewerCommand(ctx, viewerID, id)
}

func scanViewerCommand(row scanner) (ViewerCommand, error) {
	var command ViewerCommand
	var createdAt, updatedAt string
	var sentAt, completedAt sql.NullString
	if err := row.Scan(&command.ID, &command.ViewerID, &command.Type, &command.Message, &command.Route,
		&command.Mode, &command.State, &command.Error, &createdAt, &sentAt, &completedAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ViewerCommand{}, ErrViewerCommandNotFound
		}
		return ViewerCommand{}, err
	}
	command.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	command.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if sentAt.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, sentAt.String)
		command.SentAt = &parsed
	}
	if completedAt.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, completedAt.String)
		command.CompletedAt = &parsed
	}
	return command, nil
}

func requireViewerCommandChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if changed == 0 {
		return ErrViewerCommandNotFound
	}
	return nil
}
