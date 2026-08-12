package viewerinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const initialCompensationTimeout = 60 * time.Second

const (
	PhaseInstallBackedUp          Phase = "install_backed_up"
	PhaseInstallEntryPointsGone   Phase = "install_entry_points_removed"
	PhaseInstallStable            Phase = "install_stable_payload"
	PhaseInstallRuntimeRegistered Phase = "install_runtime_registered"
	PhaseInstallConfigured        Phase = "install_configured"
)

type InitialRegistration interface {
	Registration
	Disable(context.Context) error
	EnableRuntime(context.Context) error
	RegisterRuntime(context.Context, RegistrationOptions) (string, error)
	RegisterUninstall(context.Context) error
	Unregister(context.Context) error
}

type InitialRequest struct {
	Transaction          Request
	PayloadDir           string
	SetupPath            string
	RegistrationOptions  RegistrationOptions
	Configure            func(serviceSID string) error
	PreviousRegistration func() (RegistrationOptions, error)
}

type initialSnapshot struct {
	SchemaVersion             int                   `json:"schemaVersion"`
	Mode                      string                `json:"mode"`
	PreviousMonitoringUserSID string                `json:"previousMonitoringUserSid,omitempty"`
	ReleaseExisted            bool                  `json:"releaseExisted"`
	Files                     []initialFileSnapshot `json:"files"`
}

type initialFileSnapshot struct {
	Name    string `json:"name"`
	Existed bool   `json:"existed"`
}

type initialOwnedFile struct {
	name string
	path string
}

func (manager Manager) InstallInitial(ctx context.Context, request InitialRequest) (result error) {
	owner, err := Acquire(manager.Layout)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, owner.Close()) }()
	return manager.installInitialOwned(ctx, owner, request)
}

func (manager Manager) installInitialOwned(ctx context.Context, owner *Ownership, request InitialRequest) error {
	registration, ok := manager.Registration.(InitialRegistration)
	if !ok {
		return errors.New("initial installation registration adapter is required")
	}
	if !owner.owns(manager.Layout) {
		return errors.New("matching transaction ownership is required")
	}
	if err := validateInitialRequest(request); err != nil {
		return err
	}
	journal, err := LoadJournal(manager.Layout)
	if err != nil {
		return err
	}
	if present, presentErr := initialSnapshotPresent(manager.Layout, journal.TransactionID); presentErr != nil {
		return presentErr
	} else if present {
		if err := manager.recoverInitialLocked(ctx, &journal, registration); err != nil {
			return err
		}
		journal, err = LoadJournal(manager.Layout)
		if err != nil {
			return err
		}
	} else if incomplete(journal.Phase) {
		if err := manager.recoverLocked(ctx, &journal); err != nil {
			return err
		}
	}

	_, previous, err := prepareInitialSnapshot(manager.Layout, request)
	if err != nil {
		return err
	}
	journal.TransactionID = request.Transaction.TransactionID
	journal.Generation = request.Transaction.Generation
	journal.Release = request.Transaction.Release
	journal.Previous = previous
	journal.RollbackState = ""
	journal.Phase = PhaseInstallBackedUp
	if err := saveJournal(manager.Layout, journal); err != nil {
		return errors.Join(err, removeInitialSnapshot(manager.Layout, request.Transaction.TransactionID))
	}
	if err := manager.failAfter(PhaseInstallBackedUp); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := registration.Disable(ctx); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := manager.advanceInitial(&journal, PhaseInstallEntryPointsGone); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := InstallStablePayload(manager.Layout, request.PayloadDir, request.SetupPath); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := manager.advanceInitial(&journal, PhaseInstallStable); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	stagedOptions := request.RegistrationOptions
	stagedOptions.Staged = true
	serviceSID, err := registration.RegisterRuntime(ctx, stagedOptions)
	if err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := manager.advanceInitial(&journal, PhaseInstallRuntimeRegistered); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := request.Configure(serviceSID); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := manager.advanceInitial(&journal, PhaseInstallConfigured); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}

	// The bounded snapshot marker remains the durable rollback authority while
	// the release manager uses its existing phase sequence.
	journal.Phase = ""
	if err := saveJournal(manager.Layout, journal); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	releaseManager := manager
	releaseManager.Registration = initialActivationRegistration{InitialRegistration: registration}
	if err := releaseManager.applyOwned(ctx, owner, request.Transaction, false); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := registration.RegisterUninstall(ctx); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	if err := removeInitialSnapshot(manager.Layout, request.Transaction.TransactionID); err != nil {
		return manager.compensateInitial(ctx, err, registration)
	}
	return nil
}

type initialActivationRegistration struct{ InitialRegistration }

