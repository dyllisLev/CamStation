//go:build windows

package viewerservice

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const viewerPipeSDDL = "D:P(D;;GA;;;NU)(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;IU)"

var getNamedPipeClientSessionID = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetNamedPipeClientSessionId")

type windowsPipeListener struct {
	name       *uint16
	descriptor *windows.SECURITY_DESCRIPTOR
	mu         sync.Mutex
	closed     bool
	pending    windows.Handle
	ready      chan struct{}
	readyOnce  sync.Once
}

type windowsPipeConnection struct {
	*os.File
	handle windows.Handle
}

func NewPipeListener() (PipeListener, error) {
	name, err := windows.UTF16PtrFromString(ViewerServicePipeName)
	if err != nil {
		return nil, fmt.Errorf("encode pipe name: %w", err)
	}
	descriptor, err := windows.SecurityDescriptorFromString(viewerPipeSDDL)
	if err != nil {
		return nil, fmt.Errorf("build pipe security descriptor: %w", err)
	}
	return &windowsPipeListener{name: name, descriptor: descriptor, ready: make(chan struct{})}, nil
}

func (listener *windowsPipeListener) Accept() (PipeConnection, error) {
	listener.mu.Lock()
	if listener.closed {
		listener.mu.Unlock()
		return nil, ErrListenerClosed
	}
	listener.mu.Unlock()

	attributes := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: listener.descriptor,
	}
	handle, err := windows.CreateNamedPipe(
		listener.name,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		windows.PIPE_UNLIMITED_INSTANCES,
		MaxManagementMessageBytes,
		MaxManagementMessageBytes,
		0,
		&attributes,
	)
	runtime.KeepAlive(listener.descriptor)
	if err != nil {
		return nil, fmt.Errorf("create named pipe: %w", err)
	}
	connected := false
	defer func() {
		if !connected {
			_ = windows.CloseHandle(handle)
		}
	}()

	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("create pipe connection event: %w", err)
	}
	defer windows.CloseHandle(event)
	overlapped := windows.Overlapped{HEvent: event}

	listener.mu.Lock()
	if listener.closed {
		listener.mu.Unlock()
		return nil, ErrListenerClosed
	}
	listener.pending = handle
	err = windows.ConnectNamedPipe(handle, &overlapped)
	listener.mu.Unlock()
	defer listener.clearPending(handle)

	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		listener.readyOnce.Do(func() { close(listener.ready) })
	case errors.Is(err, windows.ERROR_IO_PENDING):
		listener.readyOnce.Do(func() { close(listener.ready) })
		if _, err := windows.WaitForSingleObject(event, windows.INFINITE); err != nil {
			return nil, fmt.Errorf("wait for named pipe client: %w", err)
		}
		var transferred uint32
		if err := windows.GetOverlappedResult(handle, &overlapped, &transferred, false); err != nil {
			if listener.isClosed() && (errors.Is(err, windows.ERROR_OPERATION_ABORTED) || errors.Is(err, windows.ERROR_INVALID_HANDLE)) {
				return nil, ErrListenerClosed
			}
			return nil, fmt.Errorf("connect named pipe: %w", err)
		}
	default:
		return nil, fmt.Errorf("connect named pipe: %w", err)
	}
	if listener.isClosed() {
		return nil, ErrListenerClosed
	}
	connected = true
	return &windowsPipeConnection{File: os.NewFile(uintptr(handle), ViewerServicePipeName), handle: handle}, nil
}

func (listener *windowsPipeListener) Ready() <-chan struct{} {
	return listener.ready
}

func (listener *windowsPipeListener) Close() error {
	listener.mu.Lock()
	if listener.closed {
		listener.mu.Unlock()
		return nil
	}
	listener.closed = true
	pending := listener.pending
	listener.mu.Unlock()
	if pending != 0 {
		if err := windows.CancelIoEx(pending, nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) && !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			return fmt.Errorf("cancel named pipe accept: %w", err)
		}
	}
	return nil
}

