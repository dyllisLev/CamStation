package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"camstation/internal/camera"
	"camstation/internal/store"
)

type cameraSecurityProbe struct {
	calls int32
}

func (p *cameraSecurityProbe) Probe(_ context.Context, rawURL string, _ time.Duration) (camera.ProbeResult, error) {
	atomic.AddInt32(&p.calls, 1)
	return camera.ProbeResult{
		URL:       store.RedactURL(rawURL),
		Reachable: false,
		Failure:   "dial " + rawURL + ": secret-password",
		CheckedAt: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
	}, errors.New("probe failed for " + rawURL + " with secret-password")
}

func TestCameraManagementSecurity_rejectsMissingGuardForMutations_whenNoTrustedConsoleHeaders(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"scan", http.MethodPost, "/api/cameras/scan", `{"host":"192.168.1.10"}`},
		{"preview", http.MethodPost, "/api/cameras/preview", `{"host":"192.168.1.10","role":"recording","profileToken":"main"}`},
		{"create", http.MethodPost, "/api/cameras", cameraSecurityCreateBody(t, "guard-create", routeSyntheticRTSPURL("guard-create"))},
		{"update scan", http.MethodPost, "/api/cameras/guard-existing/scan", `{"host":"192.168.1.10"}`},
		{"update preview", http.MethodPost, "/api/cameras/guard-existing/preview", `{"host":"192.168.1.10","role":"recording","profileToken":"main"}`},
		{"update", http.MethodPut, "/api/cameras/guard-existing", cameraSecurityCreateBody(t, "guard-existing", routeSyntheticRTSPURL("guard-update"))},
		{"delete", http.MethodDelete, "/api/cameras/guard-existing", ``},
		{"probe", http.MethodPost, "/api/camera/probe", `{"url":"rtsp://192.168.1.10:554/main"}`},
	}
	seedStatus, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", cameraSecurityCreateBody(t, "guard-existing", routeSyntheticRTSPURL("guard-existing")), trustedConsoleHeaders())
	if seedStatus != http.StatusOK {
		t.Fatalf("seed camera status = %d, want %d", seedStatus, http.StatusOK)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			status, payload := requestJSONWithHeaders(t, server.handler, tt.method, tt.path, tt.body, nil)

			// Then
			if status != http.StatusForbidden {
				t.Fatalf("%s %s status = %d, want %d; body=%#v", tt.method, tt.path, status, http.StatusForbidden, payload)
			}
		})
	}
}

func TestCameraManagementSecurity_rejectsInvalidGuard_whenOriginIsUntrusted(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	headers := trustedConsoleHeaders()
	headers.Set("Origin", "https://evil.example")

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/camera/probe", `{"url":"rtsp://192.168.1.10:554/main"}`, headers)

	// Then
	if status != http.StatusForbidden {
		t.Fatalf("untrusted origin status = %d, want %d; body=%#v", status, http.StatusForbidden, payload)
	}
}

func TestCameraManagementSecurity_allowsPrivateLANConsoleOrigin(t *testing.T) {
	t.Parallel()

	// Given
	req := httptest.NewRequest(http.MethodPost, "http://192.168.0.20:18080/api/cameras/scan", strings.NewReader(`{}`))
	req.Header.Set("X-CamStation-Management", "1")
	req.Header.Set("Origin", "http://192.168.0.20:18080")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	if !isTrustedCameraManagementRequest(req) {
		t.Fatalf("private LAN same-origin console request was rejected")
	}
}

func TestCameraManagementSecurity_allowsManagementHeader_whenFetchMetadataIsAbsent(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPut, "http://192.168.0.20:18080/api/cameras/goat-yard", strings.NewReader(`{}`))
	req.Header.Set("X-CamStation-Management", "1")

	if !isTrustedCameraManagementRequest(req) {
		t.Fatalf("management request without Sec-Fetch-Site was rejected")
	}
}

