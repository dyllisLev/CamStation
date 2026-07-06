package main

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"camstation/internal/cameraprofile"
)

func validateScanTarget(ctx context.Context, req cameraprofile.ScanRequest) error {
	target, err := scanTarget(req)
	if err != nil {
		return err
	}
	if err := validateCameraHost(ctx, target.host); err != nil {
		return err
	}
	for _, port := range []int{target.urlPort, req.RTSPPort, req.HTTPPort, req.ONVIFPort} {
		if port != 0 && !isAllowedCameraPort(port) {
			return errUnsafeCameraTarget
		}
	}
	return nil
}

func validateProbeTarget(ctx context.Context, rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return errUnsafeCameraTarget
	}
	if parsed.User != nil || hasCredentialQuery(parsed.Query()) || !isAllowedCameraScheme(parsed.Scheme) {
		return errUnsafeCameraTarget
	}
	host := parsed.Hostname()
	if host == "" {
		return errUnsafeCameraTarget
	}
	if err := validateCameraHost(ctx, host); err != nil {
		return err
	}
	port := defaultPort(parsed)
	if port != 0 && !isAllowedCameraPort(port) {
		return errUnsafeCameraTarget
	}
	return nil
}

func scanTarget(req cameraprofile.ScanRequest) (cameraTarget, error) {
	target := cameraTarget{host: strings.TrimSpace(req.Host)}
	if req.URL == "" {
		if target.host == "" {
			return cameraTarget{}, errUnsafeCameraTarget
		}
		return target, nil
	}
	parsed, err := url.ParseRequestURI(req.URL)
	if err != nil {
		return cameraTarget{}, errUnsafeCameraTarget
	}
	if parsed.User != nil || hasCredentialQuery(parsed.Query()) || !isAllowedScanURLScheme(parsed.Scheme) {
		return cameraTarget{}, errUnsafeCameraTarget
	}
	if parsed.Hostname() == "" {
		return cameraTarget{}, errUnsafeCameraTarget
	}
	if target.host == "" {
		target.host = parsed.Hostname()
	}
	target.urlPort = defaultPort(parsed)
	return target, nil
}

func hasCredentialQuery(values url.Values) bool {
	for key := range values {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "user", "username", "password", "passwd", "pwd", "token":
			return true
		}
	}
	return false
}

type cameraTarget struct {
	host    string
	urlPort int
}

func defaultPort(parsed *url.URL) int {
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			return -1
		}
		return port
	}
	switch strings.ToLower(parsed.Scheme) {
	case "rtsp":
		return 554
	case "http":
		return 80
	case "https":
		return 443
	default:
		return 0
	}
}

func validateCameraHost(ctx context.Context, host string) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || strings.ContainsAny(host, " \t\r\n/@") {
		return errUnsafeCameraTarget
	}
	if ip := net.ParseIP(host); ip != nil {
		if isAllowedCameraIP(ip) {
			return nil
		}
		return errUnsafeCameraTarget
	}
	if !isValidDNSHost(host) {
		return errUnsafeCameraTarget
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addrs) == 0 {
		return errUnsafeCameraTarget
	}
	for _, addr := range addrs {
		if !isAllowedCameraIP(addr.IP) {
			return errUnsafeCameraTarget
		}
	}
	return nil
}

func isValidDNSHost(host string) bool {
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func isAllowedCameraIP(ip net.IP) bool {
	return ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}

func isAllowedScanURLScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "rtsp", "rtsps":
		return true
	default:
		return false
	}
}

func isAllowedCameraScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "rtsp", "rtsps", "http", "https":
		return true
	default:
		return false
	}
}

func isAllowedCameraPort(port int) bool {
	switch port {
	case 80, 443, 554, 5000, 5001, 8000, 8001, 8080, 8081, 8554, 8899, 10080, 10554, 10555:
		return true
	default:
		return false
	}
}
