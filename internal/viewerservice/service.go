package viewerservice

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var ErrListenerClosed = errors.New("viewer service listener is closed")

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
	Store    ConfigStore
	Listener PipeListener
	Server   *Server
	Logs     *LogManager

	mu          sync.Mutex
	connections map[PipeConnection]struct{}
	handlers    sync.WaitGroup
}

func (service *Service) Run(ctx context.Context) error {
	if service.Listener == nil {
		return errors.New("viewer service listener is unavailable")
	}
	runCtx, cancel := context.WithCancel(ctx)
	server := service.server()
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
	manager := ConfigManager{Store: service.Store, NewID: newLeaseID}
	var logError func(context.Context, error) string
	if service.Logs != nil {
		logError = service.Logs.ErrorLogger
	}
	service.Server = NewServer(manager, NewLeaseManager(time.Now, 15*time.Second), "", logError)
	if service.Logs != nil {
		service.Server.SetLeaseLogAssigner(service.Logs.AssignViewerLog)
	}
	return service.Server
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
		if err := WriteResponse(connection, response); err != nil {
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
	return &Service{Store: store, Listener: listener, Logs: logs}, nil
}
