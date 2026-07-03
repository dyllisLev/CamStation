package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (d *DB) MarkReadyRecordingSegmentsBackedUp(ctx context.Context, jobID int64, updatedBefore int64) (int64, error) {
	backedUpAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET backup_state = 'backed_up', backed_up_at = ?, backup_job_id = ?, updated_at = ?
		 WHERE status = 'ready' AND backup_state != 'backed_up' AND updated_at <= ?`,
		backedUpAt,
		jobID,
		time.Now().Unix(),
		updatedBefore,
	)
	if err != nil {
		return 0, fmt.Errorf("mark recording segments backed up: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read backed up segment count: %w", err)
	}
	return count, nil
}

func backupStateOrPending(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "pending"
	}
	return value
}
