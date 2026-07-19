package viewerservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogRedactsSecretsAndBoundsJSONLine(t *testing.T) {
	root := t.TempDir()
	logs := newLogManager(root, 4*1024, 5, func(string, string) error { return nil })
	logs.now = func() time.Time { return time.Date(2026, 7, 18, 2, 3, 4, 0, time.UTC) }
	detail := `json={"authorization":"Bearer json-auth","token":"json-token","updateToken":"camel-token","update_token":"snake-token"} ` +
		"Authorization: Bearer abc123 server=https://user:pass@cam.example/live?token=hidden " +
		"rtsp://admin:camera@10.0.0.7/stream nonce=nonce-secret response body={\"secret\":\"body\"}\nline2-secret-body " +
		"-----BEGIN PRIVATE KEY----- private -----END PRIVATE KEY-----" + strings.Repeat("x", MaxLogRecordBytes)
	if err := logs.WriteService(LogRecord{Component: "service", State: "failed", Code: "pipe_error", CorrelationID: "corr-1", Detail: detail}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ServiceLogFilename))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxLogRecordBytes || !strings.HasSuffix(string(data), "\n") || strings.Count(string(data), "\n") != 1 {
		t.Fatalf("unbounded or multiline record: bytes=%d data=%q", len(data), data)
	}
	for _, secret := range []string{"json-auth", "json-token", "camel-token", "snake-token", "abc123", "user:pass", "hidden", "admin:camera", "10.0.0.7", "nonce-secret", `\"secret\":\"body\"`, "line2-secret-body", "PRIVATE KEY", "private"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("log contains %q: %s", secret, data)
		}
	}
}

func TestLogAlwaysWritesUTCStateAndCorrelationID(t *testing.T) {
	root := t.TempDir()
	logs := newLogManager(root, DefaultLogRotateBytes, DefaultLogRetainedFiles, func(string, string) error { return nil })
	if err := logs.WriteService(LogRecord{Component: "service"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ServiceLogFilename))
	if err != nil {
		t.Fatal(err)
	}
	var line logLine
	decodeJSON(t, bytes.TrimSpace(data), &line)
	if line.Timestamp == "" || !strings.HasSuffix(line.Timestamp, "Z") || line.State == "" || line.CorrelationID == "" {
		t.Fatalf("incomplete bounded line=%+v", line)
	}
}

func TestLogPreservesRecoverableErrorCode(t *testing.T) {
	root := t.TempDir()
	logs := newLogManager(root, DefaultLogRotateBytes, DefaultLogRetainedFiles, func(string, string) error { return nil })
	correlationID := logs.ErrorLogger(t.Context(), ErrLoggingUnavailable)
	data, err := os.ReadFile(filepath.Join(root, ServiceLogFilename))
	if err != nil {
		t.Fatal(err)
	}
	var line logLine
	decodeJSON(t, bytes.TrimSpace(data), &line)
	if line.Code != CodeLoggingUnavailable || line.CorrelationID != correlationID {
		t.Fatalf("line=%+v correlation=%q", line, correlationID)
	}
}

func TestLogErrorLoggerNeverWritesArbitraryErrorDetail(t *testing.T) {
	root := t.TempDir()
	logs := newLogManager(root, DefaultLogRotateBytes, DefaultLogRetainedFiles, func(string, string) error { return nil })
	correlationID := logs.ErrorLogger(t.Context(), errors.New(`HTTP 500: {"sensitive":"raw-body"}`))
	if correlationID == "" {
		t.Fatal("missing correlation ID")
	}
	data, err := os.ReadFile(filepath.Join(root, ServiceLogFilename))
	if err != nil {
		t.Fatal(err)
	}
	var line logLine
	decodeJSON(t, bytes.TrimSpace(data), &line)
	if line.Detail != "" || strings.Contains(string(data), "sensitive") || strings.Contains(string(data), "raw-body") {
		t.Fatalf("arbitrary error detail was logged: %s", data)
	}
}

func TestLogReturnsNoCorrelationWhenRecordCannotBeWritten(t *testing.T) {
	logs := newLogManager(filepath.Join(t.TempDir(), "missing", "logs"), DefaultLogRotateBytes, DefaultLogRetainedFiles, func(string, string) error { return nil })
	if correlationID := logs.ErrorLogger(t.Context(), ErrLoggingUnavailable); correlationID != "" {
		t.Fatalf("correlation without record=%q", correlationID)
	}
}

func TestLogRedactsCompleteAuthorizationValues(t *testing.T) {
	for _, test := range []struct {
		name      string
		input     string
		forbidden []string
	}{
		{name: "digest", input: `request failed Authorization: Digest username="user", realm="camera", response="credential"`, forbidden: []string{"user", "camera", "credential"}},
		{name: "ntlm", input: `proxy-authorization=NTLM TlRMTVNTUA-secret-blob`, forbidden: []string{"TlRMTVNTUA", "secret-blob"}},
		{name: "json", input: `{"authorization":"Custom first second third"}`, forbidden: []string{"first", "second", "third"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			redacted := redactLogDetail(test.input)
			for _, forbidden := range test.forbidden {
				if strings.Contains(redacted, forbidden) {
					t.Fatalf("redacted authorization contains %q: %q", forbidden, redacted)
				}
			}
		})
	}
}

