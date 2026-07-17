package viewerinstall

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	ServiceName                       = "CamStationViewerAgent"
	ViewerTaskName                    = "CamStationViewer"
	RecoveryTaskName                  = "CamStationViewerRecovery"
	viewerShortcutName                = "CamStation Viewer.lnk"
	viewerShortcutIconEnv             = "CAMSTATION_VIEWER_SHORTCUT_ICON"
	viewerShortcutWorkingDirectoryEnv = "CAMSTATION_VIEWER_SHORTCUT_WORKING_DIRECTORY"
)

type RegistrationOptions struct {
	MonitoringUserSID string
	ServerURL         string
	DisplayName       string
	Staged            bool
}

type RecoveryAction struct {
	Type    string
	DelayMS int
}

type SystemRegistration struct{ Layout Layout }

func (registration SystemRegistration) Stop(ctx context.Context) error {
	return stopRegistered(ctx, registration.Layout)
}

func (registration SystemRegistration) Start(ctx context.Context) error {
	return startRegistered(ctx, registration.Layout)
}

func (registration SystemRegistration) Validate(ctx context.Context, journal Journal) error {
	if err := requireReleaseFiles(registration.Layout.ReleaseDir(journal.Release.ReleaseID)); err != nil {
		return err
	}
	current, err := LoadCurrent(registration.Layout)
	if err != nil || current.ReleaseID != journal.Release.ReleaseID {
		return errors.New("activated release pointer does not match transaction")
	}
	if journal.Previous == nil || journal.CommandID == 0 || journal.PayloadHash == "" {
		return validateRegistered(ctx, registration.Layout)
	}
	validationCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := validateRegistered(validationCtx, registration.Layout); err != nil {
			return err
		}
		marker, err := LoadCommitMarker(registration.Layout)
		if err == nil && commitMarkerMatches(marker, journal) {
			return nil
		}
		select {
		case <-validationCtx.Done():
			return errors.New("server-authorized update validation timed out")
		case <-ticker.C:
		}
	}
}

func (registration SystemRegistration) Disable(ctx context.Context) error {
	return disableAndStopRegistered(ctx, registration.Layout)
}

func (registration SystemRegistration) EnableRuntime(ctx context.Context) error {
	return enableRegistered(ctx, registration.Layout)
}

func (registration SystemRegistration) RegisterRuntime(ctx context.Context, options RegistrationOptions) (string, error) {
	return RegisterRuntime(ctx, registration.Layout, options)
}

func (registration SystemRegistration) RegisterUninstall(ctx context.Context) error {
	return RegisterUninstall(ctx, registration.Layout)
}

func (registration SystemRegistration) Unregister(ctx context.Context) error {
	return UnregisterAll(ctx, registration.Layout)
}

func unregisterSequence(ctx context.Context, disableAndStop func(context.Context) error, deletes ...func(context.Context) error) error {
	if err := disableAndStop(ctx); err != nil {
		return err
	}
	var result error
	for _, remove := range deletes {
		result = errors.Join(result, remove(ctx))
	}
	return result
}

func disableAndStopScript() string {
	return `$ErrorActionPreference='Stop'; $viewer=Get-ScheduledTask -TaskName '` + ViewerTaskName + `' -ErrorAction SilentlyContinue; if($viewer){Disable-ScheduledTask -InputObject $viewer -ErrorAction Stop | Out-Null; Stop-ScheduledTask -InputObject $viewer -ErrorAction Stop}; ` +
		`$s=Get-Service -Name '` + ServiceName + `' -ErrorAction SilentlyContinue; if($s){Set-Service -Name '` + ServiceName + `' -StartupType Disabled -ErrorAction Stop; if($s.Status -ne 'Stopped'){Stop-Service -InputObject $s -ErrorAction Stop; $s.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(25))}}; exit 0`
}

func stopRegisteredScript() string {
	return `$ErrorActionPreference='Stop'; $t=Get-ScheduledTask -TaskName '` + ViewerTaskName + `' -ErrorAction SilentlyContinue; if($t){Stop-ScheduledTask -InputObject $t -ErrorAction Stop}; ` +
		`$s=Get-Service -Name '` + ServiceName + `' -ErrorAction SilentlyContinue; if($s -and $s.Status -ne 'Stopped'){Stop-Service -InputObject $s -ErrorAction Stop; $s.WaitForStatus('Stopped',[TimeSpan]::FromSeconds(25))}; exit 0`
}

