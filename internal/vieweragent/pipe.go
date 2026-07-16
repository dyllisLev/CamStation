package vieweragent

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	PipeProtocolVersion = 1
	MaxPipeMessageBytes = 64 * 1024
	ViewerPipeName      = `\\.\pipe\CamStationViewerAgent`
)

type PipeMessage struct {
	Version    int             `json:"version"`
	RequestID  string          `json:"requestId"`
	Type       string          `json:"type"`
	PID        int             `json:"pid,omitempty"`
	SessionID  uint32          `json:"sessionId,omitempty"`
	Generation int64           `json:"generation,omitempty"`
	Nonce      string          `json:"nonce,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func applyPipeIdentity(message PipeMessage, processID uint32, sessionID uint32, consoleSessionID uint32) (PipeMessage, error) {
	if processID == 0 || message.PID != int(processID) || message.SessionID != sessionID {
		return PipeMessage{}, errors.New("pipe process identity mismatch")
	}
	if consoleSessionID == ^uint32(0) || sessionID != consoleSessionID {
		return PipeMessage{}, errors.New("pipe client is not in the active console session")
	}
	message.PID = int(processID)
	message.SessionID = sessionID
	return message, nil
}

func ReadPipeMessage(reader io.Reader) (PipeMessage, error) {
	return readPipeMessage(bufio.NewReaderSize(reader, MaxPipeMessageBytes+1))
}

func readPipeMessage(reader *bufio.Reader) (PipeMessage, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxPipeMessageBytes {
		return PipeMessage{}, errors.New("pipe message exceeds 64 KiB")
	}
	if err != nil {
		return PipeMessage{}, err
	}
	line = line[:len(line)-1]
	if len(line) == 0 {
		return PipeMessage{}, errors.New("empty pipe message")
	}
	var message PipeMessage
	decoder := json.NewDecoder(strings.NewReader(string(line)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return PipeMessage{}, err
	}
	if message.Version != PipeProtocolVersion || strings.TrimSpace(message.RequestID) == "" || strings.TrimSpace(message.Type) == "" {
		return PipeMessage{}, errors.New("invalid pipe protocol message")
	}
	return message, nil
}

func WritePipeMessage(writer io.Writer, message PipeMessage) error {
	if message.Version != PipeProtocolVersion || strings.TrimSpace(message.RequestID) == "" || strings.TrimSpace(message.Type) == "" {
		return errors.New("invalid pipe protocol message")
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(encoded)+1 > MaxPipeMessageBytes {
		return errors.New("pipe message exceeds 64 KiB")
	}
	encoded = append(encoded, '\n')
	_, err = writer.Write(encoded)
	return err
}
