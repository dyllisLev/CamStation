package onvif

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestClientCallUsesDigestServicePathAndEscapedBody(t *testing.T) {
	const password = "camera-secret"
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != string(ServicePTZ) {
			t.Errorf("path = %q, want %q", r.URL.Path, ServicePTZ)
		}
		payload, _ := io.ReadAll(r.Body)
		requestBody = string(payload)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(`<Envelope><Body><ok/></Body></Envelope>`))
	}))
	defer server.Close()

	_, err := NewClient(server.Client()).Call(
		context.Background(),
		targetFromServerURL(t, server.URL, "operator", password),
		ServicePTZ,
		"http://www.onvif.org/ver20/ptz/wsdl/GetNodes",
		`<tptz:GetNodes><tt:Name>`+Escape(`A&B`)+`</tt:Name></tptz:GetNodes>`,
	)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if strings.Contains(requestBody, password) {
		t.Fatal("SOAP envelope leaked the plain password")
	}
	for _, required := range []string{"PasswordDigest", "Nonce", "Created", "A&amp;B"} {
		if !strings.Contains(requestBody, required) {
			t.Fatalf("request missing %q", required)
		}
	}
}

func TestClientCallRejectsAuthenticationWithoutReturningPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `fault camera-secret`, http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := NewClient(server.Client()).Call(
		context.Background(),
		targetFromServerURL(t, server.URL, "u", "p"),
		ServiceDevice,
		"action",
		`<tds:GetDeviceInformation/>`,
	)
	if !errors.Is(err, ErrAuthenticationFailed) || strings.Contains(err.Error(), "camera-secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func targetFromServerURL(t *testing.T, rawURL, username, password string) Target {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}
	return Target{Host: parsed.Hostname(), Port: port, Username: username, Password: password}
}
