package main

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"camstation/internal/store"
)

func (d routeDeps) registerPlaybackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/playback/cameras", d.handlePlaybackCameras)
	mux.HandleFunc("GET /api/playback/timeline", d.handlePlaybackTimeline)
	mux.HandleFunc("GET /api/playback/resolve", d.handlePlaybackResolve)
	mux.HandleFunc("GET /api/playback/segments", d.handlePlaybackSegments)
	mux.HandleFunc("GET /api/playback/media/{mediaId}/manifest.m3u8", d.handlePlaybackManifest)
	mux.HandleFunc("GET /api/playback/media/{mediaId}/init", d.handlePlaybackInit)
	mux.HandleFunc("GET /api/playback/media/{mediaId}/fragments/{sequence}", d.handlePlaybackFragment)
}

func (d routeDeps) handlePlaybackCameras(w http.ResponseWriter, r *http.Request) {
	cameras, err := d.db.ListPlaybackCameras(r.Context())
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"cameras": cameras})
}

func (d routeDeps) playbackCamera(w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	key := r.URL.Query().Get("cameraKey")
	if key == "" || len(key) > 200 {
		writePlaybackBadRequest(w, "cameraKey is required")
		return "", 0, false
	}
	id, err := d.db.PlaybackCameraID(r.Context(), key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writePlaybackBadRequest(w, "unknown cameraKey")
		} else {
			writePlaybackError(w, err)
		}
		return "", 0, false
	}
	return key, id, true
}
func playbackMilliseconds(r *http.Request, name string) (int64, error) {
	value, err := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	if err != nil || value < 0 || value > 253402300799999 {
		return 0, fmt.Errorf("%s must be UTC epoch milliseconds", name)
	}
	return value, nil
}
func playbackRange(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	from, err := playbackMilliseconds(r, "fromMs")
	if err != nil {
		writePlaybackBadRequest(w, err.Error())
		return 0, 0, false
	}
	to, err := playbackMilliseconds(r, "toMs")
	if err != nil {
		writePlaybackBadRequest(w, err.Error())
		return 0, 0, false
	}
	if to <= from || to-from > 32*24*60*60*1000 {
		writePlaybackBadRequest(w, "range must be positive and at most 32 days")
		return 0, 0, false
	}
	return from, to, true
}

type playbackCoverage struct {
	StartMs   int64  `json:"startMs"`
	EndMs     int64  `json:"endMs"`
	MediaID   string `json:"mediaId"`
	SegmentID int64  `json:"segmentId"`
	State     string `json:"state"`
}

func playbackCoverageFor(s store.PlaybackSpan) playbackCoverage {
	state := s.Status
	if state == "finalizing" {
		state = "recording"
	}
	if state == "failed" {
		state = "recovered"
	}
	return playbackCoverage{s.StartMs, s.EndMs, store.PlaybackMediaID(s), s.SegmentID, state}
}

func (d routeDeps) handlePlaybackTimeline(w http.ResponseWriter, r *http.Request) {
	key, id, ok := d.playbackCamera(w, r)
	if !ok {
		return
	}
	from, to, ok := playbackRange(w, r)
	if !ok {
		return
	}
	spans, err := d.db.PlaybackSpans(r.Context(), id, from, to)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	end, active, err := d.db.PlaybackBounds(r.Context(), id)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	coverage := make([]playbackCoverage, 0, len(spans))
	for _, s := range spans {
		coverage = append(coverage, playbackCoverageFor(s))
	}
	writeJSON(w, 200, map[string]any{"cameraKey": key, "fromMs": from, "toMs": to, "coverage": coverage, "playableEndMs": end, "recordingActive": active, "serverNowMs": time.Now().UnixMilli()})
}

