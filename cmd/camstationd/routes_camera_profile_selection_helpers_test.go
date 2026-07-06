package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

func createRouteProfileTemplate(t *testing.T, db *store.DB, profileName string, model string) store.CameraProfileTemplate {
	t.Helper()

	template, err := db.CreateCameraProfileTemplate(t.Context(), routeTestProfileTemplate(profileName, model))
	if err != nil {
		t.Fatalf("create profile template fixture: %v", err)
	}
	return template
}

func withRouteScanner(t *testing.T, scan func() (cameraprofile.DeviceScanResult, error)) {
	t.Helper()

	previous := newRouteDeviceScanner
	newRouteDeviceScanner = func() routeDeviceScanner {
		return routeDeviceScannerFunc(func(context.Context, cameraprofile.ScanRequest) (cameraprofile.DeviceScanResult, error) {
			return scan()
		})
	}
	t.Cleanup(func() {
		newRouteDeviceScanner = previous
	})
}

func routeDeviceScanResult(recordingURL string, liveURL string) cameraprofile.DeviceScanResult {
	return cameraprofile.DeviceScanResult{
		Host:         "192.168.1.10",
		Manufacturer: "VStarcam",
		Model:        "V400D",
		Adapter:      "vstarcam",
		RTSPPort:     554,
		HTTPPort:     80,
		ONVIFPort:    80,
		Capabilities: cameraprofile.Capabilities{PTZ: true},
		Channels: []cameraprofile.ScanChannel{{
			Index: 0,
			Label: "lens-1",
			Candidates: []cameraprofile.StreamCandidate{
				routeStreamCandidate(cameraprofile.StreamRoleRecording, "main", recordingURL, "PROFILE_000"),
				routeStreamCandidate(cameraprofile.StreamRoleLive, "sub", liveURL, "PROFILE_001"),
			},
		}},
		LastScan: map[string]any{
			"adapter": "vstarcam",
		},
	}
}

func routeStreamCandidate(role cameraprofile.StreamRole, label string, rawURL string, token string) cameraprofile.StreamCandidate {
	return cameraprofile.StreamCandidate{
		RoleHint:     role,
		Label:        label,
		Source:       "onvif",
		URL:          rawURL,
		RedactedURL:  store.RedactURL(rawURL),
		Codec:        "h264",
		Width:        2304,
		Height:       1296,
		FPS:          12,
		BitrateKbps:  1024,
		ProfileToken: token,
	}
}

func cameraTemplateSelectionBody(t *testing.T, name string, streamName string, templateID int64, rawURL string) string {
	t.Helper()

	body := map[string]any{
		"name":              name,
		"streamName":        streamName,
		"profileTemplateId": templateID,
		"host":              "192.168.1.10",
		"rtspPort":          554,
		"httpPort":          80,
		"onvifPort":         80,
		"streamSelections": []map[string]any{
			{"role": "recording", "profileToken": "PROFILE_000"},
			{"role": "live", "profileToken": "PROFILE_001"},
		},
		"streams": []cameraprofile.StreamCandidate{
			routeStreamCandidate(cameraprofile.StreamRoleRecording, "main", rawURL, "PROFILE_000"),
			routeStreamCandidate(cameraprofile.StreamRoleLive, "sub", strings.Replace(rawURL, "av0_0", "av0_1", 1), "PROFILE_001"),
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal template selection body: %v", err)
	}
	return string(encoded)
}

func cameraManualSelectionBody(t *testing.T, name string, streamName string, rawURL string) string {
	t.Helper()

	body := map[string]any{
		"name":       name,
		"streamName": streamName,
		"host":       "192.168.1.10",
		"rtspPort":   554,
		"httpPort":   80,
		"onvifPort":  80,
		"streams": []cameraprofile.StreamCandidate{
			routeStreamCandidate(cameraprofile.StreamRoleRecording, "main", rawURL, "PROFILE_000"),
			routeStreamCandidate(cameraprofile.StreamRoleLive, "sub", strings.Replace(rawURL, "av0_0", "av0_1", 1), "PROFILE_001"),
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal manual selection body: %v", err)
	}
	return string(encoded)
}

func cameraManualWithNewTemplateBody(t *testing.T, name string, streamName string, rawURL string) string {
	t.Helper()

	body := map[string]any{}
	if err := json.Unmarshal([]byte(cameraManualSelectionBody(t, name, streamName, rawURL)), &body); err != nil {
		t.Fatalf("decode manual body: %v", err)
	}
	template := routeTestProfileTemplate(name+" reusable", "V400D")
	body["saveProfileTemplate"] = template
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal manual template body: %v", err)
	}
	return string(encoded)
}

func requirePayloadObject(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want object in %#v", key, payload[key], payload)
	}
	return value
}

func requirePayloadArray(t *testing.T, payload map[string]any, key string) []any {
	t.Helper()

	value, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want array in %#v", key, payload[key], payload)
	}
	return value
}

func requestJSONArrayWithHeaders(t *testing.T, handler http.Handler, method string, target string, body string, headers http.Header) (int, []map[string]any) {
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

	var payload []map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s %s response: %v; body=%s", method, target, err, rec.Body.String())
		}
	}
	return rec.Code, payload
}

type routeDeviceScannerFunc func(ctx context.Context, req cameraprofile.ScanRequest) (cameraprofile.DeviceScanResult, error)

func (f routeDeviceScannerFunc) ScanResult(ctx context.Context, req cameraprofile.ScanRequest) (cameraprofile.DeviceScanResult, error) {
	return f(ctx, req)
}
