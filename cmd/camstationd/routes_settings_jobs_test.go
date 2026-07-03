package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func requestJSON(t *testing.T, handler http.Handler, method, target, body string) (int, map[string]any) {
	t.Helper()

	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
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

func TestSettingsAPI_UpdateReadReset_masksDiscordSecret(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	secret := routeTestDiscordWebhookURL()
	body := `{"recording":{"segmentMinutes":10,"retentionDays":21,"maxStorageGB":512.5},"backup":{"enabled":true,"target":"gdrive:/cctvTest","retentionDays":14},"alerts":{"discordEnabled":true,"discordWebhookUrl":"` + secret + `"}}`

	// When
	status, updated := requestJSON(t, server.handler, http.MethodPut, "/api/settings", body)

	// Then
	if status != http.StatusOK {
		t.Fatalf("PUT /api/settings status = %d, want %d; body=%#v", status, http.StatusOK, updated)
	}
	assertPublicPayloadDoesNotContain(t, updated, secret)
	writeAPIEvidence(t, "settings-update.json", map[string]any{"status": status, "body": updated})
	alerts, ok := updated["alerts"].(map[string]any)
	if !ok {
		t.Fatalf("alerts response missing: %#v", updated)
	}
	discord, ok := alerts["discordWebhook"].(map[string]any)
	if !ok {
		t.Fatalf("discordWebhook response missing: %#v", alerts)
	}
	if discord["hasSecret"] != true || discord["masked"] == "" || discord["fingerprint"] == "" {
		t.Fatalf("discord mask = %#v", discord)
	}

	status, read := requestJSON(t, server.handler, http.MethodGet, "/api/settings", "")
	if status != http.StatusOK {
		t.Fatalf("GET /api/settings status = %d, want %d; body=%#v", status, http.StatusOK, read)
	}
	assertPublicPayloadDoesNotContain(t, read, secret)
	writeAPIEvidence(t, "settings-read.json", map[string]any{"status": status, "body": read})

	status, reset := requestJSON(t, server.handler, http.MethodPost, "/api/settings/reset", `{}`)
	if status != http.StatusOK {
		t.Fatalf("POST /api/settings/reset status = %d, want %d; body=%#v", status, http.StatusOK, reset)
	}
	writeAPIEvidence(t, "settings-reset.json", map[string]any{"status": status, "body": reset})
	alerts, ok = reset["alerts"].(map[string]any)
	if !ok {
		t.Fatalf("reset alerts missing: %#v", reset)
	}
	discord, ok = alerts["discordWebhook"].(map[string]any)
	if !ok {
		t.Fatalf("reset discordWebhook missing: %#v", alerts)
	}
	if discord["hasSecret"] != false {
		t.Fatalf("reset discord hasSecret = %v, want false", discord["hasSecret"])
	}
}

func TestSettingsAPI_InvalidWebhookAndNegativeRetention_returnBadRequest(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)

	// When
	webhookStatus, webhookBody := requestJSON(t, server.handler, http.MethodPut, "/api/settings", `{"alerts":{"discordWebhookUrl":"https://example.test/not-discord"}}`)
	retentionStatus, retentionBody := requestJSON(t, server.handler, http.MethodPut, "/api/settings", `{"recording":{"segmentMinutes":10,"retentionDays":-1,"maxStorageGB":1}}`)

	// Then
	if webhookStatus != http.StatusBadRequest {
		t.Fatalf("invalid webhook status = %d, want %d; body=%#v", webhookStatus, http.StatusBadRequest, webhookBody)
	}
	if retentionStatus != http.StatusBadRequest {
		t.Fatalf("negative retention status = %d, want %d; body=%#v", retentionStatus, http.StatusBadRequest, retentionBody)
	}
	writeAPIEvidence(t, "settings-invalid-webhook.json", map[string]any{"status": webhookStatus, "body": webhookBody})
	writeAPIEvidence(t, "settings-negative-retention.json", map[string]any{"status": retentionStatus, "body": retentionBody})
}

