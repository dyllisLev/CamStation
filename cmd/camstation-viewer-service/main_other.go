//go:build !windows

package main

import (
	"errors"

	"camstation/internal/viewerservice"
)

func runPlatform(bool) error {
	return errors.Join(errors.New("CamStation Viewer Service is unsupported on this platform"), viewerservice.ErrUnsupportedPlatform)
}
