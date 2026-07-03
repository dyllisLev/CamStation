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
		var req struct {
			URL     string `json:"url"`
			Timeout int    `json:"timeoutSeconds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.URL == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("url is required"))
			return
		}
		timeout := time.Duration(req.Timeout) * time.Second
		if timeout <= 0 || timeout > 30*time.Second {
			timeout = 12 * time.Second
		}

		result, err := d.prober.Probe(r.Context(), req.URL, timeout)
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
			Details: map[string]any{"result": result, "error": errString(err)},
		})
		if err != nil {
			writeJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "result": result})
			return
		}
		writeJSON(w, status, map[string]any{"ok": true, "result": result})
	})
}
