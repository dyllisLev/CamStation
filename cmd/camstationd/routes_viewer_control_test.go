package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestViewerHeartbeatEnsuresExactDesiredReleaseCommand(t *testing.T) {
	server := newTestRouteServer(t)
	releaseDir := filepath.Join(filepath.Dir(server.recordingsDir), "viewer-releases", "current")
	digest := publishViewerFixture(t, releaseDir, []byte("installer"))
	controlAt := time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	heartbeat := `{
		"id":"viewer-control","displayName":"Control wall","appVersion":"2.0.0",
		"hostname":"viewer-host","deviceLabel":"wall","route":"/live?viewer=1","mode":"live",
		"agent":{"state":"online","version":"2.1.0"},
		"control":{"state":"control_degraded","lastSuccessAt":"` + controlAt + `"},
		"viewer":{"state":"running","version":"2.0.0"},
		"renderer":{"state":"ready"},"update":{"state":"idle","generation":0},"streams":[]
	}`

	status, body := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", heartbeat)
	if status != http.StatusOK {
		t.Fatalf("heartbeat status = %d; body=%#v", status, body)
	}
	viewer, ok := body["viewer"].(map[string]any)
	if !ok || viewer["status"] != "control_degraded" {
		t.Fatalf("heartbeat viewer health = %#v", body["viewer"])
	}
	desired, ok := body["desiredRelease"].(map[string]any)
	if !ok || desired["version"] != "2.0.0-dev.1" || desired["sha256"] != digest || desired["generation"] != float64(1) {
		t.Fatalf("desired release = %#v", body["desiredRelease"])
	}
	ttl, ttlOK := desired["ttlSeconds"].(float64)
	if desired["commandId"] == nil || desired["payloadHash"] == "" || desired["createdAt"] == "" || !ttlOK || ttl <= 0 {
		t.Fatalf("desired release omitted durable command identity = %#v", desired)
	}
	commandID := int64(desired["commandId"].(float64))
	delivered := performRequest(t, server.handler, http.MethodGet, "/api/viewers/viewer-control/commands/next")
	if delivered.Code != http.StatusOK {
		t.Fatalf("deliver ensured command status = %d body=%s", delivered.Code, delivered.Body.String())
	}
	var updateCommand store.ViewerCommand
	if err := json.Unmarshal(delivered.Body.Bytes(), &updateCommand); err != nil {
		t.Fatalf("decode delivered update command: %v", err)
	}
	if updateCommand.ID != commandID || updateCommand.PayloadHash != desired["payloadHash"] || updateCommand.Generation != 1 {
		t.Fatalf("delivered command %#v does not match desired %#v", updateCommand, desired)
	}
	if _, err := server.db.ApplyViewerCommandResult(t.Context(), "viewer-control", commandID, store.ViewerCommandResult{
		State: store.ViewerCommandRejected, OperationKey: "update-1",
	}); err != nil {
		t.Fatalf("reject desired update command: %v", err)
	}
	status, body = requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", heartbeat)
	desired, ok = body["desiredRelease"].(map[string]any)
	if status != http.StatusOK || !ok || desired["generation"] != float64(1) || int64(desired["commandId"].(float64)) != commandID {
		t.Fatalf("rejected desired release rearmed = %d %#v", status, body["desiredRelease"])
	}
	commands, err := server.db.ListViewerCommands(t.Context(), "viewer-control")
	if err != nil || len(commands) != 1 {
		t.Fatalf("same-target commands = %#v err=%v", commands, err)
	}

	newDigest := publishViewerFixture(t, releaseDir, []byte("installer replacement"))
	status, body = requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", heartbeat)
	desired, ok = body["desiredRelease"].(map[string]any)
	if status != http.StatusOK || !ok || desired["sha256"] != newDigest || desired["generation"] != float64(2) || int64(desired["commandId"].(float64)) == commandID {
		t.Fatalf("new digest desired release = %d %#v", status, body["desiredRelease"])
	}
	assertEncodedDoesNotContain(t, body, releaseDir)
}

func TestViewerHeartbeatDoesNotEnqueueInstalledReleaseUnlessReportedDigestDiffers(t *testing.T) {
	server := newTestRouteServer(t)
	releaseDir := filepath.Join(filepath.Dir(server.recordingsDir), "viewer-releases", "current")
	digest := publishViewerFixture(t, releaseDir, []byte("installer"))
	heartbeat := `{
		"id":"viewer-current","displayName":"Current wall","appVersion":"2.0.0-dev.1",
		"route":"/live?viewer=1","mode":"live","agent":{"state":"online","version":"2.0.0-dev.1"},
		"viewer":{"state":"running","version":"2.0.0-dev.1"},"renderer":{"state":"ready"},"streams":[]
	}`
	status, body := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", heartbeat)
	commands, err := server.db.ListViewerCommands(t.Context(), "viewer-current")
	if status != http.StatusOK || body["desiredRelease"] != nil || err != nil || len(commands) != 0 {
		t.Fatalf("installed release heartbeat = %d body=%#v commands=%#v err=%v", status, body, commands, err)
	}

	heartbeat = strings.Replace(heartbeat, `"version":"2.0.0-dev.1"`, `"version":"2.0.0-dev.1","artifactSha256":"`+strings.Repeat("0", 64)+`"`, 1)
	status, body = requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", heartbeat)
	desired, ok := body["desiredRelease"].(map[string]any)
	if status != http.StatusOK || !ok || desired["sha256"] != digest || desired["generation"] != float64(1) {
		t.Fatalf("same-version new digest heartbeat = %d desired=%#v", status, body["desiredRelease"])
	}
}

