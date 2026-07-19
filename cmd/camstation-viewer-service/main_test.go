package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestRunRejectsUnknownFlagsWithExitCodeTwo(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"--install"}, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestServiceStopAndPreShutdownAreBounded(t *testing.T) {
	for _, command := range []serviceCommand{commandStop, commandPreShutdown} {
		t.Run(command.String(), func(t *testing.T) {
			commands := make(chan serviceCommand, 1)
			statuses := make(chan serviceStatus, 8)
			ready := make(chan struct{})
			close(ready)
			run := func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			}
			done := make(chan error, 1)
			go func() { done <- superviseService(run, ready, commands, statuses, time.Second) }()
			var got []serviceState
			got = append(got, (<-statuses).State, (<-statuses).State)
			commands <- command
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			close(statuses)
			for status := range statuses {
				got = append(got, status.State)
			}
			want := []serviceState{stateStartPending, stateRunning, stateStopPending, stateStopped}
			if !slices.Equal(got, want) {
				t.Fatalf("states=%v want=%v", got, want)
			}
		})
	}
}

func TestServiceReportsRunningOnlyAfterPipeIsReady(t *testing.T) {
	commands := make(chan serviceCommand, 1)
	statuses := make(chan serviceStatus, 8)
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- superviseService(func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}, ready, commands, statuses, time.Second)
	}()
	if status := <-statuses; status.State != stateStartPending || status.WaitHint != time.Second {
		t.Fatalf("initial status=%+v", status)
	}
	select {
	case status := <-statuses:
		t.Fatalf("reported state before ready: %+v", status)
	case <-time.After(20 * time.Millisecond):
	}
	close(ready)
	if status := <-statuses; status.State != stateRunning {
		t.Fatalf("ready state=%v", status.State)
	}
	commands <- commandStop
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServiceShutdownTimeoutIsBounded(t *testing.T) {
	commands := make(chan serviceCommand, 1)
	commands <- commandShutdown
	statuses := make(chan serviceStatus, 8)
	ready := make(chan struct{})
	close(ready)
	started := time.Now()
	err := superviseService(func(context.Context) error { select {} }, ready, commands, statuses, 20*time.Millisecond)
	if !errors.Is(err, errShutdownTimeout) || time.Since(started) > time.Second {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(started))
	}
}
