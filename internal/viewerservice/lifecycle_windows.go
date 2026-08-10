//go:build windows

package viewerservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const installedViewerServiceName = "CamStationViewerService"

var (
	wtsAPI32                    = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQueryUserToken       = wtsAPI32.NewProc("WTSQueryUserToken")
	userEnv                     = windows.NewLazySystemDLL("userenv.dll")
	procCreateEnvironmentBlock  = userEnv.NewProc("CreateEnvironmentBlock")
	procDestroyEnvironmentBlock = userEnv.NewProc("DestroyEnvironmentBlock")
)

type windowsViewerLauncher struct{}

type windowsServiceRestarter struct{}

func newPlatformViewerLauncher() ViewerLauncher { return windowsViewerLauncher{} }

func newPlatformServiceRestarter() ServiceRestarter { return windowsServiceRestarter{} }

func (windowsViewerLauncher) StartViewer(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	executable, err := installedAdjacentExecutable("CamStationViewer.exe")
	if err != nil {
		return err
	}
	sessionID := windows.WTSGetActiveConsoleSessionId()
	if sessionID == 0xffffffff {
		return ErrInteractiveSessionUnavailable
	}
	var token windows.Token
	result, _, callErr := procWTSQueryUserToken.Call(uintptr(sessionID), uintptr(unsafe.Pointer(&token)))
	if result == 0 {
		if callErr == windows.ERROR_NO_TOKEN {
			return ErrInteractiveSessionUnavailable
		}
		return fmt.Errorf("query active console token: %w", callErr)
	}
	defer token.Close()

	var environment uintptr
	result, _, callErr = procCreateEnvironmentBlock.Call(uintptr(unsafe.Pointer(&environment)), uintptr(token), 0)
	if result == 0 {
		return fmt.Errorf("create user environment: %w", callErr)
	}
	defer procDestroyEnvironmentBlock.Call(environment)

	application, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	directory, err := windows.UTF16PtrFromString(filepath.Dir(executable))
	if err != nil {
		return err
	}
	desktop, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		return err
	}
	startup := windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfo{})), Desktop: desktop}
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP)
	if err := windows.CreateProcessAsUser(token, application, nil, nil, nil, false, flags,
		(*uint16)(unsafe.Pointer(environment)), directory, &startup, &process); err != nil {
		return fmt.Errorf("start installed Viewer in console session: %w", err)
	}
	windows.CloseHandle(process.Thread)
	windows.CloseHandle(process.Process)
	return nil
}

func (windowsServiceRestarter) RequestServiceRestart(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	executable, err := installedAdjacentExecutable("CamStationViewerService.exe")
	if err != nil {
		return err
	}
	command := exec.Command(executable, "--restart-service-helper")
	command.Dir = filepath.Dir(executable)
	command.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start service restart helper: %w", err)
	}
	return command.Process.Release()
}

func RunServiceRestartHelper(ctx context.Context) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(installedViewerServiceName)
	if err != nil {
		return fmt.Errorf("open Viewer service: %w", err)
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query Viewer service: %w", err)
	}
	if status.State != svc.Stopped {
		if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("stop Viewer service: %w", err)
		}
		if err := waitForServiceState(ctx, service, svc.Stopped); err != nil {
			return err
		}
	}
	if err := service.Start(); err != nil {
		return fmt.Errorf("start Viewer service: %w", err)
	}
	return waitForServiceState(ctx, service, svc.Running)
}

func waitForServiceState(ctx context.Context, service *mgr.Service, expected svc.State) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := service.Query()
		if err != nil {
			return err
		}
		if status.State == expected {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for Viewer service state %d: %w", expected, ctx.Err())
		case <-ticker.C:
		}
	}
}

func installedAdjacentExecutable(name string) (string, error) {
	current, err := os.Executable()
	if err != nil {
		return "", err
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Base(current), "CamStationViewerService.exe") {
		return "", errors.New("Viewer service executable is not installed under its fixed name")
	}
	target := filepath.Clean(filepath.Join(filepath.Dir(current), name))
	if filepath.Dir(target) != filepath.Dir(current) || !strings.EqualFold(filepath.Base(target), name) {
		return "", errors.New("invalid adjacent Viewer executable")
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("installed Viewer target is not a regular file")
	}
	return target, nil
}
