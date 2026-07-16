package vieweragent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"camstation/internal/viewerinstall"
)

const defaultHeartbeatInterval = 10 * time.Second
const defaultViewerCommandDeadline = 45 * time.Second
const defaultViewerRestartDeadline = 45 * time.Second
const defaultViewerShutdownDeadline = 5 * time.Second
const viewerHealthCheckInterval = time.Second
const viewerHealthStaleAfter = 15 * time.Second
const controlCommandQueueDepth = 16

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

type UpdateExecutor interface {
	Run(context.Context, UpdateTarget) error
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
	ServePipe                func(context.Context, Config, func(PipeMessage) (PipeMessage, error), func()) error
	Ready                    func()
	Updater                  UpdateExecutor

	stateMu          sync.Mutex
	telemetryMu      sync.Mutex
	viewerStreams    []ViewerStreamState
	rendererProgress *time.Time
	viewerCommandMu  sync.Mutex
	viewerCommand    *viewerCommandRequest
	commandMu        sync.Mutex
	sideEffectMu     sync.Mutex
	recoveryMu       sync.Mutex
	recoveryInFlight bool
}

type viewerCommandRequest struct {
	command      Command
	operationKey string
	result       chan error
	delivered    bool
}

type controlCommandDispatcher struct {
	agent       *Agent
	ctx         context.Context
	cancel      context.CancelFunc
	updates     chan ControlResult
	immediate   chan ControlResult
	workers     sync.WaitGroup
	errOnce     sync.Once
	terminalErr error
}

type controlCommandGate struct {
	ready    atomic.Bool
	dispatch func(ControlResult) error
}

func (gate *controlCommandGate) open() { gate.ready.Store(true) }

func (gate *controlCommandGate) dispatchWhenReady(result ControlResult) error {
	if result.Command != nil && !gate.ready.Load() {
		return nil
	}
	return gate.dispatch(result)
}

func newControlCommandDispatcher(ctx context.Context, cancel context.CancelFunc, agent *Agent) *controlCommandDispatcher {
	dispatcher := &controlCommandDispatcher{
		agent: agent, ctx: ctx, cancel: cancel,
		updates: make(chan ControlResult, controlCommandQueueDepth), immediate: make(chan ControlResult, controlCommandQueueDepth),
	}
	dispatcher.workers.Add(2)
	go dispatcher.run(dispatcher.updates)
	go dispatcher.run(dispatcher.immediate)
	return dispatcher
}

func (dispatcher *controlCommandDispatcher) dispatch(result ControlResult) error {
	if result.Command == nil {
		return dispatcher.agent.handleControlResult(dispatcher.ctx, result)
	}
	queue := dispatcher.immediate
	if result.Command.Type == "update_app" {
		queue = dispatcher.updates
	}
	select {
	case queue <- result:
		return nil
	case <-dispatcher.ctx.Done():
		return dispatcher.ctx.Err()
	default:
		return errors.New("control command queue is full")
	}
}

func (dispatcher *controlCommandDispatcher) run(queue <-chan ControlResult) {
	defer dispatcher.workers.Done()
	for {
		if dispatcher.ctx.Err() != nil {
			return
		}
		select {
		case <-dispatcher.ctx.Done():
			return
		case result := <-queue:
			err := dispatcher.agent.handleControlResult(dispatcher.ctx, result)
			if errors.Is(err, ErrCommandEngine) {
				continue
			}
			if err != nil {
				dispatcher.errOnce.Do(func() {
					dispatcher.terminalErr = err
					dispatcher.cancel()
				})
				return
			}
		}
	}
}

