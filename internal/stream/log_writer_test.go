package stream

import (
	"bytes"
	"strings"
	"testing"
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
