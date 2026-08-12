package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"camstation/internal/store"
)

func TestCameraProfiles_listReturnsJSON_whenNoTemplatesExist(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)

	// When
	rec := requestCameraProfileTemplate(t, server.handler, http.MethodGet, "/api/camera-profiles", "", nil)
	t.Logf("httptest GET /api/camera-profiles -> status=%d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())

	// Then
	requireJSONResponse(t, rec, http.StatusOK)
	var payload []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode list response: %v; body=%s", err, rec.Body.String())
	}
	if len(payload) != 0 {
		t.Fatalf("profile template list length = %d, want 0", len(payload))
	}
}

func TestCameraProfiles_CRUD_whenTemplatePayloadIsCredentialFree(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	headers := trustedConsoleHeaders()

	// When: create
	createRec := requestCameraProfileTemplate(t, server.handler, http.MethodPost, "/api/camera-profiles", cameraProfileTemplateBody("Dual Lens", "V400D"), headers)

	// Then
	requireJSONResponse(t, createRec, http.StatusCreated)
	created := decodeProfileTemplateObject(t, createRec)
	id := int64(created["id"].(float64))
	if id <= 0 {
		t.Fatalf("created id = %d, want positive id", id)
	}
	assertProfileTemplatePayload(t, created, "Dual Lens", "V400D")
	assertBodyDoesNotContainCredentialMarkers(t, createRec.Body.String())

	// When: get
	getRec := requestCameraProfileTemplate(t, server.handler, http.MethodGet, "/api/camera-profiles/"+strconv.FormatInt(id, 10), "", nil)

	// Then
	requireJSONResponse(t, getRec, http.StatusOK)
	assertProfileTemplatePayload(t, decodeProfileTemplateObject(t, getRec), "Dual Lens", "V400D")

	// When: update
	updateRec := requestCameraProfileTemplate(t, server.handler, http.MethodPut, "/api/camera-profiles/"+strconv.FormatInt(id, 10), cameraProfileTemplateBody("Dual Lens Updated", "V400D Pro"), headers)

	// Then
	requireJSONResponse(t, updateRec, http.StatusOK)
	assertProfileTemplatePayload(t, decodeProfileTemplateObject(t, updateRec), "Dual Lens Updated", "V400D Pro")

	// When: list
	listRec := requestCameraProfileTemplate(t, server.handler, http.MethodGet, "/api/camera-profiles", "", nil)

	// Then
	requireJSONResponse(t, listRec, http.StatusOK)
	var listed []map[string]any
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v; body=%s", err, listRec.Body.String())
	}
	if len(listed) != 1 {
		t.Fatalf("profile template list length = %d, want 1", len(listed))
	}
	assertProfileTemplatePayload(t, listed[0], "Dual Lens Updated", "V400D Pro")

	// When: delete
	deleteRec := requestCameraProfileTemplate(t, server.handler, http.MethodDelete, "/api/camera-profiles/"+strconv.FormatInt(id, 10), "", headers)

	// Then
	requireJSONResponse(t, deleteRec, http.StatusOK)
	var deletePayload map[string]any
	if err := json.Unmarshal(deleteRec.Body.Bytes(), &deletePayload); err != nil {
		t.Fatalf("decode delete response: %v; body=%s", err, deleteRec.Body.String())
	}
	if deletePayload["ok"] != true || int64(deletePayload["id"].(float64)) != id {
		t.Fatalf("delete payload = %#v, want ok true and id %d", deletePayload, id)
	}

	// When: get deleted
	missingRec := requestCameraProfileTemplate(t, server.handler, http.MethodGet, "/api/camera-profiles/"+strconv.FormatInt(id, 10), "", nil)

	// Then
	requireJSONResponse(t, missingRec, http.StatusNotFound)
}

func TestCameraProfiles_rejectsInvalidPayloads_whenCreateBodyIsMissingRequiredFieldsOrContainsCredentials(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	headers := trustedConsoleHeaders()
	tests := []struct {
		name string
		body string
	}{
		{"missing profile name", `{"manufacturer":"VStarcam","model":"V400D","adapter":"vstarcam","version":1}`},
		{"credentialed stream path", strings.Replace(cameraProfileTemplateBody("Credential Leak", "V400D"), `"/tcp/av0_0"`, `"rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0"`, 1)},
		{"query credential stream path", strings.Replace(cameraProfileTemplateBody("Query Credential Leak", "V400D"), `"/tcp/av0_0"`, `"/tcp/av0_0?user=admin&password=query-secret&token=query-token"`, 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			rec := requestCameraProfileTemplate(t, server.handler, http.MethodPost, "/api/camera-profiles", tt.body, headers)

			// Then
			requireJSONResponse(t, rec, http.StatusBadRequest)
			assertBodyDoesNotContainCredentialMarkers(t, rec.Body.String())
		})
	}
}

func TestCameraProfiles_returnsJSONNotFound_whenTemplateIDIsMissingOrMalformed(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	headers := trustedConsoleHeaders()
	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"get missing", http.MethodGet, "/api/camera-profiles/999", ""},
		{"put missing", http.MethodPut, "/api/camera-profiles/999", cameraProfileTemplateBody("Missing", "V400D")},
		{"delete missing", http.MethodDelete, "/api/camera-profiles/999", ""},
		{"malformed id", http.MethodGet, "/api/camera-profiles/not-a-number", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			rec := requestCameraProfileTemplate(t, server.handler, tt.method, tt.target, tt.body, headers)

			// Then
			requireJSONResponse(t, rec, http.StatusNotFound)
		})
	}
}

func TestCameraProfiles_blocksDelete_whenTemplateIsReferencedByCamera(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	template, err := server.db.CreateCameraProfileTemplate(t.Context(), routeTestProfileTemplate("Referenced", "V400D"))
	if err != nil {
		t.Fatalf("create profile template fixture: %v", err)
	}
	if _, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name:              "Referenced",
		URL:               "rtsp://admin:secret@192.168.0.55:10554/tcp/av0_0",
		StreamName:        "referenced-template",
		State:             "streaming",
		ProfileTemplateID: &template.ID,
	}); err != nil {
		t.Fatalf("create referenced camera fixture: %v", err)
	}

	// When
	rec := requestCameraProfileTemplate(t, server.handler, http.MethodDelete, "/api/camera-profiles/"+strconv.FormatInt(template.ID, 10), "", trustedConsoleHeaders())

	// Then
	requireJSONResponse(t, rec, http.StatusConflict)
}

func TestCameraProfiles_rejectsMissingGuard_whenMutationLacksTrustedConsoleHeaders(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	template, err := server.db.CreateCameraProfileTemplate(t.Context(), routeTestProfileTemplate("Guarded", "V400D"))
	if err != nil {
		t.Fatalf("create profile template fixture: %v", err)
	}
	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"create", http.MethodPost, "/api/camera-profiles", cameraProfileTemplateBody("Guarded Create", "V400D")},
		{"update", http.MethodPut, "/api/camera-profiles/" + strconv.FormatInt(template.ID, 10), cameraProfileTemplateBody("Guarded Update", "V400D")},
		{"delete", http.MethodDelete, "/api/camera-profiles/" + strconv.FormatInt(template.ID, 10), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			rec := requestCameraProfileTemplate(t, server.handler, tt.method, tt.target, tt.body, nil)

			// Then
			requireJSONResponse(t, rec, http.StatusForbidden)
		})
	}
}
