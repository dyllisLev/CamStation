package vieweragent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type executorFunc func(context.Context, Command, string) error

func (f executorFunc) Execute(ctx context.Context, command Command, operationKey string) error {
	return f(ctx, command, operationKey)
}

type reporterFunc func(context.Context, Command, CommandState, string, string) error

func (f reporterFunc) Report(ctx context.Context, command Command, state CommandState, operationKey, commandError string) error {
	return f(ctx, command, state, operationKey, commandError)
}

func serveTestViewerPipe(ctx context.Context, _ Config, _ func(PipeMessage) (PipeMessage, error), ready func()) error {
	ready()
	<-ctx.Done()
	return nil
}

func TestCommandRunningIntentIsDurableBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	var calls int
	agent := Agent{Paths: paths, Now: func() time.Time { return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC) }}
	agent.Executor = executorFunc(func(_ context.Context, command Command, operationKey string) error {
		calls++
		ledger, err := LoadCommandLedger(paths.Commands)
		if err != nil {
			return err
		}
		record := ledger.Records[command.Key()]
		if record.State != CommandRunning || record.PayloadHash != "payload-hash" || record.RunningAt == nil || operationKey == "" {
			t.Fatalf("intent not durable before execution: %+v operationKey=%q", record, operationKey)
		}
		return nil
	})

	command := Command{ID: 11, Type: "ping", PayloadHash: "payload-hash", TTLSeconds: 300, CreatedAt: agent.Now()}
	result, err := agent.HandleCommand(t.Context(), command)
	if err != nil || result.State != CommandSucceeded {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := agent.HandleCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("duplicate command executed %d times", calls)
	}
}

func TestDuplicateCommandReplaysTerminalResult(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	var reported []CommandState
	agent := Agent{Paths: paths, Reporter: reporterFunc(func(_ context.Context, _ Command, state CommandState, _, _ string) error {
		reported = append(reported, state)
		return nil
	})}
	command := Command{ID: 12, Type: "ping", PayloadHash: "payload-hash", TTLSeconds: 300}
	if _, err := agent.HandleCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	reported = nil
	if _, err := agent.HandleCommand(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0] != CommandSucceeded {
		t.Fatalf("duplicate result was not replayed: %v", reported)
	}
}

func TestCommandErrorsAreReducedToSafeCategories(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	agent := Agent{Paths: paths, Executor: executorFunc(func(context.Context, Command, string) error {
		return errors.New(`failed at C:\secret\viewer.exe with http://camera.local/private`)
	})}
	result, err := agent.HandleCommand(t.Context(), Command{ID: 13, Type: "reload_live", PayloadHash: "payload-hash", TTLSeconds: 300})
	if err == nil || result.Error != "execution_failed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRestartGenerationReconcilesWithoutRepeatingSideEffect(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	if err := SaveMachineState(paths.State, MachineState{ViewerGeneration: 8}); err != nil {
		t.Fatal(err)
	}
	ledger := CommandLedger{Records: map[string]CommandRecord{"21": {ID: 21, Type: "restart_viewer", State: CommandRunning, PayloadHash: "h", OperationKey: "viewer-generation-8", Generation: 8}}}
	if err := SaveCommandLedger(paths.Commands, ledger); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths, Now: func() time.Time { return now }, Executor: executorFunc(func(context.Context, Command, string) error {
		t.Fatal("reconciled restart executed twice")
		return nil
	})}
	results, err := agent.Reconcile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].State != CommandSucceeded {
		t.Fatalf("unexpected reconcile result: %+v", results)
	}
}

