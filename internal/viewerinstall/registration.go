package viewerinstall

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	ServiceName      = "CamStationViewerAgent"
	ViewerTaskName   = "CamStationViewer"
	RecoveryTaskName = "CamStationViewerRecovery"
)

type RegistrationOptions struct {
	MonitoringUserSID string
	ServerURL         string
	DisplayName       string
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
	return validateRegistered(ctx, registration.Layout)
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

func SCMRecoveryArgs() []string {
	parts := make([]string, 0, len(SCMRecoveryActions()))
	for _, action := range SCMRecoveryActions() {
		parts = append(parts, fmt.Sprintf("%s/%d", action.Type, action.DelayMS))
	}
	return []string{"failure", ServiceName, "reset=", "86400", "actions=", strings.Join(parts, "/")}
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
	if !absolutePlatformPath(bootstrapPath) || !absolutePlatformPath(installDir) || !validTaskSID(userSID) {
		return "", errors.New("invalid Viewer task configuration")
	}
	return taskXML(
		`<LogonTrigger><Enabled>true</Enabled><UserId>`+xmlEscape(userSID)+`</UserId></LogonTrigger>`, userSID, "InteractiveToken", "LeastPrivilege",
		bootstrapPath, `--install-dir &quot;`+xmlEscape(installDir)+`&quot;`,
	), nil
}

func RecoveryTaskXML(updaterPath string) (string, error) {
	if !absolutePlatformPath(updaterPath) {
		return "", errors.New("invalid recovery updater path")
	}
	return taskXML(
		`<BootTrigger><Enabled>true</Enabled></BootTrigger>`, "S-1-5-18", "ServiceAccount", "HighestAvailable",
		updaterPath, "--recover",
	), nil
}

func absolutePlatformPath(value string) bool {
	return filepath.IsAbs(value) || (len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/'))
}

func taskXML(trigger, sid, logonType, runLevel, command, arguments string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` +
		`<RegistrationInfo><Description>CamStation Viewer supervised startup</Description></RegistrationInfo>` +
		`<Triggers>` + trigger + `</Triggers>` +
		`<Principals><Principal id="Author"><UserId>` + xmlEscape(sid) + `</UserId><LogonType>` + logonType + `</LogonType><RunLevel>` + runLevel + `</RunLevel></Principal></Principals>` +
		`<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><ExecutionTimeLimit>PT0S</ExecutionTimeLimit></Settings>` +
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

func stableHostPath(layout Layout) string {
	return filepath.Join(layout.InstallDir, "CamStationViewerHost.exe")
}
func stableBootstrapPath(layout Layout) string {
	return filepath.Join(layout.InstallDir, "CamStationViewerBootstrap.exe")
}
func stableUpdaterPath(layout Layout) string {
	return filepath.Join(layout.StateDir, "updater", "CamStationViewerUpdater.exe")
}

func serviceBinaryPath(layout Layout) string {
	return fmt.Sprintf(`"%s" --install-dir "%s" --config "%s"`, stableHostPath(layout), layout.InstallDir, filepath.Join(layout.StateDir, "config.json"))
}
