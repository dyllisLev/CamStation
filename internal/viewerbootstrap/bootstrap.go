package viewerbootstrap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	GracefulShutdownDeadline = 5 * time.Second
	TotalRecoveryDeadline    = 45 * time.Second
)

type LaunchGrant struct {
	Generation int64
	Nonce      string
	SessionID  uint32
}

type LaunchSpec struct {
	Executable string
	Args       []string
}

type ProcessAdapter interface {
	CurrentViewer(context.Context, string) (string, error)
	RequestGrant(context.Context) (LaunchGrant, error)
	StartSuspended(context.Context, LaunchSpec) (ManagedProcess, error)
}

type ManagedProcess interface {
	AssignKillOnCloseJob() error
	Resume() error
	Wait() error
	RequestStop() error
	CloseJob() error
}

type GenerationGate struct {
	mu       sync.Mutex
	accepted bool
}

func (gate *GenerationGate) Accept(generation int64) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if generation <= 0 || gate.accepted {
		return false
	}
	gate.accepted = true
	return true
}

func BuildLaunchSpec(installDir, viewerPath string, grant LaunchGrant) (LaunchSpec, error) {
	installDir = filepath.Clean(strings.TrimSpace(installDir))
	if !filepath.IsAbs(installDir) || grant.Generation <= 0 || grant.Nonce == "" || len(grant.Nonce) > 256 || strings.ContainsAny(grant.Nonce, "\r\n\t") {
		return LaunchSpec{}, errors.New("invalid Viewer launch configuration")
	}
	executable := viewerPath
	if !filepath.IsAbs(executable) {
		executable = filepath.Join(installDir, executable)
	}
	executable = filepath.Clean(executable)
	relative, err := filepath.Rel(installDir, executable)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return LaunchSpec{}, errors.New("Viewer executable escapes install directory")
	}
	return LaunchSpec{
		Executable: executable,
		Args: []string{
			"--agent-generation=" + strconv.FormatInt(grant.Generation, 10),
			"--agent-nonce=" + grant.Nonce,
			"--agent-session=" + strconv.FormatUint(uint64(grant.SessionID), 10),
		},
	}, nil
}

func Run(ctx context.Context, installDir string, adapter ProcessAdapter) error {
	return RunWithDeadlines(ctx, installDir, adapter, GracefulShutdownDeadline, TotalRecoveryDeadline)
}

func RunWithDeadlines(ctx context.Context, installDir string, adapter ProcessAdapter, gracefulDeadline, totalDeadline time.Duration) error {
	if adapter == nil || gracefulDeadline <= 0 || totalDeadline <= 0 {
		return errors.New("invalid Viewer bootstrap policy")
	}
	setupCtx, cancelSetup := context.WithTimeout(ctx, totalDeadline)
	viewerPath, err := adapter.CurrentViewer(setupCtx, installDir)
	if err != nil {
		cancelSetup()
		return err
	}
	grant, err := adapter.RequestGrant(setupCtx)
	if err != nil {
		cancelSetup()
		return err
	}
	var gate GenerationGate
	if !gate.Accept(grant.Generation) {
		cancelSetup()
		return errors.New("Agent launch generation was already accepted")
	}
	spec, err := BuildLaunchSpec(installDir, viewerPath, grant)
	if err != nil {
		cancelSetup()
		return err
	}
	process, err := adapter.StartSuspended(setupCtx, spec)
	if err != nil {
		cancelSetup()
		return err
	}
	defer process.CloseJob()
	if err := process.AssignKillOnCloseJob(); err != nil {
		cancelSetup()
		return fmt.Errorf("assign Viewer Job: %w", err)
	}
	if err := process.Resume(); err != nil {
		cancelSetup()
		return fmt.Errorf("resume Viewer: %w", err)
	}
	cancelSetup()

	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = process.RequestStop()
	}
	timer := time.NewTimer(gracefulDeadline)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return nil
	}
}
