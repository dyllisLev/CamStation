package viewerservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

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
