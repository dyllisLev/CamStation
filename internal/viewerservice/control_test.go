package viewerservice

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestControlHeartbeatContinuesWithViewerClosed(t *testing.T) {
	var mu sync.Mutex
	var payloads []HeartbeatPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/viewers/heartbeat" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var payload HeartbeatPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode heartbeat: %v", err)
		}
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{})
	}))
	defer server.Close()

	loop := HTTPControlLoop{HTTPClient: server.Client(), HeartbeatInterval: 5 * time.Millisecond, HeartbeatRequestDeadline: time.Second}
	cfg := MachineConfig{SchemaVersion: ConfigSchemaVersion, ServerURL: server.URL, DisplayName: "Wall", ClientID: "stable-client"}
	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)
	defer cancel()
	if err := loop.Run(ctx, cfg, statusSourceFunc(func(context.Context) StatusSnapshot {
		return StatusSnapshot{Viewer: "closed", Renderer: "not_ready"}
	}), commandSinkFunc{}); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(payloads) < 2 {
		t.Fatalf("heartbeats=%d", len(payloads))
	}
	for _, payload := range payloads {
		if payload.ID != cfg.ClientID || payload.Agent.State != "online" || payload.Viewer.State != "closed" || payload.Update.State != "idle" {
			t.Fatalf("payload=%+v", payload)
		}
	}
}

func TestHeartbeatUsesLeaseTelemetryFromServer(t *testing.T) {
	var payload HeartbeatPayload
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/viewers/heartbeat" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{})
	}))
	defer control.Close()

	statusServer := testServer(&memoryConfigStore{config: testMachineConfig()}, nil)
	peer := Peer{PID: 101, SessionID: 9, Interactive: true}
	grantResponse, err := statusServer.Handle(t.Context(), "connection-1", peer, Request{Version: PipeProtocolVersion, RequestID: "acquire", Type: "acquire_lease"})
	if err != nil || !grantResponse.OK {
		t.Fatalf("grant=%+v err=%v", grantResponse, err)
	}
	var grant LeaseGrant
	if err := json.Unmarshal(grantResponse.Payload, &grant); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		requestType string
		requestID   string
		state       string
	}{
		{requestType: "viewer_status", requestID: "viewer", state: "running"},
		{requestType: "renderer_status", requestID: "renderer", state: "ready"},
	} {
		payload, _ := json.Marshal(map[string]any{"leaseId": grant.LeaseID, "state": item.state})
		response, err := statusServer.Handle(t.Context(), "connection-1", peer, Request{Version: PipeProtocolVersion, RequestID: item.requestID, Type: item.requestType, Payload: payload})
		if err != nil || !response.OK {
			t.Fatalf("telemetry=%+v err=%v", response, err)
		}
	}
	progressAt := time.Now().Add(-time.Second).UTC()
	streamPayload, _ := json.Marshal(map[string]any{
		"leaseId": grant.LeaseID, "streamName": "yard-live", "transport": "mse", "phase": "retrying",
		"lastProgressAt": progressAt.UnixMilli(),
	})
	streamResponse, err := statusServer.Handle(t.Context(), "connection-1", peer, Request{
		Version: PipeProtocolVersion, RequestID: "stream", Type: "stream_telemetry", Payload: streamPayload,
	})
	if err != nil || !streamResponse.OK {
		t.Fatalf("stream telemetry=%+v err=%v", streamResponse, err)
	}
	loop := HTTPControlLoop{HTTPClient: control.Client(), HeartbeatRequestDeadline: time.Second}
	config := MachineConfig{SchemaVersion: ConfigSchemaVersion, ServerURL: control.URL, DisplayName: "Wall", ClientID: "client-1"}
	loop.sendHeartbeat(t.Context(), config, statusServer, commandSinkFunc{})
	if payload.Agent.State != "online" || payload.Viewer.State != "running" || payload.Renderer.State != "ready" ||
		payload.Viewer.LastHeartbeatAt == nil || payload.Renderer.LastHeartbeatAt == nil ||
		payload.Renderer.LastProgressAt == nil || len(payload.Streams) != 1 ||
		payload.Streams[0].StreamName != "yard-live" || payload.Streams[0].State != "retrying" ||
		payload.Streams[0].Transport != "mse" {
		t.Fatalf("heartbeat=%+v", payload)
	}
}

