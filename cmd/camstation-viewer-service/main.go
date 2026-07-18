package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	serviceName            = "CamStationViewerService"
	serviceShutdownTimeout = 15 * time.Second
)

var (
	errStartupTimeout  = errors.New("viewer service startup timed out")
	errShutdownTimeout = errors.New("viewer service shutdown timed out")
)

type serviceCommand uint8

const (
	commandStop serviceCommand = iota + 1
	commandShutdown
	commandPreShutdown
)

func (command serviceCommand) String() string {
	switch command {
	case commandStop:
		return "stop"
	case commandShutdown:
		return "shutdown"
	case commandPreShutdown:
		return "preshutdown"
	default:
		return "unknown"
	}
}

type serviceState uint8

const (
	stateStartPending serviceState = iota + 1
	stateRunning
	stateStopPending
	stateStopped
)

type serviceStatus struct {
	State      serviceState
	Checkpoint uint32
	WaitHint   time.Duration
}

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	console := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--console":
		console = true
	default:
		fmt.Fprintln(stderr, "usage: CamStationViewerService.exe [--console]")
		return 2
	}
	if err := runPlatform(console); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func superviseService(runService func(context.Context) error, ready <-chan struct{}, commands <-chan serviceCommand, statuses chan<- serviceStatus, shutdownTimeout time.Duration) error {
	statuses <- serviceStatus{State: stateStartPending, Checkpoint: 1, WaitHint: shutdownTimeout}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runService(ctx) }()
	startupTimer := time.NewTimer(shutdownTimeout)
	for {
		select {
		case <-ready:
			startupTimer.Stop()
			goto running
		case err := <-done:
			startupTimer.Stop()
			statuses <- serviceStatus{State: stateStopped, Checkpoint: 3}
			return err
		case command := <-commands:
			if command != commandStop && command != commandShutdown && command != commandPreShutdown {
				continue
			}
			startupTimer.Stop()
			return stopService(cancel, done, statuses, shutdownTimeout, nil)
		case <-startupTimer.C:
			return stopService(cancel, done, statuses, shutdownTimeout, errStartupTimeout)
		}
	}

running:
	statuses <- serviceStatus{State: stateRunning}

	for {
		select {
		case err := <-done:
			statuses <- serviceStatus{State: stateStopped, Checkpoint: 3}
			return err
		case command := <-commands:
			if command != commandStop && command != commandShutdown && command != commandPreShutdown {
				continue
			}
			return stopService(cancel, done, statuses, shutdownTimeout, nil)
		}
	}
}

func stopService(cancel context.CancelFunc, done <-chan error, statuses chan<- serviceStatus, timeout time.Duration, result error) error {
	statuses <- serviceStatus{State: stateStopPending, Checkpoint: 2, WaitHint: timeout}
	cancel()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		statuses <- serviceStatus{State: stateStopped, Checkpoint: 3}
		if result != nil {
			return result
		}
		return err
	case <-timer.C:
		statuses <- serviceStatus{State: stateStopped, Checkpoint: 3}
		if result != nil {
			return errors.Join(result, errShutdownTimeout)
		}
		return errShutdownTimeout
	}
}
