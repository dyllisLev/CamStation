package vieweragent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunControlDispatchesForcedRestartWhileUpdateIsPending(t *testing.T) {
	updateStarted := make(chan struct{})
	updateStopped := make(chan struct{})
	restartExecuted := make(chan struct{})
	var heartbeats atomic.Int32
	updateCommand := Command{
		ID: 92, Type: "update_app", PayloadHash: "update-92", TTLSeconds: 300,
		DesiredVersion: "2.9.2", ArtifactSHA256: strings.Repeat("a", 64), Generation: 9,
	}
	restartCommand := Command{ID: 93, Type: "restart_viewer", PayloadHash: "restart-93", TTLSeconds: 300}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/control"):
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			writeCommand := func(command Command) {
				payload, _ := json.Marshal(command)
				_, _ = w.Write(append(append([]byte("data: "), payload...), '\n', '\n'))
				flusher.Flush()
			}
			writeCommand(updateCommand)
			select {
			case <-updateStarted:
				writeCommand(restartCommand)
			case <-r.Context().Done():
				return
			}
			<-r.Context().Done()
		case r.URL.Path == "/api/viewers/heartbeat":
			heartbeats.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	paths := MachinePaths{State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json")}
	agent := NewAgent(Config{ClientID: "dispatch-client", ServerURL: server.URL, DisplayName: "Dispatch", InstallDir: dir}, paths)
	agent.HTTPClient = server.Client()
	agent.HeartbeatInterval = 5 * time.Millisecond
	agent.ControlReadDeadline = time.Second
	agent.ServePipe = serveTestViewerPipe
	agent.Executor = executorFunc(func(ctx context.Context, command Command, _ string) error {
		switch command.Type {
		case "update_app":
			close(updateStarted)
			defer close(updateStopped)
			<-ctx.Done()
			return ctx.Err()
		case "restart_viewer":
			close(restartExecuted)
		}
		return nil
	})

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- agent.Run(ctx) }()
	select {
	case <-restartExecuted:
		cancel()
	case <-t.Context().Done():
		t.Fatal("forced restart was not dispatched while update remained pending")
	}
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-updateStopped:
	default:
		t.Fatal("pending update worker was not joined during Agent shutdown")
	}
	ledger, err := LoadCommandLedger(paths.Commands)
	if err != nil || ledger.Records[restartCommand.Key()].State != CommandSucceeded || ledger.Records[updateCommand.Key()].State != CommandFailed {
		t.Fatalf("ledger=%+v err=%v", ledger.Records, err)
	}
	if heartbeats.Load() == 0 {
		t.Fatal("Agent heartbeat stopped while update was pending")
	}
}
