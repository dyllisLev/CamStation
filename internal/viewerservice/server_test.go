package viewerservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServiceRunsWithoutConfigurationOrViewer(t *testing.T) {
	listener := newFakePipeListener()
	runtime := Service{Store: missingConfigStore{}, Listener: listener}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)
	status := runtime.Status()
	if status.Configured || status.Viewer != "closed" || status.Renderer != "not_ready" {
		t.Fatalf("status=%+v", status)
	}
	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestServiceCancellationClosesClientsAndWaitsForHandlers(t *testing.T) {
	listener := newFakePipeListener()
	connection := newBlockingPipeConnection(Peer{PID: 10, SessionID: 2, Interactive: true, UserSID: "S-1-5-21-1000"})
	listener.connections <- connection
	runtime := Service{Store: missingConfigStore{}, Listener: listener}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)
	connection.WaitRead(t)
	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
	if !connection.Closed() {
		t.Fatal("service returned before closing active client")
	}
}

func TestServiceWritesBoundedLifecycleLog(t *testing.T) {
	root := t.TempDir()
	listener := newFakePipeListener()
	logs := newLogManager(root, DefaultLogRotateBytes, DefaultLogRetainedFiles, func(string, string) error { return nil })
	runtime := Service{Store: missingConfigStore{}, Listener: listener, Logs: logs}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)
	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ServiceLogFilename))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("lifecycle records=%d want=2: %s", len(lines), data)
	}
	var states []string
	for _, data := range lines {
		var line logLine
		decodeJSON(t, data, &line)
		if line.CorrelationID == "" {
			t.Fatalf("missing correlation: %+v", line)
		}
		states = append(states, line.State)
	}
	if !slices.Equal(states, []string{"running", "stopped"}) {
		t.Fatalf("states=%v", states)
	}
}

func TestServiceStructuredFailureKeepsClientAndAcceptLoopUsable(t *testing.T) {
	listener := newFakePipeListener()
	runtime := Service{Store: missingConfigStore{}, Listener: listener}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)

	client, service := net.Pipe()
	listener.connections <- &fakePipeConnection{ReadWriteCloser: service, peer: Peer{PID: 10, SessionID: 2, Interactive: true, UserSID: "S-1-5-21-1000"}}
	writeRequest(t, client, Request{Version: PipeProtocolVersion, RequestID: "bad", Type: "unknown"})
	if response := readResponse(t, client); response.OK || response.ErrorCode != CodeUnsupportedRequest {
		t.Fatalf("response=%+v", response)
	}
	writeRequest(t, client, Request{Version: PipeProtocolVersion, RequestID: "status", Type: "get_status"})
	if response := readResponse(t, client); !response.OK {
		t.Fatalf("next response=%+v", response)
	}
	_ = client.Close()

	malformedClient, malformedService := net.Pipe()
	listener.connections <- &fakePipeConnection{ReadWriteCloser: malformedService, peer: Peer{PID: 11, SessionID: 3, Interactive: true, UserSID: "S-1-5-21-1001"}}
	if _, err := malformedClient.Write([]byte("not-json\n")); err != nil {
		t.Fatal(err)
	}
	_ = malformedClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := malformedClient.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("malformed client was not closed: %v", err)
	}

	nextClient, nextService := net.Pipe()
	listener.connections <- &fakePipeConnection{ReadWriteCloser: nextService, peer: Peer{PID: 12, SessionID: 4, Interactive: true, UserSID: "S-1-5-21-1002"}}
	writeRequest(t, nextClient, Request{Version: PipeProtocolVersion, RequestID: "next", Type: "get_status"})
	if response := readResponse(t, nextClient); !response.OK {
		t.Fatalf("response after malformed peer=%+v", response)
	}
	_ = nextClient.Close()
	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestServiceVerifiedIdentityFailureClosesOnlyThatClient(t *testing.T) {
	listener := newFakePipeListener()
	runtime := Service{Store: missingConfigStore{}, Listener: listener}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)

	failedClient, failedService := net.Pipe()
	listener.connections <- &fakePipeConnection{ReadWriteCloser: failedService, peerErr: ErrPeerIdentity}
	_ = failedClient.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := failedClient.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("identity-failed client was not closed: %v", err)
	}

	nextClient, nextService := net.Pipe()
	listener.connections <- &fakePipeConnection{ReadWriteCloser: nextService, peer: Peer{PID: 12, SessionID: 4, UserSID: "S-1-5-21-1002"}}
	writeRequest(t, nextClient, Request{Version: PipeProtocolVersion, RequestID: "next", Type: "get_status"})
	if response := readResponse(t, nextClient); !response.OK {
		t.Fatalf("response after identity failure=%+v", response)
	}
	_ = nextClient.Close()
	cancel()
	if err := waitResult(t, done); err != nil {
		t.Fatal(err)
	}
}

