package vieweragent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CurrentRelease struct {
	SchemaVersion int    `json:"schemaVersion"`
	AgentPath     string `json:"agentPath"`
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

func RunChildSupervisor(ctx context.Context, run func(context.Context) error, sleep func(context.Context, time.Duration) error) error {
	if run == nil || sleep == nil {
		return errors.New("child supervisor requires run and sleep functions")
	}
	delays := [...]time.Duration{5 * time.Second, 30 * time.Second, 120 * time.Second}
	var lastErr error
	for attempt := 0; attempt <= len(delays); attempt++ {
		if ctx.Err() != nil {
			return nil
		}
		lastErr = run(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if attempt == len(delays) {
			break
		}
		if err := sleep(ctx, delays[attempt]); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
	if lastErr == nil {
		return errors.New("versioned Agent exited")
	}
	return lastErr
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
