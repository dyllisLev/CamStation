package vieweragent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateFilesReplaceAtomicallyAndRejectOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := SaveMachineState(path, MachineState{ClientID: "client-one", ControlState: "control_degraded"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveMachineState(path, MachineState{ClientID: "c", ControlState: "online"}); err != nil {
		t.Fatal(err)
	}
	state, err := LoadMachineState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.ClientID != "c" || state.ControlState != "online" {
		t.Fatalf("unexpected replacement: %+v", state)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".state.json.*")); len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}

	if err := os.WriteFile(path, make([]byte, MaxStateFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMachineState(path); err == nil {
		t.Fatal("oversized state file was accepted")
	}
}

func TestOpenStateFileWithRetryRecoversTransientLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	transient := errors.New("sharing violation")
	attempts := 0
	sleeps := 0
	file, err := openStateFileWithRetry(
		func() (*os.File, error) {
			attempts++
			if attempts < 3 {
				return nil, fmt.Errorf("open attempt %d: %w", attempts, transient)
			}
			return os.Open(path)
		},
		func(err error) bool { return errors.Is(err, transient) },
		func(delay time.Duration) {
			if delay != stateFileOpenRetryDelay {
				t.Fatalf("retry delay=%v want=%v", delay, stateFileOpenRetryDelay)
			}
			sleeps++
		},
	)
	if err != nil || attempts != 3 || sleeps != 2 {
		t.Fatalf("file=%v attempts=%d sleeps=%d err=%v", file, attempts, sleeps, err)
	}
	contents, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(contents) != `{"schemaVersion":1}` {
		t.Fatalf("contents=%q readErr=%v closeErr=%v", contents, readErr, closeErr)
	}
}

func TestOpenStateFileWithRetryReturnsNonretryableErrorImmediately(t *testing.T) {
	denied := errors.New("access denied")
	attempts := 0
	sleeps := 0
	file, err := openStateFileWithRetry(
		func() (*os.File, error) {
			attempts++
			return nil, fmt.Errorf("open state: %w", denied)
		},
		func(error) bool { return false },
		func(time.Duration) { sleeps++ },
	)
	if file != nil || !errors.Is(err, denied) || attempts != 1 || sleeps != 0 {
		t.Fatalf("file=%v attempts=%d sleeps=%d err=%v", file, attempts, sleeps, err)
	}
}

func TestOpenStateFileWithRetryExhaustionIsBoundedAndWrapsLastCause(t *testing.T) {
	attempts := 0
	totalSleep := time.Duration(0)
	var lastErr error
	file, err := openStateFileWithRetry(
		func() (*os.File, error) {
			attempts++
			lastErr = fmt.Errorf("sharing violation attempt %d", attempts)
			return nil, lastErr
		},
		func(error) bool { return true },
		func(delay time.Duration) { totalSleep += delay },
	)
	wantSleep := time.Duration(stateFileOpenAttempts-1) * stateFileOpenRetryDelay
	if file != nil || attempts != stateFileOpenAttempts || totalSleep != wantSleep || totalSleep > 500*time.Millisecond || !errors.Is(err, lastErr) || !strings.Contains(err.Error(), fmt.Sprintf("after %d attempts", stateFileOpenAttempts)) {
		t.Fatalf("file=%v attempts=%d totalSleep=%v lastErr=%v err=%v", file, attempts, totalSleep, lastErr, err)
	}
}

func TestUpdateQuarantineRequiresNewDigestOrGeneration(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	journal := UpdateJournal{}
	journal.Quarantine("2.0.0", "abc", 4, now, "validation_failed")
	if !journal.IsQuarantined("2.0.0", "abc", 4) {
		t.Fatal("exact failed target was not quarantined")
	}
	if journal.IsQuarantined("2.0.0", "def", 4) || journal.IsQuarantined("2.0.0", "abc", 5) {
		t.Fatal("new digest or generation did not rearm update")
	}
}

func TestViewerRestartBudgetIsBoundedAndPersistent(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	state := MachineState{ViewerRestartHistory: []time.Time{now.Add(-40 * time.Minute), now.Add(-20 * time.Minute)}}
	allowed, generation := state.AllowViewerRestart(now, false, "")
	if !allowed || generation != 1 {
		t.Fatalf("first restart rejected: allowed=%v generation=%d", allowed, generation)
	}
	if allowed, _ := state.AllowViewerRestart(now.Add(5*time.Minute), false, ""); allowed {
		t.Fatal("ten-minute restart spacing was ignored")
	}
	if allowed, _ := state.AllowViewerRestart(now.Add(11*time.Minute), false, ""); allowed {
		t.Fatal("three-per-hour restart budget was ignored")
	}
	if allowed, _ := state.AllowViewerRestart(now.Add(11*time.Minute), true, "command-9"); !allowed {
		t.Fatal("one explicit forced restart should be allowed")
	}
	if allowed, _ := state.AllowViewerRestart(now.Add(12*time.Minute), true, "command-9"); allowed {
		t.Fatal("forced restart command was applied twice")
	}
	if allowed, next := state.AllowViewerRestart(now.Add(12*time.Minute), true, "command-10"); !allowed || next <= generation {
		t.Fatalf("a distinct forced restart was rejected: allowed=%v generation=%d", allowed, next)
	}
}
