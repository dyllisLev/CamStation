package vieweragent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestBootstrapGrantRegistersExactlyOneElectronForGeneration(t *testing.T) {
	agent := pipeTestAgent(t)
	grant, err := agent.handlePipeMessage(PipeMessage{
		Version: PipeProtocolVersion, RequestID: "bootstrap-1", Type: "bootstrap_request", PID: 42, SessionID: 3,
	})
	if err != nil || grant.Type != "bootstrap_grant" || grant.Generation <= 0 || grant.Nonce == "" {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: PipeProtocolVersion, RequestID: "bootstrap-2", Type: "bootstrap_request", PID: 43, SessionID: 3,
	}); err == nil {
		t.Fatal("second bootstrap was accepted for the same generation")
	}

	registered, err := agent.handlePipeMessage(PipeMessage{
		Version: PipeProtocolVersion, RequestID: "viewer-1", Type: "viewer_register", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	})
	if err != nil || registered.Type != "viewer_registered" || registered.Generation != grant.Generation || registered.Nonce != grant.Nonce {
		t.Fatalf("registered=%+v err=%v", registered, err)
	}
	var payload struct {
		ServerURL string `json:"serverUrl"`
	}
	if err := json.Unmarshal(registered.Payload, &payload); err != nil || payload.ServerURL != agent.Config.ServerURL {
		t.Fatalf("payload=%s decoded=%+v err=%v", registered.Payload, payload, err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: PipeProtocolVersion, RequestID: "viewer-2", Type: "viewer_register", PID: 100, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	}); err == nil {
		t.Fatal("second Electron was accepted for the same generation")
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "viewer-heartbeat", Type: "viewer_heartbeat", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "bootstrap-running", Type: "bootstrap_request", PID: 44, SessionID: 3,
	}); err == nil {
		t.Fatal("running Viewer generation was replaced by a bootstrap request")
	}
	_, err = agent.updateState(func(state *MachineState) error {
		state.ExpectedViewerGeneration = state.ViewerGeneration + 1
		state.ViewerState = "restart_authorized"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "old-viewer-generation-claim", Type: "viewer_heartbeat", PID: 99, SessionID: 3,
		Generation: grant.Generation + 1, Nonce: grant.Nonce,
	}); err == nil {
		t.Fatal("current Viewer identity claimed the authorized next generation")
	}
	next, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "bootstrap-authorized", Type: "bootstrap_request", PID: 44, SessionID: 3,
	})
	if err != nil || next.Generation != grant.Generation+1 || next.Nonce == "" {
		t.Fatalf("authorized next=%+v err=%v", next, err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "bootstrap-hijack", Type: "bootstrap_request", PID: 45, SessionID: 3,
	}); err == nil {
		t.Fatal("outstanding authorized grant was replaced")
	}
}

func TestViewerPipeStoresBoundedStreamTelemetry(t *testing.T) {
	agent := pipeTestAgent(t)
	grant, _ := agent.handlePipeMessage(PipeMessage{Version: 1, RequestID: "b", Type: "bootstrap_request", PID: 42, SessionID: 3})
	_, _ = agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "v", Type: "viewer_register", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	})
	payload := json.RawMessage(`{"streamName":"yard-live","transport":"webrtc","phase":"playing","lastBinaryAt":1000,"lastProgressAt":2000,"url":"rtsp://secret@camera/live"}`)
	response, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "s", Type: "stream_telemetry", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce, Payload: payload,
	})
	if err != nil || response.Type != "ack" {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	streams, progress := agent.viewerTelemetry()
	if len(streams) != 1 || streams[0].StreamName != "yard-live" || streams[0].State != "playing" {
		t.Fatalf("streams=%+v", streams)
	}
	if progress == nil || progress.UnixMilli() != 2000 {
		t.Fatalf("renderer progress=%v", progress)
	}
	encoded, _ := json.Marshal(streams)
	if string(encoded) == "" || json.Valid(encoded) == false {
		t.Fatalf("invalid stored telemetry %s", encoded)
	}
	if containsSecret(string(encoded)) {
		t.Fatalf("unapproved telemetry leaked: %s", encoded)
	}
}