func TestCameraProbeSecurity_rejectsUnsafeTargets_whenTargetIsPublicMetadataMalformedOrCredentialed(t *testing.T) {
	t.Parallel()

	// Given
	prober := &cameraSecurityProbe{}
	server := newCameraMutationRouteServer(t, prober)
	tests := []struct {
		name string
		body string
	}{
		{"public ip", `{"url":"rtsp://8.8.8.8:554/main"}`},
		{"metadata ip", `{"url":"http://169.254.169.254:80/latest/meta-data"}`},
		{"link local", `{"url":"rtsp://169.254.1.10:554/main"}`},
		{"unsafe scheme", `{"url":"file://192.168.1.10/etc/passwd"}`},
		{"disallowed port", `{"url":"rtsp://192.168.1.10:22/main"}`},
		{"url credentials", `{"url":"rtsp://operator:secret@192.168.1.10:554/main"}`},
		{"query credentials", `{"url":"rtsp://192.168.1.10:554/main?user=operator&password=query-secret&token=query-token"}`},
		{"malformed host", `{"url":"rtsp://bad host:554/main"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/camera/probe", tt.body, trustedConsoleHeaders())

			// Then
			if status != http.StatusBadRequest {
				t.Fatalf("probe status = %d, want %d; body=%#v", status, http.StatusBadRequest, payload)
			}
		})
	}
	if got := atomic.LoadInt32(&prober.calls); got != 0 {
		t.Fatalf("unsafe target reached prober %d times, want 0", got)
	}
}

func TestCameraScanSecurity_rejectsUnsafeTargets_whenHostOrPortsAreUnsafe(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	tests := []struct {
		name string
		body string
	}{
		{"public host", `{"host":"8.8.8.8","rtspPort":554,"httpPort":80,"onvifPort":80}`},
		{"metadata host", `{"host":"169.254.169.254","rtspPort":554,"httpPort":80,"onvifPort":80}`},
		{"malformed host", `{"host":"bad host","rtspPort":554,"httpPort":80,"onvifPort":80}`},
		{"unsafe url scheme", `{"url":"http://192.168.1.10:80/onvif","host":"192.168.1.10"}`},
		{"embedded credentials", `{"url":"rtsp://operator:secret@192.168.1.10:554/main"}`},
		{"query credentials", `{"url":"rtsp://192.168.1.10:554/main?user=operator&password=query-secret","host":"192.168.1.10"}`},
		{"disallowed rtsp port", `{"host":"192.168.1.10","rtspPort":22,"httpPort":80,"onvifPort":80}`},
		{"disallowed onvif port", `{"host":"192.168.1.10","rtspPort":554,"httpPort":80,"onvifPort":22}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/scan", tt.body, trustedConsoleHeaders())

			// Then
			if status != http.StatusBadRequest {
				t.Fatalf("scan status = %d, want %d; body=%#v", status, http.StatusBadRequest, payload)
			}
		})
	}
}

func TestCameraPreviewSecurity_rejectsRegisteredPreview_whenSavedCredentialsWouldBeReused(t *testing.T) {
	t.Parallel()

	// Given
	server := newCameraMutationRouteServer(t, &fakeRouteCameraProber{})
	secretURL := routeSyntheticRTSPURL("registered-preview")
	body := cameraSecurityCreateBody(t, "registered-preview", secretURL)
	createStatus, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras", body, trustedConsoleHeaders())
	if createStatus != http.StatusOK {
		t.Fatalf("seed camera status = %d, want %d", createStatus, http.StatusOK)
	}

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/registered-preview/preview", `{"role":"recording","profileToken":"main"}`, trustedConsoleHeaders())

	// Then
	if status != http.StatusBadRequest {
		t.Fatalf("registered preview status = %d, want %d; body=%#v", status, http.StatusBadRequest, payload)
	}
	assertEncodedDoesNotContain(t, payload, secretURL)
}

func TestRedactLastScan_removesQueryCredentials_whenPublicCameraIncludesScanMetadata(t *testing.T) {
	t.Parallel()

	// Given
	secretURL := "rtsp://192.168.1.10:554/main?user=operator&password=query-secret&token=query-token"
	camera := store.Camera{
		ID:          1,
		Name:        "scan-redaction",
		StreamName:  "scan-redaction",
		RedactedURL: store.RedactURL(secretURL),
		State:       "streaming",
		LastScanJSON: map[string]any{
			"stream": secretURL,
			"nested": map[string]any{"candidate": secretURL},
		},
		CreatedAt: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
	}

	// When
	public := publicCameraFromStore(camera)

	// Then
	encoded := mustMarshalString(t, public)
	for _, forbidden := range []string{"operator", "query-secret", "query-token"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("public lastScan leaked query credential fragment %q in %s", forbidden, encoded)
		}
	}
}

func TestCameraProbeSecurity_rejectsQueryCredentialURLBeforeProbe(t *testing.T) {
	t.Parallel()

	// Given
	secretURL := "rtsp://192.168.1.10:554/main?user=operator&password=query-secret&token=query-token"
	prober := &cameraSecurityProbe{}
	server := newCameraMutationRouteServer(t, prober)

	// When
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/camera/probe", `{"url":"`+secretURL+`"}`, trustedConsoleHeaders())

	// Then
	if status != http.StatusBadRequest {
		t.Fatalf("probe status = %d, want %d; body=%#v", status, http.StatusBadRequest, payload)
	}
	if got := atomic.LoadInt32(&prober.calls); got != 0 {
		t.Fatalf("query credential target reached prober %d times, want 0", got)
	}
	encoded := mustMarshalString(t, payload)
	for _, forbidden := range []string{"operator", "query-secret", "query-token", "secret-password"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("probe response leaked %q in %s", forbidden, encoded)
		}
	}
}
