package viewerservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestControlFallsBackFromSSEToLongPoll(t *testing.T) {
	var sseCalls, pollCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/viewers/client-1/control":
			sseCalls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/api/viewers/client-1/commands/next":
			pollCalls.Add(1)
			if r.URL.Query().Get("wait") != "24" {
				t.Errorf("poll wait=%q", r.URL.Query().Get("wait"))
			}
			_ = json.NewEncoder(w).Encode(Command{ID: 7, Type: "ping", PayloadHash: "hash", TTLSeconds: 300})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result, err := (ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-1"}).Next(t.Context())
	if err != nil || result.Transport != ControlTransportLongPoll || result.Command == nil || result.Command.ID != 7 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if sseCalls.Load() != 1 || pollCalls.Load() != 1 {
		t.Fatalf("sse=%d poll=%d", sseCalls.Load(), pollCalls.Load())
	}
}

func TestSSEInactivityDeadlineResetsAfterEveryFrame(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for range 3 {
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
			time.Sleep(15 * time.Millisecond)
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-idle", ReadDeadline: 25 * time.Millisecond}
	seen, err := client.StreamSSE(t.Context(), func(ControlResult) error { return nil })
	if !errors.Is(err, ErrControlInactivity) || seen != 3 {
		t.Fatalf("seen=%d err=%v", seen, err)
	}
}

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
	command := Command{ID: 7, Type: "reload_live", PayloadHash: strings.Repeat("a", 64), TTLSeconds: 300}
	if err := runtime.DeliverViewerCommand(command); err != nil {
		t.Fatal(err)
	}
	commandResponse := readResponse(t, client)
	var delivered Command
	if err := json.Unmarshal(commandResponse.Payload, &delivered); err != nil || delivered.ID != command.ID {
		t.Fatalf("response=%+v delivered=%+v err=%v", commandResponse, delivered, err)
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
