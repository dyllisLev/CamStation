package viewerservice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingCommandReporter struct {
	mu      sync.Mutex
	results []CommandResult
	err     error
}

func (reporter *recordingCommandReporter) Report(_ context.Context, _ Command, result CommandResult) error {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	reporter.results = append(reporter.results, result)
	return reporter.err
}

func (reporter *recordingCommandReporter) states() []string {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	states := make([]string, 0, len(reporter.results))
	for _, result := range reporter.results {
		states = append(states, result.State)
	}
	return states
}

type commandExecutorFunc func(context.Context, Command, string) error

func (execute commandExecutorFunc) ExecuteCommand(ctx context.Context, command Command, operationKey string) error {
	return execute(ctx, command, operationKey)
}

func TestCommandEnginePersistsBeforeExecutionAndSuppressesDuplicate(t *testing.T) {
	journal := &MemoryCommandJournalStore{}
	reporter := &recordingCommandReporter{}
	executions := 0
	engine := &CommandEngine{
		Store:    journal,
		Reporter: func() CommandReporter { return reporter },
		Executor: commandExecutorFunc(func(_ context.Context, command Command, operationKey string) error {
			executions++
			persisted, err := journal.Load()
			if err != nil {
				t.Fatal(err)
			}
			record := persisted.Records[command.Key()]
			if record.State != "running" || record.OperationKey != operationKey || record.AcceptedBootGeneration != 1 {
				t.Fatalf("side effect preceded durable intent: %#v", record)
			}
			return nil
		}),
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	command := Command{ID: 41, Type: "ping", PayloadHash: strings.Repeat("a", 64), TTLSeconds: 300, CreatedAt: time.Now().UTC()}
	if err := engine.Handle(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if err := engine.Handle(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	if executions != 1 {
		t.Fatalf("executions=%d want=1", executions)
	}
	states := reporter.states()
	want := []string{"acknowledged", "running", "succeeded"}
	if strings.Join(states, ",") != strings.Join(want, ",") {
		t.Fatalf("states=%v want=%v", states, want)
	}
}

func TestCommandEngineRejectsChangedPayloadAndExpiredCommand(t *testing.T) {
	journal := &MemoryCommandJournalStore{}
	reporter := &recordingCommandReporter{}
	executions := 0
	now := time.Now().UTC()
	engine := &CommandEngine{
		Store: journal, Now: func() time.Time { return now },
		Reporter: func() CommandReporter { return reporter },
		Executor: commandExecutorFunc(func(context.Context, Command, string) error { executions++; return nil }),
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	command := Command{ID: 42, Type: "ping", PayloadHash: "hash-a", TTLSeconds: 300, CreatedAt: now}
	if err := engine.Handle(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	command.PayloadHash = "hash-b"
	if err := engine.Handle(t.Context(), command); err == nil {
		t.Fatal("changed command payload was accepted")
	}
	expired := Command{ID: 43, Type: "ping", PayloadHash: "expired", TTLSeconds: 30, CreatedAt: now.Add(-time.Minute)}
	if err := engine.Handle(t.Context(), expired); err == nil {
		t.Fatal("expired command was accepted")
	}
	unsupported := Command{ID: 44, Type: "shell", PayloadHash: "unsupported", TTLSeconds: 300, CreatedAt: now}
	if err := engine.Handle(t.Context(), unsupported); err == nil {
		t.Fatal("unsupported command was accepted")
	}
	if executions != 1 {
		t.Fatalf("executions=%d want=1", executions)
	}
}

func TestCommandEngineReconcilesServiceRestartOnNextBoot(t *testing.T) {
	journal := &MemoryCommandJournalStore{}
	reporter := &recordingCommandReporter{}
	first := &CommandEngine{
		Store:    journal,
		Reporter: func() CommandReporter { return reporter },
		Executor: commandExecutorFunc(func(context.Context, Command, string) error { return ErrServiceRestartRequested }),
	}
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	command := Command{ID: 44, Type: "restart_service", PayloadHash: "restart", TTLSeconds: 300, CreatedAt: time.Now().UTC()}
	if err := first.Handle(t.Context(), command); !errors.Is(err, ErrServiceRestartRequested) {
		t.Fatalf("first restart result=%v", err)
	}
	persisted, err := journal.Load()
	if err != nil {
		t.Fatal(err)
	}
	record := persisted.Records[command.Key()]
	if record.State != "running" || record.TargetBootGeneration != 2 || record.OperationKey != "service-generation-2" {
		t.Fatalf("restart record=%#v", record)
	}
	secondExecutions := 0
	second := &CommandEngine{
		Store:    journal,
		Reporter: func() CommandReporter { return reporter },
		Executor: commandExecutorFunc(func(context.Context, Command, string) error { secondExecutions++; return nil }),
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	if err := second.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if secondExecutions != 0 {
		t.Fatalf("restart side effect repeated %d times", secondExecutions)
	}
	persisted, err = journal.Load()
	if err != nil || persisted.Records[command.Key()].State != "succeeded" {
		t.Fatalf("reconciled record=%#v err=%v", persisted.Records[command.Key()], err)
	}
}

func TestCommandEngineRetriesUnreportedTerminalResultWithoutRepeatingExecution(t *testing.T) {
	journal := &MemoryCommandJournalStore{}
	reporter := &recordingCommandReporter{err: errors.New("server unavailable")}
	executions := 0
	engine := &CommandEngine{
		Store:    journal,
		Reporter: func() CommandReporter { return reporter },
		Executor: commandExecutorFunc(func(context.Context, Command, string) error {
			executions++
			return nil
		}),
	}
	if err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	command := Command{ID: 45, Type: "ping", PayloadHash: "retry", TTLSeconds: 300, CreatedAt: time.Now().UTC()}
	if err := engine.Handle(t.Context(), command); err != nil {
		t.Fatal(err)
	}
	persisted, err := journal.Load()
	if err != nil {
		t.Fatal(err)
	}
	if record := persisted.Records[command.Key()]; record.State != "succeeded" || record.ResultReportedAt != nil {
		t.Fatalf("unreported record=%#v", record)
	}
	reporter.mu.Lock()
	reporter.err = nil
	reporter.mu.Unlock()
	if err := engine.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	persisted, err = journal.Load()
	if err != nil {
		t.Fatal(err)
	}
	if record := persisted.Records[command.Key()]; record.ResultReportedAt == nil {
		t.Fatalf("reconciled record=%#v", record)
	}
	if executions != 1 {
		t.Fatalf("executions=%d want=1", executions)
	}
}

func TestFileCommandJournalRoundTripIsBounded(t *testing.T) {
	store := FileCommandJournalStore{Path: t.TempDir() + "/commands.json"}
	journal := emptyCommandJournal()
	journal.BootGeneration = 7
	journal.Records["9"] = LocalCommandRecord{ID: 9, Type: "ping", PayloadHash: "hash", State: "succeeded"}
	if err := store.Save(journal); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BootGeneration != 7 || loaded.Records["9"].State != "succeeded" {
		t.Fatalf("loaded=%#v", loaded)
	}
}