func TestIncompleteRestartReconcilesTowardSameGeneration(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{ViewerGeneration: 7, ExpectedViewerGeneration: 8}); err != nil {
		t.Fatal(err)
	}
	ledger := CommandLedger{Records: map[string]CommandRecord{"22": {ID: 22, Type: "restart_viewer", State: CommandRunning, PayloadHash: "h", OperationKey: "viewer-generation-8", Generation: 8}}}
	if err := SaveCommandLedger(paths.Commands, ledger); err != nil {
		t.Fatal(err)
	}
	var calls int
	agent := Agent{Paths: paths, Executor: executorFunc(func(_ context.Context, command Command, operationKey string) error {
		calls++
		if command.ID != 22 || operationKey != "viewer-generation-8" {
			t.Fatalf("unexpected reconciliation: %+v key=%q", command, operationKey)
		}
		return nil
	})}
	results, err := agent.Reconcile(t.Context())
	if err != nil || calls != 1 || len(results) != 1 || results[0].State != CommandSucceeded {
		t.Fatalf("calls=%d results=%+v err=%v", calls, results, err)
	}
}

func TestExpiredRunningRestartIsDurablyExpiredWithoutExecution(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	ledger := CommandLedger{Records: map[string]CommandRecord{"24": {
		ID: 24, Type: "restart_viewer", State: CommandRunning, PayloadHash: "expired",
		OperationKey: "viewer-generation-9", Generation: 9, CreatedAt: now.Add(-2 * time.Minute), TTLSeconds: 60,
	}}}
	if err := SaveCommandLedger(paths.Commands, ledger); err != nil {
		t.Fatal(err)
	}
	var reported []CommandState
	agent := Agent{
		Paths: paths,
		Now:   func() time.Time { return now },
		Executor: executorFunc(func(context.Context, Command, string) error {
			t.Fatal("expired restart executed")
			return nil
		}),
		Reporter: reporterFunc(func(_ context.Context, _ Command, state CommandState, _, _ string) error {
			reported = append(reported, state)
			return nil
		}),
	}
	results, err := agent.Reconcile(t.Context())
	if err != nil || len(results) != 1 || results[0].State != CommandExpired {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	persisted, err := LoadCommandLedger(paths.Commands)
	if err != nil || persisted.Records["24"].State != CommandExpired {
		t.Fatalf("persisted=%+v err=%v", persisted.Records["24"], err)
	}
	if len(reported) != 1 || reported[0] != CommandExpired {
		t.Fatalf("reported=%v", reported)
	}
}

func TestReceivedRestartResumesItsPersistedGeneration(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	if err := SaveMachineState(paths.State, MachineState{ExpectedViewerGeneration: 1, ForcedViewerRestartID: "23", ForcedViewerRestartAt: &now}); err != nil {
		t.Fatal(err)
	}
	ledger := CommandLedger{Records: map[string]CommandRecord{"23": {ID: 23, Type: "restart_viewer", State: CommandReceived, PayloadHash: "h", ReceivedAt: now}}}
	if err := SaveCommandLedger(paths.Commands, ledger); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths, Now: func() time.Time { return now }, Executor: executorFunc(func(context.Context, Command, string) error { return nil })}
	result, err := agent.HandleCommand(t.Context(), Command{ID: 23, Type: "restart_viewer", PayloadHash: "h", TTLSeconds: 300})
	if err != nil || result.State != CommandSucceeded || result.Generation != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAgentRestartReconcilesOnNextBoot(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{AgentBootGeneration: 3}); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths}
	command := Command{ID: 31, Type: "restart_agent", PayloadHash: "restart-hash", TTLSeconds: 300}
	record, err := agent.HandleCommand(t.Context(), command)
	if !errors.Is(err, ErrAgentRestartRequested) || record.State != CommandRunning || record.Generation != 4 {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	if err := SaveMachineState(paths.State, MachineState{AgentBootGeneration: 4}); err != nil {
		t.Fatal(err)
	}
	results, err := agent.Reconcile(t.Context())
	if err != nil || len(results) != 1 || results[0].State != CommandSucceeded {
		t.Fatalf("results=%+v err=%v", results, err)
	}
}

