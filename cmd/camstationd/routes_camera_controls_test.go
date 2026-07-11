package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"camstation/internal/cameracontrol"
	"camstation/internal/store"
)

func TestCameraControlRoutesRequireManagementHeaderForGET(t *testing.T) {
	server := newCameraControlRouteServer(t, &fakeCameraController{})
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodGet, "/api/cameras/goat-yard/controls", "", nil)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
	}
}

func TestCameraControlRoutesStopAndPresetBodies(t *testing.T) {
	fake := &fakeCameraController{presets: []cameracontrol.Preset{{Token: "preset/a?b", Name: "입구"}}}
	server := newCameraControlRouteServer(t, fake)
	headers := trustedConsoleHeaders()
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets/goto", `{"token":"preset/a?b"}`, headers)
	if status != http.StatusOK || fake.gotoPresetToken != "preset/a?b" {
		t.Fatalf("status/token = %d/%q", status, fake.gotoPresetToken)
	}
	status, _ = requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/stop", `{}`, headers)
	if status != http.StatusOK || fake.stopCalls != 1 {
		t.Fatalf("status/stopCalls = %d/%d", status, fake.stopCalls)
	}
}

func TestCameraControlRoutesRejectRoleStreamAlias(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard-live/ptz/stop", `{}`, trustedConsoleHeaders())
	if status != http.StatusNotFound || fake.stopCalls != 0 {
		t.Fatalf("status/stopCalls = %d/%d", status, fake.stopCalls)
	}
}

func TestCameraControlRoutesRefreshPersistsCapabilities(t *testing.T) {
	fake := &fakeCameraController{capabilities: supportedControlCapabilities()}
	server := newCameraControlRouteServer(t, fake)
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/controls/refresh", `{}`, trustedConsoleHeaders())
	if status != http.StatusOK {
		t.Fatalf("status = %d; payload=%v", status, payload)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "goat-yard")
	if err != nil || !stored.ControlCapabilities.PTZ.Available || stored.ControlCapabilities.MaxPresets != 100 {
		t.Fatalf("stored capabilities/error = %#v/%v", stored.ControlCapabilities, err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode refresh response: %v", err)
	}
	for _, secret := range []string{"camera-secret", "192.0.2.10", "rtsp://"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("refresh response leaked %q: %s", secret, encoded)
		}
	}
}

func TestCameraControlRoutesClampMoveAndRejectZero(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	headers := trustedConsoleHeaders()
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/move", `{"pan":3,"tilt":-2,"zoom":0.5}`, headers)
	if status != http.StatusOK || fake.move != (cameracontrol.MoveVector{Pan: 1, Tilt: -1, Zoom: .5}) {
		t.Fatalf("status/move = %d/%#v", status, fake.move)
	}
	status, _ = requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/move", `{"pan":0,"tilt":0,"zoom":0}`, headers)
	if status != http.StatusBadRequest || fake.moveCalls != 1 {
		t.Fatalf("zero status/moveCalls = %d/%d", status, fake.moveCalls)
	}
}

func TestCameraControlRoutesNormalizeControllerErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{name: "invalid", err: cameracontrol.ErrInvalidCommand, status: http.StatusBadRequest, message: "카메라 제어 명령이 올바르지 않습니다."},
		{name: "timeout", err: cameracontrol.ErrTimeout, status: http.StatusGatewayTimeout, message: "카메라 제어 응답 시간이 초과되었습니다."},
		{name: "authentication", err: cameracontrol.ErrAuthenticationFailed, status: http.StatusBadGateway, message: "카메라 인증에 실패했습니다."},
		{name: "unavailable", err: errors.New("camera-secret at http://192.0.2.10/onvif"), status: http.StatusBadGateway, message: "카메라 제어를 사용할 수 없습니다."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newCameraControlRouteServer(t, &fakeCameraController{err: tt.err})
			status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/home/goto", `{}`, trustedConsoleHeaders())
			if status != tt.status || payload["error"] != tt.message {
				t.Fatalf("status/payload = %d/%v", status, payload)
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("encode error response: %v", err)
			}
			for _, secret := range []string{"camera-secret", "192.0.2.10", "rtsp://", "onvif"} {
				if strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(secret)) {
					t.Fatalf("error response leaked %q: %s", secret, encoded)
				}
			}
		})
	}
}

