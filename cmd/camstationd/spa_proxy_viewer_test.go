package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"camstation/internal/store"
)

func TestGo2RTCProxyAllowsOnlyRegisteredPublicStreamNames(t *testing.T) {
	cameras := []store.Camera{{
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
	proxy, err := go2RTCProxy(newPreviewRegistry(), func(context.Context, string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/ws?src=yard-private-input", nil)
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