func TestLogRotationRetainsAtMostFiveBoundedFiles(t *testing.T) {
	root := t.TempDir()
	logs := newLogManager(root, 180, 5, func(string, string) error { return nil })
	for i := 0; i < 30; i++ {
		if err := logs.WriteService(LogRecord{Component: "service", State: "running", CorrelationID: "rotation", Detail: strings.Repeat("x", 60)}); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(root, ServiceLogFilename+"*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 5 {
		t.Fatalf("retained files=%d want=5: %v", len(matches), matches)
	}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 180 {
			t.Fatalf("%s size=%d exceeds rotation bound", match, info.Size())
		}
	}
}

func TestLogViewerFilenameIsStableAndCannotTraverse(t *testing.T) {
	root := t.TempDir()
	logs := newLogManager(root, DefaultLogRotateBytes, DefaultLogRetainedFiles, func(path, sid string) error {
		if !strings.HasPrefix(path, root+string(os.PathSeparator)) || sid != "S-1-5-21-1000" {
			t.Fatalf("secure path=%q sid=%q", path, sid)
		}
		return os.WriteFile(path, nil, 0o600)
	})
	first, err := logs.AssignViewerLog(Peer{PID: 9, SessionID: 4, Interactive: true, UserSID: "S-1-5-21-1000"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := logs.AssignViewerLog(Peer{PID: 99, SessionID: 4, Interactive: true, UserSID: "S-1-5-21-1000"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(filepath.Base(first), "viewer-4-") || filepath.Ext(first) != ".log" {
		t.Fatalf("unstable viewer paths first=%q second=%q", first, second)
	}
	if _, err := logs.openManaged("../outside.log"); !errors.Is(err, ErrUnsafeLogPath) {
		t.Fatalf("traversal error=%v", err)
	}
	if _, err := logs.openManaged(`..\outside.log`); !errors.Is(err, ErrUnsafeLogPath) {
		t.Fatalf("Windows traversal error=%v", err)
	}
}

func TestLogViewerRotationRetainsAtMostFiveFiles(t *testing.T) {
	root := t.TempDir()
	secure := func(path, _ string) error {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		return file.Close()
	}
	logs := newLogManager(root, 180, 5, secure)
	path, err := logs.AssignViewerLog(Peer{PID: 9, SessionID: 4, Interactive: true, UserSID: "S-1-5-21-1000"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString(strings.Repeat("x", 60)); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if err := logs.MaintainViewerLogs(); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 5 {
		t.Fatalf("viewer retained files=%d want=5: %v", len(matches), matches)
	}
	for _, match := range matches {
		if info, err := os.Stat(match); err != nil || info.Size() > 180 {
			t.Fatalf("viewer log %q info=%v err=%v", match, info, err)
		}
	}
}

func TestLogViewerMaintenanceBoundsAlreadyOversizedFile(t *testing.T) {
	root := t.TempDir()
	secure := func(path, _ string) error {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		return file.Close()
	}
	logs := newLogManager(root, 180, 5, secure)
	path, err := logs.AssignViewerLog(Peer{PID: 9, SessionID: 4, Interactive: true, UserSID: "S-1-5-21-1000"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if _, err := file.WriteString(strings.Repeat("x", 59) + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := logs.MaintainViewerLogs(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 180 {
			t.Fatalf("oversized retained viewer log %q size=%d", match, info.Size())
		}
	}
}

func TestLogViewerCannotSelectArbitraryPathAndACLFailureIsRecoverable(t *testing.T) {
	root := t.TempDir()
	wantErr := errors.New("acl unavailable")
	logs := newLogManager(root, DefaultLogRotateBytes, DefaultLogRetainedFiles, func(path, _ string) error {
		if err := os.WriteFile(path, nil, 0o666); err != nil {
			return err
		}
		return wantErr
	})
	server := testServer(&memoryConfigStore{config: testMachineConfig()}, nil)
	server.SetLeaseLogAssigner(logs.AssignViewerLog)
	peer := Peer{PID: 10, SessionID: 2, Interactive: true, UserSID: "S-1-5-21-1000"}
	response, err := server.Handle(t.Context(), "connection-a", peer, Request{
		Version: PipeProtocolVersion, RequestID: "lease", Type: "acquire_lease",
		Payload: []byte(`{"logPath":"C:\\\\Users\\\\Public\\\\chosen.log","pid":999,"sessionId":999}`),
	})
	if err != nil || response.OK || response.ErrorCode != CodeLoggingUnavailable {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "viewer-*.log")); err != nil || len(matches) != 0 {
		t.Fatalf("insecure viewer log survived ACL failure: matches=%v err=%v", matches, err)
	}
	server.SetLeaseLogAssigner(func(Peer) (string, error) { return filepath.Join(root, "assigned.log"), nil })
	response, err = server.Handle(t.Context(), "connection-b", peer, Request{
		Version: PipeProtocolVersion, RequestID: "lease-2", Type: "acquire_lease",
		Payload: []byte(`{"logPath":"C:\\\\Users\\\\Public\\\\chosen.log"}`),
	})
	if err != nil || !response.OK {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	var grant LeaseGrant
	decodeJSON(t, response.Payload, &grant)
	if grant.LogPath != filepath.Join(root, "assigned.log") || strings.Contains(grant.LogPath, "chosen") {
		t.Fatalf("grant path=%q", grant.LogPath)
	}
}

func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
