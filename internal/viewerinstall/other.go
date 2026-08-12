//go:build !windows

package viewerinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func RegisterAll(context.Context, Layout, RegistrationOptions) (string, error) {
	return "", errors.New("Windows registration requires Windows")
}

func RegisterRuntime(context.Context, Layout, RegistrationOptions) (string, error) {
	return "", errors.New("Windows registration requires Windows")
}

func RegisterUninstall(context.Context, Layout) error {
	return errors.New("Windows registration requires Windows")
}

func UnregisterAll(context.Context, Layout) error {
	return errors.New("Windows registration requires Windows")
}

func ActiveConsoleUserSID(context.Context) (string, error) {
	return "", errors.New("active console SID requires Windows")
}

func stopRegistered(context.Context, Layout) error           { return nil }
func disableAndStopRegistered(context.Context, Layout) error { return nil }
func enableRegistered(context.Context, Layout) error         { return nil }
func startRegistered(context.Context, Layout) error          { return nil }
func validateRegistered(context.Context, Layout) error       { return nil }

type Ownership struct {
	file     *os.File
	stateDir string
}

func Acquire(layout Layout) (*Ownership, error) {
	if err := layout.Ensure(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(layout.LockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrUpdateOwned
		}
		return nil, err
	}
	return &Ownership{file: file, stateDir: filepath.Clean(layout.StateDir)}, nil
}

func (owner *Ownership) Close() error {
	if owner == nil || owner.file == nil {
		return nil
	}
	file := owner.file
	owner.file = nil
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return errors.Join(err, file.Close())
}

func (owner *Ownership) owns(layout Layout) bool {
	return owner != nil && owner.file != nil && owner.stateDir == filepath.Clean(layout.StateDir)
}