type missingConfigStore struct{}

func (missingConfigStore) Load(context.Context) (MachineConfig, error) {
	return MachineConfig{}, ErrNotConfigured
}
func (missingConfigStore) Save(context.Context, MachineConfig) error { return nil }

type fakePipeListener struct {
	connections chan PipeConnection
	closed      chan struct{}
	ready       chan struct{}
	readyOnce   sync.Once
	closeOnce   sync.Once
}

func newFakePipeListener() *fakePipeListener {
	return &fakePipeListener{connections: make(chan PipeConnection, 8), closed: make(chan struct{}), ready: make(chan struct{})}
}

func (listener *fakePipeListener) Accept() (PipeConnection, error) {
	listener.readyOnce.Do(func() { close(listener.ready) })
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.closed:
		return nil, ErrListenerClosed
	}
}

func (listener *fakePipeListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return nil
}

func (listener *fakePipeListener) Ready() <-chan struct{} { return listener.ready }

func (listener *fakePipeListener) WaitReady(t *testing.T) {
	t.Helper()
	select {
	case <-listener.ready:
	case <-time.After(time.Second):
		t.Fatal("listener was not started")
	}
}

type fakePipeConnection struct {
	io.ReadWriteCloser
	peer    Peer
	peerErr error
}

func (connection *fakePipeConnection) Peer() (Peer, error) {
	return connection.peer, connection.peerErr
}

type blockingPipeConnection struct {
	peer      Peer
	read      chan struct{}
	closed    chan struct{}
	readOnce  sync.Once
	closeOnce sync.Once
}

func newBlockingPipeConnection(peer Peer) *blockingPipeConnection {
	return &blockingPipeConnection{peer: peer, read: make(chan struct{}), closed: make(chan struct{})}
}

func (connection *blockingPipeConnection) Read([]byte) (int, error) {
	connection.readOnce.Do(func() { close(connection.read) })
	<-connection.closed
	return 0, io.ErrClosedPipe
}

func (*blockingPipeConnection) Write(data []byte) (int, error) { return len(data), nil }
func (connection *blockingPipeConnection) Close() error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}
func (connection *blockingPipeConnection) Peer() (Peer, error) { return connection.peer, nil }
func (connection *blockingPipeConnection) WaitRead(t *testing.T) {
	t.Helper()
	select {
	case <-connection.read:
	case <-time.After(time.Second):
		t.Fatal("handler did not start reading")
	}
}
func (connection *blockingPipeConnection) Closed() bool {
	select {
	case <-connection.closed:
		return true
	default:
		return false
	}
}

func writeRequest(t *testing.T, writer io.Writer, request Request) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func readResponse(t *testing.T, reader io.Reader) Response {
	t.Helper()
	var response Response
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	return response
}

func waitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("service did not stop within bound")
		return nil
	}
}

func TestRequestErrorReturnsSafeResponseAndKeepsConnectionUsable(t *testing.T) {
	secret := "registry C:\\secret token=do-not-return"
	store := &memoryConfigStore{
		config:  testMachineConfig(),
		saveErr: errors.New(secret),
	}
	var logged error
	server := testServer(store, func(_ context.Context, err error) string {
		logged = err
		return "corr-42"
	})
	peer := Peer{PID: 10, SessionID: 2, Interactive: true}
	response, err := server.Handle(context.Background(), "connection-a", peer, Request{
		Version: PipeProtocolVersion, RequestID: "configure", Type: "configure",
		Payload: json.RawMessage(`{"serverUrl":"https://new.example","displayName":"새 뷰어","autoStart":true}`),
	})
	if err != nil || response.OK || response.ErrorCode != CodeStorageFailed {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), secret) || strings.Contains(response.Message, "registry") || !strings.Contains(response.Message, "corr-42") {
		t.Fatalf("unsafe response=%s", encoded)
	}
	if logged == nil || !strings.Contains(logged.Error(), secret) {
		t.Fatalf("logged error=%v", logged)
	}

	store.saveErr = nil
	next, err := server.Handle(context.Background(), "connection-a", peer, Request{
		Version: PipeProtocolVersion, RequestID: "status", Type: "get_status",
	})
	if err != nil || !next.OK {
		t.Fatalf("next response=%+v err=%v", next, err)
	}
}