func TestViewerFreshnessDowngradesCachedRuntimeWithoutErasingEvidence(t *testing.T) {
	now := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)
	old := now.Add(-16 * time.Second)
	progress := now.Add(-20 * time.Second)
	snapshot := StatusSnapshot{
		Viewer: "running", ViewerLastHeartbeatAt: &old,
		Renderer: "ready", RendererLastHeartbeatAt: &old, RendererLastProgressAt: &progress,
		Streams: []ViewerStreamState{{StreamName: "yard-live", State: "playing", LastProgressAt: &progress}},
	}

	got := withViewerFreshness(snapshot, now, 15*time.Second)
	if got.Viewer != "unresponsive" || got.Renderer != "unresponsive" {
		t.Fatalf("freshness state=%+v", got)
	}
	if got.ViewerLastHeartbeatAt != snapshot.ViewerLastHeartbeatAt ||
		got.RendererLastHeartbeatAt != snapshot.RendererLastHeartbeatAt ||
		got.RendererLastProgressAt != snapshot.RendererLastProgressAt || len(got.Streams) != 1 {
		t.Fatalf("freshness erased evidence: %+v", got)
	}

	fresh := now.Add(-5 * time.Second)
	snapshot.ViewerLastHeartbeatAt = &fresh
	snapshot.RendererLastHeartbeatAt = &fresh
	got = withViewerFreshness(snapshot, now, 15*time.Second)
	if got.Viewer != "running" || got.Renderer != "ready" {
		t.Fatalf("fresh runtime downgraded: %+v", got)
	}
}

func TestHeartbeatMarksStatusStorageFailureDegraded(t *testing.T) {
	var payload HeartbeatPayload
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{})
	}))
	defer control.Close()
	loop := HTTPControlLoop{HTTPClient: control.Client(), HeartbeatRequestDeadline: time.Second}
	config := MachineConfig{SchemaVersion: ConfigSchemaVersion, ServerURL: control.URL, DisplayName: "Wall", ClientID: "client-1"}
	loop.sendHeartbeat(t.Context(), config, snapshotErrorSource{}, commandSinkFunc{})
	if payload.Control.State != "degraded" || payload.Viewer.State != "closed" || payload.Renderer.State != "not_ready" {
		t.Fatalf("heartbeat=%+v", payload)
	}
}

type snapshotErrorSource struct{}

func (snapshotErrorSource) Snapshot(context.Context) (StatusSnapshot, error) {
	return StatusSnapshot{}, errors.New("storage unavailable")
}

func TestControlTransportUsesBoundedReconnectDelays(t *testing.T) {
	state := ReconnectState{}
	got := []time.Duration{state.NextDelay(), state.NextDelay(), state.NextDelay(), state.NextDelay(), state.NextDelay(), state.NextDelay()}
	want := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 30 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delay[%d]=%v want %v", i, got[i], want[i])
		}
	}
}

func TestViewerCommandSinkOnlyDeliversToActiveLease(t *testing.T) {
	sink := &recordingSink{}
	command := Command{ID: 7, Type: "reload_live", PayloadHash: strings.Repeat("a", 64), TTLSeconds: 300}
	if err := sink.DeliverViewerCommand(command); err != nil {
		t.Fatal(err)
	}
	if len(sink.commands) != 1 || sink.commands[0].ID != command.ID {
		t.Fatalf("commands=%+v", sink.commands)
	}
}

