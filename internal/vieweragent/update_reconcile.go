package vieweragent

import (
	"errors"
	"path/filepath"
	"strings"

	"camstation/internal/viewerinstall"
)

func ReconcileCommittedUpdate(stateDir string) (bool, error) {
	return reconcileCommittedUpdate(stateDir, SaveUpdateJournal)
}

func reconcileCommittedUpdate(stateDir string, save func(string, UpdateJournal) error) (bool, error) {
	if !filepath.IsAbs(stateDir) || save == nil {
		return false, errors.New("absolute update state directory is required")
	}
	transaction, err := viewerinstall.LoadJournal(viewerinstall.Layout{InstallDir: filepath.Join(stateDir, "unused"), StateDir: stateDir})
	if err != nil {
		return false, err
	}
	if transaction.Phase != viewerinstall.PhaseCommitted {
		return false, nil
	}
	path := filepath.Join(stateDir, "update.json")
	journal, err := LoadUpdateJournal(path)
	if err != nil {
		return false, err
	}
	matched := journal.TransactionID != "" && journal.TransactionID == transaction.TransactionID &&
		journal.Generation == transaction.Generation && journal.TargetVersion == transaction.Release.Version &&
		strings.EqualFold(journal.ArtifactSHA256, transaction.Release.Digest)
	if !matched {
		return false, nil
	}
	if journal.State == "committed" {
		return true, nil
	}
	journal.State = "committed"
	journal.LastError = ""
	if err := save(path, journal); err != nil {
		return false, err
	}
	return true, nil
}