func (d routeDeps) handlePlaybackResolve(w http.ResponseWriter, r *http.Request) {
	_, id, ok := d.playbackCamera(w, r)
	if !ok {
		return
	}
	at, err := playbackMilliseconds(r, "atMs")
	if err != nil {
		writePlaybackBadRequest(w, err.Error())
		return
	}
	spans, err := d.db.PlaybackSpans(r.Context(), id, at, at+1)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	out := map[string]any{"status": "gap", "requestedAtMs": at}
	if len(spans) > 0 {
		s := spans[len(spans)-1]
		if s.Fragmented && s.VideoCodec != "avc1" && s.VideoCodec != "hvc1" && s.VideoCodec != "hev1" {
			out["status"] = "unsupported"
			writeJSON(w, 200, out)
			return
		}
		kind := "file"
		url := fmt.Sprintf("/api/recordings/segments/%d/play", s.SegmentID)
		mediaStartSeconds := 0.0
		if s.Fragmented {
			_, index, err := d.db.RecordingMedia(r.Context(), s.SegmentID)
			if err != nil {
				writePlaybackError(w, err)
				return
			}
			if len(index.Fragments) == 0 {
				writePlaybackError(w, store.ErrRecordingSegmentNotFound)
				return
			}
			// HLS starts at the shared audio/video origin. Video coverage can
			// begin later when audio precedes the first video keyframe.
			mediaStartSeconds = float64(index.Fragments[0].MediaStartMs) / 1000
			kind = "hls"
			url = "/api/playback/media/" + store.PlaybackMediaID(s) + "/manifest.m3u8"
		}
		out["status"] = "found"
		out["media"] = map[string]any{"id": store.PlaybackMediaID(s), "kind": kind, "url": url, "startMs": s.StartMs, "endMs": s.EndMs, "mediaStartSeconds": mediaStartSeconds, "requestedOffsetSeconds": mediaStartSeconds + float64(at-s.StartMs)/1000, "growing": s.Status == "recording" || s.Status == "finalizing", "timeBasis": s.TimeBasis}
	} else {
		previous, next, err := d.db.PlaybackNeighbors(r.Context(), id, at)
		if err != nil {
			writePlaybackError(w, err)
			return
		}
		if previous != nil {
			out["previousAtMs"] = *previous - 1
		}
		if next != nil {
			out["nextAtMs"] = *next
		}
		end, active, err := d.db.PlaybackBounds(r.Context(), id)
		if err != nil {
			writePlaybackError(w, err)
			return
		}
		if active && next == nil && at <= time.Now().UnixMilli()+5000 && (end == nil || at >= *end) {
			waiting, err := d.db.PlaybackWaitingAt(r.Context(), id, at)
			if err != nil {
				writePlaybackError(w, err)
				return
			}
			if waiting {
				out["status"] = "edge_wait"
			}
		}
	}
	writeJSON(w, 200, out)
}

func (d routeDeps) handlePlaybackSegments(w http.ResponseWriter, r *http.Request) {
	_, id, ok := d.playbackCamera(w, r)
	if !ok {
		return
	}
	from, to, ok := playbackRange(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writePlaybackBadRequest(w, "limit must be 1 through 200")
			return
		}
		limit = n
	}
	var beforeMs, beforeID int64
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			writePlaybackBadRequest(w, "invalid cursor")
			return
		}
		parts := strings.Split(string(decoded), ":")
		if len(parts) != 2 {
			writePlaybackBadRequest(w, "invalid cursor")
			return
		}
		beforeMs, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			writePlaybackBadRequest(w, "invalid cursor")
			return
		}
		beforeID, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || beforeID <= 0 {
			writePlaybackBadRequest(w, "invalid cursor")
			return
		}
	}
	spans, err := d.db.PlaybackSpansPage(r.Context(), id, from, to, beforeMs, beforeID, limit+1)
	if err != nil {
		writePlaybackError(w, err)
		return
	}
	var next any
	if len(spans) > limit {
		spans = spans[:limit]
		last := spans[len(spans)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", last.StartMs, last.SegmentID)))
	}
	segments := make([]playbackCoverage, 0, len(spans))
	for _, s := range spans {
		segments = append(segments, playbackCoverageFor(s))
	}
	writeJSON(w, 200, map[string]any{"segments": segments, "nextCursor": next})
}

func writePlaybackBadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, 400, map[string]string{"error": message, "code": "invalid_request"})
}
func writePlaybackError(w http.ResponseWriter, err error) {
	status, code, message := 500, "playback_unavailable", "playback is temporarily unavailable"
	switch {
	case errors.Is(err, store.ErrPlaybackMediaDeleted):
		status, code, message = 410, "media_deleted", "recording was deleted"
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, store.ErrRecordingSegmentNotFound):
		status, code, message = 404, "media_not_found", "recording was not found"
	case errors.Is(err, store.ErrRecordingSegmentFileMissing):
		status, code, message = 410, "media_missing", "recording file is no longer available"
	}
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}
