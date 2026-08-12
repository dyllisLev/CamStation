package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"camstation/internal/store"
)

func (d routeDeps) registerProbeRoute(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/camera/probe", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		var req struct {
			URL     string `json:"url"`
			Timeout int    `json:"timeoutSeconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		if req.URL == "" {
			writeSafeError(w, http.StatusBadRequest, fmt.Errorf("url is required"))
			return
		}
		if err := validateProbeTarget(r.Context(), req.URL); err != nil {
			writeSafeError(w, http.StatusBadRequest, err)
			return
		}
		timeout := time.Duration(req.Timeout) * time.Second
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = 12 * time.Second
		}

		release, err := withCameraNetworkSlot(r.Context())
		if err != nil {
			writeSafeError(w, http.StatusTooManyRequests, err)
			return
		}
		defer release()
		result, err := d.prober.Probe(r.Context(), req.URL, timeout)
		publicResult := safeProbeResult(result, req.URL, err != nil)
		level := "info"
		message := "camera probe succeeded"
		status := http.StatusOK
		if err != nil {
			level = "error"
			message = "camera probe failed"
			status = http.StatusBadGateway
		}
		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "camera.probe",
			Level:   level,
			Message: message,
			Details: map[string]any{"result": publicResult, "error": safeCameraError(err)},
		})
		if err != nil {
			writeJSON(w, status, map[string]any{"ok": false, "error": safeCameraError(err), "result": publicResult})
			return
		}
		writeJSON(w, status, map[string]any{"ok": true, "result": publicResult})
	})
}
