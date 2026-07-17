package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"camstation/internal/store"
)

var errCameraDisabled = errors.New("camera is disabled")

type cameraActivationRequest struct {
	Enabled *bool `json:"enabled"`
}

func (d routeDeps) registerCameraActivationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PATCH /api/cameras/{streamName}/enabled", func(w http.ResponseWriter, r *http.Request) {
		if !requireCameraManagementRequest(w, r) {
			return
		}
		var req cameraActivationRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil || req.Enabled == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "활성 상태 요청이 올바르지 않습니다."})
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "활성 상태 요청은 하나의 JSON 객체여야 합니다."})
			return
		}

		streamName := r.PathValue("streamName")
		camera, err := d.db.GetCameraByStream(r.Context(), streamName)
		if err != nil || camera.StreamName != streamName {
			writeSafeError(w, http.StatusNotFound, store.ErrNotFound)
			return
		}
		if camera.Enabled == *req.Enabled {
			writeJSON(w, http.StatusOK, map[string]any{
				"saved": true, "applied": true, "camera": publicCameraFromStore(camera, d.streamer.Status(r.Context())),
			})
			return
		}

		if err := d.db.SetCameraEnabled(r.Context(), camera.StreamName, *req.Enabled); err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		result := d.applyPolicies(r.Context())
		if !result.Applied {
			restoreErr := d.db.SetCameraEnabled(r.Context(), camera.StreamName, camera.Enabled)
			recovery := result
			if restoreErr == nil {
				recovery = d.applyPolicies(r.Context())
			}
			fresh, _ := d.db.GetCameraByStream(r.Context(), camera.StreamName)
			_ = d.db.AppendEvent(r.Context(), store.Event{
				Source: "camera", Level: "error", Message: "camera activation apply failed",
				Details: map[string]any{"name": camera.Name, "stream": camera.StreamName, "enabled": *req.Enabled},
			})
			status := http.StatusBadGateway
			message := "활성 상태를 적용하지 못해 이전 상태로 복원했습니다."
			if restoreErr != nil || !recovery.Applied {
				status = http.StatusServiceUnavailable
				message = "활성 상태 적용과 이전 상태 복원을 확인하지 못했습니다."
			}
			writeJSON(w, status, map[string]any{
				"saved": false, "applied": recovery.Applied, "camera": publicCameraFromStore(fresh), "error": message,
			})
			return
		}

		fresh, err := d.db.GetCameraByStream(r.Context(), camera.StreamName)
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		if d.recordingEnabled && d.recorderManager != nil {
			cameras, listErr := d.db.ListCameras(r.Context(), true)
			if listErr != nil {
				writeSafeError(w, http.StatusInternalServerError, listErr)
				return
			}
			d.recorderManager.Reconcile(appliedRecordingCameras(cameras))
		}
		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source: "camera", Level: "info", Message: "camera activation changed",
			Details: map[string]any{"name": fresh.Name, "stream": fresh.StreamName, "enabled": fresh.Enabled},
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"saved": true, "applied": true, "camera": publicCameraFromStore(fresh, d.streamer.Status(r.Context())),
		})
	})
}

func writeCameraDisabled(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "비활성 카메라입니다."})
}
