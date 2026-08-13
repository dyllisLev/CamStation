package viewerservice

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"camstation/internal/opslog"
)

const (
	viewerDiagnosticMaxAttempt    = 32
	viewerDiagnosticMaxDurationMS = 30 * 60 * 1000
	viewerDiagnosticMaxCounter    = 100
)

var (
	viewerDiagnosticEventPattern       = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
	viewerDiagnosticCorrelationPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	viewerDiagnosticSessionPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`)
	viewerDiagnosticCodePattern        = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

func decodeViewerDiagnostic(fields map[string]json.RawMessage) (LogRecord, error) {
	allowed := map[string]bool{
		"leaseId": true, "level": true, "component": true, "event": true,
		"correlationId": true, "sessionId": true, "streamName": true,
		"transport": true, "phase": true, "state": true, "attempt": true,
		"durationMs": true, "attemptElapsedMs": true, "retryMs": true, "readyState": true,
		"reconnectCount": true, "fallbackCount": true, "usingFallback": true,
		"errorCode": true,
	}
	for key := range fields {
		if !allowed[key] {
			return LogRecord{}, fmt.Errorf("%w: unsupported diagnostic field", ErrInvalidRequest)
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic event", ErrInvalidRequest)
	}
	var input struct {
		Level            string `json:"level"`
		Component        string `json:"component"`
		Event            string `json:"event"`
		CorrelationID    string `json:"correlationId"`
		SessionID        string `json:"sessionId"`
		StreamName       string `json:"streamName"`
		Transport        string `json:"transport"`
		Phase            string `json:"phase"`
		State            string `json:"state"`
		Attempt          int    `json:"attempt"`
		DurationMS       int64  `json:"durationMs"`
		AttemptElapsedMS int64  `json:"attemptElapsedMs"`
		RetryMS          int64  `json:"retryMs"`
		ReadyState       int    `json:"readyState"`
		ReconnectCount   int    `json:"reconnectCount"`
		FallbackCount    int    `json:"fallbackCount"`
		UsingFallback    bool   `json:"usingFallback"`
		ErrorCode        string `json:"errorCode"`
	}
	if err := json.Unmarshal(encoded, &input); err != nil {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic event", ErrInvalidRequest)
	}
	level, ok := viewerDiagnosticLevel(input.Level)
	if !ok || !validViewerDiagnosticComponent(input.Component) || !viewerDiagnosticEventPattern.MatchString(input.Event) {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic identity", ErrInvalidRequest)
	}
	if input.CorrelationID != "" && !viewerDiagnosticCorrelationPattern.MatchString(input.CorrelationID) {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic correlation", ErrInvalidRequest)
	}
	if input.SessionID != "" && !viewerDiagnosticSessionPattern.MatchString(input.SessionID) {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic session", ErrInvalidRequest)
	}
	if input.StreamName != "" && !validViewerStreamName(input.StreamName) {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic stream", ErrInvalidRequest)
	}
	if input.Transport != "" && input.Transport != "webrtc" && input.Transport != "mse" {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic transport", ErrInvalidRequest)
	}
	if input.Phase != "" && !validViewerStreamPhase(input.Phase) {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic phase", ErrInvalidRequest)
	}
	if input.State != "" && !viewerDiagnosticCodePattern.MatchString(input.State) {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic state", ErrInvalidRequest)
	}
	if input.ErrorCode != "" && !viewerDiagnosticCodePattern.MatchString(input.ErrorCode) {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic error code", ErrInvalidRequest)
	}
	if input.Attempt < 0 || input.Attempt > viewerDiagnosticMaxAttempt || input.DurationMS < 0 || input.DurationMS > viewerDiagnosticMaxDurationMS ||
		input.AttemptElapsedMS < 0 || input.AttemptElapsedMS > viewerDiagnosticMaxDurationMS ||
		input.RetryMS < 0 || input.RetryMS > viewerDiagnosticMaxDurationMS || input.ReadyState < 0 || input.ReadyState > 4 ||
		input.ReconnectCount < 0 || input.ReconnectCount > viewerDiagnosticMaxCounter ||
		input.FallbackCount < 0 || input.FallbackCount > viewerDiagnosticMaxCounter {
		return LogRecord{}, fmt.Errorf("%w: invalid diagnostic counters", ErrInvalidRequest)
	}
	return LogRecord{
		Level: level, Component: input.Component, Event: input.Event,
		CorrelationID: input.CorrelationID, SessionID: input.SessionID, StreamName: input.StreamName,
		Transport: input.Transport, Phase: input.Phase, State: input.State, Attempt: input.Attempt,
		DurationMS: input.DurationMS, AttemptElapsedMS: input.AttemptElapsedMS,
		RetryMS: input.RetryMS, ReadyState: input.ReadyState,
		ReconnectCount: input.ReconnectCount, FallbackCount: input.FallbackCount,
		UsingFallback: input.UsingFallback, Code: input.ErrorCode,
	}, nil
}

func viewerDiagnosticLevel(value string) (opslog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return opslog.Debug, true
	case "info":
		return opslog.Info, true
	case "warn":
		return opslog.Warn, true
	case "error":
		return opslog.Error, true
	default:
		return 0, false
	}
}

func validViewerDiagnosticComponent(value string) bool {
	switch value {
	case "viewer.main", "viewer.renderer", "viewer.playback", "viewer.control":
		return true
	default:
		return false
	}
}
