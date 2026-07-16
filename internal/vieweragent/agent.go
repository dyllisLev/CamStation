package vieweragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultHeartbeatInterval = 10 * time.Second
const defaultViewerCommandDeadline = 45 * time.Second
const defaultViewerRestartDeadline = 45 * time.Second

var (
	ErrCommandRejected       = errors.New("viewer command rejected")
	ErrAgentRestartRequested = errors.New("Agent restart requested")
	ErrCommandEngine         = errors.New("command engine unavailable")
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
	ViewerCommandDeadline    time.Duration
	ViewerRestartDeadline    time.Duration
	AgentVersion             string
	Now                      func() time.Time
	LoadLedger               func(string) (CommandLedger, error)
	SaveLedger               func(string, CommandLedger) error
	Ready                    func()

	stateMu          sync.Mutex
	telemetryMu      sync.Mutex
	viewerStreams    []ViewerStreamState
	rendererProgress *time.Time
	viewerCommandMu  sync.Mutex
	viewerCommand    *viewerCommandRequest
}

type viewerCommandRequest struct {
	command      Command
	operationKey string
	result       chan error
	delivered    bool
}

func NewAgent(config Config, paths MachinePaths) Agent {
	return Agent{
		Config:                   config,
		Paths:                    paths,
		HeartbeatInterval:        defaultHeartbeatInterval,
		HeartbeatRequestDeadline: DefaultHeartbeatRequestDeadline,
		ControlReadDeadline:      DefaultControlReadDeadline,
		ViewerCommandDeadline:    defaultViewerCommandDeadline,
		ViewerRestartDeadline:    defaultViewerRestartDeadline,
		Now:                      time.Now,
	}
}

