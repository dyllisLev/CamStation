package viewerservice

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	PipeProtocolVersion       = 2
	MaxManagementMessageBytes = 64 * 1024
	ViewerServicePipeName     = `\\.\pipe\CamStationViewerService`
)

var (
	ErrProtocol        = errors.New("invalid management protocol")
	ErrMessageTooLarge = errors.New("management message exceeds 64 KiB")
)

type Request struct {
	Version   int             `json:"version"`
	RequestID string          `json:"requestId"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	Version   int             `json:"version"`
	RequestID string          `json:"requestId"`
	OK        bool            `json:"ok"`
	ErrorCode string          `json:"errorCode,omitempty"`
	Message   string          `json:"message,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func ReadRequest(reader io.Reader) (Request, error) {
	return readRequest(bufio.NewReaderSize(reader, MaxManagementMessageBytes+1))
}

func readRequest(reader *bufio.Reader) (Request, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxManagementMessageBytes {
		return Request{}, ErrMessageTooLarge
	}
	if err != nil {
		return Request{}, fmt.Errorf("%w: incomplete frame: %v", ErrProtocol, err)
	}
	line = line[:len(line)-1]
	if len(line) == 0 {
		return Request{}, fmt.Errorf("%w: empty frame", ErrProtocol)
	}

	var request Request
	decoder := json.NewDecoder(strings.NewReader(string(line)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("%w: decode request: %v", ErrProtocol, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Request{}, fmt.Errorf("%w: trailing JSON", ErrProtocol)
	}
	if err := validateRequest(request); err != nil {
		return Request{}, err
	}
	return request, nil
}

func WriteResponse(writer io.Writer, response Response) error {
	if response.Version != PipeProtocolVersion || strings.TrimSpace(response.RequestID) == "" {
		return fmt.Errorf("%w: invalid response", ErrProtocol)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("%w: encode response: %v", ErrProtocol, err)
	}
	if len(encoded)+1 > MaxManagementMessageBytes {
		return ErrMessageTooLarge
	}
	encoded = append(encoded, '\n')
	_, err = writer.Write(encoded)
	return err
}

func validateRequest(request Request) error {
	if request.Version != PipeProtocolVersion || strings.TrimSpace(request.RequestID) == "" || strings.TrimSpace(request.Type) == "" {
		return fmt.Errorf("%w: invalid request envelope", ErrProtocol)
	}
	return nil
}
