package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"camstation/internal/camera"
	"camstation/internal/store"
)

var (
	errCameraManagementForbidden = errors.New("camera management request forbidden")
	errUnsafeCameraTarget        = errors.New("unsafe camera target")
	cameraNetworkSlots           = make(chan struct{}, 4)
)

func requireCameraManagementRequest(w http.ResponseWriter, r *http.Request) bool {
	if isTrustedCameraManagementRequest(r) {
		return true
	}
	writeSafeError(w, http.StatusForbidden, errCameraManagementForbidden)
	return false
}

func isTrustedCameraManagementRequest(r *http.Request) bool {
	if r.Header.Get("X-CamStation-Management") != "1" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" && !isTrustedConsoleOrigin(origin) {
		return false
	}
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if site == "" {
		return true
	}
	return site == "same-origin" || site == "same-site"
}

func isTrustedConsoleOrigin(rawOrigin string) bool {
	parsed, err := url.Parse(rawOrigin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func withCameraNetworkSlot(ctx context.Context) (func(), error) {
	select {
	case cameraNetworkSlots <- struct{}{}:
		return func() { <-cameraNetworkSlots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("camera operation canceled: %w", ctx.Err())
	default:
		return nil, fmt.Errorf("camera operation limit reached: %w", errUnsafeCameraTarget)
	}
}

func writeSafeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"ok": false, "error": safeCameraError(err)})
}

func safeCameraError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, errCameraManagementForbidden):
		return "camera management request forbidden"
	case errors.Is(err, errUnsafeCameraTarget):
		return "unsafe camera target"
	default:
		return "camera operation failed"
	}
}

func safeProbeResult(result camera.ProbeResult, rawURL string, failed bool) camera.ProbeResult {
	if result.URL == "" {
		result.URL = store.RedactURL(rawURL)
	} else {
		result.URL = store.RedactURL(result.URL)
	}
	if failed {
		result.Failure = "camera probe failed"
	}
	return result
}
