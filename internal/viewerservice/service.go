package viewerservice

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var ErrListenerClosed = errors.New("viewer service listener is closed")
var ErrCommandQueueFull = errors.New("viewer command queue is full")

type PipeConnection interface {
	io.ReadWriteCloser
	Peer() (Peer, error)
}

type PipeListener interface {
	Accept() (PipeConnection, error)
	Close() error
	Ready() <-chan struct{}
}

func (service *Service) Ready() <-chan struct{} {
	return service.Listener.Ready()
}

type Service struct {
	Store            ConfigStore
	Listener         PipeListener
	Server           *Server
	Logs             *LogManager
	Control          ControlLoop
	ControlLoop      ControlLoop
	Journal          CommandJournalStore
	Engine           *CommandEngine
	ViewerLauncher   ViewerLauncher
	ServiceRestarter ServiceRestarter

	mu             sync.Mutex
	connections    map[PipeConnection]struct{}
	byID           map[string]*serviceConnection
	desired        UpdateNotice
	reporter       CommandReporter
	commandQueue   chan Command
	commandResults map[string]chan LocalCommandResult
	handlers       sync.WaitGroup
}

type serviceConnection struct {
	connection PipeConnection
	id         string
	writeMu    sync.Mutex
	commands   chan queuedCommand
	writerDone chan struct{}
}

type deferredConnectionValidator struct{}

func (deferredConnectionValidator) Validate(context.Context, ConfigDraft, string) error {
	// The control loop owns bounded server connectivity and registration checks
	// after persistence.  A first-run configuration must therefore not be
	// rejected merely because no synchronous validator was injected.
	return nil
}

type queuedCommand struct {
	token LeaseToken
	event Event
}

func (state *serviceConnection) runWriter(leases *LeaseManager) {
	defer close(state.writerDone)
	for item := range state.commands {
		if !leases.ValidateToken(item.token) {
			continue
		}
		state.writeMu.Lock()
		if leases.ValidateToken(item.token) {
			_ = WriteEvent(state.connection, item.event)
		}
		state.writeMu.Unlock()
	}
}

func (service *Service) Run(ctx context.Context) error {
	if service.Listener == nil {
		return errors.New("viewer service listener is unavailable")
	}
	runCtx, cancel := context.WithCancel(ctx)
	server := service.server()
	server.SetCommandResultHandler(service.acceptCommandResult)
	engine := service.commandEngine()
	if err := engine.Start(); err != nil {
		return fmt.Errorf("start viewer command engine: %w", err)
	}
	commandDone := make(chan struct{})
	go func() {
		defer close(commandDone)
		service.runCommands(runCtx, engine)
	}()
	controlCtx, controlCancel := context.WithCancel(runCtx)
	controlDone := make(chan struct{})
	go func() {
		defer close(controlDone)
		service.runControl(controlCtx, server)
	}()
	if service.Logs != nil {
		_ = service.Logs.WriteService(LogRecord{Component: "service", State: "running"})
		defer func() { _ = service.Logs.WriteService(LogRecord{Component: "service", State: "stopped"}) }()
	}
	stopClosing := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			_ = service.Listener.Close()
			service.closeConnections()
		case <-stopClosing:
		}
	}()
	defer close(stopClosing)
	defer func() {
		cancel()
		controlCancel()
		<-controlDone
		<-commandDone
		_ = service.Listener.Close()
		service.closeConnections()
		service.handlers.Wait()
	}()

	for {
		connection, err := service.Listener.Accept()
		if err != nil {
			if runCtx.Err() != nil || errors.Is(err, ErrListenerClosed) {
				return nil
			}
			// A failed instance must not take down the local service.
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-runCtx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
				continue
			}
		}
		if connection == nil {
			continue
		}
		service.addConnection(connection)
		service.handlers.Add(1)
		go service.handleConnection(runCtx, server, connection)
	}
}

func (service *Service) commandEngine() *CommandEngine {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.Engine == nil {
		store := service.Journal
		if store == nil {
			store = &MemoryCommandJournalStore{}
		}
		service.Engine = &CommandEngine{Store: store, Executor: service, Reporter: service.commandReporter}
	}
	return service.Engine
}

func (service *Service) commandReporter() CommandReporter {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.reporter
}

