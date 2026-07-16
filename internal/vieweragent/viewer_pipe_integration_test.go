package vieweragent

import (
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
