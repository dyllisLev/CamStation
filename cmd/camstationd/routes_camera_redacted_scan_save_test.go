package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"camstation/internal/cameraprofile"
)

func TestCameraProfileCreate_savesTemplateSelectionFromRedactedScanResponse(t *testing.T) {
	t.Parallel()

	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	template := createRouteProfileTemplate(t, server.db, "Matched Redacted Scan Save", "V400D")
	withRouteScanner(t, func() (cameraprofile.DeviceScanResult, error) {
		return routeDeviceScanResult(routeSyntheticRTSPURL("scan-to-save-main"), routeSyntheticRTSPURL("scan-to-save-live")), nil
	})
	scanStatus, scanPayload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/scan", `{"host":"192.168.1.10","rtspPort":554,"httpPort":80,"onvifPort":80}`, trustedConsoleHeaders())
	if scanStatus != http.StatusOK {
		t.Fatalf("scan status = %d, want %d; body=%#v", scanStatus, http.StatusOK, scanPayload)
	}
	scanObject := requirePayloadObject(t, scanPayload, "scan")
	encodedScan := mustMarshalString(t, scanObject)
	if countJSONKey(scanObject, "url") != 0 || countJSONKey(scanObject, "redactedUrl") == 0 {
		t.Fatalf("scan response should expose only redacted candidate URLs before save flow: %s", encodedScan)
	}
	body := map[string]any{
		"name":              "Redacted Scan Save",
		"streamName":        "redacted-scan-save",
		"host":              "192.168.1.10",
		"rtspPort":          554,
		"httpPort":          80,
		"onvifPort":         80,
		"profileTemplateId": template.ID,
		"profile":           scanObject,
		"channelIndex":      0,
		"streamSelections": []map[string]any{
			{"role": "recording", "profileToken": "PROFILE_000"},
			{"role": "live", "profileToken": "PROFILE_001"},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal redacted scan save body: %v", err)
	}

	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", string(bodyBytes), trustedConsoleHeaders())

	if status != http.StatusOK {
		t.Fatalf("POST /api/cameras status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "redacted-scan-save")
	if err != nil {
		t.Fatalf("read stored camera: %v", err)
	}
	if len(stored.Streams) != 2 {
		t.Fatalf("stored streams = %d, want 2: %#v", len(stored.Streams), stored.Streams)
	}
	if stored.Streams[0].URL == "" || stored.Streams[1].URL == "" {
		t.Fatalf("template selection from redacted scan did not reconstruct stream URLs: %#v", stored.Streams)
	}
	if stored.Streams[0].ProfileToken != "PROFILE_000" || stored.Streams[1].ProfileToken != "PROFILE_001" {
		t.Fatalf("stored profile tokens = %q/%q, want PROFILE_000/PROFILE_001", stored.Streams[0].ProfileToken, stored.Streams[1].ProfileToken)
	}
	if !strings.Contains(stored.Streams[0].URL, "/tcp/av0_0") || !strings.Contains(stored.Streams[1].URL, "/tcp/av0_1") {
		t.Fatalf("stored URLs = %q/%q, want template paths", stored.Streams[0].URL, stored.Streams[1].URL)
	}
}