func TestJobsAPI_CreateTransitionAndRejectDuplicateActiveJob(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)

	// When
	status, created := requestJSON(t, server.handler, http.MethodPost, "/api/jobs", `{"kind":"backup","singleFlightKey":"backup:daily","timeoutSeconds":30}`)

	// Then
	if status != http.StatusCreated {
		t.Fatalf("POST /api/jobs status = %d, want %d; body=%#v", status, http.StatusCreated, created)
	}
	if created["state"] != "queued" {
		t.Fatalf("created state = %v, want queued", created["state"])
	}
	id, ok := created["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("created id invalid: %#v", created["id"])
	}

	status, duplicate := requestJSON(t, server.handler, http.MethodPost, "/api/jobs", `{"kind":"backup","singleFlightKey":"backup:daily","timeoutSeconds":30}`)
	if status != http.StatusConflict {
		t.Fatalf("duplicate job status = %d, want %d; body=%#v", status, http.StatusConflict, duplicate)
	}

	target := "/api/jobs/" + strconv.FormatInt(int64(id), 10)
	status, running := requestJSON(t, server.handler, http.MethodPost, target+"/start", `{}`)
	if status != http.StatusOK || running["state"] != "running" {
		t.Fatalf("start job status/body = %d/%#v", status, running)
	}

	status, cancelled := requestJSON(t, server.handler, http.MethodPost, target+"/cancel", `{"reason":"operator cancelled"}`)
	if status != http.StatusOK || cancelled["state"] != "cancelled" {
		t.Fatalf("cancel job status/body = %d/%#v", status, cancelled)
	}
}

func assertPublicPayloadDoesNotContain(t *testing.T, payload map[string]any, forbidden string) {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if strings.Contains(string(encoded), forbidden) {
		t.Fatalf("public payload leaked forbidden secret")
	}
}

func routeTestDiscordWebhookURL() string {
	return "https://" + "discord.com" + "/api/" + "webhooks/" + strings.Repeat("2", 18) + "/" + strings.Repeat("b", 64)
}

func routeTestDiscordWebhookURLUppercase() string {
	return "HTTPS://" + "DISCORD.COM" + "/API/" + "WEBHOOKS/" + strings.Repeat("4", 18) + "/" + strings.Repeat("D", 64)
}

func TestJobsAPI_RedactsSecretLikePublicJobFields(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	secret := routeTestDiscordWebhookURLUppercase()

	// When
	status, rejected := requestJSON(t, server.handler, http.MethodPost, "/api/jobs", `{"kind":"backup","singleFlightKey":"backup:`+secret+`"}`)

	// Then
	if status != http.StatusBadRequest {
		t.Fatalf("secret-like singleFlightKey status = %d, want %d", status, http.StatusBadRequest)
	}
	assertPublicPayloadDoesNotContain(t, rejected, secret)

	status, created := requestJSON(t, server.handler, http.MethodPost, "/api/jobs", `{"kind":"backup","singleFlightKey":"backup:redact-api"}`)
	if status != http.StatusCreated {
		t.Fatalf("create safe job status = %d, want %d", status, http.StatusCreated)
	}
	id, ok := created["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("created id invalid")
	}
	target := "/api/jobs/" + strconv.FormatInt(int64(id), 10)
	status, _ = requestJSON(t, server.handler, http.MethodPost, target+"/start", `{}`)
	if status != http.StatusOK {
		t.Fatalf("start job status = %d, want %d", status, http.StatusOK)
	}
	status, failed := requestJSON(t, server.handler, http.MethodPost, target+"/fail", `{"error":"failed `+secret+`","result":{"detail":"`+secret+`"}}`)
	if status != http.StatusOK {
		t.Fatalf("fail job status = %d, want %d", status, http.StatusOK)
	}
	assertPublicPayloadDoesNotContain(t, failed, secret)

	listReq := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	listRec := httptest.NewRecorder()
	server.handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list jobs status = %d, want %d", listRec.Code, http.StatusOK)
	}
	if strings.Contains(listRec.Body.String(), secret) {
		t.Fatalf("list jobs response leaked forbidden secret")
	}
}

func TestJobsAPI_RejectsSecretLikeKind(t *testing.T) {
	t.Parallel()

	// Given
	server := newTestRouteServer(t)
	secret := routeTestDiscordWebhookURLUppercase()

	// When
	status, body := requestJSON(t, server.handler, http.MethodPost, "/api/jobs", `{"kind":"`+secret+`","singleFlightKey":"kind:redact"}`)

	// Then
	if status != http.StatusBadRequest {
		t.Fatalf("secret-like kind status = %d, want %d", status, http.StatusBadRequest)
	}
	assertPublicPayloadDoesNotContain(t, body, secret)
}

func writeAPIEvidence(t *testing.T, name string, payload map[string]any) {
	t.Helper()

	dir := os.Getenv("CAMSTATION_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0o755); err != nil {
		t.Fatalf("create api evidence dir: %v", err)
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal api evidence: %v", err)
	}
	if err := os.WriteFile(filepath.Join(apiDir, name), encoded, 0o644); err != nil {
		t.Fatalf("write api evidence: %v", err)
	}
}
