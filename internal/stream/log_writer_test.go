package stream

import (
	"bytes"
	"strings"
	"testing"

	"camstation/internal/opslog"
)

func TestRedactingLineWriterMasksSplitCameraCredentials(t *testing.T) {
	var output bytes.Buffer
	writer := newRedactingLineWriter(&output)
	chunks := []string{
		"ERR producer rtsp://ad",
		"min:rtsp-secret@192.0.2.10/main\nHTTP http://192.0.2.20/flv?channel=0&user=ad",
		"min&password=flv-secret&token=query-secret\n",
	}
	for _, chunk := range chunks {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	got := output.String()
	for _, secret := range []string{"admin:rtsp-secret", "user=admin", "flv-secret", "query-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("writer leaked %q in %q", secret, got)
		}
	}
	if !strings.Contains(got, "rtsp://redacted:redacted@") || !strings.Contains(got, "password=redacted") || !strings.Contains(got, "token=redacted") {
		t.Fatalf("writer did not preserve redacted diagnostics: %q", got)
	}
}

func TestOperationalLineWriterMapsGo2RTCLevelAndRemovesRawURL(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	writer := newOperationalLineWriter(logger, "stream.go2rtc", opslog.Info)
	if _, err := writer.Write([]byte("07:00:00.000 WRN github.com/AlexxIT/go2rtc producer rtsp://admin:secret@10.0.0.2/live?token=bad\n")); err != nil {
		t.Fatal(err)
	}
	records := decodeOperationalLines(t, output.String())
	if len(records) != 1 || records[0].Level != "warn" || records[0].Component != "stream.go2rtc" || records[0].Event != "child_output" {
		t.Fatalf("records=%+v", records)
	}
	lower := strings.ToLower(output.String())
	for _, forbidden := range []string{"admin", "secret", "10.0.0.2", "token=", "rtsp://"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("operational writer leaked %q: %s", forbidden, output.String())
		}
	}
}

func TestGo2RTCLineLevelUsesHighestRecognizedSeverity(t *testing.T) {
	if got := go2RTCLineLevel("00:00:00 INF nested message ERR failure", opslog.Debug); got != opslog.Error {
		t.Fatalf("level=%s want=error", got)
	}
}

func TestOperationalLineWriterReportsOversizedChildLine(t *testing.T) {
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	writer := newOperationalLineWriter(logger, "stream.go2rtc", opslog.Info)
	if _, err := writer.Write([]byte(strings.Repeat("x", maxBufferedLogLine+1))); err != nil {
		t.Fatal(err)
	}
	records := decodeOperationalLines(t, output.String())
	if len(records) != 1 || records[0].Event != "line_dropped" || records[0].Level != "warn" ||
		records[0].ErrorCode != "line_too_long" {
		t.Fatalf("records=%+v", records)
	}
}

func TestRedactingLineWriterSuppressesIncompleteLine(t *testing.T) {
	var output bytes.Buffer
	writer := newRedactingLineWriter(&output)
	if _, err := writer.Write([]byte("rtsp://admin:secret@192.0.2.10/main")); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("incomplete potentially secret line was emitted: %q", output.String())
	}
}