func TestRequestErrorOnlyProtocolOrIdentityFailureIsSurfaced(t *testing.T) {
	server := testServer(&memoryConfigStore{config: testMachineConfig()}, nil)
	peer := Peer{PID: 10, SessionID: 2, Interactive: true}

	if _, err := ReadRequest(strings.NewReader("not-json\n")); !errors.Is(err, ErrProtocol) {
		t.Fatalf("malformed frame error=%v", err)
	}
	if _, err := server.Handle(context.Background(), "connection-a", Peer{}, Request{
		Version: PipeProtocolVersion, RequestID: "status", Type: "get_status",
	}); !errors.Is(err, ErrPeerIdentity) {
		t.Fatalf("identity error=%v", err)
	}
	if _, err := server.Handle(context.Background(), "connection-a", Peer{PID: 10, SessionID: 2}, Request{
		Version: PipeProtocolVersion, RequestID: "configure", Type: "configure", Payload: json.RawMessage(`{}`),
	}); !errors.Is(err, ErrPeerIdentity) {
		t.Fatalf("noninteractive configure error=%v", err)
	}
	response, err := server.Handle(context.Background(), "connection-a", peer, Request{
		Version: PipeProtocolVersion, RequestID: "unknown", Type: "unknown",
	})
	if err != nil || response.OK || response.ErrorCode != CodeUnsupportedRequest {
		t.Fatalf("operation response=%+v err=%v", response, err)
	}
}

func TestLeaseBusyResponseContainsNoSecretLeaseData(t *testing.T) {
	server := testServer(&memoryConfigStore{config: testMachineConfig()}, nil)
	first := Peer{PID: 10, SessionID: 2, Interactive: true}
	response, err := server.Handle(context.Background(), "connection-a", first, Request{
		Version: PipeProtocolVersion, RequestID: "lease-a", Type: "acquire_lease",
	})
	if err != nil || !response.OK {
		t.Fatalf("first response=%+v err=%v", response, err)
	}
	var grant LeaseGrant
	if err := json.Unmarshal(response.Payload, &grant); err != nil || grant.LeaseID == "" || grant.HeartbeatSeconds != 5 {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}

	busy, err := server.Handle(context.Background(), "connection-b", Peer{PID: 11, SessionID: 3, Interactive: true}, Request{
		Version: PipeProtocolVersion, RequestID: "lease-b", Type: "acquire_lease",
	})
	if err != nil || busy.OK || busy.ErrorCode != CodeLeaseBusy || len(busy.Payload) != 0 {
		t.Fatalf("busy response=%+v err=%v", busy, err)
	}
	encoded, _ := json.Marshal(busy)
	if strings.Contains(string(encoded), grant.LeaseID) || strings.Contains(string(encoded), `"pid"`) || strings.Contains(string(encoded), `"sessionId"`) {
		t.Fatalf("busy response leaks lease data: %s", encoded)
	}
}

func TestDisconnectReleasesOwnedLeaseImmediately(t *testing.T) {
	server := testServer(&memoryConfigStore{config: testMachineConfig()}, nil)
	request := Request{Version: PipeProtocolVersion, RequestID: "lease", Type: "acquire_lease"}
	if response, err := server.Handle(context.Background(), "connection-a", Peer{PID: 10, SessionID: 2, Interactive: true}, request); err != nil || !response.OK {
		t.Fatalf("acquire response=%+v err=%v", response, err)
	}
	server.HandleDisconnect("connection-a")
	request.RequestID = "lease-2"
	if response, err := server.Handle(context.Background(), "connection-b", Peer{PID: 11, SessionID: 3, Interactive: true}, request); err != nil || !response.OK {
		t.Fatalf("acquire after disconnect response=%+v err=%v", response, err)
	}
}

