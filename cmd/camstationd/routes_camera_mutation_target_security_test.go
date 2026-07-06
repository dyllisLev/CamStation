package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"camstation/internal/cameraprofile"
)

func TestCameraMutationSecurity_rejectsUnsafeRequestSuppliedStreamTargets(t *testing.T) {
	t.Parallel()

	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	seedStatus, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", cameraSecurityCreateBody(t, "mutation-target-existing", routeSyntheticRTSPURL("mutation-target-existing")), trustedConsoleHeaders())
	if seedStatus != http.StatusOK {
		t.Fatalf("seed camera status = %d, want %d", seedStatus, http.StatusOK)
	}
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "create stream public ip",
			method: http.MethodPost,
			path:   "/api/cameras",
			body:   cameraUnsafeStreamTargetBody(t, "unsafe-create-stream", "rtsp://8.8.8.8:554/tcp/av0_0"),
		},
		{
			name:   "create profile candidate credentials",
			method: http.MethodPost,
			path:   "/api/cameras",
			body:   cameraUnsafeProfileTargetBody(t, "unsafe-create-profile", "rtsp://operator:secret@192.168.1.10:554/tcp/av0_0"),
		},
		{
			name:   "update stream metadata ip",
			method: http.MethodPut,
			path:   "/api/cameras/mutation-target-existing",
			body:   cameraUnsafeStreamTargetBody(t, "unsafe-update-stream", "rtsp://169.254.169.254:80/latest/meta-data"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, payload := requestJSONWithHeaders(t, server.handler, tt.method, tt.path, tt.body, trustedConsoleHeaders())

			if status != http.StatusBadRequest {
				t.Fatalf("%s %s status = %d, want %d; body=%#v", tt.method, tt.path, status, http.StatusBadRequest, payload)
			}
			assertEncodedDoesNotContain(t, payload, "secret")
		})
	}
}

func cameraUnsafeStreamTargetBody(t *testing.T, streamName string, rawURL string) string {
	t.Helper()

	body := map[string]any{
		"name":       "Unsafe Target",
		"streamName": streamName,
		"host":       "192.168.1.10",
		"rtspPort":   554,
		"streams": []cameraprofile.StreamCandidate{{
			RoleHint:     cameraprofile.StreamRoleRecording,
			Label:        "main",
			Source:       "qa",
			URL:          rawURL,
			ProfileToken: "main",
		}},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal unsafe stream target body: %v", err)
	}
	return string(encoded)
}

func cameraUnsafeProfileTargetBody(t *testing.T, streamName string, rawURL string) string {
	t.Helper()

	body := map[string]any{
		"name":       "Unsafe Profile Target",
		"streamName": streamName,
		"host":       "192.168.1.10",
		"rtspPort":   554,
		"profile": cameraprofile.DeviceProfile{
			Channels: []cameraprofile.ChannelProfile{{
				Index: 0,
				Candidates: []cameraprofile.StreamCandidate{{
					RoleHint:     cameraprofile.StreamRoleRecording,
					Label:        "main",
					Source:       "qa",
					URL:          rawURL,
					ProfileToken: "main",
				}},
			}},
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal unsafe profile target body: %v", err)
	}
	return string(encoded)
}
