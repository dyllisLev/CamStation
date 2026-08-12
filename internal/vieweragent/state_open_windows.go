//go:build windows

package vieweragent

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func openStateFile(path string) (*os.File, error) {
	return openStateFileWithRetry(
		func() (*os.File, error) { return os.Open(path) },
		func(err error) bool {
			return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
		},
		time.Sleep,
	)
}