func TestCameraControlRoutesPersistScannedProfileCapabilities(t *testing.T) {
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	var body map[string]any
	if err := json.Unmarshal([]byte(cameraManualSelectionBody(t, "PTZ Save", "ptz-save", routeSyntheticRTSPURL("ptz-save"))), &body); err != nil {
		t.Fatalf("decode request fixture: %v", err)
	}
	body["profile"] = map[string]any{
		"host": "192.168.1.10", "manufacturer": "Synthetic", "model": "PTZ", "adapter": "onvif",
		"capabilities": map[string]any{"ptz": true, "microphone": true, "maxPresets": 100},
		"channels":     []any{map[string]any{"index": 0, "candidates": body["streams"]}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode camera body: %v", err)
	}
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", string(encoded), trustedConsoleHeaders())
	if status != http.StatusOK {
		t.Fatalf("status = %d; payload=%v", status, payload)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "ptz-save")
	if err != nil || !stored.ControlCapabilities.PTZ.Available || !stored.ControlCapabilities.Presets.Available || stored.ControlCapabilities.Listen.Support != store.ControlSupportSupported {
		t.Fatalf("stored capabilities/error = %#v/%v", stored.ControlCapabilities, err)
	}
	cameraPayload := payload["camera"].(map[string]any)
	if _, ok := cameraPayload["controlCapabilities"].(map[string]any); !ok {
		t.Fatalf("public camera missing controlCapabilities: %#v", cameraPayload)
	}
}

func TestCameraControlRoutesUpdateWithoutProfilePreservesCapabilities(t *testing.T) {
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	camera, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name: "Existing PTZ", StreamName: "existing-ptz", URL: routeSyntheticRTSPURL("existing-ptz"), State: "streaming",
		ControlCapabilities: supportedControlCapabilities(),
	})
	if err != nil {
		t.Fatalf("seed camera: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(cameraManualSelectionBody(t, "Existing PTZ Updated", camera.StreamName, camera.URL)), &body); err != nil {
		t.Fatalf("decode request fixture: %v", err)
	}
	delete(body, "profile")
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode camera body: %v", err)
	}
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPut, "/api/cameras/"+camera.StreamName, string(encoded), trustedConsoleHeaders())
	if status != http.StatusOK {
		t.Fatalf("status = %d; payload=%v", status, payload)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil || !stored.ControlCapabilities.PTZ.Available || stored.ControlCapabilities.MaxPresets != 100 {
		t.Fatalf("stored capabilities/error = %#v/%v", stored.ControlCapabilities, err)
	}
}

func TestCameraControlRoutesRejectFieldsOnEmptyBodyActions(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	for _, target := range []string{
		"/api/cameras/goat-yard/controls/refresh",
		"/api/cameras/goat-yard/ptz/stop",
		"/api/cameras/goat-yard/ptz/home/goto",
	} {
		status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, target, `{"host":"camera.invalid"}`, trustedConsoleHeaders())
		if status != http.StatusBadRequest {
			t.Fatalf("POST %s status = %d, want %d", target, status, http.StatusBadRequest)
		}
	}
	if fake.discoverCalls != 0 || fake.stopCalls != 0 || fake.gotoHomeCalls != 0 {
		t.Fatalf("controller calls = discover:%d stop:%d home:%d", fake.discoverCalls, fake.stopCalls, fake.gotoHomeCalls)
	}
}

func TestCameraControlRoutesPersistOverlayAndReconcilePresetNames(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	headers := trustedConsoleHeaders()
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets", `{"name":"입구"}`, headers)
	if status != http.StatusOK || payload["name"] != "입구" {
		t.Fatalf("create = %d/%v", status, payload)
	}
	fake.presets = []cameracontrol.Preset{{Token: "created-token", Name: "PRESET_0"}, {Token: "camera-token", Name: "카메라 이름"}}
	status, list := requestJSONArrayWithHeaders(t, server.handler, http.MethodGet, "/api/cameras/goat-yard/ptz/presets", "", headers)
	pairs := make(map[string]string, len(list))
	for _, preset := range list {
		token, tokenOK := preset["token"].(string)
		name, nameOK := preset["name"].(string)
		if !tokenOK || !nameOK {
			t.Fatalf("invalid preset payload: %#v", preset)
		}
		pairs[token] = name
	}
	if status != http.StatusOK || len(list) != 2 || len(pairs) != 2 || pairs["created-token"] != "입구" || pairs["camera-token"] != "카메라 이름" {
		t.Fatalf("list = %d/%#v", status, list)
	}
	fake.presets = []cameracontrol.Preset{{Token: "camera-token", Name: "카메라 이름"}}
	status, _ = requestJSONArrayWithHeaders(t, server.handler, http.MethodGet, "/api/cameras/goat-yard/ptz/presets", "", headers)
	camera, err := server.db.GetCameraByStream(t.Context(), "goat-yard")
	if err != nil {
		t.Fatalf("get camera: %v", err)
	}
	names, err := server.db.ListCameraPresetNames(t.Context(), camera.ID)
	if status != http.StatusOK || err != nil || len(names) != 0 {
		t.Fatalf("reconcile = %d/%#v/%v", status, names, err)
	}
}

