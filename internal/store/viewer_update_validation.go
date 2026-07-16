package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ViewerUpdateValidationObservation struct {
	ViewerID       string
	CommandID      int64
	PayloadHash    string
	TransactionID  string
	Generation     int64
	TargetVersion  string
	ArtifactSHA256 string
	Healthy        bool
}

type viewerUpdateValidation struct {
	ViewerUpdateValidationObservation
	HealthySince   *time.Time
	LastObservedAt time.Time
	CommitToken    string
}

func (d *DB) ObserveViewerUpdateValidation(ctx context.Context, observation ViewerUpdateValidationObservation, now time.Time, healthyFor, maxGap time.Duration) (string, error) {
	observation.ViewerID = strings.TrimSpace(observation.ViewerID)
	observation.PayloadHash = strings.TrimSpace(observation.PayloadHash)
	observation.TransactionID = strings.TrimSpace(observation.TransactionID)
	observation.TargetVersion = strings.TrimSpace(observation.TargetVersion)
	observation.ArtifactSHA256 = strings.ToLower(strings.TrimSpace(observation.ArtifactSHA256))
	if observation.ViewerID == "" || observation.CommandID <= 0 || observation.PayloadHash == "" ||
		observation.TransactionID == "" || observation.Generation <= 0 || observation.TargetVersion == "" ||
		len(observation.ArtifactSHA256) != 64 || healthyFor <= 0 || maxGap <= 0 || now.IsZero() {
		return "", fmt.Errorf("invalid viewer update validation observation: %w", ErrValidation)
	}
	if _, err := hex.DecodeString(observation.ArtifactSHA256); err != nil {
		return "", fmt.Errorf("invalid viewer update validation digest: %w", ErrValidation)
	}
	now = now.UTC()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin viewer update validation: %w", err)
	}
	defer tx.Rollback()

	current, found, err := loadViewerUpdateValidation(ctx, tx, observation.ViewerID)
	if err != nil {
		return "", err
	}
	exact := found && sameViewerUpdateValidation(current.ViewerUpdateValidationObservation, observation)
	continuous := exact && current.HealthySince != nil && !now.Before(current.LastObservedAt) && now.Sub(current.LastObservedAt) <= maxGap
	var healthySince *time.Time
	commitToken := ""
	if observation.Healthy {
		if continuous {
			healthySince = current.HealthySince
			commitToken = current.CommitToken
		} else {
			started := now
			healthySince = &started
		}
		if commitToken == "" && now.Sub(*healthySince) >= healthyFor {
			commitToken, err = randomCommitToken()
			if err != nil {
				return "", err
			}
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO viewer_update_validations(viewer_id, command_id, payload_hash, transaction_id,
			generation, target_version, artifact_sha256, healthy_since, last_observed_at, commit_token)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(viewer_id) DO UPDATE SET command_id=excluded.command_id,
			payload_hash=excluded.payload_hash, transaction_id=excluded.transaction_id,
			generation=excluded.generation, target_version=excluded.target_version,
			artifact_sha256=excluded.artifact_sha256, healthy_since=excluded.healthy_since,
			last_observed_at=excluded.last_observed_at, commit_token=excluded.commit_token`,
		observation.ViewerID, observation.CommandID, observation.PayloadHash, observation.TransactionID,
		observation.Generation, observation.TargetVersion, observation.ArtifactSHA256,
		viewerTimeValue(healthySince), now.Format(time.RFC3339Nano), commitToken,
	)
	if err != nil {
		return "", fmt.Errorf("save viewer update validation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit viewer update validation: %w", err)
	}
	return commitToken, nil
}

func (d *DB) ResetViewerUpdateValidation(ctx context.Context, viewerID string) error {
	if _, err := d.db.ExecContext(ctx, `DELETE FROM viewer_update_validations WHERE viewer_id = ?`, strings.TrimSpace(viewerID)); err != nil {
		return fmt.Errorf("reset viewer update validation: %w", err)
	}
	return nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadViewerUpdateValidation(ctx context.Context, query queryRower, viewerID string) (viewerUpdateValidation, bool, error) {
	var current viewerUpdateValidation
	var healthySince sql.NullString
	var lastObservedAt string
	err := query.QueryRowContext(ctx,
		`SELECT viewer_id, command_id, payload_hash, transaction_id, generation, target_version,
			artifact_sha256, healthy_since, last_observed_at, commit_token
		 FROM viewer_update_validations WHERE viewer_id = ?`, viewerID,
	).Scan(&current.ViewerID, &current.CommandID, &current.PayloadHash, &current.TransactionID,
		&current.Generation, &current.TargetVersion, &current.ArtifactSHA256, &healthySince,
		&lastObservedAt, &current.CommitToken)
	if errors.Is(err, sql.ErrNoRows) {
		return viewerUpdateValidation{}, false, nil
	}
	if err != nil {
		return viewerUpdateValidation{}, false, fmt.Errorf("load viewer update validation: %w", err)
	}
	current.LastObservedAt, err = time.Parse(time.RFC3339Nano, lastObservedAt)
	if err != nil {
		return viewerUpdateValidation{}, false, errors.New("invalid persisted viewer update observation time")
	}
	current.HealthySince = parseViewerTime(healthySince)
	return current, true, nil
}

func sameViewerUpdateValidation(left, right ViewerUpdateValidationObservation) bool {
	return left.ViewerID == right.ViewerID && left.CommandID == right.CommandID &&
		left.PayloadHash == right.PayloadHash && left.TransactionID == right.TransactionID &&
		left.Generation == right.Generation && left.TargetVersion == right.TargetVersion &&
		left.ArtifactSHA256 == right.ArtifactSHA256
}

func randomCommitToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate viewer update commit token: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}
