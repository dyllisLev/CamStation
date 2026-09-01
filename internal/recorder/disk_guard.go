package recorder

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"

	"camstation/internal/opslog"
)

const (
	statusPaused               = "paused"
	defaultDiskStopUsedPercent = 90.0
	defaultDiskCheckInterval   = 15 * time.Second
	recorderQuitTimeout        = 10 * time.Second
	recorderTerminateTimeout   = 10 * time.Second
)

var ErrRecordingDiskFull = errors.New("recording disk usage limit reached")

type DiskUsage struct {
	TotalBytes     uint64
	AvailableBytes uint64
}

type DiskUsageChecker func(path string) (DiskUsage, error)

type Option func(*Manager)

func WithLogger(logger *opslog.Logger) Option {
	return func(manager *Manager) {
		manager.logger = logger
	}
}

type diskGuard struct {
	check           DiskUsageChecker
	stopUsedPercent float64
	checkInterval   time.Duration
}

func WithDiskUsageChecker(check DiskUsageChecker) Option {
	return func(m *Manager) {
		m.diskGuard.check = check
	}
}

func WithDiskCheckInterval(interval time.Duration) Option {
	return func(m *Manager) {
		if interval > 0 {
			m.diskGuard.checkInterval = interval
		}
	}
}

func defaultDiskGuard() diskGuard {
	return diskGuard{
		check:           statfsDiskUsage,
		stopUsedPercent: defaultDiskStopUsedPercent,
		checkInterval:   defaultDiskCheckInterval,
	}
}

func (m *Manager) checkDiskCapacity() error {
	if m.diskGuard.check == nil || m.diskGuard.stopUsedPercent <= 0 {
		return nil
	}
	for _, path := range m.diskGuardPaths() {
		usage, err := m.diskGuard.check(path)
		if err != nil {
			return fmt.Errorf("check recording disk usage: %w", err)
		}
		usedPercent := usage.UsedPercent()
		if usedPercent >= m.diskGuard.stopUsedPercent {
			return fmt.Errorf("recording disk usage %.1f%% is at or above %.1f%%: %w", usedPercent, m.diskGuard.stopUsedPercent, ErrRecordingDiskFull)
		}
	}
	return nil
}

func (m *Manager) diskGuardPaths() []string {
	if m.recordingsDir == m.tempDir {
		return []string{m.tempDir}
	}
	return []string{m.tempDir, m.recordingsDir}
}

func (u DiskUsage) UsedPercent() float64 {
	if u.TotalBytes == 0 {
		return 100
	}
	if u.AvailableBytes >= u.TotalBytes {
		return 0
	}
	usedBytes := u.TotalBytes - u.AvailableBytes
	return float64(usedBytes) * 100 / float64(u.TotalBytes)
}

func statfsDiskUsage(path string) (DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskUsage{}, err
	}
	blockSize := uint64(stat.Bsize)
	return DiskUsage{
		TotalBytes:     stat.Blocks * blockSize,
		AvailableBytes: stat.Bavail * blockSize,
	}, nil
}

func (w *worker) waitForProcess(cmd *exec.Cmd, stdin io.WriteCloser, waitDone <-chan error) error {
	if stdin != nil {
		defer stdin.Close()
	}
	ticker := time.NewTicker(w.manager.diskGuard.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			_ = stopRecorderProcess(cmd, stdin, waitDone)
			return nil
		case err := <-waitDone:
			return err
		case <-ticker.C:
			if err := w.manager.checkDiskCapacity(); err != nil {
				_ = stopRecorderProcess(cmd, stdin, waitDone)
				return err
			}
		}
	}
}

func stopRecorderProcess(cmd *exec.Cmd, stdin io.WriteCloser, waitDone <-chan error) error {
	if stdin != nil {
		_, writeErr := io.WriteString(stdin, "q\n")
		_ = stdin.Close()
		if writeErr == nil {
			if err, exited := waitForProcessExit(waitDone, recorderQuitTimeout); exited {
				return err
			}
		}
	}

	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	if err, exited := waitForProcessExit(waitDone, recorderTerminateTimeout); exited {
		return err
	}

	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return <-waitDone
}

func waitForProcessExit(waitDone <-chan error, timeout time.Duration) (error, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-waitDone:
		return err, true
	case <-timer.C:
		return nil, false
	}
}
