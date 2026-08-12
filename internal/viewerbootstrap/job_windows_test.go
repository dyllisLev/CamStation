//go:build windows

package viewerbootstrap

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsJobTerminationPreservesProcessHandleForPendingWait(t *testing.T) {
	const pendingProcess windows.Handle = 123
	process := &windowsProcess{assigned: true, process: pendingProcess}
	if err := process.TerminateJob(); err != nil {
		t.Fatal(err)
	}
	if process.process != pendingProcess {
		t.Fatal("Job termination disposed the process handle before Wait completed")
	}
}
