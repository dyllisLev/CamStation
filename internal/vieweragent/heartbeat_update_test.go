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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"camstation/internal/viewerinstall"
)

func TestHeartbeatDesiredCommandWaitsUntilStartupReconcileOwnsNoLedgerState(t *testing.T) {
	var calls atomic.Int32
	gate := controlCommandGate{dispatch: func(ControlResult) error {
		calls.Add(1)
		return nil
	}}
	result := ControlResult{Command: &Command{ID: 1, Type: "update_app"}}
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := gate.dispatchWhenReady(result); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if calls.Load() != 0 {
		t.Fatalf("desired commands reached ledger during startup reconcile: %d", calls.Load())
	}
	gate.open()
	if err := gate.dispatchWhenReady(result); err != nil || calls.Load() != 1 {
		t.Fatalf("post-reconcile dispatch calls=%d err=%v", calls.Load(), err)
	}
}

func TestHeartbeatReportsInstalledReleaseAndDispatchesDesiredUpdateWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	stateDir := filepath.Join(root, "state")
	digest := strings.Repeat("a", 64)
	payloadHash := strings.Repeat("b", 64)
	writeCurrentReleaseFixture(t, installDir, "2.4.0", digest)
	paths := MachinePaths{State: filepath.Join(stateDir, "state.json"), Commands: filepath.Join(stateDir, "commands.json"), Update: filepath.Join(stateDir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{ViewerState: "running", RendererState: "ready"}); err != nil {
		t.Fatal(err)
	}
	payloads := make(chan HeartbeatPayload, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var heartbeat HeartbeatPayload
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			t.Error(err)
			return
		}
		payloads <- heartbeat
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{DesiredRelease: &HeartbeatDesiredUpdate{
			Version: "2.5.0", SHA256: strings.Repeat("c", 64), CommandID: 51,
			PayloadHash: payloadHash, Generation: 8, TTLSeconds: 300, CreatedAt: time.Now().UTC(),
		}})
	}))
	defer server.Close()
	started := make(chan struct{})
	var calls atomic.Int32
	agent := Agent{
		Config: Config{ClientID: "viewer-heartbeat-update", DisplayName: "Wall", ServerURL: server.URL, InstallDir: installDir},
		Paths:  paths, HTTPClient: server.Client(), AgentVersion: "embedded-build-version",
		Updater: updateExecutorFunc(func(ctx context.Context, target UpdateTarget) error {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-ctx.Done()
			return ctx.Err()
		}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	dispatcher := newControlCommandDispatcher(ctx, cancel, &agent)
	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: agent.Config.ClientID}
	begin := time.Now()
	agent.sendHeartbeat(ctx, client, dispatcher.dispatch)
	agent.sendHeartbeat(ctx, client, dispatcher.dispatch)
	if time.Since(begin) > 500*time.Millisecond {
		t.Fatal("heartbeat blocked behind update execution")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("heartbeat desired update was not dispatched")
	}
	first := <-payloads
	if first.AppVersion != "2.4.0" || first.Agent.Version != "2.4.0" || first.Agent.ArtifactSHA256 != digest {
		t.Fatalf("heartbeat installed identity=%#v appVersion=%q", first.Agent, first.AppVersion)
	}
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("duplicate heartbeat update executions=%d", calls.Load())
	}
	cancel()
	_ = dispatcher.wait()
}

func TestHeartbeatCommitTokenWritesOnlyExactInstalledTransactionMarker(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	stateDir := filepath.Join(root, "state")
	digest := strings.Repeat("a", 64)
	writeCurrentReleaseFixture(t, installDir, "2.4.0", digest)
	paths := MachinePaths{Update: filepath.Join(stateDir, "update.json")}
	journal := UpdateJournal{
		State: "installer_launched", TransactionID: "update-2.4.0-a-7", CommandID: 41,
		PayloadHash: strings.Repeat("b", 64), TargetVersion: "2.4.0", ArtifactSHA256: digest, Generation: 7,
	}
	if err := SaveUpdateJournal(paths.Update, journal); err != nil {
		t.Fatal(err)
	}
	agent := Agent{Config: Config{InstallDir: installDir}, Paths: paths}
	response := HeartbeatResponse{
		DesiredRelease: &HeartbeatDesiredUpdate{
			Version: journal.TargetVersion, SHA256: journal.ArtifactSHA256, CommandID: journal.CommandID,
			PayloadHash: journal.PayloadHash, Generation: journal.Generation, TTLSeconds: 300,
		},
		CommitToken: strings.Repeat("c", 64),
	}
	if err := agent.acceptHeartbeatCommit(response); err != nil {
		t.Fatal(err)
	}
	marker, err := viewerinstall.LoadCommitMarker(viewerinstall.Layout{InstallDir: installDir, StateDir: stateDir})
	if err != nil || marker.TransactionID != journal.TransactionID || marker.CommandID != journal.CommandID || marker.Token != response.CommitToken {
		t.Fatalf("marker=%#v err=%v", marker, err)
	}

	if err := viewerinstall.RemoveCommitMarker(viewerinstall.Layout{InstallDir: installDir, StateDir: stateDir}); err != nil {
		t.Fatal(err)
	}
	response.DesiredRelease.CommandID++
	if err := agent.acceptHeartbeatCommit(response); err == nil {
		t.Fatal("wrong command token accepted")
	}
	if _, err := viewerinstall.LoadCommitMarker(viewerinstall.Layout{InstallDir: installDir, StateDir: stateDir}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong command wrote marker: %v", err)
	}
}

