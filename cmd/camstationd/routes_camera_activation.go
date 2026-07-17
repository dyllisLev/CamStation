package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

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

		d.activationMu.Lock()
		defer d.activationMu.Unlock()
		applyCtx, cancelApply := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
		defer cancelApply()

		streamName := r.PathValue("streamName")
		camera, err := d.db.GetCameraByStream(applyCtx, streamName)
		if err != nil || camera.StreamName != streamName {
			writeSafeError(w, http.StatusNotFound, store.ErrNotFound)
			return
		}
		changed := camera.Enabled != *req.Enabled
		if changed {
			err = d.db.SetCameraEnabled(applyCtx, camera.StreamName, *req.Enabled)
		}
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		result := d.applyPolicies(applyCtx)
		if !result.Applied {
			recoveryCtx, cancelRecovery := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
			defer cancelRecovery()
			var restoreErr error
			if changed {
				restoreErr = d.db.SetCameraEnabled(recoveryCtx, camera.StreamName, camera.Enabled)
			}
			recovery := result
			if restoreErr == nil {
				recovery = d.applyPolicies(recoveryCtx)
			}
			fresh, _ := d.db.GetCameraByStream(recoveryCtx, camera.StreamName)
			_ = d.db.AppendEvent(recoveryCtx, store.Event{
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

		fresh, err := d.db.GetCameraByStream(applyCtx, camera.StreamName)
		if err != nil {
			writeSafeError(w, http.StatusInternalServerError, err)
			return
		}
		if d.recordingEnabled && d.recorderManager != nil {
			cameras, listErr := d.db.ListCameras(applyCtx, true)
			if listErr != nil {
				writeSafeError(w, http.StatusInternalServerError, listErr)
				return
			}
			d.recorderManager.Reconcile(appliedRecordingCameras(cameras))
		}
		_ = d.db.AppendEvent(applyCtx, store.Event{
			Source: "camera", Level: "info", Message: "camera activation changed",
			Details: map[string]any{"name": fresh.Name, "stream": fresh.StreamName, "enabled": fresh.Enabled},
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"saved": true, "applied": true, "camera": publicCameraFromStore(fresh, d.streamer.Status(applyCtx)),
		})
	})
}

func writeCameraDisabled(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "비활성 카메라입니다."})
}
