//go:build windows

package viewerinstall

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

type Ownership struct {
	handle   windows.Handle
	stateDir string
}

func Acquire(layout Layout) (*Ownership, error) {
	name, err := windows.UTF16PtrFromString(`Global\CamStationViewerUpdate`)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return nil, err
	}
	result, err := windows.WaitForSingleObject(handle, 0)
	if err != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		windows.CloseHandle(handle)
		if err == nil {
			err = ErrUpdateOwned
		}
		return nil, err
	}
	owner := &Ownership{handle: handle, stateDir: filepath.Clean(layout.StateDir)}
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
	return errors.Join(windows.ReleaseMutex(handle), windows.CloseHandle(handle))
}

func (owner *Ownership) owns(layout Layout) bool {
	return owner != nil && owner.handle != 0 && owner.stateDir == filepath.Clean(layout.StateDir)
}

func RegisterAll(ctx context.Context, layout Layout, options RegistrationOptions) (string, error) {
	if err := layout.Ensure(); err != nil {
		return "", err
	}
	if !validTaskSID(options.MonitoringUserSID) {
		return "", errors.New("valid monitoring user SID is required")
	}
	if _, err := runWindows(ctx, "sc.exe", "create", ServiceName, "binPath=", serviceBinaryPath(layout), "start=", "auto", "obj=", "LocalSystem", "DisplayName=", "CamStation Viewer Agent"); err != nil {
		// A repair may encounter an existing service; sc config below is authoritative.
		if !strings.Contains(strings.ToLower(err.Error()), "1073") && !strings.Contains(strings.ToLower(err.Error()), "exists") {
			return "", err
		}
	}
	if _, err := runWindows(ctx, "sc.exe", "config", ServiceName, "binPath=", serviceBinaryPath(layout), "start=", "auto", "obj=", "LocalSystem", "DisplayName=", "CamStation Viewer Agent"); err != nil {
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
	viewerXML, err := ViewerTaskXML(stableBootstrapPath(layout), layout.InstallDir, options.MonitoringUserSID)
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
	uninstall := filepath.Join(layout.InstallDir, "CamStationViewerSetup.exe") + " --uninstall"
	registry := `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\CamStationViewer`
	for _, entry := range [][]string{
		{"add", registry, "/v", "DisplayName", "/t", "REG_SZ", "/d", "CamStation Viewer", "/f"},
		{"add", registry, "/v", "UninstallString", "/t", "REG_SZ", "/d", uninstall, "/f"},
		{"add", registry, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1", "/f"},
	} {
		if _, err := runWindows(ctx, "reg.exe", entry...); err != nil {
			return "", err
		}
	}
	return serviceSID, nil
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
	script := `$u=(Get-CimInstance Win32_ComputerSystem).UserName; if(!$u){exit 2}; (New-Object Security.Principal.NTAccount($u)).Translate([Security.Principal.SecurityIdentifier]).Value`
	output, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return "", errors.New("active console user is required")
	}
	sid := strings.TrimSpace(output)
	if !validTaskSID(sid) {
		return "", errors.New("active console user SID is invalid")
	}
	return sid, nil
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
	// Disable every automatic entry point before waiting for the live processes to stop.
	script := `$names=@('` + ViewerTaskName + `','` + RecoveryTaskName + `'); foreach($name in $names){$t=Get-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue; if($t){Disable-ScheduledTask -InputObject $t -ErrorAction Stop | Out-Null; Stop-ScheduledTask -InputObject $t -ErrorAction Stop}}; ` +
		`$s=Get-Service -Name '` + ServiceName + `' -ErrorAction SilentlyContinue; if($s){Set-Service -Name '` + ServiceName + `' -StartupType Disabled -ErrorAction Stop; if($s.Status -ne 'Stopped'){Stop-Service -InputObject $s -ErrorAction Stop; $s.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(25))}}`
	_, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
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
		script := `if((Get-Service -Name '` + ServiceName + `' -ErrorAction Stop).Status -ne 'Running'){exit 2}`
		if _, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script); err == nil {
			if _, taskErr := runWindows(ctx, "schtasks.exe", "/Query", "/TN", ViewerTaskName); taskErr == nil {
				return nil
			}
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
