//go:build windows

package viewerinstall

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

type Ownership struct {
	handle       windows.Handle
	stateDir     string
	threadLocked bool
	processGuard bool
}

var processTransactionOwned atomic.Bool

func Acquire(layout Layout) (*Ownership, error) {
	if !processTransactionOwned.CompareAndSwap(false, true) {
		return nil, ErrUpdateOwned
	}
	// Windows mutex ownership belongs to an OS thread, not a goroutine.
	// Every production caller defers Close on this same goroutine.
	runtime.LockOSThread()
	name, err := windows.UTF16PtrFromString(`Global\CamStationViewerUpdate`)
	if err != nil {
		runtime.UnlockOSThread()
		processTransactionOwned.Store(false)
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		runtime.UnlockOSThread()
		processTransactionOwned.Store(false)
		return nil, err
	}
	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		windows.CloseHandle(handle)
		runtime.UnlockOSThread()
		processTransactionOwned.Store(false)
		if err == nil {
			err = ErrUpdateOwned
		}
		return nil, err
	}
	owner := &Ownership{handle: handle, stateDir: filepath.Clean(layout.StateDir), threadLocked: true, processGuard: true}
	if err := layout.Ensure(); err != nil {
		_ = owner.Close()
		return nil, err
	}
	return owner, nil
}

func (owner *Ownership) Close() error {
	if owner == nil || owner.handle == 0 {
		return nil
	}
	handle := owner.handle
	owner.handle = 0
	err := errors.Join(windows.ReleaseMutex(handle), windows.CloseHandle(handle))
	if owner.threadLocked {
		owner.threadLocked = false
		runtime.UnlockOSThread()
	}
	if owner.processGuard {
		owner.processGuard = false
		processTransactionOwned.Store(false)
	}
	return err
}

func (owner *Ownership) owns(layout Layout) bool {
	return owner != nil && owner.handle != 0 && owner.processGuard && owner.stateDir == filepath.Clean(layout.StateDir)
}

func RegisterAll(ctx context.Context, layout Layout, options RegistrationOptions) (string, error) {
	options.Staged = false
	serviceSID, err := RegisterRuntime(ctx, layout, options)
	if err != nil {
		return "", errors.Join(err, UnregisterAll(ctx, layout))
	}
	if err := RegisterUninstall(ctx, layout); err != nil {
		return "", errors.Join(err, UnregisterAll(ctx, layout))
	}
	return serviceSID, nil
}

func RegisterRuntime(ctx context.Context, layout Layout, options RegistrationOptions) (string, error) {
	if err := layout.Ensure(); err != nil {
		return "", err
	}
	if !validTaskSID(options.MonitoringUserSID) {
		return "", errors.New("valid monitoring user SID is required")
	}
	startMode := "auto"
	if options.Staged {
		startMode = "disabled"
	}
	if _, err := runWindows(ctx, "sc.exe", "create", ServiceName, "binPath=", serviceBinaryPath(layout), "start=", startMode, "obj=", "LocalSystem", "DisplayName=", "CamStation Viewer Agent"); err != nil {
		// A repair may encounter an existing service; sc config below is authoritative.
		if !strings.Contains(strings.ToLower(err.Error()), "1073") && !strings.Contains(strings.ToLower(err.Error()), "exists") {
			return "", err
		}
	}
	if _, err := runWindows(ctx, "sc.exe", "config", ServiceName, "binPath=", serviceBinaryPath(layout), "start=", startMode, "obj=", "LocalSystem", "DisplayName=", "CamStation Viewer Agent"); err != nil {
		return "", err
	}
	if err := configureServiceRecovery(); err != nil {
		return "", err
	}
	if _, err := runWindows(ctx, "sc.exe", "sidtype", ServiceName, "unrestricted"); err != nil {
		return "", err
	}
	serviceSID, err := accountSID(ctx, `NT SERVICE\`+ServiceName)
	if err != nil {
		return "", err
	}
	viewerXML, err := viewerTaskXML(stableBootstrapPath(layout), layout.InstallDir, options.MonitoringUserSID, !options.Staged)
	if err != nil {
		return "", err
	}
	recoveryXML, err := RecoveryTaskXML(stableUpdaterPath(layout))
	if err != nil {
		return "", err
	}
	viewerTaskFile := filepath.Join(layout.StateDir, "viewer-task.xml")
	recoveryTaskFile := filepath.Join(layout.StateDir, "recovery-task.xml")
	if err := atomicWrite(viewerTaskFile, []byte(viewerXML), 0o600); err != nil {
		return "", err
	}
	if err := atomicWrite(recoveryTaskFile, []byte(recoveryXML), 0o600); err != nil {
		return "", err
	}
	if _, err := runWindows(ctx, "schtasks.exe", "/Create", "/TN", ViewerTaskName, "/XML", viewerTaskFile, "/F"); err != nil {
		return "", err
	}
	if _, err := runWindows(ctx, "schtasks.exe", "/Create", "/TN", RecoveryTaskName, "/XML", recoveryTaskFile, "/F"); err != nil {
		return "", err
	}
	for _, dir := range []string{layout.InstallDir, layout.StateDir} {
		if _, err := runWindows(ctx, "icacls.exe", dir, "/inheritance:r", "/grant:r", `SYSTEM:(OI)(CI)F`, `BUILTIN\Administrators:(OI)(CI)F`, options.MonitoringUserSID+`:(OI)(CI)RX`); err != nil {
			return "", err
		}
	}
	return serviceSID, nil
}

func RegisterUninstall(ctx context.Context, layout Layout) error {
	uninstall := filepath.Join(layout.InstallDir, "CamStationViewerSetup.exe") + " --uninstall"
	registry := `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\CamStationViewer`
	for _, entry := range [][]string{
		{"add", registry, "/v", "DisplayName", "/t", "REG_SZ", "/d", "CamStation Viewer", "/f"},
		{"add", registry, "/v", "UninstallString", "/t", "REG_SZ", "/d", uninstall, "/f"},
		{"add", registry, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1", "/f"},
	} {
		if _, err := runWindows(ctx, "reg.exe", entry...); err != nil {
			return err
		}
	}
	return nil
}

func configureServiceRecovery() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(ServiceName)
	if err != nil {
		return err
	}
	defer service.Close()
	actions, err := windowsRecoveryActions()
	if err != nil {
		return err
	}
	if err := service.SetRecoveryActions(actions, 86400); err != nil {
		return err
	}
	return service.SetRecoveryActionsOnNonCrashFailures(true)
}

func windowsRecoveryActions() ([]mgr.RecoveryAction, error) {
	policy := SCMRecoveryActions()
	actions := make([]mgr.RecoveryAction, 0, len(policy))
	for _, action := range policy {
		typeID := mgr.NoAction
		switch action.Type {
		case "restart":
			typeID = mgr.ServiceRestart
		case "none":
		default:
			return nil, fmt.Errorf("unsupported SCM recovery action %q", action.Type)
		}
		actions = append(actions, mgr.RecoveryAction{Type: typeID, Delay: time.Duration(action.DelayMS) * time.Millisecond})
	}
	return actions, nil
}

func UnregisterAll(ctx context.Context, layout Layout) error {
	commands := [][]string{
		{"schtasks.exe", "/Delete", "/TN", ViewerTaskName, "/F"},
		{"schtasks.exe", "/Delete", "/TN", RecoveryTaskName, "/F"},
		{"sc.exe", "delete", ServiceName},
		{"reg.exe", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\CamStationViewer`, "/f"},
	}
	deletes := make([]func(context.Context) error, 0, len(commands))
	for _, command := range commands {
		command := command
		deletes = append(deletes, func(ctx context.Context) error {
			_, err := runWindows(ctx, command[0], command[1:]...)
			if err == nil {
				return nil
			}
			// Missing registrations are already in the desired uninstall state.
			lower := strings.ToLower(err.Error())
			if strings.Contains(lower, "1060") || strings.Contains(lower, "cannot find") || strings.Contains(lower, "not exist") {
				return nil
			}
			return err
		})
	}
	return unregisterSequence(ctx, func(ctx context.Context) error {
		return disableAndStopRegistered(ctx, layout)
	}, deletes...)
}

