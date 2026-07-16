package viewerinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestTransactionRecoveryAlwaysLeavesOneCompleteRelease(t *testing.T) {
	phases := []Phase{
		PhaseStaged,
		PhasePointerBackedUp,
		PhaseActivated,
		PhaseServiceStarted,
		PhaseValidating,
		PhaseRollingBack,
	}
	for _, failedAfter := range phases {
		t.Run(string(failedAfter), func(t *testing.T) {
			manager, request, old := transactionFixture(t)
			if failedAfter == PhaseRollingBack {
				manager.Registration = failingValidationRegistration{}
			}
			manager.FailAfter = func(phase Phase) error {
				if phase == failedAfter {
					return errInjectedPowerLoss
				}
				return nil
			}
			if err := manager.Apply(t.Context(), request); !errors.Is(err, errInjectedPowerLoss) {
				t.Fatalf("Apply error=%v", err)
			}

			reopened := Manager{Layout: manager.Layout, Registration: noOpRegistration{}}
			if err := reopened.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			current, err := LoadCurrent(manager.Layout)
			if err != nil {
				t.Fatalf("current pointer missing after recovery: %v", err)
			}
			if current.ReleaseID != old.ReleaseID && current.ReleaseID != request.Release.ReleaseID {
				t.Fatalf("unexpected current release %q", current.ReleaseID)
			}
			assertCompleteRelease(t, manager.Layout, current)
			assertNoMixedRelease(t, manager.Layout, old, request.Release)
		})
	}
}

func TestCommittedTransactionKeepsNewReleaseAcrossRecovery(t *testing.T) {
	manager, request, _ := transactionFixture(t)
	if err := manager.Apply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := manager.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	current, err := LoadCurrent(manager.Layout)
	if err != nil || current.ReleaseID != request.Release.ReleaseID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	journal, err := LoadJournal(manager.Layout)
	if err != nil || journal.Phase != PhaseCommitted {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}
}

