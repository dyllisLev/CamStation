//go:build linux || darwin

package ffmpegsupervisor

import (
	"errors"
	"os"
	"syscall"
)

func acquireSlot(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	for i := 0; i < 3; i++ {
		f, err := os.OpenFile(slotName(dir, i), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return f, nil
		}
		f.Close()
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, err
		}
	}
	return nil, errCapacity
}
func processAlive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func acquire(dir string) (func(), error) {
	f, err := acquireSlot(dir)
	if err != nil {
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
