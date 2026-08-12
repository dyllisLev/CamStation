package viewerservice

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CommandErrorExpired            = "command_expired"
	CommandErrorPayloadChanged     = "command_payload_changed"
	CommandErrorInterrupted        = "command_interrupted"
	CommandErrorUnsupported        = "unsupported_command"
	CommandErrorExecutionFailed    = "execution_failed"
	CommandErrorJournalUnavailable = "command_journal_unavailable"
	MaxRetainedCommandRecords      = 256
)

var ErrServiceRestartRequested = errors.New("service restart requested")

var errCommandReporterUnavailable = errors.New("command reporter unavailable")

type CommandExecutor interface {
	ExecuteCommand(context.Context, Command, string) error
}

type CommandEngine struct {
	Store    CommandJournalStore
	Executor CommandExecutor
	Reporter func() CommandReporter
	Now      func() time.Time

	mu             sync.Mutex
	bootGeneration int64
}

type commandRejection struct{ code string }

type commandFailure struct{ code string }

func (rejection commandRejection) Error() string { return rejection.code }

func (failure commandFailure) Error() string { return failure.code }

func RejectCommand(code string) error {
	return commandRejection{code: code}
}

func FailCommand(code string) error {
	return commandFailure{code: code}
}

func (engine *CommandEngine) Start() error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.startLocked()
}

func (engine *CommandEngine) BootGeneration() int64 {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.bootGeneration
}

func (engine *CommandEngine) Handle(ctx context.Context, command Command) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.bootGeneration == 0 {
		if err := engine.startLocked(); err != nil {
			engine.report(ctx, command, CommandResult{State: "failed", Error: CommandErrorJournalUnavailable})
			return err
		}
	}
	if command.Type == "restart_agent" {
		command.Type = "restart_service"
	}
	if !supportedRuntimeCommand(command.Type) {
		engine.report(ctx, command, CommandResult{State: "rejected", Error: CommandErrorUnsupported})
		return RejectCommand(CommandErrorUnsupported)
	}
	if command.TTLSeconds <= 0 {
		command.TTLSeconds = 300
	}
	journal, err := engine.Store.Load()
	if err != nil {
		engine.report(ctx, command, CommandResult{State: "failed", Error: CommandErrorJournalUnavailable})
		return err
	}
	key := command.Key()
	now := engine.now().UTC()
	if existing, found := journal.Records[key]; found {
		if existing.PayloadHash != command.PayloadHash {
			engine.report(ctx, command, CommandResult{State: "rejected", Error: CommandErrorPayloadChanged, OperationKey: existing.OperationKey})
			return RejectCommand(CommandErrorPayloadChanged)
		}
		if terminalLocalCommandState(existing.State) {
			engine.reportTerminal(ctx, command, &journal, existing)
			return nil
		}
		if existing.Type == "restart_service" && existing.State == "running" &&
			existing.TargetBootGeneration > 0 && engine.bootGeneration >= existing.TargetBootGeneration {
			return engine.finish(ctx, command, journal, existing, "succeeded", "")
		}
		if existing.AcceptedBootGeneration == engine.bootGeneration {
			engine.report(ctx, command, resultFromLocalRecord(existing))
			return nil
		}
		return engine.finish(ctx, command, journal, existing, "failed", CommandErrorInterrupted)
	}
	createdAt := command.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	if !now.Before(createdAt.Add(time.Duration(command.TTLSeconds) * time.Second)) {
		record := LocalCommandRecord{
			ID: command.ID, Type: command.Type, PayloadHash: command.PayloadHash, OperationKey: "command-" + key,
			State: "expired", Error: CommandErrorExpired, CreatedAt: createdAt, TTLSeconds: command.TTLSeconds,
			AcceptedAt: now, CompletedAt: &now,
		}
		journal.Records[key] = record
		pruneCommandJournal(&journal)
		if err := engine.Store.Save(journal); err != nil {
			return err
		}
		engine.reportTerminal(ctx, command, &journal, record)
		return RejectCommand(CommandErrorExpired)
	}

	operationKey := "command-" + key
	record := LocalCommandRecord{
		ID: command.ID, Type: command.Type, PayloadHash: command.PayloadHash, OperationKey: operationKey,
		State: "acknowledged", CreatedAt: createdAt, TTLSeconds: command.TTLSeconds, AcceptedAt: now,
		AcceptedBootGeneration: engine.bootGeneration,
	}
	if command.Type == "restart_service" {
		record.TargetBootGeneration = engine.bootGeneration + 1
		record.OperationKey = "service-generation-" + strconv.FormatInt(record.TargetBootGeneration, 10)
	}
	journal.Records[key] = record
	pruneCommandJournal(&journal)
	if err := engine.Store.Save(journal); err != nil {
		engine.report(ctx, command, CommandResult{State: "failed", Error: CommandErrorJournalUnavailable})
		return err
	}
	engine.report(ctx, command, resultFromLocalRecord(record))

	runningAt := engine.now().UTC()
	record.State = "running"
	record.RunningAt = &runningAt
	journal.Records[key] = record
	if err := engine.Store.Save(journal); err != nil {
		engine.report(ctx, command, CommandResult{State: "failed", Error: CommandErrorJournalUnavailable, OperationKey: record.OperationKey})
		return err
	}
	engine.report(ctx, command, resultFromLocalRecord(record))

	if engine.Executor == nil {
		return engine.finish(ctx, command, journal, record, "failed", CommandErrorExecutionFailed)
	}
	err = engine.Executor.ExecuteCommand(ctx, command, record.OperationKey)
	if errors.Is(err, ErrServiceRestartRequested) {
		return err
	}
	if err != nil {
		var rejection commandRejection
		if errors.As(err, &rejection) {
			return engine.finish(ctx, command, journal, record, "rejected", rejection.code)
		}
		var failure commandFailure
		if errors.As(err, &failure) {
			return engine.finish(ctx, command, journal, record, "failed", failure.code)
		}
		return engine.finish(ctx, command, journal, record, "failed", CommandErrorExecutionFailed)
	}
	return engine.finish(ctx, command, journal, record, "succeeded", "")
}

