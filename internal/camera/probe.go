package camera

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
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
	Index       int     `json:"index"`
	Type        string  `json:"type"`
	Codec       string  `json:"codec"`
	Profile     string  `json:"profile,omitempty"`
	Level       string  `json:"level,omitempty"`
	PixelFormat string  `json:"pixelFormat,omitempty"`
	BitDepth    int     `json:"bitDepth,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	FrameRate   string  `json:"frameRate,omitempty"`
	FPS         float64 `json:"fps,omitempty"`
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
		"-show_entries", "format=format_name:stream=index,codec_type,codec_name,profile,level,pix_fmt,bits_per_raw_sample,width,height,avg_frame_rate",
		"-of", "json",
	}
	isRTSP := false
	if parsed, _ := url.Parse(rawURL); parsed != nil && (parsed.Scheme == "rtsp" || parsed.Scheme == "rtsps") {
		isRTSP = true
		args = append(args, "-rtsp_transport", "tcp")
	}
	args = append(args, rawURL)
	cmd := exec.CommandContext(probeCtx, "ffprobe", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Duration = time.Since(start).Round(time.Millisecond)
	if isRTSP {
		result.TransportHint = "tcp"
	}
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

	parsed, err := parseFFProbePayload(&stdout)
	if err != nil {
		result.Failure = "could not parse ffprobe output"
		return result, err
	}
	result.Format = parsed.Format
	result.Streams = parsed.Streams
	result.Reachable = true
	return result, nil
}

func parseFFProbePayload(reader io.Reader) (ProbeResult, error) {
	var payload struct {
		Format struct {
			FormatName string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			Index            int    `json:"index"`
			CodecType        string `json:"codec_type"`
			CodecName        string `json:"codec_name"`
			Profile          string `json:"profile"`
			Level            int    `json:"level"`
			PixelFormat      string `json:"pix_fmt"`
			BitsPerRawSample string `json:"bits_per_raw_sample"`
			Width            int    `json:"width"`
			Height           int    `json:"height"`
			AvgFrameRate     string `json:"avg_frame_rate"`
		} `json:"streams"`
	}
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		return ProbeResult{}, err
	}
	result := ProbeResult{Format: payload.Format.FormatName}
	for _, stream := range payload.Streams {
		bitDepth, _ := strconv.Atoi(stream.BitsPerRawSample)
		if bitDepth == 0 {
			bitDepth = pixelFormatBitDepth(stream.PixelFormat)
		}
		result.Streams = append(result.Streams, Stream{
			Index: stream.Index, Type: stream.CodecType, Codec: stream.CodecName,
			Profile: stream.Profile, Level: formatCodecLevel(stream.Level), PixelFormat: stream.PixelFormat,
			BitDepth: bitDepth, Width: stream.Width, Height: stream.Height,
			FrameRate: stream.AvgFrameRate, FPS: parseFrameRate(stream.AvgFrameRate),
		})
	}
	return result, nil
}

func pixelFormatBitDepth(pixelFormat string) int {
	pixelFormat = strings.ToLower(pixelFormat)
	for _, depth := range []int{16, 14, 12, 10, 9} {
		if strings.Contains(pixelFormat, "p"+strconv.Itoa(depth)) {
			return depth
		}
	}
	if pixelFormat != "" {
		return 8
	}
	return 0
}

func parseFrameRate(value string) float64 {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		fps, _ := strconv.ParseFloat(value, 64)
		return fps
	}
	numerator, _ := strconv.ParseFloat(parts[0], 64)
	denominator, _ := strconv.ParseFloat(parts[1], 64)
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func formatCodecLevel(level int) string {
	if level == 0 {
		return ""
	}
	if level >= 90 {
		return fmt.Sprintf("%.1f", float64(level)/30)
	}
	return fmt.Sprintf("%.1f", float64(level)/10)
}

func RedactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("redacted", "redacted")
	}
	query := parsed.Query()
	for key := range query {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "user", "username", "password", "passwd", "pwd", "token":
			query.Set(key, "redacted")
		}
	}
	parsed.RawQuery = query.Encode()
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
