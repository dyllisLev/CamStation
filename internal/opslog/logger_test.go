package opslog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoggerUsesLongestComponentLevelAndLegacyPlaybackFallback(t *testing.T) {
	logger, err := New(Config{
		Level:               "warn",
		ComponentLevels:     "stream=info,stream.live_warm=debug",
		LegacyPlaybackLevel: "info",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	checks := []struct {
		component string
		level     Level
		want      bool
	}{
		{"recorder", Info, false},
		{"recorder", Warn, true},
		{"stream.go2rtc", Info, true},
		{"stream.live_warm", Debug, true},
		{"stream.live_warm.ffmpeg", Debug, true},
		{"playback", Info, true},
		{"playback", Debug, false},
	}
	for _, check := range checks {
		if got := logger.Enabled(check.component, check.level); got != check.want {
			t.Fatalf("Enabled(%q, %s)=%v want=%v", check.component, check.level, got, check.want)
		}
	}

	explicit, err := New(Config{
		Level: "info", ComponentLevels: "playback=error", LegacyPlaybackLevel: "debug",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = explicit.Close() })
	if explicit.Enabled("playback", Warn) || !explicit.Enabled("playback", Error) {
		t.Fatal("explicit playback component level did not override legacy setting")
	}
}

func TestLoggerRejectsInvalidLevelConfiguration(t *testing.T) {
	for _, config := range []Config{
		{Level: "verbose"},
		{Level: "info", ComponentLevels: "stream"},
		{Level: "info", ComponentLevels: "stream=debug,stream=warn"},
		{Level: "info", ComponentLevels: "Stream Warm=debug"},
		{Level: "info", LegacyPlaybackLevel: "trace"},
		{Level: "info", FilePath: filepath.Join(t.TempDir(), "ops.jsonl"), MaxBytes: 0, RetainedFiles: 2},
	} {
		if logger, err := New(config); err == nil {
			_ = logger.Close()
			t.Fatalf("invalid config accepted: %+v", config)
		}
	}
}

func TestBoundedTextWithZeroLimitReturnsEmpty(t *testing.T) {
	if got := boundedText("must-not-survive", 0); got != "" {
		t.Fatalf("boundedText zero limit=%q", got)
	}
}

func TestNewFromEnvironmentConfiguresPersistentRotation(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "operational-logs")
	t.Setenv("CAMSTATION_LOG_DIR", logDir)
	t.Setenv("CAMSTATION_LOG_LEVEL", "warn")
	t.Setenv("CAMSTATION_LOG_LEVELS", "stream.live_warm=debug")
	t.Setenv("CAMSTATION_LOG_MAX_MB", "1")
	t.Setenv("CAMSTATION_LOG_FILES", "2")

	var stdout bytes.Buffer
	logger, err := NewFromEnvironment(&stdout, filepath.Join(t.TempDir(), "state", "camstation.db"))
	if err != nil {
		t.Fatal(err)
	}
	if !logger.Enabled("stream.live_warm.ffmpeg", Debug) || logger.Enabled("recorder", Info) {
		t.Fatal("environment log policy was not applied")
	}
	if logger.file == nil || logger.file.maxBytes != 1<<20 || logger.file.retained != 2 {
		t.Fatalf("environment rotation config=%+v", logger.file)
	}
	if err := logger.Log(Warn, "daemon", "started", Fields{State: "running"}); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 {
		t.Fatal("stdout copy was not written")
	}
	if info, err := os.Stat(filepath.Join(logDir, DefaultFilename)); err != nil || info.Size() == 0 {
		t.Fatalf("persistent log was not written: info=%v err=%v", info, err)
	}
}

func TestNewFromEnvironmentRejectsInvalidRotationBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "max below range", key: "CAMSTATION_LOG_MAX_MB", value: "0"},
		{name: "max above range", key: "CAMSTATION_LOG_MAX_MB", value: "1025"},
		{name: "files below range", key: "CAMSTATION_LOG_FILES", value: "0"},
		{name: "files above range", key: "CAMSTATION_LOG_FILES", value: "65"},
		{name: "not an integer", key: "CAMSTATION_LOG_FILES", value: "many"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CAMSTATION_LOG_DIR", t.TempDir())
			t.Setenv(test.key, test.value)
			logger, err := NewFromEnvironment(io.Discard, filepath.Join(t.TempDir(), "camstation.db"))
			if err == nil {
				_ = logger.Close()
				t.Fatalf("%s=%q was accepted", test.key, test.value)
			}
		})
	}
}