func (dispatcher *controlCommandDispatcher) wait() error {
	dispatcher.workers.Wait()
	return dispatcher.terminalErr
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
	agent.commandMu.Lock()
	commandLocked := true
	defer func() {
		if commandLocked {
			agent.commandMu.Unlock()
		}
	}()
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
		agent.sideEffectMu.Lock()
		allowed, stateErr := agent.updateState(func(state *MachineState) error {
			if state.ForcedViewerRestartID == command.Key() && state.ExpectedViewerGeneration > state.ViewerGeneration {
				generation = state.ExpectedViewerGeneration
				return nil
			}
			if state.ViewerState == "restart_authorized" && state.ExpectedViewerGeneration > state.ViewerGeneration &&
				(state.ViewerNonce == "" || state.ExpectedViewerPID == 0) {
				generation = state.ExpectedViewerGeneration
				state.ForcedViewerRestartID = command.Key()
				state.ForcedViewerRestartAt = &now
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
		agent.sideEffectMu.Unlock()
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
	agent.commandMu.Unlock()
	commandLocked = false
	executionErr := agent.executeCommand(ctx, executor, command, record.OperationKey)
	if errors.Is(executionErr, ErrAgentRestartRequested) {
		return record, ErrAgentRestartRequested
	}
	if executionErr != nil {
		return agent.finishExecutedCommand(ctx, command, record, CommandFailed, commandErrorCategory(executionErr))
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
	return agent.finishExecutedCommand(ctx, command, record, CommandSucceeded, "")
}

func (agent *Agent) Reconcile(ctx context.Context) (results []CommandRecord, resultErr error) {
	defer func() {
		if errors.Is(resultErr, ErrCommandEngine) {
			_ = agent.markCommandEngineFailure()
		}
	}()
	if _, err := ReconcileCommittedUpdate(filepath.Dir(agent.Paths.Update)); err != nil {
		return nil, err
	}
	if err := agent.reconcileAbandonedInstallerHandoff(); err != nil {
		return nil, err
	}
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
		if reconciled.State == CommandRunning && reconciled.Type == "update_app" {
			journal, loadErr := LoadUpdateJournal(agent.Paths.Update)
			if loadErr != nil {
				return results, loadErr
			}
			if updateCanResume(journal.State) {
				reconciled, reconcileErr = agent.resumeUpdate(ctx, key, reconciled, ledger)
				if reconcileErr != nil {
					return results, reconcileErr
				}
			}
		}
		if reconciled.State != CommandRunning {
			results = append(results, reconciled)
			agent.report(ctx, Command{ID: record.ID, Type: record.Type, PayloadHash: record.PayloadHash}, reconciled.State, reconciled.OperationKey, reconciled.Error)
		}
	}
	return results, nil
}

func (agent *Agent) reconcileAbandonedInstallerHandoff() error {
	stateDir := filepath.Dir(agent.Paths.Update)
	layout := viewerinstall.Layout{InstallDir: agent.Config.InstallDir, StateDir: stateDir}
	transaction, err := viewerinstall.LoadJournal(layout)
	if err != nil {
		return err
	}
	journal, err := LoadUpdateJournal(agent.Paths.Update)
	if err != nil {
		return err
	}
	if journal.State != "installer_launched" || !transactionMatchesUpdate(transaction, journal) || !incompleteTransactionPhase(transaction.Phase) {
		return nil
	}
	if !filepath.IsAbs(layout.InstallDir) {
		return errors.New("absolute Viewer install directory is required for ownership probe")
	}
	return withAvailableTransactionOwnership(layout, func(*viewerinstall.Ownership) error {
		return agent.reconcileAbandonedInstallerHandoffOwned(layout)
	})
}

func withAvailableTransactionOwnership(layout viewerinstall.Layout, critical func(*viewerinstall.Ownership) error) (resultErr error) {
	if critical == nil {
		return errors.New("transaction critical section is required")
	}
	owner, err := viewerinstall.Acquire(layout)
	if errors.Is(err, viewerinstall.ErrUpdateOwned) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, owner.Close())
	}()
	return critical(owner)
}

func (agent *Agent) reconcileAbandonedInstallerHandoffOwned(layout viewerinstall.Layout) error {
	// Re-read and save while retaining the machine-wide transaction owner so a
	// live installer cannot commit between the observation and state change.
	transaction, err := viewerinstall.LoadJournal(layout)
	if err != nil {
		return err
	}
	journal, err := LoadUpdateJournal(agent.Paths.Update)
	if err != nil {
		return err
	}
	if journal.State != "installer_launched" || !transactionMatchesUpdate(transaction, journal) || !incompleteTransactionPhase(transaction.Phase) {
		return nil
	}
	journal.State = "launching_installer"
	journal.LastError = ""
	return SaveUpdateJournal(agent.Paths.Update, journal)
}

func updateCanResume(state string) bool {
	switch state {
	case "", "checking_release", "metadata_retry_wait", "downloading", "download_retry_wait", "verified", "waiting_for_viewer_session", "launching_installer", "launch_failed":
		return true
	default:
		return false
	}
}

func (agent *Agent) resumeUpdate(ctx context.Context, key string, record CommandRecord, ledger CommandLedger) (CommandRecord, error) {
	executor := agent.Executor
	if executor == nil {
		executor = agent
	}
	command := Command{
		ID: record.ID, Type: record.Type, PayloadHash: record.PayloadHash,
		DesiredVersion: record.DesiredVersion, ArtifactSHA256: record.ArtifactSHA256, Generation: record.Generation,
	}
	err := agent.executeCommand(ctx, executor, command, record.OperationKey)
	if errors.Is(err, ErrAgentRestartRequested) {
		return record, ErrAgentRestartRequested
	}
	now := agent.now().UTC()
	if err != nil {
		record.State = CommandFailed
		record.Error = commandErrorCategory(err)
	} else {
		record.State = CommandSucceeded
	}
	record.CompletedAt = &now
	ledger.Records[key] = record
	if saveErr := agent.saveCommandLedger(ledger); saveErr != nil {
		return record, saveErr
	}
	return record, err
}

func (agent *Agent) resumeViewerRestart(ctx context.Context, key string, record CommandRecord, ledger CommandLedger) (CommandRecord, error) {
	executor := agent.Executor
	if executor == nil {
		executor = agent
	}
	command := Command{ID: record.ID, Type: record.Type, PayloadHash: record.PayloadHash, Generation: record.Generation}
	executionErr := agent.executeCommand(ctx, executor, command, record.OperationKey)
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
		convergeViewerAfterAgentStart(state)
		if state.ViewerState == "" {
			state.ViewerState = "not_logged_in"
		}
		return nil
	}); err != nil {
		return err
	}

	servePipe := agent.ServePipe
	if servePipe == nil {
		servePipe = ServeViewerPipe
	}
	pipeReady := make(chan struct{})
	pipeDone := make(chan struct{})
	var pipeReadyOnce sync.Once
	var pipeErr error
	go func() {
		defer close(pipeDone)
		pipeErr = servePipe(runCtx, agent.Config, agent.handlePipeMessage, func() {
			pipeReadyOnce.Do(func() { close(pipeReady) })
		})
		if pipeErr != nil && runCtx.Err() == nil {
			_ = agent.markPipeFailure()
		}
	}()
	joinPipe := func() {
		cancel()
		<-pipeDone
	}
	pipeFailure := func() error {
		if pipeErr == nil {
			return errors.New("Viewer pipe stopped during Agent startup")
		}
		return fmt.Errorf("start Viewer pipe: %w", pipeErr)
	}
	select {
	case <-pipeReady:
	case <-pipeDone:
		return pipeFailure()
	case <-ctx.Done():
		joinPipe()
		return nil
	}
	if err := agent.checkCommandEngine(); err != nil {
		joinPipe()
		return err
	}
	dispatcher := newControlCommandDispatcher(runCtx, cancel, agent)
	heartbeatCommands := controlCommandGate{dispatch: dispatcher.dispatch}
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		agent.runHeartbeats(runCtx, client, heartbeatCommands.dispatchWhenReady)
	}()
	joinRuntime := func() {
		cancel()
		<-heartbeatDone
		<-pipeDone
		_ = dispatcher.wait()
	}

	reconcileDone := make(chan error, 1)
	go func() {
		_, err := agent.Reconcile(runCtx)
		reconcileDone <- err
	}()
	select {
	case err := <-reconcileDone:
		if err != nil {
			joinRuntime()
			return err
		}
	case <-pipeDone:
		cancel()
		<-reconcileDone
		<-heartbeatDone
		if ctx.Err() != nil {
			return nil
		}
		return pipeFailure()
	case <-ctx.Done():
		cancel()
		<-reconcileDone
		<-heartbeatDone
		<-pipeDone
		return nil
	}
	if err := agent.checkCommandEngine(); err != nil {
		joinRuntime()
		return err
	}
	heartbeatCommands.open()
	select {
	case <-pipeDone:
		joinRuntime()
		return pipeFailure()
	case <-ctx.Done():
		joinRuntime()
		return nil
	default:
	}
	if agent.Ready != nil {
		agent.Ready()
	}
	supervisorDone := make(chan struct{})
	go func() {
		defer close(supervisorDone)
		agent.runViewerSupervisor(runCtx)
	}()
	shutdown := func() {
		cancel()
		<-supervisorDone
		<-heartbeatDone
		<-pipeDone
	}

	controlErr := client.RunControl(runCtx, &ReconnectState{}, dispatcher.dispatch)
	cancel()
	dispatchErr := dispatcher.wait()
	shutdown()
	if dispatchErr != nil {
		controlErr = dispatchErr
	}
	if errors.Is(controlErr, ErrAgentRestartRequested) {
		return ErrAgentRestartRequested
	}
	if ctx.Err() != nil || errors.Is(controlErr, context.Canceled) {
		return nil
	}
	return controlErr
}

