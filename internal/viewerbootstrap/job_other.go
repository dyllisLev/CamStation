//go:build !windows

package viewerbootstrap

import "errors"

func NewPlatformAdapter() (ProcessAdapter, error) {
	return nil, errors.New("Viewer bootstrap is only available on Windows")
}