func TestCameraControlRoutesSerializePresetCreateBehindList(t *testing.T) {
	fake := &fakeCameraController{
		listPresetsStarted:  make(chan struct{}),
		releaseListPresets:  make(chan struct{}),
		createPresetStarted: make(chan struct{}),
	}
	server := newCameraControlRouteServer(t, fake)
	headers := trustedConsoleHeaders()
	type listResult struct {
		status int
		list   []map[string]any
	}
	listed := make(chan listResult, 1)
	go func() {
		status, list := requestJSONArrayWithHeaders(t, server.handler, http.MethodGet, "/api/cameras/goat-yard/ptz/presets", "", headers)
		listed <- listResult{status: status, list: list}
	}()
	<-fake.listPresetsStarted

	gotoDone := make(chan int, 1)
	go func() {
		status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets/goto", `{"token":"camera-token"}`, headers)
		gotoDone <- status
	}()
	select {
	case status := <-gotoDone:
		if status != http.StatusOK {
			t.Fatalf("goto while list blocked = %d", status)
		}
	case <-time.After(time.Second):
		close(fake.releaseListPresets)
		<-listed
		<-gotoDone
		t.Fatalf("goto was unnecessarily serialized behind preset list")
	}

	type createResult struct {
		status  int
		payload map[string]any
	}
	createRequested := make(chan struct{})
	created := make(chan createResult, 1)
	go func() {
		close(createRequested)
		status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets", `{"name":"입구"}`, headers)
		created <- createResult{status: status, payload: payload}
	}()
	<-createRequested
	overlapped := false
	select {
	case <-fake.createPresetStarted:
		overlapped = true
	case <-time.After(250 * time.Millisecond):
	}
	close(fake.releaseListPresets)
	result := <-listed
	if result.status != http.StatusOK || len(result.list) != 0 {
		t.Fatalf("stale list = %d/%v", result.status, result.list)
	}
	create := <-created
	if overlapped {
		t.Fatalf("create reached device while preset list was in progress")
	}
	if create.status != http.StatusOK || create.payload["name"] != "입구" {
		t.Fatalf("create = %d/%v", create.status, create.payload)
	}
	camera, err := server.db.GetCameraByStream(t.Context(), "goat-yard")
	if err != nil {
		t.Fatalf("get camera: %v", err)
	}
	names, err := server.db.ListCameraPresetNames(t.Context(), camera.ID)
	if err != nil || names["created-token"] != "입구" {
		t.Fatalf("names/error = %#v/%v", names, err)
	}
}

func TestCameraControlRoutesDeletePresetNameOnlyAfterDeviceSuccess(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	camera, err := server.db.GetCameraByStream(t.Context(), "goat-yard")
	if err != nil {
		t.Fatalf("get camera: %v", err)
	}
	if err := server.db.UpsertCameraPresetName(t.Context(), camera.ID, "saved-token", "입구"); err != nil {
		t.Fatalf("seed preset name: %v", err)
	}
	fake.err = cameracontrol.ErrUnavailable
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets/delete", `{"token":"saved-token"}`, trustedConsoleHeaders())
	names, err := server.db.ListCameraPresetNames(t.Context(), camera.ID)
	if status != http.StatusBadGateway || err != nil || names["saved-token"] != "입구" {
		t.Fatalf("failed delete = %d/%#v/%v", status, names, err)
	}
	fake.err = nil
	status, _ = requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets/delete", `{"token":"saved-token"}`, trustedConsoleHeaders())
	names, err = server.db.ListCameraPresetNames(t.Context(), camera.ID)
	if status != http.StatusOK || err != nil || len(names) != 0 {
		t.Fatalf("successful delete = %d/%#v/%v", status, names, err)
	}
}

