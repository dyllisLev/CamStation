package main

import (
	"bytes"
	"context"
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
		args   []string
		mode   installerMode
		silent bool
	}{
		{args: nil, mode: modeInstall},
		{args: []string{"/S"}, mode: modeInstall, silent: true},
		{args: []string{"/s", "--update", "--transaction-id", "update-1", "--generation", "1", "--expected-sha", digest, "--parent-pid", "42"}, mode: modeUpdate, silent: true},
		{args: []string{"--rollback", "update-1"}, mode: modeRollback},
		{args: []string{"--uninstall"}, mode: modeUninstall},
		{args: []string{"--recover"}, mode: modeRecover},
	}
	for _, test := range tests {
		options, err := parseInstallerArgs(test.args)
		if err != nil || options.mode != test.mode || options.silent != test.silent {
			t.Fatalf("args=%v options=%+v err=%v", test.args, options, err)
		}
	}
}

func TestSilentProgressReporterSuppressesAllProgress(t *testing.T) {
	var output bytes.Buffer
	report := installerProgress(true, &output)
	report("Preparing installation")
	report("Installation complete")
	if output.Len() != 0 {
		t.Fatalf("silent output=%q", output.String())
	}
}

func TestDefaultProgressReporterDoesNotPromptForInput(t *testing.T) {
	var output bytes.Buffer
	report := installerProgress(false, &output)
	report("Preparing installation")
	report("Installation complete")
	if got := output.String(); !strings.Contains(got, "Preparing installation") || !strings.Contains(got, "Installation complete") || strings.Contains(got, "press") {
		t.Fatalf("default progress=%q", got)
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

func TestUpdaterPromotesLaunchingHandoffIdempotently(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	options := installerOptions{mode: modeUpdate, transactionID: "update-8", generation: 8, expectedSHA: digest}
	journal := vieweragent.UpdateJournal{State: "launching_installer", TransactionID: "update-8", Generation: 8, ArtifactSHA256: digest, TargetVersion: "2.0.8"}
	if err := vieweragent.SaveUpdateJournal(filepath.Join(layout.StateDir, "update.json"), journal); err != nil {
		t.Fatal(err)
	}
	if err := promoteUpdateHandoff(layout, options, "2.0.8"); err != nil {
		t.Fatal(err)
	}
	if err := promoteUpdateHandoff(layout, options, "2.0.8"); err != nil {
		t.Fatalf("idempotent promotion failed: %v", err)
	}
	promoted, err := vieweragent.LoadUpdateJournal(filepath.Join(layout.StateDir, "update.json"))
	if err != nil || promoted.State != "installer_launched" {
		t.Fatalf("promoted=%+v err=%v", promoted, err)
	}
}

func TestBootRecoveryReconcilesCommittedTransactionBoundary(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "release")
	if err := os.MkdirAll(filepath.Join(source, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"camstation-viewer-agent.exe": []byte("agent"), "viewer/CamStationViewer.exe": []byte("viewer")} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	digest := strings.Repeat("a", 64)
	request := viewerinstall.Request{TransactionID: "update-12", Generation: 12, SourceDir: source, Release: viewerinstall.Release{Version: "2.2.1", Digest: digest, ReleaseID: viewerinstall.ReleaseID("2.2.1", digest)}}
	registration := installerTestRegistration{}
	if err := (viewerinstall.Manager{Layout: layout, Registration: registration}).Apply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := vieweragent.SaveUpdateJournal(filepath.Join(layout.StateDir, "update.json"), vieweragent.UpdateJournal{State: "installer_launched", TransactionID: request.TransactionID, Generation: request.Generation, TargetVersion: request.Release.Version, ArtifactSHA256: digest}); err != nil {
		t.Fatal(err)
	}
	if err := recoverAndReconcile(t.Context(), layout, registration); err != nil {
		t.Fatal(err)
	}
	journal, err := vieweragent.LoadUpdateJournal(filepath.Join(layout.StateDir, "update.json"))
	if err != nil || journal.State != "committed" {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

type installerTestRegistration struct{}

func (installerTestRegistration) Stop(context.Context) error                            { return nil }
func (installerTestRegistration) Start(context.Context) error                           { return nil }
func (installerTestRegistration) Validate(context.Context, viewerinstall.Journal) error { return nil }

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
