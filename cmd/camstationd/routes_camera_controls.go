package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"camstation/internal/cameracontrol"
	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

const (
	cameraControlRouteTimeout   = 2500 * time.Millisecond
	cameraControlRefreshTimeout = 8 * time.Second
)

type moveCameraRequest struct {
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
	Zoom float64 `json:"zoom"`
}

type cameraPresetNameRequest struct {
	Name string `json:"name"`
}

type cameraPresetTokenRequest struct {
	Token string `json:"token"`
}

func (d routeDeps) registerCameraControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cameras/{streamName}/controls", d.getCameraControls)
	mux.HandleFunc("POST /api/cameras/{streamName}/controls/refresh", d.refreshCameraControls)
	mux.HandleFunc("POST /api/cameras/{streamName}/ptz/move", d.moveCamera)
	mux.HandleFunc("POST /api/cameras/{streamName}/ptz/stop", d.stopCamera)
	mux.HandleFunc("POST /api/cameras/{streamName}/ptz/home/goto", d.gotoCameraHome)
	mux.HandleFunc("POST /api/cameras/{streamName}/ptz/home/set", d.setCameraHome)
	mux.HandleFunc("GET /api/cameras/{streamName}/ptz/presets", d.listCameraPresets)
	mux.HandleFunc("POST /api/cameras/{streamName}/ptz/presets", d.createCameraPreset)
	mux.HandleFunc("POST /api/cameras/{streamName}/ptz/presets/goto", d.gotoCameraPreset)
	mux.HandleFunc("POST /api/cameras/{streamName}/ptz/presets/delete", d.deleteCameraPreset)
}

func (d routeDeps) getCameraControls(w http.ResponseWriter, r *http.Request) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlCamera(r.Context(), r.PathValue("streamName"))
	if err != nil {
		writeCameraControlError(w, err)
		return
	}
	status := cameracontrol.Status{PanTilt: "UNKNOWN", Zoom: "UNKNOWN"}
	if camera.State == "streaming" && camera.ControlCapabilities.PTZ.Available {
		ctx, cancel := context.WithTimeout(r.Context(), cameraControlRouteTimeout)
		defer cancel()
		status, err = d.cameraController.Status(ctx, camera)
		if err != nil {
			d.recordCameraControlFailure(r.Context(), camera.StreamName, "status", err)
			writeCameraControlError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": camera.ControlCapabilities, "status": status})
}

func (d routeDeps) refreshCameraControls(w http.ResponseWriter, r *http.Request) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlCamera(r.Context(), r.PathValue("streamName"))
	if err != nil {
		writeCameraControlError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cameraControlRefreshTimeout)
	defer cancel()
	capabilities, err := d.cameraController.Discover(ctx, camera)
	if err != nil {
		d.recordCameraControlFailure(r.Context(), camera.StreamName, "refresh", err)
		writeCameraControlError(w, err)
		return
	}
	if err := d.db.UpdateCameraControlCapabilities(r.Context(), camera.StreamName, capabilities); err != nil {
		writeCameraControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"capabilities": capabilities})
}

func (d routeDeps) moveCamera(w http.ResponseWriter, r *http.Request) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlCamera(r.Context(), r.PathValue("streamName"))
	if err != nil {
		writeCameraControlError(w, err)
		return
	}
	if !cameraControlAvailable(camera, camera.ControlCapabilities.PTZ) {
		writeControlUnavailable(w)
		return
	}
	var req moveCameraRequest
	if !decodeControlJSON(w, r, &req) {
		return
	}
	move := cameracontrol.MoveVector{Pan: clampControl(req.Pan), Tilt: clampControl(req.Tilt), Zoom: clampControl(req.Zoom)}
	if move.Pan == 0 && move.Tilt == 0 && move.Zoom == 0 {
		writeCameraControlError(w, cameracontrol.ErrInvalidCommand)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cameraControlRouteTimeout)
	defer cancel()
	if err := d.cameraController.Move(ctx, camera, move); err != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), cameraControlRouteTimeout)
		_ = d.cameraController.Stop(stopCtx, camera)
		stopCancel()
		d.recordCameraControlFailure(r.Context(), camera.StreamName, "move", err)
		writeCameraControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d routeDeps) stopCamera(w http.ResponseWriter, r *http.Request) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlCamera(r.Context(), r.PathValue("streamName"))
	if err != nil {
		writeCameraControlError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cameraControlRouteTimeout)
	defer cancel()
	if err := d.cameraController.Stop(ctx, camera); err != nil {
		d.recordCameraControlFailure(r.Context(), camera.StreamName, "stop", err)
		writeCameraControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d routeDeps) gotoCameraHome(w http.ResponseWriter, r *http.Request) {
	d.cameraHomeAction(w, r, "home_goto", false)
}

