package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/vieweragent"
	"camstation/internal/viewerinstall"
)

func TestInstallerModesAreExplicitAndBounded(t *testing.T) {
	digest := strings.Repeat("a", 64)
	tests := []struct {
		args []string
		mode installerMode
	}{
		{nil, modeInstall},
		{[]string{"/S"}, modeInstall},
		{[]string{"--update", "--transaction-id", "update-1", "--generation", "1", "--expected-sha", digest, "--parent-pid", "42"}, modeUpdate},
		{[]string{"--rollback", "update-1"}, modeRollback},
		{[]string{"--uninstall"}, modeUninstall},
		{[]string{"--recover"}, modeRecover},
	}
	for _, test := range tests {
		options, err := parseInstallerArgs(test.args)
		if err != nil || options.mode != test.mode {
			t.Fatalf("args=%v options=%+v err=%v", test.args, options, err)
		}
	}
}

func TestUpdaterReverifiesItsOwnExactArtifactHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "CamStationViewerSetup.exe")
	if err := os.WriteFile(path, []byte("MZ setup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileSHA(path, "574ce2739035aaff515080c231b4fb9ed9103174d63e201caec23d3d9a657dfc"); err != nil {
		t.Fatal(err)
	}
	if err := verifyFileSHA(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("altered updater executable was accepted")
	}
}

func TestUpdaterRequiresExactDurableAgentHandoff(t *testing.T) {
	digest := strings.Repeat("a", 64)
	options := installerOptions{mode: modeUpdate, transactionID: "update-7", generation: 7, expectedSHA: digest}
	journal := vieweragent.UpdateJournal{State: "installer_launched", TransactionID: "update-7", Generation: 7, ArtifactSHA256: digest, TargetVersion: "2.0.7"}
	if err := validateUpdateHandoff(journal, options, "2.0.7"); err != nil {
		t.Fatal(err)
	}
	journal.Generation++
	if err := validateUpdateHandoff(journal, options, "2.0.7"); err == nil {
		t.Fatal("mismatched Agent handoff was accepted")
	}
}

func TestEmbeddedBuildPayloadIsReadableByProductionExtractor(t *testing.T) {
	payload, err := payloadFS.ReadFile("payload/release.zip")
	if err != nil {
		t.Skip("transient build payload is intentionally absent")
	}
	manifest, err := viewerinstall.ExtractPayload(bytes.NewReader(payload), int64(len(payload)), t.TempDir())
	if err != nil || manifest.Version == "" || len(manifest.Files) < 4 {
		t.Fatalf("embedded payload manifest=%+v err=%v", manifest, err)
	}
}

func TestUpdateModeRejectsIncompleteOrArbitraryInputs(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, args := range [][]string{
		{"--update"},
		{"--update", "--transaction-id", `..\escape`, "--generation", "1", "--expected-sha", digest},
		{"--update", "--transaction-id", "tx", "--generation", "0", "--expected-sha", digest},
		{"--update", "--transaction-id", "tx", "--generation", "1", "--expected-sha", "bad"},
		{"--uninstall", "--update"},
		{"--server-url", "http://evil.example"},
	} {
		if _, err := parseInstallerArgs(args); err == nil {
			t.Fatalf("unsafe args accepted: %v", args)
		}
	}
}