func TestLoggerWritesBoundedRedactedJSONRecord(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	logger.now = func() time.Time { return time.Date(2026, 8, 13, 7, 0, 0, 123, time.UTC) }
	t.Cleanup(func() { _ = logger.Close() })

	err = logger.Log(Info, "stream.live_warm", "worker_failed", Fields{
		CorrelationID:         "corr-1",
		SessionID:             "playback-12345678",
		DocumentID:            "document-12345678",
		ViewerID:              "password=viewer-secret",
		StreamName:            "gate-live",
		Surface:               "official_viewer",
		CandidateRole:         "primary",
		Attempt:               2,
		AttemptGeneration:     3,
		ResubscribeGeneration: 1,
		RetryMS:               5000,
		ErrorCode:             "input_timeout",
		TerminalReason:        "setup_timeout",
		Message: `open rtsp://admin:secret@10.0.0.2/live?token=abc failed password=hunter2 ` +
			`at /var/lib/camstation/media/file.mp4 C:\ProgramData\CamStation\secret.log ` +
			`from "/var/lib/CamStation Private/file.mp4" -----BEGIN PRIVATE KEY----- private-material -----END PRIVATE KEY----- ` +
			"candidate:1 1 UDP 1 192.0.2.5 5000 typ host\nice-pwd=ice-secret\nsdp=v=0",
	})
	if err != nil {
		t.Fatal(err)
	}

	line := strings.TrimSpace(output.String())
	if len(line) == 0 || len(line) > MaxRecordBytes {
		t.Fatalf("record length=%d", len(line))
	}
	var record Record
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("invalid JSON %q: %v", line, err)
	}
	if record.Timestamp != "2026-08-13T07:00:00.000000123Z" || record.Level != "info" ||
		record.Component != "stream.live_warm" || record.Event != "worker_failed" ||
		record.Attempt != 2 || record.RetryMS != 5000 || record.StreamName != "gate-live" ||
		record.DocumentID != "document-12345678" || record.Surface != "official_viewer" ||
		record.CandidateRole != "primary" || record.AttemptGeneration != 3 ||
		record.ResubscribeGeneration != 1 || record.TerminalReason != "setup_timeout" {
		t.Fatalf("record=%+v", record)
	}
	lower := strings.ToLower(line)
	for _, forbidden := range []string{
		"admin", "viewer-secret", "10.0.0.2", "token=", "hunter2", "rtsp://", "/var/lib", "programdata",
		"secret.log", "camstation private", "192.0.2.5", "ice-secret", "private-material", "v=0",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("record leaked %q: %s", forbidden, line)
		}
	}
	for _, placeholder := range []string{"[REDACTED_PEM]", "candidate:[REDACTED]", "ice-credential=[REDACTED]", "sdp=[REDACTED]"} {
		if !strings.Contains(line, placeholder) {
			t.Fatalf("record missing %q: %s", placeholder, line)
		}
	}
}

func TestRotatingLoggerPersistsAcrossReopenAndRetainsBoundedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "camstationd.jsonl")
	writeBatch := func(start, count int) {
		logger, err := New(Config{
			Level: "debug", FilePath: path, MaxBytes: 1024, RetainedFiles: 3,
		})
		if err != nil {
			t.Fatal(err)
		}
		for index := start; index < start+count; index++ {
			if err := logger.Log(Info, "test.component", "batch", Fields{
				CorrelationID: fmt.Sprintf("record-%03d", index), Message: strings.Repeat("x", 120),
			}); err != nil {
				t.Fatal(err)
			}
		}
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}

	writeBatch(0, 8)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeBatch(8, 8)
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() == 0 || after.Size() == 0 {
		t.Fatal("active log did not survive reopen")
	}
	matches, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("retained files=%d want=3: %v", len(matches), matches)
	}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 1024 {
			t.Fatalf("oversized log %s size=%d", filepath.Base(match), info.Size())
		}
	}
}

