//go:build !windows

package main

import (
	"errors"
	"os"
	"time"

	"camstation/internal/viewerinstall"
)

func ensureElevated([]string) (bool, error) { return false, nil }
func waitParent(int, time.Duration) error   { return nil }
func removeInstallation(layout viewerinstall.Layout) error {
	return errors.Join(removeAll(layout.InstallDir), removeAll(layout.StateDir))
}

func removeAll(path string) error { return os.RemoveAll(path) }
