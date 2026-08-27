package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"camstation/internal/opslog"
)

const (
	playbackEventMaxBytes       = 4 << 10
	playbackEventDefaultLimit   = 600
	playbackEventDefaultWindow  = time.Minute
	playbackEventMaxElapsedMS   = 30 * 60 * 1000
	playbackEventMaxAttempt     = 32
	playbackEventMaxCounter     = 100
	playbackEventMaxGeneration  = 1_000_000
	playbackEventMaxSessionSize = 64
)

var playbackSessionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,63}$`)

type playbackClientEvent struct {
	SessionID             string `json:"sessionId"`
	DocumentID            string `json:"documentId,omitempty"`
	Surface               string `json:"surface,omitempty"`
	Event                 string `json:"event"`
	StreamName            string `json:"streamName"`
	CandidateRole         string `json:"candidateRole,omitempty"`
	Transport             string `json:"transport"`
	Phase                 string `json:"phase"`
	Attempt               int    `json:"attempt"`
	AttemptGeneration     int    `json:"attemptGeneration,omitempty"`
	ResubscribeGeneration int    `json:"resubscribeGeneration,omitempty"`
	ElapsedMS             int64  `json:"elapsedMs,omitempty"`
	AttemptElapsedMS      int64  `json:"attemptElapsedMs,omitempty"`
	ErrorCategory         string `json:"errorCategory,omitempty"`
	TerminalReason        string `json:"terminalReason,omitempty"`
	ReadyState            int    `json:"readyState"`
	UsingFallback         bool   `json:"usingFallback,omitempty"`
	ReconnectCount        int    `json:"reconnectCount,omitempty"`
	FallbackCount         int    `json:"fallbackCount,omitempty"`
}

type playbackLogRecord struct {
	Timestamp             string `json:"timestamp"`
	Level                 string `json:"level"`
	Event                 string `json:"event"`
	SessionID             string `json:"sessionId"`
	DocumentID            string `json:"documentId,omitempty"`
	Surface               string `json:"surface,omitempty"`
	StreamName            string `json:"streamName"`
	CandidateRole         string `json:"candidateRole,omitempty"`
	Transport             string `json:"transport"`
	Phase                 string `json:"phase"`
	Attempt               int    `json:"attempt"`
	AttemptGeneration     int    `json:"attemptGeneration,omitempty"`
	ResubscribeGeneration int    `json:"resubscribeGeneration,omitempty"`
	ElapsedMS             int64  `json:"elapsedMs"`
	AttemptElapsedMS      int64  `json:"attemptElapsedMs,omitempty"`
	ErrorCategory         string `json:"errorCategory,omitempty"`
	TerminalReason        string `json:"terminalReason,omitempty"`
	ReadyState            int    `json:"readyState"`
	UsingFallback         bool   `json:"usingFallback"`
	ReconnectCount        int    `json:"reconnectCount"`
	FallbackCount         int    `json:"fallbackCount"`
	ClientIP              string `json:"clientIp,omitempty"`
	CameraID              int64  `json:"cameraId,omitempty"`
}

type playbackLogLevel uint8

const (
	playbackDebug playbackLogLevel = iota
	playbackInfo
	playbackWarn
	playbackError
	playbackOff
)

type playbackEventSink struct {
	threshold  playbackLogLevel
	logger     *log.Logger
	operations *opslog.Logger
	now        func() time.Time
}

func newPlaybackEventSinkWithLogger(logger *opslog.Logger) *playbackEventSink {
	return &playbackEventSink{operations: logger, now: time.Now}
}

func newPlaybackEventSink(value string, writer io.Writer) (*playbackEventSink, error) {
	threshold, err := parsePlaybackLogLevel(value)
	if err != nil {
		return nil, err
	}
	if writer == nil {
		writer = io.Discard
	}
	return &playbackEventSink{
		threshold: threshold,
		logger:    log.New(writer, "playback_event ", 0),
		now:       time.Now,
	}, nil
}

func parsePlaybackLogLevel(value string) (playbackLogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return playbackInfo, nil
	case "debug":
		return playbackDebug, nil
	case "warn":
		return playbackWarn, nil
	case "error":
		return playbackError, nil
	case "off":
		return playbackOff, nil
	default:
		return 0, fmt.Errorf("CAMSTATION_PLAYBACK_LOG_LEVEL must be one of off, error, warn, info, or debug")
	}
}

func playbackEventLevel(event string) (playbackLogLevel, string, bool) {
	switch event {
	case "socket_open", "signaling_answer", "first_track", "media_source_open", "mse_ready", "first_media",
		"primary_probe_started", "primary_probe_failed", "session_closed":
		return playbackDebug, "debug", true
	case "attempt_started", "playback_started", "primary_probe_succeeded":
		return playbackInfo, "info", true
	case "attempt_failed":
		return playbackWarn, "warn", true
	case "episode_exhausted", "unsupported":
		return playbackError, "error", true
	default:
		return 0, "", false
	}
}

func (s *playbackEventSink) enabled(event string) bool {
	level, _, ok := playbackEventLevel(event)
	if ok && s.operations != nil {
		return s.operations.Enabled("playback", playbackOperationalLevel(level))
	}
	return ok && s.threshold != playbackOff && level >= s.threshold
}

func (s *playbackEventSink) write(event playbackClientEvent, clientIP string, cameraID int64) {
	if s.operations != nil {
		level, _, _ := playbackEventLevel(event.Event)
		_ = s.operations.Log(playbackOperationalLevel(level), "playback", event.Event, opslog.Fields{
			SessionID:             event.SessionID,
			DocumentID:            event.DocumentID,
			ClientIP:              clientIP,
			CameraID:              cameraID,
			StreamName:            event.StreamName,
			Surface:               event.Surface,
			CandidateRole:         event.CandidateRole,
			Transport:             event.Transport,
			Phase:                 event.Phase,
			Attempt:               event.Attempt,
			AttemptGeneration:     event.AttemptGeneration,
			ResubscribeGeneration: event.ResubscribeGeneration,
			DurationMS:            event.ElapsedMS,
			AttemptElapsedMS:      event.AttemptElapsedMS,
			ErrorCode:             event.ErrorCategory,
			TerminalReason:        event.TerminalReason,
			ReadyState:            event.ReadyState,
			UsingFallback:         event.UsingFallback,
			ReconnectCount:        event.ReconnectCount,
			FallbackCount:         event.FallbackCount,
		})
		return
	}
	_, level, _ := playbackEventLevel(event.Event)
	record := playbackLogRecord{
		Timestamp:             s.now().UTC().Format(time.RFC3339Nano),
		Level:                 level,
		Event:                 event.Event,
		SessionID:             event.SessionID,
		DocumentID:            event.DocumentID,
		Surface:               event.Surface,
		StreamName:            event.StreamName,
		CandidateRole:         event.CandidateRole,
		Transport:             event.Transport,
		Phase:                 event.Phase,
		Attempt:               event.Attempt,
		AttemptGeneration:     event.AttemptGeneration,
		ResubscribeGeneration: event.ResubscribeGeneration,
		ElapsedMS:             event.ElapsedMS,
		AttemptElapsedMS:      event.AttemptElapsedMS,
		ErrorCategory:         event.ErrorCategory,
		TerminalReason:        event.TerminalReason,
		ReadyState:            event.ReadyState,
		UsingFallback:         event.UsingFallback,
		ReconnectCount:        event.ReconnectCount,
		FallbackCount:         event.FallbackCount,
		ClientIP:              clientIP,
		CameraID:              cameraID,
	}
	encoded, err := json.Marshal(record)
	if err == nil {
		s.logger.Print(string(encoded))
	}
}

func playbackOperationalLevel(level playbackLogLevel) opslog.Level {
	switch level {
	case playbackDebug:
		return opslog.Debug
	case playbackInfo:
		return opslog.Info
	case playbackWarn:
		return opslog.Warn
	case playbackError:
		return opslog.Error
	default:
		return opslog.Off
	}
}

type playbackRateWindow struct {
	startedAt time.Time
	count     int
}

type playbackEventRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]playbackRateWindow
	now     func() time.Time
}

func newPlaybackEventRateLimiter(limit int, window time.Duration) *playbackEventRateLimiter {
	if limit <= 0 {
		limit = playbackEventDefaultLimit
	}
	if window <= 0 {
		window = playbackEventDefaultWindow
	}
	return &playbackEventRateLimiter{
		limit: limit, window: window, clients: make(map[string]playbackRateWindow), now: time.Now,
	}
}

func (l *playbackEventRateLimiter) allow(remoteAddr string) bool {
	host := playbackRemoteHost(remoteAddr)
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.clients[host]
	if entry.startedAt.IsZero() || now.Sub(entry.startedAt) >= l.window {
		entry = playbackRateWindow{startedAt: now}
	}
	if entry.count >= l.limit {
		l.clients[host] = entry
		return false
	}
	entry.count++
	l.clients[host] = entry
	if len(l.clients) > 1024 {
		for key, candidate := range l.clients {
			if now.Sub(candidate.startedAt) >= l.window {
				delete(l.clients, key)
			}
		}
	}
	return true
}

func playbackRemoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return host
}

func (d routeDeps) registerPlaybackDiagnosticRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/playback/events", func(w http.ResponseWriter, r *http.Request) {
		if !d.playbackRateLimit.allow(r.RemoteAddr) {
			writeError(w, http.StatusTooManyRequests, fmt.Errorf("playback event rate limit exceeded"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, playbackEventMaxBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var event playbackClientEvent
		if err := decoder.Decode(&event); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid playback event"))
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			writeError(w, http.StatusBadRequest, fmt.Errorf("playback event must be one JSON object"))
			return
		}
		if err := validatePlaybackClientEvent(event); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !d.playbackEvents.enabled(event.Event) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		cameras, err := d.db.ListCameras(r.Context(), false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("validate playback stream"))
			return
		}
		cameraID, registered := registeredPublicCameraID(cameras, event.StreamName)
		if !registered {
			writeError(w, http.StatusNotFound, fmt.Errorf("playback stream is not registered"))
			return
		}
		d.playbackEvents.write(event, playbackRemoteHost(r.RemoteAddr), cameraID)
		w.WriteHeader(http.StatusNoContent)
	})
}

func validatePlaybackClientEvent(event playbackClientEvent) error {
	_, _, knownEvent := playbackEventLevel(event.Event)
	if !knownEvent || !playbackSessionPattern.MatchString(event.SessionID) || len(event.SessionID) > playbackEventMaxSessionSize {
		return fmt.Errorf("invalid playback event identity")
	}
	if event.DocumentID != "" && (!playbackSessionPattern.MatchString(event.DocumentID) || len(event.DocumentID) > playbackEventMaxSessionSize) {
		return fmt.Errorf("invalid playback document identity")
	}
	if !validPlaybackSurface(event.Surface) || !validPlaybackCandidateRole(event.CandidateRole) || !validPlaybackTerminalReason(event.TerminalReason) {
		return fmt.Errorf("invalid playback diagnostic context")
	}
	if event.StreamName == "" || len(event.StreamName) > 128 || event.StreamName != strings.TrimSpace(event.StreamName) || strings.ContainsAny(event.StreamName, "\r\n\t") {
		return fmt.Errorf("invalid playback stream")
	}
	if event.Transport != "webrtc" && event.Transport != "mse" {
		return fmt.Errorf("invalid playback transport")
	}
	switch event.Phase {
	case "connecting", "retrying", "fallback", "recovering", "playing", "stalled", "cooldown", "unsupported":
	default:
		return fmt.Errorf("invalid playback phase")
	}
	if event.Attempt < 1 || event.Attempt > playbackEventMaxAttempt || event.ReadyState < 0 || event.ReadyState > 4 {
		return fmt.Errorf("invalid playback attempt state")
	}
	if event.AttemptGeneration < 0 || event.AttemptGeneration > playbackEventMaxGeneration ||
		event.ResubscribeGeneration < 0 || event.ResubscribeGeneration > playbackEventMaxGeneration {
		return fmt.Errorf("invalid playback generation")
	}
	if event.ElapsedMS < 0 || event.ElapsedMS > playbackEventMaxElapsedMS || event.AttemptElapsedMS < 0 || event.AttemptElapsedMS > playbackEventMaxElapsedMS {
		return fmt.Errorf("invalid playback duration")
	}
	if event.ReconnectCount < 0 || event.ReconnectCount > playbackEventMaxCounter || event.FallbackCount < 0 || event.FallbackCount > playbackEventMaxCounter {
		return fmt.Errorf("invalid playback counters")
	}
	switch event.ErrorCategory {
	case "", "none", "setup_timeout", "media_stall", "socket", "signaling", "media", "unsupported", "episode_exhausted":
	default:
		return fmt.Errorf("invalid playback error category")
	}
	return nil
}

func validPlaybackSurface(value string) bool {
	switch value {
	case "", "official_viewer", "viewer_browser", "operator_live", "viewer_page", "control_room_preview", "camera_profile_preview", "unknown":
		return true
	default:
		return false
	}
}

func validPlaybackCandidateRole(value string) bool {
	switch value {
	case "", "primary", "fallback", "primary_probe":
		return true
	default:
		return false
	}
}

func validPlaybackTerminalReason(value string) bool {
	switch value {
	case "", "setup_timeout", "media_stall", "socket", "signaling", "media", "unsupported",
		"retry_budget_exhausted", "primary_restored", "resubscribe_requested", "candidates_changed",
		"transport_changed", "surface_changed", "component_unmounted":
		return true
	default:
		return false
	}
}

func defaultPlaybackEventSink() (*playbackEventSink, error) {
	return newPlaybackEventSink(os.Getenv("CAMSTATION_PLAYBACK_LOG_LEVEL"), os.Stdout)
}
