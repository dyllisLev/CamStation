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
	WaitReady(context.Context, int64) error
	RelaunchAuthorized(context.Context, int64) (bool, error)
}

type ManagedProcess interface {
	AssignKillOnCloseJob() error
	Resume() error
	Wait() error
	RequestStop() error
	TerminateJob() error
	Dispose() error
}

type GenerationGate struct {
	mu   sync.Mutex
	last int64
}

func (gate *GenerationGate) Accept(generation int64) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if generation <= gate.last {
		return false
	}
	gate.last = generation
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

// ResolveViewerPath resolves symlinks/reparse points before enforcing that the
// executable remains beneath the installed release root.
func ResolveViewerPath(installDir, viewerPath string) (string, error) {
	installDir = filepath.Clean(strings.TrimSpace(installDir))
	viewerPath = filepath.Clean(strings.TrimSpace(viewerPath))
	if !filepath.IsAbs(installDir) || viewerPath == "." {
		return "", errors.New("invalid Viewer path")
	}
	if !filepath.IsAbs(viewerPath) {
		viewerPath = filepath.Join(installDir, viewerPath)
	}
	resolvedRoot, err := filepath.EvalSymlinks(installDir)
	if err != nil {
		return "", fmt.Errorf("resolve install directory: %w", err)
	}
	resolvedViewer, err := filepath.EvalSymlinks(viewerPath)
	if err != nil {
		return "", fmt.Errorf("resolve Viewer executable: %w", err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedViewer)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("Viewer executable escapes resolved install directory")
	}
	return resolvedViewer, nil
}

func Run(ctx context.Context, installDir string, adapter ProcessAdapter) error {
	return RunWithDeadlines(ctx, installDir, adapter, GracefulShutdownDeadline, TotalRecoveryDeadline)
}

func RunWithDeadlines(ctx context.Context, installDir string, adapter ProcessAdapter, gracefulDeadline, totalDeadline time.Duration) error {
	if adapter == nil || gracefulDeadline <= 0 || totalDeadline <= 0 {
		return errors.New("invalid Viewer bootstrap policy")
	}
	var gate GenerationGate
	var recoveryDeadline time.Time
	for {
		startupBudget := totalDeadline
		if !recoveryDeadline.IsZero() {
			startupBudget = time.Until(recoveryDeadline)
			recoveryDeadline = time.Time{}
			if startupBudget <= 0 {
				return errors.New("Viewer recovery timed out before relaunch")
			}
		}
		generation, childErr, authorized, err := runGeneration(ctx, installDir, adapter, &gate, gracefulDeadline, startupBudget)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		recoveryDeadline = time.Now().Add(totalDeadline)
		if !authorized {
			recoveryCtx, cancelRecovery := context.WithDeadline(ctx, recoveryDeadline)
			authorized, err = adapter.RelaunchAuthorized(recoveryCtx, generation)
			cancelRecovery()
			if err != nil {
				return err
			}
		}
		if !authorized {
			if childErr != nil {
				return fmt.Errorf("Viewer exited: %w", childErr)
			}
			return errors.New("Viewer exited without an authorized next generation")
		}
	}
}

