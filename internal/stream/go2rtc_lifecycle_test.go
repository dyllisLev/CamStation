package stream

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A real listener makes successful replacement depend on the previous process
// releasing the same socket. Only this test's own child processes are stopped.
func TestGo2RTCLifecycleHelper(t *testing.T) {
	if os.Getenv("CAMSTATION_LIFECYCLE_HELPER") != "1" {
		return
	}
	address := os.Args[len(os.Args)-1]
	listener, err := net.Listen("tcp", address)
	if err != nil {
		os.Exit(2)
	}
	_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	os.Exit(0)
}

func lifecycleStreamer(t *testing.T) *Go2RTC {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	binary := filepath.Join(t.TempDir(), "go2rtc-test")
	executable := "'" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "'"
	script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run=^TestGo2RTCLifecycleHelper$ -- \"$@\"\n", executable)
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMSTATION_LIFECYCLE_HELPER", "1")
	g := NewGo2RTC(address)
	g.binary, g.apiURL = binary, "http://"+address
	t.Cleanup(func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.stopProcessLocked()
	})
	if err := g.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGo2RTCRestartWaitsForPreviousChildReaping(t *testing.T) {
	g := lifecycleStreamer(t)
	g.mu.Lock()
	old := g.cmd
	reaped := g.cmdDone
	delayed := make(chan struct{})
	g.cmdDone = delayed
	g.mu.Unlock()
	// Model a delayed Wait completion: even once Kill is delivered, replacement
	// must not begin until the lifecycle owner observes its completion signal.
	finished := make(chan error, 1)
	go func() { finished <- g.restartProcess(context.Background()) }()
	select {
	case <-reaped:
	case <-time.After(3 * time.Second):
		close(delayed)
		t.Fatal("previous child was not killed and reaped")
	}
	select {
	case err := <-finished:
		close(delayed)
		t.Fatalf("replacement started before Wait completion: %v", err)
	default:
	}
	close(delayed)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("restart deadlocked with exit logging")
	}
	g.mu.Lock()
	replacement := g.cmd
	g.mu.Unlock()
	if replacement == old || !healthy(context.Background(), g.apiURL) {
		t.Fatal("replacement did not own a healthy listener")
	}
}

func TestGo2RTCRepeatedListenerReplacement(t *testing.T) {
	g := lifecycleStreamer(t)
	for i := 0; i < 8; i++ {
		g.mu.Lock()
		done := g.cmdDone
		g.mu.Unlock()
		if err := g.restartProcess(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
		default:
			t.Fatal("replacement healthy before previous Wait completed")
		}
		if err := g.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
