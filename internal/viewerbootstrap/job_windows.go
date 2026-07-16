//go:build windows

package viewerbootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"camstation/internal/vieweragent"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

type windowsAdapter struct{}

func NewPlatformAdapter() (ProcessAdapter, error) { return windowsAdapter{}, nil }

func (windowsAdapter) CurrentViewer(ctx context.Context, installDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	current, err := vieweragent.LoadCurrentRelease(installDir)
	if err != nil {
		return "", err
	}
	if current.ViewerPath == "" {
		return "", errors.New("current Viewer path is missing")
	}
	info, err := os.Stat(current.ViewerPath)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("current Viewer is not a regular file")
	}
	return current.ViewerPath, nil
}

func (windowsAdapter) RequestGrant(ctx context.Context) (LaunchGrant, error) {
	connection, err := winio.DialPipeContext(ctx, vieweragent.ViewerPipeName)
	if err != nil {
		return LaunchGrant{}, err
	}
	defer connection.Close()
	deadline := time.Now().Add(5 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)
	pid := uint32(os.Getpid())
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(pid, &sessionID); err != nil {
		return LaunchGrant{}, err
	}
	request := vieweragent.PipeMessage{
		Version: vieweragent.PipeProtocolVersion, RequestID: fmt.Sprintf("bootstrap-%d-%d", pid, time.Now().UnixNano()),
		Type: "bootstrap_request", PID: int(pid), SessionID: sessionID,
	}
	if err := vieweragent.WritePipeMessage(connection, request); err != nil {
		return LaunchGrant{}, err
	}
	response, err := vieweragent.ReadPipeMessage(connection)
	if err != nil {
		return LaunchGrant{}, err
	}
	if response.RequestID != request.RequestID || response.Type != "bootstrap_grant" || response.Generation <= 0 || response.Nonce == "" {
		return LaunchGrant{}, errors.New("Agent rejected Viewer bootstrap")
	}
	return LaunchGrant{Generation: response.Generation, Nonce: response.Nonce, SessionID: sessionID}, nil
}

func (windowsAdapter) StartSuspended(ctx context.Context, spec LaunchSpec) (ManagedProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	application, err := windows.UTF16PtrFromString(spec.Executable)
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{spec.Executable}, spec.Args...)))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	currentDirectory, err := windows.UTF16PtrFromString(filepath.Dir(spec.Executable))
	if err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{}))}
	var info windows.ProcessInformation
	if err := windows.CreateProcess(application, commandLine, nil, nil, false,
		windows.CREATE_SUSPENDED|windows.CREATE_NEW_PROCESS_GROUP, nil, currentDirectory, &startup, &info); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	process := &windowsProcess{job: job, process: info.Process, thread: info.Thread, pid: info.ProcessId}
	if err := ctx.Err(); err != nil {
		_ = process.CloseJob()
		return nil, err
	}
	return process, nil
}

type windowsProcess struct {
	mu       sync.Mutex
	job      windows.Handle
	process  windows.Handle
	thread   windows.Handle
	pid      uint32
	assigned bool
}

func (process *windowsProcess) AssignKillOnCloseJob() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.job == 0 || process.process == 0 {
		return errors.New("Viewer process is closed")
	}
	if err := windows.AssignProcessToJobObject(process.job, process.process); err != nil {
		return err
	}
	process.assigned = true
	return nil
}

func (process *windowsProcess) Resume() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.thread == 0 {
		return errors.New("Viewer thread is closed")
	}
	_, err := windows.ResumeThread(process.thread)
	if err == nil {
		windows.CloseHandle(process.thread)
		process.thread = 0
	}
	return err
}

func (process *windowsProcess) Wait() error {
	process.mu.Lock()
	handle := process.process
	process.mu.Unlock()
	if handle == 0 {
		return errors.New("Viewer process is closed")
	}
	status, err := windows.WaitForSingleObject(handle, windows.INFINITE)
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("unexpected Viewer wait status %d", status)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("Viewer exited with code %d", exitCode)
	}
	return nil
}

func (process *windowsProcess) RequestStop() error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, process.pid)
}

func (process *windowsProcess) CloseJob() error {
	process.mu.Lock()
	defer process.mu.Unlock()
	var first error
	if !process.assigned && process.process != 0 {
		_ = windows.TerminateProcess(process.process, 1)
	}
	for _, handle := range []*windows.Handle{&process.job, &process.thread, &process.process} {
		if *handle != 0 {
			if err := windows.CloseHandle(*handle); err != nil && first == nil {
				first = err
			}
			*handle = 0
		}
	}
	return first
}