// Reconcile completes durable work that crossed a Service process boundary
// and retries terminal reports that were not acknowledged by the server. It
// does not repeat a side effect.
func (engine *CommandEngine) Reconcile(ctx context.Context) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.bootGeneration == 0 {
		if err := engine.startLocked(); err != nil {
			return err
		}
	}
	journal, err := engine.Store.Load()
	if err != nil {
		return err
	}
	for key, record := range journal.Records {
		if !terminalLocalCommandState(record.State) && record.AcceptedBootGeneration < engine.bootGeneration {
			now := engine.now().UTC()
			if record.Type == "restart_service" && record.State == "running" &&
				record.TargetBootGeneration > 0 && engine.bootGeneration >= record.TargetBootGeneration {
				record.State = "succeeded"
				record.Error = ""
			} else {
				record.State = "failed"
				record.Error = CommandErrorInterrupted
			}
			record.CompletedAt = &now
			record.ResultReportedAt = nil
			journal.Records[key] = record
			if err := engine.Store.Save(journal); err != nil {
				return err
			}
		}
		if !terminalLocalCommandState(record.State) || record.ResultReportedAt != nil {
			continue
		}
		command := Command{
			ID: record.ID, Type: record.Type, PayloadHash: record.PayloadHash,
			TTLSeconds: record.TTLSeconds, CreatedAt: record.CreatedAt,
		}
		engine.reportTerminal(ctx, command, &journal, record)
	}
	return nil
}

func (engine *CommandEngine) startLocked() error {
	if engine.Store == nil {
		engine.Store = &MemoryCommandJournalStore{}
	}
	journal, err := engine.Store.Load()
	if err != nil {
		return err
	}
	journal.BootGeneration++
	if journal.BootGeneration <= 0 {
		return errors.New("invalid command journal boot generation")
	}
	pruneCommandJournal(&journal)
	if err := engine.Store.Save(journal); err != nil {
		return err
	}
	engine.bootGeneration = journal.BootGeneration
	return nil
}

func (engine *CommandEngine) finish(ctx context.Context, command Command, journal CommandJournal, record LocalCommandRecord, state, code string) error {
	now := engine.now().UTC()
	record.State = state
	record.Error = code
	record.CompletedAt = &now
	record.ResultReportedAt = nil
	journal.Records[command.Key()] = record
	pruneCommandJournal(&journal)
	if err := engine.Store.Save(journal); err != nil {
		return err
	}
	engine.reportTerminal(ctx, command, &journal, record)
	if state == "failed" || state == "rejected" || state == "expired" {
		return fmt.Errorf("%s", code)
	}
	return nil
}

func (engine *CommandEngine) reportTerminal(ctx context.Context, command Command, journal *CommandJournal, record LocalCommandRecord) {
	if record.ResultReportedAt != nil || !terminalLocalCommandState(record.State) {
		return
	}
	if err := engine.report(ctx, command, resultFromLocalRecord(record)); err != nil {
		return
	}
	now := engine.now().UTC()
	record.ResultReportedAt = &now
	journal.Records[command.Key()] = record
	_ = engine.Store.Save(*journal)
}

func (engine *CommandEngine) report(ctx context.Context, command Command, result CommandResult) error {
	if engine.Reporter == nil {
		return errCommandReporterUnavailable
	}
	reporter := engine.Reporter()
	if reporter == nil {
		return errCommandReporterUnavailable
	}
	reportCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return reporter.Report(reportCtx, command, result)
}

func (engine *CommandEngine) now() time.Time {
	if engine.Now != nil {
		return engine.Now()
	}
	return time.Now()
}

func supportedRuntimeCommand(commandType string) bool {
	switch strings.TrimSpace(commandType) {
	case "ping", "reload_live", "resubscribe_stream", "restart_viewer", "restart_service":
		return true
	default:
		return false
	}
}

func terminalLocalCommandState(state string) bool {
	switch state {
	case "succeeded", "failed", "rejected", "expired":
		return true
	default:
		return false
	}
}

func resultFromLocalRecord(record LocalCommandRecord) CommandResult {
	return CommandResult{State: record.State, Error: record.Error, OperationKey: record.OperationKey}
}

func pruneCommandJournal(journal *CommandJournal) {
	if journal == nil || len(journal.Records) <= MaxRetainedCommandRecords {
		return
	}
	for len(journal.Records) > MaxRetainedCommandRecords {
		var oldestKey string
		var oldest time.Time
		for key, record := range journal.Records {
			if !terminalLocalCommandState(record.State) || record.CompletedAt == nil {
				continue
			}
			if oldestKey == "" || record.CompletedAt.Before(oldest) {
				oldestKey = key
				oldest = *record.CompletedAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(journal.Records, oldestKey)
	}
}
