package viewerservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
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
	State       string `json:"state"`
	Version     string `json:"version,omitempty"`
	Filename    string `json:"filename,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	CommandID   int64  `json:"commandId,omitempty"`
	Generation  int64  `json:"generation,omitempty"`
}

type StatusSnapshot struct {
	Configured              bool                `json:"configured"`
	Config                  *PublicConfig       `json:"config,omitempty"`
	Connection              string              `json:"connection"`
	ControlLastSuccessAt    *time.Time          `json:"controlLastSuccessAt,omitempty"`
	Viewer                  string              `json:"viewer"`
	ViewerLastHeartbeatAt   *time.Time          `json:"viewerLastHeartbeatAt,omitempty"`
	Renderer                string              `json:"renderer"`
	RendererLastHeartbeatAt *time.Time          `json:"rendererLastHeartbeatAt,omitempty"`
	RendererLastProgressAt  *time.Time          `json:"rendererLastProgressAt,omitempty"`
	Streams                 []ViewerStreamState `json:"streams,omitempty"`
	Installed               string              `json:"installedVersion"`
	Update                  UpdateSnapshot      `json:"update"`
	AutoStart               bool                `json:"autoStart"`
	LeaseAvailable          bool                `json:"leaseAvailable"`
}

type LeaseGrant struct {
	LeaseID          string `json:"leaseId"`
	HeartbeatSeconds int    `json:"heartbeatSeconds"`
	LogPath          string `json:"logPath,omitempty"`
}

type LocalCommandResult struct {
	LeaseID      string `json:"leaseId"`
	OperationKey string `json:"operationKey"`
	Succeeded    bool   `json:"succeeded"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

type Server struct {
	config           ConfigManager
	leases           *LeaseManager
	installedVersion string
	logError         func(context.Context, error) string
	leaseLogAssigner func(Peer) (string, error)
	commandResult    func(LocalCommandResult) error

	mu                      sync.Mutex
	connection              string
	controlLastSuccessAt    *time.Time
	viewer                  string
	viewerLastHeartbeatAt   *time.Time
	renderer                string
	rendererLastHeartbeatAt *time.Time
	rendererLastProgressAt  *time.Time
	streams                 []ViewerStreamState
	update                  UpdateSnapshot
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

func (server *Server) SetCommandResultHandler(handler func(LocalCommandResult) error) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.commandResult = handler
}

func (server *Server) SetConnection(state string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.connection = state
	if state == "online" {
		now := time.Now().UTC()
		server.controlLastSuccessAt = &now
	}
}

func (server *Server) SetDesiredUpdate(update UpdateNotice) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.update = UpdateSnapshot{
		State: "idle", Version: update.Version, Filename: update.Filename,
		SHA256: update.SHA256, DownloadURL: update.DownloadURL, SizeBytes: update.SizeBytes,
		CommandID: update.CommandID, Generation: update.Generation,
	}
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
		server.markViewerHeartbeat()
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
	case "command_result":
		var result LocalCommandResult
		if err := decodePayload(request.Payload, &result); err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		if err := server.leases.Authorize(connectionID, result.LeaseID, peer); err != nil {
			return Response{}, fmt.Errorf("%w: %v", ErrPeerIdentity, err)
		}
		if !validLocalCommandResult(result) {
			return server.errorResponse(ctx, request, fmt.Errorf("%w: invalid command result", ErrInvalidRequest)), nil
		}
		server.mu.Lock()
		handler := server.commandResult
		server.mu.Unlock()
		if handler == nil {
			return server.errorResponse(ctx, request, ErrUnsupportedRequest), nil
		}
		if err := handler(result); err != nil {
			return server.errorResponse(ctx, request, err), nil
		}
		return successResponse(request, nil), nil
	default:
		return server.errorResponse(ctx, request, ErrUnsupportedRequest), nil
	}
}

