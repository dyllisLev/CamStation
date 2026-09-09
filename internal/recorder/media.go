package recorder

import (
	"context"
	"os"

	"camstation/internal/opslog"
	"camstation/internal/recordingmedia"
)

func (w *worker) publishCurrent() {
	w.segmentMu.Lock()
	defer w.segmentMu.Unlock()
	segment := w.currentRef()
	if segment == nil {
		return
	}
	if _, err := w.publishSegment(segment); err != nil {
		w.logMediaIndexError(segment, err)
	}
}

// The file itself carries packet epoch references, so an archived backup remains
// independently indexable. Only complete, synced ranges become DB-visible.
func (w *worker) publishSegment(segment *segmentRef) (recordingmedia.Index, error) {
	idx, err := recordingmedia.Inspect(segment.path)
	if err != nil {
		return idx, err
	}
	if len(idx.Fragments) == 0 || idx.CommittedOffset <= segment.committed {
		return idx, nil
	}
	f, err := os.OpenFile(segment.path, os.O_RDWR, 0)
	if err != nil {
		return idx, err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return idx, err
	}
	if closeErr != nil {
		return idx, closeErr
	}
	if err = w.manager.db.PublishRecordingMedia(context.Background(), segment.id, idx); err != nil {
		return idx, err
	}
	segment.committed = idx.CommittedOffset
	return idx, nil
}

func (w *worker) logMediaIndexError(segment *segmentRef, err error) {
	w.manager.logFFmpegRateLimited(opslog.Warn, "media_index_failed", opslog.Fields{
		CameraID: w.camera.ID, StreamName: w.camera.StreamName, Filename: segment.filename,
		ErrorCode: "media_index_failed", Message: err.Error(),
	})
}
