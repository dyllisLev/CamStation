package opslog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"camstation/internal/store"
)

const (
	MaxRecordBytes         = 4 << 10
	DefaultMaxBytes        = 25 << 20
	DefaultRetainedFiles   = 8
	DefaultFilename        = "camstationd.jsonl"
	fileFailureReportEvery = time.Minute
)

type Level uint8

const (
	Debug Level = iota + 1
	Info
	Warn
	Error
	Off
)

func (level Level) String() string {
	switch level {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	case Off:
		return "off"
	default:
		return "unknown"
	}
}

type Config struct {
	Level               string
	ComponentLevels     string
	LegacyPlaybackLevel string
	Writer              io.Writer
	FilePath            string
	MaxBytes            int64
	RetainedFiles       int
}

type Fields struct {
	CorrelationID    string
	SessionID        string
	ViewerID         string
	ClientIP         string
	CameraID         int64
	StreamName       string
	Filename         string
	Transport        string
	Phase            string
	State            string
	Attempt          int
	DurationMS       int64
	AttemptElapsedMS int64
	RetryMS          int64
	Frame            int64
	MediaTimeMS      int64
	SizeBytes        int64
	ReadyState       int
	ReconnectCount   int
	FallbackCount    int
	UsingFallback    bool
	ErrorCode        string
	Message          string
}

type Record struct {
	Timestamp        string `json:"timestamp"`
	Level            string `json:"level"`
	Component        string `json:"component"`
	Event            string `json:"event"`
	CorrelationID    string `json:"correlationId,omitempty"`
	SessionID        string `json:"sessionId,omitempty"`
	ViewerID         string `json:"viewerId,omitempty"`
	ClientIP         string `json:"clientIp,omitempty"`
	CameraID         int64  `json:"cameraId,omitempty"`
	StreamName       string `json:"streamName,omitempty"`
	Filename         string `json:"filename,omitempty"`
	Transport        string `json:"transport,omitempty"`
	Phase            string `json:"phase,omitempty"`
	State            string `json:"state,omitempty"`
	Attempt          int    `json:"attempt,omitempty"`
	DurationMS       int64  `json:"durationMs,omitempty"`
	AttemptElapsedMS int64  `json:"attemptElapsedMs,omitempty"`
	RetryMS          int64  `json:"retryMs,omitempty"`
	Frame            int64  `json:"frame,omitempty"`
	MediaTimeMS      int64  `json:"mediaTimeMs,omitempty"`
	SizeBytes        int64  `json:"sizeBytes,omitempty"`
	ReadyState       int    `json:"readyState,omitempty"`
	ReconnectCount   int    `json:"reconnectCount,omitempty"`
	FallbackCount    int    `json:"fallbackCount,omitempty"`
	UsingFallback    bool   `json:"usingFallback,omitempty"`
	ErrorCode        string `json:"errorCode,omitempty"`
	Message          string `json:"message,omitempty"`
}

type Logger struct {
	policy *Policy
	writer io.Writer
	file   *rotatingWriter
	now    func() time.Time
	mu     sync.Mutex
	closed bool

	fileUnavailable       bool
	lastFileFailureReport time.Time
}

type Policy struct {
	threshold Level
	overrides map[string]Level
}

var (
	urlPattern       = regexp.MustCompile(`(?i)\b(?:https?|rtsp|rtsps)://[^\s"'<>]+`)
	pemPattern       = regexp.MustCompile(`(?is)-----BEGIN [^-\r\n]+-----.*?-----END [^-\r\n]+-----`)
	sdpPattern       = regexp.MustCompile(`(?i)\b(?:offer|answer|sdp)\s*[:=]\s*[^\r\n]+`)
	iceCandidatePat  = regexp.MustCompile(`(?i)\bcandidate:[^\r\n]+`)
	iceCredentialPat = regexp.MustCompile(`(?i)\bice-(?:ufrag|pwd)\s*[:=]\s*[^\s,;]+`)
	secretKVPattern  = regexp.MustCompile(`(?i)\b(password|secret|token|nonce|api[_-]?key)\s*[:=]\s*[^\s,;]+`)
	authorizationPat = regexp.MustCompile(`(?is)\b(proxy[-_]?authorization|authorization)\s*[:=].*`)
	quotedPathPat    = regexp.MustCompile(`(?i)(?:"(?:[a-z]:\\|/)[^"\r\n]*"|'(?:[a-z]:\\|/)[^'\r\n]*')`)
	windowsPathPat   = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s"'<>]+`)
	posixPathPat     = regexp.MustCompile(`(^|[\s=:,(])/[^\s"'<>),;]+`)
)

func New(config Config) (*Logger, error) {
	policy, err := newPolicy(config.Level, config.ComponentLevels, config.LegacyPlaybackLevel)
	if err != nil {
		return nil, err
	}

	logger := &Logger{policy: policy, writer: config.Writer, now: time.Now}
	if logger.writer == nil {
		logger.writer = io.Discard
	}
	if strings.TrimSpace(config.FilePath) != "" {
		if config.MaxBytes <= 0 {
			return nil, errors.New("log max bytes must be positive when file logging is enabled")
		}
		if config.RetainedFiles < 1 {
			return nil, errors.New("retained log files must be positive when file logging is enabled")
		}
		logger.file, err = newRotatingWriter(config.FilePath, config.MaxBytes, config.RetainedFiles)
		if err != nil {
			return nil, err
		}
	}
	return logger, nil
}

