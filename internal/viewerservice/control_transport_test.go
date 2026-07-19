package viewerservice

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
	state := ReconnectState{}
	var got []time.Duration
	for range 7 {
		got = append(got, state.NextDelay())
	}
	want := []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reconnect delays=%v want=%v", got, want)
	}
	state.Reset()
	if got := state.NextDelay(); got != time.Second {
		t.Fatalf("reset delay=%v", got)
	}
}

func TestHeartbeatDecodesBoundedDesiredUpdateAndCommitToken(t *testing.T) {
	digest := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"viewer":{"id":"viewer-1"},"desiredRelease":{"version":"2.4.0","sha256":"%s","downloadUrl":"/api/viewers/app/download","commandId":41,"payloadHash":"%s","generation":7,"ttlSeconds":300,"commandState":"running","createdAt":"2026-07-16T00:00:00Z"},"commitToken":"%s"}`,
			digest, strings.Repeat("b", 64), strings.Repeat("c", 64))
	}))
	defer server.Close()
	response, err := (ControlClient{HTTPClient: server.Client(), ServerURL: server.URL}).ExchangeHeartbeat(t.Context(), HeartbeatPayload{})
	if err != nil || response.DesiredRelease == nil || response.DesiredRelease.CommandID != 41 ||
		response.DesiredRelease.Generation != 7 || response.DesiredRelease.SHA256 != digest || response.CommitToken != strings.Repeat("c", 64) {
		t.Fatalf("heartbeat response=%#v err=%v", response, err)
	}
}

func TestHeartbeatRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"viewer":{"id":"` + strings.Repeat("x", maxControlMessageBytes) + `"}}`))
	}))
	defer server.Close()
	if _, err := (ControlClient{HTTPClient: server.Client(), ServerURL: server.URL}).ExchangeHeartbeat(t.Context(), HeartbeatPayload{}); err == nil {
		t.Fatal("oversized heartbeat response accepted")
	}
}

func TestControlCharacterizationFallsBackFromSSEToLongPoll(t *testing.T) {
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

func TestSSECharacterizationInactivityDeadlineResetsAfterEveryFrame(t *testing.T) {
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

func TestSSEHeaderBlackholeFallsBackWithinDeadline(t *testing.T) {
	var pollCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/control"):
			<-r.Context().Done()
		case strings.HasSuffix(r.URL.Path, "/commands/next"):
			pollCalls.Add(1)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-header-blackhole", ReadDeadline: 30 * time.Millisecond}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	result, err := client.Next(ctx)
	if err != nil || result.Transport != ControlTransportLongPoll || pollCalls.Load() != 1 {
		t.Fatalf("result=%+v polls=%d elapsed=%v err=%v", result, pollCalls.Load(), time.Since(started), err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("header blackhole fallback took %v", elapsed)
	}
}

func TestSSEHeaderDeadlineDoesNotEndHealthyStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for range 5 {
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
			time.Sleep(12 * time.Millisecond)
		}
	}))
	defer server.Close()

	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-header-healthy", ReadDeadline: 30 * time.Millisecond}
	seen, err := client.StreamSSE(t.Context(), func(ControlResult) error { return nil })
	if err == nil || seen < 5 {
		t.Fatalf("healthy stream ended before all frames: seen=%d err=%v", seen, err)
	}
}

func TestSSEProbesDoNotShortenLongPoll(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	var sseCalls, pollCalls, earlyCancels, overlappingPolls, activePolls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/control") {
			sseCalls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		pollCalls.Add(1)
		if activePolls.Add(1) > 1 {
			overlappingPolls.Add(1)
		}
		defer activePolls.Add(-1)
		select {
		case <-time.After(70 * time.Millisecond):
			_ = json.NewEncoder(w).Encode(Command{ID: 70, Type: "ping", PayloadHash: "poll-70", TTLSeconds: 300})
		case <-r.Context().Done():
			earlyCancels.Add(1)
		}
	}))
	defer server.Close()

	probes := ReconnectState{delays: []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 15 * time.Millisecond, 20 * time.Millisecond}}
	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-full-poll", ReadDeadline: 150 * time.Millisecond}
	var commandID atomic.Int64
	err := client.RunControl(ctx, &probes, func(result ControlResult) error {
		if result.Command != nil {
			commandID.Store(result.Command.ID)
			cancel()
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if commandID.Load() != 70 || sseCalls.Load() < 4 || pollCalls.Load() != 1 || earlyCancels.Load() != 0 || overlappingPolls.Load() != 0 {
		t.Fatalf("command=%d sse=%d polls=%d earlyCancel=%d overlap=%d", commandID.Load(), sseCalls.Load(), pollCalls.Load(), earlyCancels.Load(), overlappingPolls.Load())
	}
}

func TestLongPollCommandArrivesWhileSSEProbePending(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	var sseCalls atomic.Int32
	var probeActive atomic.Bool
	var commandDuringProbe atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/control") {
			if sseCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			probeActive.Store(true)
			defer probeActive.Store(false)
			<-r.Context().Done()
			return
		}
		time.Sleep(30 * time.Millisecond)
		commandDuringProbe.Store(probeActive.Load())
		_ = json.NewEncoder(w).Encode(Command{ID: 71, Type: "ping", PayloadHash: "poll-71", TTLSeconds: 300})
	}))
	defer server.Close()

	probes := ReconnectState{delays: []time.Duration{5 * time.Millisecond}}
	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-pending-probe", ReadDeadline: 100 * time.Millisecond}
	var commandID atomic.Int64
	err := client.RunControl(ctx, &probes, func(result ControlResult) error {
		if result.Command != nil {
			commandID.Store(result.Command.ID)
			cancel()
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if commandID.Load() != 71 || !commandDuringProbe.Load() {
		t.Fatalf("command=%d duringProbe=%v sseCalls=%d", commandID.Load(), commandDuringProbe.Load(), sseCalls.Load())
	}
}

func TestSSERejectsCumulativeOversizedFrame(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 700 {
			_, _ = fmt.Fprintf(w, "data: %s\n", strings.Repeat("x", 100))
		}
		_, _ = fmt.Fprint(w, "\n")
	}))
	defer server.Close()

	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-large-frame", ReadDeadline: time.Second}
	_, err := client.StreamSSE(t.Context(), func(ControlResult) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "SSE frame exceeds 64 KiB") {
		t.Fatalf("oversized frame err=%v", err)
	}
}

func TestSSEAcceptsWhitespacePaddedFrame(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data:    {\"id\":72,\"type\":\"ping\",\"payloadHash\":\"space\",\"ttlSeconds\":300}   \n\n")
	}))
	defer server.Close()

	client := ControlClient{HTTPClient: server.Client(), ServerURL: server.URL, ClientID: "client-space-frame", ReadDeadline: time.Second}
	var commandID int64
	_, err := client.StreamSSE(t.Context(), func(result ControlResult) error {
		commandID = result.Command.ID
		return errStopSSETest
	})
	if !errors.Is(err, errStopSSETest) || commandID != 72 {
		t.Fatalf("command=%d err=%v", commandID, err)
	}
}
