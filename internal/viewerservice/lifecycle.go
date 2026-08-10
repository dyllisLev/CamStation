package viewerservice

import (
	"context"
	"errors"
	"time"
)

const (
	ViewerRestartDeadline  = 45 * time.Second
	ServiceRestartDeadline = 45 * time.Second
)

var ErrInteractiveSessionUnavailable = errors.New("interactive session unavailable")

type ViewerLauncher interface {
	StartViewer(context.Context) error
}

type ServiceRestarter interface {
	RequestServiceRestart(context.Context) error
}

func (service *Service) restartViewer(ctx context.Context, command Command, operationKey string) error {
	restartCtx, cancel := context.WithTimeout(ctx, ViewerRestartDeadline)
	defer cancel()
	server := service.server()
	previous, hadViewer := server.leases.Token()
	if hadViewer {
		if err := service.executeViewerIPCCommand(restartCtx, command, operationKey); err != nil {
			return err
		}
	} else {
		launcher := service.ViewerLauncher
		if launcher == nil {
			launcher = newPlatformViewerLauncher()
		}
		if launcher == nil {
			return RejectCommand("interactive_session_unavailable")
		}
		if err := launcher.StartViewer(restartCtx); err != nil {
			if errors.Is(err, ErrInteractiveSessionUnavailable) {
				return RejectCommand("interactive_session_unavailable")
			}
			return FailCommand("viewer_start_failed")
		}
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		token, available := server.leases.Token()
		if available && (!hadViewer || token != previous) {
			status, err := server.Snapshot(restartCtx)
			if err == nil && status.Viewer == "running" && status.Renderer == "ready" {
				return nil
			}
		}
		select {
		case <-restartCtx.Done():
			return FailCommand("viewer_restart_timeout")
		case <-ticker.C:
		}
	}
}

func (service *Service) restartService(ctx context.Context, _ string) error {
	restarter := service.ServiceRestarter
	if restarter == nil {
		restarter = newPlatformServiceRestarter()
	}
	if restarter == nil {
		return RejectCommand("service_restart_unavailable")
	}
	restartCtx, cancel := context.WithTimeout(ctx, ServiceRestartDeadline)
	defer cancel()
	if err := restarter.RequestServiceRestart(restartCtx); err != nil {
		return FailCommand("service_restart_failed")
	}
	return ErrServiceRestartRequested
}