func NewFromEnvironment(stdout io.Writer, dbPath string) (*Logger, error) {
	logDir := strings.TrimSpace(os.Getenv("CAMSTATION_LOG_DIR"))
	if logDir == "" {
		logDir = filepath.Join(filepath.Dir(dbPath), "logs")
	}
	maxBytes, err := boundedEnvInt64("CAMSTATION_LOG_MAX_MB", DefaultMaxBytes>>20, 1, 1024)
	if err != nil {
		return nil, err
	}
	retained, err := boundedEnvInt64("CAMSTATION_LOG_FILES", DefaultRetainedFiles, 1, 64)
	if err != nil {
		return nil, err
	}
	return New(Config{
		Level:               os.Getenv("CAMSTATION_LOG_LEVEL"),
		ComponentLevels:     os.Getenv("CAMSTATION_LOG_LEVELS"),
		LegacyPlaybackLevel: os.Getenv("CAMSTATION_PLAYBACK_LOG_LEVEL"),
		Writer:              stdout,
		FilePath:            filepath.Join(logDir, DefaultFilename),
		MaxBytes:            maxBytes << 20,
		RetainedFiles:       int(retained),
	})
}

func (logger *Logger) Enabled(component string, level Level) bool {
	return logger != nil && logger.policy != nil && logger.policy.Enabled(component, level)
}

func NewPolicy(level, componentLevels string) (*Policy, error) {
	return newPolicy(level, componentLevels, "")
}

func newPolicy(level, componentLevels, legacyPlaybackLevel string) (*Policy, error) {
	threshold, err := parseLevel(level, Info)
	if err != nil {
		return nil, fmt.Errorf("CAMSTATION_LOG_LEVEL: %w", err)
	}
	overrides, err := parseComponentLevels(componentLevels)
	if err != nil {
		return nil, fmt.Errorf("CAMSTATION_LOG_LEVELS: %w", err)
	}
	if strings.TrimSpace(legacyPlaybackLevel) != "" {
		legacy, parseErr := parseLevel(legacyPlaybackLevel, Info)
		if parseErr != nil {
			return nil, fmt.Errorf("CAMSTATION_PLAYBACK_LOG_LEVEL: %w", parseErr)
		}
		if _, exists := overrides["playback"]; !exists {
			overrides["playback"] = legacy
		}
	}
	return &Policy{threshold: threshold, overrides: overrides}, nil
}

func (policy *Policy) Enabled(component string, level Level) bool {
	if policy == nil || level < Debug || level > Error {
		return false
	}
	threshold := policy.threshold
	for candidate := strings.TrimSpace(component); candidate != ""; {
		if override, ok := policy.overrides[candidate]; ok {
			threshold = override
			break
		}
		separator := strings.LastIndexByte(candidate, '.')
		if separator < 0 {
			break
		}
		candidate = candidate[:separator]
	}
	return threshold != Off && level >= threshold
}

func (logger *Logger) Log(level Level, component, event string, fields Fields) error {
	if !logger.Enabled(component, level) {
		return nil
	}
	now := logger.now().UTC()
	record := Record{
		Timestamp:        now.Format(time.RFC3339Nano),
		Level:            level.String(),
		Component:        boundedText(component, 96),
		Event:            boundedText(event, 96),
		CorrelationID:    boundedSafeText(fields.CorrelationID, 128),
		SessionID:        boundedSafeText(fields.SessionID, 128),
		ViewerID:         boundedSafeText(fields.ViewerID, 128),
		ClientIP:         boundedSafeText(fields.ClientIP, 64),
		CameraID:         fields.CameraID,
		StreamName:       boundedSafeText(fields.StreamName, 128),
		Filename:         boundedSafeText(fields.Filename, 255),
		Transport:        boundedSafeText(fields.Transport, 32),
		Phase:            boundedSafeText(fields.Phase, 64),
		State:            boundedSafeText(fields.State, 64),
		Attempt:          fields.Attempt,
		DurationMS:       fields.DurationMS,
		AttemptElapsedMS: fields.AttemptElapsedMS,
		RetryMS:          fields.RetryMS,
		Frame:            fields.Frame,
		MediaTimeMS:      fields.MediaTimeMS,
		SizeBytes:        fields.SizeBytes,
		ReadyState:       fields.ReadyState,
		ReconnectCount:   fields.ReconnectCount,
		FallbackCount:    fields.FallbackCount,
		UsingFallback:    fields.UsingFallback,
		ErrorCode:        boundedSafeText(fields.ErrorCode, 96),
		Message:          boundedText(SanitizeMessage(fields.Message), 2048),
	}
	if record.Component == "" || record.Event == "" {
		return errors.New("log component and event are required")
	}
	encoded, err := encodeBounded(record)
	if err != nil {
		return err
	}

	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return errors.New("logger is closed")
	}
	var fileErr, writerErr error
	reportFileFailure := false
	reportFileRecovery := false
	if logger.file != nil {
		_, fileErr = logger.file.Write(encoded)
		if fileErr != nil {
			if !logger.fileUnavailable || logger.lastFileFailureReport.IsZero() ||
				now.Sub(logger.lastFileFailureReport) >= fileFailureReportEvery {
				reportFileFailure = true
				logger.lastFileFailureReport = now
			}
			logger.fileUnavailable = true
		} else if logger.fileUnavailable {
			reportFileRecovery = true
			logger.fileUnavailable = false
			logger.lastFileFailureReport = time.Time{}
		}
	}
	_, writerErr = logger.writer.Write(encoded)
	if reportFileFailure {
		fallback, fallbackErr := encodeBounded(Record{
			Timestamp: now.Format(time.RFC3339Nano), Level: Error.String(), Component: "opslog",
			Event: "persistent_write_failed", ErrorCode: "persistent_write_failed",
			Message: boundedText(SanitizeMessage(fileErr.Error()), 512),
		})
		if fallbackErr == nil {
			_, fallbackErr = logger.writer.Write(fallback)
		}
		writerErr = errors.Join(writerErr, fallbackErr)
	}
	if reportFileRecovery {
		recovery, recoveryErr := encodeBounded(Record{
			Timestamp: now.Format(time.RFC3339Nano), Level: Info.String(), Component: "opslog",
			Event: "persistent_write_recovered", State: "ready",
		})
		if recoveryErr == nil {
			_, recoveryErr = logger.writer.Write(recovery)
		}
		writerErr = errors.Join(writerErr, recoveryErr)
	}
	return errors.Join(fileErr, writerErr)
}

