package cameraprofile

import (
	"encoding/xml"
	"fmt"
	"math"
	"net/url"
	"strings"
)

func derivedVStarcamURI(req ScanRequest, index int) string {
	if req.Host == "" {
		return ""
	}
	port := req.RTSPPort
	if port == 0 {
		port = 10554
	}
	path := "/tcp/av0_0"
	if index > 0 {
		path = "/tcp/av0_1"
	}
	return fmt.Sprintf("rtsp://%s:%d%s", req.Host, port, path)
}

func withCredentials(rawURL, username, password string) string {
	if rawURL == "" || username == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil {
		return rawURL
	}
	if password != "" {
		parsed.User = url.UserPassword(username, password)
	} else {
		parsed.User = url.User(username)
	}
	return parsed.String()
}

func redactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("redacted", "redacted")
	}
	redactQueryCredentials(parsed)
	return parsed.String()
}

func textByLocalName(raw, name string) string {
	decoder := xml.NewDecoder(strings.NewReader(xmlDocument(raw)))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != name {
			continue
		}
		var text string
		if err := decoder.DecodeElement(&text, &start); err != nil {
			return ""
		}
		return strings.TrimSpace(text)
	}
}

func xmlDocument(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "<?xml") ||
		strings.Contains(trimmed, ":Envelope") ||
		strings.Contains(trimmed, "<Envelope") {
		return trimmed
	}
	return "<root>" + raw + "</root>"
}

func roundFPS(value float64) float64 {
	if value == 0 {
		return 0
	}
	return math.Round(value*100) / 100
}