func TestHeartbeatIdentityUsesInstalledOuterArtifactRelease(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "install")
	digest := strings.Repeat("d", 64)
	writeCurrentReleaseFixture(t, installDir, "2.5.0", digest)
	version, artifactSHA256 := installedReleaseIdentity(installDir)
	if version != "2.5.0" || artifactSHA256 != digest {
		t.Fatalf("installed identity version=%q digest=%q", version, artifactSHA256)
	}
}

func TestHeartbeatReconcilesCommittedInstallerTransactionAndReportsCommandWithoutReboot(t *testing.T) {
	stateDir, target := committedUpdateFixture(t)
	paths := MachinePaths{State: filepath.Join(stateDir, "state.json"), Commands: filepath.Join(stateDir, "commands.json"), Update: filepath.Join(stateDir, "update.json")}
	if err := SaveMachineState(paths.State, MachineState{ViewerState: "running", RendererState: "ready"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveUpdateJournal(paths.Update, UpdateJournal{
		State: "installer_launched", TransactionID: target.TransactionID, CommandID: target.CommandID,
		PayloadHash: target.PayloadHash, TargetVersion: target.Version, ArtifactSHA256: target.SHA256, Generation: target.Generation,
	}); err != nil {
		t.Fatal(err)
	}
	record := CommandRecord{
		ID: target.CommandID, Type: "update_app", PayloadHash: target.PayloadHash, OperationKey: target.TransactionID,
		DesiredVersion: target.Version, ArtifactSHA256: target.SHA256, Generation: target.Generation,
		State: CommandRunning, TTLSeconds: 300, CreatedAt: time.Now().UTC(),
	}
	if err := SaveCommandLedger(paths.Commands, CommandLedger{Records: map[string]CommandRecord{commandKey(target.CommandID): record}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(HeartbeatResponse{DesiredRelease: &HeartbeatDesiredUpdate{
			Version: target.Version, SHA256: target.SHA256, CommandID: target.CommandID,
			PayloadHash: target.PayloadHash, Generation: target.Generation, TTLSeconds: 300, CreatedAt: record.CreatedAt,
		}})
	}))
	defer server.Close()
	reported := make(chan CommandState, 2)
	agent := Agent{
		Config: Config{ClientID: "viewer-post-commit", DisplayName: "Wall", ServerURL: server.URL}, Paths: paths,
		HTTPClient: server.Client(), Reporter: reporterFunc(func(_ context.Context, _ Command, state CommandState, _, _ string) error {
			reported <- state
			return nil
		}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	dispatcher := newControlCommandDispatcher(ctx, cancel, &agent)
	agent.sendHeartbeat(ctx, ControlClient{HTTPClient: server.Client(), ServerURL: server.URL}, dispatcher.dispatch)
	select {
	case state := <-reported:
		if state != CommandSucceeded {
			t.Fatalf("reported state=%q", state)
		}
	case <-time.After(time.Second):
		t.Fatal("committed update command was not reconciled")
	}
	journal, err := LoadUpdateJournal(paths.Update)
	ledger, ledgerErr := LoadCommandLedger(paths.Commands)
	if err != nil || ledgerErr != nil || journal.State != "committed" || ledger.Records[commandKey(target.CommandID)].State != CommandSucceeded {
		t.Fatalf("journal=%#v ledger=%#v err=%v ledgerErr=%v", journal, ledger, err, ledgerErr)
	}
	cancel()
	_ = dispatcher.wait()
}

func writeCurrentReleaseFixture(t *testing.T, installDir, version, digest string) {
	t.Helper()
	releaseID := version + "-" + digest
	releaseDir := filepath.Join(installDir, "releases", releaseID)
	if err := os.MkdirAll(filepath.Join(releaseDir, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"camstation-viewer-agent.exe", filepath.Join("viewer", "CamStationViewer.exe")} {
		if err := os.WriteFile(filepath.Join(releaseDir, name), []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	pointer := CurrentRelease{
		SchemaVersion: SchemaVersion, ReleaseID: releaseID, Version: version, Digest: digest,
		AgentPath:  filepath.Join("releases", releaseID, "camstation-viewer-agent.exe"),
		ViewerPath: filepath.Join("releases", releaseID, "viewer", "CamStationViewer.exe"),
	}
	encoded, err := json.Marshal(pointer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "current.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