func (logger *Logger) Close() error {
	if logger == nil {
		return nil
	}
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if logger.closed {
		return nil
	}
	logger.closed = true
	if logger.file != nil {
		return logger.file.Close()
	}
	return nil
}

func SanitizeMessage(message string) string {
	redacted := store.RedactText(strings.TrimSpace(message))
	redacted = pemPattern.ReplaceAllString(redacted, "[REDACTED_PEM]")
	redacted = urlPattern.ReplaceAllString(redacted, "[REDACTED_URL]")
	redacted = sdpPattern.ReplaceAllString(redacted, "sdp=[REDACTED]")
	redacted = iceCandidatePat.ReplaceAllString(redacted, "candidate:[REDACTED]")
	redacted = iceCredentialPat.ReplaceAllString(redacted, "ice-credential=[REDACTED]")
	redacted = quotedPathPat.ReplaceAllString(redacted, "[REDACTED_PATH]")
	redacted = windowsPathPat.ReplaceAllString(redacted, "[REDACTED_PATH]")
	redacted = posixPathPat.ReplaceAllString(redacted, "$1[REDACTED_PATH]")
	redacted = secretKVPattern.ReplaceAllString(redacted, "$1=[REDACTED]")
	return authorizationPat.ReplaceAllString(redacted, "$1=[REDACTED]")
}

func encodeBounded(record Record) ([]byte, error) {
	for {
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("encode operational log: %w", err)
		}
		if len(encoded)+1 <= MaxRecordBytes {
			return append(encoded, '\n'), nil
		}
		if record.Message == "" {
			return nil, fmt.Errorf("operational log metadata exceeds %d bytes", MaxRecordBytes)
		}
		record.Message = boundedText(record.Message, utf8.RuneCountInString(record.Message)/2)
	}
}

func parseLevel(value string, fallback Level) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return fallback, nil
	case "debug":
		return Debug, nil
	case "info":
		return Info, nil
	case "warn", "warning":
		return Warn, nil
	case "error":
		return Error, nil
	case "off":
		return Off, nil
	default:
		return 0, fmt.Errorf("level must be one of debug, info, warn, error, or off")
	}
}

func parseComponentLevels(value string) (map[string]Level, error) {
	overrides := make(map[string]Level)
	if strings.TrimSpace(value) == "" {
		return overrides, nil
	}
	for _, raw := range strings.Split(value, ",") {
		key, levelValue, ok := strings.Cut(strings.TrimSpace(raw), "=")
		key = strings.TrimSpace(key)
		if !ok || !validComponent(key) || strings.TrimSpace(levelValue) == "" {
			return nil, fmt.Errorf("component levels must use component=level entries")
		}
		if _, exists := overrides[key]; exists {
			return nil, fmt.Errorf("duplicate component level %q", key)
		}
		level, err := parseLevel(levelValue, Info)
		if err != nil {
			return nil, fmt.Errorf("component %q: %w", key, err)
		}
		overrides[key] = level
	}
	return overrides, nil
}

func validComponent(value string) bool {
	if value == "" || len(value) > 96 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func boundedText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if maximum <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	return string(runes[:maximum])
}

func boundedSafeText(value string, maximum int) string {
	return boundedText(SanitizeMessage(value), maximum)
}

func boundedEnvInt64(name string, fallback, minimum, maximum int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be an integer from %d to %d", name, minimum, maximum)
	}
	return parsed, nil
}
