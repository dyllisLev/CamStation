package vieweragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultHeartbeatInterval = 10 * time.Second

var (
	ErrCommandRejected       = errors.New("viewer command rejected")
	ErrAgentRestartRequested = errors.New("Agent restart requested")
)

type Executor interface {
	Execute(context.Context, Command, string) error
}

type CommandReporter interface {
	Report(context.Context, Command, CommandState, string, string) error
}

type Agent struct {
	Config                   Config
	Paths                    MachinePaths
	Executor                 Executor
	Reporter                 CommandReporter
	HTTPClient               *http.Client
	HeartbeatInterval        time.Duration
	HeartbeatRequestDeadline time.Duration
	ControlReadDeadline      time.Duration
	AgentVersion             string
	Now                      func() time.Time

	stateMu sync.Mutex
}

func NewAgent(config Config, paths MachinePaths) Agent {
	return Agent{
		Config:                   config,
		Paths:                    paths,
		HeartbeatInterval:        defaultHeartbeatInterval,
		HeartbeatRequestDeadline: DefaultHeartbeatRequestDeadline,
		ControlReadDeadline:      DefaultControlReadDeadline,
		Now:                      time.Now,
	}
}

func (agent *Agent) HandleCommand(ctx context.Context, command Command) (CommandRecord, error) {
	if err := validateCommand(command); err != nil {
		return CommandRecord{}, err
	}
	if !supportedCommand(command.Type) {
		return agent.rejectCommand(ctx, command, "unsupported command")
	}
	if command.TTLSeconds <= 0 {
		command.TTLSeconds = 300
	}
	now := agent.now().UTC()
	ledger, err := LoadCommandLedger(agent.Paths.Commands)
	if err != nil {
		return CommandRecord{}, err
	}
	if current, exists := ledger.Records[command.Key()]; exists {
		if current.PayloadHash != command.PayloadHash {
			return current, fmt.Errorf("command payload changed: %w", ErrCommandRejected)
		}
		if current.State.terminal() {
			agent.report(ctx, command, current.State, current.OperationKey, current.Error)
			return current, nil
		}
		if current.State == CommandRunning {
			reconciled, reconcileErr := agent.reconcileRecord(command.Key(), current, ledger)
			if reconcileErr == nil {
				agent.report(ctx, command, reconciled.State, reconciled.OperationKey, reconciled.Error)
			}
			return reconciled, reconcileErr
		}
	}

	record := CommandRecord{
		ID: command.ID, Type: command.Type, PayloadHash: command.PayloadHash,
		DesiredVersion: command.DesiredVersion, ArtifactSHA256: strings.ToLower(command.ArtifactSHA256),
		Generation: command.Generation, State: CommandReceived, ReceivedAt: now,
	}
	ledger.Records[command.Key()] = record
	if err := SaveCommandLedger(agent.Paths.Commands, ledger); err != nil {
		return CommandRecord{}, err
	}

	if !command.CreatedAt.IsZero() && !now.Before(command.CreatedAt.Add(time.Duration(command.TTLSeconds)*time.Second)) {
		return agent.finishCommand(ctx, command, ledger, record, CommandExpired, "command expired")
	}
	if command.Type == "update_app" {
		journal, loadErr := LoadUpdateJournal(agent.Paths.Update)
		if loadErr != nil {
			return record, loadErr
		}
		if journal.IsQuarantined(command.DesiredVersion, command.ArtifactSHA256, command.Generation) {
			return agent.finishCommand(ctx, command, ledger, record, CommandRejected, "target quarantined")
		}
	}
	agent.report(ctx, command, CommandReceived, "", "")

	record.OperationKey = "command-" + command.Key()
	switch command.Type {
	case "restart_viewer":
		var generation int64
		allowed, stateErr := agent.updateState(func(state *MachineState) error {
			if state.ForcedViewerRestartID == command.Key() && state.ExpectedViewerGeneration > state.ViewerGeneration {
				generation = state.ExpectedViewerGeneration
				return nil
			}
			var ok bool
			ok, generation = state.AllowViewerRestart(now, true, command.Key())
			if !ok {
				return ErrCommandRejected
			}
			return nil
		})
		if stateErr != nil || !allowed {
			return agent.finishCommand(ctx, command, ledger, record, CommandRejected, "restart budget exhausted")
		}
		record.Generation = generation
		record.OperationKey = fmt.Sprintf("viewer-generation-%d", generation)
	case "restart_agent":
		state, loadErr := agent.loadState()
		if loadErr != nil {
			return record, loadErr
		}
		record.Generation = state.AgentBootGeneration + 1
		record.OperationKey = fmt.Sprintf("agent-generation-%d", record.Generation)
	case "update_app":
		record.OperationKey = fmt.Sprintf("update-%s-%s-%d", command.DesiredVersion, strings.ToLower(command.ArtifactSHA256), command.Generation)
	}
	record.State = CommandRunning
	record.RunningAt = &now
	ledger.Records[command.Key()] = record
	if err := SaveCommandLedger(agent.Paths.Commands, ledger); err != nil {
		return CommandRecord{}, err
	}
	agent.report(ctx, command, CommandRunning, record.OperationKey, "")

	executor := agent.Executor
	if executor == nil {
		executor = builtinExecutor{}
	}
	executionErr := executor.Execute(ctx, command, record.OperationKey)
	if errors.Is(executionErr, ErrAgentRestartRequested) {
		return record, ErrAgentRestartRequested
	}
	if executionErr != nil {
		return agent.finishCommand(ctx, command, ledger, record, CommandFailed, commandErrorCategory(executionErr))
	}
	if command.Type == "restart_viewer" {
		_, _ = agent.updateState(func(state *MachineState) error {
			if record.Generation > state.ViewerGeneration {
				state.ViewerGeneration = record.Generation
			}
			state.ViewerState = "running"
			return nil
		})
	}
	return agent.finishCommand(ctx, command, ledger, record, CommandSucceeded, "")
}

