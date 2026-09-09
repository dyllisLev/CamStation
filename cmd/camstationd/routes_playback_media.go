package main

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"camstation/internal/recordingmedia"
	"camstation/internal/store"
)

func playbackMediaSegmentID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("mediaId")
	if !strings.HasPrefix(raw, "fmp4-") {
		writePlaybackBadRequest(w, "invalid mediaId")
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(raw, "fmp4-"), 10, 64)
	if err != nil || id <= 0 {
		writePlaybackBadRequest(w, "invalid mediaId")
		return 0, false
	}
	return id, true
}
func (d routeDeps) handlePlaybackManifest(w http.ResponseWriter, r *http.Request) {
	id, ok := playbackMediaSegmentID(w, r)
	if !ok {
		return
	}
	segment, idx, err := d.db.RecordingMedia(r.Context(), id)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	if len(idx.Fragments) == 0 {
		writePlaybackError(w, store.ErrRecordingSegmentNotFound)
		return
	}
	target := 1.0
	for i, f := range idx.Fragments {
		durationMs := f.MediaEndMs - f.MediaStartMs
		if i == 0 {
			durationMs = f.MediaEndMs
		}
		target = math.Max(target, math.Ceil(float64(durationMs)/1000))
	}
	var body strings.Builder
	fmt.Fprintf(&body, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%.0f\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-PLAYLIST-TYPE:EVENT\n#EXT-X-INDEPENDENT-SEGMENTS\n#EXT-X-MAP:URI=\"init\"\n", target, idx.Fragments[0].Sequence)
	for i, f := range idx.Fragments {
		startMs, durationMs := f.StartMs, f.MediaEndMs-f.MediaStartMs
		if i == 0 {
			// Include audio preroll in the HLS timeline without advertising it
			// as video coverage. Later seek offsets use this same origin.
			startMs -= f.MediaStartMs
			durationMs = f.MediaEndMs
		}
		fmt.Fprintf(&body, "#EXT-X-PROGRAM-DATE-TIME:%s\n#EXTINF:%.3f,\nfragments/%d\n", time.UnixMilli(startMs).UTC().Format("2006-01-02T15:04:05.000Z"), float64(durationMs)/1000, f.Sequence)
	}
	if segment.Status != "recording" && segment.Status != "finalizing" {
		body.WriteString("#EXT-X-ENDLIST\n")
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.WriteString(w, body.String())
}
func (d routeDeps) handlePlaybackInit(w http.ResponseWriter, r *http.Request) {
	id, ok := playbackMediaSegmentID(w, r)
	if !ok {
		return
	}
	_, idx, err := d.db.RecordingMedia(r.Context(), id)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	d.servePlaybackBytes(w, r, id, 0, idx.InitLength, idx)
}
func (d routeDeps) handlePlaybackFragment(w http.ResponseWriter, r *http.Request) {
	id, ok := playbackMediaSegmentID(w, r)
	if !ok {
		return
	}
	sequence, err := strconv.ParseInt(r.PathValue("sequence"), 10, 64)
	if err != nil || sequence < 0 {
		writePlaybackBadRequest(w, "invalid fragment sequence")
		return
	}
	_, idx, err := d.db.RecordingMedia(r.Context(), id)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	for _, f := range idx.Fragments {
		if f.Sequence == sequence {
			d.servePlaybackBytes(w, r, id, f.Offset, f.Length, idx)
			return
		}
	}
	writePlaybackError(w, store.ErrRecordingSegmentNotFound)
}
func (d routeDeps) servePlaybackBytes(w http.ResponseWriter, r *http.Request, id, offset, length int64, idx recordingmedia.Index) {
	if offset < 0 || length <= 0 || offset > idx.CommittedOffset-length {
		writePlaybackError(w, fmt.Errorf("invalid committed media range"))
		return
	}
	file, err := d.db.OpenPlaybackRecordingFile(r.Context(), id, d.recordingsDir, d.tempDir)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() < offset+length {
		writePlaybackError(w, store.ErrRecordingSegmentFileMissing)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "private, max-age=3600, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "recording.mp4", time.Time{}, io.NewSectionReader(file, offset, length))
}
