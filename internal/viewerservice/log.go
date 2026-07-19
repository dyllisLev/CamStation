package viewerservice

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	DefaultLogsRoot         = `C:\ProgramData\CamStation\Viewer\Logs`
	ServiceLogFilename      = "service.log"
	DefaultLogRotateBytes   = 10 * 1024 * 1024
	DefaultLogRetainedFiles = 5
	MaxLogRecordBytes       = 4 * 1024
	CodeLoggingUnavailable  = "logging_unavailable"
)

var (
	ErrUnsafeLogPath        = errors.New("unsafe log path")
	ErrLoggingUnavailable   = errors.New(CodeLoggingUnavailable)
	logURLPattern           = regexp.MustCompile(`(?i)\b(?:https?|rtsp|rtsps)://[^\s"'<>]+`)
	logPEMPattern           = regexp.MustCompile(`(?is)-----BEGIN [^-\r\n]+-----.*?-----END [^-\r\n]+-----`)
	logResponsePattern      = regexp.MustCompile(`(?is)(?:raw\s+response|response\s+body)\s*[:=].*`)
	logAuthorizationPattern = regexp.MustCompile(`(?is)\b"?(proxy[-_]?authorization|authorization)"?\s*[:=].*`)
	logSecretPattern        = regexp.MustCompile(`(?i)\b"?(password|secret|token|nonce|api[_-]?key|update[_-]?token)"?\s*[:=]\s*"?[^"\s,;}]+"?`)
)

type LogRecord struct {
	Component     string
	State         string
	Code          string
	CorrelationID string
	Detail        string
}

