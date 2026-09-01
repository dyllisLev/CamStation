package recorder

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestRecorderPausesBeforeStartingFFmpeg_whenDiskUsageIsAtLimit(t *testing.T) {
	// Given: the recording filesystem is already at the 90% stop threshold.
	root := t.TempDir()
	manager := New(nil, root, root, 1,
		WithDiskUsageChecker(func(string) (DiskUsage, error) {
			return DiskUsage{TotalBytes: 100, AvailableBytes: 10}, nil
		}),
		WithDiskCheckInterval(10*time.Millisecond),
	)
	t.Cleanup(manager.StopAll)

	// When: recording is started for a camera.
	if err := manager.Start(store.Camera{ID: 1, Name: "Front", StreamName: "front", Enabled: true}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Then: the worker stays paused and reports the disk guard instead of launching ffmpeg.
	status := waitForWorkerState(t, manager, "paused")
	if !strings.Contains(status.LastError, "recording disk usage") {
		t.Fatalf("LastError = %q, want disk guard message", status.LastError)
	}
}

func TestRecorderTerminatesRunningProcess_whenDiskUsageCrossesLimit(t *testing.T) {
	// Given: a recorder process is running and the disk checker crosses the 90% stop threshold.
	root := t.TempDir()
	checks := 0
	manager := New(nil, root, root, 1,
		WithDiskUsageChecker(func(string) (DiskUsage, error) {
			checks++
			if checks == 1 {
				return DiskUsage{TotalBytes: 100, AvailableBytes: 50}, nil
			}
			return DiskUsage{TotalBytes: 100, AvailableBytes: 9}, nil
		}),
		WithDiskCheckInterval(10*time.Millisecond),
	)
	worker := &worker{manager: manager, stop: make(chan struct{})}
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep command: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	// When: the worker monitors the process.
	err := worker.waitForProcess(cmd, nil, waitDone)

	// Then: the disk guard terminates the process and reports the disk-full sentinel.
	if !errors.Is(err, ErrRecordingDiskFull) {
		t.Fatalf("waitForProcess() error = %v, want ErrRecordingDiskFull", err)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
		t.Fatal("recorder process is still running, want terminated")
	}
}

func TestRecorderQuitsRunningProcessThroughStdinBeforeSignals(t *testing.T) {
	// Given: a recorder-like process ignores TERM but exits cleanly when it receives FFmpeg's q command.
	root := t.TempDir()
	manager := New(nil, root, root, 1)
	worker := &worker{manager: manager, stop: make(chan struct{})}
	cmd := exec.Command("sh", "-c", `trap '' TERM; IFS= read -r line; [ "$line" = q ]`)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("open command stdin: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start recorder-like command: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	close(worker.stop)

	// When: the worker owns the shutdown request.
	startedAt := time.Now()
	err = worker.waitForProcess(cmd, stdin, waitDone)

	// Then: q closes the process immediately and no signal timeout is consumed.
	if err != nil {
		t.Fatalf("waitForProcess() error = %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed >= time.Second {
		t.Fatalf("stdin shutdown took %s, want less than one second", elapsed)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
		t.Fatalf("process state = %#v, want clean exit", cmd.ProcessState)
	}
}

func waitForWorkerState(t *testing.T, manager *Manager, state string) WorkerStatus {
	t.Helper()

	deadline := time.After(time.Second)
	for {
		status := manager.Status()
		if len(status.Workers) == 1 && status.Workers[0].State == state {
			return status.Workers[0]
		}
		select {
		case <-deadline:
			t.Fatalf("worker did not reach state %q; last status = %#v", state, status)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
