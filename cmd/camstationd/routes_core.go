package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"camstation/internal/cleanup"
	"camstation/internal/store"
)

func (d routeDeps) registerCoreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"mode":      "development",
			"startedAt": time.Now().UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("GET /api/cameras", func(w http.ResponseWriter, r *http.Request) {
		cameras, err := d.db.ListCameras(r.Context(), false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		status := d.streamer.Status(r.Context())
		annotateCameraRuntimeStatus(cameras, status)
		writeJSON(w, http.StatusOK, publicCameras(cameras, status))
	})

	mux.HandleFunc("GET /api/layouts", func(w http.ResponseWriter, r *http.Request) {
		layouts, err := d.db.ListLayouts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, layouts)
	})

	mux.HandleFunc("POST /api/layouts", func(w http.ResponseWriter, r *http.Request) {
		var req store.LayoutProfile
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		req.ID = layoutID()
		layout, err := d.db.CreateLayout(r.Context(), req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, layout)
	})

	mux.HandleFunc("PUT /api/layouts/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req store.LayoutProfile
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		layout, err := d.db.UpdateLayout(r.Context(), r.PathValue("id"), req)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, layout)
	})

	mux.HandleFunc("GET /api/timeline", func(w http.ResponseWriter, r *http.Request) {
		streamName := r.URL.Query().Get("cam")
		date := r.URL.Query().Get("date")
		if streamName == "" || date == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("cam and date are required"))
			return
		}
		from, to, err := dayRangeKST(date)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		segmentStreamName := streamName
		if cameras, err := d.db.ListCameras(r.Context(), true); err == nil {
			if camera, ok := cameraByStream(cameras, streamName); ok && camera.RecordingStreamName != "" {
				segmentStreamName = camera.RecordingStreamName
			}
		}
		segments, err := d.db.ListRecordingSegments(r.Context(), segmentStreamName, from, to, "ready", "recording")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"segments":      timelineSegments(segments),
			"motion_events": []any{},
		})
	})

	mux.HandleFunc("GET /api/recorders/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, publicRecorderStatusFromInternal(d.recorderManager.Status()))
	})

	mux.HandleFunc("POST /api/recorders/start", func(w http.ResponseWriter, r *http.Request) {
		cameras, err := d.db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		streamName := r.URL.Query().Get("stream")
		if streamName != "" {
			camera, ok := cameraByStream(cameras, streamName)
			if !ok {
				writeError(w, http.StatusNotFound, fmt.Errorf("camera stream not found: %s", streamName))
				return
			}
			if err := d.recorderManager.Start(camera); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		} else {
			d.recorderManager.Reconcile(cameras)
		}
		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "recorder",
			Level:   "info",
			Message: "recorder workers started",
			Details: map[string]any{"stream": streamName, "cameras": len(cameras), "input": "go2rtc-local-rtsp"},
		})
		writeJSON(w, http.StatusOK, publicRecorderStatusFromInternal(d.recorderManager.Status()))
	})

	mux.HandleFunc("POST /api/recorders/stop", func(w http.ResponseWriter, r *http.Request) {
		streamName := r.URL.Query().Get("stream")
		if streamName != "" {
			d.recorderManager.Stop(streamName)
		} else {
			d.recorderManager.StopAll()
		}
		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "recorder",
			Level:   "info",
			Message: "recorder workers stopped",
			Details: map[string]any{"stream": streamName},
		})
		writeJSON(w, http.StatusOK, publicRecorderStatusFromInternal(d.recorderManager.Status()))
	})

	mux.HandleFunc("GET /api/recordings/storage", func(w http.ResponseWriter, r *http.Request) {
		recordingsBytes, err := cleanup.DirSize(d.recordingsDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		tempBytes, err := cleanup.DirSize(d.tempDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		maxBytes, err := recordingStorageLimitBytes(r.Context(), d.db, d.maxStorageBytes)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, publicRecordingStorage{
			RecordingsDir:      publicManagedRecordingsDir,
			TempDir:            publicManagedTempDir,
			RecordingsBytes:    recordingsBytes,
			TempBytes:          tempBytes,
			MaxBytes:           maxBytes,
			AutoCleanupEnabled: maxBytes > 0,
		})
	})

	mux.HandleFunc("POST /api/recordings/cleanup", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			MaxBytes     int64   `json:"maxBytes"`
			MaxStorageGB float64 `json:"maxStorageGB"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		if req.MaxBytes <= 0 && req.MaxStorageGB > 0 {
			req.MaxBytes = gbToBytes(req.MaxStorageGB)
		}
		if req.MaxBytes <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("maxBytes or maxStorageGB is required"))
			return
		}
		result, err := d.cleaner.EnforceMaxBytes(r.Context(), req.MaxBytes)
		if err != nil {
			_ = d.db.AppendEvent(r.Context(), store.Event{
				Source:  "recording.cleanup",
				Level:   "error",
				Message: "recording cleanup failed",
				Details: map[string]any{"error": err.Error(), "maxBytes": req.MaxBytes},
			})
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "recording.cleanup",
			Level:   "info",
			Message: "recording cleanup completed",
			Details: map[string]any{"maxBytes": result.MaxBytes, "beforeBytes": result.BeforeBytes, "afterBytes": result.AfterBytes, "deleted": len(result.Deleted)},
		})
		writeJSON(w, http.StatusOK, result)
	})
}
