//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"

	"camstation/internal/viewerservice"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

type scmHandler struct{}

func runPlatform(console bool) error {
	if console {
		if !windows.GetCurrentProcessToken().IsElevated() {
			return errors.New("--console requires an elevated Windows terminal")
		}
		runtime, err := viewerservice.NewRuntimeService(viewerservice.RegistryStore{})
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		return runtime.Run(ctx)
	}
	return svc.Run(serviceName, scmHandler{})
}

func (scmHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	runtime, err := viewerservice.NewRuntimeService(viewerservice.RegistryStore{})
	if err != nil {
		changes <- svc.Status{State: svc.StartPending, CheckPoint: 1, WaitHint: 5000}
		changes <- svc.Status{State: svc.Stopped, CheckPoint: 2}
		return true, 1
	}
	commands := make(chan serviceCommand)
	statuses := make(chan serviceStatus, 8)
	finished := make(chan struct{})
	var requestAdapter sync.WaitGroup
	requestAdapter.Add(1)
	go func() {
		defer requestAdapter.Done()
		for {
			select {
			case request := <-requests:
				var command serviceCommand
				switch request.Cmd {
				case svc.Stop:
					command = commandStop
				case svc.Shutdown:
					command = commandShutdown
				case svc.PreShutdown:
					command = commandPreShutdown
				default:
					continue
				}
				select {
				case commands <- command:
				case <-finished:
					return
				}
			case <-finished:
				return
			}
		}
	}()
	serviceDone := make(chan error, 1)
	go func() {
		serviceDone <- superviseService(runtime.Run, runtime.Ready(), commands, statuses, serviceShutdownTimeout)
	}()
	for status := range statuses {
		changes <- scmStatus(status)
		if status.State == stateStopped {
			break
		}
	}
	err = <-serviceDone
	close(finished)
	requestAdapter.Wait()
	if err != nil {
		return true, 1
	}
	return false, 0
}

func scmStatus(status serviceStatus) svc.Status {
	result := svc.Status{CheckPoint: status.Checkpoint, WaitHint: uint32(status.WaitHint.Milliseconds())}
	switch status.State {
	case stateStartPending:
		result.State = svc.StartPending
	case stateRunning:
		result.State = svc.Running
		result.Accepts = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPreShutdown
	case stateStopPending:
		result.State = svc.StopPending
	case stateStopped:
		result.State = svc.Stopped
	}
	return result
}
