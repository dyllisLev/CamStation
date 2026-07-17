package viewerinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSystemRegistrationUpdateRequiresExactCommitMarkerButInitialInstallDoesNot(t *testing.T) {
	root := t.TempDir()
	layout := Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	release := Release{Version: "2.4.0", Digest: digest, ReleaseID: ReleaseID("2.4.0", digest)}
	if err := os.MkdirAll(filepath.Join(layout.ReleaseDir(release.ReleaseID), "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"camstation-viewer-agent.exe", filepath.Join("viewer", "CamStationViewer.exe")} {
		if err := os.WriteFile(filepath.Join(layout.ReleaseDir(release.ReleaseID), name), []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveCurrent(layout, currentFor(release)); err != nil {
		t.Fatal(err)
	}
	registration := SystemRegistration{Layout: layout}
	initial := Journal{TransactionID: "install-1", Generation: 1, Release: release}
	if err := registration.Validate(t.Context(), initial); err != nil {
		t.Fatalf("initial validation unexpectedly required commit marker: %v", err)
	}

	previous := currentFor(Release{Version: "2.3.0", Digest: strings.Repeat("b", 64), ReleaseID: ReleaseID("2.3.0", strings.Repeat("b", 64))})
	update := Journal{TransactionID: "update-2.4.0-a-7", CommandID: 41, PayloadHash: "payload-41", Generation: 7, Release: release, Previous: &previous}
	marker := CommitMarker{
		TransactionID: update.TransactionID, CommandID: 41, PayloadHash: "payload-41",
		Version: release.Version, Digest: release.Digest, Generation: update.Generation,
		Token: strings.Repeat("c", 64),
	}
	if err := SaveCommitMarker(layout, marker); err != nil {
		t.Fatal(err)
	}
	if err := registration.Validate(t.Context(), update); err != nil {
		t.Fatalf("exact update marker rejected: %v", err)
	}

	marker.TransactionID = "stale-transaction"
	if err := SaveCommitMarker(layout, marker); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := registration.Validate(ctx, update); err == nil {
		t.Fatal("mismatched update marker accepted")
	}
}

func TestUnregisterAbortsBeforeDeletingAnythingWhenDisableOrStopFails(t *testing.T) {
	stopErr := errors.New("service did not stop")
	deleted := 0
	err := unregisterSequence(t.Context(), func(context.Context) error {
		return stopErr
	}, func(context.Context) error {
		deleted++
		return nil
	})
	if !errors.Is(err, stopErr) || deleted != 0 {
		t.Fatalf("err=%v deleted=%d", err, deleted)
	}
}

func TestDisableAndStopKeepsRunningBootRecoveryProcessAlive(t *testing.T) {
	script := disableAndStopScript()
	if strings.Contains(script, RecoveryTaskName) || strings.Contains(script, "$recovery") {
		t.Fatalf("boot recovery task was disabled during recoverable transaction: %s", script)
	}
}

func TestInteractiveShellSIDUsesShellProcessOwner(t *testing.T) {
	var selectedPID uint32
	sid, err := interactiveShellSID(77, func(pid uint32) (string, error) {
		selectedPID = pid
		return "S-1-5-21-1000", nil
	})
	if err != nil || sid != "S-1-5-21-1000" || selectedPID != 77 {
		t.Fatalf("sid=%q selectedPID=%d err=%v", sid, selectedPID, err)
	}
}

func TestInteractiveShellSIDRejectsMissingShellAndInvalidOwner(t *testing.T) {
	if _, err := interactiveShellSID(0, func(uint32) (string, error) {
		return "S-1-5-21-1000", nil
	}); err == nil || !strings.Contains(err.Error(), "interactive desktop session") {
		t.Fatalf("missing shell err=%v", err)
	}
	if _, err := interactiveShellSID(77, func(uint32) (string, error) {
		return "not-a-sid", nil
	}); err == nil || !strings.Contains(err.Error(), "SID is invalid") {
		t.Fatalf("invalid SID err=%v", err)
	}
}

func TestWindowsRegistrationPolicyIsBounded(t *testing.T) {
	wantActions := []RecoveryAction{{Type: "restart", DelayMS: 5000}, {Type: "restart", DelayMS: 30000}, {Type: "restart", DelayMS: 120000}, {Type: "none", DelayMS: 0}}
	if got := SCMRecoveryActions(); !reflect.DeepEqual(got, wantActions) {
		t.Fatalf("Windows recovery action mapping=%+v want=%+v", got, wantActions)
	}
}

func TestRegistrationValidationRequiresRunningServiceAndViewerTask(t *testing.T) {
	script := validateRegisteredScript()
	for _, required := range []string{ServiceName, ViewerTaskName, "Running"} {
		if !strings.Contains(script, required) {
			t.Fatalf("registration validation omitted %q: %s", required, script)
		}
	}
}

func TestViewerLogonTaskUsesConfiguredSIDAndIgnoreNew(t *testing.T) {
	taskXML, err := ViewerTaskXML(`C:\Program Files\CamStation Viewer\CamStationViewerBootstrap.exe`, `C:\Program Files\CamStation Viewer`, "S-1-5-21-123")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"<LogonType>InteractiveToken</LogonType>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "--install-dir", "CamStationViewerBootstrap.exe"} {
		if !strings.Contains(taskXML, required) {
			t.Fatalf("task XML missing %q: %s", required, taskXML)
		}
	}
	var task struct {
		Triggers struct {
			LogonTrigger struct {
				UserID string `xml:"UserId"`
			} `xml:"LogonTrigger"`
		} `xml:"Triggers"`
		Principals struct {
			Principal struct {
				UserID string `xml:"UserId"`
			} `xml:"Principal"`
		} `xml:"Principals"`
	}
	if err := xml.Unmarshal([]byte(taskXML), &task); err != nil {
		t.Fatal(err)
	}
	if task.Triggers.LogonTrigger.UserID != "S-1-5-21-123" || task.Principals.Principal.UserID != "S-1-5-21-123" {
		t.Fatalf("SID trigger=%q principal=%q", task.Triggers.LogonTrigger.UserID, task.Principals.Principal.UserID)
	}
}

func TestPublicDesktopShortcutTargetsStableViewerTask(t *testing.T) {
	script, err := publicDesktopShortcutScript(
		`C:\Program Files\CamStation Viewer\CamStationViewerBootstrap.exe`,
		`C:\Program Files\CamStation Viewer`,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"CommonDesktopDirectory",
		"CamStation Viewer.lnk",
		"schtasks.exe",
		`/Run /TN "CamStationViewer"`,
		"CreateShortcut",
		"IconLocation",
		"WorkingDirectory",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("shortcut script missing %q: %s", required, script)
		}
	}
}

func TestPublicDesktopShortcutRemovalOwnsOnlyExactLink(t *testing.T) {
	script := removePublicDesktopShortcutScript()
	for _, required := range []string{"CommonDesktopDirectory", "CamStation Viewer.lnk", "-LiteralPath", "SilentlyContinue"} {
		if !strings.Contains(script, required) {
			t.Fatalf("removal script missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "*.lnk") {
		t.Fatalf("removal script uses a broad shortcut pattern: %s", script)
	}
}

func TestStagedViewerLogonTaskRemainsDisabledUntilReleaseActivation(t *testing.T) {
	taskXML, err := viewerTaskXML(`C:\Program Files\CamStation Viewer\CamStationViewerBootstrap.exe`, `C:\Program Files\CamStation Viewer`, "S-1-5-21-123", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(taskXML, "<Enabled>false</Enabled>") {
		t.Fatalf("staged task was runnable: %s", taskXML)
	}
}

func TestBootRecoveryTaskRunsStableUpdaterAsSystem(t *testing.T) {
	xml, err := RecoveryTaskXML(`C:\ProgramData\CamStation\Viewer\updater\CamStationViewerUpdater.exe`)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"<BootTrigger>", "<UserId>S-1-5-18</UserId>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "--recover"} {
		if !strings.Contains(xml, required) {
			t.Fatalf("recovery task XML missing %q: %s", required, xml)
		}
	}
}

func TestExtractPayloadRejectsTraversalAndHashMismatch(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest PayloadManifest
		files    map[string][]byte
	}{
		{
			name: "traversal",
			manifest: PayloadManifest{SchemaVersion: SchemaVersion, Version: "2.0.0", Digest: strings.Repeat("a", 64),
				Files: []PayloadFile{{Path: "../escape.exe", Size: 2, SHA256: sha256HexBytes([]byte("MZ"))}}},
			files: map[string][]byte{"../escape.exe": []byte("MZ")},
		},
		{
			name: "hash mismatch",
			manifest: PayloadManifest{SchemaVersion: SchemaVersion, Version: "2.0.0", Digest: strings.Repeat("b", 64),
				Files: []PayloadFile{{Path: "release/camstation-viewer-agent.exe", Size: 2, SHA256: strings.Repeat("0", 64)}}},
			files: map[string][]byte{"release/camstation-viewer-agent.exe": []byte("MZ")},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			archive := payloadArchive(t, test.manifest, test.files)
			if _, err := ExtractPayload(bytes.NewReader(archive), int64(len(archive)), t.TempDir()); err == nil {
				t.Fatal("unsafe payload accepted")
			}
		})
	}
}

func TestExtractPayloadVerifiesEveryManifestFile(t *testing.T) {
	files := map[string][]byte{
		"stable/CamStationViewerHost.exe":      []byte("host"),
		"stable/CamStationViewerBootstrap.exe": []byte("bootstrap"),
		"release/camstation-viewer-agent.exe":  []byte("agent"),
		"release/viewer/CamStationViewer.exe":  []byte("viewer"),
		"defaults.json":                        []byte(`{"serverUrl":"http://camstation:18080","displayName":"Wall","allowDevelopmentUnsigned":true}`),
	}
	manifest := PayloadManifest{SchemaVersion: SchemaVersion, Version: "2.0.0", Digest: strings.Repeat("c", 64)}
	for name, data := range files {
		manifest.Files = append(manifest.Files, PayloadFile{Path: name, Size: int64(len(data)), SHA256: sha256HexBytes(data)})
	}
	archive := payloadArchive(t, manifest, files)
	destination := t.TempDir()
	got, err := ExtractPayload(bytes.NewReader(archive), int64(len(archive)), destination)
	if err != nil || got.Version != manifest.Version || got.Digest != manifest.Digest {
		t.Fatalf("manifest=%+v err=%v", got, err)
	}
	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("file %s=%q err=%v", name, data, err)
		}
	}
}

func payloadArchive(t *testing.T, manifest PayloadManifest, files map[string][]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	manifestData, _ := json.Marshal(manifest)
	entry, _ := writer.Create("manifest.json")
	_, _ = entry.Write(manifestData)
	for name, data := range files {
		entry, _ := writer.Create(name)
		_, _ = entry.Write(data)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func sha256HexBytes(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
