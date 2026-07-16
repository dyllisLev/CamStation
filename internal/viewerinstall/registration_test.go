package viewerinstall

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsRegistrationPolicyIsBounded(t *testing.T) {
	want := []string{"failure", ServiceName, "reset=", "86400", "actions=", "restart/5000/restart/30000/restart/120000"}
	if got := SCMRecoveryArgs(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("recovery args=%v", got)
	}
	if strings.Contains(strings.Join(SCMRecoveryArgs(), "/"), "restart/0") {
		t.Fatal("unbounded fourth restart was configured")
	}
}

func TestViewerLogonTaskUsesConfiguredSIDAndIgnoreNew(t *testing.T) {
	xml, err := ViewerTaskXML(`C:\Program Files\CamStation Viewer\CamStationViewerBootstrap.exe`, `C:\Program Files\CamStation Viewer`, "S-1-5-21-123")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"<UserId>S-1-5-21-123</UserId>", "<LogonType>InteractiveToken</LogonType>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>", "--install-dir", "CamStationViewerBootstrap.exe"} {
		if !strings.Contains(xml, required) {
			t.Fatalf("task XML missing %q: %s", required, xml)
		}
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
