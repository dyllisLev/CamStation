package main

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"camstation/internal/store"
)

func (d routeDeps) registerRecordingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/recordings/segments", d.handleRecordingSegmentsList)
	mux.HandleFunc("GET /api/recordings/segments/{id}", d.handleRecordingSegmentDetail)
	mux.HandleFunc("GET /api/recordings/segments/{id}/download", d.handleRecordingSegmentDownload)
	mux.HandleFunc("GET /api/recordings/segments/{id}/play", d.handleRecordingSegmentPlay)
	mux.HandleFunc("DELETE /api/recordings/segments/{id}", d.handleRecordingSegmentDelete)
}

func (d routeDeps) handleRecordingSegmentsList(w http.ResponseWriter, r *http.Request) {
	filter, err := recordingSegmentFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	segments, err := d.db.ListRecordingSegmentsForConsole(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"segments": recordingSegmentResponses(segments)})
}

func (d routeDeps) handleRecordingSegmentDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := recordingSegmentID(w, r)
	if !ok {
		return
	}
	segment, err := d.db.GetRecordingSegmentByID(r.Context(), id)
	if err != nil {
		writeRecordingSegmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, recordingSegmentResponseFromStore(segment))
}

func (d routeDeps) handleRecordingSegmentDownload(w http.ResponseWriter, r *http.Request) {
	id, ok := recordingSegmentID(w, r)
	if !ok {
		return
	}
	segment, file, info, err := d.db.OpenReadyRecordingSegmentFile(r.Context(), id, d.recordingsDir)
	if err != nil {
		writeRecordingSegmentError(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": segment.Filename}))
	if strings.EqualFold(filepath.Ext(segment.Filename), ".mp4") {
		w.Header().Set("Content-Type", "video/mp4")
	}
	http.ServeContent(w, r, segment.Filename, info.ModTime(), file)
}

func (d routeDeps) handleRecordingSegmentPlay(w http.ResponseWriter, r *http.Request) {
	id, ok := recordingSegmentID(w, r)
	if !ok {
		return
	}
	segment, file, info, err := d.db.OpenReadyRecordingSegmentFile(r.Context(), id, d.recordingsDir)
	if err != nil {
		writeRecordingSegmentError(w, err)
		return
	}
	defer file.Close()
	if strings.EqualFold(filepath.Ext(segment.Filename), ".mp4") {
		w.Header().Set("Content-Type", "video/mp4")
	}
	http.ServeContent(w, r, segment.Filename, info.ModTime(), file)
}

func (d routeDeps) handleRecordingSegmentDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := recordingSegmentID(w, r)
	if !ok {
		return
	}
	segment, err := d.db.DeleteReadyRecordingSegmentFile(r.Context(), id, d.recordingsDir)
	if err != nil {
		writeRecordingSegmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, recordingSegmentResponseFromStore(segment))
}

func recordingSegmentFilter(r *http.Request) (store.RecordingSegmentFilter, error) {
	query := r.URL.Query()
	filter := store.RecordingSegmentFilter{
		StreamName: strings.TrimSpace(query.Get("stream")),
		Statuses:   splitRecordingStatuses(query["status"]),
	}
	if limit := strings.TrimSpace(query.Get("limit")); limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil || parsed < 0 {
			return store.RecordingSegmentFilter{}, fmt.Errorf("limit must be a positive integer")
		}
		filter.Limit = parsed
	}
	from, err := optionalSegmentTime(query.Get("from"))
	if err != nil {
		return store.RecordingSegmentFilter{}, fmt.Errorf("from must be unix seconds or RFC3339: %w", err)
	}
	to, err := optionalSegmentTime(query.Get("to"))
	if err != nil {
		return store.RecordingSegmentFilter{}, fmt.Errorf("to must be unix seconds or RFC3339: %w", err)
	}
	filter.From = from
	filter.To = to
	return filter, nil
}

func recordingSegmentID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("recording segment id must be a positive integer"))
		return 0, false
	}
	return id, true
}

func writeRecordingSegmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrRecordingSegmentNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrRecordingSegmentNotReady):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrRecordingSegmentDeleteConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrRecordingSegmentUnsafePath):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrRecordingSegmentFileMissing):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

func splitRecordingStatuses(values []string) []string {
	statuses := make([]string, 0, len(values))
	for _, value := range values {
		for _, status := range strings.Split(value, ",") {
			status = strings.TrimSpace(status)
			if status != "" {
				statuses = append(statuses, status)
			}
		}
	}
	return statuses
}

func optionalSegmentTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		parsed := time.Unix(unix, 0)
		return &parsed, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