func TestExplicitRollbackRestoresTransactionPreviousRelease(t *testing.T) {
	manager, request, old := transactionFixture(t)
	if err := manager.Apply(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := manager.Rollback(t.Context(), request.TransactionID); err != nil {
		t.Fatal(err)
	}
	current, err := LoadCurrent(manager.Layout)
	if err != nil || current.ReleaseID != old.ReleaseID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
}

func TestFailedTargetIsQuarantinedByVersionDigestAndGeneration(t *testing.T) {
	manager, request, _ := transactionFixture(t)
	manager.Registration = failingValidationRegistration{}
	if err := manager.Apply(t.Context(), request); err == nil {
		t.Fatal("failed validation was accepted")
	}
	if err := manager.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	journal, err := LoadJournal(manager.Layout)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.IsQuarantined(request.Release.Version, request.Release.Digest, request.Generation) {
		t.Fatalf("failed target was not quarantined: %+v", journal.Quarantined)
	}
	if err := manager.Apply(t.Context(), request); !errors.Is(err, ErrQuarantined) {
		t.Fatalf("quarantined target error=%v", err)
	}
	rearmed := request
	rearmed.Generation++
	manager.Registration = noOpRegistration{}
	if err := manager.Apply(t.Context(), rearmed); err != nil {
		t.Fatalf("new generation did not rearm target: %v", err)
	}
}

func TestTransactionOwnershipIsExclusive(t *testing.T) {
	manager, request, _ := transactionFixture(t)
	owner, err := Acquire(manager.Layout)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := manager.Apply(context.Background(), request); !errors.Is(err, ErrUpdateOwned) {
		t.Fatalf("second owner error=%v", err)
	}
}

func TestImmutableReleaseDirectoryCannotBeReusedWithDifferentContents(t *testing.T) {
	manager, request, old := transactionFixture(t)
	poisoned := manager.Layout.ReleaseDir(request.Release.ReleaseID)
	if err := os.MkdirAll(filepath.Join(poisoned, "viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poisoned, "camstation-viewer-agent.exe"), []byte("poisoned-agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(poisoned, "viewer", "CamStationViewer.exe"), []byte("poisoned-viewer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := manager.Apply(t.Context(), request); err == nil {
		t.Fatal("different contents reused immutable release directory")
	}
	current, err := LoadCurrent(manager.Layout)
	if err != nil || current.ReleaseID != old.ReleaseID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
}

var errInjectedPowerLoss = errors.New("injected power loss")

type noOpRegistration struct{}

func (noOpRegistration) Stop(context.Context) error              { return nil }
func (noOpRegistration) Start(context.Context) error             { return nil }
func (noOpRegistration) Validate(context.Context, Journal) error { return nil }

type failingValidationRegistration struct{ noOpRegistration }

func (failingValidationRegistration) Validate(context.Context, Journal) error {
	return errors.New("new release unhealthy")
}

func transactionFixture(t *testing.T) (Manager, Request, Current) {
	t.Helper()
	root := t.TempDir()
	layout := Layout{InstallDir: filepath.Join(root, "program-files"), StateDir: filepath.Join(root, "program-data")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	oldSource := makeReleaseSource(t, root, "old")
	oldDigest := directoryDigest(t, oldSource)
	oldRelease := Release{Version: "1.0.0", Digest: oldDigest, ReleaseID: ReleaseID("1.0.0", oldDigest)}
	oldDir := layout.ReleaseDir(oldRelease.ReleaseID)
	if err := copyTree(oldSource, oldDir); err != nil {
		t.Fatal(err)
	}
	old := Current{SchemaVersion: SchemaVersion, ReleaseID: oldRelease.ReleaseID, Version: oldRelease.Version, Digest: oldRelease.Digest}
	if err := SaveCurrent(layout, old); err != nil {
		t.Fatal(err)
	}
	newSource := makeReleaseSource(t, root, "new")
	newDigest := directoryDigest(t, newSource)
	request := Request{
		TransactionID: "tx-7",
		Generation:    7,
		SourceDir:     newSource,
		Release:       Release{Version: "2.0.0", Digest: newDigest, ReleaseID: ReleaseID("2.0.0", newDigest)},
	}
	return Manager{Layout: layout, Registration: noOpRegistration{}}, request, old
}

func makeReleaseSource(t *testing.T, root, value string) string {
	t.Helper()
	dir := filepath.Join(root, "source-"+value)
	if err := os.MkdirAll(filepath.Join(dir, "viewer"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"camstation-viewer-agent.exe": value + "-agent", "viewer/CamStationViewer.exe": value + "-viewer"} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func directoryDigest(t *testing.T, dir string) string {
	t.Helper()
	hash := sha256.New()
	for _, name := range []string{"camstation-viewer-agent.exe", "viewer/CamStationViewer.exe"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		hash.Write([]byte(name))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func assertCompleteRelease(t *testing.T, layout Layout, current Current) {
	t.Helper()
	for _, name := range []string{"camstation-viewer-agent.exe", "viewer/CamStationViewer.exe"} {
		info, err := os.Stat(filepath.Join(layout.ReleaseDir(current.ReleaseID), filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("release %s incomplete at %s: %v", current.ReleaseID, name, err)
		}
	}
}

func assertNoMixedRelease(t *testing.T, layout Layout, old Current, next Release) {
	t.Helper()
	current, _ := LoadCurrent(layout)
	agent, _ := os.ReadFile(filepath.Join(layout.ReleaseDir(current.ReleaseID), "camstation-viewer-agent.exe"))
	viewer, _ := os.ReadFile(filepath.Join(layout.ReleaseDir(current.ReleaseID), "viewer", "CamStationViewer.exe"))
	if (string(agent) == "old-agent") != (string(viewer) == "old-viewer") {
		t.Fatalf("mixed old/new release: agent=%q viewer=%q old=%s new=%s", agent, viewer, old.ReleaseID, next.ReleaseID)
	}
}