func (service *Service) SetCommandReporter(reporter CommandReporter) {
	service.mu.Lock()
	service.reporter = reporter
	engine := service.Engine
	service.mu.Unlock()
	if reporter != nil && engine != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = engine.Reconcile(ctx)
	}
}

func (service *Service) commandChannel() chan Command {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.commandQueue == nil {
		service.commandQueue = make(chan Command, 16)
	}
	return service.commandQueue
}

func (service *Service) runCommands(ctx context.Context, engine *CommandEngine) {
	queue := service.commandChannel()
	for {
		select {
		case <-ctx.Done():
			return
		case command := <-queue:
			_ = engine.Handle(ctx, command)
			if ctx.Err() != nil {
				return
			}
		}
	}
}

func (service *Service) Status() StatusSnapshot {
	status, err := service.server().status(context.Background())
	if err != nil {
		return StatusSnapshot{Viewer: "closed", Renderer: "not_ready", Update: UpdateSnapshot{State: "idle"}}
	}
	return status
}

func (service *Service) server() *Server {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.Server != nil {
		return service.Server
	}
	manager := ConfigManager{Store: service.Store, Validator: deferredConnectionValidator{}, NewID: newLeaseID}
	var logError func(context.Context, error) string
	if service.Logs != nil {
		logError = service.Logs.ErrorLogger
	}
	service.Server = NewServer(manager, NewLeaseManager(time.Now, 15*time.Second), "", logError)
	service.Server.SetCommandResultHandler(service.acceptCommandResult)
	if service.Logs != nil {
		service.Server.SetLeaseLogAssigner(service.Logs.AssignViewerLog)
	}
	return service.Server
}

