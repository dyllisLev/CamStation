package main

import (
	"net/http"
	"strconv"
	"testing"
)

func TestIncidentsAPI_RejectsInvalidCreateAndPatchWithoutPersisting(t *testing.T) {
	t.Parallel()

	// Given
	server := newIncidentRouteServer(t)

	// When
	createSeverityStatus, createSeverityBody := requestJSON(t, server.handler, http.MethodPost, "/api/incidents", `{"title":"bad severity","severity":"urgent","source":"manual"}`)
	createStatusStatus, createStatusBody := requestJSON(t, server.handler, http.MethodPost, "/api/incidents", `{"title":"bad status","severity":"high","status":"resolved","source":"manual"}`)
	listStatus, listBody := requestJSON(t, server.handler, http.MethodGet, "/api/incidents", "")
	createValidStatus, createValidBody := requestJSON(t, server.handler, http.MethodPost, "/api/incidents", `{"title":"valid incident","severity":"high","source":"manual"}`)
	id := int64(createValidBody["id"].(float64))
	patchSeverityStatus, patchSeverityBody := requestJSON(t, server.handler, http.MethodPatch, "/api/incidents/"+strconv.FormatInt(id, 10), `{"severity":"urgent"}`)
	patchResolvedStatus, patchResolvedBody := requestJSON(t, server.handler, http.MethodPatch, "/api/incidents/"+strconv.FormatInt(id, 10), `{"status":"resolved"}`)
	detailStatus, detailBody := requestJSON(t, server.handler, http.MethodGet, "/api/incidents/"+strconv.FormatInt(id, 10), "")

	// Then
	if createSeverityStatus != http.StatusBadRequest {
		t.Fatalf("invalid create severity status = %d, want %d; body=%#v", createSeverityStatus, http.StatusBadRequest, createSeverityBody)
	}
	if createStatusStatus != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d, want %d; body=%#v", createStatusStatus, http.StatusBadRequest, createStatusBody)
	}
	if listStatus != http.StatusOK || len(listBody["incidents"].([]any)) != 0 {
		t.Fatalf("incidents after invalid creates status/body = %d/%#v", listStatus, listBody)
	}
	if createValidStatus != http.StatusCreated {
		t.Fatalf("valid create status = %d, want %d; body=%#v", createValidStatus, http.StatusCreated, createValidBody)
	}
	if patchSeverityStatus != http.StatusBadRequest {
		t.Fatalf("invalid patch severity status = %d, want %d; body=%#v", patchSeverityStatus, http.StatusBadRequest, patchSeverityBody)
	}
	if patchResolvedStatus != http.StatusBadRequest {
		t.Fatalf("invalid patch resolved status = %d, want %d; body=%#v", patchResolvedStatus, http.StatusBadRequest, patchResolvedBody)
	}
	if detailStatus != http.StatusOK || detailBody["severity"] != "high" || detailBody["status"] != "open" {
		t.Fatalf("detail after invalid patch status/body = %d/%#v", detailStatus, detailBody)
	}
	writeAPIEvidence(t, "incident-validation-rejections.json", map[string]any{
		"invalidCreateSeverity": map[string]any{"status": createSeverityStatus, "body": createSeverityBody},
		"invalidCreateStatus":   map[string]any{"status": createStatusStatus, "body": createStatusBody},
		"invalidPatchSeverity":  map[string]any{"status": patchSeverityStatus, "body": patchSeverityBody},
		"invalidPatchResolved":  map[string]any{"status": patchResolvedStatus, "body": patchResolvedBody},
		"detailAfterRejected":   map[string]any{"status": detailStatus, "body": detailBody},
	})
}