func (agent *Agent) Reconcile(ctx context.Context) ([]CommandRecord, error) {
	ledger, err := LoadCommandLedger(agent.Paths.Commands)
	if err != nil {
		return nil, err
	}
	results := make([]CommandRecord, 0)
	for key, record := range ledger.Records {
		if record.State != CommandRunning {
			continue
		}
		reconciled, reconcileErr := agent.reconcileRecord(key, record, ledger)
		if reconcileErr != nil {
			return results, reconcileErr
		}
		if reconciled.State == CommandRunning && reconciled.Type == "restart_viewer" {
			reconciled, reconcileErr = agent.resumeViewerRestart(ctx, key, reconciled, ledger)
			if reconcileErr != nil {
				return results, reconcileErr
			}
		}
		if reconciled.State != CommandRunning {
			results = append(results, reconciled)
			agent.report(ctx, Command{ID: record.ID, Type: record.Type, PayloadHash: record.PayloadHash}, reconciled.State, reconciled.OperationKey, reconciled.Error)
		}
	}
	return results, nil
}

func (agent *Agent) resumeViewerRestart(ctx context.Context, key string, record CommandRecord, ledger CommandLedger) (CommandRecord, error) {
	executor := agent.Executor
	if executor == nil {
		executor = builtinExecutor{}
	}
	command := Command{ID: record.ID, Type: record.Type, PayloadHash: record.PayloadHash, Generation: record.Generation}
	executionErr := executor.Execute(ctx, command, record.OperationKey)
	now := agent.now().UTC()
	if executionErr != nil {
		record.State = CommandFailed
		record.Error = commandErrorCategory(executionErr)
	} else {
		record.State = CommandSucceeded
		_, _ = agent.updateState(func(state *MachineState) error {
			if record.Generation > state.ViewerGeneration {
				state.ViewerGeneration = record.Generation
			}
			state.ViewerState = "running"
			return nil
		})
	}
	record.CompletedAt = &now
	ledger.Records[key] = record
	if err := SaveCommandLedger(agent.Paths.Commands, ledger); err != nil {
		return record, err
	}
	return record, nil
}

func (agent *Agent) Run(ctx context.Context) error {
	if agent.Config.ClientID == "" {
		return errors.New("Agent config is required")
	}
	if agent.Paths.State == "" || agent.Paths.Commands == "" || agent.Paths.Update == "" {
		return errors.New("Agent machine paths are required")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := ControlClient{
		HTTPClient: agent.HTTPClient, ServerURL: agent.Config.ServerURL, ClientID: agent.Config.ClientID,
		ReadDeadline: agent.ControlReadDeadline,
	}
	if agent.Reporter == nil {
		agent.Reporter = client
	}
	if _, err := agent.updateState(func(state *MachineState) error {
		state.ClientID = agent.Config.ClientID
		state.AgentBootGeneration++
		if state.ControlState == "" {
			state.ControlState = "control_degraded"
		}
		if state.ViewerState == "" {
			state.ViewerState = "not_logged_in"
		}
		return nil
	}); err != nil {
		return err
	}
	_, _ = agent.Reconcile(runCtx)

	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		agent.runHeartbeats(runCtx, client)
	}()
	pipeDone := make(chan struct{})
	go func() {
		defer close(pipeDone)
		if err := ServeViewerPipe(runCtx, agent.Config, agent.handlePipeMessage); err != nil && runCtx.Err() == nil {
			_ = agent.markPipeFailure()
		}
	}()
	shutdown := func() {
		cancel()
		<-heartbeatDone
		<-pipeDone
	}

	reconnect := ReconnectState{}
	for {
		result, err := client.Next(runCtx)
		if err != nil {
			if runCtx.Err() != nil {
				shutdown()
				return nil
			}
			_, _ = agent.updateState(func(state *MachineState) error {
				state.ControlState = "control_degraded"
				return nil
			})
			if err := waitContext(runCtx, reconnect.NextDelay()); err != nil {
				shutdown()
				return nil
			}
			continue
		}
		reconnect.Reset()
		now := agent.now().UTC()
		_, _ = agent.updateState(func(state *MachineState) error {
			if result.Transport == ControlTransportSSE {
				state.ControlState = "online"
			} else {
				state.ControlState = "control_degraded"
			}
			state.LastControlSuccessAt = &now
			return nil
		})
		if result.Command != nil {
			_, handleErr := agent.HandleCommand(runCtx, *result.Command)
			if errors.Is(handleErr, ErrAgentRestartRequested) {
				shutdown()
				return ErrAgentRestartRequested
			}
		}
	}
}

