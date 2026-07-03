package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/cameraprofile"
	"camstation/internal/store"
	"camstation/internal/stream"
)

func TestConsoleLayoutKeepsDesktopSidebar(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "layouts", "ConsoleLayout.tsx"))
	if err != nil {
		t.Fatalf("read console layout: %v", err)
	}
	content := string(source)

	if !strings.Contains(content, `className="new-console-sidebar`) {
		t.Fatalf("ConsoleLayout must keep the desktop left sidebar")
	}
	if strings.Contains(content, `className="new-command new-console-command"`) {
		t.Fatalf("ConsoleLayout should not use the live top command bar for console navigation")
	}
	if strings.Contains(content, `location.pathname === "/" || location.pathname === "/live"`) {
		t.Fatalf("ConsoleLayout must not treat the control room route as a fullscreen live workspace")
	}
	if !strings.Contains(content, `const isLiveWorkspace = location.pathname === "/live";`) {
		t.Fatalf("ConsoleLayout should only fullscreen the live route")
	}
}

func TestConsolePagesKeepSeparateRoles(t *testing.T) {
	t.Parallel()

	controlRoom, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "pages", "ControlRoomPage.tsx"))
	if err != nil {
		t.Fatalf("read control room page: %v", err)
	}
	live, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "pages", "LivePage.tsx"))
	if err != nil {
		t.Fatalf("read live page: %v", err)
	}

	if strings.Contains(string(controlRoom), "LiveWorkspace") {
		t.Fatalf("ControlRoomPage must not render LiveWorkspace directly")
	}
	if !strings.Contains(string(controlRoom), "ControlRoomDashboard") {
		t.Fatalf("ControlRoomPage must render ControlRoomDashboard")
	}
	if !strings.Contains(string(live), "LiveWorkspace") {
		t.Fatalf("LivePage must keep rendering LiveWorkspace")
	}
	for _, required := range []string{
		"useCameras",
		"useStreamStatus",
		"useRecorderStatus",
		"useRecordingStorage",
		"useEvents",
		"new-control-summary",
		"시청 연결",
		"저장공간",
	} {
		if !strings.Contains(string(controlRoom), required) {
			t.Fatalf("ControlRoomPage missing dashboard requirement %q", required)
		}
	}
	for _, required := range []string{
		"new-control-table",
		"카메라 연결",
		"스트림 상태",
		"녹화",
		"최근 오류",
		"new-control-ops",
		"Recorder workers",
		"Recent events",
	} {
		if !strings.Contains(string(controlRoom), required) {
			t.Fatalf("ControlRoomPage missing table or operations requirement %q", required)
		}
	}
	for _, required := range []string{
		"CameraPreviewModal",
		"previewCamera",
		"new-preview-modal",
		"useMseStream",
	} {
		if !strings.Contains(string(controlRoom), required) {
			t.Fatalf("ControlRoomPage missing preview modal requirement %q", required)
		}
	}
	for _, forbidden := range []string{
		"useRestartStreams",
		"RotateCw",
		"재시작",
		"스트림 재시작",
	} {
		if strings.Contains(string(controlRoom), forbidden) {
			t.Fatalf("ControlRoomPage should not expose restart controls; found %q", forbidden)
		}
	}
}