func (listener *windowsPipeListener) clearPending(handle windows.Handle) {
	listener.mu.Lock()
	defer listener.mu.Unlock()
	if listener.pending == handle {
		listener.pending = 0
	}
}

func (listener *windowsPipeListener) isClosed() bool {
	listener.mu.Lock()
	defer listener.mu.Unlock()
	return listener.closed
}

func (connection *windowsPipeConnection) Peer() (Peer, error) {
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(connection.handle, &pid); err != nil || pid == 0 {
		return Peer{}, fmt.Errorf("%w: named pipe client PID: %v", ErrPeerIdentity, err)
	}
	pipeSession, err := namedPipeClientSessionID(connection.handle)
	if err != nil {
		return Peer{}, fmt.Errorf("%w: named pipe client session: %v", ErrPeerIdentity, err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return Peer{}, fmt.Errorf("%w: open client process: %v", ErrPeerIdentity, err)
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return Peer{}, fmt.Errorf("%w: open client token: %v", ErrPeerIdentity, err)
	}
	defer token.Close()
	tokenSession, err := tokenSessionID(token)
	if err != nil || tokenSession != pipeSession {
		return Peer{}, fmt.Errorf("%w: pipe and token sessions do not match", ErrPeerIdentity)
	}
	user, err := token.GetTokenUser()
	if err != nil || user.User.Sid == nil {
		return Peer{}, fmt.Errorf("%w: read client user SID: %v", ErrPeerIdentity, err)
	}
	tokenInteractive, err := tokenHasInteractiveGroup(token)
	if err != nil {
		return Peer{}, fmt.Errorf("%w: read client token groups: %v", ErrPeerIdentity, err)
	}
	sessionActive, err := activeUserSession(tokenSession)
	if err != nil {
		return Peer{}, fmt.Errorf("%w: query client session: %v", ErrPeerIdentity, err)
	}
	interactive := isInteractivePeer(tokenSession, tokenInteractive, sessionActive)
	return Peer{PID: pid, SessionID: tokenSession, UserSID: user.User.Sid.String(), Interactive: interactive}, nil
}

func activeUserSession(sessionID uint32) (bool, error) {
	var sessions *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &sessions, &count); err != nil {
		return false, err
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))
	for _, session := range unsafe.Slice(sessions, count) {
		if session.SessionID == sessionID {
			return session.State == windows.WTSActive, nil
		}
	}
	return false, nil
}

func namedPipeClientSessionID(handle windows.Handle) (uint32, error) {
	if err := getNamedPipeClientSessionID.Find(); err != nil {
		return 0, err
	}
	var sessionID uint32
	result, _, callErr := getNamedPipeClientSessionID.Call(uintptr(handle), uintptr(unsafe.Pointer(&sessionID)))
	if result == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return 0, callErr
		}
		return 0, windows.ERROR_INVALID_DATA
	}
	return sessionID, nil
}

func tokenSessionID(token windows.Token) (uint32, error) {
	var sessionID uint32
	var returned uint32
	err := windows.GetTokenInformation(
		token,
		windows.TokenSessionId,
		(*byte)(unsafe.Pointer(&sessionID)),
		uint32(unsafe.Sizeof(sessionID)),
		&returned,
	)
	if err != nil {
		return 0, err
	}
	if returned != uint32(unsafe.Sizeof(sessionID)) {
		return 0, windows.ERROR_INVALID_DATA
	}
	return sessionID, nil
}

func tokenHasInteractiveGroup(token windows.Token) (bool, error) {
	interactiveSID, err := windows.CreateWellKnownSid(windows.WinInteractiveSid)
	if err != nil {
		return false, err
	}
	groups, err := token.GetTokenGroups()
	if err != nil {
		return false, err
	}
	for _, group := range groups.AllGroups() {
		if group.Sid != nil && group.Sid.Equals(interactiveSID) && group.Attributes&windows.SE_GROUP_ENABLED != 0 && group.Attributes&windows.SE_GROUP_USE_FOR_DENY_ONLY == 0 {
			return true, nil
		}
	}
	return false, nil
}
