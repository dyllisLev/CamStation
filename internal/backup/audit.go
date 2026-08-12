package backup

import (
	"context"
	"fmt"
	"path/filepath"

	"camstation/internal/store"
)

const (
	jobEventRecordingBackedUp     = "recording_backed_up"
	jobEventRecordingLocalDeleted = "recording_local_deleted"
)

type recordingAuditEvent struct {
	JobID   int64
	Type    string
	Message string
	Segment store.RecordingSegment
	Source  string
}

type recordingAuditBatch struct {
	JobID    int64
	Source   string
	Segments []store.RecordingSegment
}

func (r *Runner) appendBackedUpAuditEvents(ctx context.Context, batch recordingAuditBatch) error {
	for _, segment := range batch.Segments {
		if err := r.appendRecordingAuditEvent(ctx, recordingAuditEvent{
			JobID:   batch.JobID,
			Type:    jobEventRecordingBackedUp,
			Message: "파일 백업 완료",
			Segment: segment,
			Source:  batch.Source,
		}); err != nil {
			return fmt.Errorf("append backed up recording audit event %d: %w", segment.ID, err)
		}
	}
	return nil
}

func (r *Runner) deleteBackedUpSegments(ctx context.Context, batch recordingAuditBatch) (int64, error) {
	var deleted int64
	for _, segment := range batch.Segments {
		if _, err := r.db.DeleteBackedUpRecordingSegmentFile(ctx, segment.ID, batch.Source); err != nil {
			return deleted, fmt.Errorf("delete backed up recording segment %d: %w", segment.ID, err)
		}
		if err := r.appendRecordingAuditEvent(ctx, recordingAuditEvent{
			JobID:   batch.JobID,
			Type:    jobEventRecordingLocalDeleted,
			Message: "로컬 파일 삭제 완료",
			Segment: segment,
			Source:  batch.Source,
		}); err != nil {
			return deleted, fmt.Errorf("append deleted recording audit event %d: %w", segment.ID, err)
		}
		deleted++
	}
	return deleted, nil
}

func (r *Runner) appendRecordingAuditEvent(ctx context.Context, event recordingAuditEvent) error {
	details := map[string]any{
		"segmentId":   event.Segment.ID,
		"streamName":  event.Segment.StreamName,
		"filename":    event.Segment.Filename,
		"archivePath": archivePathForSegment(event.Source, event.Segment),
	}
	if event.Segment.FileSize != nil {
		details["sizeBytes"] = *event.Segment.FileSize
	}
	return r.db.AppendJobEvent(ctx, store.JobEventAppend{
		JobID:   event.JobID,
		Type:    event.Type,
		Message: event.Message,
		Details: details,
	})
}

func archivePathForSegment(source string, segment store.RecordingSegment) string {
	if segment.FinalPath == "" {
		return filepath.ToSlash(segment.Filename)
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return filepath.ToSlash(segment.Filename)
	}
	absPath, err := filepath.Abs(segment.FinalPath)
	if err != nil {
		return filepath.ToSlash(segment.Filename)
	}
	rel, err := filepath.Rel(absSource, absPath)
	if err != nil || rel == "." || rel == ".." {
		return filepath.ToSlash(segment.Filename)
	}
	if relHasParentPrefix(rel) {
		return filepath.ToSlash(segment.Filename)
	}
	return filepath.ToSlash(rel)
}

func relHasParentPrefix(path string) bool {
	return len(path) > 3 && path[:3] == fmt.Sprintf("..%c", filepath.Separator)
}
