package viewerinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Phase string

const (
	PhasePreparing       Phase = "preparing"
	PhaseStaged          Phase = "staged"
	PhasePointerBackedUp Phase = "pointer_backed_up"
	PhaseActivated       Phase = "activated"
	PhaseServiceStarted  Phase = "service_started"
	PhaseValidating      Phase = "validating"
	PhaseRollingBack     Phase = "rolling_back"
	PhaseRolledBack      Phase = "rolled_back"
	PhaseCommitted       Phase = "committed"
)

var (
	ErrUpdateOwned = errors.New("another update owns the machine transaction")
	ErrQuarantined = errors.New("update target is quarantined")
)

type Release struct {
	Version   string `json:"version"`
	Digest    string `json:"digest"`
	ReleaseID string `json:"releaseId"`
}

type Request struct {
	TransactionID string
	Generation    int64
	SourceDir     string
	Release       Release
}

type Quarantine struct {
	Version    string    `json:"version"`
	Digest     string    `json:"digest"`
	Generation int64     `json:"generation"`
	At         time.Time `json:"at"`
	Reason     string    `json:"reason"`
}

type Journal struct {
	SchemaVersion int          `json:"schemaVersion"`
	TransactionID string       `json:"transactionId,omitempty"`
	Generation    int64        `json:"generation,omitempty"`
	Release       Release      `json:"release,omitempty"`
	Previous      *Current     `json:"previous,omitempty"`
	Phase         Phase        `json:"phase,omitempty"`
	RollbackState string       `json:"rollbackState,omitempty"`
	Quarantined   []Quarantine `json:"quarantined,omitempty"`
	UpdatedAt     time.Time    `json:"updatedAt,omitempty"`
}

func (journal Journal) IsQuarantined(version, digest string, generation int64) bool {
	for _, failed := range journal.Quarantined {
		if failed.Version == version && failed.Digest == strings.ToLower(digest) && failed.Generation == generation {
			return true
		}
	}
	return false
}

func LoadJournal(layout Layout) (Journal, error) {
	var journal Journal
	err := readJSON(layout.JournalPath(), &journal)
	if errors.Is(err, os.ErrNotExist) {
		return Journal{SchemaVersion: SchemaVersion}, nil
	}
	if err != nil {
		return Journal{}, err
	}
	if journal.SchemaVersion != SchemaVersion {
		return Journal{}, errors.New("unsupported transaction journal schema")
	}
	return journal, nil
}

func saveJournal(layout Layout, journal Journal) error {
	journal.SchemaVersion = SchemaVersion
	journal.UpdatedAt = time.Now().UTC()
	return atomicWriteJSON(layout.JournalPath(), journal)
}

type Registration interface {
	Stop(context.Context) error
	Start(context.Context) error
	Validate(context.Context, Journal) error
}

type Manager struct {
	Layout         Layout
	Registration   Registration
	AfterPreparing func(Journal) error
	FailAfter      func(Phase) error
}

func (manager Manager) Apply(ctx context.Context, request Request) error {
	owner, err := Acquire(manager.Layout)
	if err != nil {
		return err
	}
	defer owner.Close()
	return manager.ApplyOwned(ctx, owner, request)
}

func (manager Manager) ApplyOwned(ctx context.Context, owner *Ownership, request Request) error {
	return manager.applyOwned(ctx, owner, request, true)
}