func TestAgentStartupConvergesStaleViewerIdentityOnce(t *testing.T) {
	tests := []struct {
		name         string
		state        MachineState
		wantState    string
		wantExpected int64
		wantRenderer string
	}{
		{
			name: "running Viewer from prior Agent boot",
			state: MachineState{ViewerGeneration: 7, ExpectedViewerGeneration: 7, ViewerState: "running", RendererState: "ready",
				ViewerNonce: "old", ExpectedViewerPID: 99, ExpectedViewerSession: 3},
			wantState: "restart_authorized", wantExpected: 8, wantRenderer: "not_ready",
		},
		{
			name: "starting Viewer preserves higher expected generation",
			state: MachineState{ViewerGeneration: 7, ExpectedViewerGeneration: 9, ViewerState: "starting", RendererState: "not_ready",
				ViewerNonce: "old", ExpectedViewerPID: 99, ExpectedViewerSession: 3},
			wantState: "restart_authorized", wantExpected: 9, wantRenderer: "not_ready",
		},
		{
			name: "old identity converges regardless of stale label",
			state: MachineState{ViewerGeneration: 7, ExpectedViewerGeneration: 7, ViewerState: "failed", RendererState: "failed",
				ViewerNonce: "old", ExpectedViewerPID: 99, ExpectedViewerSession: 3},
			wantState: "restart_authorized", wantExpected: 8, wantRenderer: "not_ready",
		},
		{
			name:      "true initial no Viewer state is unchanged",
			state:     MachineState{ViewerState: "not_logged_in", RendererState: "not_ready"},
			wantState: "not_logged_in", wantExpected: 0, wantRenderer: "not_ready",
		},
		{
			name:      "already authorized generation is not incremented again",
			state:     MachineState{ViewerGeneration: 7, ExpectedViewerGeneration: 9, ViewerState: "restart_authorized", RendererState: "not_ready"},
			wantState: "restart_authorized", wantExpected: 9, wantRenderer: "not_ready",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
			if err := SaveMachineState(paths.State, tt.state); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			agent := NewAgent(Config{ClientID: "startup-client", ServerURL: "http://127.0.0.1:1", DisplayName: "Viewer", InstallDir: dir}, paths)
			agent.ServePipe = serveTestViewerPipe
			agent.Ready = cancel
			if err := agent.Run(ctx); err != nil {
				t.Fatal(err)
			}
			state, err := LoadMachineState(paths.State)
			if err != nil {
				t.Fatal(err)
			}
			if state.ViewerState != tt.wantState || state.ExpectedViewerGeneration != tt.wantExpected || state.RendererState != tt.wantRenderer {
				t.Fatalf("state=%+v", state)
			}
			if state.ViewerNonce != "" || state.ExpectedViewerPID != 0 || state.ExpectedViewerSession != 0 {
				t.Fatalf("stale Viewer identity survived Agent startup: %+v", state)
			}
		})
	}
}

