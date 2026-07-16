package vieweragent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CurrentRelease struct {
	SchemaVersion int    `json:"schemaVersion"`
	AgentPath     string `json:"agentPath"`
}

const PlannedRestartExitCode = 75

type ChildExitKind string

const (
	ChildCrashed        ChildExitKind = "crashed"
	ChildPlannedRestart ChildExitKind = "planned_restart"
	ChildStopped        ChildExitKind = "stopped"
)

type ChildExit struct {
	Kind ChildExitKind
	Err  error
}

type HostChild interface {
	Wait() error
	RequestStop() error
	Kill() error
}

func LoadCurrentRelease(installDir string) (CurrentRelease, error) {
	var current CurrentRelease
	if err := readBoundedJSON(filepath.Join(installDir, "current.json"), &current); err != nil {
		return CurrentRelease{}, err
	}
	return ValidateCurrentRelease(installDir, current)
}

func ValidateCurrentRelease(installDir string, current CurrentRelease) (CurrentRelease, error) {
	installDir = filepath.Clean(installDir)
	if !filepath.IsAbs(installDir) || current.AgentPath == "" {
		return CurrentRelease{}, errors.New("invalid current release pointer")
	}
	agentPath := current.AgentPath
	if !filepath.IsAbs(agentPath) {
		agentPath = filepath.Join(installDir, agentPath)
	}
	agentPath = filepath.Clean(agentPath)
	relative, err := filepath.Rel(installDir, agentPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return CurrentRelease{}, errors.New("current Agent path escapes install directory")
	}
	if current.SchemaVersion != SchemaVersion {
		return CurrentRelease{}, errors.New("unsupported current release schema")
	}
	current.SchemaVersion = SchemaVersion
	current.AgentPath = agentPath
	return current, nil
}

func RunChildSupervisor(ctx context.Context, run func(context.Context) ChildExit, sleep func(context.Context, time.Duration) error) error {
	if run == nil || sleep == nil {
		return errors.New("child supervisor requires run and sleep functions")
	}
	delays := [...]time.Duration{5 * time.Second, 30 * time.Second, 120 * time.Second}
	var lastErr error
	crashRestarts := 0
	plannedReloaded := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		exit := run(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if exit.Kind == ChildPlannedRestart && !plannedReloaded {
			plannedReloaded = true
			continue
		}
		if exit.Kind == ChildPlannedRestart {
			exit = ChildExit{Kind: ChildCrashed, Err: errors.New("repeated planned restart")}
		}
		switch exit.Kind {
		case ChildStopped:
			return nil
		case ChildCrashed:
			lastErr = exit.Err
		}
		if crashRestarts == len(delays) {
			break
		}
		if err := sleep(ctx, delays[crashRestarts]); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		crashRestarts++
	}
	if lastErr == nil {
		return errors.New("versioned Agent exited")
	}
	return lastErr
}

func RunManagedChild(ctx context.Context, child HostChild, gracefulDeadline time.Duration) ChildExit {
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case err := <-done:
		return classifyChildExit(err)
	case <-ctx.Done():
		_ = child.RequestStop()
	}
	timer := time.NewTimer(gracefulDeadline)
	defer timer.Stop()
	select {
	case <-done:
		return ChildExit{Kind: ChildStopped}
	case <-timer.C:
		_ = child.Kill()
		killTimer := time.NewTimer(gracefulDeadline)
		defer killTimer.Stop()
		select {
		case <-done:
		case <-killTimer.C:
		}
		return ChildExit{Kind: ChildStopped}
	}
}

func classifyChildExit(err error) ChildExit {
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) && exitCoder.ExitCode() == PlannedRestartExitCode {
		return ChildExit{Kind: ChildPlannedRestart}
	}
	if err == nil {
		err = errors.New("versioned Agent exited")
	}
	return ChildExit{Kind: ChildCrashed, Err: err}
}

func LoadReadyRelease(installDir string) (CurrentRelease, error) {
	current, err := LoadCurrentRelease(installDir)
	if err != nil {
		return CurrentRelease{}, err
	}
	if err := EnsureCurrentAgentExists(current); err != nil {
		return CurrentRelease{}, err
	}
	return current, nil
}

func EstablishHostReadiness(load func() (CurrentRelease, error), start func(CurrentRelease) (HostChild, error), ready func()) (HostChild, error) {
	current, err := load()
	if err != nil {
		return nil, err
	}
	child, err := start(current)
	if err != nil {
		return nil, err
	}
	ready()
	return child, nil
}

func WaitForAgentReady(reader io.ReadCloser, timeout time.Duration) error {
	defer reader.Close()
	done := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(reader).ReadString('\n')
		if err == nil && line != "ready\n" {
			err = errors.New("invalid Agent readiness signal")
		}
		done <- err
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		_ = reader.Close()
		<-done
		return errors.New("Agent readiness timed out")
	}
}

func SleepContext(ctx context.Context, delay time.Duration) error { return waitContext(ctx, delay) }

func EnsureCurrentAgentExists(current CurrentRelease) error {
	info, err := os.Stat(current.AgentPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("current Agent is not a regular file")
	}
	return nil
}