func TestCamerasPageUsesProfileRegistrationFlow(t *testing.T) {
	t.Parallel()

	paths := []string{
		filepath.Join("..", "..", "web", "src", "app", "api.ts"),
		filepath.Join("..", "..", "web", "src", "app", "queries.ts"),
		filepath.Join("..", "..", "web", "src", "pages", "CamerasPage.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "CameraProfileRegistration.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "RegisteredCameraTable.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "RegisteredCameraProfile.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "RegisteredCameraEditForm.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "RegisteredCameraDeleteControls.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "RegisteredCameraStoredProfile.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "CameraSummary.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "ProfileSelectionPanel.tsx"),
		filepath.Join("..", "..", "web", "src", "pages", "cameras", "model.ts"),
	}
	var content strings.Builder
	for _, path := range paths {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read cameras source %s: %v", path, err)
		}
		content.Write(source)
	}

	for _, required := range []string{
		"useScanCamera",
		"usePreviewCamera",
		"useScanRegisteredCamera",
		"usePreviewRegisteredCamera",
		"프로파일 스캔",
		"장비 프로파일",
		"녹화 프로필",
		"라이브 프로필",
		"미리보기",
		"streamSelections",
		"VStarcam",
		"selectedCameraId",
		"onSelectCamera",
		"등록된 카메라",
		"프로파일 수정",
		"프로파일 재스캔",
		"수정 저장",
		"카메라 삭제",
		"scanRegisteredCamera",
		"previewRegisteredCamera",
		"updateCamera",
		"deleteCamera",
		"녹화 스트림",
		"라이브 스트림",
	} {
		if !strings.Contains(content.String(), required) {
			t.Fatalf("CamerasPage missing profile registration requirement %q", required)
		}
	}
	for _, forbidden := range []string{
		"Register Camera",
		"Registered Cameras",
		"FeatureMatrix",
		"Save and probe",
		"Camera Operations",
	} {
		if strings.Contains(content.String(), forbidden) {
			t.Fatalf("CamerasPage still contains legacy UI marker %q", forbidden)
		}
	}
}

func TestAnnotateCameraRuntimeStatusPrefersRunningRoleStream(t *testing.T) {
	t.Parallel()

	cameras := []store.Camera{{
		Name:                "염소장",
		StreamName:          "goat-yard",
		RecordingStreamName: "goat-yard-recording",
		LiveStreamName:      "goat-yard-live",
		State:               "offline",
		Streams: []store.CameraStream{{
			Role:             store.CameraStreamRoleRecording,
			Go2RTCStreamName: "goat-yard-recording",
			State:            "offline",
		}},
	}}

	annotateCameraRuntimeStatus(cameras, stream.Status{Streams: map[string]stream.StreamRuntime{
		"goat-yard-recording": {State: "running", ProducerCount: 1, ConsumerCount: 1},
		"goat-yard-live":      {State: "running", ProducerCount: 1},
	}})

	if cameras[0].State != "streaming" {
		t.Fatalf("camera state = %q, want streaming", cameras[0].State)
	}
	if cameras[0].Streams[0].State != "running" {
		t.Fatalf("role stream state = %q, want running", cameras[0].Streams[0].State)
	}
}

func TestSelectProfileCandidatesKeepsSelectedRoles(t *testing.T) {
	t.Parallel()

	profile := cameraprofile.DeviceProfile{
		Channels: []cameraprofile.ChannelProfile{{
			Index: 0,
			Candidates: []cameraprofile.StreamCandidate{
				{RoleHint: cameraprofile.StreamRoleRecording, Label: "main", URL: "rtsp://camera/main", ProfileToken: "main"},
				{RoleHint: cameraprofile.StreamRoleLive, Label: "sub", URL: "rtsp://camera/sub", ProfileToken: "sub"},
			},
		}},
	}

	selected := selectProfileCandidates(profile, 0, []cameraStreamSelection{
		{Role: cameraprofile.StreamRoleRecording, ProfileToken: "main"},
		{Role: cameraprofile.StreamRoleLive, ProfileToken: "sub"},
	})

	if len(selected) != 2 {
		t.Fatalf("selected candidates = %d, want 2", len(selected))
	}
	if selected[0].RoleHint != cameraprofile.StreamRoleRecording || selected[0].URL != "rtsp://camera/main" {
		t.Fatalf("recording selection = %#v", selected[0])
	}
	if selected[1].RoleHint != cameraprofile.StreamRoleLive || selected[1].URL != "rtsp://camera/sub" {
		t.Fatalf("live selection = %#v", selected[1])
	}
}
