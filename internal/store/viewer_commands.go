package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrViewerCommandNotFound = errors.New("viewer command not found")

type ViewerCommandState string

const (
	ViewerCommandPending      ViewerCommandState = "pending"
	ViewerCommandDelivered    ViewerCommandState = "delivered"
	ViewerCommandAcknowledged ViewerCommandState = "acknowledged"
	ViewerCommandRunning      ViewerCommandState = "running"
	ViewerCommandSucceeded    ViewerCommandState = "succeeded"
	ViewerCommandFailed       ViewerCommandState = "failed"
	ViewerCommandRejected     ViewerCommandState = "rejected"
	ViewerCommandExpired      ViewerCommandState = "expired"
	ViewerCommandCancelled    ViewerCommandState = "cancelled"
	ViewerCommandDeleted      ViewerCommandState = "deleted"

	// ViewerCommandSent keeps source compatibility with the legacy queue name.
	ViewerCommandSent ViewerCommandState = ViewerCommandDelivered
)

type ViewerCommandCreate struct {
	Type           string `json:"type"`
	Message        string `json:"message"`
	Route          string `json:"route"`
	Mode           string `json:"mode"`
	StreamName     string `json:"streamName"`
	DesiredVersion string `json:"desiredVersion"`
	ArtifactSHA256 string `json:"artifactSha256"`
	TTLSeconds     int    `json:"ttlSeconds"`
	Generation     int64  `json:"generation"`
}

type ViewerCommandUpdate struct {
	State        ViewerCommandState `json:"state"`
	Error        string             `json:"error"`
	OperationKey string             `json:"operationKey"`
}

type ViewerCommandResult = ViewerCommandUpdate

