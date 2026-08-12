package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"camstation/internal/cameraprofile"
	"camstation/internal/store"
)

func cameraSecurityCreateBody(t *testing.T, streamName string, rawURL string) string {
	t.Helper()

	body := map[string]any{
		"name":       streamName,
		"streamName": streamName,
		"url":        rawURL,
		"profile": cameraprofile.DeviceProfile{
			Host:         "192.168.1.10",
			Manufacturer: "Synthetic",
			Model:        "Security",
			Adapter:      "onvif",
			Channels: []cameraprofile.ChannelProfile{{
				Index: 0,
				Candidates: []cameraprofile.StreamCandidate{{
					RoleHint:     cameraprofile.StreamRoleRecording,
					Label:        "main",
					Source:       "qa",
					URL:          rawURL,
					RedactedURL:  store.RedactURL(rawURL),
					ProfileToken: "main",
				}},
			}},
		},
		"streams": []cameraprofile.StreamCandidate{{
			RoleHint:     cameraprofile.StreamRoleRecording,
			Label:        "main",
			Source:       "qa",
			URL:          rawURL,
			RedactedURL:  store.RedactURL(rawURL),
			ProfileToken: "main",
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal camera security body: %v", err)
	}
	return string(encoded)
}

func trustedConsoleHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Origin", "http://127.0.0.1")
	headers.Set("Sec-Fetch-Site", "same-origin")
	headers.Set("X-CamStation-Management", "1")
	return headers
}

func requestJSONWithHeaders(t *testing.T, handler http.Handler, method string, target string, body string, headers http.Header) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var payload map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %s %s response: %v; body=%s", method, target, err, rec.Body.String())
		}
	}
	return rec.Code, payload
}