func (manager Manager) applyOwned(ctx context.Context, owner *Ownership, request Request, recoverInitial bool) error {
	if !owner.owns(manager.Layout) {
		return errors.New("matching transaction ownership is required")
	}
	if err := manager.Layout.Ensure(); err != nil {
		return err
	}
	if manager.Registration == nil {
		return errors.New("registration adapter is required")
	}
	if err := validateRequest(request); err != nil {
		return err
	}
	journal, err := LoadJournal(manager.Layout)
	if err != nil {
		return err
	}
	if recoverInitial {
		present, presentErr := initialSnapshotPresent(manager.Layout, journal.TransactionID)
		if presentErr != nil {
			return presentErr
		}
		if present {
			registration, ok := manager.Registration.(InitialRegistration)
			if !ok {
				return errors.New("initial installation recovery adapter is required")
			}
			if err := manager.recoverInitialLocked(ctx, &journal, registration); err != nil {
				return err
			}
			journal, err = LoadJournal(manager.Layout)
			if err != nil {
				return err
			}
		}
	}
	if journal.Phase == PhaseCommitted && journalMatchesRequest(journal, request) {
		return nil
	}
	if incomplete(journal.Phase) {
		if err := manager.recoverLocked(ctx, &journal); err != nil {
			return err
		}
	}
	if journal.IsQuarantined(request.Release.Version, request.Release.Digest, request.Generation) {
		return ErrQuarantined
	}
	var previous *Current
	if current, loadErr := LoadCurrent(manager.Layout); loadErr == nil {
		previous = &current
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return loadErr
	}
	journal.TransactionID = request.TransactionID
	journal.Generation = request.Generation
	journal.Release = request.Release
	journal.Previous = previous
	journal.RollbackState = ""
	if err := manager.advance(&journal, PhasePreparing); err != nil {
		return err
	}
	if manager.AfterPreparing != nil {
		if err := manager.AfterPreparing(journal); err != nil {
			return err
		}
	}

	staging := filepath.Join(manager.Layout.StagingRoot(), request.TransactionID)
	_ = os.RemoveAll(staging)
	if err := copyTree(request.SourceDir, staging); err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := requireReleaseFiles(staging); err != nil {
		return err
	}
	final := manager.Layout.ReleaseDir(request.Release.ReleaseID)
	if _, statErr := os.Stat(final); errors.Is(statErr, os.ErrNotExist) {
		if err := os.Rename(staging, final); err != nil {
			return err
		}
		if err := syncDir(manager.Layout.ReleaseRoot()); err != nil {
			return err
		}
	} else if statErr != nil {
		return statErr
	} else if err := requireReleaseFiles(final); err != nil {
		return errors.New("existing immutable release is incomplete")
	} else if equal, err := equalTrees(staging, final); err != nil {
		return err
	} else if !equal {
		return errors.New("existing immutable release contents differ")
	}
	if err := manager.advance(&journal, PhaseStaged); err != nil {
		return err
	}
	preserveInitialBackup, err := initialSnapshotPresent(manager.Layout, request.TransactionID)
	if err != nil {
		return err
	}
	if err := backupMachineState(manager.Layout, request.TransactionID, previous, preserveInitialBackup); err != nil {
		return err
	}
	if err := manager.advance(&journal, PhasePointerBackedUp); err != nil {
		return err
	}
	if err := manager.Registration.Stop(ctx); err != nil {
		return manager.rollbackFailure(ctx, &journal, "registration_stop_failed", err)
	}
	if err := SaveCurrent(manager.Layout, currentFor(request.Release)); err != nil {
		return manager.rollbackFailure(ctx, &journal, "activation_failed", err)
	}
	if err := manager.advance(&journal, PhaseActivated); err != nil {
		return err
	}
	if err := manager.Registration.Start(ctx); err != nil {
		return manager.rollbackFailure(ctx, &journal, "registration_start_failed", err)
	}
	if err := manager.advance(&journal, PhaseServiceStarted); err != nil {
		return err
	}
	if err := manager.advance(&journal, PhaseValidating); err != nil {
		return err
	}
	if err := manager.Registration.Validate(ctx, journal); err != nil {
		return manager.rollbackFailure(ctx, &journal, "validation_failed", err)
	}
	return manager.advance(&journal, PhaseCommitted)
}

func journalMatchesRequest(journal Journal, request Request) bool {
	return journal.TransactionID == request.TransactionID && journal.Generation == request.Generation &&
		journal.Release.Version == request.Release.Version && journal.Release.Digest == request.Release.Digest &&
		journal.Release.ReleaseID == request.Release.ReleaseID
}

func equalTrees(left, right string) (bool, error) {
	leftFiles, err := treeHashes(left)
	if err != nil {
		return false, err
	}
	rightFiles, err := treeHashes(right)
	if err != nil || len(leftFiles) != len(rightFiles) {
		return false, err
	}
	for name, hash := range leftFiles {
		if rightFiles[name] != hash {
			return false, nil
		}
	}
	return true, nil
}

func treeHashes(root string) (map[string]string, error) {
	files := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("immutable release contains non-regular file")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
	return files, err
}

func (manager Manager) Recover(ctx context.Context) error {
	owner, err := Acquire(manager.Layout)
	if err != nil {
		return err
	}
	defer owner.Close()
	journal, err := LoadJournal(manager.Layout)
	if err != nil {
		return err
	}
	if present, presentErr := initialSnapshotPresent(manager.Layout, journal.TransactionID); presentErr != nil {
		return presentErr
	} else if present {
		registration, ok := manager.Registration.(InitialRegistration)
		if !ok {
			return errors.New("initial installation recovery adapter is required")
		}
		return manager.recoverInitialLocked(ctx, &journal, registration)
	}
	return manager.recoverLocked(ctx, &journal)
}

func (manager Manager) Rollback(ctx context.Context, transactionID string) error {
	owner, err := Acquire(manager.Layout)
	if err != nil {
		return err
	}
	defer owner.Close()
	journal, err := LoadJournal(manager.Layout)
	if err != nil {
		return err
	}
	if transactionID == "" || journal.TransactionID != transactionID || journal.Previous == nil {
		return errors.New("rollback transaction is unavailable")
	}
	journal.Phase = PhaseRollingBack
	journal.RollbackState = "requested"
	if err := saveJournal(manager.Layout, journal); err != nil {
		return err
	}
	return manager.recoverLocked(ctx, &journal)
}

