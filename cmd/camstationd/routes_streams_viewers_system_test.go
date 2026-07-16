package main

import (
	"net/http"
	"strconv"
	"testing"

	"camstation/internal/store"
	"camstation/internal/stream"
)

func TestStreamOperations_RestartProbeAndDeleteUseSafeDTOs(t *testing.T) {
	t.Parallel()

	// Given
	fakeStreamer := &fakeStreamController{status: stream.Status{
		Installed: true,
		Running:   true,
		APIURL:    "http://127.0.0.1:1984",
		Streams: map[string]stream.StreamRuntime{
			"gate-main": {State: "running", ProducerCount: 1},
		},
	}}
	server := newTestRouteServerWithStreamer(t, fakeStreamer)
	secretURL := "rtsp://admin:" + "camera-" + "pass@10.10.10.55:554/live"
	camera, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name:       "Gate",
		URL:        secretURL,
		StreamName: "gate-main",
		State:      "active",
	})
	if err != nil {
		t.Fatalf("seed camera: %v", err)
	}

	// When
	probeStatus, probeBody := requestJSON(t, server.handler, http.MethodPost, "/api/streams/"+camera.StreamName+"/probe", `{}`)
	restartStatus, restartBody := requestJSON(t, server.handler, http.MethodPost, "/api/streams/"+camera.StreamName+"/restart", `{}`)
	deleteStatus, deleteBody := requestJSON(t, server.handler, http.MethodDelete, "/api/streams/"+camera.StreamName, `{}`)

	// Then
	if probeStatus != http.StatusOK {
		t.Fatalf("probe status = %d, want %d; body=%#v", probeStatus, http.StatusOK, probeBody)
	}
	if restartStatus != http.StatusOK {
		t.Fatalf("restart status = %d, want %d; body=%#v", restartStatus, http.StatusOK, restartBody)
	}
	if fakeStreamer.restartCalls != 1 {
		t.Fatalf("restart calls = %d, want 1", fakeStreamer.restartCalls)
	}
	if deleteStatus != http.StatusConflict {
		t.Fatalf("delete status = %d, want %d; body=%#v", deleteStatus, http.StatusConflict, deleteBody)
	}
	assertPublicPayloadDoesNotContain(t, probeBody, secretURL)
	assertPublicPayloadDoesNotContain(t, restartBody, secretURL)
	assertPublicPayloadDoesNotContain(t, deleteBody, secretURL)
	assertEncodedDoesNotContain(t, probeBody, "apiUrl")
	assertEncodedDoesNotContain(t, probeBody, "127.0.0.1:1984")
	assertEncodedDoesNotContain(t, restartBody, "apiUrl")
	assertEncodedDoesNotContain(t, restartBody, "127.0.0.1:1984")
	writeAPIEvidence(t, "streams-operations.json", map[string]any{
		"probe":   map[string]any{"status": probeStatus, "body": probeBody},
		"restart": map[string]any{"status": restartStatus, "body": restartBody},
		"delete":  map[string]any{"status": deleteStatus, "body": deleteBody},
	})
}

