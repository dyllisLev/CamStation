package recorder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"camstation/internal/store"
)

type RecoveryResult struct {
	Recovered   int
	Quarantined int
	FailedMoves int
}

type recoveryStore interface {
	ListRecordingSegmentsByStatus(ctx context.Context, statuses ...string) ([]store.RecordingSegment, error)
	MarkRecordingSegmentStatusByID(ctx context.Context, id int64, status, message string) error
}

func RecoverInterruptedSegments(ctx context.Context, db recoveryStore, quarantineRoot string) (RecoveryResult, error) {
	var result RecoveryResult
	segments, err := db.ListRecordingSegmentsByStatus(ctx, "recording", "finalizing")
	if err != nil {
		return result, err
	}
	for _, segment := range segments {
		message := "interrupted recorder recovered on startup"
		if segment.TempPath != "" {
			if moved, moveErr := quarantineTemp(segment.TempPath, quarantineRoot); moveErr != nil {
				result.FailedMoves++
				message = fmt.Sprintf("%s; quarantine failed: %v", message, moveErr)
			} else if moved {
				result.Quarantined++
			}
		}
		if err := db.MarkRecordingSegmentStatusByID(ctx, segment.ID, "failed", message); err != nil {
			return result, err
		}
		result.Recovered++
	}
	return result, nil
}

func quarantineTemp(path, quarantineRoot string) (bool, error) {
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	date, ok := dateFromSegmentPath(path)
	if !ok {
		date = time.Now().In(kst()).Format("2006-01-02")
	}
	streamName := filepath.Base(filepath.Dir(filepath.Dir(path)))
	if streamName == "." || streamName == string(filepath.Separator) {
		streamName = "unknown"
	}
	targetDir := filepath.Join(quarantineRoot, "temp", date, streamName)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return false, err
	}
	target := filepath.Join(targetDir, filepath.Base(path))
	if _, err := os.Stat(target); err == nil {
		target = filepath.Join(targetDir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(path)))
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.Rename(path, target); err != nil {
		return false, err
	}
	return true, nil
}