func TestRequestStatusNeverExposesPrivateConfiguration(t *testing.T) {
	server := testServer(&memoryConfigStore{config: testMachineConfig()}, nil)
	response, err := server.Handle(context.Background(), "connection-a", Peer{PID: 10, SessionID: 2}, Request{
		Version: PipeProtocolVersion, RequestID: "status", Type: "get_status",
	})
	if err != nil || !response.OK {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	encoded := string(response.Payload)
	for _, forbidden := range []string{"client-secret", "clientId", RegistrySubkey, "token", "registry"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("status contains %q: %s", forbidden, encoded)
		}
	}
	var status StatusSnapshot
	if err := json.Unmarshal(response.Payload, &status); err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.Config == nil || status.Config.ServerURL != "https://cam.example" ||
		status.Config.DisplayName != "관제실" || !status.AutoStart || !status.LeaseAvailable {
		t.Fatalf("status=%+v", status)
	}
}

func TestRequestLeaseRefreshReportsAndReleaseRequireOwner(t *testing.T) {
	server := testServer(&memoryConfigStore{config: testMachineConfig()}, nil)
	peer := Peer{PID: 10, SessionID: 2, Interactive: true}
	grantResponse, err := server.Handle(context.Background(), "connection-a", peer, Request{
		Version: PipeProtocolVersion, RequestID: "acquire", Type: "acquire_lease",
	})
	if err != nil {
		t.Fatal(err)
	}
	var grant LeaseGrant
	if err := json.Unmarshal(grantResponse.Payload, &grant); err != nil {
		t.Fatal(err)
	}

	requests := []Request{
		{Version: PipeProtocolVersion, RequestID: "heartbeat", Type: "lease_heartbeat", Payload: leasePayload(t, grant.LeaseID, nil)},
		{Version: PipeProtocolVersion, RequestID: "viewer", Type: "viewer_status", Payload: leasePayload(t, grant.LeaseID, map[string]any{"state": "running"})},
		{Version: PipeProtocolVersion, RequestID: "renderer", Type: "renderer_status", Payload: leasePayload(t, grant.LeaseID, map[string]any{"state": "ready"})},
		{Version: PipeProtocolVersion, RequestID: "stream", Type: "stream_telemetry", Payload: leasePayload(t, grant.LeaseID, map[string]any{"streamName": "yard-live", "phase": "playing"})},
		{Version: PipeProtocolVersion, RequestID: "diagnostic", Type: "diagnostic_event", Payload: leasePayload(t, grant.LeaseID, map[string]any{"code": "renderer_ready"})},
	}
	for _, request := range requests {
		response, err := server.Handle(context.Background(), "connection-a", peer, request)
		if err != nil || !response.OK {
			t.Fatalf("%s response=%+v err=%v", request.Type, response, err)
		}
	}

	foreign := requests[0]
	foreign.RequestID = "foreign"
	if _, err := server.Handle(context.Background(), "connection-b", peer, foreign); !errors.Is(err, ErrPeerIdentity) {
		t.Fatalf("foreign lease error=%v", err)
	}
	release := Request{Version: PipeProtocolVersion, RequestID: "release", Type: "release_lease", Payload: leasePayload(t, grant.LeaseID, nil)}
	if response, err := server.Handle(context.Background(), "connection-a", peer, release); err != nil || !response.OK {
		t.Fatalf("release response=%+v err=%v", response, err)
	}
}

func testServer(store ConfigStore, logger func(context.Context, error) string) *Server {
	manager := ConfigManager{
		Store:     store,
		Validator: validatorFunc(func(context.Context, ConfigDraft, string) error { return nil }),
		NewID:     func() (string, error) { return "client-new", nil },
	}
	return NewServer(manager, NewLeaseManager(time.Now, 15*time.Second), "2.0.0", logger)
}

func testMachineConfig() MachineConfig {
	return MachineConfig{
		SchemaVersion: ConfigSchemaVersion,
		ServerURL:     "https://cam.example",
		DisplayName:   "관제실",
		ClientID:      "client-secret",
		AutoStart:     true,
	}
}

func leasePayload(t *testing.T, leaseID string, fields map[string]any) json.RawMessage {
	t.Helper()
	payload := map[string]any{"leaseId": leaseID}
	for key, value := range fields {
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
