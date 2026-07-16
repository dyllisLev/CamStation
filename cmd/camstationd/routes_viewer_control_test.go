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

func TestViewerHeartbeatRequiresExactReportedVersionAndDigest(t *testing.T) {
	server := newTestRouteServer(t)
	releaseDir := filepath.Join(filepath.Dir(server.recordingsDir), "viewer-releases", "current")
	digest := publishViewerFixture(t, releaseDir, []byte("installer"))
	missingDigest := `{
		"id":"viewer-missing-digest","displayName":"Missing digest wall","appVersion":"2.0.0-dev.1",
		"route":"/live?viewer=1","mode":"live","agent":{"state":"online","version":"2.0.0-dev.1"},
		"viewer":{"state":"running","version":"2.0.0-dev.1"},"renderer":{"state":"ready"},"streams":[]
	}`
	status, body := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", missingDigest)
	desired, ok := body["desiredRelease"].(map[string]any)
	if status != http.StatusOK || !ok || desired["sha256"] != digest || desired["generation"] != float64(1) {
		t.Fatalf("missing digest heartbeat = %d desired=%#v", status, body["desiredRelease"])
	}
	commandID := desired["commandId"]

	malformedDigest := strings.Replace(missingDigest, `"version":"2.0.0-dev.1"`, `"version":"2.0.0-dev.1","artifactSha256":"NOT-A-DIGEST"`, 1)
	status, body = requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", malformedDigest)
	desired, ok = body["desiredRelease"].(map[string]any)
	commands, err := server.db.ListViewerCommands(t.Context(), "viewer-missing-digest")
	if status != http.StatusOK || !ok || desired["commandId"] != commandID || err != nil || len(commands) != 1 {
		t.Fatalf("malformed digest heartbeat = %d desired=%#v commands=%#v err=%v", status, body["desiredRelease"], commands, err)
	}
	assertEncodedDoesNotContain(t, body, "NOT-A-DIGEST")

	wrongDigest := strings.Replace(missingDigest, `"version":"2.0.0-dev.1"`, `"version":"2.0.0-dev.1","artifactSha256":"`+strings.Repeat("0", 64)+`"`, 1)
	status, body = requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", wrongDigest)
	desired, ok = body["desiredRelease"].(map[string]any)
	if status != http.StatusOK || !ok || desired["commandId"] != commandID || desired["generation"] != float64(1) {
		t.Fatalf("different digest heartbeat = %d desired=%#v", status, body["desiredRelease"])
	}

	exact := strings.Replace(missingDigest, `"viewer-missing-digest"`, `"viewer-exact"`, 1)
	exact = strings.Replace(exact, `"version":"2.0.0-dev.1"`, `"version":"2.0.0-dev.1","artifactSha256":"`+digest+`"`, 1)
	status, body = requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", exact)
	commands, err = server.db.ListViewerCommands(t.Context(), "viewer-exact")
	if status != http.StatusOK || body["desiredRelease"] != nil || err != nil || len(commands) != 0 {
		t.Fatalf("exact release heartbeat = %d desired=%#v commands=%#v err=%v", status, body["desiredRelease"], commands, err)
	}

	oldClient := `{"id":"viewer-old","displayName":"Old wall","route":"/live?viewer=1","mode":"live","agent":{"state":"online"}}`
	status, body = requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", oldClient)
	desired, ok = body["desiredRelease"].(map[string]any)
	if status != http.StatusOK || !ok || desired["sha256"] != digest || desired["generation"] != float64(1) {
		t.Fatalf("old client heartbeat = %d desired=%#v", status, body["desiredRelease"])
	}
}

func TestViewerUpdateCommitHealthRequiresExactFreshIndependentSignals(t *testing.T) {
	now := time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	command := store.ViewerCommand{
		ID: 41, ViewerID: "viewer-health-gate", Type: "update_app", DesiredVersion: "2.4.0", ArtifactSHA256: digest,
		PayloadHash: "payload-41", Generation: 7, OperationKey: "update-2.4.0-target-7",
		State: store.ViewerCommandRunning,
	}
	heartbeat := store.ViewerHeartbeat{
		ID: "viewer-health-gate", AppVersion: "2.4.0",
		Agent:    store.ViewerAgentHealth{State: "online", Version: "2.4.0", ArtifactSHA256: digest},
		Control:  store.ViewerControlHealth{State: "online", LastSuccessAt: timePointer(now.Add(-time.Second))},
		Viewer:   store.ViewerProcessHealth{State: "running", Version: "2.4.0", LastHeartbeatAt: timePointer(now.Add(-time.Second))},
		Renderer: store.ViewerRendererHealth{State: "ready", LastHeartbeatAt: timePointer(now.Add(-time.Second))},
		Update: store.ViewerUpdateHealth{
			State: "installer_launched", TargetVersion: "2.4.0", ArtifactSHA256: digest,
			Generation: 7, CommandID: 41, PayloadHash: command.PayloadHash, TransactionID: "update-2.4.0-target-7",
		},
	}
	observation, exact := viewerUpdateValidationObservation(heartbeat, command, now)
	if !exact || !observation.Healthy || observation.TransactionID != heartbeat.Update.TransactionID {
		t.Fatalf("exact health rejected: exact=%v observation=%#v", exact, observation)
	}

	checks := []func(*store.ViewerHeartbeat){
		func(value *store.ViewerHeartbeat) {
			value.Control.LastSuccessAt = timePointer(now.Add(-16 * time.Second))
		},
		func(value *store.ViewerHeartbeat) { value.Viewer.State = "restarting" },
		func(value *store.ViewerHeartbeat) { value.Renderer.LastHeartbeatAt = nil },
		func(value *store.ViewerHeartbeat) { value.Agent.ArtifactSHA256 = strings.Repeat("b", 64) },
		func(value *store.ViewerHeartbeat) { value.Update.PayloadHash = "wrong-payload" },
		func(value *store.ViewerHeartbeat) { value.Update.TransactionID = "wrong-transaction" },
	}
	for index, mutate := range checks {
		changed := heartbeat
		mutate(&changed)
		observation, exact := viewerUpdateValidationObservation(changed, command, now)
		if index >= len(checks)-2 {
			if exact {
				t.Fatalf("case %d mismatched transaction accepted", index)
			}
			continue
		}
		if exact && observation.Healthy {
			t.Fatalf("case %d unhealthy signal accepted: %#v", index, observation)
		}
	}
}