func (agent *Agent) HandleCommand(ctx context.Context, command Command) (result CommandRecord, resultErr error) {
	defer func() {
		if errors.Is(resultErr, ErrCommandEngine) {
			_ = agent.markCommandEngineFailure()
		}
	}()
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
	ledger, err := agent.loadCommandLedger()
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
		if commandRecordExpired(current, now) {
			expired, expireErr := agent.expireRecord(ctx, command.Key(), current, ledger)
			if expireErr != nil {
				return expired, expireErr
			}
			return expired, fmt.Errorf("command expired: %w", ErrCommandRejected)
		}
		if current.State == CommandRunning {
			reconciled, reconcileErr := agent.reconcileRecord(command.Key(), current, ledger)
			if reconcileErr == nil {
				agent.report(ctx, command, reconciled.State, reconciled.OperationKey, reconciled.Error)
			}
			return reconciled, reconcileErr
		}
	}

	createdAt := command.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	record := CommandRecord{
		ID: command.ID, Type: command.Type, PayloadHash: command.PayloadHash,
		DesiredVersion: command.DesiredVersion, ArtifactSHA256: strings.ToLower(command.ArtifactSHA256),
		Generation: command.Generation, State: CommandReceived, CreatedAt: createdAt,
		TTLSeconds: command.TTLSeconds, ReceivedAt: now,
	}
	ledger.Records[command.Key()] = record
	if err := agent.saveCommandLedger(ledger); err != nil {
		return CommandRecord{}, err
	}

	if commandRecordExpired(record, now) {
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
			state.ViewerState = "restart_authorized"
			return nil
		})
		if stateErr != nil || !allowed {
			return agent.finishCommand(ctx, command, ledger, record, CommandRejected, "restart budget exhausted")
		}
		record.Generation = generation
		command.Generation = generation
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
	if err := agent.saveCommandLedger(ledger); err != nil {
		return CommandRecord{}, err
	}
	agent.report(ctx, command, CommandRunning, record.OperationKey, "")

	executor := agent.Executor
	if executor == nil {
		executor = agent
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

func (agent *Agent) Reconcile(ctx context.Context) (results []CommandRecord, resultErr error) {
	defer func() {
		if errors.Is(resultErr, ErrCommandEngine) {
			_ = agent.markCommandEngineFailure()
		}
	}()
	ledger, err := agent.loadCommandLedger()
	if err != nil {
		return nil, err
	}
	results = make([]CommandRecord, 0)
	for key, record := range ledger.Records {
		if record.State.terminal() {
			continue
		}
		if commandRecordExpired(record, agent.now().UTC()) {
			expired, expireErr := agent.expireRecord(ctx, key, record, ledger)
			if expireErr != nil {
				return results, expireErr
			}
			results = append(results, expired)
			continue
		}
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
		executor = agent
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
	if err := agent.saveCommandLedger(ledger); err != nil {
		return record, err
	}
	return record, nil
}

func (agent *Agent) expireRecord(ctx context.Context, key string, record CommandRecord, ledger CommandLedger) (CommandRecord, error) {
	now := agent.now().UTC()
	record.State = CommandExpired
	record.Error = "command expired"
	record.CompletedAt = &now
	ledger.Records[key] = record
	if err := agent.saveCommandLedger(ledger); err != nil {
		return record, err
	}
	agent.report(ctx, Command{ID: record.ID, Type: record.Type, PayloadHash: record.PayloadHash, CreatedAt: record.CreatedAt, TTLSeconds: record.TTLSeconds}, CommandExpired, record.OperationKey, record.Error)
	return record, nil
}

func commandRecordExpired(record CommandRecord, now time.Time) bool {
	return record.TTLSeconds > 0 && !record.CreatedAt.IsZero() && !now.Before(record.CreatedAt.Add(time.Duration(record.TTLSeconds)*time.Second))
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
		state.ControlState = "control_degraded"
		state.CommandEngineHealthy = false
		if state.ViewerState == "" {
			state.ViewerState = "not_logged_in"
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := agent.Reconcile(runCtx); err != nil {
		return err
	}
	if err := agent.checkCommandEngine(); err != nil {
		return err
	}
	if agent.Ready != nil {
		agent.Ready()
	}

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

	controlErr := client.RunControl(runCtx, &ReconnectState{}, func(result ControlResult) error {
		err := agent.handleControlResult(runCtx, result)
		if errors.Is(err, ErrCommandEngine) {
			return nil
		}
		return err
	})
	shutdown()
	if errors.Is(controlErr, ErrAgentRestartRequested) {
		return ErrAgentRestartRequested
	}
	if ctx.Err() != nil || errors.Is(controlErr, context.Canceled) {
		return nil
	}
	return controlErr
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
	heartbeat.Streams, heartbeat.Renderer.LastProgressAt = agent.viewerTelemetry()
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
	ledger, err := agent.loadCommandLedger()
	if err != nil {
		return CommandRecord{}, err
	}
	now := agent.now().UTC()
	record := CommandRecord{ID: command.ID, Type: command.Type, PayloadHash: command.PayloadHash, State: CommandRejected, Error: message, ReceivedAt: now, CompletedAt: &now}
	ledger.Records[command.Key()] = record
	if err := agent.saveCommandLedger(ledger); err != nil {
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
	if err := agent.saveCommandLedger(ledger); err != nil {
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
	if err := agent.saveCommandLedger(ledger); err != nil {
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

func (agent *Agent) loadCommandLedger() (CommandLedger, error) {
	load := agent.LoadLedger
	if load == nil {
		load = LoadCommandLedger
	}
	ledger, err := load(agent.Paths.Commands)
	if err != nil {
		return CommandLedger{}, fmt.Errorf("%w: ledger read failed", ErrCommandEngine)
	}
	return ledger, nil
}

func (agent *Agent) saveCommandLedger(ledger CommandLedger) error {
	save := agent.SaveLedger
	if save == nil {
		save = SaveCommandLedger
	}
	if err := save(agent.Paths.Commands, ledger); err != nil {
		return fmt.Errorf("%w: ledger write failed", ErrCommandEngine)
	}
	return nil
}

func (agent *Agent) markCommandEngineFailure() error {
	_, err := agent.updateState(func(state *MachineState) error {
		state.CommandEngineHealthy = false
		state.ControlState = "control_degraded"
		return nil
	})
	return err
}

func (agent *Agent) checkCommandEngine() error {
	ledger, err := agent.loadCommandLedger()
	if err != nil {
		_ = agent.markCommandEngineFailure()
		return err
	}
	if err := agent.saveCommandLedger(ledger); err != nil {
		_ = agent.markCommandEngineFailure()
		return err
	}
	_, err = agent.updateState(func(state *MachineState) error {
		state.CommandEngineHealthy = true
		return nil
	})
	return err
}

func (agent *Agent) applyControlResult(result ControlResult) error {
	if !result.Proven {
		_, err := agent.updateState(func(state *MachineState) error {
			state.ControlState = "control_degraded"
			return nil
		})
		return err
	}
	state, err := agent.loadState()
	if err != nil {
		return err
	}
	if !state.CommandEngineHealthy {
		if err := agent.checkCommandEngine(); err != nil {
			return err
		}
	}
	now := agent.now().UTC()
	_, err = agent.updateState(func(state *MachineState) error {
		if !state.CommandEngineHealthy || result.Transport != ControlTransportSSE {
			state.ControlState = "control_degraded"
		} else {
			state.ControlState = "online"
		}
		state.LastControlSuccessAt = &now
		return nil
	})
	return err
}

func (agent *Agent) handleControlResult(ctx context.Context, result ControlResult) error {
	if result.Command == nil {
		return agent.applyControlResult(result)
	}
	if err := agent.markCommandEngineFailure(); err != nil {
		return err
	}
	_, handleErr := agent.HandleCommand(ctx, *result.Command)
	if errors.Is(handleErr, ErrAgentRestartRequested) {
		return ErrAgentRestartRequested
	}
	if errors.Is(handleErr, ErrCommandEngine) {
		return handleErr
	}
	if err := agent.checkCommandEngine(); err != nil {
		return err
	}
	return agent.applyControlResult(result)
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
			initial := state.ViewerGeneration == 0 && state.ExpectedViewerGeneration == 0 &&
				state.ViewerNonce == "" && state.ExpectedViewerPID == 0 &&
				(state.ViewerState == "" || state.ViewerState == "not_logged_in")
			authorized := state.ViewerState == "restart_authorized" && state.ExpectedViewerGeneration > state.ViewerGeneration
			if !initial && !authorized {
				return errors.New("Viewer bootstrap generation is not authorized")
			}
			generation := state.ExpectedViewerGeneration
			if initial {
				generation = 1
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
	case "viewer_register":
		payload, err := json.Marshal(struct {
			ServerURL string `json:"serverUrl"`
		}{ServerURL: agent.Config.ServerURL})
		if err != nil {
			return PipeMessage{}, err
		}
		_, err = agent.updateState(func(state *MachineState) error {
			if state.ViewerState != "restarting" || message.PID <= 0 || message.PID == state.ExpectedViewerPID ||
				message.SessionID != state.ExpectedViewerSession || message.Generation != state.ExpectedViewerGeneration ||
				message.Nonce == "" || message.Nonce != state.ViewerNonce {
				return errors.New("stale Viewer registration")
			}
			state.ExpectedViewerPID = message.PID
			state.ViewerState = "starting"
			return nil
		})
		response.Type = "viewer_registered"
		response.Generation = message.Generation
		response.Nonce = message.Nonce
		response.Payload = payload
		return response, err
	case "viewer_heartbeat", "renderer_status", "stream_telemetry", "command_result":
		var stream *ViewerStreamState
		if message.Type == "stream_telemetry" {
			decoded, streamErr := decodeViewerStreamTelemetry(message.Payload, now)
			if streamErr != nil {
				return PipeMessage{}, streamErr
			}
			stream = &decoded
		}
		_, err := agent.updateState(func(state *MachineState) error {
			currentDuringRestart := state.ViewerState == "restart_authorized" && message.Generation == state.ViewerGeneration
			expectedGeneration := (state.ViewerState == "starting" || state.ViewerState == "running") &&
				message.Generation == state.ExpectedViewerGeneration
			if message.PID != state.ExpectedViewerPID || message.SessionID != state.ExpectedViewerSession ||
				(!currentDuringRestart && !expectedGeneration) || message.Nonce == "" || message.Nonce != state.ViewerNonce {
				return errors.New("stale Viewer pipe identity")
			}
			state.ViewerGeneration = message.Generation
			if message.Type != "command_result" && !currentDuringRestart {
				state.ViewerState = "running"
			}
			state.ViewerLastHeartbeatAt = &now
			if message.Type == "renderer_status" {
				var payload struct {
					State string `json:"state"`
				}
				if err := json.Unmarshal(message.Payload, &payload); err != nil || !validRendererState(payload.State) {
					return errors.New("renderer state is required")
				}
				state.RendererState = payload.State
				state.RendererLastHeartbeatAt = &now
			}
			return nil
		})
		if err == nil && stream != nil {
			agent.storeViewerTelemetry(*stream)
		}
		if err == nil && message.Type == "command_result" {
			err = agent.completeViewerCommand(message.Payload)
		}
		if err == nil && message.Type != "command_result" {
			if commandPayload := agent.pendingViewerCommand(); commandPayload != nil {
				response.Type = "command"
				response.Payload = commandPayload
				return response, nil
			}
		}
		response.Type = "ack"
		return response, err
	default:
		return PipeMessage{}, errors.New("unsupported pipe message type")
	}
}

func (agent *Agent) Execute(ctx context.Context, command Command, operationKey string) error {
	switch command.Type {
	case "ping":
		return nil
	case "restart_viewer":
		return agent.executeViewerRestart(ctx, command, operationKey)
	case "restart_agent":
		return ErrAgentRestartRequested
	case "reload_live", "resubscribe_stream":
		return agent.executeViewerCommand(ctx, command, operationKey)
	default:
		return errors.New("command adapter is not installed")
	}
}

func (agent *Agent) executeViewerCommand(ctx context.Context, command Command, operationKey string) error {
	if operationKey == "" || (command.Type != "reload_live" && command.Type != "resubscribe_stream" && command.Type != "shutdown") ||
		(command.Type == "resubscribe_stream" && !validViewerStreamName(command.StreamName)) {
		return ErrCommandRejected
	}
	request := &viewerCommandRequest{command: command, operationKey: operationKey, result: make(chan error, 1)}
	agent.viewerCommandMu.Lock()
	if agent.viewerCommand != nil {
		agent.viewerCommandMu.Unlock()
		return errors.New("Viewer command already pending")
	}
	agent.viewerCommand = request
	agent.viewerCommandMu.Unlock()
	defer func() {
		agent.viewerCommandMu.Lock()
		if agent.viewerCommand == request {
			agent.viewerCommand = nil
		}
		agent.viewerCommandMu.Unlock()
	}()
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(agent.viewerCommandDeadline()):
		return errors.New("Viewer command timed out")
	}
}

func (agent *Agent) executeViewerRestart(ctx context.Context, command Command, operationKey string) error {
	if command.Generation <= 0 || operationKey == "" {
		return ErrCommandRejected
	}
	restartCtx, cancel := context.WithTimeout(ctx, agent.viewerRestartDeadline())
	defer cancel()
	state, err := agent.loadState()
	if err != nil {
		return err
	}
	if state.ViewerGeneration >= command.Generation && state.ViewerState == "running" && state.RendererState == "ready" {
		return nil
	}
	if state.ViewerState == "restart_authorized" && state.ExpectedViewerPID != 0 {
		if err := agent.executeViewerCommand(restartCtx, Command{Type: "shutdown", Generation: command.Generation}, operationKey); err != nil {
			agent.failViewerRestart(command.Generation)
			return err
		}
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err = agent.loadState()
		if err != nil {
			return err
		}
		if state.ViewerGeneration == command.Generation && state.ViewerState == "running" && state.RendererState == "ready" {
			return nil
		}
		select {
		case <-restartCtx.Done():
			agent.failViewerRestart(command.Generation)
			return errors.New("Viewer restart timed out")
		case <-ticker.C:
		}
	}
}

func (agent *Agent) failViewerRestart(generation int64) {
	_, _ = agent.updateState(func(state *MachineState) error {
		if state.ViewerGeneration < generation && state.ExpectedViewerGeneration == generation {
			state.ExpectedViewerGeneration = state.ViewerGeneration
			state.ViewerNonce = ""
			state.ExpectedViewerPID = 0
			state.ExpectedViewerSession = 0
			state.ViewerState = "recovery_failed"
			state.RendererState = "failed"
		}
		return nil
	})
}

func (agent *Agent) viewerRestartDeadline() time.Duration {
	if agent.ViewerRestartDeadline > 0 {
		return agent.ViewerRestartDeadline
	}
	return defaultViewerRestartDeadline
}

func (agent *Agent) pendingViewerCommand() json.RawMessage {
	agent.viewerCommandMu.Lock()
	defer agent.viewerCommandMu.Unlock()
	if agent.viewerCommand == nil || agent.viewerCommand.delivered {
		return nil
	}
	agent.viewerCommand.delivered = true
	payload, err := json.Marshal(struct {
		Type         string `json:"type"`
		StreamName   string `json:"streamName,omitempty"`
		OperationKey string `json:"operationKey"`
	}{agent.viewerCommand.command.Type, agent.viewerCommand.command.StreamName, agent.viewerCommand.operationKey})
	if err != nil {
		return nil
	}
	return payload
}

func (agent *Agent) completeViewerCommand(payload json.RawMessage) error {
	var result struct {
		OperationKey string `json:"operationKey"`
		Succeeded    bool   `json:"succeeded"`
	}
	if err := json.Unmarshal(payload, &result); err != nil || result.OperationKey == "" {
		return errors.New("invalid Viewer command result")
	}
	agent.viewerCommandMu.Lock()
	defer agent.viewerCommandMu.Unlock()
	if agent.viewerCommand == nil || agent.viewerCommand.operationKey != result.OperationKey {
		return errors.New("stale Viewer command result")
	}
	if result.Succeeded && agent.viewerCommand.command.Type == "shutdown" {
		generation := agent.viewerCommand.command.Generation
		if _, err := agent.updateState(func(state *MachineState) error {
			if state.ExpectedViewerGeneration != generation || state.ViewerGeneration >= generation {
				return errors.New("Viewer restart generation changed")
			}
			state.ViewerNonce = ""
			state.ExpectedViewerPID = 0
			state.ExpectedViewerSession = 0
			state.RendererState = "not_ready"
			return nil
		}); err != nil {
			return err
		}
	}
	if result.Succeeded {
		agent.viewerCommand.result <- nil
	} else {
		agent.viewerCommand.result <- errors.New("Viewer command failed")
	}
	agent.viewerCommand = nil
	return nil
}

func decodeViewerStreamTelemetry(payload json.RawMessage, now time.Time) (ViewerStreamState, error) {
	var input struct {
		StreamName     string `json:"streamName"`
		Transport      string `json:"transport"`
		Phase          string `json:"phase"`
		LastBinaryAt   int64  `json:"lastBinaryAt"`
		LastProgressAt int64  `json:"lastProgressAt"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return ViewerStreamState{}, errors.New("invalid Viewer stream telemetry")
	}
	input.StreamName = strings.TrimSpace(input.StreamName)
	if !validViewerStreamName(input.StreamName) ||
		(input.Transport != "webrtc" && input.Transport != "mse") || !validViewerStreamPhase(input.Phase) {
		return ViewerStreamState{}, errors.New("invalid Viewer stream telemetry")
	}
	stream := ViewerStreamState{StreamName: input.StreamName, State: input.Phase, Transport: input.Transport, UpdatedAt: &now}
	stream.LastBinaryAt = viewerTelemetryTime(input.LastBinaryAt)
	stream.LastProgressAt = viewerTelemetryTime(input.LastProgressAt)
	return stream, nil
}

func (agent *Agent) viewerCommandDeadline() time.Duration {
	if agent.ViewerCommandDeadline > 0 {
		return agent.ViewerCommandDeadline
	}
	return defaultViewerCommandDeadline
}

func replaceViewerStream(streams []ViewerStreamState, replacement ViewerStreamState) []ViewerStreamState {
	for index := range streams {
		if streams[index].StreamName == replacement.StreamName {
			streams[index] = replacement
			return streams
		}
	}
	if len(streams) >= 64 {
		streams = streams[1:]
	}
	return append(streams, replacement)
}

func (agent *Agent) storeViewerTelemetry(stream ViewerStreamState) {
	agent.telemetryMu.Lock()
	defer agent.telemetryMu.Unlock()
	agent.viewerStreams = replaceViewerStream(agent.viewerStreams, stream)
	if stream.LastProgressAt != nil && (agent.rendererProgress == nil || stream.LastProgressAt.After(*agent.rendererProgress)) {
		agent.rendererProgress = stream.LastProgressAt
	}
}

func (agent *Agent) viewerTelemetry() ([]ViewerStreamState, *time.Time) {
	agent.telemetryMu.Lock()
	defer agent.telemetryMu.Unlock()
	streams := append([]ViewerStreamState(nil), agent.viewerStreams...)
	return streams, agent.rendererProgress
}

func viewerTelemetryTime(milliseconds int64) *time.Time {
	if milliseconds <= 0 {
		return nil
	}
	value := time.UnixMilli(milliseconds).UTC()
	return &value
}

func validViewerStreamName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n\t") || strings.HasPrefix(value, "//") {
		return false
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		return false
	}
	return true
}

func validViewerStreamPhase(value string) bool {
	switch value {
	case "connecting", "retrying", "fallback", "recovering", "playing", "stalled", "cooldown", "unsupported":
		return true
	default:
		return false
	}
}

func validRendererState(value string) bool {
	switch value {
	case "ready", "not_ready", "unresponsive", "failed":
		return true
	default:
		return false
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