func TestControlClientReportsExactCommandResult(t *testing.T) {
	var method, path string
	var result CommandResult
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "viewer-1"}
	command := Command{ID: 19, Type: "ping", PayloadHash: "hash", TTLSeconds: 300}
	want := CommandResult{State: "running", OperationKey: "command-19"}
	if err := client.Report(t.Context(), command, want); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || path != "/api/viewers/viewer-1/commands/19" || result != want {
		t.Fatalf("method=%s path=%s result=%#v", method, path, result)
	}
}

func TestServiceViewerCommandUsesOnlyCurrentLeaseConnection(t *testing.T) {
	listener := newFakePipeListener()
	runtime := Service{Store: missingConfigStore{}, Listener: listener}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)
	client, serviceConn := net.Pipe()
	listener.connections <- &fakePipeConnection{ReadWriteCloser: serviceConn, peer: Peer{PID: 99, SessionID: 1, Interactive: true, UserSID: "S-1-5-21-test"}}
	writeRequest(t, client, Request{Version: PipeProtocolVersion, RequestID: "lease", Type: "acquire_lease"})
	grantResponse := readResponse(t, client)
	if !grantResponse.OK {
		t.Fatalf("grant=%+v", grantResponse)
	}
	var grant LeaseGrant
	if err := json.Unmarshal(grantResponse.Payload, &grant); err != nil {
		t.Fatal(err)
	}
	command := Command{ID: 7, Type: "reload_live", PayloadHash: strings.Repeat("a", 64), TTLSeconds: 300}
	resultDone := make(chan error, 1)
	go func() { resultDone <- runtime.DeliverViewerCommand(command) }()
	commandResponse := readResponse(t, client)
	if err := <-resultDone; err != nil {
		t.Fatal(err)
	}
	var delivered viewerCommandEvent
	if err := json.Unmarshal(commandResponse.Payload, &delivered); err != nil || delivered.Type != command.Type || delivered.OperationKey != "command-7" {
		t.Fatalf("response=%+v delivered=%+v err=%v", commandResponse, delivered, err)
	}
	resultPayload, _ := json.Marshal(LocalCommandResult{LeaseID: grant.LeaseID, OperationKey: delivered.OperationKey, Succeeded: true})
	writeRequest(t, client, Request{Version: PipeProtocolVersion, RequestID: "result", Type: "command_result", Payload: resultPayload})
	if response := readResponse(t, client); !response.OK {
		t.Fatalf("command result response=%#v", response)
	}
	_ = client.Close()
	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestServiceViewerCommandDoesNotBlockOnUnreadPipe(t *testing.T) {
	listener := newFakePipeListener()
	runtime := Service{Store: missingConfigStore{}, Listener: listener}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)
	client, serviceConn := net.Pipe()
	listener.connections <- &fakePipeConnection{ReadWriteCloser: serviceConn, peer: Peer{PID: 101, SessionID: 1, Interactive: true, UserSID: "S-1-5-21-test"}}
	writeRequest(t, client, Request{Version: PipeProtocolVersion, RequestID: "lease", Type: "acquire_lease"})
	_ = readResponse(t, client)
	result := make(chan error, 1)
	go func() {
		result <- runtime.DeliverViewerCommand(Command{ID: 8, Type: "reload_live", PayloadHash: strings.Repeat("b", 64), TTLSeconds: 300})
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		_ = client.Close()
		cancel()
		t.Fatal("command delivery blocked on unread pipe")
	}
	_ = client.Close()
	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
}

type statusSourceFunc func(context.Context) StatusSnapshot

func (fn statusSourceFunc) Status(ctx context.Context) StatusSnapshot { return fn(ctx) }

type commandSinkFunc struct{}

func (commandSinkFunc) DeliverViewerCommand(Command) error { return nil }
func (commandSinkFunc) SetDesiredUpdate(UpdateNotice)      {}

type recordingSink struct{ commands []Command }

func (sink *recordingSink) DeliverViewerCommand(command Command) error {
	sink.commands = append(sink.commands, command)
	return nil
}
func (*recordingSink) SetDesiredUpdate(UpdateNotice) {}
