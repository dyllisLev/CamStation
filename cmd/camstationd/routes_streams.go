package main

import (
	"fmt"
	"net/http"
	"time"

	"camstation/internal/camera"
	"camstation/internal/store"
)

type streamOperationResponse struct {
	OK         bool               `json:"ok"`
	Operation  string             `json:"operation"`
	StreamName string             `json:"streamName"`
	CameraName string             `json:"cameraName"`
	Status     publicStreamStatus `json:"status"`
	Probe      *streamProbeDTO    `json:"probe,omitempty"`
	Message    string             `json:"message,omitempty"`
}

type streamProbeDTO struct {
	Reachable bool      `json:"reachable"`
	Format    string    `json:"format,omitempty"`
	Streams   int       `json:"streams"`
	Failure   string    `json:"failure,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

func (d routeDeps) registerStreamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/streams/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, publicGo2RTCStatus(d.streamer.Status(r.Context())))
	})

	mux.HandleFunc("POST /api/streams/restart", func(w http.ResponseWriter, r *http.Request) {
		cameras, err := d.db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := d.streamer.Restart(r.Context(), cameras); err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		if d.recordingEnabled {
			d.recorderManager.Reconcile(cameras)
		}
		writeJSON(w, http.StatusOK, publicGo2RTCStatus(d.streamer.Status(r.Context())))
	})

	mux.HandleFunc("POST /api/streams/{stream}/restart", func(w http.ResponseWriter, r *http.Request) {
		cameraRow, ok := d.streamCamera(w, r)
		if !ok {
			return
		}
		cameras, err := d.db.ListCameras(r.Context(), true)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := d.streamer.Restart(r.Context(), cameras); err != nil {
			_ = d.auditStreamOperation(r, "restart", cameraRow, "error", "stream restart failed", err.Error())
			writeError(w, http.StatusBadGateway, fmt.Errorf("stream restart failed"))
			return
		}
		if d.recordingEnabled {
			d.recorderManager.Reconcile(cameras)
		}
		message := "stream restart requested"
		_ = d.auditStreamOperation(r, "restart", cameraRow, "info", "stream restart requested", message)
		writeJSON(w, http.StatusOK, streamOperationResponse{
			OK:         true,
			Operation:  "restart",
			StreamName: cameraRow.StreamName,
			CameraName: cameraRow.Name,
			Status:     publicGo2RTCStatus(d.streamer.Status(r.Context())),
			Message:    message,
		})
	})

	mux.HandleFunc("POST /api/streams/{stream}/probe", func(w http.ResponseWriter, r *http.Request) {
		cameraRow, ok := d.streamCamera(w, r)
		if !ok {
			return
		}
		probe := streamProbeDTO{CheckedAt: time.Now().UTC()}
		if d.prober == nil {
			probe.Failure = "camera prober unavailable"
		} else {
			result, err := d.prober.Probe(r.Context(), cameraRow.URL, 12*time.Second)
			probe = streamProbeDTO{
				Reachable: result.Reachable,
				Format:    result.Format,
				Streams:   len(result.Streams),
				Failure:   camera.RedactText(result.Failure, cameraRow.URL),
				CheckedAt: result.CheckedAt,
			}
			if err != nil && probe.Failure == "" {
				probe.Failure = camera.RedactText(err.Error(), cameraRow.URL)
			}
		}
		_ = d.auditStreamOperation(r, "probe", cameraRow, "info", "stream probe requested", probe.Failure)
		writeJSON(w, http.StatusOK, streamOperationResponse{
			OK:         probe.Failure == "",
			Operation:  "probe",
			StreamName: cameraRow.StreamName,
			CameraName: cameraRow.Name,
			Status:     publicGo2RTCStatus(d.streamer.Status(r.Context())),
			Probe:      &probe,
		})
	})

	mux.HandleFunc("DELETE /api/streams/{stream}", func(w http.ResponseWriter, r *http.Request) {
		cameraRow, ok := d.streamCamera(w, r)
		if !ok {
			return
		}
		_ = d.auditStreamOperation(r, "delete_rejected", cameraRow, "warning", "stream delete rejected", "camera management owns stream deletion")
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":      "stream deletion belongs to camera management",
			"streamName": cameraRow.StreamName,
		})
	})
}

func (d routeDeps) streamCamera(w http.ResponseWriter, r *http.Request) (store.Camera, bool) {
	streamName := r.PathValue("stream")
	cameraRow, err := d.db.GetCameraByStream(r.Context(), streamName)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("stream not found: %s", streamName))
		return store.Camera{}, false
	}
	if !cameraRow.Enabled {
		writeCameraDisabled(w)
		return store.Camera{}, false
	}
	return cameraRow, true
}

func (d routeDeps) auditStreamOperation(r *http.Request, operation string, cameraRow store.Camera, level string, message string, detail string) error {
	return d.db.AppendEvent(r.Context(), store.Event{
		Source:  "stream",
		Level:   level,
		Message: message,
		Details: map[string]any{
			"operation":  operation,
			"streamName": cameraRow.StreamName,
			"cameraName": store.RedactText(cameraRow.Name),
			"detail":     store.RedactText(detail),
		},
	})
}