func TestRendererStatusAcceptsOnlyFixedStates(t *testing.T) {
	agent := pipeTestAgent(t)
	grant, _ := agent.handlePipeMessage(PipeMessage{Version: 1, RequestID: "b", Type: "bootstrap_request", PID: 42, SessionID: 3})
	_, _ = agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "v", Type: "viewer_register", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	})
	for _, state := range []string{"ready", "not_ready", "unresponsive", "failed"} {
		payload, _ := json.Marshal(map[string]string{"state": state})
		if _, err := agent.handlePipeMessage(PipeMessage{
			Version: 1, RequestID: "renderer-" + state, Type: "renderer_status", PID: 99, SessionID: 3,
			Generation: grant.Generation, Nonce: grant.Nonce, Payload: payload,
		}); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
	malicious := json.RawMessage(`{"state":"rtsp://admin:secret@camera/live"}`)
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "renderer-bad", Type: "renderer_status", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce, Payload: malicious,
	}); err == nil {
		t.Fatal("arbitrary renderer state was accepted")
	}
	state, err := agent.loadState()
	if err != nil || containsSecret(state.RendererState) {
		t.Fatalf("renderer state=%q err=%v", state.RendererState, err)
	}
}

func TestViewerCommandBrokerDeliversAndCompletesOneCommand(t *testing.T) {
	agent := pipeTestAgent(t)
	grant, _ := agent.handlePipeMessage(PipeMessage{Version: 1, RequestID: "b", Type: "bootstrap_request", PID: 42, SessionID: 3})
	_, _ = agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "v", Type: "viewer_register", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	})

	done := make(chan error, 1)
	go func() {
		done <- agent.executeViewerCommand(t.Context(), Command{Type: "resubscribe_stream", StreamName: "yard-live"}, "command-7")
	}()
	deadline := time.Now().Add(time.Second)
	var response PipeMessage
	for time.Now().Before(deadline) {
		response, _ = agent.handlePipeMessage(PipeMessage{
			Version: 1, RequestID: "h", Type: "viewer_heartbeat", PID: 99, SessionID: 3,
			Generation: grant.Generation, Nonce: grant.Nonce,
		})
		if response.Type == "command" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if response.Type != "command" {
		t.Fatal("Viewer command was not delivered")
	}
	duplicate, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "h-2", Type: "viewer_heartbeat", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	})
	if err != nil || duplicate.Type != "ack" {
		t.Fatalf("pending Viewer command was delivered twice: response=%+v err=%v", duplicate, err)
	}
	var commandPayload struct {
		Type         string `json:"type"`
		StreamName   string `json:"streamName"`
		OperationKey string `json:"operationKey"`
	}
	if err := json.Unmarshal(response.Payload, &commandPayload); err != nil || commandPayload.Type != "resubscribe_stream" || commandPayload.StreamName != "yard-live" || commandPayload.OperationKey != "command-7" {
		t.Fatalf("payload=%s decoded=%+v err=%v", response.Payload, commandPayload, err)
	}
	resultPayload := json.RawMessage(`{"operationKey":"command-7","succeeded":true}`)
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "r", Type: "command_result", PID: 99, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce, Payload: resultPayload,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Viewer command result did not complete")
	}
}

func TestViewerCommandBrokerStopsAfterBoundedDeadline(t *testing.T) {
	agent := pipeTestAgent(t)
	agent.ViewerCommandDeadline = 5 * time.Millisecond
	started := time.Now()
	err := agent.executeViewerCommand(t.Context(), Command{Type: "reload_live"}, "command-8")
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(started))
	}
}

