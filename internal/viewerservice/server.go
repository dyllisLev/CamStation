package viewerservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

const (
	CodeInvalidRequest     = "invalid_request"
	CodeUnsupportedRequest = "unsupported_request"
	CodeLeaseBusy          = "lease_busy"
	CodeLeaseFailed        = "lease_failed"
	LeaseHeartbeatSeconds  = 5
)

var (
	ErrInvalidRequest     = errors.New(CodeInvalidRequest)
	ErrUnsupportedRequest = errors.New(CodeUnsupportedRequest)
)

type PublicConfig struct {
	ServerURL   string `json:"serverUrl"`
	DisplayName string `json:"displayName"`
}

type UpdateSnapshot struct {
	State string `json:"state"`
}

type StatusSnapshot struct {
	Configured     bool           `json:"configured"`
	Config         *PublicConfig  `json:"config,omitempty"`
	Connection     string         `json:"connection"`
	Viewer         string         `json:"viewer"`
	Renderer       string         `json:"renderer"`
	Installed      string         `json:"installedVersion"`
	Update         UpdateSnapshot `json:"update"`
	AutoStart      bool           `json:"autoStart"`
	LeaseAvailable bool           `json:"leaseAvailable"`
}

type LeaseGrant struct {
	LeaseID          string `json:"leaseId"`
	HeartbeatSeconds int    `json:"heartbeatSeconds"`
	LogPath          string `json:"logPath,omitempty"`
}

type Server struct {
	config           ConfigManager
	leases           *LeaseManager
	installedVersion string
	logError         func(context.Context, error) string
	leaseLogAssigner func(Peer) (string, error)

	mu         sync.Mutex
	connection string
	viewer     string
	renderer   string
	update     UpdateSnapshot
}

func NewServer(config ConfigManager, leases *LeaseManager, installedVersion string, logError func(context.Context, error) string) *Server {
	return &Server{
		config:           config,
		leases:           leases,
		installedVersion: installedVersion,
		logError:         logError,
		viewer:           "closed",
		renderer:         "not_ready",
		update:           UpdateSnapshot{State: "idle"},
	}
}

func (server *Server) SetLeaseLogAssigner(assigner func(Peer) (string, error)) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.leaseLogAssigner = assigner
}

func (server *Server) Handle(ctx context.Context, connectionID string, peer Peer, request Request) (Response, error) {
	if err := validateRequest(request); err != nil {
		return Response{}, err
	}
	if connectionID == "" || peer.PID == 0 {
		return Response{}, ErrPeerIdentity
	}

	switch request.Type {
	case "get_status":
		return server.statusResponse(ctx, request)
	case "configure":
		if !peer.Interactive {
			return Response{}, ErrPeerIdentity
		}
		var draft ConfigDraft
		if err := decodePayload(request.Payload, &draft); err != nil {
			return server.errorResponse(ctx, request, fmt.Errorf("%w: configure payload", ErrInvalidRequest)), nil
		}
		if _, err := server.config.Commit(ctx, draft); err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		return server.statusResponse(ctx, request)
	case "acquire_lease":
		if !peer.Interactive {
			return Response{}, ErrPeerIdentity
		}
		lease, err := server.leases.Acquire(connectionID, peer)
		if err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		server.mu.Lock()
		assignLog := server.leaseLogAssigner
		server.mu.Unlock()
		var logPath string
		if assignLog != nil {
			logPath, err = assignLog(peer)
			if err != nil {
				_ = server.leases.Release(connectionID, lease.ID, peer)
				return server.errorResponse(ctx, request, fmt.Errorf("%w: %v", ErrLoggingUnavailable, err)), nil
			}
		}
		return successResponse(request, LeaseGrant{LeaseID: lease.ID, HeartbeatSeconds: LeaseHeartbeatSeconds, LogPath: logPath}), nil
	case "lease_heartbeat":
		leaseID, _, err := decodeLeasePayload(request.Payload)
		if err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		if err := server.leases.Refresh(connectionID, leaseID, peer); err != nil {
			return Response{}, fmt.Errorf("%w: %v", ErrPeerIdentity, err)
		}
		return successResponse(request, nil), nil
	case "release_lease":
		leaseID, _, err := decodeLeasePayload(request.Payload)
		if err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		if err := server.leases.Release(connectionID, leaseID, peer); err != nil {
			return Response{}, fmt.Errorf("%w: %v", ErrPeerIdentity, err)
		}
		server.setViewerState("closed", "not_ready")
		return successResponse(request, nil), nil
	case "viewer_status", "renderer_status", "stream_telemetry", "diagnostic_event":
		leaseID, payload, err := decodeLeasePayload(request.Payload)
		if err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		if err := server.leases.Authorize(connectionID, leaseID, peer); err != nil {
			return Response{}, fmt.Errorf("%w: %v", ErrPeerIdentity, err)
		}
		if err := server.recordReport(request.Type, payload); err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		return successResponse(request, nil), nil
	default:
		return server.errorResponse(ctx, request, ErrUnsupportedRequest), nil
	}
}

