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
	path := filepath.Join(stateDir, "update.json")
	journal, err := LoadUpdateJournal(path)
	if err != nil {
		return false, err
	}
	matched := transactionMatchesUpdate(transaction, journal)
	if matched && transaction.Phase == viewerinstall.PhaseCommitted {
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
	if journal.State != "installer_launched" {
		return false, nil
	}
	if transaction.TransactionID != "" && !matched {
		return false, nil
	}
	if matched && incompleteTransactionPhase(transaction.Phase) {
		return false, nil
	}
	if matched && transaction.Phase == viewerinstall.PhaseRolledBack {
		for _, failed := range transaction.Quarantined {
			if failed.Version == transaction.Release.Version && failed.Digest == transaction.Release.Digest && failed.Generation == transaction.Generation {
				journal.State = "rejected"
				journal.LastError = failed.Reason
				journal.Quarantine(failed.Version, failed.Digest, failed.Generation, failed.At, failed.Reason)
				return false, save(path, journal)
			}
		}
	}
	journal.State = "launching_installer"
	journal.LastError = ""
	return false, save(path, journal)
}

func transactionMatchesUpdate(transaction viewerinstall.Journal, journal UpdateJournal) bool {
	return journal.TransactionID != "" && journal.TransactionID == transaction.TransactionID &&
		journal.Generation == transaction.Generation && journal.TargetVersion == transaction.Release.Version &&
		strings.EqualFold(journal.ArtifactSHA256, transaction.Release.Digest)
}

func incompleteTransactionPhase(phase viewerinstall.Phase) bool {
	return phase != "" && phase != viewerinstall.PhaseCommitted && phase != viewerinstall.PhaseRolledBack
}
