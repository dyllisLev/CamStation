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

var (
	ErrControlInactivity = errors.New("control stream inactive")
	errSSEFrameComplete  = errors.New("SSE frame complete")
)

type ReconnectState struct {
	failures int
	delays   []time.Duration
}

func (state *ReconnectState) NextDelay() time.Duration {
	delays := state.delays
	if len(delays) == 0 {
		delays = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 5 * time.Minute}
	}
	if state.failures >= len(delays) {
		state.failures++
		return delays[len(delays)-1]
	}
	delay := delays[state.failures]
	state.failures++
	return delay
}

func (state *ReconnectState) Reset() { state.failures = 0 }

func (state *ReconnectState) ObserveSSESession(frames int) {
	if frames >= 2 {
		state.Reset()
	}
}

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
	Proven    bool
}

type ControlClient struct {
	HTTPClient   *http.Client
	ServerURL    string
	ClientID     string
	ReadDeadline time.Duration
}

func (client ControlClient) Next(ctx context.Context) (ControlResult, error) {
	var result ControlResult
	_, err := client.StreamSSE(ctx, func(frame ControlResult) error {
		result = frame
		return errSSEFrameComplete
	})
	if errors.Is(err, errSSEFrameComplete) {
		return result, nil
	}
	if ctx.Err() != nil {
		return ControlResult{}, ctx.Err()
	}
	return client.longPoll(ctx)
}

func (client ControlClient) StreamSSE(ctx context.Context, onFrame func(ControlResult) error) (int, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	request, err := http.NewRequestWithContext(streamCtx, http.MethodGet, client.endpoint("/api/viewers/"+url.PathEscape(client.ClientID)+"/control"), nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := client.httpClient().Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		return 0, fmt.Errorf("SSE status %s", response.Status)
	}
	frames := make(chan sseFrame, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanSSE(streamCtx, response.Body, frames)
	}()
	defer func() {
		cancel()
		_ = response.Body.Close()
		<-done
	}()

	timer := time.NewTimer(client.deadline())
	defer timer.Stop()
	seen := 0
	for {
		select {
		case <-ctx.Done():
			return seen, ctx.Err()
		case <-timer.C:
			return seen, ErrControlInactivity
		case frame, ok := <-frames:
			if !ok {
				return seen, errors.New("SSE ended")
			}
			if frame.err != nil {
				return seen, frame.err
			}
			seen++
			resetTimer(timer, client.deadline())
			if err := onFrame(frame.result); err != nil {
				return seen, err
			}
		}
	}
}

type sseFrame struct {
	result ControlResult
	err    error
}

func scanSSE(ctx context.Context, reader io.Reader, frames chan<- sseFrame) {
	defer close(frames)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), maxControlMessageBytes)
	keepalive := false
	data := make([]string, 0, 1)
	send := func(frame sseFrame) bool {
		select {
		case frames <- frame:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			if strings.HasPrefix(line, ":") {
				keepalive = true
			} else if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
			continue
		}
		if keepalive && len(data) == 0 {
			if !send(sseFrame{result: ControlResult{Transport: ControlTransportSSE, Proven: true}}) {
				return
			}
		} else if len(data) > 0 {
			var command Command
			if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &command); err != nil {
				send(sseFrame{err: fmt.Errorf("decode SSE command: %w", err)})
				return
			}
			if err := validateCommand(command); err != nil {
				send(sseFrame{err: err})
				return
			}
			if !send(sseFrame{result: ControlResult{Transport: ControlTransportSSE, Command: &command, Proven: true}}) {
				return
			}
		}
		keepalive = false
		data = data[:0]
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		send(sseFrame{err: err})
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
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
		return ControlResult{Transport: ControlTransportLongPoll, Proven: true}, nil
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
	return ControlResult{Transport: ControlTransportLongPoll, Command: &command, Proven: true}, nil
}

func (client ControlClient) RunControl(ctx context.Context, probes *ReconnectState, onResult func(ControlResult) error) error {
	if probes == nil {
		probes = &ReconnectState{}
	}
	for {
		var callbackErr error
		frames, _ := client.StreamSSE(ctx, func(result ControlResult) error {
			callbackErr = onResult(result)
			return callbackErr
		})
		if callbackErr != nil {
			return callbackErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		probes.ObserveSSESession(frames)
		delay := probes.NextDelay()
		if err := onResult(ControlResult{Transport: ControlTransportLongPoll}); err != nil {
			return err
		}
		probeAt := time.Now().Add(delay)
		for time.Until(probeAt) > 0 {
			pollCtx, cancel := context.WithDeadline(ctx, probeAt)
			result, pollErr := client.longPoll(pollCtx)
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if pollErr == nil {
				if err := onResult(result); err != nil {
					return err
				}
				if result.Command == nil {
					remaining := time.Until(probeAt)
					if remaining > time.Second {
						remaining = time.Second
					}
					if remaining > 0 && waitContext(ctx, remaining) != nil {
						return ctx.Err()
					}
				}
				continue
			}
			remaining := time.Until(probeAt)
			if remaining <= 0 {
				break
			}
			if remaining > time.Second {
				remaining = time.Second
			}
			if waitContext(ctx, remaining) != nil {
				return ctx.Err()
			}
		}
	}
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
