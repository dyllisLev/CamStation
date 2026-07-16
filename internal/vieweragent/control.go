package vieweragent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultControlReadDeadline      = 25 * time.Second
	DefaultHeartbeatRequestDeadline = 10 * time.Second
	DefaultCommandReportDeadline    = 5 * time.Second
	ControlTransportSSE             = "sse"
	ControlTransportLongPoll        = "long_poll"
	maxControlMessageBytes          = 64 * 1024
)

type ReconnectState struct{ failures int }

func (state *ReconnectState) NextDelay() time.Duration {
	delays := [...]time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}
	if state.failures >= len(delays) {
		state.failures++
		return 5 * time.Minute
	}
	delay := delays[state.failures]
	state.failures++
	return delay
}

func (state *ReconnectState) Reset() { state.failures = 0 }

type Command struct {
	ID             int64     `json:"id"`
	ViewerID       string    `json:"viewerId,omitempty"`
	Type           string    `json:"type"`
	Message        string    `json:"message,omitempty"`
	Route          string    `json:"route,omitempty"`
	Mode           string    `json:"mode,omitempty"`
	StreamName     string    `json:"streamName,omitempty"`
	DesiredVersion string    `json:"desiredVersion,omitempty"`
	ArtifactSHA256 string    `json:"artifactSha256,omitempty"`
	PayloadHash    string    `json:"payloadHash"`
	TTLSeconds     int       `json:"ttlSeconds"`
	Generation     int64     `json:"generation"`
	CreatedAt      time.Time `json:"createdAt"`
}

func (command Command) Key() string { return commandKey(command.ID) }

type ControlResult struct {
	Transport string
	Command   *Command
}

type ControlClient struct {
	HTTPClient   *http.Client
	ServerURL    string
	ClientID     string
	ReadDeadline time.Duration
}

func (client ControlClient) Next(ctx context.Context) (ControlResult, error) {
	result, err := client.receiveSSE(ctx)
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		return ControlResult{}, ctx.Err()
	}
	return client.longPoll(ctx)
}

func (client ControlClient) receiveSSE(ctx context.Context) (ControlResult, error) {
	deadline := client.deadline()
	readCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	request, err := http.NewRequestWithContext(readCtx, http.MethodGet, client.endpoint("/api/viewers/"+url.PathEscape(client.ClientID)+"/control"), nil)
	if err != nil {
		return ControlResult{}, err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.httpClient().Do(request)
	if err != nil {
		return ControlResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return ControlResult{}, fmt.Errorf("SSE status %s", response.Status)
	}

	scanner := bufio.NewScanner(io.LimitReader(response.Body, maxControlMessageBytes+1))
	scanner.Buffer(make([]byte, 1024), maxControlMessageBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ":") {
			return ControlResult{Transport: ControlTransportSSE}, nil
		}
		if strings.HasPrefix(line, "data:") {
			var command Command
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &command); err != nil {
				return ControlResult{}, fmt.Errorf("decode SSE command: %w", err)
			}
			if err := validateCommand(command); err != nil {
				return ControlResult{}, err
			}
			return ControlResult{Transport: ControlTransportSSE, Command: &command}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return ControlResult{}, err
	}
	return ControlResult{}, errors.New("SSE ended without command or keepalive")
}

func (client ControlClient) longPoll(ctx context.Context) (ControlResult, error) {
	pollCtx, cancel := context.WithTimeout(ctx, client.deadline())
	defer cancel()
	endpoint := client.endpoint("/api/viewers/"+url.PathEscape(client.ClientID)+"/commands/next") + "?wait=24"
	request, err := http.NewRequestWithContext(pollCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ControlResult{}, err
	}
	response, err := client.httpClient().Do(request)
	if err != nil {
		return ControlResult{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return ControlResult{Transport: ControlTransportLongPoll}, nil
	}
	if response.StatusCode != http.StatusOK {
		return ControlResult{}, fmt.Errorf("long poll status %s", response.Status)
	}
	var command Command
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxControlMessageBytes+1))
	if err := decoder.Decode(&command); err != nil {
		return ControlResult{}, fmt.Errorf("decode long poll command: %w", err)
	}
	if err := validateCommand(command); err != nil {
		return ControlResult{}, err
	}
	return ControlResult{Transport: ControlTransportLongPoll, Command: &command}, nil
}

func (client ControlClient) Report(ctx context.Context, command Command, state CommandState, operationKey, commandError string) error {
	reportCtx, cancel := context.WithTimeout(ctx, DefaultCommandReportDeadline)
	defer cancel()
	payload := struct {
		State        string `json:"state"`
		Error        string `json:"error,omitempty"`
		OperationKey string `json:"operationKey,omitempty"`
	}{serverCommandState(state), commandError, operationKey}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	path := "/api/viewers/" + url.PathEscape(client.ClientID) + "/commands/" + strconv.FormatInt(command.ID, 10)
	request, err := http.NewRequestWithContext(reportCtx, http.MethodPatch, client.endpoint(path), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("report command status %s", response.Status)
	}
	return nil
}

type HeartbeatPayload struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	AppVersion  string `json:"appVersion"`
	Hostname    string `json:"hostname"`
	Route       string `json:"route"`
	Mode        string `json:"mode"`
	Agent       struct {
		State   string `json:"state"`
		Version string `json:"version,omitempty"`
	} `json:"agent"`
	Control struct {
		State         string     `json:"state"`
		LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	} `json:"control"`
	Viewer struct {
		State           string     `json:"state"`
		Version         string     `json:"version,omitempty"`
		LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	} `json:"viewer"`
	Renderer struct {
		State           string     `json:"state"`
		LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
	} `json:"renderer"`
	Update struct {
		State         string `json:"state"`
		TargetVersion string `json:"targetVersion,omitempty"`
		Generation    int64  `json:"generation"`
	} `json:"update"`
}

func (client ControlClient) SendHeartbeat(ctx context.Context, heartbeat HeartbeatPayload) error {
	encoded, err := json.Marshal(heartbeat)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint("/api/viewers/heartbeat"), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("heartbeat status %s", response.Status)
	}
	return nil
}

func (client ControlClient) endpoint(path string) string {
	return strings.TrimSuffix(client.ServerURL, "/") + path
}

func (client ControlClient) httpClient() *http.Client {
	if client.HTTPClient != nil {
		return client.HTTPClient
	}
	return http.DefaultClient
}

func (client ControlClient) deadline() time.Duration {
	if client.ReadDeadline > 0 {
		return client.ReadDeadline
	}
	return DefaultControlReadDeadline
}

func validateCommand(command Command) error {
	if command.ID <= 0 || strings.TrimSpace(command.Type) == "" || strings.TrimSpace(command.PayloadHash) == "" {
		return errors.New("invalid viewer command")
	}
	return nil
}

func serverCommandState(state CommandState) string {
	if state == CommandReceived {
		return "acknowledged"
	}
	return string(state)
}