func (agent *Agent) runHeartbeats(ctx context.Context, client ControlClient) {
	interval := agent.HeartbeatInterval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		agent.sendHeartbeat(ctx, client)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (agent *Agent) sendHeartbeat(ctx context.Context, client ControlClient) {
	state, err := agent.loadState()
	if err != nil {
		return
	}
	hostname, _ := os.Hostname()
	heartbeat := HeartbeatPayload{
		ID: agent.Config.ClientID, DisplayName: agent.Config.DisplayName, AppVersion: agent.AgentVersion,
		Hostname: hostname, Route: "/live?viewer=1", Mode: "live",
	}
	heartbeat.Agent.State = "online"
	heartbeat.Agent.Version = agent.AgentVersion
	heartbeat.Control.State = state.ControlState
	heartbeat.Control.LastSuccessAt = state.LastControlSuccessAt
	heartbeat.Viewer.State = state.ViewerState
	if heartbeat.Viewer.State == "" {
		heartbeat.Viewer.State = "not_logged_in"
	}
	heartbeat.Viewer.LastHeartbeatAt = state.ViewerLastHeartbeatAt
	heartbeat.Renderer.State = state.RendererState
	if heartbeat.Renderer.State == "" {
		heartbeat.Renderer.State = "not_ready"
	}
	heartbeat.Renderer.LastHeartbeatAt = state.RendererLastHeartbeatAt
	journal, _ := LoadUpdateJournal(agent.Paths.Update)
	heartbeat.Update.State = journal.State
	heartbeat.Update.TargetVersion = journal.TargetVersion
	heartbeat.Update.Generation = journal.Generation
	deadline := agent.HeartbeatRequestDeadline
	if deadline <= 0 {
		deadline = DefaultHeartbeatRequestDeadline
	}
	heartbeatCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if err := client.SendHeartbeat(heartbeatCtx, heartbeat); err == nil {
		now := agent.now().UTC()
		_, _ = agent.updateState(func(state *MachineState) error {
			state.LastHeartbeatAt = &now
			return nil
		})
	}
}

func (agent *Agent) rejectCommand(ctx context.Context, command Command, message string) (CommandRecord, error) {
	ledger, err := LoadCommandLedger(agent.Paths.Commands)
	if err != nil {
		return CommandRecord{}, err
	}
	now := agent.now().UTC()
	record := CommandRecord{ID: command.ID, Type: command.Type, PayloadHash: command.PayloadHash, State: CommandRejected, Error: message, ReceivedAt: now, CompletedAt: &now}
	ledger.Records[command.Key()] = record
	if err := SaveCommandLedger(agent.Paths.Commands, ledger); err != nil {
		return CommandRecord{}, err
	}
	agent.report(ctx, command, CommandRejected, "", message)
	return record, fmt.Errorf("%s: %w", message, ErrCommandRejected)
}

func (agent *Agent) finishCommand(ctx context.Context, command Command, ledger CommandLedger, record CommandRecord, state CommandState, message string) (CommandRecord, error) {
	now := agent.now().UTC()
	record.State = state
	record.Error = message
	record.CompletedAt = &now
	ledger.Records[command.Key()] = record
	if err := SaveCommandLedger(agent.Paths.Commands, ledger); err != nil {
		return CommandRecord{}, err
	}
	agent.report(ctx, command, state, record.OperationKey, message)
	if state == CommandFailed || state == CommandRejected || state == CommandExpired {
		return record, fmt.Errorf("%s: %w", message, ErrCommandRejected)
	}
	return record, nil
}

func (agent *Agent) reconcileRecord(key string, record CommandRecord, ledger CommandLedger) (CommandRecord, error) {
	state, err := agent.loadState()
	if err != nil {
		return record, err
	}
	reached := (record.Type == "restart_viewer" && state.ViewerGeneration >= record.Generation) ||
		(record.Type == "restart_agent" && state.AgentBootGeneration >= record.Generation)
	if record.Type == "update_app" {
		journal, loadErr := LoadUpdateJournal(agent.Paths.Update)
		if loadErr != nil {
			return record, loadErr
		}
		reached = journal.State == "committed" && journal.TargetVersion == record.DesiredVersion &&
			strings.EqualFold(journal.ArtifactSHA256, record.ArtifactSHA256) && journal.Generation == record.Generation
	}
	if !reached {
		return record, nil
	}
	now := agent.now().UTC()
	record.State = CommandSucceeded
	record.CompletedAt = &now
	ledger.Records[key] = record
	if err := SaveCommandLedger(agent.Paths.Commands, ledger); err != nil {
		return record, err
	}
	return record, nil
}

func (agent *Agent) loadState() (MachineState, error) {
	agent.stateMu.Lock()
	defer agent.stateMu.Unlock()
	return LoadMachineState(agent.Paths.State)
}

func (agent *Agent) updateState(change func(*MachineState) error) (bool, error) {
	agent.stateMu.Lock()
	defer agent.stateMu.Unlock()
	state, err := LoadMachineState(agent.Paths.State)
	if err != nil {
		return false, err
	}
	if err := change(&state); err != nil {
		return false, err
	}
	return true, SaveMachineState(agent.Paths.State, state)
}

func (agent *Agent) report(ctx context.Context, command Command, state CommandState, operationKey, commandError string) {
	if agent.Reporter != nil {
		_ = agent.Reporter.Report(ctx, command, state, operationKey, commandError)
	}
}

func (agent *Agent) now() time.Time {
	if agent.Now != nil {
		return agent.Now()
	}
	return time.Now()
}

func supportedCommand(commandType string) bool {
	switch commandType {
	case "ping", "reload_live", "restart_viewer", "restart_agent", "resubscribe_stream", "restart_stream", "update_app", "capture_diagnostics":
		return true
	default:
		return false
	}
}

func commandErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrCommandRejected) {
		return "rejected"
	}
	return "execution_failed"
}