func unregisterScheduledTasksScript() string {
	return `$tasks=Get-ScheduledTask -TaskPath '\' -ErrorAction Stop; ` +
		`$tasks | Where-Object { $_.TaskName -eq '` + ViewerTaskName + `' -or $_.TaskName -eq '` + RecoveryTaskName + `' } | Unregister-ScheduledTask -Confirm:$false -ErrorAction Stop; exit 0`
}

func unregisterUninstallRegistryScript() string {
	return `$path='Registry::HKEY_LOCAL_MACHINE\Software\Microsoft\Windows\CurrentVersion\Uninstall\CamStationViewer'; ` +
		`if(Test-Path -LiteralPath $path -ErrorAction Stop){Remove-Item -LiteralPath $path -Force -ErrorAction Stop}; exit 0`
}

func validateRegisteredScript() string {
	return `$s=Get-Service -Name '` + ServiceName + `' -ErrorAction Stop; if($s.Status -ne 'Running'){exit 2}; ` +
		`$t=Get-ScheduledTask -TaskName '` + ViewerTaskName + `' -ErrorAction Stop; if($t.State -ne 'Running'){exit 3}`
}

func publicDesktopShortcutScript(bootstrapPath, installDir string) (string, []string, error) {
	if !absolutePlatformPath(bootstrapPath) || !absolutePlatformPath(installDir) {
		return "", nil, errors.New("invalid Viewer shortcut paths")
	}
	script := `$ErrorActionPreference='Stop'; $desktop=[Environment]::GetFolderPath('CommonDesktopDirectory'); if(!$desktop){throw 'public desktop is unavailable'}; ` +
		`$path=[IO.Path]::Combine($desktop,'` + viewerShortcutName + `'); ` +
		`$link=(New-Object -ComObject WScript.Shell).CreateShortcut($path); ` +
		`$link.TargetPath=[IO.Path]::Combine($env:SystemRoot,'System32','schtasks.exe'); ` +
		`$link.Arguments='/Run /TN "` + ViewerTaskName + `"'; ` +
		`$link.IconLocation=$env:` + viewerShortcutIconEnv + `+',0'; ` +
		`$link.WorkingDirectory=$env:` + viewerShortcutWorkingDirectoryEnv + `; ` +
		`$link.Description='CamStation Viewer'; $link.Save(); ` +
		`if(!(Test-Path -LiteralPath $path -PathType Leaf)){throw 'shortcut was not created'}; ` +
		`$saved=(New-Object -ComObject WScript.Shell).CreateShortcut($path); ` +
		`if($saved.TargetPath -ne [IO.Path]::Combine($env:SystemRoot,'System32','schtasks.exe')){throw 'shortcut target verification failed'}; ` +
		`if($saved.Arguments -ne '/Run /TN "` + ViewerTaskName + `"'){throw 'shortcut arguments verification failed'}; ` +
		`if($saved.IconLocation -ne $env:` + viewerShortcutIconEnv + `+',0'){throw 'shortcut icon verification failed'}; ` +
		`if($saved.WorkingDirectory -ne $env:` + viewerShortcutWorkingDirectoryEnv + `){throw 'shortcut working directory verification failed'}`
	environment := []string{
		viewerShortcutIconEnv + "=" + bootstrapPath,
		viewerShortcutWorkingDirectoryEnv + "=" + installDir,
	}
	return script, environment, nil
}

func removePublicDesktopShortcutScript() string {
	return `$desktop=[Environment]::GetFolderPath('CommonDesktopDirectory'); if(!$desktop){throw 'public desktop is unavailable'}; ` +
		`$path=[IO.Path]::Combine($desktop,'` + viewerShortcutName + `'); ` +
		`if(Test-Path -LiteralPath $path -ErrorAction Stop){` +
		`if(!(Test-Path -LiteralPath $path -PathType Leaf -ErrorAction Stop)){throw 'public desktop shortcut path is not a leaf'}; ` +
		`Remove-Item -LiteralPath $path -Force -ErrorAction Stop}`
}

func SCMRecoveryActions() []RecoveryAction {
	return []RecoveryAction{
		{Type: "restart", DelayMS: 5000},
		{Type: "restart", DelayMS: 30000},
		{Type: "restart", DelayMS: 120000},
		{Type: "none", DelayMS: 0},
	}
}

