package vieweragent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestQuarantinedUpdateIsRejectedWithoutExecution(t *testing.T) {
	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	journal := UpdateJournal{}
	journal.Quarantine("2.0.0", "bad", 4, time.Now(), "rollback")
	if err := SaveUpdateJournal(paths.Update, journal); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Paths: paths, Executor: executorFunc(func(context.Context, Command, string) error {
		t.Fatal("quarantined update executed")
		return nil
	})}
	result, err := agent.HandleCommand(t.Context(), Command{ID: 4, Type: "update_app", DesiredVersion: "2.0.0", ArtifactSHA256: "bad", Generation: 4, PayloadHash: "p", TTLSeconds: 300})
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
	agent.HeartbeatInterval = 10 * time.Millisecond
	agent.ControlReadDeadline = time.Second
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Millisecond)
	defer cancel()
	_ = agent.Run(ctx)
	if heartbeats.Load() < 3 {
		t.Fatalf("heartbeats stopped without Viewer IPC: %d", heartbeats.Load())
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
