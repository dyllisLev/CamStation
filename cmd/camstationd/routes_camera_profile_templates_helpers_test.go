package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"camstation/internal/store"
)

func requestCameraProfileTemplate(t *testing.T, handler http.Handler, method string, target string, body string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func requireJSONResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()

	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; content-type=%q body=%s", rec.Code, wantStatus, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json; body=%s", contentType, rec.Body.String())
	}
}

func decodeProfileTemplateObject(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode profile template response: %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func assertProfileTemplatePayload(t *testing.T, payload map[string]any, wantName string, wantModel string) {
	t.Helper()

	if payload["profileName"] != wantName {
		t.Fatalf("profileName = %v, want %q in %#v", payload["profileName"], wantName, payload)
	}
	if payload["model"] != wantModel {
		t.Fatalf("model = %v, want %q in %#v", payload["model"], wantModel, payload)
	}
	if payload["manufacturer"] != "VStarcam" || payload["adapter"] != "vstarcam" {
		t.Fatalf("profile identity = %#v, want VStarcam/vstarcam", payload)
	}
}

func assertBodyDoesNotContainCredentialMarkers(t *testing.T, body string) {
	t.Helper()

	for _, forbidden := range []string{"admin", "secret", "password", "username", "rtsp://", "@"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked credential marker %q in %s", forbidden, body)
		}
	}
}

func cameraProfileTemplateBody(profileName string, model string) string {
	encoded, err := json.Marshal(routeTestProfileTemplate(profileName, model))
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func routeTestProfileTemplate(profileName string, model string) store.CameraProfileTemplate {
	return routeTestProfileTemplateWithChannels(profileName, model, []store.CameraProfileTemplateChannel{
		routeTemplateChannel(0, "lens-1", "/tcp/av0_0", "/tcp/av0_1", "PROFILE_000", "PROFILE_001"),
	})
}

func routeDualLensProfileTemplate(profileName string, model string) store.CameraProfileTemplate {
	return routeTestProfileTemplateWithChannels(profileName, model, []store.CameraProfileTemplateChannel{
		routeTemplateChannel(0, "lens-1", "/tcp/av0_0", "/tcp/av0_1", "PROFILE_000", "PROFILE_001"),
		routeTemplateChannel(1, "lens-2", "/tcp/av1_0", "/tcp/av1_1", "PROFILE_100", "PROFILE_101"),
	})
}

func routeTestProfileTemplateWithChannels(profileName string, model string, channels []store.CameraProfileTemplateChannel) store.CameraProfileTemplate {
	return store.CameraProfileTemplate{
		ProfileName:  profileName,
		Manufacturer: "VStarcam",
		Model:        model,
		Adapter:      "vstarcam",
		Version:      1,
		MatchRules: []store.CameraProfileMatchRule{
			{Field: "manufacturer", Operator: "equals", Value: "VStarcam"},
			{Field: "model", Operator: "contains", Value: model},
		},
		Channels: channels,
		Capabilities: store.CameraProfileCapabilities{
			ONVIF:        true,
			RTSP:         true,
			MultiChannel: len(channels) > 1,
		},
	}
}

func routeTemplateChannel(index int, name string, recordingPath string, livePath string, recordingToken string, liveToken string) store.CameraProfileTemplateChannel {
	return store.CameraProfileTemplateChannel{
		Index: index,
		Name:  name,
		Streams: []store.CameraProfileTemplateStream{
			{
				Role:         store.CameraStreamRoleRecording,
				Label:        "main",
				Source:       "onvif",
				Path:         recordingPath,
				ProfileToken: recordingToken,
				Codec:        "h264",
				Width:        2304,
				Height:       1296,
				FPS:          12,
				BitrateKbps:  1024,
			},
			{
				Role:         store.CameraStreamRoleLive,
				Label:        "sub",
				Source:       "onvif",
				Path:         livePath,
				ProfileToken: liveToken,
				Codec:        "h264",
				Width:        448,
				Height:       256,
				FPS:          12,
				BitrateKbps:  512,
			},
		},
	}
}
