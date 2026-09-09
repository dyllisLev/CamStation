//go:build !linux && !darwin

package ffmpegsupervisor

import (
	"errors"
	"os"
)

func acquire(dir string) (func(), error) {
	return nil, errors.New("NVENC process admission unavailable on platform")
}
func processAlive(pid int) bool { return false }

func acquireSlot(dir string) (*os.File, error) {
	return nil, errors.New("NVENC process admission unavailable on platform")
}
