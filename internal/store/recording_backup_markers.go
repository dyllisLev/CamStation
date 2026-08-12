package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type RecordingSegmentsBackupMark struct {
	JobID         int64
	UpdatedBefore int64
	SourceDir     string
}

type recordingSegmentsBackupSelection struct {
	UpdatedBefore int64
	SourceDir     string
}

func (d *DB) MarkReadyRecordingSegmentsBackedUp(ctx context.Context, mark RecordingSegmentsBackupMark) ([]RecordingSegment, error) {
	sourceDir, err := filepath.Abs(mark.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolve backup source dir: %w", err)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin backup marker update: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	segments, err := readyRecordingSegmentsPendingBackup(ctx, tx, recordingSegmentsBackupSelection{
		UpdatedBefore: mark.UpdatedBefore,
		SourceDir:     sourceDir,
	})
	if err != nil {
		return nil, err
	}
	if len(segments) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty backup marker update: %w", err)
		}
		committed = true
		return []RecordingSegment{}, nil
	}

	backedUpAt := time.Now().UTC().Format(time.RFC3339Nano)
	updatedAt := time.Now().Unix()
	args := make([]any, 0, len(segments)+3)
	args = append(args, backedUpAt, mark.JobID, updatedAt)
	placeholders := make([]string, 0, len(segments))
	for _, segment := range segments {
		placeholders = append(placeholders, "?")
		args = append(args, segment.ID)
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE recording_segments
		 SET backup_state = 'backed_up', backed_up_at = ?, backup_job_id = ?, updated_at = ?
		 WHERE status = 'ready' AND backup_state != 'backed_up' AND id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("mark recording segments backed up: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("read backed up segment count: %w", err)
	}
	if count != int64(len(segments)) {
		return nil, fmt.Errorf("backup marker changed %d of %d selected segments: %w", count, len(segments), ErrRecordingSegmentNotReady)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit backup marker update: %w", err)
	}
	committed = true
	for index := range segments {
		segments[index].BackupState = "backed_up"
		segments[index].BackedUpAt = backedUpAt
		segments[index].BackupJobID = mark.JobID
		segments[index].UpdatedAt = updatedAt
	}
	return segments, nil
}

func (d *DB) ListReadyBackedUpRecordingSegments(ctx context.Context, source string) ([]RecordingSegment, error) {
	sourceDir, err := filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("resolve backup source dir: %w", err)
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
		        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
		        error, created_at, updated_at
		   FROM recording_segments
		  WHERE status = 'ready' AND backup_state = 'backed_up'
		  ORDER BY ts_start ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list ready backed up recording segments: %w", err)
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ready backed up recording segment: %w", err)
		}
		if recordingSegmentInsideBackupSource(sourceDir, segment.FinalPath) {
			segments = append(segments, segment)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ready backed up recording segments: %w", err)
	}
	return segments, nil
}

func readyRecordingSegmentsPendingBackup(ctx context.Context, tx *sql.Tx, selection recordingSegmentsBackupSelection) ([]RecordingSegment, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
		        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
		        error, created_at, updated_at
		   FROM recording_segments
		  WHERE status = 'ready' AND backup_state != 'backed_up' AND updated_at <= ?
		  ORDER BY ts_start ASC, id ASC`,
		selection.UpdatedBefore,
	)
	if err != nil {
		return nil, fmt.Errorf("list recording segments pending backup mark: %w", err)
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recording segment pending backup mark: %w", err)
		}
		if recordingSegmentInsideBackupSource(selection.SourceDir, segment.FinalPath) {
			segments = append(segments, segment)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recording segments pending backup mark: %w", err)
	}
	return segments, nil
}

func recordingSegmentInsideBackupSource(sourceDir, finalPath string) bool {
	if finalPath == "" {
		return false
	}
	absPath, err := filepath.Abs(finalPath)
	if err != nil {
		return false
	}
	return pathWithin(sourceDir, absPath)
}

func backupStateOrPending(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "pending"
	}
	return value
}