func ActiveConsoleUserSID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	shell := windows.GetShellWindow()
	if shell == 0 {
		return "", errors.New("interactive desktop session is required")
	}
	var processID uint32
	if _, err := windows.GetWindowThreadProcessId(shell, &processID); err != nil {
		return "", fmt.Errorf("interactive shell process lookup failed: %w", err)
	}
	return interactiveShellSID(processID, shellProcessUserSID)
}

func shellProcessUserSID(processID uint32) (string, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return "", err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

func accountSID(ctx context.Context, account string) (string, error) {
	script := `(New-Object Security.Principal.NTAccount($args[0])).Translate([Security.Principal.SecurityIdentifier]).Value`
	output, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script, account)
	if err != nil {
		return "", errors.New("service SID lookup failed")
	}
	sid := strings.TrimSpace(output)
	if !validTaskSID(sid) {
		return "", errors.New("service SID is invalid")
	}
	return sid, nil
}

func stopRegistered(ctx context.Context, _ Layout) error {
	// Ending the task terminates the bootstrap; closing its Job handle kills the full Electron tree.
	script := `$t=Get-ScheduledTask -TaskName '` + ViewerTaskName + `' -ErrorAction SilentlyContinue; if($t){Stop-ScheduledTask -InputObject $t -ErrorAction Stop}; ` +
		`$s=Get-Service -Name '` + ServiceName + `' -ErrorAction SilentlyContinue; if($s -and $s.Status -ne 'Stopped'){Stop-Service -InputObject $s -ErrorAction Stop; $s.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(25))}`
	_, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	return err
}

func disableAndStopRegistered(ctx context.Context, _ Layout) error {
	// Keep boot recovery enabled while disabling every application entry point.
	// The recovery task may also be running this installer now.
	_, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", disableAndStopScript())
	return err
}

func enableRegistered(ctx context.Context, _ Layout) error {
	if _, err := runWindows(ctx, "sc.exe", "config", ServiceName, "start=", "auto"); err != nil {
		return err
	}
	_, err := runWindows(ctx, "schtasks.exe", "/Change", "/TN", ViewerTaskName, "/ENABLE")
	return err
}

func startRegistered(ctx context.Context, _ Layout) error {
	if _, err := runWindows(ctx, "sc.exe", "start", ServiceName); err != nil {
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "1056") && !strings.Contains(lower, "already") {
			return err
		}
	}
	if _, err := runWindows(ctx, "schtasks.exe", "/Run", "/TN", ViewerTaskName); err != nil {
		return err
	}
	return nil
}

func validateRegistered(ctx context.Context, _ Layout) error {
	deadline := time.NewTicker(time.Second)
	defer deadline.Stop()
	for {
		if _, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", validateRegisteredScript()); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("new Agent and Viewer registration validation timed out")
		case <-deadline.C:
		}
	}
}

func runWindows(ctx context.Context, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