func TestViewersAPI_HeartbeatRegistryUpdateDeleteAndCommandLifecycle(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	secret := routeTestDiscordWebhookURL()
	heartbeat := `{"id":"viewer-01","displayName":"Wall ` + secret + `","appVersion":"2.0.0","hostname":"viewer-host","deviceLabel":"North wall","route":"/live","mode":"grid","streams":[{"streamName":"gate-main","state":"running","latencyMs":45}]}`

	// When
	heartbeatStatus, heartbeatBody := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", heartbeat)
	listStatus, listBody := requestJSONArray(t, server.handler, http.MethodGet, "/api/viewers", "")
	patchStatus, patchBody := requestJSON(t, server.handler, http.MethodPatch, "/api/viewers/viewer-01", `{"label":"Control wall","note":"rotate"}`)
	commandStatus, commandBody := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/viewer-01/commands", `{"type":"refresh","message":"refresh `+secret+`","route":"/live"}`)

	// Then
	if heartbeatStatus != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want %d; body=%#v", heartbeatStatus, http.StatusOK, heartbeatBody)
	}
	if listStatus != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%#v", listStatus, http.StatusOK, listBody)
	}
	if patchStatus != http.StatusOK || patchBody["label"] != "Control wall" {
		t.Fatalf("patch status/body = %d/%#v", patchStatus, patchBody)
	}
	if commandStatus != http.StatusCreated || commandBody["state"] != "pending" {
		t.Fatalf("command create status/body = %d/%#v", commandStatus, commandBody)
	}
	assertPublicPayloadDoesNotContain(t, heartbeatBody, secret)
	assertPublicArrayDoesNotContain(t, listBody, secret)
	assertPublicPayloadDoesNotContain(t, commandBody, secret)

	id := int64(commandBody["id"].(float64))
	commandPath := "/api/viewers/viewer-01/commands/" + strconv.FormatInt(id, 10)
	queueStatus, queueBody := requestJSONArray(t, server.handler, http.MethodGet, "/api/viewers/viewer-01/commands", "")
	ackStatus, ackBody := requestJSON(t, server.handler, http.MethodPatch, commandPath, `{"state":"acknowledged"}`)
	cancelCreateStatus, cancelCreateBody := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/viewer-01/commands", `{"type":"blank","message":"standby"}`)
	cancelID := int64(cancelCreateBody["id"].(float64))
	cancelPath := "/api/viewers/viewer-01/commands/" + strconv.FormatInt(cancelID, 10)
	cancelStatus, cancelBody := requestJSON(t, server.handler, http.MethodPost, cancelPath+"/cancel", `{"reason":"operator"}`)
	deleteCommandStatus, deleteCommandBody := requestJSON(t, server.handler, http.MethodDelete, cancelPath, `{}`)
	onlineDeleteStatus, onlineDeleteBody := requestJSON(t, server.handler, http.MethodDelete, "/api/viewers/viewer-01", `{}`)
	stalePatchStatus, stalePatchBody := requestJSON(t, server.handler, http.MethodPatch, "/api/viewers/viewer-01", `{"label":"Control wall","status":"stale","note":"rotate"}`)
	staleDeleteStatus, staleDeleteBody := requestJSON(t, server.handler, http.MethodDelete, "/api/viewers/viewer-01", `{}`)
	unknownDeleteStatus, unknownDeleteBody := requestJSON(t, server.handler, http.MethodDelete, "/api/viewers/missing", `{}`)
	badHeartbeatStatus, badHeartbeatBody := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", `{"id":""}`)

	if queueStatus != http.StatusOK || len(queueBody) != 1 || queueBody[0]["state"] != "pending" {
		t.Fatalf("queue status/body = %d/%#v", queueStatus, queueBody)
	}
	if ackStatus != http.StatusOK || ackBody["state"] != "acknowledged" {
		t.Fatalf("ack status/body = %d/%#v", ackStatus, ackBody)
	}
	if cancelCreateStatus != http.StatusCreated {
		t.Fatalf("cancel command create status = %d", cancelCreateStatus)
	}
	if cancelStatus != http.StatusOK || cancelBody["state"] != "cancelled" {
		t.Fatalf("cancel status/body = %d/%#v", cancelStatus, cancelBody)
	}
	if deleteCommandStatus != http.StatusOK || deleteCommandBody["state"] != "deleted" {
		t.Fatalf("delete command status/body = %d/%#v", deleteCommandStatus, deleteCommandBody)
	}
	if onlineDeleteStatus != http.StatusConflict {
		t.Fatalf("online delete status/body = %d/%#v, want conflict", onlineDeleteStatus, onlineDeleteBody)
	}
	if stalePatchStatus != http.StatusOK || stalePatchBody["status"] != "stale" {
		t.Fatalf("stale patch status/body = %d/%#v", stalePatchStatus, stalePatchBody)
	}
	if staleDeleteStatus != http.StatusOK || staleDeleteBody["deleted"] != true {
		t.Fatalf("stale delete status/body = %d/%#v", staleDeleteStatus, staleDeleteBody)
	}
	if unknownDeleteStatus != http.StatusNotFound {
		t.Fatalf("unknown viewer delete status = %d, want %d; body=%#v", unknownDeleteStatus, http.StatusNotFound, unknownDeleteBody)
	}
	if badHeartbeatStatus != http.StatusBadRequest {
		t.Fatalf("bad heartbeat status = %d, want %d; body=%#v", badHeartbeatStatus, http.StatusBadRequest, badHeartbeatBody)
	}
	writeAPIEvidence(t, "viewers-lifecycle.json", map[string]any{
		"heartbeat":     map[string]any{"status": heartbeatStatus, "body": heartbeatBody},
		"list":          map[string]any{"status": listStatus, "body": listBody},
		"patch":         map[string]any{"status": patchStatus, "body": patchBody},
		"queue":         map[string]any{"status": queueStatus, "body": queueBody},
		"ack":           map[string]any{"status": ackStatus, "body": ackBody},
		"cancel":        map[string]any{"status": cancelStatus, "body": cancelBody},
		"deleteCommand": map[string]any{"status": deleteCommandStatus, "body": deleteCommandBody},
		"onlineDelete":  map[string]any{"status": onlineDeleteStatus, "body": onlineDeleteBody},
		"stalePatch":    map[string]any{"status": stalePatchStatus, "body": stalePatchBody},
		"staleDelete":   map[string]any{"status": staleDeleteStatus, "body": staleDeleteBody},
		"failures":      map[string]any{"unknownDelete": unknownDeleteStatus, "badHeartbeat": badHeartbeatStatus, "onlineDelete": onlineDeleteStatus},
	})
}