func (d routeDeps) setCameraHome(w http.ResponseWriter, r *http.Request) {
	d.cameraHomeAction(w, r, "home_set", true)
}

func (d routeDeps) cameraHomeAction(w http.ResponseWriter, r *http.Request, operation string, set bool) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlCamera(r.Context(), r.PathValue("streamName"))
	if err != nil {
		writeCameraControlError(w, err)
		return
	}
	if !cameraControlAvailable(camera, camera.ControlCapabilities.Home) {
		writeControlUnavailable(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cameraControlRouteTimeout)
	defer cancel()
	if set {
		err = d.cameraController.SetHome(ctx, camera)
	} else {
		err = d.cameraController.GotoHome(ctx, camera)
	}
	if err != nil {
		d.recordCameraControlFailure(r.Context(), camera.StreamName, operation, err)
		writeCameraControlError(w, err)
		return
	}
	if set {
		d.recordCameraControlSuccess(r.Context(), camera.StreamName, operation)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d routeDeps) listCameraPresets(w http.ResponseWriter, r *http.Request) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlPresetCamera(w, r)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cameraControlRouteTimeout)
	defer cancel()
	presets, err := d.cameraController.ListPresets(ctx, camera)
	if err != nil {
		d.recordCameraControlFailure(r.Context(), camera.StreamName, "preset_list", err)
		writeCameraControlError(w, err)
		return
	}
	if presets == nil {
		presets = []cameracontrol.Preset{}
	}
	writeJSON(w, http.StatusOK, presets)
}

func (d routeDeps) createCameraPreset(w http.ResponseWriter, r *http.Request) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlPresetCamera(w, r)
	if err != nil {
		return
	}
	var req cameraPresetNameRequest
	if !decodeControlJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !validControlValue(req.Name) {
		writeCameraControlError(w, cameracontrol.ErrInvalidCommand)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cameraControlRouteTimeout)
	defer cancel()
	preset, err := d.cameraController.CreatePreset(ctx, camera, req.Name)
	if err != nil {
		d.recordCameraControlFailure(r.Context(), camera.StreamName, "preset_create", err)
		writeCameraControlError(w, err)
		return
	}
	d.recordCameraControlSuccess(r.Context(), camera.StreamName, "preset_create")
	writeJSON(w, http.StatusOK, preset)
}

func (d routeDeps) gotoCameraPreset(w http.ResponseWriter, r *http.Request) {
	d.cameraPresetTokenAction(w, r, "preset_goto", false)
}

func (d routeDeps) deleteCameraPreset(w http.ResponseWriter, r *http.Request) {
	d.cameraPresetTokenAction(w, r, "preset_delete", true)
}

func (d routeDeps) cameraPresetTokenAction(w http.ResponseWriter, r *http.Request, operation string, delete bool) {
	if !requireCameraManagementRequest(w, r) {
		return
	}
	camera, err := d.controlPresetCamera(w, r)
	if err != nil {
		return
	}
	var req cameraPresetTokenRequest
	if !decodeControlJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Token) == "" || !validControlValue(req.Token) {
		writeCameraControlError(w, cameracontrol.ErrInvalidCommand)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), cameraControlRouteTimeout)
	defer cancel()
	if delete {
		err = d.cameraController.DeletePreset(ctx, camera, req.Token)
	} else {
		err = d.cameraController.GotoPreset(ctx, camera, req.Token)
	}
	if err != nil {
		d.recordCameraControlFailure(r.Context(), camera.StreamName, operation, err)
		writeCameraControlError(w, err)
		return
	}
	if delete {
		d.recordCameraControlSuccess(r.Context(), camera.StreamName, operation)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d routeDeps) controlPresetCamera(w http.ResponseWriter, r *http.Request) (store.Camera, error) {
	camera, err := d.controlCamera(r.Context(), r.PathValue("streamName"))
	if err != nil {
		writeCameraControlError(w, err)
		return store.Camera{}, err
	}
	if !cameraControlAvailable(camera, camera.ControlCapabilities.Presets) {
		writeControlUnavailable(w)
		return store.Camera{}, cameracontrol.ErrUnavailable
	}
	return camera, nil
}