func TestViewerAdminListDoesNotDeliverAndSSEDeliversCommand(t *testing.T) {
	server := newTestRouteServer(t)
	seedRouteViewer(t, server, "viewer-sse")
	created, err := server.db.CreateViewerCommand(t.Context(), "viewer-sse", store.ViewerCommandCreate{Type: "restart_viewer"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	status, listed := requestJSONArray(t, server.handler, http.MethodGet, "/api/viewers/viewer-sse/commands", "")
	if status != http.StatusOK || len(listed) != 1 || listed[0]["state"] != "pending" {
		t.Fatalf("admin command list = %d %#v", status, listed)
	}
	before, err := server.db.GetViewerCommand(t.Context(), "viewer-sse", created.ID)
	if err != nil || before.DeliveredAt != nil {
		t.Fatalf("admin list delivered command: %#v err=%v", before, err)
	}

	httpServer := httptest.NewServer(server.handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/viewers/viewer-sse/control", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer response.Body.Close()
	if !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE content type = %q", response.Header.Get("Content-Type"))
	}
	event, payload := readSSEEvent(t, bufio.NewReader(response.Body))
	if event != "command" {
		t.Fatalf("SSE event = %q payload=%s", event, payload)
	}
	var delivered store.ViewerCommand
	if err := json.Unmarshal([]byte(payload), &delivered); err != nil {
		t.Fatalf("decode SSE command: %v; payload=%s", err, payload)
	}
	if delivered.ID != created.ID || delivered.State != store.ViewerCommandDelivered || delivered.DeliveredAt == nil {
		t.Fatalf("SSE delivered command = %#v", delivered)
	}
	cancel()
}

func TestViewerSSEEmitsKeepaliveWithinTenSeconds(t *testing.T) {
	server := newTestRouteServer(t)
	seedRouteViewer(t, server, "viewer-keepalive")
	httpServer := httptest.NewServer(server.handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/api/viewers/viewer-keepalive/control", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	started := time.Now()
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer response.Body.Close()
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("read SSE keepalive: %v", err)
	}
	if strings.TrimSpace(line) != ": keepalive" || time.Since(started) > 10*time.Second {
		t.Fatalf("keepalive line/time = %q/%s", line, time.Since(started))
	}
}

func TestViewerLongPollWait25ReturnsNoContent(t *testing.T) {
	t.Parallel()
	server := newTestRouteServer(t)
	seedRouteViewer(t, server, "viewer-poll")
	started := time.Now()
	response := performRequest(t, server.handler, http.MethodGet, "/api/viewers/viewer-poll/commands/next?wait=25")
	elapsed := time.Since(started)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("empty long poll = %d body=%q", response.Code, response.Body.String())
	}
	if elapsed < 24*time.Second || elapsed > 28*time.Second {
		t.Fatalf("25-second long poll duration = %s", elapsed)
	}
}

func TestViewerLongPollWaitCapsBeforeDurationOverflow(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/viewers/viewer-poll/commands/next?wait=10000000000", nil)
	wait, err := viewerLongPollWait(request)
	if err != nil || wait != 25*time.Second {
		t.Fatalf("overflowing wait = %s err=%v", wait, err)
	}
}

func TestViewerCommandDuplicateResultDoesNotChangeTimestamp(t *testing.T) {
	server := newTestRouteServer(t)
	seedRouteViewer(t, server, "viewer-result")
	created, err := server.db.CreateViewerCommand(t.Context(), "viewer-result", store.ViewerCommandCreate{Type: "ping"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	target := "/api/viewers/viewer-result/commands/" + commandIDString(created.ID)
	body := `{"state":"succeeded","operationKey":"op-result-1"}`
	firstStatus, first := requestJSON(t, server.handler, http.MethodPatch, target, body)
	secondStatus, second := requestJSON(t, server.handler, http.MethodPatch, target, body)
	if firstStatus != http.StatusOK || secondStatus != http.StatusOK {
		t.Fatalf("duplicate result statuses = %d/%d bodies=%#v/%#v", firstStatus, secondStatus, first, second)
	}
	if first["updatedAt"] != second["updatedAt"] || first["resultAt"] != second["resultAt"] {
		t.Fatalf("duplicate result changed timestamps: first=%#v second=%#v", first, second)
	}
}

func TestViewerOfflineRegistryEntryCanBeDeleted(t *testing.T) {
	server := newTestRouteServer(t)
	seedRouteViewer(t, server, "viewer-offline")
	patchStatus, _ := requestJSON(t, server.handler, http.MethodPatch, "/api/viewers/viewer-offline", `{"status":"offline"}`)
	deleteStatus, body := requestJSON(t, server.handler, http.MethodDelete, "/api/viewers/viewer-offline", "")
	if patchStatus != http.StatusOK || deleteStatus != http.StatusOK || body["deleted"] != true {
		t.Fatalf("offline viewer delete = patch %d delete %d body=%#v", patchStatus, deleteStatus, body)
	}
}

func seedRouteViewer(t *testing.T, server testRouteServer, id string) {
	t.Helper()
	if _, err := server.db.UpsertViewerHeartbeat(t.Context(), store.ViewerHeartbeat{
		ID: id, DisplayName: id, Route: "/live?viewer=1", Mode: "live",
	}); err != nil {
		t.Fatalf("seed viewer %s: %v", id, err)
	}
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	var event, data string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return event, data
		}
		if strings.HasPrefix(line, "event: ") {
			event = strings.TrimPrefix(line, "event: ")
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func commandIDString(id int64) string {
	return strconv.FormatInt(id, 10)
}