func TestLoggerConcurrentWritesRemainOneJSONRecordPerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "camstationd.jsonl")
	logger, err := New(Config{Level: "debug", FilePath: path, MaxBytes: 1 << 20, RetainedFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for index := 0; index < 50; index++ {
				if err := logger.Log(Debug, "test.concurrent", "write", Fields{
					CorrelationID: fmt.Sprintf("%d-%d", worker, index),
				}); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}(worker)
	}
	workers.Wait()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("line %d invalid: %v", count+1, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 400 {
		t.Fatalf("records=%d want=400", count)
	}
}

func TestLoggerRateLimitsRepeatedMessagesAndEmitsNumericSummary(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	current := time.Date(2026, 8, 14, 3, 10, 0, 0, time.UTC)
	started := current
	logger.now = func() time.Time { return current }
	t.Cleanup(func() { _ = logger.Close() })

	for index := 0; index < 1000; index++ {
		if err := logger.LogRateLimited(time.Minute, Error, "recorder.ffmpeg", "ffmpeg_error", Fields{
			CameraID:   7,
			StreamName: "gate-recording",
			ErrorCode:  "ffmpeg_error",
			Message:    fmt.Sprintf("DTS %d, next:%d st:0 invalid dropping", index+100, index+200),
		}); err != nil {
			t.Fatal(err)
		}
		current = current.Add(50 * time.Millisecond)
	}
	if records := decodeTestRecords(t, output.Bytes()); len(records) != 1 || records[0].MessageFingerprint == "" || records[0].SuppressedCount != 0 {
		t.Fatalf("initial rate-limited records=%+v", records)
	}

	current = started.Add(time.Minute)
	if err := logger.LogRateLimited(time.Minute, Error, "recorder.ffmpeg", "ffmpeg_error", Fields{
		CameraID:   7,
		StreamName: "gate-recording",
		ErrorCode:  "ffmpeg_error",
		Message:    "DTS 9000, next:200 st:0 invalid dropping",
	}); err != nil {
		t.Fatal(err)
	}
	records := decodeTestRecords(t, output.Bytes())
	if len(records) != 2 || records[1].Message != "" || records[1].SuppressedCount != 1000 ||
		records[1].WindowMS != time.Minute.Milliseconds() ||
		records[1].MessageFingerprint != records[0].MessageFingerprint {
		t.Fatalf("rate-limited summary=%+v", records)
	}

	if err := logger.LogRateLimited(time.Minute, Error, "recorder.ffmpeg", "ffmpeg_error", Fields{
		CameraID:   8,
		StreamName: "other-recording",
		ErrorCode:  "ffmpeg_error",
		Message:    "DTS 9000, next:200 st:0 invalid dropping",
	}); err != nil {
		t.Fatal(err)
	}
	if records = decodeTestRecords(t, output.Bytes()); len(records) != 3 || records[2].MessageFingerprint != records[0].MessageFingerprint {
		t.Fatalf("separate worker was suppressed: %+v", records)
	}

	current = current.Add(2 * time.Minute)
	if err := logger.LogRateLimited(time.Minute, Error, "recorder.ffmpeg", "ffmpeg_error", Fields{
		CameraID:   7,
		StreamName: "gate-recording",
		ErrorCode:  "ffmpeg_error",
		Message:    "DTS 9100, next:210 st:0 invalid dropping",
	}); err != nil {
		t.Fatal(err)
	}
	records = decodeTestRecords(t, output.Bytes())
	if len(records) != 4 || records[3].Message == "" || records[3].SuppressedCount != 1 {
		t.Fatalf("signal after quiet interval did not restore context: %+v", records)
	}
}

func TestLoggerReportsPersistentFileFailureToStdoutOncePerInterval(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{
		Level: "debug", Writer: &output, FilePath: filepath.Join(t.TempDir(), DefaultFilename),
		MaxBytes: 1 << 20, RetainedFiles: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger.now = func() time.Time { return time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC) }
	if err := logger.file.Close(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := logger.Log(Info, "daemon", "heartbeat", Fields{Attempt: index + 1}); err == nil {
			t.Fatal("closed persistent writer did not return an error")
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	records := decodeTestRecords(t, output.Bytes())
	if len(records) != 3 || records[0].Event != "heartbeat" || records[1].Event != "persistent_write_failed" ||
		records[1].Level != "error" || records[1].Component != "opslog" || records[2].Event != "heartbeat" {
		t.Fatalf("records=%+v", records)
	}
}

func decodeTestRecords(t *testing.T, data []byte) []Record {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	records := make([]Record, 0)
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return records
}

func TestLineWriterBuffersFragmentsAndUsesOperationalRedaction(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	writer := NewLineWriter(logger, "daemon.legacy", Info)
	for _, fragment := range []string{"connect rtsp://ad", "min:secret@10.0.0.2/live", " failed\n"} {
		if _, err := writer.Write([]byte(fragment)); err != nil {
			t.Fatal(err)
		}
	}
	var record Record
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatal(err)
	}
	if record.Component != "daemon.legacy" || record.Event != "message" || record.Level != "info" {
		t.Fatalf("record=%+v", record)
	}
	lower := strings.ToLower(output.String())
	for _, forbidden := range []string{"admin", "secret", "10.0.0.2", "rtsp://"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("line writer leaked %q: %s", forbidden, output.String())
		}
	}
}
