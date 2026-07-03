package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrDiagnosticArtifactNotFound = errors.New("diagnostic artifact not found")

type DiagnosticArtifactCreate struct {
	JobID     int64
	Name      string
	Path      string
	SizeBytes int64
	SHA256    string
}

type DiagnosticArtifact struct {
	ID        int64      `json:"id"`
	JobID     int64      `json:"jobId"`
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	SizeBytes int64      `json:"sizeBytes"`
	SHA256    string     `json:"sha256"`
	CreatedAt time.Time  `json:"createdAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

func (d *DB) CreateDiagnosticArtifact(ctx context.Context, create DiagnosticArtifactCreate) (DiagnosticArtifact, error) {
	create.Name = RedactText(strings.TrimSpace(create.Name))
	create.Path = RedactText(strings.TrimSpace(create.Path))
	create.SHA256 = RedactText(strings.TrimSpace(create.SHA256))
	if create.JobID <= 0 || create.Name == "" || create.Path == "" {
		return DiagnosticArtifact{}, fmt.Errorf("artifact jobId, name, and path are required: %w", ErrValidation)
	}
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO diagnostic_artifacts(job_id, name, path, size_bytes, sha256, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		create.JobID, create.Name, create.Path, create.SizeBytes, create.SHA256, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return DiagnosticArtifact{}, fmt.Errorf("create diagnostic artifact: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return DiagnosticArtifact{}, fmt.Errorf("created diagnostic artifact id: %w", err)
	}
	return d.GetDiagnosticArtifact(ctx, id)
}

func (d *DB) ListDiagnosticArtifacts(ctx context.Context, includeDeleted bool) ([]DiagnosticArtifact, error) {
	query := `SELECT id, job_id, name, path, size_bytes, sha256, created_at, deleted_at
		FROM diagnostic_artifacts`
	if !includeDeleted {
		query += ` WHERE deleted_at IS NULL`
	}
	query += ` ORDER BY id DESC`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list diagnostic artifacts: %w", err)
	}
	defer rows.Close()
	artifacts := make([]DiagnosticArtifact, 0)
	for rows.Next() {
		artifact, err := scanDiagnosticArtifact(rows)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (d *DB) GetDiagnosticArtifact(ctx context.Context, id int64) (DiagnosticArtifact, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, job_id, name, path, size_bytes, sha256, created_at, deleted_at
		 FROM diagnostic_artifacts WHERE id = ?`,
		id,
	)
	return scanDiagnosticArtifact(row)
}

func (d *DB) MarkDiagnosticArtifactDeleted(ctx context.Context, id int64) (DiagnosticArtifact, error) {
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`UPDATE diagnostic_artifacts SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`,
		now.Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return DiagnosticArtifact{}, fmt.Errorf("delete diagnostic artifact: %w", err)
	}
	if err := requireDiagnosticArtifactChanged(res); err != nil {
		return DiagnosticArtifact{}, err
	}
	return d.GetDiagnosticArtifact(ctx, id)
}

func (d *DB) DeleteDiagnosticArtifactHistory(ctx context.Context) (int64, error) {
	res, err := d.db.ExecContext(ctx, `DELETE FROM diagnostic_artifacts WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return 0, fmt.Errorf("delete diagnostic artifact history: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("diagnostic history rows affected: %w", err)
	}
	return deleted, nil
}

func (d *DB) Vacuum(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, `VACUUM`); err != nil {
		return fmt.Errorf("vacuum database: %w", err)
	}
	return nil
}

func scanDiagnosticArtifact(row scanner) (DiagnosticArtifact, error) {
	var artifact DiagnosticArtifact
	var createdAt string
	var deletedAt sql.NullString
	if err := row.Scan(&artifact.ID, &artifact.JobID, &artifact.Name, &artifact.Path,
		&artifact.SizeBytes, &artifact.SHA256, &createdAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DiagnosticArtifact{}, ErrDiagnosticArtifactNotFound
		}
		return DiagnosticArtifact{}, err
	}
	artifact.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if deletedAt.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, deletedAt.String)
		artifact.DeletedAt = &parsed
	}
	return artifact, nil
}

func requireDiagnosticArtifactChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if changed == 0 {
		return ErrDiagnosticArtifactNotFound
	}
	return nil
}