func (server *Server) HandleDisconnect(connectionID string) {
	if server.leases != nil && server.leases.ReleaseConnection(connectionID) {
		server.setViewerState("closed", "not_ready")
	}
}

func (server *Server) statusResponse(ctx context.Context, request Request) (Response, error) {
	status, err := server.status(ctx)
	if err != nil {
		return server.errorResponse(ctx, request, err), nil
	}
	return successResponse(request, status), nil
}

func (server *Server) status(ctx context.Context) (StatusSnapshot, error) {
	status := StatusSnapshot{Installed: server.installedVersion, LeaseAvailable: server.leases != nil && server.leases.Available()}
	config, err := loadOrEmpty(ctx, server.config.Store)
	if err != nil {
		return StatusSnapshot{}, storageError(err)
	}
	if config.SchemaVersion != 0 {
		status.Configured = true
		status.Config = &PublicConfig{ServerURL: config.ServerURL, DisplayName: config.DisplayName}
		status.AutoStart = config.AutoStart
	}

	server.mu.Lock()
	defer server.mu.Unlock()
	status.Connection = server.connection
	if status.Connection == "" {
		if status.Configured {
			status.Connection = "connecting"
		} else {
			status.Connection = "unconfigured"
		}
	}
	status.Viewer = server.viewer
	status.Renderer = server.renderer
	status.Update = server.update
	return status, nil
}

func (server *Server) recordReport(requestType string, payload map[string]json.RawMessage) error {
	if requestType != "viewer_status" && requestType != "renderer_status" {
		return nil
	}
	var state string
	if err := json.Unmarshal(payload["state"], &state); err != nil || !validReportedState(requestType, state) {
		return fmt.Errorf("%w: invalid %s state", ErrInvalidRequest, requestType)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if requestType == "viewer_status" {
		server.viewer = state
	} else {
		server.renderer = state
	}
	return nil
}

func (server *Server) setViewerState(viewer, renderer string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.viewer = viewer
	server.renderer = renderer
}

func (server *Server) errorResponse(ctx context.Context, request Request, err error) Response {
	code := ErrorCode(err)
	if code == "" {
		switch {
		case errors.Is(err, ErrInvalidRequest):
			code = CodeInvalidRequest
		case errors.Is(err, ErrUnsupportedRequest):
			code = CodeUnsupportedRequest
		case errors.Is(err, ErrLeaseBusy):
			code = CodeLeaseBusy
		case errors.Is(err, ErrLoggingUnavailable):
			code = CodeLoggingUnavailable
		default:
			code = CodeLeaseFailed
		}
	}
	message := safeErrorMessage(code)
	if server.logError != nil {
		if correlationID := strings.TrimSpace(server.logError(ctx, err)); correlationID != "" {
			message += " (참조: " + correlationID + ")"
		}
	}
	return Response{Version: PipeProtocolVersion, RequestID: request.RequestID, ErrorCode: code, Message: message}
}

func successResponse(request Request, payload any) Response {
	response := Response{Version: PipeProtocolVersion, RequestID: request.RequestID, OK: true}
	if payload != nil {
		response.Payload, _ = json.Marshal(payload)
	}
	return response
}

func decodePayload(payload json.RawMessage, target any) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: payload is required", ErrInvalidRequest)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode payload: %v", ErrInvalidRequest, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing payload JSON", ErrInvalidRequest)
	}
	return nil
}

func decodeLeasePayload(payload json.RawMessage) (string, map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := decodePayload(payload, &fields); err != nil {
		return "", nil, err
	}
	var leaseID string
	if err := json.Unmarshal(fields["leaseId"], &leaseID); err != nil || strings.TrimSpace(leaseID) == "" {
		return "", nil, fmt.Errorf("%w: lease ID is required", ErrInvalidRequest)
	}
	return leaseID, fields, nil
}

func validReportedState(requestType, state string) bool {
	allowed := map[string]map[string]bool{
		"viewer_status":   {"starting": true, "running": true, "closed": true, "failed": true},
		"renderer_status": {"not_ready": true, "ready": true, "unresponsive": true, "failed": true},
	}
	return allowed[requestType][state]
}

func safeErrorMessage(code string) string {
	switch code {
	case CodeInvalidInput:
		return "입력값을 확인해 주세요."
	case CodeServerUnreachable:
		return "서버에 연결할 수 없습니다."
	case CodeAPIIncompatible:
		return "서버 버전이 호환되지 않습니다."
	case CodeRegistrationRejected:
		return "Viewer 등록이 거부되었습니다."
	case CodeStorageFailed:
		return "설정을 저장할 수 없습니다."
	case CodeLeaseBusy:
		return "다른 사용자 세션에서 Viewer가 실행 중입니다."
	case CodeUnsupportedRequest:
		return "지원하지 않는 요청입니다."
	case CodeInvalidRequest:
		return "요청 내용을 확인해 주세요."
	case CodeLoggingUnavailable:
		return "Viewer 로그를 준비할 수 없습니다. 잠시 후 다시 시도해 주세요."
	default:
		return "요청을 처리할 수 없습니다."
	}
}