func TestCameraControlRoutesCompensateWhenPresetNamePersistenceFails(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	var deleteCameraErr error
	fake.onCreatePreset = func() { _, deleteCameraErr = server.db.DeleteCamera(t.Context(), "goat-yard") }
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets", `{"name":"입구"}`, trustedConsoleHeaders())
	if deleteCameraErr != nil {
		t.Fatalf("delete camera: %v", deleteCameraErr)
	}
	if status != http.StatusBadGateway || fake.deletePresetToken != "created-token" {
		t.Fatalf("compensation = %d/%q/%v", status, fake.deletePresetToken, payload)
	}
	errorMessage, ok := payload["error"].(string)
	if !ok {
		t.Fatalf("error payload = %#v", payload)
	}
	for _, secret := range []string{"rtsp://", "camera.invalid", "FOREIGN KEY"} {
		if strings.Contains(errorMessage, secret) {
			t.Fatalf("leaked %q: %s", secret, errorMessage)
		}
	}
}

type fakeCameraController struct {
	capabilities                       store.CameraControlCapabilities
	status                             cameracontrol.Status
	presets                            []cameracontrol.Preset
	move                               cameracontrol.MoveVector
	gotoPresetToken, deletePresetToken string
	createdPresetName                  string
	moveCalls, stopCalls               int
	discoverCalls, gotoHomeCalls       int
	onCreatePreset                     func()
	listPresetsStarted                 chan struct{}
	releaseListPresets                 chan struct{}
	createPresetStarted                chan struct{}
	err                                error
}

func (f *fakeCameraController) Discover(context.Context, store.Camera) (store.CameraControlCapabilities, error) {
	f.discoverCalls++
	return f.capabilities, f.err
}
func (f *fakeCameraController) Status(context.Context, store.Camera) (cameracontrol.Status, error) {
	return f.status, f.err
}
func (f *fakeCameraController) Move(_ context.Context, _ store.Camera, move cameracontrol.MoveVector) error {
	f.move = move
	f.moveCalls++
	return f.err
}
func (f *fakeCameraController) Stop(context.Context, store.Camera) error {
	f.stopCalls++
	return f.err
}
func (f *fakeCameraController) GotoHome(context.Context, store.Camera) error {
	f.gotoHomeCalls++
	return f.err
}
func (f *fakeCameraController) SetHome(context.Context, store.Camera) error { return f.err }
func (f *fakeCameraController) ListPresets(context.Context, store.Camera) ([]cameracontrol.Preset, error) {
	presets := append([]cameracontrol.Preset(nil), f.presets...)
	if f.listPresetsStarted != nil {
		close(f.listPresetsStarted)
	}
	if f.releaseListPresets != nil {
		<-f.releaseListPresets
	}
	return presets, f.err
}
func (f *fakeCameraController) CreatePreset(_ context.Context, _ store.Camera, name string) (cameracontrol.Preset, error) {
	f.createdPresetName = name
	if f.createPresetStarted != nil {
		close(f.createPresetStarted)
	}
	if f.onCreatePreset != nil {
		f.onCreatePreset()
	}
	return cameracontrol.Preset{Token: "created-token", Name: name}, f.err
}
func (f *fakeCameraController) GotoPreset(_ context.Context, _ store.Camera, token string) error {
	f.gotoPresetToken = token
	return f.err
}
func (f *fakeCameraController) DeletePreset(_ context.Context, _ store.Camera, token string) error {
	f.deletePresetToken = token
	return f.err
}

func newCameraControlRouteServer(t *testing.T, controller cameraControlService) testRouteServer {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	camera, err := db.UpsertCamera(t.Context(), store.Camera{
		Name: "염소장", URL: "rtsp://operator:camera-secret@192.0.2.10/main",
		StreamName: "goat-yard", State: "streaming", Host: "192.0.2.10", ONVIFPort: 80,
		ControlCapabilities: supportedControlCapabilities(),
	})
	if err != nil {
		t.Fatalf("seed camera: %v", err)
	}
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, []store.CameraStream{
		{Role: store.CameraStreamRoleRecording, URL: camera.URL, Go2RTCStreamName: "goat-yard-recording", ProfileToken: "PROFILE_000", State: "streaming"},
		{Role: store.CameraStreamRoleLive, URL: camera.URL, Go2RTCStreamName: "goat-yard-live", ProfileToken: "PROFILE_001", State: "streaming"},
	}); err != nil {
		t.Fatalf("seed streams: %v", err)
	}
	handler, err := (routeDeps{db: db, cameraController: controller}).handler()
	if err != nil {
		t.Fatalf("build routes: %v", err)
	}
	return testRouteServer{handler: handler, db: db}
}

func supportedControlCapabilities() store.CameraControlCapabilities {
	supported := store.CameraControlFeature{Support: store.ControlSupportSupported, Available: true}
	return store.CameraControlCapabilities{PTZ: supported, Home: supported, Presets: supported, MaxPresets: 100}
}
