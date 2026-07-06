package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"camstation/internal/store"
)

func (d routeDeps) registerCameraMutationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/cameras", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		var req cameraCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateCameraMutationTargets(r.Context(), req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		saved, result, probeErr, err := persistCameraProfile(r.Context(), d.db, d.prober, req, "")
		if err != nil {
			if errors.Is(err, errBadCameraProfileRequest) {
				writeError(w, http.StatusBadRequest, err)
			} else if errors.Is(err, errCameraProfileScanFailed) {
				writeError(w, http.StatusBadGateway, err)
			} else {
				writeError(w, http.StatusInternalServerError, err)
			}
			return
		}
		level, message := cameraMutationEvent("camera registered", probeErr)
		publicSaved := publicCameraFromStore(saved)

		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "camera",
			Level:   level,
			Message: message,
			Details: map[string]any{
				"name":    saved.Name,
				"stream":  saved.StreamName,
				"state":   saved.State,
				"adapter": saved.ProfileAdapter,
				"result":  result,
				"error":   errString(probeErr),
			},
		})

		cameras, err := d.db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := d.streamer.Restart(r.Context(), cameras); err != nil {
			_ = d.db.AppendEvent(r.Context(), store.Event{
				Source:  "go2rtc",
				Level:   "error",
				Message: "go2rtc restart failed",
				Details: map[string]any{"error": err.Error()},
			})
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":      probeErr == nil,
				"camera":  publicSaved,
				"go2rtc":  publicGo2RTCStatus(d.streamer.Status(r.Context())),
				"warning": publicCameraRestartWarning(err),
			})
			return
		}
		if d.recordingEnabled {
			d.recorderManager.Reconcile(cameras)
		}

		status := http.StatusOK
		if probeErr != nil {
			status = http.StatusAccepted
		}
		writeJSON(w, status, map[string]any{"ok": probeErr == nil, "camera": publicSaved, "go2rtc": publicGo2RTCStatus(d.streamer.Status(r.Context()))})
	})

	mux.HandleFunc("PUT /api/cameras/{streamName}", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		existing, err := d.db.GetCameraByStream(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		var req cameraCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if err := validateCameraMutationTargets(r.Context(), req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		req = cameraUpdateRequest(existing, req)
		saved, result, probeErr, err := persistCameraProfile(r.Context(), d.db, d.prober, req, existing.StreamName)
		if err != nil {
			if errors.Is(err, errBadCameraProfileRequest) {
				writeError(w, http.StatusBadRequest, err)
			} else if errors.Is(err, errCameraProfileScanFailed) {
				writeError(w, http.StatusBadGateway, err)
			} else {
				writeError(w, http.StatusInternalServerError, err)
			}
			return
		}
		level, message := cameraMutationEvent("camera updated", probeErr)
		publicSaved := publicCameraFromStore(saved)

		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "camera",
			Level:   level,
			Message: message,
			Details: map[string]any{
				"name":    saved.Name,
				"stream":  saved.StreamName,
				"state":   saved.State,
				"adapter": saved.ProfileAdapter,
				"result":  result,
				"error":   errString(probeErr),
			},
		})

		cameras, err := d.db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := d.streamer.Restart(r.Context(), cameras); err != nil {
			_ = d.db.AppendEvent(r.Context(), store.Event{
				Source:  "go2rtc",
				Level:   "error",
				Message: "go2rtc restart failed",
				Details: map[string]any{"error": err.Error()},
			})
			if d.recordingEnabled {
				d.recorderManager.Reconcile(cameras)
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":      probeErr == nil,
				"camera":  publicSaved,
				"go2rtc":  publicGo2RTCStatus(d.streamer.Status(r.Context())),
				"warning": publicCameraRestartWarning(err),
			})
			return
		}
		if d.recordingEnabled {
			d.recorderManager.Reconcile(cameras)
		}

		status := http.StatusOK
		if probeErr != nil {
			status = http.StatusAccepted
		}
		writeJSON(w, status, map[string]any{"ok": probeErr == nil, "camera": publicSaved, "go2rtc": publicGo2RTCStatus(d.streamer.Status(r.Context()))})
	})

	mux.HandleFunc("DELETE /api/cameras/{streamName}", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		deleted, err := d.db.DeleteCamera(r.Context(), r.PathValue("streamName"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		publicDeleted := publicCameraFromStore(deleted)

		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "camera",
			Level:   "warning",
			Message: "camera deleted",
			Details: map[string]any{
				"name":   deleted.Name,
				"stream": deleted.StreamName,
				"roles":  len(deleted.Streams),
			},
		})

		cameras, err := d.db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := d.streamer.Restart(r.Context(), cameras); err != nil {
			_ = d.db.AppendEvent(r.Context(), store.Event{
				Source:  "go2rtc",
				Level:   "error",
				Message: "go2rtc restart failed",
				Details: map[string]any{"error": err.Error()},
			})
			if d.recordingEnabled {
				d.recorderManager.Reconcile(cameras)
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"ok":      true,
				"camera":  publicDeleted,
				"go2rtc":  publicGo2RTCStatus(d.streamer.Status(r.Context())),
				"warning": publicCameraRestartWarning(err),
			})
			return
		}
		if d.recordingEnabled {
			d.recorderManager.Reconcile(cameras)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "camera": publicDeleted, "go2rtc": publicGo2RTCStatus(d.streamer.Status(r.Context()))})
	})
}

func publicCameraRestartWarning(err error) string {
	if err == nil {
		return ""
	}
	return "go2rtc restart failed; camera changes were saved"
}
