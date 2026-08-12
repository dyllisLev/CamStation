//go:build !windows

package main

import (
	"errors"

	"camstation/internal/viewerservice"
)

func runPlatform(bool) error {
	return errors.Join(errors.New("CamStation Viewer Service is unsupported on this platform"), viewerservice.ErrUnsupportedPlatform)
}

func runRestartHelperPlatform() error {
	return errors.Join(errors.New("Viewer service restart helper is unsupported on this platform"), viewerservice.ErrUnsupportedPlatform)
}