func convergeViewerAfterAgentStart(state *MachineState) {
	authorized := state.ViewerState == "restart_authorized" && state.ExpectedViewerGeneration > state.ViewerGeneration
	staleState := state.ViewerState == "running" || state.ViewerState == "starting" || state.ViewerState == "restarting"
	staleIdentity := state.ViewerNonce != "" || state.ExpectedViewerPID != 0 || state.ExpectedViewerSession != 0
	if !authorized && !staleState && !staleIdentity {
		return
	}
	if state.ExpectedViewerGeneration <= state.ViewerGeneration {
		state.ExpectedViewerGeneration = state.ViewerGeneration + 1
	}
	state.ViewerState = "restart_authorized"
	state.RendererState = "not_ready"
	state.ViewerNonce = ""
	state.ExpectedViewerPID = 0
	state.ExpectedViewerSession = 0
}

func (agent *Agent) runHeartbeats(ctx context.Context, client ControlClient, dispatch ...func(ControlResult) error) {
	interval := agent.HeartbeatInterval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		agent.sendHeartbeat(ctx, client, dispatch...)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (agent *Agent) runViewerSupervisor(ctx context.Context) {
	ticker := time.NewTicker(viewerHealthCheckInterval)
	defer ticker.Stop()
	for {
		_ = agent.recoverViewerIfUnhealthy(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (agent *Agent) recoverViewerIfUnhealthy(ctx context.Context) error {
	agent.recoveryMu.Lock()
	if agent.recoveryInFlight {
		agent.recoveryMu.Unlock()
		return nil
	}
	agent.recoveryInFlight = true
	agent.recoveryMu.Unlock()
	defer func() {
		agent.recoveryMu.Lock()
		agent.recoveryInFlight = false
		agent.recoveryMu.Unlock()
	}()

	agent.sideEffectMu.Lock()
	if err := ctx.Err(); err != nil {
		agent.sideEffectMu.Unlock()
		return err
	}
	now := agent.now().UTC()
	state, err := agent.loadState()
	if err != nil || viewerRecoveryCause(state, now) == "" {
		agent.sideEffectMu.Unlock()
		return err
	}
	var generation int64
	var allowed bool
	_, err = agent.updateState(func(state *MachineState) error {
		if viewerRecoveryCause(*state, now) == "" {
			return nil
		}
		allowed, generation = state.AllowViewerRestart(now, false, "")
		if !allowed {
			state.ViewerState = "recovery_failed"
			return nil
		}
		state.ViewerState = "restart_authorized"
		return nil
	})
	agent.sideEffectMu.Unlock()
	if err != nil || !allowed {
		return err
	}
	return agent.executeViewerRestart(ctx, Command{Type: "restart_viewer", Generation: generation}, fmt.Sprintf("viewer-generation-%d", generation))
}

func viewerRecoveryCause(state MachineState, now time.Time) string {
	if state.ViewerState != "running" {
		return ""
	}
	switch state.RendererState {
	case "unresponsive":
		return "renderer_unresponsive"
	case "failed":
		return "renderer_failed"
	}
	if viewerHealthStale(state.ViewerLastHeartbeatAt, now) {
		return "ipc_stale"
	}
	if viewerHealthStale(state.RendererLastHeartbeatAt, now) {
		return "renderer_stale"
	}
	return ""
}

func viewerHealthStale(at *time.Time, now time.Time) bool {
	return at == nil || at.After(now.Add(5*time.Second)) || now.Sub(*at) >= viewerHealthStaleAfter
}

func (agent *Agent) sendHeartbeat(ctx context.Context, client ControlClient, dispatch ...func(ControlResult) error) {
	state, err := agent.loadState()
	if err != nil {
		return
	}
	hostname, _ := os.Hostname()
	installedVersion, artifactSHA256 := installedReleaseIdentity(agent.Config.InstallDir)
	if installedVersion == "" {
		installedVersion = agent.AgentVersion
	}
	heartbeat := HeartbeatPayload{
		ID: agent.Config.ClientID, DisplayName: agent.Config.DisplayName, AppVersion: installedVersion,
		Hostname: hostname, Route: "/live?viewer=1", Mode: "live",
	}
	heartbeat.Agent.State = "online"
	switch state.ViewerState {
	case "restart_authorized", "restarting":
		heartbeat.Agent.State = "recovering"
	case "recovery_failed":
		heartbeat.Agent.State = "recovery_failed"
	}
	heartbeat.Agent.Version = installedVersion
	heartbeat.Agent.ArtifactSHA256 = artifactSHA256
	heartbeat.Control.State = state.ControlState
	heartbeat.Control.LastSuccessAt = state.LastControlSuccessAt
	heartbeat.Viewer.State = state.ViewerState
	if heartbeat.Viewer.State == "" {
		heartbeat.Viewer.State = "not_logged_in"
	}
	heartbeat.Viewer.LastHeartbeatAt = state.ViewerLastHeartbeatAt
	heartbeat.Viewer.Version = installedVersion
	heartbeat.Renderer.State = state.RendererState
	if heartbeat.Renderer.State == "" {
		heartbeat.Renderer.State = "not_ready"
	}
	heartbeat.Renderer.LastHeartbeatAt = state.RendererLastHeartbeatAt
	heartbeat.Streams, heartbeat.Renderer.LastProgressAt = agent.viewerTelemetry()
	journal, _ := LoadUpdateJournal(agent.Paths.Update)
	heartbeat.Update.State = journal.State
	heartbeat.Update.TargetVersion = journal.TargetVersion
	heartbeat.Update.ArtifactSHA256 = journal.ArtifactSHA256
	heartbeat.Update.Generation = journal.Generation
	heartbeat.Update.CommandID = journal.CommandID
	heartbeat.Update.PayloadHash = journal.PayloadHash
	heartbeat.Update.TransactionID = journal.TransactionID
	deadline := agent.HeartbeatRequestDeadline
	if deadline <= 0 {
		deadline = DefaultHeartbeatRequestDeadline
	}
	heartbeatCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	if response, err := client.ExchangeHeartbeat(heartbeatCtx, heartbeat); err == nil {
		now := agent.now().UTC()
		_, _ = agent.updateState(func(state *MachineState) error {
			state.LastHeartbeatAt = &now
			return nil
		})
		_, _ = ReconcileCommittedUpdate(filepath.Dir(agent.Paths.Update))
		_ = agent.acceptHeartbeatCommit(response)
		if response.DesiredRelease != nil && len(dispatch) > 0 && dispatch[0] != nil {
			command := response.DesiredRelease.Command()
			_ = dispatch[0](ControlResult{Transport: controlTransportHeartbeat, Command: &command})
		}
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

func (agent *Agent) finishExecutedCommand(ctx context.Context, command Command, record CommandRecord, state CommandState, message string) (CommandRecord, error) {
	agent.commandMu.Lock()
	defer agent.commandMu.Unlock()
	ledger, err := agent.loadCommandLedger()
	if err != nil {
		return CommandRecord{}, err
	}
	return agent.finishCommand(ctx, command, ledger, record, state, message)
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
		exactTarget := journal.TargetVersion == record.DesiredVersion && strings.EqualFold(journal.ArtifactSHA256, record.ArtifactSHA256) && journal.Generation == record.Generation
		if exactTarget && (journal.State == "metadata_failed" || journal.State == "rejected") {
			now := agent.now().UTC()
			record.State = CommandFailed
			if journal.State == "rejected" {
				record.State = CommandRejected
			}
			record.Error = journal.LastError
			if record.Error == "" {
				record.Error = "metadata_request_failed"
			}
			record.CompletedAt = &now
			ledger.Records[key] = record
			if err := agent.saveCommandLedger(ledger); err != nil {
				return record, err
			}
			return record, nil
		}
		reached = exactTarget && journal.State == "committed"
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
	agent.commandMu.Lock()
	defer agent.commandMu.Unlock()
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
	if result.Transport == controlTransportHeartbeat {
		return nil
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

func (agent *Agent) executeCommand(ctx context.Context, executor Executor, command Command, operationKey string) error {
	if command.Type != "restart_agent" {
		return executor.Execute(ctx, command, operationKey)
	}
	agent.sideEffectMu.Lock()
	defer agent.sideEffectMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return executor.Execute(ctx, command, operationKey)
}

func supportedCommand(commandType string) bool {
	switch commandType {
	case "ping", "reload_live", "restart_viewer", "restart_agent", "resubscribe_stream", "update_app":
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
		var rendererState, rendererSource string
		if message.Type == "stream_telemetry" {
			decoded, streamErr := decodeViewerStreamTelemetry(message.Payload, now)
			if streamErr != nil {
				return PipeMessage{}, streamErr
			}
			stream = &decoded
		}
		if message.Type == "renderer_status" {
			var payload struct {
				State  string `json:"state"`
				Source string `json:"source"`
			}
			if err := json.Unmarshal(message.Payload, &payload); err != nil || !validRendererStatus(payload.State, payload.Source) {
				return PipeMessage{}, errors.New("renderer state and source are required")
			}
			rendererState, rendererSource = payload.State, payload.Source
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
			if message.Type != "command_result" && message.Type != "renderer_status" &&
				!currentDuringRestart && state.ViewerState != "starting" {
				state.ViewerState = "running"
			}
			state.ViewerLastHeartbeatAt = &now
			if message.Type == "renderer_status" {
				if !currentDuringRestart {
					state.RendererState = rendererState
					if rendererSource == "renderer" {
						state.RendererLastHeartbeatAt = &now
						state.ViewerState = "running"
					}
				}
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
	case "update_app":
		target := UpdateTarget{
			Version: command.DesiredVersion, SHA256: strings.ToLower(command.ArtifactSHA256),
			Generation: command.Generation, TransactionID: operationKey,
			CommandID: command.ID, PayloadHash: command.PayloadHash,
		}
		var err error
		if agent.Updater != nil {
			err = agent.Updater.Run(ctx, target)
		} else {
			runner := UpdateRunner{
				HTTPClient: agent.HTTPClient, ServerURL: agent.Config.ServerURL, StateDir: filepath.Dir(agent.Paths.Update),
				AllowDevelopmentUnsigned: agent.Config.AllowDevelopmentUnsigned,
				ExpectedSignerThumbprint: agent.Config.SignerThumbprint,
			}
			var prepared preparedUpdate
			prepared, err = runner.prepare(ctx, target)
			if err == nil {
				err = agent.activatePreparedUpdate(ctx, prepared)
			}
		}
		if errors.Is(err, ErrUpdateLaunched) {
			return ErrAgentRestartRequested
		}
		return err
	case "reload_live", "resubscribe_stream":
		return agent.executeViewerCommand(ctx, command, operationKey)
	default:
		return errors.New("command adapter is not installed")
	}
}

func (agent *Agent) waitViewerReady(ctx context.Context, stableFor time.Duration) error {
	_, err := agent.waitViewerReadyGeneration(ctx, stableFor)
	return err
}

func (agent *Agent) waitViewerReadyGeneration(ctx context.Context, stableFor time.Duration) (int64, error) {
	if stableFor <= 0 {
		return 0, errors.New("invalid Viewer-ready duration")
	}
	interval := stableFor / 4
	if interval < 5*time.Millisecond {
		interval = 5 * time.Millisecond
	}
	if interval > 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var readySince time.Time
	var readyGeneration int64
	for {
		state, err := agent.loadState()
		if err != nil {
			return 0, err
		}
		now := agent.now()
		generation := readyViewerGeneration(state, now)
		if generation > 0 {
			if readySince.IsZero() || readyGeneration != generation {
				readySince = now
				readyGeneration = generation
			}
			if now.Sub(readySince) >= stableFor {
				return readyGeneration, nil
			}
		} else {
			readySince = time.Time{}
			readyGeneration = 0
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

func readyViewerGeneration(state MachineState, now time.Time) int64 {
	freshViewer := state.ViewerLastHeartbeatAt != nil && !state.ViewerLastHeartbeatAt.After(now.Add(5*time.Second)) && now.Sub(*state.ViewerLastHeartbeatAt) <= viewerHealthStaleAfter
	freshRenderer := state.RendererLastHeartbeatAt != nil && !state.RendererLastHeartbeatAt.After(now.Add(5*time.Second)) && now.Sub(*state.RendererLastHeartbeatAt) <= viewerHealthStaleAfter
	if state.ViewerGeneration <= 0 || state.ExpectedViewerGeneration != state.ViewerGeneration ||
		state.ViewerState != "running" || state.RendererState != "ready" || !freshViewer || !freshRenderer {
		return 0
	}
	return state.ViewerGeneration
}

func (agent *Agent) activatePreparedUpdate(ctx context.Context, prepared preparedUpdate) error {
	if err := prepared.markWaiting(); err != nil {
		return err
	}
	for {
		generation, err := agent.waitViewerReadyGeneration(ctx, 30*time.Second)
		if err != nil {
			return err
		}
		launched, err := agent.launchPreparedUpdateIfReady(ctx, prepared, generation)
		if err != nil || launched {
			return err
		}
	}
}

func (agent *Agent) launchPreparedUpdateIfReady(ctx context.Context, prepared preparedUpdate, generation int64) (bool, error) {
	agent.sideEffectMu.Lock()
	defer agent.sideEffectMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	state, err := agent.loadState()
	if err != nil {
		return false, err
	}
	if readyViewerGeneration(state, agent.now()) != generation {
		return false, nil
	}
	return true, prepared.launchInstaller()
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
		shutdownCtx, cancelShutdown := context.WithTimeout(restartCtx, defaultViewerShutdownDeadline)
		_ = agent.executeViewerCommand(shutdownCtx, Command{Type: "shutdown", Generation: command.Generation}, operationKey)
		cancelShutdown()
	}
	if err := agent.prepareViewerRelaunch(command.Generation); err != nil {
		agent.failViewerRestart(command.Generation)
		return err
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

func (agent *Agent) prepareViewerRelaunch(generation int64) error {
	_, err := agent.updateState(func(state *MachineState) error {
		if state.ViewerGeneration >= generation {
			return nil
		}
		if state.ExpectedViewerGeneration == generation &&
			(state.ViewerState == "restarting" || state.ViewerState == "starting") {
			return nil
		}
		if state.ExpectedViewerGeneration != generation || state.ViewerState != "restart_authorized" {
			return errors.New("Viewer restart generation changed")
		}
		state.ViewerNonce = ""
		state.ExpectedViewerPID = 0
		state.ExpectedViewerSession = 0
		state.RendererState = "not_ready"
		return nil
	})
	return err
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

func validRendererStatus(state, source string) bool {
	if source == "renderer" {
		return state == "ready"
	}
	if source != "host" {
		return false
	}
	switch state {
	case "not_ready", "unresponsive", "failed":
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
