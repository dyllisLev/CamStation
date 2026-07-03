package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type alertDoerFunc func(*http.Request) (*http.Response, error)

func (f alertDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSettingsTestAlertAPI_SendsDiscordWebhookWithoutLeakingSecret(t *testing.T) {
	// Given
	server := newTestRouteServer(t)
	secret := routeTestDiscordWebhookURL()
	_, settings := requestJSON(t, server.handler, http.MethodPut, "/api/settings", `{"alerts":{"discordEnabled":true,"discordWebhookUrl":"`+secret+`"}}`)
	if settings["alerts"] == nil {
		t.Fatalf("settings update failed: %#v", settings)
	}
	var sent *http.Request
	previous := alertHTTPClient
	alertHTTPClient = alertDoerFunc(func(req *http.Request) (*http.Response, error) {
		sent = req
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() {
		alertHTTPClient = previous
	})

	// When
	status, body := requestJSON(t, server.handler, http.MethodPost, "/api/settings/test-alert", `{}`)

	// Then
	if status != http.StatusOK {
		t.Fatalf("test alert status = %d, want %d; body=%#v", status, http.StatusOK, body)
	}
	if sent == nil || sent.Method != http.MethodPost || sent.URL.String() != secret {
		t.Fatalf("discord request = %#v", sent)
	}
	assertPublicPayloadDoesNotContain(t, body, secret)
}
