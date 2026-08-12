package viewerinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitialInstallFailureRemovesOnlyCleanInstallPayloadAndRegistrations(t *testing.T) {
	fixture := initialInstallFixture(t, false)
	unrelated := filepath.Join(fixture.manager.Layout.ReleaseRoot(), "unrelated-release")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	registrationErr := errors.New("uninstall registration failed")
	fixture.registration.failUninstallOnce = registrationErr

	err := fixture.manager.InstallInitial(t.Context(), fixture.request)
	if !errors.Is(err, registrationErr) {
		t.Fatalf("InstallInitial error=%v", err)
	}
	for _, path := range initialOwnedPaths(fixture.manager.Layout) {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed clean install left %s: %v", path, statErr)
		}
	}
	if _, statErr := os.Stat(fixture.manager.Layout.ReleaseDir(fixture.request.Transaction.Release.ReleaseID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed clean install left target release: %v", statErr)
	}
	if _, statErr := os.Stat(unrelated); statErr != nil {
		t.Fatalf("failed clean install removed unrelated release: %v", statErr)
	}
	for _, name := range []string{"state.json", "commands.json", "update.json"} {
		if _, statErr := os.Stat(filepath.Join(fixture.manager.Layout.StateDir, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed clean install left newly created %s: %v", name, statErr)
		}
	}
	if fixture.registration.registered || fixture.registration.uninstallRegistered {
		t.Fatalf("failed clean install left registration: %+v", fixture.registration)
	}
	journal, loadErr := LoadJournal(fixture.manager.Layout)
	if loadErr != nil || journal.Phase != PhaseRolledBack {
		t.Fatalf("journal=%+v err=%v", journal, loadErr)
	}
}

func TestInitialRepairFailureRestoresPreviousStableConfigCurrentAndRegistration(t *testing.T) {
	fixture := initialInstallFixture(t, true)
	registrationErr := errors.New("uninstall registration failed")
	fixture.registration.failUninstallOnce = registrationErr

	err := fixture.manager.InstallInitial(t.Context(), fixture.request)
	if !errors.Is(err, registrationErr) {
		t.Fatalf("InstallInitial error=%v", err)
	}
	for _, path := range stableInstallPaths(fixture.manager.Layout) {
		data, readErr := os.ReadFile(path)
		if readErr != nil || string(data) != "old:"+filepath.Base(path) {
			t.Fatalf("stable file %s=%q err=%v", path, data, readErr)
		}
	}
	config, readErr := os.ReadFile(filepath.Join(fixture.manager.Layout.StateDir, "config.json"))
	if readErr != nil || string(config) != "old-config" {
		t.Fatalf("restored config=%q err=%v", config, readErr)
	}
	current, loadErr := LoadCurrent(fixture.manager.Layout)
	if loadErr != nil || current.ReleaseID != fixture.previous.ReleaseID {
		t.Fatalf("current=%+v err=%v", current, loadErr)
	}
	if !fixture.registration.registered || !fixture.registration.uninstallRegistered || fixture.registration.options.MonitoringUserSID != "S-1-5-21-100" {
		t.Fatalf("previous registration not restored: %+v", fixture.registration)
	}
	if fixture.registration.starts < 2 {
		t.Fatalf("previous installation was not restarted: starts=%d", fixture.registration.starts)
	}
	if _, statErr := os.Stat(fixture.manager.Layout.ReleaseDir(fixture.request.Transaction.Release.ReleaseID)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed repair left new target release: %v", statErr)
	}
}

func TestInitialInstallCleanupFailureIsJoinedAndRemainsRecoverable(t *testing.T) {
	fixture := initialInstallFixture(t, false)
	installErr := errors.New("uninstall registration failed")
	cleanupErr := errors.New("registration cleanup failed")
	fixture.registration.failUninstallOnce = installErr
	fixture.registration.failCleanupOnce = cleanupErr

	err := fixture.manager.InstallInitial(t.Context(), fixture.request)
	if !errors.Is(err, installErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("joined error=%v", err)
	}
	if recoverErr := fixture.manager.Recover(t.Context()); recoverErr != nil {
		t.Fatal(recoverErr)
	}
	if fixture.registration.registered || fixture.registration.uninstallRegistered {
		t.Fatalf("retry recovery left registration: %+v", fixture.registration)
	}
}

func TestInitialInstallCompensatesAfterCallerContextIsCanceled(t *testing.T) {
	fixture := initialInstallFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	configureErr := errors.New("configuration failed after timeout")
	fixture.registration.respectContext = true
	fixture.request.Configure = func(string) error {
		cancel()
		return configureErr
	}

	err := fixture.manager.InstallInitial(ctx, fixture.request)
	if !errors.Is(err, configureErr) || errors.Is(err, context.Canceled) {
		t.Fatalf("InstallInitial error=%v", err)
	}
	for _, path := range initialOwnedPaths(fixture.manager.Layout) {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("canceled install left %s: %v", path, statErr)
		}
	}
	if fixture.registration.registered || fixture.registration.uninstallRegistered {
		t.Fatalf("canceled install left registration: %+v", fixture.registration)
	}
}

