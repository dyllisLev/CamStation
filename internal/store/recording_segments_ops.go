package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrRecordingSegmentNotFound       = errors.New("recording segment not found")
	ErrRecordingSegmentNotReady       = errors.New("recording segment is not ready")
	ErrRecordingSegmentUnsafePath     = errors.New("recording segment path is outside recordings dir")
	ErrRecordingSegmentFileMissing    = errors.New("recording segment file missing")
	ErrRecordingSegmentDeleteConflict = errors.New("recording segment delete already in progress")
)

type RecordingSegmentFilter struct {
	StreamName string
	Statuses   []string
	From       *time.Time
	To         *time.Time
	Limit      int
}

func (d *DB) ListRecordingSegmentsForConsole(ctx context.Context, filter RecordingSegmentFilter) ([]RecordingSegment, error) {
	args := make([]any, 0, 8)
	clauses := []string{"1=1"}
	if filter.StreamName != "" {
		clauses = append(clauses, "stream_name = ?")
		args = append(args, filter.StreamName)
	}
	if filter.From != nil {
		clauses = append(clauses, "ts_start >= ?")
		args = append(args, float64(filter.From.Unix()))
	}
	if filter.To != nil {
		clauses = append(clauses, "ts_start < ?")
		args = append(args, float64(filter.To.Unix()))
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			status = strings.TrimSpace(status)
			if status == "" {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		if len(placeholders) > 0 {
			clauses = append(clauses, "status IN ("+strings.Join(placeholders, ",")+")")
		}
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
			        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
			        error, created_at, updated_at
			   FROM recording_segments
		  WHERE `+strings.Join(clauses, " AND ")+`
		  ORDER BY ts_start DESC, id DESC
		  LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list recording segments: %w", err)
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recording segment: %w", err)
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recording segments: %w", err)
	}
	return segments, nil
}

func (d *DB) GetRecordingSegmentByID(ctx context.Context, id int64) (RecordingSegment, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
			        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
			        error, created_at, updated_at
			   FROM recording_segments
		  WHERE id = ?`,
		id,
	)
	segment, err := scanRecordingSegment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return RecordingSegment{}, fmt.Errorf("recording segment %d: %w", id, ErrRecordingSegmentNotFound)
	}
	if err != nil {
		return RecordingSegment{}, fmt.Errorf("get recording segment %d: %w", id, err)
	}
	return segment, nil
}

func (d *DB) OpenReadyRecordingSegmentFile(ctx context.Context, id int64, recordingsDir string) (RecordingSegment, *os.File, os.FileInfo, error) {
	segment, path, info, err := d.readyRecordingSegmentPath(ctx, id, recordingsDir)
	if err != nil {
		return RecordingSegment{}, nil, nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RecordingSegment{}, nil, nil, fmt.Errorf("open segment file %d: %w", id, ErrRecordingSegmentFileMissing)
		}
		return RecordingSegment{}, nil, nil, fmt.Errorf("open segment file %d: %w", id, err)
	}
	return segment, file, info, nil
}

func (d *DB) readyRecordingSegmentPath(ctx context.Context, id int64, recordingsDir string) (RecordingSegment, string, os.FileInfo, error) {
	segment, err := d.GetRecordingSegmentByID(ctx, id)
	if err != nil {
		return RecordingSegment{}, "", nil, err
	}
	if segment.Status != "ready" {
		return RecordingSegment{}, "", nil, fmt.Errorf("recording segment %d status %q: %w", id, segment.Status, ErrRecordingSegmentNotReady)
	}
	path, info, err := safeRecordingSegmentPath(recordingsDir, segment.FinalPath)
	if err != nil {
		return RecordingSegment{}, "", nil, err
	}
	return segment, path, info, nil
}

func safeRecordingSegmentPath(root, path string) (string, os.FileInfo, error) {
	if path == "" {
		return "", nil, ErrRecordingSegmentUnsafePath
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve recordings dir: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, fmt.Errorf("resolve recording segment path: %w", err)
	}
	if !pathWithin(absRoot, absPath) {
		return "", nil, ErrRecordingSegmentUnsafePath
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, ErrRecordingSegmentFileMissing
		}
		return "", nil, fmt.Errorf("stat recording segment path: %w", err)
	}
	if info.IsDir() {
		return "", nil, ErrRecordingSegmentUnsafePath
	}
	evalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", nil, fmt.Errorf("resolve recordings dir symlinks: %w", err)
	}
	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve recording segment symlinks: %w", err)
	}
	if !pathWithin(evalRoot, evalPath) {
		return "", nil, ErrRecordingSegmentUnsafePath
	}
	return absPath, info, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