func (agent *Agent) handlePipeMessage(message PipeMessage) (PipeMessage, error) {
	now := agent.now().UTC()
	response := PipeMessage{Version: PipeProtocolVersion, RequestID: message.RequestID}
	switch message.Type {
	case "bootstrap_request":
		if message.PID <= 0 {
			return PipeMessage{}, errors.New("bootstrap PID is required")
		}
		nonce, err := newClientID()
		if err != nil {
			return PipeMessage{}, err
		}
		_, err = agent.updateState(func(state *MachineState) error {
			generation := state.ExpectedViewerGeneration
			if generation <= state.ViewerGeneration {
				generation = state.ViewerGeneration + 1
			}
			state.ExpectedViewerGeneration = generation
			state.ViewerNonce = nonce
			state.ExpectedViewerPID = message.PID
			state.ExpectedViewerSession = message.SessionID
			state.ViewerState = "restarting"
			response.Type = "bootstrap_grant"
			response.Generation = generation
			response.Nonce = nonce
			return nil
		})
		return response, err
	case "viewer_heartbeat", "renderer_status":
		_, err := agent.updateState(func(state *MachineState) error {
			if message.PID != state.ExpectedViewerPID || message.SessionID != state.ExpectedViewerSession ||
				message.Generation != state.ExpectedViewerGeneration || message.Nonce == "" || message.Nonce != state.ViewerNonce {
				return errors.New("stale Viewer pipe identity")
			}
			state.ViewerGeneration = message.Generation
			state.ViewerState = "running"
			state.ViewerLastHeartbeatAt = &now
			if message.Type == "renderer_status" {
				var payload struct {
					State string `json:"state"`
				}
				if err := json.Unmarshal(message.Payload, &payload); err != nil || strings.TrimSpace(payload.State) == "" {
					return errors.New("renderer state is required")
				}
				state.RendererState = payload.State
				state.RendererLastHeartbeatAt = &now
			}
			return nil
		})
		response.Type = "ack"
		return response, err
	default:
		return PipeMessage{}, errors.New("unsupported pipe message type")
	}
}

func (agent *Agent) markPipeFailure() error {
	_, err := agent.updateState(func(state *MachineState) error {
		state.ViewerState = "failed"
		state.RendererState = "not_ready"
		return nil
	})
	return err
}

type builtinExecutor struct{}

func (builtinExecutor) Execute(_ context.Context, command Command, _ string) error {
	if command.Type == "ping" {
		return nil
	}
	if command.Type == "restart_agent" {
		return ErrAgentRestartRequested
	}
	return errors.New("command adapter is not installed")
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