func runGeneration(
	ctx context.Context,
	installDir string,
	adapter ProcessAdapter,
	gate *GenerationGate,
	gracefulDeadline time.Duration,
	totalDeadline time.Duration,
) (int64, error, bool, error) {
	setupCtx, cancelSetup := context.WithTimeout(ctx, totalDeadline)
	defer cancelSetup()
	viewerPath, err := adapter.CurrentViewer(setupCtx, installDir)
	if err != nil {
		return 0, nil, false, err
	}
	grant, err := adapter.RequestGrant(setupCtx)
	if err != nil {
		return 0, nil, false, err
	}
	if !gate.Accept(grant.Generation) {
		return 0, nil, false, errors.New("Agent launch generation is not strictly increasing")
	}
	spec, err := BuildLaunchSpec(installDir, viewerPath, grant)
	if err != nil {
		return 0, nil, false, err
	}
	process, err := adapter.StartSuspended(setupCtx, spec)
	if err != nil {
		return 0, nil, false, err
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	if err := process.AssignKillOnCloseJob(); err != nil {
		_ = process.TerminateJob()
		waitAndDispose(process, done, setupCtx, gracefulDeadline)
		return 0, nil, false, fmt.Errorf("assign Viewer Job: %w", err)
	}
	if err := process.Resume(); err != nil {
		_ = process.TerminateJob()
		waitAndDispose(process, done, setupCtx, gracefulDeadline)
		return 0, nil, false, fmt.Errorf("resume Viewer: %w", err)
	}

	ready := make(chan error, 1)
	go func() { ready <- adapter.WaitReady(setupCtx, grant.Generation) }()
	select {
	case childErr := <-done:
		_ = process.TerminateJob()
		_ = process.Dispose()
		return grant.Generation, childErr, false, errors.New("Viewer exited before renderer readiness")
	case readyErr := <-ready:
		if readyErr != nil {
			_ = process.RequestStop()
			_ = process.TerminateJob()
			waitAndDispose(process, done, setupCtx, gracefulDeadline)
			return grant.Generation, nil, false, fmt.Errorf("Viewer readiness failed: %w", readyErr)
		}
		cancelSetup()
	case <-setupCtx.Done():
		_ = process.RequestStop()
		_ = process.TerminateJob()
		waitAndDispose(process, done, setupCtx, gracefulDeadline)
		return grant.Generation, nil, false, errors.New("Viewer startup timed out")
	}

	type relaunchResult struct {
		authorized bool
		err        error
	}
	relaunchCtx, cancelRelaunch := context.WithCancel(ctx)
	defer cancelRelaunch()
	relaunch := make(chan relaunchResult, 1)
	go func() {
		authorized, err := adapter.RelaunchAuthorized(relaunchCtx, grant.Generation)
		relaunch <- relaunchResult{authorized: authorized, err: err}
	}()
	for {
		select {
		case childErr := <-done:
			_ = process.TerminateJob()
			_ = process.Dispose()
			return grant.Generation, childErr, false, nil
		case result := <-relaunch:
			if result.err != nil {
				if ctx.Err() != nil {
					relaunch = nil
					continue
				}
				return grant.Generation, nil, false, result.err
			}
			if !result.authorized {
				relaunch = nil
				continue
			}
			_ = process.RequestStop()
			timer := time.NewTimer(gracefulDeadline)
			select {
			case childErr := <-done:
				timer.Stop()
				_ = process.TerminateJob()
				_ = process.Dispose()
				return grant.Generation, childErr, true, nil
			case <-timer.C:
				_ = process.TerminateJob()
				waitAndDispose(process, done, context.Background(), gracefulDeadline)
				return grant.Generation, nil, true, nil
			}
		case <-ctx.Done():
			_ = process.RequestStop()
			timer := time.NewTimer(gracefulDeadline)
			select {
			case <-done:
				timer.Stop()
				_ = process.TerminateJob()
				_ = process.Dispose()
				return grant.Generation, nil, false, nil
			case <-timer.C:
				_ = process.TerminateJob()
				waitAndDispose(process, done, context.Background(), gracefulDeadline)
				return grant.Generation, nil, false, nil
			}
		}
	}
}

func waitAndDispose(process ManagedProcess, done <-chan error, ctx context.Context, maximum time.Duration) {
	timer := time.NewTimer(maximum)
	defer timer.Stop()
	select {
	case <-done:
		_ = process.Dispose()
	case <-ctx.Done():
		go disposeAfterWait(process, done)
	case <-timer.C:
		go disposeAfterWait(process, done)
	}
}

func disposeAfterWait(process ManagedProcess, done <-chan error) {
	<-done
	_ = process.Dispose()
}