func TestDefaultRestartViewerReachesAuthorizedNextReadyGeneration(t *testing.T) {
	agent := pipeTestAgent(t)
	agent.ViewerRestartDeadline = time.Second
	current := registerReadyViewer(t, agent, 42, 99)

	type commandResult struct {
		record CommandRecord
		err    error
	}
	done := make(chan commandResult, 1)
	go func() {
		record, err := agent.HandleCommand(t.Context(), Command{
			ID: 50, Type: "restart_viewer", PayloadHash: "restart-50", TTLSeconds: 300,
		})
		done <- commandResult{record: record, err: err}
	}()

	var shutdown PipeMessage
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		shutdown, _ = agent.handlePipeMessage(PipeMessage{
			Version: 1, RequestID: "restart-heartbeat", Type: "viewer_heartbeat", PID: 99, SessionID: 3,
			Generation: current.Generation, Nonce: current.Nonce,
		})
		if shutdown.Type == "command" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	var shutdownPayload struct {
		Type         string `json:"type"`
		OperationKey string `json:"operationKey"`
	}
	if err := json.Unmarshal(shutdown.Payload, &shutdownPayload); err != nil || shutdownPayload.Type != "shutdown" || shutdownPayload.OperationKey != "viewer-generation-2" {
		t.Fatalf("shutdown=%+v payload=%+v err=%v", shutdown, shutdownPayload, err)
	}
	result, _ := json.Marshal(map[string]any{"operationKey": shutdownPayload.OperationKey, "succeeded": true})
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "shutdown-result", Type: "command_result", PID: 99, SessionID: 3,
		Generation: current.Generation, Nonce: current.Nonce, Payload: result,
	}); err != nil {
		t.Fatal(err)
	}

	next, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "restart-bootstrap", Type: "bootstrap_request", PID: 43, SessionID: 3,
	})
	if err != nil || next.Generation != 2 {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "restart-viewer", Type: "viewer_register", PID: 100, SessionID: 3,
		Generation: next.Generation, Nonce: next.Nonce,
	}); err != nil {
		t.Fatal(err)
	}
	ready := json.RawMessage(`{"state":"ready"}`)
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "restart-ready", Type: "renderer_status", PID: 100, SessionID: 3,
		Generation: next.Generation, Nonce: next.Nonce, Payload: ready,
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.record.State != CommandSucceeded || result.record.Generation != 2 {
			t.Fatalf("record=%+v err=%v", result.record, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("restart_viewer did not observe the ready generation")
	}
}

