package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestViewerReleaseRoutesServeVerifiedInstaller(t *testing.T) {
	server := newTestRouteServer(t)
	releaseDir := filepath.Join(filepath.Dir(server.recordingsDir), "viewer-releases", "current")
	artifact := []byte("installer")
	digest := publishViewerFixture(t, releaseDir, artifact)

	status, metadata := requestJSON(t, server.handler, http.MethodGet, "/api/viewers/app/version", "")
	if status != http.StatusOK || metadata["downloadUrl"] != "/api/viewers/app/download" {
		t.Fatalf("metadata = %d %#v", status, metadata)
	}
	if metadata["version"] != "2.0.0-dev.1" || metadata["filename"] != "CamStationViewerSetup.exe" || metadata["sha256"] != digest {
		t.Fatalf("metadata fields = %#v", metadata)
	}
	if metadata["sizeBytes"] != float64(len(artifact)) || metadata["publishedAt"] != "2026-07-16T01:02:03Z" || metadata["developmentUnsigned"] != true {
		t.Fatalf("metadata release details = %#v", metadata)
	}
	metadataResponse := performRequest(t, server.handler, http.MethodGet, "/api/viewers/app/version")
	if got := metadataResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("metadata Cache-Control = %q, want no-store", got)
	}

	response := performRequest(t, server.handler, http.MethodGet, "/api/viewers/app/download")
	if response.Code != http.StatusOK {
		t.Fatalf("download status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="CamStationViewerSetup.exe"` {
		t.Fatalf("content disposition = %q", got)
	}
	if got := response.Header().Get("Content-Type"); got != "application/vnd.microsoft.portable-executable" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(len(artifact)) {
		t.Fatalf("content length = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
	if got := response.Body.String(); got != string(artifact) {
		t.Fatalf("download body = %q", got)
	}
}

func TestViewerReleaseRoutesReturnServiceUnavailableWithoutValidRelease(t *testing.T) {
	server := newTestRouteServer(t)
	releaseDir := filepath.Join(filepath.Dir(server.recordingsDir), "viewer-releases", "current")

	for _, target := range []string{"/api/viewers/app/version", "/api/viewers/app/download"} {
		response := performRequest(t, server.handler, http.MethodGet, target)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s status = %d, want %d; body=%s", target, response.Code, http.StatusServiceUnavailable, response.Body.String())
		}
		if strings.Contains(response.Body.String(), releaseDir) {
			t.Fatalf("GET %s leaked release directory in %q", target, response.Body.String())
		}
		if target == "/api/viewers/app/version" && response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET %s Cache-Control = %q, want no-store", target, response.Header().Get("Cache-Control"))
		}
	}

	publishViewerFixture(t, releaseDir, []byte("installer"))
	manifestPath := filepath.Join(releaseDir, "release.json")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read release manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(strings.Replace(string(manifest), `"sha256":"`, `"sha256":"00`, 1)), 0o644); err != nil {
		t.Fatalf("corrupt release manifest: %v", err)
	}
	response := performRequest(t, server.handler, http.MethodGet, "/api/viewers/app/version")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("invalid release status = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	if strings.Contains(response.Body.String(), releaseDir) {
		t.Fatalf("invalid release leaked release directory in %q", response.Body.String())
	}
}

func publishViewerFixture(t *testing.T, dir string, artifact []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create viewer release dir: %v", err)
	}
	digest := sha256.Sum256(artifact)
	digestHex := hex.EncodeToString(digest[:])
	manifest := struct {
		Version             string    `json:"version"`
		Filename            string    `json:"filename"`
		SizeBytes           int64     `json:"sizeBytes"`
		SHA256              string    `json:"sha256"`
		PublishedAt         time.Time `json:"publishedAt"`
		DevelopmentUnsigned bool      `json:"developmentUnsigned"`
	}{
		Version:             "2.0.0-dev.1",
		Filename:            "CamStationViewerSetup.exe",
		SizeBytes:           int64(len(artifact)),
		SHA256:              digestHex,
		PublishedAt:         time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC),
		DevelopmentUnsigned: true,
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal viewer release manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), encoded, 0o644); err != nil {
		t.Fatalf("write viewer release manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.Filename), artifact, 0o644); err != nil {
		t.Fatalf("write viewer release artifact: %v", err)
	}
	return digestHex
}

func performRequest(t *testing.T, handler http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