func (d routeDeps) controlCamera(ctx context.Context, streamName string) (store.Camera, error) {
	camera, err := d.db.GetCameraByStream(ctx, streamName)
	if err != nil || camera.StreamName != streamName {
		return store.Camera{}, sql.ErrNoRows
	}
	return camera, nil
}

func decodeControlJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "제어 요청 형식이 올바르지 않습니다."})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "제어 요청은 하나의 JSON 객체여야 합니다."})
		return false
	}
	return true
}

func writeCameraControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cameracontrol.ErrInvalidCommand):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "카메라 제어 명령이 올바르지 않습니다."})
	case errors.Is(err, cameracontrol.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "카메라 제어 응답 시간이 초과되었습니다."})
	case errors.Is(err, cameracontrol.ErrAuthenticationFailed):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "카메라 인증에 실패했습니다."})
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "등록된 카메라를 찾을 수 없습니다."})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "카메라 제어를 사용할 수 없습니다."})
	}
}

func writeControlUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]string{"error": "카메라가 제어 가능한 상태가 아닙니다."})
}

func cameraControlAvailable(camera store.Camera, feature store.CameraControlFeature) bool {
	return camera.State == "streaming" && feature.Support == store.ControlSupportSupported && feature.Available
}

func validControlValue(value string) bool {
	count := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && count >= 1 && count <= 64
}

func clampControl(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Max(-1, math.Min(1, value))
}

func (d routeDeps) recordCameraControlSuccess(ctx context.Context, streamName, operation string) {
	_ = d.db.AppendEvent(ctx, store.Event{
		Source: "camera-control", Level: "info", Message: "카메라 제어 요청 완료",
		Details: map[string]any{"streamName": streamName, "operation": operation},
	})
}

func (d routeDeps) recordCameraControlFailure(ctx context.Context, streamName, operation string, err error) {
	_ = d.db.AppendEvent(ctx, store.Event{
		Source: "camera-control", Level: "warning", Message: "카메라 제어 요청 실패",
		Details: map[string]any{"streamName": streamName, "operation": operation, "category": cameraControlErrorCategory(err)},
	})
}

func cameraControlErrorCategory(err error) string {
	switch {
	case errors.Is(err, cameracontrol.ErrInvalidCommand):
		return "invalid"
	case errors.Is(err, cameracontrol.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, cameracontrol.ErrAuthenticationFailed):
		return "authentication"
	case errors.Is(err, sql.ErrNoRows):
		return "not_found"
	default:
		return "unavailable"
	}
}

func controlCapabilitiesFromProfile(profile cameraprofile.DeviceProfile) store.CameraControlCapabilities {
	unknown := store.CameraControlFeature{Support: store.ControlSupportUnknown, Reason: "protocol_unverified"}
	caps := store.CameraControlCapabilities{PTZ: unknown, Home: unknown, Presets: unknown, Listen: unknown, Talk: unknown, Siren: unknown}
	if profile.Capabilities.PTZ {
		caps.PTZ = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: true}
	}
	if profile.Capabilities.MaxPresets > 0 {
		caps.Presets = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: true}
		caps.MaxPresets = profile.Capabilities.MaxPresets
	}
	if profile.Capabilities.Microphone {
		caps.Listen = store.CameraControlFeature{Support: store.ControlSupportSupported, Reason: "browser_audio_unavailable"}
	}
	return caps
}
