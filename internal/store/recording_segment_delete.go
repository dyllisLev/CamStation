package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

func (d *DB) DeleteReadyRecordingSegmentFile(ctx context.Context, id int64, recordingsDir string) (RecordingSegment, error) {
	return d.deleteReadyRecordingSegmentFile(ctx, recordingSegmentDeleteRequest{
		ID:            id,
		RecordingsDir: recordingsDir,
		Reason:        "operator delete",
	})
}

func (d *DB) DeleteBackedUpRecordingSegmentFile(ctx context.Context, id int64, recordingsDir string) (RecordingSegment, error) {
	return d.deleteReadyRecordingSegmentFile(ctx, recordingSegmentDeleteRequest{
		ID:              id,
		RecordingsDir:   recordingsDir,
		Reason:          "backup cleanup",
		RequireBackedUp: true,
	})
}

type recordingSegmentDeleteRequest struct {
	ID              int64
	RecordingsDir   string
	Reason          string
	RequireBackedUp bool
}

func (d *DB) deleteReadyRecordingSegmentFile(ctx context.Context, request recordingSegmentDeleteRequest) (RecordingSegment, error) {
	segment, path, _, err := d.readyRecordingSegmentPath(ctx, request.ID, request.RecordingsDir)
	if err != nil {
		return RecordingSegment{}, err
	}
	if request.RequireBackedUp && segment.BackupState != "backed_up" {
		return RecordingSegment{}, fmt.Errorf("recording segment %d backup state %q: %w", request.ID, segment.BackupState, ErrRecordingSegmentNotReady)
	}
	stagedPath := path + ".deleting-" + strconv.FormatInt(request.ID, 10)
	if _, err := os.Stat(stagedPath); err == nil {
		return RecordingSegment{}, fmt.Errorf("stage recording segment delete: %w", ErrRecordingSegmentDeleteConflict)
	} else if !errors.Is(err, os.ErrNotExist) {
		return RecordingSegment{}, fmt.Errorf("stat staged recording segment delete path: %w", err)
	}
	if err := os.Rename(path, stagedPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RecordingSegment{}, fmt.Errorf("delete segment file %d: %w", request.ID, ErrRecordingSegmentFileMissing)
		}
		return RecordingSegment{}, fmt.Errorf("stage segment file %d for delete: %w", request.ID, err)
	}
	if err := d.MarkRecordingSegmentDeleted(ctx, request.ID, request.Reason); err != nil {
		if restoreErr := os.Rename(stagedPath, path); restoreErr != nil {
			return RecordingSegment{}, errors.Join(
				fmt.Errorf("mark recording segment %d deleted: %w", request.ID, err),
				fmt.Errorf("restore recording segment file %d: %w", request.ID, restoreErr),
			)
		}
		return RecordingSegment{}, fmt.Errorf("mark recording segment %d deleted: %w", request.ID, err)
	}
	if err := os.Remove(stagedPath); err != nil {
		restoreErr := os.Rename(stagedPath, path)
		statusErr := d.MarkRecordingSegmentStatusByID(ctx, request.ID, "ready", request.Reason+" failed")
		return RecordingSegment{}, errors.Join(
			fmt.Errorf("remove staged recording segment file %d: %w", request.ID, err),
			restoreErr,
			statusErr,
		)
	}
	segment.Status = "deleted"
	segment.Error = request.Reason
	segment.UpdatedAt = time.Now().Unix()
	return segment, nil
}

func (d *DB) MarkRecordingSegmentDeleted(ctx context.Context, id int64, reason string) error {
	result, err := d.db.ExecContext(ctx,
		`UPDATE recording_segments
		 SET status = 'deleted', error = ?, updated_at = ?
		 WHERE id = ? AND status = 'ready'`,
		nullString(reason),
		time.Now().Unix(),
		id,
	)
	if err != nil {
		return fmt.Errorf("mark recording segment %d deleted: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read recording segment %d delete result: %w", id, err)
	}
	if affected == 0 {
		return fmt.Errorf("recording segment %d was not ready for delete: %w", id, ErrRecordingSegmentNotReady)
	}
	return nil
}
