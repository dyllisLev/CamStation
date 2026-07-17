package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestGo2RTCProxyAllowsOnlyRegisteredPublicStreamNames(t *testing.T) {
	cameras := []store.Camera{{
		Enabled:             true,
		StreamName:          "yard",
		RecordingStreamName: "yard-recording",
		LiveStreamName:      "yard-live",
		FocusStreamName:     "yard-focus",
		Streams: []store.CameraStream{{
			Go2RTCStreamName: "yard-private-input",
		}},
		Outputs: []store.CameraOutput{
			{Purpose: store.CameraOutputRecording, StreamName: "yard-recording"},
			{Purpose: store.CameraOutputLive, StreamName: "yard-live"},
			{Purpose: store.CameraOutputFocus, StreamName: "yard-focus"},
		},
	}}

	for _, streamName := range []string{"yard", "yard-recording", "yard-live", "yard-focus"} {
		if !isRegisteredPublicStream(cameras, streamName) {
			t.Fatalf("public stream %q was rejected", streamName)
		}
	}
	for _, streamName := range []string{"", "yard-private-input", "rtsp://admin:secret@camera/live", "unknown"} {
		if isRegisteredPublicStream(cameras, streamName) {
			t.Fatalf("non-public stream %q was accepted", streamName)
		}
	}
}

func TestGo2RTCProxyRejectsUnregisteredWebSocketSourceBeforeProxying(t *testing.T) {
	registrationChecked := false
	proxy, err := go2RTCProxy(newPreviewRegistry(), func(_ context.Context, streamName string) bool {
		registrationChecked = true
		return streamName == "yard-live"
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ws?src=yard-private-input", nil)
	req.Host = "camstation.local:18080"
	req.Header.Set("Origin", "http://camstation.local:18080")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if !registrationChecked {
		t.Fatal("registration callback was not reached")
	}
}

func TestGo2RTCProxyRejectsRegisteredPreviewAfterCameraIsDisabled(t *testing.T) {
	previews := newPreviewRegistry()
	previewName, _ := previews.PutForCamera("rtsp://admin:secret@192.0.2.10/main", "yard", 10*time.Minute)
	proxy, err := go2RTCProxy(previews, func(_ context.Context, streamName string) bool {
		return streamName != "yard"
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ws?src="+previewName, nil)
	req.Host = "camstation.local:18080"
	req.Header.Set("Origin", "http://camstation.local:18080")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestGo2RTCProxyOriginMustMatchConfiguredConsoleHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://camstation.local:18080/api/ws?src=yard-live", nil)
	req.Header.Set("Origin", "http://camstation.local:18080")
	if !isPlayerOriginAllowed(req) {
		t.Fatal("matching console origin was rejected")
	}
	req.Header.Set("Origin", "http://other.local:18080")
	if isPlayerOriginAllowed(req) {
		t.Fatal("cross-origin player request was accepted")
	}
}