func (manager Manager) recoverLocked(ctx context.Context, journal *Journal) error {
	if !incomplete(journal.Phase) {
		return nil
	}
	if manager.Registration == nil {
		return errors.New("registration adapter is required")
	}
	if journal.Previous == nil {
		if err := manager.Registration.Stop(ctx); err != nil {
			return err
		}
		if err := RemoveCurrent(manager.Layout); err != nil {
			return err
		}
		if journal.Release.ReleaseID != "" {
			if err := os.RemoveAll(manager.Layout.ReleaseDir(journal.Release.ReleaseID)); err != nil {
				return err
			}
			if err := syncDir(manager.Layout.ReleaseRoot()); err != nil {
				return err
			}
		}
		if journal.TransactionID != "" {
			if err := os.RemoveAll(filepath.Join(manager.Layout.StagingRoot(), journal.TransactionID)); err != nil {
				return err
			}
		}
		journal.RollbackState = "clean_no_release"
		if err := manager.advance(journal, PhaseRolledBack); err != nil {
			return err
		}
		return nil
	}
	if journal.Phase != PhaseRollingBack {
		if err := manager.advance(journal, PhaseRollingBack); err != nil {
			return err
		}
	}
	if err := manager.Registration.Stop(ctx); err != nil {
		return err
	}
	if err := SaveCurrent(manager.Layout, *journal.Previous); err != nil {
		return err
	}
	if err := restoreMachineState(manager.Layout, journal.TransactionID); err != nil {
		return err
	}
	if err := manager.Registration.Start(ctx); err != nil {
		return err
	}
	journal.RollbackState = "restored"
	return manager.advance(journal, PhaseRolledBack)
}

func (manager Manager) rollbackFailure(ctx context.Context, journal *Journal, reason string, cause error) error {
	if !journal.IsQuarantined(journal.Release.Version, journal.Release.Digest, journal.Generation) {
		journal.Quarantined = append(journal.Quarantined, Quarantine{
			Version: journal.Release.Version, Digest: journal.Release.Digest, Generation: journal.Generation,
			At: time.Now().UTC(), Reason: reason,
		})
	}
	if err := manager.advance(journal, PhaseRollingBack); err != nil {
		return errors.Join(cause, err)
	}
	present, err := initialSnapshotPresent(manager.Layout, journal.TransactionID)
	if err != nil {
		return errors.Join(cause, err)
	}
	if present {
		// Initial install owns the wider stable/config/registration snapshot and
		// must restore it before any prior service is restarted.
		return cause
	}
	if err := manager.recoverLocked(ctx, journal); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (manager Manager) advance(journal *Journal, phase Phase) error {
	journal.Phase = phase
	if err := saveJournal(manager.Layout, *journal); err != nil {
		return err
	}
	return manager.failAfter(phase)
}

func validateRequest(request Request) error {
	if request.TransactionID == "" || len(request.TransactionID) > 128 || !validVersion(request.TransactionID) || request.Generation <= 0 || !filepath.IsAbs(request.SourceDir) {
		return errors.New("invalid update transaction request")
	}
	if request.Release.ReleaseID == "" || request.Release.ReleaseID != ReleaseID(request.Release.Version, request.Release.Digest) {
		return errors.New("invalid immutable release identity")
	}
	return nil
}

func requireReleaseFiles(dir string) error {
	for _, name := range []string{"camstation-viewer-agent.exe", filepath.Join("viewer", "CamStationViewer.exe")} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("release is missing %s", name)
		}
	}
	return nil
}

func incomplete(phase Phase) bool {
	return phase != "" && phase != PhaseCommitted && phase != PhaseRolledBack
}

func backupMachineState(layout Layout, transactionID string, current *Current, preserveRoot bool) error {
	backup := layout.TransactionBackup(transactionID)
	if preserveRoot {
		if err := os.RemoveAll(filepath.Join(backup, "state")); err != nil {
			return err
		}
		if err := removeFileAtomic(filepath.Join(backup, "current.json")); err != nil {
			return err
		}
	} else {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(backup, "state"), 0o700); err != nil {
		return err
	}
	if current != nil {
		if err := atomicWriteJSON(filepath.Join(backup, "current.json"), current); err != nil {
			return err
		}
	}
	for _, name := range []string{"config.json", "state.json", "commands.json", "update.json"} {
		source := filepath.Join(layout.StateDir, name)
		data, err := os.ReadFile(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(backup, "state", name), data, 0o600); err != nil {
			return err
		}
	}
	return syncDir(backup)
}

func restoreMachineState(layout Layout, transactionID string) error {
	backup := filepath.Join(layout.TransactionBackup(transactionID), "state")
	entries, err := os.ReadDir(backup)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(backup, entry.Name()))
		if err != nil {
			return err
		}
		if err := atomicWrite(filepath.Join(layout.StateDir, entry.Name()), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
