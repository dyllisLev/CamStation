package viewerservice

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestProtocolCodecIsVersionedStrictAndBounded(t *testing.T) {
	valid := `{"version":2,"requestId":"request-1","type":"get_status"}` + "\n"
	request, err := ReadRequest(strings.NewReader(valid))
	if err != nil || request.Version != PipeProtocolVersion || request.RequestID != "request-1" || request.Type != "get_status" {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if ViewerServicePipeName != `\\.\pipe\CamStationViewerService` {
		t.Fatalf("pipe name=%q", ViewerServicePipeName)
	}

	tests := []struct {
		name    string
		message string
		want    error
	}{
		{name: "unknown field", message: `{"version":2,"requestId":"r","type":"get_status","clientId":"secret"}` + "\n", want: ErrProtocol},
		{name: "extra JSON", message: `{"version":2,"requestId":"r","type":"get_status"} {}` + "\n", want: ErrProtocol},
		{name: "wrong version", message: `{"version":1,"requestId":"r","type":"get_status"}` + "\n", want: ErrProtocol},
		{name: "empty request ID", message: `{"version":2,"requestId":" ","type":"get_status"}` + "\n", want: ErrProtocol},
		{name: "empty type", message: `{"version":2,"requestId":"r","type":""}` + "\n", want: ErrProtocol},
		{name: "malformed JSON", message: `{"version":2` + "\n", want: ErrProtocol},
		{name: "missing frame delimiter", message: `{"version":2,"requestId":"r","type":"get_status"}`, want: ErrProtocol},
		{name: "over 64 KiB", message: strings.Repeat("x", MaxManagementMessageBytes) + "\n", want: ErrMessageTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ReadRequest(strings.NewReader(test.message)); !errors.Is(err, test.want) {
				t.Fatalf("ReadRequest error=%v, want %v", err, test.want)
			}
		})
	}
}

func TestProtocolResponseCodecWritesOneBoundedFrame(t *testing.T) {
	var encoded bytes.Buffer
	response := Response{Version: PipeProtocolVersion, RequestID: "request-1", OK: true}
	if err := WriteResponse(&encoded, response); err != nil {
		t.Fatal(err)
	}
	if got := encoded.String(); got != `{"version":2,"requestId":"request-1","ok":true}`+"\n" {
		t.Fatalf("encoded response=%q", got)
	}

	response.Payload = []byte(`{"value":"` + strings.Repeat("x", MaxManagementMessageBytes) + `"}`)
	if err := WriteResponse(&bytes.Buffer{}, response); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("WriteResponse error=%v", err)
	}
}
