package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"camstation/internal/store"
)

type alertDoer interface {
	Do(*http.Request) (*http.Response, error)
}

var alertHTTPClient alertDoer = &http.Client{Timeout: 10 * time.Second}

func (d routeDeps) registerAlertRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/settings/test-alert", func(w http.ResponseWriter, r *http.Request) {
		result, err := d.sendTestAlert(r.Context())
		if err != nil {
			status := http.StatusBadGateway
			if isAlertConfigurationError(err) {
				status = http.StatusConflict
			}
			_ = d.db.AppendEvent(r.Context(), store.Event{
				Source:  "alerts",
				Level:   "error",
				Message: "alert test failed",
				Details: map[string]any{"provider": "discord"},
			})
			writeError(w, status, err)
			return
		}
		_ = d.db.AppendEvent(r.Context(), store.Event{
			Source:  "alerts",
			Level:   "info",
			Message: "alert test sent",
			Details: map[string]any{"provider": "discord"},
		})
		writeJSON(w, http.StatusOK, result)
	})
}

func (d routeDeps) sendTestAlert(ctx context.Context) (map[string]any, error) {
	settings, err := d.db.GetAlertDeliverySettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.DiscordEnabled {
		return nil, alertConfigurationError("discord alerts are disabled")
	}
	if settings.DiscordWebhookURL == "" {
		return nil, alertConfigurationError("discord webhook is not configured")
	}
	if err := sendDiscordTestAlert(ctx, settings.DiscordWebhookURL); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":       true,
		"provider": "discord",
		"webhook":  settings.DiscordWebhook,
		"sentAt":   time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func sendDiscordTestAlert(ctx context.Context, webhookURL string) error {
	payload := map[string]any{
		"content": "CamStation 알림 테스트입니다.",
		"allowed_mentions": map[string]any{
			"parse": []string{},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode alert test: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build alert test request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := alertHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("send discord alert test: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("discord alert test failed with status %d", resp.StatusCode)
	}
	return nil
}

type alertConfigurationError string

func (e alertConfigurationError) Error() string {
	return string(e)
}

func isAlertConfigurationError(err error) bool {
	_, ok := err.(alertConfigurationError)
	return ok
}