func (service *Service) runControl(ctx context.Context, server *Server) {
	loop := service.Control
	if loop == nil {
		loop = service.ControlLoop
	}
	if loop == nil {
		loop = HTTPControlLoop{}
	}
	for {
		config, err := loadOrEmpty(ctx, service.Store)
		if err == nil && config.SchemaVersion != 0 && strings.TrimSpace(config.ClientID) != "" {
			if runErr := loop.Run(ctx, config, server, service); runErr != nil && ctx.Err() == nil {
				server.SetConnection("degraded")
				timer := time.NewTimer(250 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			if ctx.Err() != nil {
				return
			}
			continue
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (service *Service) DeliverViewerCommand(command Command) error {
	if command.Type == "restart_agent" {
		command.Type = "restart_service"
	}
	select {
	case service.commandChannel() <- command:
		return nil
	default:
		return ErrCommandQueueFull
	}
}

type viewerCommandEvent struct {
	Type         string `json:"type"`
	StreamName   string `json:"streamName,omitempty"`
	OperationKey string `json:"operationKey"`
}

func (service *Service) ExecuteCommand(ctx context.Context, command Command, operationKey string) error {
	switch command.Type {
	case "ping":
		return nil
	case "reload_live", "resubscribe_stream":
		commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		return service.executeViewerIPCCommand(commandCtx, command, operationKey)
	case "restart_viewer":
		return service.restartViewer(ctx, command, operationKey)
	case "restart_service":
		return service.restartService(ctx, operationKey)
	default:
		return RejectCommand(CommandErrorUnsupported)
	}
}

func (service *Service) executeViewerIPCCommand(ctx context.Context, command Command, operationKey string) error {
	server := service.server()
	token, ok := server.leases.Token()
	if !ok {
		return RejectCommand("viewer_unavailable")
	}
	result := make(chan LocalCommandResult, 1)
	service.mu.Lock()
	if service.commandResults == nil {
		service.commandResults = make(map[string]chan LocalCommandResult)
	}
	if _, exists := service.commandResults[operationKey]; exists {
		service.mu.Unlock()
		return errors.New("viewer command operation is already active")
	}
	service.commandResults[operationKey] = result
	service.mu.Unlock()
	defer func() {
		service.mu.Lock()
		delete(service.commandResults, operationKey)
		service.mu.Unlock()
	}()
	payload, err := json.Marshal(viewerCommandEvent{Type: command.Type, StreamName: command.StreamName, OperationKey: operationKey})
	if err != nil {
		return err
	}
	if err := service.dispatchViewerEvent(token, Event{
		Version: PipeProtocolVersion, Event: "viewer_command", EventID: "command-" + command.Key(), Payload: payload,
	}); err != nil {
		if errors.Is(err, ErrLeaseOwner) {
			return RejectCommand("viewer_unavailable")
		}
		return err
	}
	select {
	case <-ctx.Done():
		return FailCommand("viewer_command_timeout")
	case reported := <-result:
		if !reported.Succeeded {
			switch reported.ErrorCode {
			case "renderer_failed", "viewer_command_failed", "viewer_relaunch_failed":
				return FailCommand(reported.ErrorCode)
			default:
				return FailCommand("viewer_command_failed")
			}
		}
		return nil
	}
}

func (service *Service) dispatchViewerEvent(token LeaseToken, event Event) error {
	server := service.server()
	service.mu.Lock()
	state := service.byID[token.ConnectionID]
	service.mu.Unlock()
	if state == nil {
		return ErrLeaseOwner
	}
	return server.leases.WithToken(token, func() error {
		select {
		case state.commands <- queuedCommand{token: token, event: event}:
			return nil
		default:
			return ErrCommandQueueFull
		}
	})
}

func (service *Service) acceptCommandResult(result LocalCommandResult) error {
	service.mu.Lock()
	waiter := service.commandResults[result.OperationKey]
	service.mu.Unlock()
	if waiter == nil {
		return fmt.Errorf("%w: command operation is not active", ErrInvalidRequest)
	}
	select {
	case waiter <- result:
		return nil
	default:
		return fmt.Errorf("%w: command result already received", ErrInvalidRequest)
	}
}

func (service *Service) SetDesiredUpdate(update UpdateNotice) {
	service.mu.Lock()
	service.desired = update
	service.mu.Unlock()
	service.server().SetDesiredUpdate(update)
}

func (service *Service) handleConnection(ctx context.Context, server *Server, connection PipeConnection) {
	defer service.handlers.Done()
	defer service.removeConnection(connection)
	defer connection.Close()
	if service.Logs != nil {
		defer func() { _ = service.Logs.MaintainViewerLogs() }()
	}

	peer, err := connection.Peer()
	if err != nil || peer.PID == 0 || peer.UserSID == "" {
		return
	}
	connectionID, err := newLeaseID()
	if err != nil {
		return
	}
	state := &serviceConnection{
		connection: connection, id: connectionID,
		commands: make(chan queuedCommand, 8), writerDone: make(chan struct{}),
	}
	go state.runWriter(server.leases)
	service.mu.Lock()
	if service.byID == nil {
		service.byID = make(map[string]*serviceConnection)
	}
	service.byID[connectionID] = state
	service.mu.Unlock()
	defer func() {
		close(state.commands)
		_ = connection.Close()
		<-state.writerDone
		service.mu.Lock()
		delete(service.byID, connectionID)
		service.mu.Unlock()
	}()
	defer server.HandleDisconnect(connectionID)
	reader := bufio.NewReaderSize(connection, MaxManagementMessageBytes+1)
	for {
		request, err := readRequest(reader)
		if err != nil {
			return
		}
		if service.Logs != nil && request.Type == "lease_heartbeat" {
			_ = service.Logs.MaintainViewerLogs()
		}
		response, err := server.Handle(ctx, connectionID, peer, request)
		if err != nil {
			return
		}
		state.writeMu.Lock()
		writeErr := WriteResponse(connection, response)
		state.writeMu.Unlock()
		if writeErr != nil {
			return
		}
	}
}

func (service *Service) addConnection(connection PipeConnection) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.connections == nil {
		service.connections = make(map[PipeConnection]struct{})
	}
	service.connections[connection] = struct{}{}
}

func (service *Service) removeConnection(connection PipeConnection) {
	service.mu.Lock()
	defer service.mu.Unlock()
	delete(service.connections, connection)
}

func (service *Service) closeConnections() {
	service.mu.Lock()
	connections := make([]PipeConnection, 0, len(service.connections))
	for connection := range service.connections {
		connections = append(connections, connection)
	}
	service.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func NewRuntimeService(store ConfigStore) (*Service, error) {
	listener, err := NewPipeListener()
	if err != nil {
		return nil, fmt.Errorf("create viewer service pipe: %w", err)
	}
	logs := NewLogManager()
	return &Service{
		Store: store, Listener: listener, Logs: logs, Control: HTTPControlLoop{InstalledVersion: InstalledVersion},
		Journal: FileCommandJournalStore{Path: DefaultCommandJournalPath},
	}, nil
}
