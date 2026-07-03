package cleanup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"camstation/internal/store"
)

type Store interface {
	GetSettings(ctx context.Context) (store.Settings, error)
	ListDeletableRecordingSegments(ctx context.Context, requireBackedUp bool) ([]store.RecordingSegment, error)
	MarkRecordingSegmentDeleted(ctx context.Context, id int64, reason string) error
}

type Cleaner struct {
	db            Store
	recordingsDir string
	mu            sync.Mutex
}

type Result struct {
	MaxBytes    int64            `json:"maxBytes"`
	BeforeBytes int64            `json:"beforeBytes"`
	AfterBytes  int64            `json:"afterBytes"`
	Deleted     []DeletedSegment `json:"deleted"`
}

type DeletedSegment struct {
	ID         int64  `json:"id"`
	StreamName string `json:"streamName"`
	Filename   string `json:"filename"`
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
}

func New(db Store, recordingsDir string) *Cleaner {
	if recordingsDir == "" {
		recordingsDir = "./data/recordings"
	}
	return &Cleaner{db: db, recordingsDir: recordingsDir}
}

func (c *Cleaner) EnforceMaxBytes(ctx context.Context, maxBytes int64) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := Result{MaxBytes: maxBytes, Deleted: []DeletedSegment{}}
	before, err := DirSize(c.recordingsDir)
	if err != nil {
		return result, err
	}
	result.BeforeBytes = before
	result.AfterBytes = before
	if maxBytes <= 0 || before <= maxBytes {
		return result, nil
	}

	settings, err := c.db.GetSettings(ctx)
	if err != nil {
		return result, err
	}
	segments, err := c.db.ListDeletableRecordingSegments(ctx, settings.Backup.ProtectUnbacked)
	if err != nil {
		return result, err
	}
	for _, segment := range segments {
		if result.AfterBytes <= maxBytes {
			break
		}
		if !isSafeRecordingPath(c.recordingsDir, segment.FinalPath) {
			return result, fmt.Errorf("refusing to delete path outside recordings dir: %s", segment.FinalPath)
		}
		info, statErr := os.Stat(segment.FinalPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				if err := c.db.MarkRecordingSegmentDeleted(ctx, segment.ID, "capacity cleanup: file already missing"); err != nil {
					return result, err
				}
				continue
			}
			return result, statErr
		}
		if info.IsDir() {
			continue
		}
		size := info.Size()
		if err := os.Remove(segment.FinalPath); err != nil {
			return result, err
		}
		if err := c.db.MarkRecordingSegmentDeleted(ctx, segment.ID, "capacity cleanup"); err != nil {
			return result, err
		}
		result.AfterBytes -= size
		result.Deleted = append(result.Deleted, DeletedSegment{
			ID:         segment.ID,
			StreamName: segment.StreamName,
			Filename:   segment.Filename,
			Path:       segment.FinalPath,
			Bytes:      size,
		})
		removeEmptyParents(c.recordingsDir, filepath.Dir(segment.FinalPath))
	}

	if result.AfterBytes < 0 {
		result.AfterBytes = 0
	}
	return result, nil
}

func DirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, err
}

func isSafeRecordingPath(root, path string) bool {
	if path == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func removeEmptyParents(root, dir string) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return
	}
	for {
		absDir, err := filepath.Abs(dir)
		if err != nil || absDir == absRoot {
			return
		}
		if rel, err := filepath.Rel(absRoot, absDir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return
		}
		if err := os.Remove(absDir); err != nil {
			return
		}
		dir = filepath.Dir(absDir)
	}
}
