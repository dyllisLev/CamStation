//go:build windows

package vieweragent

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func ServeViewerPipe(ctx context.Context, config Config, handler func(PipeMessage) (PipeMessage, error), ready func()) error {
	if handler == nil {
		return errors.New("pipe handler is required")
	}
	if ready == nil {
		return errors.New("pipe ready callback is required")
	}
	listener, err := winio.ListenPipe(ViewerPipeName, &winio.PipeConfig{
		SecurityDescriptor: pipeSecurityDescriptor(config),
		MessageMode:        false,
		InputBufferSize:    MaxPipeMessageBytes,
		OutputBufferSize:   MaxPipeMessageBytes,
	})
	if err != nil {
		return err
	}
	defer listener.Close()
	ready()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	active := make(chan struct{}, 1)
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil {
				return nil
			}
			return acceptErr
		}
		select {
		case active <- struct{}{}:
			processID, sessionID, identityErr := pipeClientIdentity(connection)
			if identityErr != nil {
				<-active
				_ = connection.Close()
				continue
			}
			go func(conn net.Conn, pid, session uint32) {
				defer func() { <-active }()
				servePipeConnection(conn, pid, session, handler)
			}(connection, processID, sessionID)
		default:
			_ = connection.Close()
		}
	}
}

func servePipeConnection(connection net.Conn, processID, sessionID uint32, handler func(PipeMessage) (PipeMessage, error)) {
	defer connection.Close()
	reader := bufio.NewReaderSize(connection, MaxPipeMessageBytes+1)
	for {
		_ = connection.SetReadDeadline(time.Now().Add(15 * time.Second))
		request, err := readPipeMessage(reader)
		if err != nil {
			return
		}
		request, err = applyPipeIdentity(request, processID, sessionID, windows.WTSGetActiveConsoleSessionId())
		if err != nil {
			return
		}
		response, err := handler(request)
		if err != nil {
			return
		}
		if response.Version == 0 {
			response.Version = PipeProtocolVersion
		}
		if response.RequestID == "" {
			response.RequestID = request.RequestID
		}
		_ = connection.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := WritePipeMessage(connection, response); err != nil {
			return
		}
	}
}

func pipeClientIdentity(connection net.Conn) (uint32, uint32, error) {
	file, ok := connection.(interface{ Fd() uintptr })
	if !ok {
		return 0, 0, errors.New("named pipe handle is unavailable")
	}
	var processID, sessionID uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(file.Fd()), &processID); err != nil {
		return 0, 0, err
	}
	if err := windows.ProcessIdToSessionId(processID, &sessionID); err != nil {
		return 0, 0, err
	}
	return processID, sessionID, nil
}

func pipeSecurityDescriptor(config Config) string {
	descriptor := "D:P(A;;GA;;;SY)"
	for _, sid := range []string{config.AgentServiceSID, config.MonitoringUserSID} {
		if validSID(sid) {
			descriptor += "(A;;GRGW;;;" + sid + ")"
		}
	}
	return descriptor
}

func validSID(sid string) bool {
	if !strings.HasPrefix(sid, "S-") || len(sid) > 184 {
		return false
	}
	for _, char := range sid[2:] {
		if char != '-' && !unicode.IsDigit(char) {
			return false
		}
	}
	return true
}