func (registration initialActivationRegistration) Start(ctx context.Context) error {
	if err := registration.EnableRuntime(ctx); err != nil {
		return err
	}
	return registration.InitialRegistration.Start(ctx)
}

func (manager Manager) advanceInitial(journal *Journal, phase Phase) error {
	journal.Phase = phase
	if err := saveJournal(manager.Layout, *journal); err != nil {
		return err
	}
	return manager.failAfter(phase)
}

func (manager Manager) failAfter(phase Phase) error {
	if manager.FailAfter == nil {
		return nil
	}
	return manager.FailAfter(phase)
}

func (manager Manager) compensateInitial(ctx context.Context, cause error, registration InitialRegistration) error {
	journal, err := LoadJournal(manager.Layout)
	if err != nil {
		return errors.Join(cause, err)
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), initialCompensationTimeout)
	defer cancel()
	return errors.Join(cause, manager.recoverInitialLocked(cleanupCtx, &journal, registration))
}

func (manager Manager) recoverInitialLocked(ctx context.Context, journal *Journal, registration InitialRegistration) error {
	snapshot, err := loadInitialSnapshot(manager.Layout, journal.TransactionID)
	if err != nil {
		return err
	}
	if err := registration.Disable(ctx); err != nil {
		return err
	}
	if err := restoreMachineState(manager.Layout, journal.TransactionID); err != nil {
		return err
	}
	if err := restoreInitialSnapshot(manager.Layout, journal.TransactionID, snapshot); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(manager.Layout.StagingRoot(), journal.TransactionID)); err != nil {
		return err
	}
	if !snapshot.ReleaseExisted && journal.Release.ReleaseID != "" {
		if err := os.RemoveAll(manager.Layout.ReleaseDir(journal.Release.ReleaseID)); err != nil {
			return err
		}
		if err := syncDir(manager.Layout.ReleaseRoot()); err != nil {
			return err
		}
	}
	if snapshot.Mode == "repair" {
		if journal.Previous == nil {
			return errors.New("repair snapshot is missing previous release")
		}
		options := RegistrationOptions{MonitoringUserSID: snapshot.PreviousMonitoringUserSID}
		if _, err := registration.RegisterRuntime(ctx, options); err != nil {
			return err
		}
		if err := registration.RegisterUninstall(ctx); err != nil {
			return err
		}
		if err := registration.Start(ctx); err != nil {
			return err
		}
		previousRelease := Release{
			Version: journal.Previous.Version, Digest: journal.Previous.Digest, ReleaseID: journal.Previous.ReleaseID,
		}
		if err := registration.Validate(ctx, Journal{Release: previousRelease}); err != nil {
			return err
		}
	} else if err := registration.Unregister(ctx); err != nil {
		return err
	}
	journal.Phase = PhaseRolledBack
	journal.RollbackState = "initial_install_restored"
	if err := saveJournal(manager.Layout, *journal); err != nil {
		return err
	}
	return removeInitialSnapshot(manager.Layout, journal.TransactionID)
}

func validateInitialRequest(request InitialRequest) error {
	if err := validateRequest(request.Transaction); err != nil {
		return err
	}
	if !filepath.IsAbs(request.PayloadDir) || !filepath.IsAbs(request.SetupPath) || request.Configure == nil || request.PreviousRegistration == nil || !validTaskSID(request.RegistrationOptions.MonitoringUserSID) {
		return errors.New("invalid initial installation request")
	}
	return nil
}

