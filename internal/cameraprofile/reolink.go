package cameraprofile

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	reolinkClearHTTPFLVSource = "reolink-http-flv"
	reolinkClearMainToken     = "reolink-clear-main"
)

func isReolinkAdapter(adapter string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(adapter)), "reolink")
}

func appendReolinkClearHTTPFLVCandidate(req ScanRequest, candidates []StreamCandidate) []StreamCandidate {
	if req.Host == "" || req.Username == "" || hasReolinkClearHTTPFLVCandidate(candidates) {
		return candidates
	}
	main, ok := preferredReolinkMainCandidate(candidates)
	if !ok {
		return candidates
	}
	channel := reolinkClearChannel(req, main)
	clearURL := reolinkClearHTTPFLVURL(req, channel)
	clear := StreamCandidate{
		RoleHint:     StreamRoleRecording,
		Label:        "Reolink Clear main HTTP-FLV",
		Source:       reolinkClearHTTPFLVSource,
		URL:          clearURL,
		RedactedURL:  redactURL(clearURL),
		Codec:        firstNonEmptyString(strings.ToLower(main.Codec), "h264"),
		Width:        main.Width,
		Height:       main.Height,
		FPS:          main.FPS,
		BitrateKbps:  main.BitrateKbps,
		ProfileToken: reolinkClearMainToken,
	}
	out := make([]StreamCandidate, 0, len(candidates)+1)
	out = append(out, clear)
	out = append(out, candidates...)
	return out
}

func hasReolinkClearHTTPFLVCandidate(candidates []StreamCandidate) bool {
	for _, candidate := range candidates {
		if candidate.Source == reolinkClearHTTPFLVSource || candidate.ProfileToken == reolinkClearMainToken {
			return true
		}
	}
	return false
}

func preferredReolinkMainCandidate(candidates []StreamCandidate) (StreamCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.RoleHint == StreamRoleRecording {
			return candidate, true
		}
	}
	if len(candidates) == 0 {
		return StreamCandidate{}, false
	}
	return candidates[0], true
}

func reolinkClearChannel(req ScanRequest, main StreamCandidate) int {
	for _, rawURL := range []string{req.URL, main.URL} {
		channel, ok := reolinkChannelFromURL(rawURL)
		if ok {
			return channel
		}
	}
	return 0
}

func reolinkChannelFromURL(rawURL string) (int, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0, false
	}
	path := strings.ToLower(parsed.Path)
	index := strings.Index(path, "preview_")
	if index < 0 {
		return 0, false
	}
	rest := path[index+len("preview_"):]
	end := strings.IndexByte(rest, '_')
	if end <= 0 {
		return 0, false
	}
	number, err := strconv.Atoi(rest[:end])
	if err != nil || number <= 0 {
		return 0, false
	}
	return number - 1, true
}

func reolinkClearHTTPFLVURL(req ScanRequest, channel int) string {
	host := req.Host
	if req.HTTPPort != 0 && req.HTTPPort != 80 {
		host = net.JoinHostPort(req.Host, strconv.Itoa(req.HTTPPort))
	}
	query := url.Values{}
	query.Set("port", "1935")
	query.Set("app", "bcs")
	query.Set("stream", fmt.Sprintf("channel%d_main.bcs", channel))
	query.Set("user", req.Username)
	query.Set("password", req.Password)
	return (&url.URL{
		Scheme:   "http",
		Host:     host,
		Path:     "/flv",
		RawQuery: query.Encode(),
	}).String()
}

func redactQueryCredentials(parsed *url.URL) {
	query := parsed.Query()
	if len(query) == 0 {
		return
	}
	for key := range query {
		if isCredentialQueryKey(key) {
			query.Set(key, "redacted")
		}
	}
	parsed.RawQuery = query.Encode()
}

func isCredentialQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "user", "username", "password", "passwd", "pwd", "token":
		return true
	default:
		return false
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