type ViewerCommand struct {
	ID             int64              `json:"id"`
	ViewerID       string             `json:"viewerId"`
	Type           string             `json:"type"`
	Message        string             `json:"message,omitempty"`
	Route          string             `json:"route,omitempty"`
	Mode           string             `json:"mode,omitempty"`
	StreamName     string             `json:"streamName,omitempty"`
	DesiredVersion string             `json:"desiredVersion,omitempty"`
	ArtifactSHA256 string             `json:"artifactSha256,omitempty"`
	PayloadHash    string             `json:"payloadHash"`
	TTLSeconds     int                `json:"ttlSeconds"`
	OperationKey   string             `json:"operationKey,omitempty"`
	Generation     int64              `json:"generation"`
	State          ViewerCommandState `json:"state"`
	Error          string             `json:"error,omitempty"`
	CreatedAt      time.Time          `json:"createdAt"`
	SentAt         *time.Time         `json:"sentAt,omitempty"`
	DeliveredAt    *time.Time         `json:"deliveredAt,omitempty"`
	AcknowledgedAt *time.Time         `json:"acknowledgedAt,omitempty"`
	RunningAt      *time.Time         `json:"runningAt,omitempty"`
	ResultAt       *time.Time         `json:"resultAt,omitempty"`
	CompletedAt    *time.Time         `json:"completedAt,omitempty"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

const viewerCommandColumns = `id, viewer_id, type, message, route, mode, stream_name,
		desired_version, artifact_sha256, payload_hash, ttl_seconds, operation_key, generation,
		state, error, created_at, sent_at, delivered_at, acknowledged_at, running_at, result_at,
		completed_at, updated_at`

const viewerCommandSelect = `SELECT ` + viewerCommandColumns + ` FROM viewer_commands`

func (d *DB) CreateViewerCommand(ctx context.Context, viewerID string, req ViewerCommandCreate) (ViewerCommand, error) {
	if _, err := d.GetViewer(ctx, viewerID, 90*time.Second); err != nil {
		return ViewerCommand{}, err
	}
	req, payloadHash, err := prepareViewerCommand(req)
	if err != nil {
		return ViewerCommand{}, err
	}
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO viewer_commands(viewer_id, type, message, route, mode, stream_name, desired_version,
			artifact_sha256, payload_hash, ttl_seconds, generation, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(viewerID), req.Type, req.Message, req.Route, req.Mode, req.StreamName,
		req.DesiredVersion, req.ArtifactSHA256, payloadHash, req.TTLSeconds, req.Generation, ViewerCommandPending,
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

func (d *DB) EnsureViewerUpdateCommand(ctx context.Context, viewerID, version, artifactSHA256 string) (ViewerCommand, error) {
	viewerID = strings.TrimSpace(viewerID)
	version = RedactText(strings.TrimSpace(version))
	artifactSHA256 = strings.TrimSpace(artifactSHA256)
	if version == "" || len(artifactSHA256) != sha256.Size*2 || strings.ToLower(artifactSHA256) != artifactSHA256 {
		return ViewerCommand{}, fmt.Errorf("invalid viewer update target: %w", ErrValidation)
	}
	if _, err := hex.DecodeString(artifactSHA256); err != nil {
		return ViewerCommand{}, fmt.Errorf("invalid viewer update digest: %w", ErrValidation)
	}
	if _, err := d.GetViewer(ctx, viewerID, 90*time.Second); err != nil {
		return ViewerCommand{}, err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return ViewerCommand{}, fmt.Errorf("begin viewer update ensure: %w", err)
	}
	defer tx.Rollback()
	existing, err := scanViewerCommand(tx.QueryRowContext(ctx,
		viewerCommandSelect+` WHERE viewer_id = ? AND type = 'update_app'
			AND desired_version = ? AND artifact_sha256 = ? ORDER BY id LIMIT 1`,
		viewerID, version, artifactSHA256,
	))
	if err == nil && existing.Generation > 0 {
		if err := tx.Commit(); err != nil {
			return ViewerCommand{}, fmt.Errorf("commit existing viewer update: %w", err)
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, ErrViewerCommandNotFound) {
		return ViewerCommand{}, fmt.Errorf("load existing viewer update: %w", err)
	}

	var generation int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(generation), 0) + 1 FROM viewer_commands
		 WHERE viewer_id = ? AND type = 'update_app'`, viewerID,
	).Scan(&generation); err != nil {
		return ViewerCommand{}, fmt.Errorf("allocate viewer update generation: %w", err)
	}
	update := ViewerCommandCreate{
		Type: "update_app", DesiredVersion: version, ArtifactSHA256: artifactSHA256, Generation: generation,
	}
	if existing.ID > 0 {
		update.Message = existing.Message
		update.Route = existing.Route
		update.Mode = existing.Mode
		update.StreamName = existing.StreamName
		update.TTLSeconds = existing.TTLSeconds
	}
	req, payloadHash, err := prepareViewerCommand(update)
	if err != nil {
		return ViewerCommand{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if existing.ID > 0 {
		existing, err = scanViewerCommand(tx.QueryRowContext(ctx,
			`UPDATE viewer_commands SET generation = ?, payload_hash = ?, updated_at = ?
			 WHERE viewer_id = ? AND id = ? RETURNING `+viewerCommandColumns,
			generation, payloadHash, now, viewerID, existing.ID,
		))
		if err != nil {
			return ViewerCommand{}, fmt.Errorf("upgrade viewer update generation: %w", err)
		}
	} else {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO viewer_commands(viewer_id, type, message, route, mode, stream_name, desired_version,
				artifact_sha256, payload_hash, ttl_seconds, generation, state, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			viewerID, req.Type, req.Message, req.Route, req.Mode, req.StreamName, req.DesiredVersion,
			req.ArtifactSHA256, payloadHash, req.TTLSeconds, req.Generation, ViewerCommandPending, now, now,
		)
		if err != nil {
			return ViewerCommand{}, fmt.Errorf("create ensured viewer update: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return ViewerCommand{}, fmt.Errorf("ensured viewer update id: %w", err)
		}
		existing, err = scanViewerCommand(tx.QueryRowContext(ctx,
			viewerCommandSelect+` WHERE viewer_id = ? AND id = ?`, viewerID, id,
		))
		if err != nil {
			return ViewerCommand{}, fmt.Errorf("load ensured viewer update: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ViewerCommand{}, fmt.Errorf("commit viewer update ensure: %w", err)
	}
	return existing, nil
}

func prepareViewerCommand(req ViewerCommandCreate) (ViewerCommandCreate, string, error) {
	req.Type = RedactText(strings.TrimSpace(req.Type))
	req.Message = RedactText(strings.TrimSpace(req.Message))
	req.Route = RedactText(strings.TrimSpace(req.Route))
	req.Mode = RedactText(strings.TrimSpace(req.Mode))
	req.StreamName = RedactText(strings.TrimSpace(req.StreamName))
	req.DesiredVersion = RedactText(strings.TrimSpace(req.DesiredVersion))
	req.ArtifactSHA256 = RedactText(strings.TrimSpace(req.ArtifactSHA256))
	if req.Type == "" {
		return ViewerCommandCreate{}, "", fmt.Errorf("command type is required: %w", ErrValidation)
	}
	if req.TTLSeconds <= 0 {
		req.TTLSeconds = 300
	}
	payload, err := json.Marshal(struct {
		Type           string `json:"type"`
		Message        string `json:"message,omitempty"`
		Route          string `json:"route,omitempty"`
		Mode           string `json:"mode,omitempty"`
		StreamName     string `json:"streamName,omitempty"`
		DesiredVersion string `json:"desiredVersion,omitempty"`
		ArtifactSHA256 string `json:"artifactSha256,omitempty"`
		Generation     int64  `json:"generation"`
	}{req.Type, req.Message, req.Route, req.Mode, req.StreamName, req.DesiredVersion, req.ArtifactSHA256, req.Generation})
	if err != nil {
		return ViewerCommandCreate{}, "", fmt.Errorf("encode viewer command payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	return req, hex.EncodeToString(digest[:]), nil
}

func (d *DB) DequeueViewerCommands(ctx context.Context, viewerID string) ([]ViewerCommand, error) {
	now := time.Now().UTC()
	if _, err := d.db.ExecContext(ctx,
		`UPDATE viewer_commands SET state = ?, sent_at = COALESCE(sent_at, ?),
			delivered_at = COALESCE(delivered_at, ?), updated_at = ?
		 WHERE viewer_id = ? AND state = ?`,
		ViewerCommandDelivered, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		strings.TrimSpace(viewerID), ViewerCommandPending,
	); err != nil {
		return nil, fmt.Errorf("mark viewer commands sent: %w", err)
	}
	return d.ListViewerCommands(ctx, viewerID)
}

func (d *DB) ListViewerCommands(ctx context.Context, viewerID string) ([]ViewerCommand, error) {
	rows, err := d.db.QueryContext(ctx,
		viewerCommandSelect+`
		 WHERE viewer_id = ?
		 ORDER BY id`,
		strings.TrimSpace(viewerID),
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

func (d *DB) DeliverNextViewerCommand(ctx context.Context, viewerID string) (ViewerCommand, bool, error) {
	viewerID = strings.TrimSpace(viewerID)
	if _, err := d.GetViewer(ctx, viewerID, 30*time.Second); err != nil {
		return ViewerCommand{}, false, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return ViewerCommand{}, false, fmt.Errorf("begin viewer command delivery: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`UPDATE viewer_commands SET state = ?, result_at = COALESCE(result_at, ?),
			completed_at = COALESCE(completed_at, ?), updated_at = ?
		 WHERE viewer_id = ? AND state IN (?, ?)
		   AND julianday(created_at) + (ttl_seconds / 86400.0) <= julianday(?)`,
		ViewerCommandExpired, now, now, now, viewerID, ViewerCommandPending, ViewerCommandDelivered, now,
	); err != nil {
		return ViewerCommand{}, false, fmt.Errorf("expire viewer commands: %w", err)
	}

	command, err := scanViewerCommand(tx.QueryRowContext(ctx,
		viewerCommandSelect+` WHERE viewer_id = ? AND state IN (?, ?) ORDER BY id LIMIT 1`,
		viewerID, ViewerCommandPending, ViewerCommandDelivered,
	))
	if errors.Is(err, ErrViewerCommandNotFound) {
		if err := tx.Commit(); err != nil {
			return ViewerCommand{}, false, fmt.Errorf("commit empty viewer command delivery: %w", err)
		}
		return ViewerCommand{}, false, nil
	}
	if err != nil {
		return ViewerCommand{}, false, fmt.Errorf("load viewer command for delivery: %w", err)
	}
	if command.State == ViewerCommandPending {
		if _, err := tx.ExecContext(ctx,
			`UPDATE viewer_commands SET state = ?, sent_at = COALESCE(sent_at, ?),
				delivered_at = COALESCE(delivered_at, ?), updated_at = ?
			 WHERE viewer_id = ? AND id = ? AND state = ?`,
			ViewerCommandDelivered, now, now, now, viewerID, command.ID, ViewerCommandPending,
		); err != nil {
			return ViewerCommand{}, false, fmt.Errorf("mark viewer command delivered: %w", err)
		}
		command, err = scanViewerCommand(tx.QueryRowContext(ctx,
			viewerCommandSelect+` WHERE viewer_id = ? AND id = ?`, viewerID, command.ID,
		))
		if err != nil {
			return ViewerCommand{}, false, fmt.Errorf("reload delivered viewer command: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ViewerCommand{}, false, fmt.Errorf("commit viewer command delivery: %w", err)
	}
	return command, true, nil
}

func (d *DB) GetViewerCommand(ctx context.Context, viewerID string, id int64) (ViewerCommand, error) {
	row := d.db.QueryRowContext(ctx,
		viewerCommandSelect+` WHERE viewer_id = ? AND id = ?`,
		strings.TrimSpace(viewerID), id,
	)
	return scanViewerCommand(row)
}

func (d *DB) ViewerDesiredReleaseGeneration(ctx context.Context, viewerID, version, artifactSHA256 string) (int64, error) {
	var generation int64
	if err := d.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(generation), 0) FROM viewer_commands
		 WHERE viewer_id = ? AND type = 'update_app' AND desired_version = ? AND artifact_sha256 = ?`,
		strings.TrimSpace(viewerID), strings.TrimSpace(version), strings.TrimSpace(artifactSHA256),
	).Scan(&generation); err != nil {
		return 0, fmt.Errorf("load viewer desired release generation: %w", err)
	}
	return generation, nil
}

func (d *DB) UpdateViewerCommand(ctx context.Context, viewerID string, id int64, req ViewerCommandUpdate) (ViewerCommand, error) {
	return d.ApplyViewerCommandResult(ctx, viewerID, id, req)
}

func (d *DB) ApplyViewerCommandResult(ctx context.Context, viewerID string, id int64, req ViewerCommandResult) (ViewerCommand, error) {
	if !isViewerCommandResultState(req.State) {
		return ViewerCommand{}, fmt.Errorf("unsupported command state: %w", ErrValidation)
	}
	req.Error = RedactText(strings.TrimSpace(req.Error))
	req.OperationKey = RedactText(strings.TrimSpace(req.OperationKey))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	terminal := isViewerCommandTerminal(req.State)
	previous := viewerCommandPreviousStates(req.State)
	updated, err := scanViewerCommand(d.db.QueryRowContext(ctx,
		`UPDATE viewer_commands SET state = ?, error = ?,
			operation_key = CASE WHEN ? = '' THEN operation_key ELSE ? END,
			acknowledged_at = CASE WHEN ? = ? THEN COALESCE(acknowledged_at, ?) ELSE acknowledged_at END,
			running_at = CASE WHEN ? = ? THEN COALESCE(running_at, ?) ELSE running_at END,
			result_at = CASE WHEN ? THEN COALESCE(result_at, ?) ELSE result_at END,
			completed_at = CASE WHEN ? THEN COALESCE(completed_at, ?) ELSE completed_at END,
			updated_at = ?
		 WHERE viewer_id = ? AND id = ? AND state IN (?, ?, ?, ?)
		   AND (operation_key = '' OR ? = '' OR operation_key = ?)
		 RETURNING `+viewerCommandColumns,
		req.State, req.Error, req.OperationKey, req.OperationKey,
		req.State, ViewerCommandAcknowledged, now,
		req.State, ViewerCommandRunning, now,
		terminal, now, terminal, now, now, strings.TrimSpace(viewerID), id,
		previous[0], previous[1], previous[2], previous[3], req.OperationKey, req.OperationKey,
	))
	if err == nil {
		return updated, nil
	}
	if !errors.Is(err, ErrViewerCommandNotFound) {
		return ViewerCommand{}, fmt.Errorf("apply viewer command result: %w", err)
	}
	current, err := d.GetViewerCommand(ctx, viewerID, id)
	if err != nil {
		return ViewerCommand{}, err
	}
	if current.State == req.State && current.Error == req.Error &&
		(req.OperationKey == "" || current.OperationKey == req.OperationKey) {
		return current, nil
	}
	if current.OperationKey != "" && req.OperationKey != "" && current.OperationKey != req.OperationKey {
		return ViewerCommand{}, fmt.Errorf("command operation key changed: %w", ErrValidation)
	}
	return ViewerCommand{}, fmt.Errorf("unsupported command transition from %s to %s: %w", current.State, req.State, ErrValidation)
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
	var sentAt, deliveredAt, acknowledgedAt, runningAt, resultAt, completedAt sql.NullString
	if err := row.Scan(&command.ID, &command.ViewerID, &command.Type, &command.Message, &command.Route,
		&command.Mode, &command.StreamName, &command.DesiredVersion, &command.ArtifactSHA256,
		&command.PayloadHash, &command.TTLSeconds, &command.OperationKey, &command.Generation,
		&command.State, &command.Error, &createdAt, &sentAt, &deliveredAt, &acknowledgedAt,
		&runningAt, &resultAt, &completedAt, &updatedAt); err != nil {
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
	command.DeliveredAt = parseViewerCommandTime(deliveredAt)
	command.AcknowledgedAt = parseViewerCommandTime(acknowledgedAt)
	command.RunningAt = parseViewerCommandTime(runningAt)
	command.ResultAt = parseViewerCommandTime(resultAt)
	if completedAt.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, completedAt.String)
		command.CompletedAt = &parsed
	}
	return command, nil
}

func parseViewerCommandTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value.String)
	return &parsed
}

func isViewerCommandResultState(state ViewerCommandState) bool {
	switch state {
	case ViewerCommandAcknowledged, ViewerCommandRunning, ViewerCommandSucceeded,
		ViewerCommandFailed, ViewerCommandRejected, ViewerCommandExpired:
		return true
	default:
		return false
	}
}

func isViewerCommandTerminal(state ViewerCommandState) bool {
	switch state {
	case ViewerCommandSucceeded, ViewerCommandFailed, ViewerCommandRejected, ViewerCommandExpired:
		return true
	default:
		return false
	}
}

func viewerCommandPreviousStates(state ViewerCommandState) [4]ViewerCommandState {
	switch state {
	case ViewerCommandAcknowledged:
		return [4]ViewerCommandState{ViewerCommandPending, ViewerCommandDelivered}
	case ViewerCommandRunning:
		return [4]ViewerCommandState{ViewerCommandPending, ViewerCommandDelivered, ViewerCommandAcknowledged}
	default:
		return [4]ViewerCommandState{
			ViewerCommandPending, ViewerCommandDelivered, ViewerCommandAcknowledged, ViewerCommandRunning,
		}
	}
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
