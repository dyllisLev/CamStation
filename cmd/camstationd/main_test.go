package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

func TestRoutesServeConsoleAtRoot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	tempDir := t.TempDir()
	db, err := store.Open(filepath.Join(tempDir, "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	handler, err := routes(
		db,
		nil,
		stream.NewGo2RTC(filepath.Join(tempDir, "go2rtc.yaml")),
		recorder.New(db, filepath.Join(tempDir, "recordings"), filepath.Join(tempDir, "temp"), 5),
		cleanup.New(db, filepath.Join(tempDir, "recordings")),
		filepath.Join(tempDir, "recordings"),
		filepath.Join(tempDir, "temp"),
		0,
		false,
	)
	if err != nil {
		t.Fatalf("build routes: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if location := rec.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want no redirect", location)
	}
}

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
