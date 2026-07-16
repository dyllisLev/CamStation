package vieweragent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestControlTimingIsExplicitAndBounded(t *testing.T) {
	if DefaultControlReadDeadline != 25*time.Second {
		t.Fatalf("read deadline=%v", DefaultControlReadDeadline)
	}
	if DefaultHeartbeatRequestDeadline != 10*time.Second {
		t.Fatalf("heartbeat deadline=%v", DefaultHeartbeatRequestDeadline)
	}
	if DefaultCommandReportDeadline != 5*time.Second {
		t.Fatalf("command report deadline=%v", DefaultCommandReportDeadline)
	}
	state := ReconnectState{}
	var got []time.Duration
	for range 7 {
		got = append(got, state.NextDelay())
	}
	want := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 5 * time.Minute, 5 * time.Minute}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reconnect delays=%v want=%v", got, want)
	}
	state.Reset()
	if got := state.NextDelay(); got != time.Second {
		t.Fatalf("reset delay=%v", got)
	}
}

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
				t.Errorf("poll wait=%q, want 24", r.URL.Query().Get("wait"))
			}
			_ = json.NewEncoder(w).Encode(Command{ID: 7, Type: "ping", PayloadHash: "hash", TTLSeconds: 300})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-1"}
	result, err := client.Next(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Transport != ControlTransportLongPoll || result.Command == nil || result.Command.ID != 7 {
		t.Fatalf("unexpected fallback result: %+v", result)
	}
	if sseCalls.Load() != 1 || pollCalls.Load() != 1 {
		t.Fatalf("calls sse=%d poll=%d", sseCalls.Load(), pollCalls.Load())
	}
}

func TestHalfOpenSSETimesOutThenPolls(t *testing.T) {
	var pollCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/viewers/client-2/control":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
		case "/api/viewers/client-2/commands/next":
			pollCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-2", ReadDeadline: 20 * time.Millisecond}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := client.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Transport != ControlTransportLongPoll || pollCalls.Load() != 1 {
		t.Fatalf("did not recover through poll: %+v calls=%d", result, pollCalls.Load())
	}
}