func TestViewerHeartbeatCommitTokenAppearsOnlyAfterContinuousExactRouteHealth(t *testing.T) {
	server := newTestRouteServer(t)
	digest := strings.Repeat("a", 64)
	viewer, err := server.db.UpsertViewerHeartbeat(t.Context(), store.ViewerHeartbeat{
		ID: "viewer-token-route", DisplayName: "Token route", Route: "/live?viewer=1", Mode: "live",
	})
	if err != nil {
		t.Fatal(err)
	}
	command, err := server.db.EnsureViewerUpdateCommand(t.Context(), viewer.ID, "2.4.0", digest)
	if err != nil {
		t.Fatal(err)
	}
	operationKey := "update-2.4.0-route-1"
	command, err = server.db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, store.ViewerCommandResult{
		State: store.ViewerCommandRunning, OperationKey: operationKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	desired := &viewerDesiredReleaseResponse{CommandID: command.ID}
	base := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	heartbeat := store.ViewerHeartbeat{
		ID: viewer.ID, AppVersion: command.DesiredVersion,
		Agent:    store.ViewerAgentHealth{State: "online", Version: command.DesiredVersion, ArtifactSHA256: digest},
		Control:  store.ViewerControlHealth{State: "online"},
		Viewer:   store.ViewerProcessHealth{State: "running", Version: command.DesiredVersion},
		Renderer: store.ViewerRendererHealth{State: "ready"},
		Update: store.ViewerUpdateHealth{
			State: "installer_launched", TargetVersion: command.DesiredVersion, ArtifactSHA256: digest,
			Generation: command.Generation, CommandID: command.ID, PayloadHash: command.PayloadHash, TransactionID: operationKey,
		},
	}
	deps := routeDeps{db: server.db}
	var token string
	for _, elapsed := range []time.Duration{0, 10 * time.Second, 20 * time.Second, 30 * time.Second} {
		now := base.Add(elapsed)
		heartbeat.Control.LastSuccessAt = timePointer(now)
		heartbeat.Viewer.LastHeartbeatAt = timePointer(now)
		heartbeat.Renderer.LastHeartbeatAt = timePointer(now)
		token, err = deps.viewerUpdateCommitToken(t.Context(), heartbeat, desired, now)
		if err != nil {
			t.Fatal(err)
		}
		if elapsed < 30*time.Second && token != "" {
			t.Fatalf("token issued at %v: %q", elapsed, token)
		}
	}
	if len(token) != 64 {
		t.Fatalf("commit token=%q", token)
	}
	heartbeat.Update.TransactionID = "wrong-transaction"
	if token, err = deps.viewerUpdateCommitToken(t.Context(), heartbeat, desired, base.Add(40*time.Second)); err != nil || token != "" {
		t.Fatalf("mismatch token=%q err=%v", token, err)
	}
}

func TestExactInstalledHeartbeatRecoversMissedRunningCommandReport(t *testing.T) {
	server := newTestRouteServer(t)
	digest := strings.Repeat("e", 64)
	viewer, err := server.db.UpsertViewerHeartbeat(t.Context(), store.ViewerHeartbeat{
		ID: "viewer-missed-running", DisplayName: "Missed running", Route: "/live?viewer=1", Mode: "live",
	})
	if err != nil {
		t.Fatal(err)
	}
	command, err := server.db.EnsureViewerUpdateCommand(t.Context(), viewer.ID, "2.4.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC)
	heartbeat := store.ViewerHeartbeat{
		ID:       viewer.ID,
		Agent:    store.ViewerAgentHealth{State: "online", Version: command.DesiredVersion, ArtifactSHA256: digest},
		Control:  store.ViewerControlHealth{State: "online", LastSuccessAt: timePointer(now)},
		Viewer:   store.ViewerProcessHealth{State: "running", LastHeartbeatAt: timePointer(now)},
		Renderer: store.ViewerRendererHealth{State: "ready", LastHeartbeatAt: timePointer(now)},
		Update: store.ViewerUpdateHealth{
			State: "installer_launched", TargetVersion: command.DesiredVersion, ArtifactSHA256: digest,
			Generation: command.Generation, CommandID: command.ID, PayloadHash: command.PayloadHash, TransactionID: expectedViewerUpdateOperationKey(command),
		},
	}
	token, err := (routeDeps{db: server.db}).viewerUpdateCommitToken(t.Context(), heartbeat, &viewerDesiredReleaseResponse{CommandID: command.ID}, now)
	if err != nil || token != "" {
		t.Fatalf("first recovered observation token=%q err=%v", token, err)
	}
	recovered, err := server.db.GetViewerCommand(t.Context(), viewer.ID, command.ID)
	if err != nil || recovered.State != store.ViewerCommandRunning || recovered.OperationKey != heartbeat.Update.TransactionID {
		t.Fatalf("recovered command=%#v err=%v", recovered, err)
	}
}

func timePointer(value time.Time) *time.Time { return &value }

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