func TestQuarantinedUpdateIsRejectedWithoutExecution(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	digest := strings.Repeat("a", 64)
	journal := UpdateJournal{}
	journal.Quarantine("2.0.0", digest, 4, time.Now(), "rollback")
	if err := SaveUpdateJournal(paths.Update, journal); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths, Executor: executorFunc(func(context.Context, Command, string) error {
		t.Fatal("quarantined update executed")
		return nil
	})}
	result, err := agent.HandleCommand(t.Context(), Command{ID: 4, Type: "update_app", DesiredVersion: "2.0.0", ArtifactSHA256: digest, Generation: 4, PayloadHash: "p", TTLSeconds: 300})
	if err == nil || result.State != CommandRejected {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestAgentHeartbeatContinuesWithoutViewerIPC(t *testing.T) {
	var heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/viewers/heartbeat":
			heartbeats.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"viewer":{"id":"client-hb"},"desiredRelease":null}`))
		case r.URL.Path == "/api/viewers/client-hb/control":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	config := Config{SchemaVersion: SchemaVersion, ServerURL: server.URL, DisplayName: "No Login", InstallDir: dir, ClientID: "client-hb"}
	agent := NewAgent(config, MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")})
	agent.HTTPClient = server.Client()
	agent.ServePipe = serveTestViewerPipe
	agent.HeartbeatInterval = 10 * time.Millisecond
	agent.ControlReadDeadline = time.Second
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	deadline := time.After(2 * time.Second)
	for heartbeats.Load() < 3 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("heartbeats stopped without Viewer IPC: %d", heartbeats.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	_ = <-done
	if heartbeats.Load() < 3 {
		t.Fatalf("heartbeats stopped without Viewer IPC: %d", heartbeats.Load())
	}
}

func TestViewerRecoveryCauseDistinguishesIPCAndRendererHealthFromControl(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	freshViewer := now.Add(-5 * time.Second)
	freshRenderer := now.Add(-4 * time.Second)
	state := MachineState{
		ViewerState: "running", ViewerLastHeartbeatAt: &freshViewer,
		RendererState: "ready", RendererLastHeartbeatAt: &freshRenderer,
		ControlState: "control_degraded",
	}
	if cause := viewerRecoveryCause(state, now); cause != "" {
		t.Fatalf("control loss triggered Viewer recovery: %q", cause)
	}
	state.RendererState = "not_ready"
	if cause := viewerRecoveryCause(state, now); cause != "" {
		t.Fatalf("fresh responsive renderer triggered recovery before its next pulse: %q", cause)
	}
	state.RendererState = "ready"

	staleViewer := now.Add(-15 * time.Second)
	state.ViewerLastHeartbeatAt = &staleViewer
	if cause := viewerRecoveryCause(state, now); cause != "ipc_stale" {
		t.Fatalf("IPC cause=%q", cause)
	}

	state.ViewerLastHeartbeatAt = &freshViewer
	staleRenderer := now.Add(-15 * time.Second)
	state.RendererLastHeartbeatAt = &staleRenderer
	if cause := viewerRecoveryCause(state, now); cause != "renderer_stale" {
		t.Fatalf("renderer pulse cause=%q", cause)
	}

	state.RendererState = "unresponsive"
	if cause := viewerRecoveryCause(state, now); cause != "renderer_unresponsive" {
		t.Fatalf("unresponsive cause=%q", cause)
	}
	state.RendererState = "failed"
	if cause := viewerRecoveryCause(state, now); cause != "renderer_failed" {
		t.Fatalf("failed cause=%q", cause)
	}
}

func TestAutomaticViewerRecoveryIsSingleFlightAndKeepsAgentHeartbeatRunning(t *testing.T) {
	var heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/heartbeat" {
			heartbeats.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	agent := pipeTestAgent(t)
	agent.Config.ServerURL = server.URL
	agent.HTTPClient = server.Client()
	agent.HeartbeatInterval = 5 * time.Millisecond
	agent.ViewerRestartDeadline = time.Second
	current := registerReadyViewer(t, agent, 42, 99)
	if _, err := agent.handlePipeMessage(PipeMessage{
		Version: 1, RequestID: "renderer-hung", Type: "renderer_status", PID: 99, SessionID: 3,
		Generation: current.Generation, Nonce: current.Nonce,
		Payload: json.RawMessage(`{"state":"unresponsive","source":"host"}`),
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	heartbeatDone := make(chan struct{})
	go func() {
		agent.runHeartbeats(ctx, ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: agent.Config.ClientID})
		close(heartbeatDone)
	}()
	recoveryDone := make(chan error, 1)
	go func() { recoveryDone <- agent.recoverViewerIfUnhealthy(ctx) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err := agent.loadState()
		if err == nil && state.ViewerState == "restart_authorized" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- agent.recoverViewerIfUnhealthy(ctx) }()
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second automatic recovery did not observe the in-flight recovery")
	}
	for heartbeats.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if heartbeats.Load() < 3 {
		t.Fatalf("Agent heartbeat stopped during Viewer recovery: %d", heartbeats.Load())
	}
	state, err := agent.loadState()
	if err != nil || len(state.ViewerRestartHistory) != 1 {
		t.Fatalf("restart history=%v err=%v", state.ViewerRestartHistory, err)
	}
	cancel()
	<-heartbeatDone
	<-recoveryDone
}

func TestAutomaticViewerRecoveryPersistsCooldownAndHourlyBudgetExhaustion(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		history []time.Time
	}{
		{name: "ten minute cooldown", history: []time.Time{now.Add(-5 * time.Minute)}},
		{name: "three per hour", history: []time.Time{now.Add(-50 * time.Minute), now.Add(-30 * time.Minute), now.Add(-11 * time.Minute)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
			if err := SaveMachineState(paths.State, MachineState{
				ViewerState: "running", RendererState: "unresponsive", ViewerRestartHistory: test.history,
			}); err != nil {
				t.Fatal(err)
			}
			agent := NewAgent(Config{ClientID: "budget-client"}, paths)
			agent.Now = func() time.Time { return now }
			if err := agent.recoverViewerIfUnhealthy(t.Context()); err != nil {
				t.Fatal(err)
			}
			persisted, err := LoadMachineState(paths.State)
			if err != nil || persisted.ViewerState != "recovery_failed" || len(persisted.ViewerRestartHistory) != len(test.history) {
				t.Fatalf("persisted=%+v err=%v", persisted, err)
			}
		})
	}
}

func TestUnsupportedViewerCommandsAreNotInAgentAllowlist(t *testing.T) {
	for _, commandType := range []string{"restart_stream", "capture_diagnostics"} {
		if supportedCommand(commandType) {
			t.Fatalf("unsupported command %q is advertised", commandType)
		}
	}
}

func TestRestartAndUpdateSideEffectsShareOneProductionBoundary(t *testing.T) {
	agent := Agent{}
	started := make(chan string, 2)
	release := make(chan struct{})
	executor := executorFunc(func(_ context.Context, command Command, _ string) error {
		started <- command.Type
		<-release
		return nil
	})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- agent.executeCommand(t.Context(), executor, Command{Type: "restart_viewer"}, "viewer-generation-2")
	}()
	if first := <-started; first != "restart_viewer" {
		t.Fatalf("first side effect=%q", first)
	}
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- agent.executeCommand(t.Context(), executor, Command{Type: "update_app"}, "update-2")
	}()
	select {
	case second := <-started:
		t.Fatalf("concurrent side effect started: %q", second)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if second := <-started; second != "update_app" {
		t.Fatalf("second side effect=%q", second)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestPipeFailureMarksViewerFailedWithoutChangingControlState(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{ControlState: "online", ViewerState: "running"}); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths}
	if err := agent.markPipeFailure(); err != nil {
		t.Fatal(err)
	}
	state, err := LoadMachineState(paths.State)
	if err != nil || state.ViewerState != "failed" || state.ControlState != "online" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestCorruptLedgerFailsStartupBeforeAnyOnlineHeartbeat(t *testing.T) {
	var heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/heartbeat" {
			heartbeats.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := os.WriteFile(paths.Commands, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{SchemaVersion: SchemaVersion, ServerURL: server.URL, DisplayName: "Broken", InstallDir: dir, ClientID: "broken-ledger"}
	agent := NewAgent(config, paths)
	agent.HTTPClient = server.Client()
	agent.ServePipe = serveTestViewerPipe
	readyCalls := 0
	agent.Ready = func() { readyCalls++ }
	err := agent.Run(t.Context())
	if !errors.Is(err, ErrCommandEngine) || heartbeats.Load() != 0 || readyCalls != 0 {
		t.Fatalf("err=%v heartbeats=%d ready=%d", err, heartbeats.Load(), readyCalls)
	}
}

func TestAgentSignalsReadyAfterCommandEngineHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/control") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	config := Config{SchemaVersion: SchemaVersion, ServerURL: server.URL, DisplayName: "Ready", InstallDir: dir, ClientID: "ready-agent"}
	agent := NewAgent(config, paths)
	agent.HTTPClient = server.Client()
	agent.ServePipe = serveTestViewerPipe
	ctx, cancel := context.WithCancel(t.Context())
	ready := make(chan struct{})
	agent.Ready = func() {
		close(ready)
		cancel()
	}
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("Agent never signaled readiness")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentStartsViewerPipeBeforeReconcilingDurableRestart(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{
		ViewerGeneration: 7, ExpectedViewerGeneration: 8, ViewerState: "running", RendererState: "ready",
		ViewerNonce: "stale", ExpectedViewerPID: 99, ExpectedViewerSession: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"61": {
		ID: 61, Type: "restart_viewer", PayloadHash: "restart-61", OperationKey: "viewer-generation-8",
		Generation: 8, State: CommandRunning,
	}}}); err != nil {
		t.Fatal(err)
	}

	pipeHandlers := make(chan func(PipeMessage) (PipeMessage, error))
	pipeStopped := make(chan struct{})
	bootstrapDone := make(chan error, 1)
	go func() {
		handler := <-pipeHandlers
		grant, err := handler(PipeMessage{Version: 1, RequestID: "recovery-bootstrap", Type: "bootstrap_request", PID: 43, SessionID: 3})
		if err == nil && grant.Generation != 8 {
			err = errors.New("bootstrap did not preserve generation 8")
		}
		if err == nil {
			_, err = handler(PipeMessage{Version: 1, RequestID: "recovery-register", Type: "viewer_register", PID: 100, SessionID: 3,
				Generation: grant.Generation, Nonce: grant.Nonce})
		}
		if err == nil {
			_, err = handler(PipeMessage{Version: 1, RequestID: "recovery-ready", Type: "renderer_status", PID: 100, SessionID: 3,
				Generation: grant.Generation, Nonce: grant.Nonce, Payload: json.RawMessage(`{"state":"ready","source":"renderer"}`)})
		}
		bootstrapDone <- err
	}()

	config := Config{SchemaVersion: SchemaVersion, ServerURL: "http://127.0.0.1:1", DisplayName: "Recovery", InstallDir: dir, ClientID: "recovery-agent"}
	agent := NewAgent(config, paths)
	agent.ViewerRestartDeadline = time.Second
	agent.ServePipe = func(ctx context.Context, _ Config, handler func(PipeMessage) (PipeMessage, error), ready func()) error {
		ready()
		pipeHandlers <- handler
		<-ctx.Done()
		close(pipeStopped)
		return nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	var readyCalls atomic.Int32
	agent.Ready = func() {
		readyCalls.Add(1)
		cancel()
	}
	if err := agent.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-bootstrapDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-pipeStopped:
	default:
		t.Fatal("early Viewer pipe was not joined on Agent shutdown")
	}
	ledger, err := LoadCommandLedger(paths.Commands)
	if err != nil || ledger.Records["61"].State != CommandSucceeded || ledger.Records["61"].Generation != 8 {
		t.Fatalf("record=%+v err=%v", ledger.Records["61"], err)
	}
	state, err := LoadMachineState(paths.State)
	if err != nil || state.ViewerGeneration != 8 || state.ExpectedViewerGeneration != 8 || state.RendererState != "ready" || !state.CommandEngineHealthy {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if readyCalls.Load() != 1 {
		t.Fatalf("ready calls=%d", readyCalls.Load())
	}
}

func TestAgentHeartbeatRunsWhileDurableViewerRecoveryIsInFlight(t *testing.T) {
	var heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/viewers/heartbeat" {
			heartbeats.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{
		ViewerGeneration: 7, ExpectedViewerGeneration: 8, ViewerState: "restart_authorized", RendererState: "not_ready",
	}); err != nil {
		t.Fatal(err)
	}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{"62": {
		ID: 62, Type: "restart_viewer", PayloadHash: "restart-62", OperationKey: "viewer-generation-8",
		Generation: 8, State: CommandRunning,
	}}}); err != nil {
		t.Fatal(err)
	}
	agent := NewAgent(Config{ClientID: "recovery-heartbeat", ServerURL: server.URL, InstallDir: dir}, paths)
	agent.HTTPClient = server.Client()
	agent.ServePipe = serveTestViewerPipe
	agent.HeartbeatInterval = 5 * time.Millisecond
	agent.ViewerRestartDeadline = time.Second
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	deadline := time.Now().Add(500 * time.Millisecond)
	for heartbeats.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if heartbeats.Load() < 3 {
		cancel()
		<-done
		t.Fatalf("heartbeats began only after Viewer recovery: %d", heartbeats.Load())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAgentPipeBindFailureAbortsStartupBeforeReadyOrHeartbeat(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	agent := NewAgent(Config{SchemaVersion: SchemaVersion, ServerURL: server.URL, DisplayName: "Bind failure", InstallDir: dir, ClientID: "bind-failure"}, paths)
	agent.HTTPClient = server.Client()
	agent.ServePipe = func(context.Context, Config, func(PipeMessage) (PipeMessage, error), func()) error {
		return errors.New("pipe bind failed")
	}
	var readyCalls atomic.Int32
	agent.Ready = func() { readyCalls.Add(1) }
	started := time.Now()
	err := agent.Run(t.Context())
	if err == nil || !strings.Contains(err.Error(), "pipe bind failed") || time.Since(started) > time.Second {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(started))
	}
	if readyCalls.Load() != 0 || requests.Load() != 0 {
		t.Fatalf("ready=%d requests=%d", readyCalls.Load(), requests.Load())
	}
}

func TestAgentStartupFailureCancelsAndJoinsEarlyViewerPipe(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*Agent, MachinePaths) error
	}{
		{name: "reconcile failure", setup: func(_ *Agent, paths MachinePaths) error {
			return os.WriteFile(paths.Commands, []byte("not-json"), 0o600)
		}},
		{name: "command engine health failure", setup: func(agent *Agent, _ MachinePaths) error {
			agent.LoadLedger = func(string) (CommandLedger, error) {
				return CommandLedger{SchemaVersion: SchemaVersion, Records: map[string]CommandRecord{}}, nil
			}
			agent.SaveLedger = func(string, CommandLedger) error { return errors.New("disk full") }
			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			dir := t.TempDir()
			paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
			agent := NewAgent(Config{SchemaVersion: SchemaVersion, ServerURL: server.URL, DisplayName: "Startup failure", InstallDir: dir, ClientID: "startup-failure"}, paths)
			agent.HTTPClient = server.Client()
			if err := test.setup(&agent, paths); err != nil {
				t.Fatal(err)
			}
			pipeStopped := make(chan struct{})
			agent.ServePipe = func(ctx context.Context, _ Config, _ func(PipeMessage) (PipeMessage, error), ready func()) error {
				ready()
				<-ctx.Done()
				close(pipeStopped)
				return nil
			}
			var readyCalls atomic.Int32
			agent.Ready = func() { readyCalls.Add(1) }
			if err := agent.Run(t.Context()); err == nil {
				t.Fatal("startup failure was reported as success")
			}
			select {
			case <-pipeStopped:
			default:
				t.Fatal("early Viewer pipe was not joined after startup failure")
			}
			if readyCalls.Load() != 0 || requests.Load() != 0 {
				t.Fatalf("ready=%d requests=%d", readyCalls.Load(), requests.Load())
			}
		})
	}
}

func TestAgentReturnsLocalControlStateFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/control") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(": keepalive\n\n"))
			w.(http.Flusher).Flush()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	config := Config{SchemaVersion: SchemaVersion, ServerURL: server.URL, DisplayName: "Broken state", InstallDir: dir, ClientID: "broken-state"}
	agent := NewAgent(config, paths)
	agent.HTTPClient = server.Client()
	agent.ServePipe = serveTestViewerPipe
	agent.Ready = func() {
		if err := os.WriteFile(paths.State, []byte("not-json"), 0o600); err != nil {
			t.Error(err)
		}
	}
	if err := agent.Run(t.Context()); err == nil {
		t.Fatal("local control state failure was reported as a clean Agent exit")
	}
}

func TestLedgerReadAndWriteFailuresDegradeCommandEngine(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*Agent, MachinePaths) error
	}{
		{name: "corrupt read", setup: func(_ *Agent, paths MachinePaths) error {
			return os.WriteFile(paths.Commands, []byte("not-json"), 0o600)
		}},
		{name: "injected write", setup: func(agent *Agent, _ MachinePaths) error {
			agent.LoadLedger = func(string) (CommandLedger, error) {
				return CommandLedger{SchemaVersion: SchemaVersion, Records: map[string]CommandRecord{}}, nil
			}
			agent.SaveLedger = func(string, CommandLedger) error { return errors.New("disk full") }
			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
			if err := SaveMachineState(paths.State, MachineState{ControlState: "online", CommandEngineHealthy: true}); err != nil {
				t.Fatal(err)
			}
			agent := Agent{Paths: paths}
			if err := test.setup(&agent, paths); err != nil {
				t.Fatal(err)
			}
			_, err := agent.HandleCommand(t.Context(), Command{ID: 90, Type: "ping", PayloadHash: "hash", TTLSeconds: 300})
			if !errors.Is(err, ErrCommandEngine) {
				t.Fatalf("err=%v", err)
			}
			state, err := LoadMachineState(paths.State)
			if err != nil || state.CommandEngineHealthy || state.ControlState != "control_degraded" {
				t.Fatalf("state=%+v err=%v", state, err)
			}
		})
	}
}

func TestTransportSuccessCannotMaskBrokenCommandEngineInHeartbeat(t *testing.T) {
	heartbeat := make(chan HeartbeatPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload HeartbeatPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		heartbeat <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{ControlState: "control_degraded", CommandEngineHealthy: false}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Commands, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := Config{SchemaVersion: SchemaVersion, ServerURL: server.URL, DisplayName: "Broken", InstallDir: dir, ClientID: "broken-control"}
	agent := NewAgent(config, paths)
	agent.HTTPClient = server.Client()
	if err := agent.applyControlResult(ControlResult{Transport: ControlTransportSSE, Proven: true}); err == nil {
		t.Fatal("broken command engine passed health check")
	}
	agent.sendHeartbeat(t.Context(), ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: config.ClientID})
	if got := <-heartbeat; got.Control.State != "control_degraded" {
		t.Fatalf("heartbeat control state=%q", got.Control.State)
	}
}

func TestHeartbeatReportsDurableViewerRecoveryFailure(t *testing.T) {
	heartbeat := make(chan HeartbeatPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload HeartbeatPayload
		_ = json.NewDecoder(r.Body).Decode(&payload)
		heartbeat <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{ViewerState: "recovery_failed", RendererState: "failed"}); err != nil {
		t.Fatal(err)
	}
	agent := NewAgent(Config{ClientID: "failed-recovery", ServerURL: server.URL}, paths)
	agent.sendHeartbeat(t.Context(), ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: agent.Config.ClientID})
	if got := <-heartbeat; got.Agent.State != "recovery_failed" || got.Viewer.State != "recovery_failed" {
		t.Fatalf("heartbeat Agent=%q Viewer=%q", got.Agent.State, got.Viewer.State)
	}
}

func TestCommandFrameStaysDegradedUntilDurableProcessingSucceeds(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{ControlState: "online", CommandEngineHealthy: true}); err != nil {
		t.Fatal(err)
	}
	agent := Agent{
		Paths: paths,
		LoadLedger: func(string) (CommandLedger, error) {
			return CommandLedger{SchemaVersion: SchemaVersion, Records: map[string]CommandRecord{}}, nil
		},
		SaveLedger: func(string, CommandLedger) error { return errors.New("disk full") },
	}
	command := Command{ID: 91, Type: "ping", PayloadHash: "hash", TTLSeconds: 300}
	err := agent.handleControlResult(t.Context(), ControlResult{Transport: ControlTransportSSE, Proven: true, Command: &command})
	if !errors.Is(err, ErrCommandEngine) {
		t.Fatalf("err=%v", err)
	}
	state, err := LoadMachineState(paths.State)
	if err != nil || state.ControlState != "control_degraded" || state.CommandEngineHealthy {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}