func TestSystemAPI_StatusDiagnosticsMaintenanceJobsAndArtifactDeletion(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	secret := routeTestDiscordWebhookURL()

	// When
	statusCode, statusBody := requestJSON(t, server.handler, http.MethodGet, "/api/system/status", "")
	diagStatus, diagBody := requestJSON(t, server.handler, http.MethodPost, "/api/system/diagnostics", `{"reason":"operator `+secret+`"}`)
	jobsStatus, jobsBody := requestJSONArray(t, server.handler, http.MethodGet, "/api/system/jobs", "")
	maintenanceStatus, maintenanceBody := requestJSON(t, server.handler, http.MethodPost, "/api/system/maintenance", `{"action":"health_check"}`)
	deferredStatus, deferredBody := requestJSON(t, server.handler, http.MethodPost, "/api/system/maintenance", `{"action":"db_vacuum","defer":true}`)

	// Then
	if statusCode != http.StatusOK || statusBody["daemon"] == nil || statusBody["go2rtc"] == nil {
		t.Fatalf("system status = %d/%#v", statusCode, statusBody)
	}
	assertEncodedDoesNotContain(t, statusBody, "apiUrl")
	assertEncodedDoesNotContain(t, statusBody, "127.0.0.1:1984")
	if diagStatus != http.StatusCreated || diagBody["artifact"] == nil {
		t.Fatalf("diagnostics status/body = %d/%#v", diagStatus, diagBody)
	}
	assertEncodedDoesNotContain(t, diagBody, `"path"`)
	assertEncodedDoesNotContain(t, diagBody, "/diagnostics/diagnostic-")
	if jobsStatus != http.StatusOK || len(jobsBody) == 0 {
		t.Fatalf("system jobs status/body = %d/%#v", jobsStatus, jobsBody)
	}
	if maintenanceStatus != http.StatusCreated || maintenanceBody["state"] != "succeeded" {
		t.Fatalf("maintenance status/body = %d/%#v", maintenanceStatus, maintenanceBody)
	}
	if deferredStatus != http.StatusCreated || deferredBody["state"] != "queued" {
		t.Fatalf("deferred maintenance status/body = %d/%#v", deferredStatus, deferredBody)
	}
	deferredID := int64(deferredBody["id"].(float64))
	cancelStatus, cancelBody := requestJSON(t, server.handler, http.MethodPost, "/api/system/jobs/"+strconv.FormatInt(deferredID, 10)+"/cancel", `{"reason":"operator cancelled"}`)
	if cancelStatus != http.StatusOK || cancelBody["state"] != "cancelled" {
		t.Fatalf("cancel system job status/body = %d/%#v", cancelStatus, cancelBody)
	}

	artifactsStatus, artifactsBody := requestJSONArray(t, server.handler, http.MethodGet, "/api/system/diagnostics/artifacts", "")
	if artifactsStatus != http.StatusOK || len(artifactsBody) == 0 {
		t.Fatalf("artifacts status/body = %d/%#v", artifactsStatus, artifactsBody)
	}
	assertPublicArrayDoesNotContain(t, artifactsBody, `"path"`)
	assertPublicArrayDoesNotContain(t, artifactsBody, "/diagnostics/diagnostic-")
	artifactID := int64(artifactsBody[0]["id"].(float64))
	deleteArtifactStatus, deleteArtifactBody := requestJSON(t, server.handler, http.MethodDelete, "/api/system/diagnostics/artifacts/"+strconv.FormatInt(artifactID, 10), `{}`)
	if deleteArtifactStatus != http.StatusOK || deleteArtifactBody["deleted"] != true {
		t.Fatalf("delete artifact status/body = %d/%#v", deleteArtifactStatus, deleteArtifactBody)
	}
	assertEncodedDoesNotContain(t, deleteArtifactBody, `"path"`)
	assertEncodedDoesNotContain(t, deleteArtifactBody, "/diagnostics/diagnostic-")
	historyDeleteStatus, historyDeleteBody := requestJSON(t, server.handler, http.MethodDelete, "/api/system/diagnostics/history", `{}`)
	if historyDeleteStatus != http.StatusOK || historyDeleteBody["deleted"] == nil {
		t.Fatalf("delete history status/body = %d/%#v", historyDeleteStatus, historyDeleteBody)
	}
	unsafeDeleteStatus, unsafeDeleteBody := requestJSON(t, server.handler, http.MethodDelete, "/api/system/diagnostics/artifacts?path=../../.env", `{}`)
	if unsafeDeleteStatus != http.StatusBadRequest {
		t.Fatalf("unsafe artifact path status = %d, want %d; body=%#v", unsafeDeleteStatus, http.StatusBadRequest, unsafeDeleteBody)
	}
	badMaintenanceStatus, badMaintenanceBody := requestJSON(t, server.handler, http.MethodPost, "/api/system/maintenance", `{"action":"self_restart"}`)
	if badMaintenanceStatus != http.StatusBadRequest {
		t.Fatalf("bad maintenance status = %d, want %d; body=%#v", badMaintenanceStatus, http.StatusBadRequest, badMaintenanceBody)
	}
	assertPublicPayloadDoesNotContain(t, statusBody, secret)
	assertPublicPayloadDoesNotContain(t, diagBody, secret)
	writeAPIEvidence(t, "system-operations.json", map[string]any{
		"status":         map[string]any{"status": statusCode, "body": statusBody},
		"diagnostics":    map[string]any{"status": diagStatus, "body": diagBody},
		"jobs":           map[string]any{"status": jobsStatus, "body": jobsBody},
		"maintenance":    map[string]any{"status": maintenanceStatus, "body": maintenanceBody},
		"cancel":         map[string]any{"status": cancelStatus, "body": cancelBody},
		"deleteArtifact": map[string]any{"status": deleteArtifactStatus, "body": deleteArtifactBody},
		"deleteHistory":  map[string]any{"status": historyDeleteStatus, "body": historyDeleteBody},
		"failures":       map[string]any{"unsafeDelete": unsafeDeleteStatus, "badMaintenance": badMaintenanceStatus},
	})
}
