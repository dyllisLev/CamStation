package camera

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

type Prober interface {
	Probe(ctx context.Context, rawURL string, timeout time.Duration) (ProbeResult, error)
}

type FFProbe struct{}

type ProbeResult struct {
	URL           string        `json:"url"`
	Reachable     bool          `json:"reachable"`
	Duration      time.Duration `json:"duration"`
	Format        string        `json:"format,omitempty"`
	Streams       []Stream      `json:"streams,omitempty"`
	Failure       string        `json:"failure,omitempty"`
	CheckedAt     time.Time     `json:"checkedAt"`
	TransportHint string        `json:"transportHint,omitempty"`
}

type Stream struct {
	Index     int    `json:"index"`
	Type      string `json:"type"`
	Codec     string `json:"codec"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	FrameRate string `json:"frameRate,omitempty"`
}

func NewFFProbe() FFProbe {
	return FFProbe{}
}

func (FFProbe) Probe(ctx context.Context, rawURL string, timeout time.Duration) (ProbeResult, error) {
	start := time.Now()
	result := ProbeResult{
		URL:       RedactURL(rawURL),
		CheckedAt: start.UTC(),
	}

	if _, err := url.ParseRequestURI(rawURL); err != nil {
		result.Failure = "invalid camera URL"
		return result, err
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{
		"-v", "error",
		"-rtsp_transport", "tcp",
		"-show_entries", "format=format_name:stream=index,codec_type,codec_name,width,height,avg_frame_rate",
		"-of", "json",
		rawURL,
	}
	cmd := exec.CommandContext(probeCtx, "ffprobe", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Duration = time.Since(start).Round(time.Millisecond)
	result.TransportHint = "tcp"
	if probeCtx.Err() != nil {
		result.Failure = "probe timed out"
		return result, probeCtx.Err()
	}
	if err != nil {
		result.Failure = RedactText(strings.TrimSpace(stderr.String()), rawURL)
		if result.Failure == "" {
			result.Failure = RedactText(err.Error(), rawURL)
		}
		return result, fmt.Errorf("ffprobe failed: %s", result.Failure)
	}

	var payload struct {
		Format struct {
			FormatName string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			Index        int    `json:"index"`
			CodecType    string `json:"codec_type"`
			CodecName    string `json:"codec_name"`
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			AvgFrameRate string `json:"avg_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		result.Failure = "could not parse ffprobe output"
		return result, err
	}

	result.Reachable = true
	result.Format = payload.Format.FormatName
	for _, stream := range payload.Streams {
		result.Streams = append(result.Streams, Stream{
			Index:     stream.Index,
			Type:      stream.CodecType,
			Codec:     stream.CodecName,
			Width:     stream.Width,
			Height:    stream.Height,
			FrameRate: stream.AvgFrameRate,
		})
	}
	return result, nil
}

func RedactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("redacted", "redacted")
	}
	return parsed.String()
}

func RedactText(text string, secrets ...string) string {
	redacted := text
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		redacted = strings.ReplaceAll(redacted, secret, RedactURL(secret))
	}
	return redacted
}
