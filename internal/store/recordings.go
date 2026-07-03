package store

import (
	"context"
	"strings"
	"time"
)

func (d *DB) OpenRecordingSegment(ctx context.Context, segment RecordingSegment) (RecordingSegment, error) {
	now := time.Now().Unix()
	if segment.Status == "" {
		segment.Status = "recording"
	}
	segment.CreatedAt = now
	segment.UpdatedAt = now
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO recording_segments(
				camera_id, stream_name, filename, temp_path, final_path, ts_start,
				ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
				error, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(stream_name, ts_start) DO UPDATE SET
				filename=excluded.filename,
				temp_path=excluded.temp_path,
				status=excluded.status,
				backup_state='pending',
				backed_up_at=NULL,
				backup_job_id=0,
				error='',
				updated_at=excluded.updated_at`,
		segment.CameraID,
		segment.StreamName,
		segment.Filename,
		nullString(segment.TempPath),
		nullString(segment.FinalPath),
		segment.TSStart,
		segment.TSEnd,
		segment.FileSize,
		segment.Status,
		backupStateOrPending(segment.BackupState),
		nullString(segment.BackedUpAt),
		segment.BackupJobID,
		nullString(segment.Error),
		segment.CreatedAt,
		segment.UpdatedAt,
	)
	if err != nil {
		return RecordingSegment{}, err
	}
	return d.GetRecordingSegment(ctx, segment.StreamName, segment.TSStart)
}

func (d *DB) CloseRecordingSegment(ctx context.Context, streamName, filename string, tsEnd float64, finalPath string, fileSize *int64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
			 SET ts_end = ?, final_path = ?, file_size = ?, status = 'ready',
			     backup_state = 'pending', backed_up_at = NULL, backup_job_id = 0,
			     error = '', updated_at = ?
			 WHERE stream_name = ? AND filename = ? AND status IN ('recording', 'finalizing', 'failed')`,
		tsEnd,
		nullString(finalPath),
		fileSize,
		time.Now().Unix(),
		streamName,
		filename,
	)
	return err
}

func (d *DB) MarkRecordingSegmentStatus(ctx context.Context, streamName, filename, status, message string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET status = ?, error = ?, updated_at = ?
		 WHERE stream_name = ? AND filename = ?`,
		status,
		nullString(message),
		time.Now().Unix(),
		streamName,
		filename,
	)
	return err
}

func (d *DB) MarkRecordingSegmentStatusByID(ctx context.Context, id int64, status, message string) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET status = ?, error = ?, updated_at = ?
		 WHERE id = ?`,
		status,
		nullString(message),
		time.Now().Unix(),
		id,
	)
	return err
}

func (d *DB) GetRecordingSegment(ctx context.Context, streamName string, tsStart float64) (RecordingSegment, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
			        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
			        error, created_at, updated_at
			 FROM recording_segments WHERE stream_name = ? AND ts_start = ?`,
		streamName,
		tsStart,
	)
	return scanRecordingSegment(row)
}

func (d *DB) ListRecordingSegments(ctx context.Context, streamName string, from, to time.Time, statuses ...string) ([]RecordingSegment, error) {
	args := []any{streamName, float64(from.Unix()), float64(to.Unix())}
	statusClause := ""
	if len(statuses) > 0 {
		placeholders := make([]string, 0, len(statuses))
		for _, status := range statuses {
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		statusClause = " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
			        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
			        error, created_at, updated_at
			 FROM recording_segments
		 WHERE stream_name = ? AND ts_start >= ? AND ts_start < ?`+statusClause+`
		 ORDER BY ts_start`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func (d *DB) ListRecordingSegmentsByStatus(ctx context.Context, statuses ...string) ([]RecordingSegment, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(statuses))
	placeholders := make([]string, 0, len(statuses))
	for _, status := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, status)
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
			        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
			        error, created_at, updated_at
			 FROM recording_segments
		 WHERE status IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY ts_start, id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func (d *DB) ListDeletableRecordingSegments(ctx context.Context, requireBackedUp bool) ([]RecordingSegment, error) {
	backupClause := ""
	if requireBackedUp {
		backupClause = " AND backup_state = 'backed_up'"
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, camera_id, stream_name, filename, temp_path, final_path, ts_start,
		        ts_end, file_size, status, backup_state, backed_up_at, backup_job_id,
		        error, created_at, updated_at
		 FROM recording_segments
		 WHERE status = 'ready' AND final_path IS NOT NULL AND final_path != ''`+backupClause+`
		 ORDER BY ts_start, id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	segments := make([]RecordingSegment, 0)
	for rows.Next() {
		segment, err := scanRecordingSegment(rows)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