func ViewerTaskXML(bootstrapPath, installDir, userSID string) (string, error) {
	return viewerTaskXML(bootstrapPath, installDir, userSID, true)
}

func viewerTaskXML(bootstrapPath, installDir, userSID string, enabled bool) (string, error) {
	if !absolutePlatformPath(bootstrapPath) || !absolutePlatformPath(installDir) || !validTaskSID(userSID) {
		return "", errors.New("invalid Viewer task configuration")
	}
	return taskXML(
		`<LogonTrigger><Enabled>true</Enabled><UserId>`+xmlEscape(userSID)+`</UserId></LogonTrigger>`, userSID, "InteractiveToken", "LeastPrivilege",
		bootstrapPath, `--install-dir &quot;`+xmlEscape(installDir)+`&quot;`, enabled,
	), nil
}

func RecoveryTaskXML(updaterPath string) (string, error) {
	if !absolutePlatformPath(updaterPath) {
		return "", errors.New("invalid recovery updater path")
	}
	return taskXML(
		`<BootTrigger><Enabled>true</Enabled></BootTrigger>`, "S-1-5-18", "ServiceAccount", "HighestAvailable",
		updaterPath, "--recover", true,
	), nil
}

func absolutePlatformPath(value string) bool {
	return filepath.IsAbs(value) || (len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/'))
}

func taskXML(trigger, sid, logonType, runLevel, command, arguments string, enabled bool) string {
	enabledText := "false"
	if enabled {
		enabledText = "true"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` +
		`<RegistrationInfo><Description>CamStation Viewer supervised startup</Description></RegistrationInfo>` +
		`<Triggers>` + trigger + `</Triggers>` +
		`<Principals><Principal id="Author"><UserId>` + xmlEscape(sid) + `</UserId><LogonType>` + logonType + `</LogonType><RunLevel>` + runLevel + `</RunLevel></Principal></Principals>` +
		`<Settings><Enabled>` + enabledText + `</Enabled><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>` +
		`<Actions Context="Author"><Exec><Command>` + xmlEscape(command) + `</Command><Arguments>` + arguments + `</Arguments></Exec></Actions></Task>`
}

func xmlEscape(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

func validTaskSID(value string) bool {
	if !strings.HasPrefix(value, "S-") || len(value) > 184 || strings.ContainsAny(value, " \r\n\t") {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "S-") {
		if (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}

func interactiveShellSID(shellPID uint32, lookup func(uint32) (string, error)) (string, error) {
	if shellPID == 0 {
		return "", errors.New("interactive desktop session is required")
	}
	sid, err := lookup(shellPID)
	if err != nil {
		return "", fmt.Errorf("interactive shell user lookup failed: %w", err)
	}
	sid = strings.TrimSpace(sid)
	if !validTaskSID(sid) {
		return "", errors.New("interactive shell user SID is invalid")
	}
	return sid, nil
}

func stableHostPath(layout Layout) string {
	return filepath.Join(layout.InstallDir, "CamStationViewerHost.exe")
}
func stableBootstrapPath(layout Layout) string {
	return filepath.Join(layout.InstallDir, "CamStationViewerBootstrap.exe")
}
func stableUpdaterPath(layout Layout) string {
	return filepath.Join(layout.StateDir, "updater", "CamStationViewerUpdater.exe")
}

func stableInstallPaths(layout Layout) []string {
	return []string{
		stableHostPath(layout),
		stableBootstrapPath(layout),
		filepath.Join(layout.InstallDir, "CamStationViewerSetup.exe"),
		stableUpdaterPath(layout),
	}
}

func initialOwnedPaths(layout Layout) []string {
	return append(stableInstallPaths(layout),
		filepath.Join(layout.StateDir, "config.json"),
		layout.CurrentPath(),
		filepath.Join(layout.StateDir, "viewer-task.xml"),
		filepath.Join(layout.StateDir, "recovery-task.xml"),
		filepath.Join(layout.StateDir, "state.json"),
		filepath.Join(layout.StateDir, "commands.json"),
		filepath.Join(layout.StateDir, "update.json"),
	)
}

func serviceBinaryPath(layout Layout) string {
	return fmt.Sprintf(`"%s" --install-dir "%s" --config "%s"`, stableHostPath(layout), layout.InstallDir, filepath.Join(layout.StateDir, "config.json"))
}
