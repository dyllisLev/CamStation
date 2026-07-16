package vieweragent

import (
	"bytes"
	"strings"
	"testing"
)

func TestPipeProtocolIsVersionedNewlineJSONAndBounded(t *testing.T) {
	message := PipeMessage{Version: PipeProtocolVersion, RequestID: "request-1", Type: "viewer_heartbeat", PID: 42, Generation: 3, Nonce: "nonce"}
	var encoded bytes.Buffer
	if err := WritePipeMessage(&encoded, message); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(encoded.Bytes(), []byte("\n")) {
		t.Fatal("pipe message was not newline delimited")
	}
	decoded, err := ReadPipeMessage(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.RequestID != message.RequestID || decoded.Version != PipeProtocolVersion {
		t.Fatalf("unexpected decoded message: %+v", decoded)
	}
	if _, err := ReadPipeMessage(strings.NewReader(strings.Repeat("x", MaxPipeMessageBytes+1) + "\n")); err == nil {
		t.Fatal("oversized pipe message was accepted")
	}
}

func TestPipeIdentityUsesOSProcessAndConsoleSession(t *testing.T) {
	message := PipeMessage{Version: PipeProtocolVersion, RequestID: "request-2", Type: "bootstrap_request", PID: 42, SessionID: 3}
	verified, err := applyPipeIdentity(message, 42, 3, 3)
	if err != nil || verified.PID != 42 || verified.SessionID != 3 {
		t.Fatalf("verified=%+v err=%v", verified, err)
	}
	if _, err := applyPipeIdentity(message, 99, 3, 3); err == nil {
		t.Fatal("declared pipe PID mismatch was accepted")
	}
	if _, err := applyPipeIdentity(message, 42, 4, 3); err == nil {
		t.Fatal("non-console pipe session was accepted")
	}
}