func validLocalCommandResult(result LocalCommandResult) bool {
	if strings.TrimSpace(result.LeaseID) == "" || strings.TrimSpace(result.OperationKey) == "" || len(result.OperationKey) > 128 {
		return false
	}
	if result.Succeeded {
		return result.ErrorCode == ""
	}
	switch result.ErrorCode {
	case "renderer_failed", "viewer_command_failed", "viewer_relaunch_failed":
		return true
	default:
		return false
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
	status.ControlLastSuccessAt = server.controlLastSuccessAt
	status.ViewerLastHeartbeatAt = server.viewerLastHeartbeatAt
	status.RendererLastHeartbeatAt = server.rendererLastHeartbeatAt
	status.RendererLastProgressAt = server.rendererLastProgressAt
	status.Streams = append([]ViewerStreamState(nil), server.streams...)
	status.Update = server.update
	return status, nil
}

// Snapshot exposes lease-backed telemetry to the service control loop while
// preserving storage errors for callers that need to report them.
func (server *Server) Snapshot(ctx context.Context) (StatusSnapshot, error) {
	return server.status(ctx)
}

func (server *Server) recordReport(requestType string, payload map[string]json.RawMessage) error {
	if requestType == "diagnostic_event" {
		return nil
	}
	if requestType == "stream_telemetry" {
		stream, err := decodeViewerStreamTelemetry(payload, time.Now().UTC())
		if err != nil {
			return err
		}
		server.storeViewerTelemetry(stream)
		return nil
	}
	if requestType != "viewer_status" && requestType != "renderer_status" {
		return fmt.Errorf("%w: unsupported report type", ErrInvalidRequest)
	}
	var state string
	if err := json.Unmarshal(payload["state"], &state); err != nil || !validReportedState(requestType, state) {
		return fmt.Errorf("%w: invalid %s state", ErrInvalidRequest, requestType)
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	now := time.Now().UTC()
	server.viewerLastHeartbeatAt = &now
	if requestType == "viewer_status" {
		server.viewer = state
	} else {
		server.renderer = state
		server.rendererLastHeartbeatAt = &now
	}
	return nil
}

func decodeViewerStreamTelemetry(payload map[string]json.RawMessage, now time.Time) (ViewerStreamState, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ViewerStreamState{}, fmt.Errorf("%w: invalid stream telemetry", ErrInvalidRequest)
	}
	var input struct {
		StreamName     string `json:"streamName"`
		Transport      string `json:"transport"`
		Phase          string `json:"phase"`
		LastBinaryAt   int64  `json:"lastBinaryAt"`
		LastProgressAt int64  `json:"lastProgressAt"`
	}
	if err := json.Unmarshal(encoded, &input); err != nil || !validViewerStreamName(input.StreamName) ||
		(input.Transport != "webrtc" && input.Transport != "mse") || !validViewerStreamPhase(input.Phase) {
		return ViewerStreamState{}, fmt.Errorf("%w: invalid stream telemetry", ErrInvalidRequest)
	}
	stream := ViewerStreamState{
		StreamName: input.StreamName, State: input.Phase, Transport: input.Transport, UpdatedAt: timePointer(now),
	}
	stream.LastBinaryAt = viewerTelemetryTime(input.LastBinaryAt)
	stream.LastProgressAt = viewerTelemetryTime(input.LastProgressAt)
	return stream, nil
}

func (server *Server) storeViewerTelemetry(stream ViewerStreamState) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.viewerLastHeartbeatAt = stream.UpdatedAt
	server.rendererLastHeartbeatAt = stream.UpdatedAt
	if stream.LastProgressAt != nil &&
		(server.rendererLastProgressAt == nil || stream.LastProgressAt.After(*server.rendererLastProgressAt)) {
		server.rendererLastProgressAt = stream.LastProgressAt
	}
	for index := range server.streams {
		if server.streams[index].StreamName == stream.StreamName {
			server.streams[index] = stream
			return
		}
	}
	if len(server.streams) >= 64 {
		copy(server.streams, server.streams[1:])
		server.streams = server.streams[:63]
	}
	server.streams = append(server.streams, stream)
}

func (server *Server) markViewerHeartbeat() {
	server.mu.Lock()
	defer server.mu.Unlock()
	now := time.Now().UTC()
	server.viewerLastHeartbeatAt = &now
}

func viewerTelemetryTime(milliseconds int64) *time.Time {
	if milliseconds <= 0 {
		return nil
	}
	return timePointer(time.UnixMilli(milliseconds).UTC())
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func validViewerStreamName(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) || strings.HasPrefix(value, "//") ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == ""
}

func validViewerStreamPhase(value string) bool {
	switch value {
	case "connecting", "retrying", "fallback", "recovering", "playing", "stalled", "cooldown", "unsupported":
		return true
	default:
		return false
	}
}

func (server *Server) setViewerState(viewer, renderer string) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.viewer = viewer
	server.renderer = renderer
	server.streams = nil
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