func TestDefaultRestartViewerFailsBoundedWithoutReadyGeneration(t *testing.T) {
	agent := pipeTestAgent(t)
	agent.ViewerRestartDeadline = 50 * time.Millisecond
	current := registerReadyViewer(t, agent, 42, 99)
	if _, err := agent.updateState(func(state *MachineState) error {
		state.ExpectedViewerGeneration = current.Generation + 1
		state.ViewerState = "restart_authorized"
		state.ViewerNonce = ""
		state.ExpectedViewerPID = 0
		state.ExpectedViewerSession = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err := agent.executeViewerRestart(t.Context(), Command{Type: "restart_viewer", Generation: current.Generation + 1}, "viewer-generation-2")
	if err == nil {
		t.Fatal("restart without a ready next generation succeeded")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("restart failure was not bounded: %v", elapsed)
	}
	state, err := agent.loadState()
	if err != nil || state.ViewerState != "recovery_failed" || state.ExpectedViewerGeneration != state.ViewerGeneration {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestRestartViewerDuringAgentRecoveryUsesAuthorizedGenerationWithoutDeadShutdown(t *testing.T) {
	agent := pipeTestAgent(t)
	agent.ViewerRestartDeadline = 10 * time.Second
	if err := SaveMachineState(agent.Paths.State, MachineState{
		ViewerGeneration: 7, ExpectedViewerGeneration: 8, ViewerState: "restart_authorized", RendererState: "not_ready",
	}); err != nil {
		t.Fatal(err)
	}
	type commandResult struct {
		record CommandRecord
		err    error
	}
	commandCtx, cancelCommand := context.WithCancel(t.Context())
	done := make(chan commandResult, 1)
	commandFinished := false
	go func() {
		record, err := agent.HandleCommand(commandCtx, Command{ID: 52, Type: "restart_viewer", PayloadHash: "restart-52", TTLSeconds: 300})
		done <- commandResult{record: record, err: err}
	}()
	defer func() {
		cancelCommand()
		if commandFinished {
			return
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}()

	var grant PipeMessage
	deadline := time.Now().Add(5 * time.Second)
	running := false
	for time.Now().Before(deadline) {
		ledger, _ := agent.loadCommandLedger()
		if ledger.Records["52"].State == CommandRunning {
			running = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !running {
		t.Fatal("restart command did not enter running state")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		grant, _ = agent.handlePipeMessage(PipeMessage{Version: 1, RequestID: "recovery-bootstrap", Type: "bootstrap_request", PID: 43, SessionID: 3})
		if grant.Type == "bootstrap_grant" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if grant.Generation != 8 {
		t.Fatalf("recovery grant=%+v", grant)
	}
	if command := agent.pendingViewerCommand(); command != nil {
		t.Fatalf("dead Viewer received shutdown during Agent recovery: %s", command)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "recovery-register", Type: "viewer_register", PID: 100, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "recovery-ready", Type: "renderer_status", PID: 100, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce, Payload: json.RawMessage(`{"state":"ready"}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		commandFinished = true
		if result.err != nil || result.record.State != CommandSucceeded || result.record.Generation != 8 {
			t.Fatalf("record=%+v err=%v", result.record, result.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("restart command did not complete on the authorized recovery generation")
	}
}

func TestRestartViewerDuringAgentRecoveryFailsBoundedWithoutReadyGeneration(t *testing.T) {
	agent := pipeTestAgent(t)
	agent.ViewerRestartDeadline = 50 * time.Millisecond
	if err := SaveMachineState(agent.Paths.State, MachineState{
		ViewerGeneration: 7, ExpectedViewerGeneration: 8, ViewerState: "restart_authorized", RendererState: "not_ready",
	}); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	record, err := agent.HandleCommand(t.Context(), Command{ID: 53, Type: "restart_viewer", PayloadHash: "restart-53", TTLSeconds: 300})
	if err == nil || record.State != CommandFailed || record.Generation != 8 {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("recovery command exceeded its deadline: %v", time.Since(started))
	}
	if command := agent.pendingViewerCommand(); command != nil {
		t.Fatalf("dead Viewer received shutdown during failed recovery: %s", command)
	}
}

func registerReadyViewer(t *testing.T, agent *Agent, bootstrapPID, viewerPID int) PipeMessage {
	t.Helper()
	grant, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "initial-bootstrap", Type: "bootstrap_request", PID: bootstrapPID, SessionID: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "initial-viewer", Type: "viewer_register", PID: viewerPID, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce,
	}); err != nil {
		t.Fatal(err)
	}
	ready := json.RawMessage(`{"state":"ready"}`)
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "initial-ready", Type: "renderer_status", PID: viewerPID, SessionID: 3,
		Generation: grant.Generation, Nonce: grant.Nonce, Payload: ready,
	}); err != nil {
		t.Fatal(err)
	}
	return grant
}

func pipeTestAgent(t *testing.T) *Agent {
	t.Helper()
	dir := t.TempDir()
	agent := NewAgent(Config{
		SchemaVersion: SchemaVersion, ClientID: "client-1", DisplayName: "Viewer", ServerURL: "http://10.0.0.5:18080", InstallDir: filepath.Join(dir, "install"),
	}, MachinePaths{
		State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json"),
	})
	return &agent
}

func containsSecret(value string) bool {
	for _, secret := range []string{"rtsp", "secret", "camera/live", "url"} {
		if len(value) >= len(secret) {
			for index := 0; index+len(secret) <= len(value); index++ {
				if value[index:index+len(secret)] == secret {
					return true
				}
			}
		}
	}
	return false
}