func TestInitialRepairValidationFailureRestartsOnlyRestoredInstallation(t *testing.T) {
	fixture := initialInstallFixture(t, true)
	validationErr := errors.New("new release unhealthy")
	fixture.registration.failValidationOnce = validationErr

	err := fixture.manager.InstallInitial(t.Context(), fixture.request)
	if !errors.Is(err, validationErr) {
		t.Fatalf("InstallInitial error=%v", err)
	}
	if fixture.registration.starts != 2 {
		t.Fatalf("repair started an intermediate mixed installation: starts=%d", fixture.registration.starts)
	}
	current, loadErr := LoadCurrent(fixture.manager.Layout)
	if loadErr != nil || current.ReleaseID != fixture.previous.ReleaseID {
		t.Fatalf("current=%+v err=%v", current, loadErr)
	}
}

func TestInitialInstallSuccessPublishesUninstallOnlyAfterValidatedRelease(t *testing.T) {
	fixture := initialInstallFixture(t, false)
	if err := fixture.manager.InstallInitial(t.Context(), fixture.request); err != nil {
		t.Fatal(err)
	}
	current, err := LoadCurrent(fixture.manager.Layout)
	if err != nil || current.ReleaseID != fixture.request.Transaction.Release.ReleaseID {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	if !fixture.registration.registered || fixture.registration.staged || !fixture.registration.uninstallRegistered || fixture.registration.starts != 1 || fixture.registration.validations != 1 {
		t.Fatalf("registration=%+v", fixture.registration)
	}
	if fixture.registration.uninstallValidationCount != 1 {
		t.Fatalf("uninstall was published before validation: validation count=%d", fixture.registration.uninstallValidationCount)
	}
	if _, err := os.Stat(initialSnapshotPath(fixture.manager.Layout, fixture.request.Transaction.TransactionID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed initial transaction retained recovery marker: %v", err)
	}
}

func TestInterruptedInitialInstallReconcilesCleanAndRepairAtEveryDurableStage(t *testing.T) {
	stages := []Phase{
		PhaseInstallBackedUp,
		PhaseInstallEntryPointsGone,
		PhaseInstallStable,
		PhaseInstallRuntimeRegistered,
		PhaseInstallConfigured,
		PhasePreparing,
		PhaseStaged,
		PhasePointerBackedUp,
		PhaseActivated,
		PhaseServiceStarted,
		PhaseValidating,
		PhaseRollingBack,
		PhaseCommitted,
	}
	for _, repair := range []bool{false, true} {
		mode := "clean"
		if repair {
			mode = "repair"
		}
		for _, stage := range stages {
			t.Run(mode+"/"+string(stage), func(t *testing.T) {
				fixture := initialInstallFixture(t, repair)
				injected := errors.New("simulated power interruption")
				fixture.manager.FailAfter = func(phase Phase) error {
					if phase == stage {
						return injected
					}
					return nil
				}
				fixture.registration.failCleanupCall = 2
				if stage == PhaseRollingBack {
					fixture.registration.failValidationOnce = errors.New("new release unhealthy")
				}
				if stage == PhaseInstallBackedUp {
					fixture.registration.failCleanupCall = 1
				}
				err := fixture.manager.InstallInitial(t.Context(), fixture.request)
				if !errors.Is(err, injected) || !errors.Is(err, errPowerLossCleanupBlocked) {
					t.Fatalf("interrupted install error=%v", err)
				}
				fixture.manager.FailAfter = nil
				if err := fixture.manager.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
				if repair {
					current, err := LoadCurrent(fixture.manager.Layout)
					if err != nil || current.ReleaseID != fixture.previous.ReleaseID || !fixture.registration.registered || !fixture.registration.uninstallRegistered || fixture.registration.options.MonitoringUserSID != "S-1-5-21-100" {
						t.Fatalf("repair did not reconcile: current=%+v registration=%+v err=%v", current, fixture.registration, err)
					}
				} else {
					if _, err := LoadCurrent(fixture.manager.Layout); !errors.Is(err, os.ErrNotExist) || fixture.registration.registered || fixture.registration.uninstallRegistered {
						t.Fatalf("clean install did not reconcile: registration=%+v current err=%v", fixture.registration, err)
					}
				}
			})
		}
	}
}

type initialFixture struct {
	manager      Manager
	request      InitialRequest
	registration *initialTestRegistration
	previous     Current
}

func initialInstallFixture(t *testing.T, repair bool) initialFixture {
	t.Helper()
	root := t.TempDir()
	layout := Layout{InstallDir: filepath.Join(root, "install"), StateDir: filepath.Join(root, "state")}
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(root, "payload")
	if err := os.MkdirAll(filepath.Join(payload, "stable"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"CamStationViewerHost.exe", "CamStationViewerBootstrap.exe"} {
		if err := os.WriteFile(filepath.Join(payload, "stable", name), []byte("new:"+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	setup := filepath.Join(root, "setup.exe")
	if err := os.WriteFile(setup, []byte("new:setup"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := makeReleaseSource(t, root, "new-initial")
	digest := directoryDigest(t, source)
	transaction := Request{
		TransactionID: "initial-transaction-1",
		Generation:    101,
		SourceDir:     source,
		Release:       Release{Version: "2.0.0", Digest: digest, ReleaseID: ReleaseID("2.0.0", digest)},
	}
	registration := &initialTestRegistration{}
	registration.onStart = func() {
		for _, name := range []string{"state.json", "commands.json", "update.json"} {
			if err := os.WriteFile(filepath.Join(layout.StateDir, name), []byte("new-runtime-state"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	fixture := initialFixture{manager: Manager{Layout: layout, Registration: registration}, registration: registration}
	fixture.request = InitialRequest{
		Transaction:         transaction,
		PayloadDir:          payload,
		SetupPath:           setup,
		RegistrationOptions: RegistrationOptions{MonitoringUserSID: "S-1-5-21-200"},
		Configure: func(serviceSID string) error {
			return os.WriteFile(filepath.Join(layout.StateDir, "config.json"), []byte("new-config:"+serviceSID), 0o600)
		},
		PreviousRegistration: func() (RegistrationOptions, error) {
			return RegistrationOptions{MonitoringUserSID: "S-1-5-21-100"}, nil
		},
	}
	if !repair {
		return fixture
	}
	oldSource := makeReleaseSource(t, root, "old-initial")
	oldDigest := directoryDigest(t, oldSource)
	oldRelease := Release{Version: "1.0.0", Digest: oldDigest, ReleaseID: ReleaseID("1.0.0", oldDigest)}
	if err := copyTree(oldSource, layout.ReleaseDir(oldRelease.ReleaseID)); err != nil {
		t.Fatal(err)
	}
	fixture.previous = currentFor(oldRelease)
	if err := SaveCurrent(layout, fixture.previous); err != nil {
		t.Fatal(err)
	}
	for _, path := range stableInstallPaths(layout) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("old:"+filepath.Base(path)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(layout.StateDir, "config.json"), []byte("old-config"), 0o600); err != nil {
		t.Fatal(err)
	}
	registration.registered = true
	registration.uninstallRegistered = true
	registration.options = RegistrationOptions{MonitoringUserSID: "S-1-5-21-100"}
	return fixture
}

type initialTestRegistration struct {
	registered               bool
	staged                   bool
	uninstallRegistered      bool
	options                  RegistrationOptions
	starts                   int
	validations              int
	uninstallValidationCount int
	failUninstallOnce        error
	failCleanupOnce          error
	failCleanupCall          int
	disableCalls             int
	failValidationOnce       error
	respectContext           bool
	onStart                  func()
}

var errPowerLossCleanupBlocked = errors.New("simulated cleanup interruption")

func (registration *initialTestRegistration) Stop(context.Context) error { return nil }

func (registration *initialTestRegistration) Start(context.Context) error {
	registration.starts++
	if registration.onStart != nil {
		registration.onStart()
	}
	return nil
}

func (registration *initialTestRegistration) Validate(context.Context, Journal) error {
	registration.validations++
	if registration.failValidationOnce != nil {
		err := registration.failValidationOnce
		registration.failValidationOnce = nil
		return err
	}
	return nil
}

func (registration *initialTestRegistration) Disable(ctx context.Context) error {
	if registration.respectContext && ctx.Err() != nil {
		return ctx.Err()
	}
	registration.disableCalls++
	if registration.disableCalls == registration.failCleanupCall {
		return errPowerLossCleanupBlocked
	}
	if registration.failCleanupOnce != nil && registration.registered {
		err := registration.failCleanupOnce
		registration.failCleanupOnce = nil
		return err
	}
	registration.staged = true
	return nil
}

func (registration *initialTestRegistration) EnableRuntime(context.Context) error {
	registration.staged = false
	return nil
}

func (registration *initialTestRegistration) RegisterRuntime(_ context.Context, options RegistrationOptions) (string, error) {
	registration.registered = true
	registration.options = options
	registration.staged = options.Staged
	return "S-1-5-80-123", nil
}

func (registration *initialTestRegistration) RegisterUninstall(context.Context) error {
	registration.uninstallValidationCount = registration.validations
	if registration.failUninstallOnce != nil {
		err := registration.failUninstallOnce
		registration.failUninstallOnce = nil
		registration.uninstallRegistered = true
		return err
	}
	registration.uninstallRegistered = true
	return nil
}

func (registration *initialTestRegistration) Unregister(context.Context) error {
	registration.registered = false
	registration.uninstallRegistered = false
	registration.staged = false
	return nil
}
