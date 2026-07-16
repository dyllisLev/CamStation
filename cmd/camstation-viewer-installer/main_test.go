package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		{args: []string{"/s", "--update", "--transaction-id", "update-1", "--command-id", "41", "--payload-hash", digest, "--generation", "1", "--expected-sha", digest, "--parent-pid", "42"}, mode: modeUpdate, silent: true},
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

func TestOwnedInstallerExecutableDetachesBeforeInitialMutation(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	owned := []string{
		filepath.Join(layout.InstallDir, "CamStationViewerSetup.exe"),
		filepath.Join(layout.StateDir, "updater", "CamStationViewerUpdater.exe"),
	}
	for _, executable := range owned {
		if !needsDetachedInstaller(executable, layout, 0, modeInstall) {
			t.Fatalf("owned installer did not detach: %s", executable)
		}
		if needsDetachedInstaller(executable, layout, 42, modeInstall) {
			t.Fatalf("detached helper recursively detached: %s", executable)
		}
		if needsDetachedInstaller(executable, layout, 0, modeUninstall) {
			t.Fatalf("uninstaller incorrectly used initial-install detachment: %s", executable)
		}
	}
	if needsDetachedInstaller(filepath.Join(root, "download", "setup.exe"), layout, 0, modeInstall) {
		t.Fatal("external installer detached unnecessarily")
	}
	options, err := parseInstallerArgs([]string{"--recover", "--detached-parent-pid", "42"})
	if err != nil || options.detachedParentPID != 42 {
		t.Fatalf("detached options=%+v err=%v", options, err)
	}
	if _, err := parseInstallerArgs([]string{"--update", "--detached-parent-pid", "42"}); err == nil {
		t.Fatal("update accepted initial-install detachment handoff")
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

func TestInitialReleaseIdentityUsesSetupArtifactSHA(t *testing.T) {
	setup := filepath.Join(t.TempDir(), "CamStationViewerSetup.exe")
	if err := os.WriteFile(setup, []byte("MZ exact published installer"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := viewerinstall.PayloadManifest{Version: "2.0.0", Digest: strings.Repeat("b", 64)}
	request, err := initialReleaseRequest(manifest, t.TempDir(), setup, 77)
	if err != nil {
		t.Fatal(err)
	}
	if request.Release.Digest != "d64227086ce1a79de0e360083ded4d98e16dbadded7c97bf91c9b82c9adb66b4" || request.Release.Digest == manifest.Digest {
		t.Fatalf("release digest=%q manifest digest=%q", request.Release.Digest, manifest.Digest)
	}
}

func TestUpdaterRequiresExactDurableAgentHandoff(t *testing.T) {
	digest := strings.Repeat("a", 64)
	options := installerOptions{mode: modeUpdate, transactionID: "update-7", commandID: 41, payloadHash: "payload-41", generation: 7, expectedSHA: digest}
	journal := vieweragent.UpdateJournal{State: "installer_launched", TransactionID: "update-7", CommandID: 41, PayloadHash: "payload-41", Generation: 7, ArtifactSHA256: digest, TargetVersion: "2.0.7"}
	if err := validateUpdateHandoff(journal, options, "2.0.7"); err != nil {
		t.Fatal(err)
	}
	journal.Generation++
	if err := validateUpdateHandoff(journal, options, "2.0.7"); err == nil {
		t.Fatal("mismatched Agent handoff was accepted")
	}
}

func TestUpdaterPromotesHandoffOnlyAfterDurableExactPreparation(t *testing.T) {
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
	request := installerUpdateRequest(t, layout, options, "2.0.8")
	owner, err := viewerinstall.Acquire(layout)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	manager := viewerinstall.Manager{Layout: layout, Registration: installerTestRegistration{}}
	manager.AfterPreparing = func(transaction viewerinstall.Journal) error {
		persisted, err := viewerinstall.LoadJournal(layout)
		if err != nil || persisted.Phase != viewerinstall.PhasePreparing {
			t.Fatalf("persisted=%+v err=%v", persisted, err)
		}
		if err := promoteUpdateHandoff(layout, options, "2.0.8", transaction); err != nil {
			return err
		}
		return promoteUpdateHandoff(layout, options, "2.0.8", transaction)
	}
	if err := manager.ApplyOwned(t.Context(), owner, request); err != nil {
		t.Fatal(err)
	}
	promoted, err := vieweragent.LoadUpdateJournal(filepath.Join(layout.StateDir, "update.json"))
	if err != nil || promoted.State != "installer_launched" {
		t.Fatalf("promoted=%+v err=%v", promoted, err)
	}
}

func TestClaimedHandoffReturnsToResumableAfterPowerLossRecovery(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	options := installerOptions{mode: modeUpdate, transactionID: "update-9", generation: 9, expectedSHA: digest}
	if err := vieweragent.SaveUpdateJournal(filepath.Join(layout.StateDir, "update.json"), vieweragent.UpdateJournal{State: "launching_installer", TransactionID: options.transactionID, Generation: options.generation, ArtifactSHA256: digest, TargetVersion: "2.0.9"}); err != nil {
		t.Fatal(err)
	}
	request := installerUpdateRequest(t, layout, options, "2.0.9")
	owner, err := viewerinstall.Acquire(layout)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("power loss after handoff claim")
	manager := viewerinstall.Manager{Layout: layout, Registration: installerTestRegistration{}}
	manager.AfterPreparing = func(transaction viewerinstall.Journal) error {
		if err := promoteUpdateHandoff(layout, options, "2.0.9", transaction); err != nil {
			return err
		}
		return injected
	}
	applyErr := manager.ApplyOwned(t.Context(), owner, request)
	_ = owner.Close()
	if !errors.Is(applyErr, injected) {
		t.Fatalf("ApplyOwned error=%v", applyErr)
	}
	if err := manager.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := vieweragent.ReconcileCommittedUpdate(layout.StateDir); err != nil {
		t.Fatal(err)
	}
	journal, err := vieweragent.LoadUpdateJournal(filepath.Join(layout.StateDir, "update.json"))
	if err != nil || journal.State != "launching_installer" {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

func TestOwnedUpdateObservationAcceptsOnlyExactCommittedTransaction(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	options := installerOptions{mode: modeUpdate, transactionID: "update-10", generation: 10, expectedSHA: strings.Repeat("a", 64)}
	request := installerUpdateRequest(t, layout, options, "2.0.10")
	owner, err := viewerinstall.Acquire(layout)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := (viewerinstall.Manager{Layout: layout, Registration: installerTestRegistration{}}).ApplyOwned(t.Context(), owner, request); err != nil {
		t.Fatal(err)
	}
	matched, err := observeExactCommitted(t.Context(), layout, options, "2.0.10", time.Millisecond)
	if err != nil || !matched {
		t.Fatalf("exact matched=%v err=%v", matched, err)
	}
	other := options
	other.transactionID = "unrelated"
	matched, err = observeExactCommitted(t.Context(), layout, other, "2.0.10", time.Millisecond)
	if err != nil || matched {
		t.Fatalf("unrelated matched=%v err=%v", matched, err)
	}
}

func TestUninstallDoesNotMutateWhileUpdateOwnsTransaction(t *testing.T) {
	root := t.TempDir()
	layout := viewerinstall.Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	owner, err := viewerinstall.Acquire(layout)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	unregistered, removed := false, false
	err = uninstallInstallation(t.Context(), layout, func(context.Context, viewerinstall.Layout) error {
		unregistered = true
		return nil
	}, func(viewerinstall.Layout) error {
		removed = true
		return nil
	})
	if !errors.Is(err, viewerinstall.ErrUpdateOwned) || unregistered || removed {
		t.Fatalf("err=%v unregistered=%v removed=%v", err, unregistered, removed)
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

func installerUpdateRequest(t *testing.T, layout viewerinstall.Layout, options installerOptions, version string) viewerinstall.Request {
	t.Helper()
	source := filepath.Join(t.TempDir(), "release")
	if err := os.MkdirAll(filepath.Join(source, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"camstation-viewer-agent.exe": []byte("agent"), "viewer/CamStationViewer.exe": []byte("viewer")} {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	release := viewerinstall.Release{Version: version, Digest: options.expectedSHA, ReleaseID: viewerinstall.ReleaseID(version, options.expectedSHA)}
	return viewerinstall.Request{TransactionID: options.transactionID, CommandID: options.commandID, PayloadHash: options.payloadHash, Generation: options.generation, SourceDir: source, Release: release}
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
