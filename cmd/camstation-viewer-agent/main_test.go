package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"camstation/internal/vieweragent"
)

func TestHostStopInputCancelsAgentCleanly(t *testing.T) {
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- watchHostControl(reader, cancel) }()
	if _, err := io.WriteString(writer, "stop\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("host stop did not cancel Agent")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPlannedRestartUsesDedicatedExitCode(t *testing.T) {
	if code := agentExitCode(vieweragent.ErrAgentRestartRequested); code != vieweragent.PlannedRestartExitCode {
		t.Fatalf("planned restart exit code=%d", code)
	}
	if code := agentExitCode(errors.New("crash")); code == vieweragent.PlannedRestartExitCode || code == 0 {
		t.Fatalf("crash exit code=%d", code)
	}
}

func TestReadySignalIsSingleLine(t *testing.T) {
	var output bytes.Buffer
	agentReadySignal(&output)
	if output.String() != "ready\n" {
		t.Fatalf("ready signal=%q", output.String())
	}
}
