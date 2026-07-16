package vieweragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var errStopSSETest = errors.New("stop SSE test")

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

func TestSSESessionKeepsOneConnectionAcrossFrames(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, frame := range []string{
			": keepalive\n\n",
			"event: command\ndata: {\"id\":41,\"type\":\"ping\",\"payloadHash\":\"h1\",\"ttlSeconds\":300}\n\n",
			": keepalive\n\n",
			"event: command\ndata: {\"id\":42,\"type\":\"ping\",\"payloadHash\":\"h2\",\"ttlSeconds\":300}\n\n",
		} {
			_, _ = fmt.Fprint(w, frame)
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-sse", ReadDeadline: time.Second}
	var frames, commands int
	seen, err := client.StreamSSE(t.Context(), func(result ControlResult) error {
		frames++
		if result.Command != nil {
			commands++
		}
		if frames == 4 {
			return errStopSSETest
		}
		return nil
	})
	if !errors.Is(err, errStopSSETest) || seen != 4 || commands != 2 || connections.Load() != 1 {
		t.Fatalf("seen=%d commands=%d connections=%d err=%v", seen, commands, connections.Load(), err)
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

func TestSuccessfulLongPollDoesNotResetSSEProbeBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var sseCalls, pollCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/control"):
			if sseCalls.Add(1) >= 7 {
				cancel()
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		case strings.HasSuffix(r.URL.Path, "/commands/next"):
			pollCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	probes := ReconnectState{delays: []time.Duration{time.Millisecond, 2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond, 30 * time.Millisecond, 50 * time.Millisecond}}
	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-probe", ReadDeadline: 20 * time.Millisecond}
	err := client.RunControl(ctx, &probes, func(ControlResult) error { return nil })
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if sseCalls.Load() < 7 || pollCalls.Load() == 0 || probes.failures < 6 {
		t.Fatalf("sse=%d poll=%d failures=%d", sseCalls.Load(), pollCalls.Load(), probes.failures)
	}
}

func TestImmediateEmptyPollCannotTightLoopBeforeNextSSEProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var sseCalls, pollCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/control") {
			if sseCalls.Add(1) == 2 {
				cancel()
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		pollCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	probes := ReconnectState{delays: []time.Duration{30 * time.Millisecond}}
	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-no-spin"}
	_ = client.RunControl(ctx, &probes, func(ControlResult) error { return nil })
	if pollCalls.Load() > 2 {
		t.Fatalf("empty long poll spun %d times in one probe interval", pollCalls.Load())
	}
}