func prepareInitialSnapshot(layout Layout, request InitialRequest) (initialSnapshot, *Current, error) {
	files := initialOwnedFiles(layout)
	snapshot := initialSnapshot{SchemaVersion: SchemaVersion, Mode: "clean", Files: make([]initialFileSnapshot, 0, len(files))}
	coreExisting := false
	for index, file := range files {
		info, err := os.Stat(file.path)
		if errors.Is(err, os.ErrNotExist) {
			snapshot.Files = append(snapshot.Files, initialFileSnapshot{Name: file.name})
			continue
		}
		if err != nil || !info.Mode().IsRegular() {
			return initialSnapshot{}, nil, errors.New("existing installation contains an invalid owned file")
		}
		snapshot.Files = append(snapshot.Files, initialFileSnapshot{Name: file.name, Existed: true})
		if index < len(stableInstallPaths(layout))+2 {
			coreExisting = true
		}
	}
	var previous *Current
	if coreExisting {
		snapshot.Mode = "repair"
		current, err := LoadCurrent(layout)
		if err != nil {
			return initialSnapshot{}, nil, errors.New("existing installation is not recoverable for repair")
		}
		if err := requireReleaseFiles(layout.ReleaseDir(current.ReleaseID)); err != nil {
			return initialSnapshot{}, nil, errors.New("existing installation release is incomplete")
		}
		for index := 0; index < len(stableInstallPaths(layout))+2; index++ {
			if !snapshot.Files[index].Existed {
				return initialSnapshot{}, nil, errors.New("existing installation is not recoverable for repair")
			}
		}
		options, err := request.PreviousRegistration()
		if err != nil || !validTaskSID(options.MonitoringUserSID) {
			return initialSnapshot{}, nil, errors.New("existing installation registration is not recoverable for repair")
		}
		snapshot.PreviousMonitoringUserSID = options.MonitoringUserSID
		previous = &current
	}
	if _, err := os.Stat(layout.ReleaseDir(request.Transaction.Release.ReleaseID)); err == nil {
		snapshot.ReleaseExisted = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return initialSnapshot{}, nil, err
	}
	backupDir := initialSnapshotFilesDir(layout, request.Transaction.TransactionID)
	if err := os.RemoveAll(filepath.Dir(backupDir)); err != nil {
		return initialSnapshot{}, nil, err
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return initialSnapshot{}, nil, err
	}
	for index, file := range files {
		if snapshot.Files[index].Existed {
			if err := copyFileAtomic(file.path, filepath.Join(backupDir, file.name)); err != nil {
				return initialSnapshot{}, nil, err
			}
		}
	}
	if err := atomicWriteJSON(initialSnapshotPath(layout, request.Transaction.TransactionID), snapshot); err != nil {
		return initialSnapshot{}, nil, err
	}
	return snapshot, previous, nil
}

func restoreInitialSnapshot(layout Layout, transactionID string, snapshot initialSnapshot) error {
	files := initialOwnedFiles(layout)
	if len(snapshot.Files) != len(files) {
		return errors.New("invalid initial installation snapshot")
	}
	for index, file := range files {
		entry := snapshot.Files[index]
		if entry.Name != file.name {
			return errors.New("invalid initial installation snapshot")
		}
		if entry.Existed {
			if err := copyFileAtomic(filepath.Join(initialSnapshotFilesDir(layout, transactionID), file.name), file.path); err != nil {
				return err
			}
			continue
		}
		if err := removeFileAtomic(file.path); err != nil {
			return err
		}
	}
	return nil
}

func loadInitialSnapshot(layout Layout, transactionID string) (initialSnapshot, error) {
	var snapshot initialSnapshot
	if transactionID == "" {
		return snapshot, errors.New("initial installation transaction is missing")
	}
	if err := readJSON(initialSnapshotPath(layout, transactionID), &snapshot); err != nil {
		return initialSnapshot{}, err
	}
	if snapshot.SchemaVersion != SchemaVersion || (snapshot.Mode != "clean" && snapshot.Mode != "repair") {
		return initialSnapshot{}, errors.New("invalid initial installation snapshot")
	}
	return snapshot, nil
}

func initialSnapshotPresent(layout Layout, transactionID string) (bool, error) {
	if transactionID == "" {
		return false, nil
	}
	_, err := os.Stat(initialSnapshotPath(layout, transactionID))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func removeInitialSnapshot(layout Layout, transactionID string) error {
	return removeFileAtomic(initialSnapshotPath(layout, transactionID))
}

func initialSnapshotPath(layout Layout, transactionID string) string {
	return filepath.Join(layout.TransactionBackup(transactionID), "initial.json")
}

func initialSnapshotFilesDir(layout Layout, transactionID string) string {
	return filepath.Join(layout.TransactionBackup(transactionID), "initial-files")
}

func initialOwnedFiles(layout Layout) []initialOwnedFile {
	return []initialOwnedFile{
		{name: "host.exe", path: stableHostPath(layout)},
		{name: "bootstrap.exe", path: stableBootstrapPath(layout)},
		{name: "setup.exe", path: filepath.Join(layout.InstallDir, "CamStationViewerSetup.exe")},
		{name: "updater.exe", path: stableUpdaterPath(layout)},
		{name: "config.json", path: filepath.Join(layout.StateDir, "config.json")},
		{name: "current.json", path: layout.CurrentPath()},
		{name: "viewer-task.xml", path: filepath.Join(layout.StateDir, "viewer-task.xml")},
		{name: "recovery-task.xml", path: filepath.Join(layout.StateDir, "recovery-task.xml")},
		{name: "state.json", path: filepath.Join(layout.StateDir, "state.json")},
		{name: "commands.json", path: filepath.Join(layout.StateDir, "commands.json")},
		{name: "update.json", path: filepath.Join(layout.StateDir, "update.json")},
	}
}

func removeFileAtomic(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
