package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

func TestCameraProfileScan_returnsScanAndMatches_whenTemplateMatchesDeviceScan(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	template := createRouteProfileTemplate(t, server.db, "Matched Scan", "V400D")
	withRouteScanner(t, func() (cameraprofile.DeviceScanResult, error) {
		return routeDeviceScanResult(routeSyntheticRTSPURL("scan-main"), routeSyntheticRTSPURL("scan-live")), nil
	})

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/scan", `{"host":"192.168.1.10","rtspPort":554,"httpPort":80,"onvifPort":80}`, trustedConsoleHeaders())
	t.Logf("httptest POST /api/cameras/scan existing camera host -> status=%d body=%s", status, mustMarshalString(t, payload))

	// Then
	if status != http.StatusOK {
		t.Fatalf("POST /api/cameras/scan status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	if _, ok := payload["profile"]; ok {
		t.Fatalf("scan response included deprecated profile field: %#v", payload)
	}
	if _, ok := payload["scan"].(map[string]any); !ok {
		t.Fatalf("scan response missing scan object: %#v", payload)
	}
	matches := requirePayloadArray(t, payload, "matches")
	if len(matches) != 1 {
		t.Fatalf("matches length = %d, want 1; payload=%#v", len(matches), payload)
	}
	match, ok := matches[0].(map[string]any)
	if !ok {
		t.Fatalf("match = %#v, want object", matches[0])
	}
	if int64(match["templateId"].(float64)) != template.ID {
		t.Fatalf("match templateId = %v, want %d", match["templateId"], template.ID)
	}
}

func TestCameraProfileCreate_persistsProfileTemplateID_whenMatchedTemplateSelected(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	template := createRouteProfileTemplate(t, server.db, "Matched Save", "V400D")
	body := cameraTemplateSelectionBody(t, "Template Save", "template-save", template.ID, routeSyntheticRTSPURL("template-save"))

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", body, trustedConsoleHeaders())

	// Then
	if status != http.StatusOK {
		t.Fatalf("POST /api/cameras status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	cameraPayload := requirePayloadObject(t, payload, "camera")
	if int64(cameraPayload["profileTemplateId"].(float64)) != template.ID {
		t.Fatalf("camera profileTemplateId = %v, want %d", cameraPayload["profileTemplateId"], template.ID)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "template-save")
	if err != nil {
		t.Fatalf("read stored camera: %v", err)
	}
	if stored.ProfileTemplateID == nil || *stored.ProfileTemplateID != template.ID {
		t.Fatalf("stored profileTemplateId = %v, want %d", stored.ProfileTemplateID, template.ID)
	}
	if stored.RecordingStreamName != "template-save-recording" || stored.LiveStreamName != "template-save-live" {
		t.Fatalf("role stream names = %q/%q, want template-save-recording/template-save-live", stored.RecordingStreamName, stored.LiveStreamName)
	}
}

func TestCameraProfileCreate_preservesSelectedTemplateChannel_whenDualLensTemplateSelected(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	template, err := server.db.CreateCameraProfileTemplate(t.Context(), routeDualLensProfileTemplate("V400D Dual Lens", "V400D"))
	if err != nil {
		t.Fatalf("create dual-lens profile template: %v", err)
	}
	body := cameraDualTemplateSelectionBody(t, "Template Save Lens 2", "template-save-lens-2", template.ID)

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", body, trustedConsoleHeaders())

	// Then
	if status != http.StatusOK {
		t.Fatalf("POST /api/cameras status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "template-save-lens-2")
	if err != nil {
		t.Fatalf("read stored camera: %v", err)
	}
	if stored.ChannelIndex == nil || *stored.ChannelIndex != 1 {
		t.Fatalf("stored channelIndex = %v, want 1", stored.ChannelIndex)
	}
	if len(stored.Streams) != 2 {
		t.Fatalf("stored streams = %d, want 2: %#v", len(stored.Streams), stored.Streams)
	}
	if stored.Streams[0].ProfileToken != "PROFILE_100" || stored.Streams[1].ProfileToken != "PROFILE_101" {
		t.Fatalf("stored profile tokens = %q/%q, want PROFILE_100/PROFILE_101", stored.Streams[0].ProfileToken, stored.Streams[1].ProfileToken)
	}
	if !strings.Contains(stored.Streams[0].RedactedURL, "/tcp/av1_0") || !strings.Contains(stored.Streams[1].RedactedURL, "/tcp/av1_1") {
		t.Fatalf("stored lens-2 URLs = %q/%q, want av1 paths", stored.Streams[0].RedactedURL, stored.Streams[1].RedactedURL)
	}
}

func TestCameraProfileCreate_leavesProfileTemplateIDNil_whenManualStreamsSelected(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	body := cameraManualSelectionBody(t, "Manual Save", "manual-save", routeSyntheticRTSPURL("manual-save"))

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", body, trustedConsoleHeaders())

	// Then
	if status != http.StatusOK {
		t.Fatalf("POST /api/cameras status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	cameraPayload := requirePayloadObject(t, payload, "camera")
	if _, ok := cameraPayload["profileTemplateId"]; ok {
		t.Fatalf("manual camera response included profileTemplateId: %#v", cameraPayload)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "manual-save")
	if err != nil {
		t.Fatalf("read stored camera: %v", err)
	}
	if stored.ProfileTemplateID != nil {
		t.Fatalf("stored profileTemplateId = %v, want nil", stored.ProfileTemplateID)
	}
}

func TestCameraProfileCreate_canSaveReusableProfileThenCamera_whenManualSelectionIncludesNewTemplate(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	body := cameraManualWithNewTemplateBody(t, "Manual Template Save", "manual-template-save", routeSyntheticRTSPURL("manual-template-save"))

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", body, trustedConsoleHeaders())

	// Then
	if status != http.StatusOK {
		t.Fatalf("POST /api/cameras status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	cameraPayload := requirePayloadObject(t, payload, "camera")
	if _, ok := cameraPayload["profileTemplateId"].(float64); !ok {
		t.Fatalf("camera response missing created profileTemplateId: %#v", cameraPayload)
	}
	templates, err := server.db.ListCameraProfileTemplates(t.Context())
	if err != nil {
		t.Fatalf("list profile templates: %v", err)
	}
	if len(templates) != 1 || templates[0].ProfileName != "Manual Template Save reusable" {
		t.Fatalf("templates = %#v, want created reusable profile", templates)
	}
}

func TestCameraProfileUpdate_preservesStableStreamName_whenProfileTemplateSelectionChanges(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	template := createRouteProfileTemplate(t, server.db, "Edit Save", "V400D")
	createBody := cameraManualSelectionBody(t, "Edit Save", "edit-stable", routeSyntheticRTSPURL("edit-initial"))
	createStatus, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", createBody, trustedConsoleHeaders())
	if createStatus != http.StatusOK {
		t.Fatalf("seed POST /api/cameras status = %d, want %d", createStatus, http.StatusOK)
	}
	updateBody := cameraTemplateSelectionBody(t, "Edit Save Updated", "ignored-new-stream", template.ID, routeSyntheticRTSPURL("edit-updated"))

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPut, "/api/cameras/edit-stable", updateBody, trustedConsoleHeaders())

	// Then
	if status != http.StatusOK {
		t.Fatalf("PUT /api/cameras/edit-stable status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	cameraPayload := requirePayloadObject(t, payload, "camera")
	if cameraPayload["streamName"] != "edit-stable" {
		t.Fatalf("streamName = %v, want edit-stable", cameraPayload["streamName"])
	}
	if int64(cameraPayload["profileTemplateId"].(float64)) != template.ID {
		t.Fatalf("profileTemplateId = %v, want %d", cameraPayload["profileTemplateId"], template.ID)
	}
}

func TestCameraCRUD_deletePreservesRecordings_whenCameraRowIsDeleted(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	createStatus, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", cameraManualSelectionBody(t, "Delete Recording", "delete-recording", routeSyntheticRTSPURL("delete-recording")), trustedConsoleHeaders())
	if createStatus != http.StatusOK {
		t.Fatalf("seed POST /api/cameras status = %d, want %d", createStatus, http.StatusOK)
	}
	stored, err := server.db.GetCameraByStream(t.Context(), "delete-recording")
	if err != nil {
		t.Fatalf("read seeded camera: %v", err)
	}
	segment, err := server.db.OpenRecordingSegment(t.Context(), store.RecordingSegment{
		CameraID:   stored.ID,
		StreamName: stored.StreamName,
		Filename:   "delete-recording.mp4",
		TSStart:    100,
		Status:     "recording",
	})
	if err != nil {
		t.Fatalf("open recording segment: %v", err)
	}

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodDelete, "/api/cameras/delete-recording", "", trustedConsoleHeaders())

	// Then
	if status != http.StatusOK {
		t.Fatalf("DELETE /api/cameras/delete-recording status = %d, want %d; body=%#v", status, http.StatusOK, payload)
	}
	segments, err := server.db.ListRecordingSegments(t.Context(), "delete-recording", time.Unix(0, 0), time.Unix(200, 0), "recording")
	if err != nil {
		t.Fatalf("list recording segments: %v", err)
	}
	if len(segments) != 1 || segments[0].ID != segment.ID {
		t.Fatalf("recording segments after camera delete = %#v, want preserved segment %d", segments, segment.ID)
	}
}

func TestCameraCRUD_publicDTOStaysRedacted_whenProfileTemplateSelectionSavesStreams(t *testing.T) {
	t.Parallel()

	// Given
	rawURL := routeSyntheticRTSPURL("redacted-save")
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	template := createRouteProfileTemplate(t, server.db, "Redacted Save", "V400D")
	createStatus, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", cameraTemplateSelectionBody(t, "Redacted Save", "redacted-save", template.ID, rawURL), trustedConsoleHeaders())
	if createStatus != http.StatusOK {
		t.Fatalf("seed POST /api/cameras status = %d, want %d", createStatus, http.StatusOK)
	}

	// When
	status, cameras := requestJSONArrayWithHeaders(t, server.handler, http.MethodGet, "/api/cameras", "", nil)
	t.Logf("httptest GET /api/cameras redacted output -> status=%d body=%s", status, mustMarshalString(t, map[string]any{"cameras": cameras}))

	// Then
	if status != http.StatusOK {
		t.Fatalf("GET /api/cameras status = %d, want %d", status, http.StatusOK)
	}
	encoded, err := json.Marshal(cameras)
	if err != nil {
		t.Fatalf("marshal cameras response: %v", err)
	}
	for _, forbidden := range []string{"url\":\"rtsp://"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("/api/cameras leaked %q in %s", forbidden, encoded)
		}
	}
}