type logLine struct {
	Timestamp     string `json:"timestamp"`
	Component     string `json:"component"`
	State         string `json:"state,omitempty"`
	Code          string `json:"code,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

type LogManager struct {
	root        string
	rotateSize  int64
	retained    int
	secureFile  func(string, string) error
	now         func() time.Time
	mu          sync.Mutex
	viewerFiles map[string]string
}

func NewLogManager() *LogManager {
	return newLogManager(DefaultLogsRoot, DefaultLogRotateBytes, DefaultLogRetainedFiles, secureViewerLogFile)
}

func newLogManager(root string, rotateSize int64, retained int, secureFile func(string, string) error) *LogManager {
	if rotateSize <= 0 {
		rotateSize = DefaultLogRotateBytes
	}
	if retained < 1 {
		retained = 1
	}
	return &LogManager{
		root: filepath.Clean(root), rotateSize: rotateSize, retained: retained,
		secureFile: secureFile, now: time.Now, viewerFiles: make(map[string]string),
	}
}

func (logs *LogManager) WriteService(record LogRecord) error {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	line, err := logs.encode(record)
	if err != nil {
		return err
	}
	path, err := logs.managedPath(ServiceLogFilename)
	if err != nil {
		return err
	}
	if err := logs.rotate(path, int64(len(line))); err != nil {
		return err
	}
	file, err := logs.openManaged(ServiceLogFilename)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return fmt.Errorf("append service log: %w", err)
	}
	return file.Close()
}

func (logs *LogManager) AssignViewerLog(peer Peer) (string, error) {
	sid := strings.TrimSpace(peer.UserSID)
	if !peer.Interactive || peer.SessionID == 0 || sid == "" {
		return "", fmt.Errorf("%w: verified interactive identity is required", ErrLoggingUnavailable)
	}
	digest := sha256.Sum256([]byte(strings.ToUpper(sid)))
	name := fmt.Sprintf("viewer-%d-%s.log", peer.SessionID, hex.EncodeToString(digest[:8]))
	path, err := logs.managedPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrLoggingUnavailable, err)
	}
	if logs.secureFile == nil {
		return "", fmt.Errorf("%w: secure file creator is unavailable", ErrLoggingUnavailable)
	}
	logs.mu.Lock()
	defer logs.mu.Unlock()
	if err := logs.rotate(path, 1); err != nil {
		return "", fmt.Errorf("%w: rotate viewer log: %v", ErrLoggingUnavailable, err)
	}
	_, statErr := os.Stat(path)
	existed := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("%w: inspect viewer log: %v", ErrLoggingUnavailable, statErr)
	}
	if err := logs.secureFile(path, sid); err != nil {
		if !existed {
			_ = os.Remove(path)
		}
		return "", fmt.Errorf("%w: secure viewer log: %v", ErrLoggingUnavailable, err)
	}
	logs.viewerFiles[name] = sid
	return path, nil
}

func (logs *LogManager) MaintainViewerLogs() error {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	for name, sid := range logs.viewerFiles {
		path, err := logs.managedPath(name)
		if err != nil {
			return err
		}
		if err := logs.rotate(path, 1); err != nil {
			return err
		}
		if err := logs.secureFile(path, sid); err != nil {
			return err
		}
	}
	return nil
}

func (logs *LogManager) ErrorLogger(ctx context.Context, logged error) string {
	correlationID := newCorrelationID()
	code := ErrorCode(logged)
	if code == "" {
		switch {
		case errors.Is(logged, ErrLoggingUnavailable):
			code = CodeLoggingUnavailable
		case errors.Is(logged, ErrInvalidRequest):
			code = CodeInvalidRequest
		case errors.Is(logged, ErrUnsupportedRequest):
			code = CodeUnsupportedRequest
		case errors.Is(logged, ErrLeaseBusy):
			code = CodeLeaseBusy
		default:
			code = "internal_error"
		}
	}
	if err := logs.WriteService(LogRecord{Component: "service", State: "failed", Code: code, CorrelationID: correlationID}); err != nil {
		return ""
	}
	return correlationID
}

func (logs *LogManager) openManaged(name string) (*os.File, error) {
	path, err := logs.managedPath(name)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open managed log: %w", err)
	}
	return file, nil
}

func (logs *LogManager) managedPath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || filepath.IsAbs(name) || filepath.Base(name) != name {
		return "", ErrUnsafeLogPath
	}
	path := filepath.Join(logs.root, name)
	relative, err := filepath.Rel(logs.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrUnsafeLogPath
	}
	return path, nil
}

func (logs *LogManager) encode(record LogRecord) ([]byte, error) {
	limit := int64(MaxLogRecordBytes)
	if logs.rotateSize < limit {
		limit = logs.rotateSize
	}
	line := logLine{
		Timestamp:     logs.now().UTC().Format(time.RFC3339Nano),
		Component:     boundedLogText(record.Component, 64),
		State:         boundedLogText(record.State, 64),
		Code:          boundedLogText(record.Code, 64),
		CorrelationID: boundedLogText(record.CorrelationID, 128),
		Detail:        boundedLogText(redactLogDetail(record.Detail), 2048),
	}
	if line.Component == "" {
		line.Component = "service"
	}
	if line.State == "" && line.Code == "" {
		line.State = "event"
	}
	if line.CorrelationID == "" {
		line.CorrelationID = newCorrelationID()
	}
	for {
		encoded, err := json.Marshal(line)
		if err != nil {
			return nil, fmt.Errorf("encode log record: %w", err)
		}
		if int64(len(encoded)+1) <= limit {
			return append(encoded, '\n'), nil
		}
		if line.Detail == "" {
			return nil, fmt.Errorf("log record metadata exceeds %d bytes", limit)
		}
		line.Detail = boundedLogText(line.Detail, len(line.Detail)/2)
	}
}

func (logs *LogManager) rotate(path string, incoming int64) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat log: %w", err)
	}
	if info.Size()+incoming <= logs.rotateSize {
		return nil
	}
	if err := logs.boundLogFile(path); err != nil {
		return err
	}
	if logs.retained == 1 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rotate log: %w", err)
		}
		return nil
	}
	for index := logs.retained - 1; index >= 1; index-- {
		destination := fmt.Sprintf("%s.%d", path, index)
		_ = os.Remove(destination)
		source := path
		if index > 1 {
			source = fmt.Sprintf("%s.%d", path, index-1)
		}
		if err := os.Rename(source, destination); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("rotate log: %w", err)
		}
		if err := logs.boundLogFile(destination); err != nil {
			return err
		}
	}
	return nil
}

func (logs *LogManager) boundLogFile(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && info.Size() <= logs.rotateSize) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat rotated log: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open oversized log: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, logs.rotateSize))
	closeErr := file.Close()
	if readErr != nil {
		return fmt.Errorf("read oversized log: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close oversized log: %w", closeErr)
	}
	length := bytes.LastIndexByte(data, '\n') + 1
	if err := os.Truncate(path, int64(length)); err != nil {
		return fmt.Errorf("bound oversized log: %w", err)
	}
	return nil
}

func redactLogDetail(value string) string {
	value = logPEMPattern.ReplaceAllString(value, "[REDACTED_PEM]")
	value = logURLPattern.ReplaceAllString(value, "[REDACTED_URL]")
	value = logResponsePattern.ReplaceAllString(value, "response_body=[REDACTED]")
	value = logAuthorizationPattern.ReplaceAllString(value, "$1=[REDACTED]")
	return logSecretPattern.ReplaceAllString(value, "$1=[REDACTED]")
}

func boundedLogText(value string, maxBytes int) string {
	value = strings.Map(func(char rune) rune {
		if char == '\r' || char == '\n' || char == '\x00' {
			return ' '
		}
		return char
	}, strings.TrimSpace(value))
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func newCorrelationID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err == nil {
		return hex.EncodeToString(value)
	}
	return fmt.Sprintf("%x", time.Now().UTC().UnixNano())
}
