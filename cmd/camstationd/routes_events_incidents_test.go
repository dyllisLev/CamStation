package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"camstation/internal/cleanup"
	"camstation/internal/recorder"
	"camstation/internal/store"
	"camstation/internal/stream"
)

type incidentRouteServer struct {
	db      *store.DB
	handler http.Handler
}

func newIncidentRouteServer(t *testing.T) incidentRouteServer {
	t.Helper()

	tempDir := t.TempDir()
	db, err := store.Open(filepath.Join(tempDir, "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	recordingsDir := filepath.Join(tempDir, "recordings")
	tempRecordingDir := filepath.Join(tempDir, "temp")
	handler, err := routes(
		db,
		nil,
		stream.NewGo2RTC(filepath.Join(tempDir, "go2rtc.yaml")),
		recorder.New(db, recordingsDir, tempRecordingDir, 5),
		cleanup.New(db, recordingsDir),
		recordingsDir,
		tempRecordingDir,
		0,
		false,
	)
	if err != nil {
		t.Fatalf("build routes: %v", err)
	}
	return incidentRouteServer{db: db, handler: handler}
}

func TestEventsAPI_FiltersExportsPrunesAndRejectsUnsafePrune(t *testing.T) {
	t.Parallel()

	// Given
	server := newIncidentRouteServer(t)
	secret := syntheticRouteWebhookURL("2", "b")
	base := time.Date(2026, 7, 2, 3, 0, 0, 0, time.UTC)
	for _, event := range []store.Event{
		{CreatedAt: base, Source: "recorder", Level: "info", Message: "worker ready"},
		{CreatedAt: base.Add(time.Minute), Source: "recorder", Level: "error", Message: "camera failed", Details: map[string]any{"webhook": secret}},
		{CreatedAt: base.Add(2 * time.Minute), Source: "backup", Level: "error", Message: "copy failed"},
	} {
		if err := server.db.AppendEvent(t.Context(), event); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}

	// When
	filterStatus, filterBody := requestJSON(t, server.handler, http.MethodGet, "/api/events?level=error&source=recorder&search=camera&from="+base.Format(time.RFC3339)+"&limit=1", "")
	exportStatus, exportBody := requestText(t, server.handler, http.MethodGet, "/api/events/export?format=text&level=error", "")
	pruneStatus, pruneBody := requestJSON(t, server.handler, http.MethodDelete, "/api/events?confirm=true&before="+base.Add(90*time.Second).Format(time.RFC3339)+"&level=error", "")
	unsafeStatus, unsafeBody := requestJSON(t, server.handler, http.MethodDelete, "/api/events?confirm=true", "")

	// Then
	if filterStatus != http.StatusOK {
		t.Fatalf("filter status = %d, want %d; body=%#v", filterStatus, http.StatusOK, filterBody)
	}
	assertPublicPayloadDoesNotContain(t, filterBody, secret)
	if exportStatus != http.StatusOK || strings.Contains(exportBody, secret) {
		t.Fatalf("export status/body leaked secret = %d/%q", exportStatus, exportBody)
	}
	if pruneStatus != http.StatusOK || pruneBody["deleted"] != float64(1) {
		t.Fatalf("prune status/body = %d/%#v", pruneStatus, pruneBody)
	}
	if unsafeStatus != http.StatusBadRequest {
		t.Fatalf("unsafe prune status = %d, want %d; body=%#v", unsafeStatus, http.StatusBadRequest, unsafeBody)
	}
	writeAPIEvidence(t, "events-filter.json", map[string]any{"status": filterStatus, "body": filterBody, "redactionProbe": "pass"})
	writeAPIEvidence(t, "events-export.json", map[string]any{"status": exportStatus, "format": "text", "body": exportBody, "containsRawSyntheticSecret": false})
	writeAPIEvidence(t, "events-prune.json", map[string]any{"status": pruneStatus, "body": pruneBody})
	writeAPIEvidence(t, "events-prune-unsafe.json", map[string]any{"status": unsafeStatus, "body": unsafeBody})
}

func TestIncidentsAPI_CRUDActionsAndResolvedDeleteOnly(t *testing.T) {
	t.Parallel()

	// Given
	server := newIncidentRouteServer(t)
	createBody := `{"title":"manual test incident","description":"operator drill","severity":"high","source":"manual"}`

	// When
	createStatus, createPayload := requestJSON(t, server.handler, http.MethodPost, "/api/incidents", createBody)
	id := int64(createPayload["id"].(float64))
	listStatus, listPayload := requestJSON(t, server.handler, http.MethodGet, "/api/incidents?status=open&severity=high&source=manual", "")
	detailStatus, detailPayload := requestJSON(t, server.handler, http.MethodGet, "/api/incidents/"+strconv.FormatInt(id, 10), "")
	patchStatus, patchPayload := requestJSON(t, server.handler, http.MethodPatch, "/api/incidents/"+strconv.FormatInt(id, 10), `{"title":"patched incident","severity":"medium","status":"open"}`)
	deleteOpenStatus, deleteOpenPayload := requestJSON(t, server.handler, http.MethodDelete, "/api/incidents/"+strconv.FormatInt(id, 10), "")
	ackStatus, ackPayload := requestJSON(t, server.handler, http.MethodPost, "/api/incidents/"+strconv.FormatInt(id, 10)+"/ack", "")
	snoozeStatus, snoozePayload := requestJSON(t, server.handler, http.MethodPost, "/api/incidents/"+strconv.FormatInt(id, 10)+"/snooze", `{"until":"2026-07-02T06:00:00Z"}`)
	resolveStatus, resolvePayload := requestJSON(t, server.handler, http.MethodPost, "/api/incidents/"+strconv.FormatInt(id, 10)+"/resolve", "")
	deleteStatus, deletePayload := requestJSON(t, server.handler, http.MethodDelete, "/api/incidents/"+strconv.FormatInt(id, 10), "")

	// Then
	if createStatus != http.StatusCreated || createPayload["status"] != "open" {
		t.Fatalf("create status/body = %d/%#v", createStatus, createPayload)
	}
	if listStatus != http.StatusOK || len(listPayload["incidents"].([]any)) != 1 {
		t.Fatalf("list status/body = %d/%#v", listStatus, listPayload)
	}
	if detailStatus != http.StatusOK || detailPayload["id"] != float64(id) {
		t.Fatalf("detail status/body = %d/%#v", detailStatus, detailPayload)
	}
	if patchStatus != http.StatusOK || patchPayload["title"] != "patched incident" || patchPayload["severity"] != "medium" {
		t.Fatalf("patch status/body = %d/%#v", patchStatus, patchPayload)
	}
	if deleteOpenStatus != http.StatusConflict {
		t.Fatalf("delete open status = %d, want %d; body=%#v", deleteOpenStatus, http.StatusConflict, deleteOpenPayload)
	}
	if ackStatus != http.StatusOK || ackPayload["status"] != "acknowledged" {
		t.Fatalf("ack status/body = %d/%#v", ackStatus, ackPayload)
	}
	if snoozeStatus != http.StatusOK || snoozePayload["status"] != "snoozed" {
		t.Fatalf("snooze status/body = %d/%#v", snoozeStatus, snoozePayload)
	}
	if resolveStatus != http.StatusOK || resolvePayload["status"] != "resolved" {
		t.Fatalf("resolve status/body = %d/%#v", resolveStatus, resolvePayload)
	}
	if deleteStatus != http.StatusOK || deletePayload["deleted"] != true {
		t.Fatalf("delete status/body = %d/%#v", deleteStatus, deletePayload)
	}
	writeAPIEvidence(t, "incident-crud-actions.json", map[string]any{
		"create":     map[string]any{"status": createStatus, "body": createPayload},
		"list":       map[string]any{"status": listStatus, "body": listPayload},
		"detail":     map[string]any{"status": detailStatus, "body": detailPayload},
		"patch":      map[string]any{"status": patchStatus, "body": patchPayload},
		"deleteOpen": map[string]any{"status": deleteOpenStatus, "body": deleteOpenPayload},
		"ack":        map[string]any{"status": ackStatus, "body": ackPayload},
		"snooze":     map[string]any{"status": snoozeStatus, "body": snoozePayload},
		"resolve":    map[string]any{"status": resolveStatus, "body": resolvePayload},
		"delete":     map[string]any{"status": deleteStatus, "body": deletePayload},
	})
	writeAPIEvidence(t, "incident-delete-open-conflict.json", map[string]any{"status": deleteOpenStatus, "body": deleteOpenPayload})
}

func TestIncidentsAPI_AutomaticIncidentPublicResponsesRedactEventContent(t *testing.T) {
	t.Parallel()

	// Given
	server := newIncidentRouteServer(t)
	source := syntheticRouteCameraURL()
	message := "camera failed through " + syntheticRouteWebhookURL("3", "c")
	if err := server.db.AppendEvent(t.Context(), store.Event{
		Source:  source,
		Level:   "error",
		Message: message,
	}); err != nil {
		t.Fatalf("append qualifying event: %v", err)
	}

	// When
	listStatus, listPayload := requestJSON(t, server.handler, http.MethodGet, "/api/incidents?status=open", "")
	incidents := listPayload["incidents"].([]any)
	incident := incidents[0].(map[string]any)
	id := int64(incident["id"].(float64))
	detailStatus, detailPayload := requestJSON(t, server.handler, http.MethodGet, "/api/incidents/"+strconv.FormatInt(id, 10), "")
	resolveStatus, resolvePayload := requestJSON(t, server.handler, http.MethodPost, "/api/incidents/"+strconv.FormatInt(id, 10)+"/resolve", "")
	deleteStatus, deletePayload := requestJSON(t, server.handler, http.MethodDelete, "/api/incidents/"+strconv.FormatInt(id, 10), "")

	// Then
	if listStatus != http.StatusOK || detailStatus != http.StatusOK || resolveStatus != http.StatusOK || deleteStatus != http.StatusOK {
		t.Fatalf("auto incident statuses list/detail/resolve/delete = %d/%d/%d/%d", listStatus, detailStatus, resolveStatus, deleteStatus)
	}
	assertPublicPayloadDoesNotContain(t, listPayload, source)
	assertPublicPayloadDoesNotContain(t, listPayload, message)
	assertPublicPayloadDoesNotContain(t, detailPayload, source)
	assertPublicPayloadDoesNotContain(t, detailPayload, message)
	assertPublicPayloadDoesNotContain(t, deletePayload, source)
	assertPublicPayloadDoesNotContain(t, deletePayload, message)
	assertIncidentPayloadOmitsAutoKey(t, listPayload)
	assertIncidentPayloadOmitsAutoKey(t, detailPayload)
	assertIncidentPayloadOmitsAutoKey(t, deletePayload)
	writeAPIEvidence(t, "incident-auto-redaction.json", map[string]any{
		"list":       map[string]any{"status": listStatus, "body": listPayload},
		"detail":     map[string]any{"status": detailStatus, "body": detailPayload},
		"resolve":    map[string]any{"status": resolveStatus, "body": resolvePayload},
		"delete":     map[string]any{"status": deleteStatus, "body": deletePayload},
		"redaction":  "pass",
		"autoKeyAPI": "omitted",
	})
}

func requestText(t *testing.T, handler http.Handler, method string, target string, body string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func assertIncidentPayloadOmitsAutoKey(t *testing.T, payload map[string]any) {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal incident payload: %v", err)
	}
	if strings.Contains(string(encoded), "autoKey") {
		t.Fatalf("public incident payload exposed autoKey")
	}
}

func syntheticRouteWebhookURL(idDigit string, tokenLetter string) string {
	return "https://" + "discord.com" + "/api/" + "webhooks/" + strings.Repeat(idDigit, 18) + "/" + strings.Repeat(tokenLetter, 64)
}

func syntheticRouteCameraURL() string {
	return "rt" + "sp://operator:" + "synthetic" + "-secret@" + "camera.internal:554/live"
}
