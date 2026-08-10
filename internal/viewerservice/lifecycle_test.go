package viewerservice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type viewerLauncherFunc func(context.Context) error

func (launch viewerLauncherFunc) StartViewer(ctx context.Context) error { return launch(ctx) }

type serviceRestarterFunc func(context.Context) error

func (restart serviceRestarterFunc) RequestServiceRestart(ctx context.Context) error {
	return restart(ctx)
}

func TestRestartViewerLaunchesClosedViewerAndWaitsForReadyLease(t *testing.T) {
	leases := NewLeaseManager(time.Now, time.Minute)
	server := NewServer(ConfigManager{Store: &memoryConfigStore{config: testMachineConfig()}}, leases, "2.0.22", nil)
	launches := 0
	service := &Service{
		Server: server,
		ViewerLauncher: viewerLauncherFunc(func(context.Context) error {
			launches++
			if _, err := leases.Acquire("new-viewer", Peer{PID: 22, SessionID: 1, Interactive: true}); err != nil {
				return err
			}
			server.setViewerState("running", "ready")
			return nil
		}),
	}
	command := Command{ID: 51, Type: "restart_viewer", PayloadHash: "restart-viewer"}
	if err := service.restartViewer(t.Context(), command, "command-51"); err != nil {
		t.Fatal(err)
	}
	if launches != 1 {
		t.Fatalf("launches=%d want=1", launches)
	}
}

func TestRestartViewerReplacesActiveLeaseAndWaitsForRenderer(t *testing.T) {
	leases := NewLeaseManager(time.Now, time.Minute)
	server := NewServer(ConfigManager{Store: &memoryConfigStore{config: testMachineConfig()}}, leases, "2.0.22", nil)
	peer := Peer{PID: 21, SessionID: 1, Interactive: true}
	oldLease, err := leases.Acquire("old-viewer", peer)
	if err != nil {
		t.Fatal(err)
	}
	server.setViewerState("running", "ready")
	events := make(chan queuedCommand, 1)
	service := &Service{
		Server: server,
		byID: map[string]*serviceConnection{
			"old-viewer": {id: "old-viewer", commands: events},
		},
	}
	done := make(chan error, 1)
	go func() {
		item := <-events
		var payload viewerCommandEvent
		if err := json.Unmarshal(item.event.Payload, &payload); err != nil {
			done <- err
			return
		}
		if payload.Type != "restart_viewer" || payload.OperationKey != "command-52" {
			done <- errors.New("unexpected restart event")
			return
		}
		if err := service.acceptCommandResult(LocalCommandResult{
			LeaseID: oldLease.ID, OperationKey: payload.OperationKey, Succeeded: true,
		}); err != nil {
			done <- err
			return
		}
		if err := leases.Release("old-viewer", oldLease.ID, peer); err != nil {
			done <- err
			return
		}
		if _, err := leases.Acquire("new-viewer", Peer{PID: 22, SessionID: 1, Interactive: true}); err != nil {
			done <- err
			return
		}
		server.setViewerState("running", "ready")
		done <- nil
	}()
	command := Command{ID: 52, Type: "restart_viewer", PayloadHash: "restart-active"}
	if err := service.restartViewer(t.Context(), command, "command-52"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRestartServiceRequiresSuccessfulDetachedHandoff(t *testing.T) {
	calls := 0
	service := &Service{ServiceRestarter: serviceRestarterFunc(func(context.Context) error {
		calls++
		return nil
	})}
	if err := service.restartService(t.Context(), "service-generation-2"); !errors.Is(err, ErrServiceRestartRequested) {
		t.Fatalf("restart result=%v", err)
	}
	if calls != 1 {
		t.Fatalf("restart calls=%d want=1", calls)
	}

	service.ServiceRestarter = serviceRestarterFunc(func(context.Context) error { return errors.New("SCM unavailable") })
	if err := service.restartService(t.Context(), "service-generation-3"); err == nil || err.Error() != "service_restart_failed" {
		t.Fatalf("failed restart result=%v", err)
	}
}
