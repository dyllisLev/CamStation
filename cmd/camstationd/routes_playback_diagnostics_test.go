package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"camstation/internal/opslog"
	"camstation/internal/store"
)

func TestPlaybackDiagnosticsRespectLogLevelAndWriteCorrelatedSafeEvents(t *testing.T) {
	server := newTestRouteServer(t)
	if _, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name: "Gate", StreamName: "gate-live", Enabled: true, State: "streaming",
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	sink, err := newPlaybackEventSink("info", &output)
	if err != nil {
		t.Fatal(err)
	}
	deps := routeDeps{
		db: server.db, playbackEvents: sink,
		playbackRateLimit: newPlaybackEventRateLimiter(100, time.Minute),
	}
	mux := http.NewServeMux()
	deps.registerPlaybackDiagnosticRoutes(mux)

	debugStatus := postPlaybackEvent(t, mux, `{
		"sessionId":"playback-12345678","event":"socket_open","streamName":"gate-live",
		"transport":"webrtc","phase":"connecting","attempt":1,"elapsedMs":14,"readyState":0
	}`)
	if debugStatus != http.StatusNoContent || output.Len() != 0 {
		t.Fatalf("debug event status/log = %d/%q, want suppressed", debugStatus, output.String())
	}
	infoStatus := postPlaybackEvent(t, mux, `{
		"sessionId":"playback-12345678","event":"attempt_started","streamName":"gate-live",
		"transport":"webrtc","phase":"connecting","attempt":1,"elapsedMs":18,"readyState":0
	}`)
	primaryRestoredStatus := postPlaybackEvent(t, mux, `{
		"sessionId":"playback-12345678","event":"primary_probe_succeeded","streamName":"gate-live",
		"transport":"mse","phase":"recovering","attempt":2,"elapsedMs":1020,"readyState":4,
		"usingFallback":true
	}`)
	warnStatus := postPlaybackEvent(t, mux, `{
		"sessionId":"playback-12345678","documentId":"document-12345678","surface":"official_viewer",
		"event":"attempt_failed","streamName":"gate-live","candidateRole":"primary",
		"transport":"webrtc","phase":"connecting","attempt":1,"elapsedMs":5019,
		"attemptGeneration":3,"resubscribeGeneration":1,"attemptElapsedMs":5001,
		"errorCategory":"setup_timeout","terminalReason":"setup_timeout","readyState":0
	}`)
	if infoStatus != http.StatusNoContent || primaryRestoredStatus != http.StatusNoContent || warnStatus != http.StatusNoContent {
		t.Fatalf("info/primary-restored/warn statuses = %d/%d/%d", infoStatus, primaryRestoredStatus, warnStatus)
	}
	logText := output.String()
	for _, want := range []string{
		`"level":"info"`, `"event":"attempt_started"`, `"sessionId":"playback-12345678"`,
		`"streamName":"gate-live"`, `"transport":"webrtc"`, `"level":"warn"`,
		`"event":"primary_probe_succeeded"`, `"transport":"mse"`, `"usingFallback":true`,
		`"event":"attempt_failed"`, `"errorCategory":"setup_timeout"`, `"attemptElapsedMs":5001`,
		`"documentId":"document-12345678"`, `"surface":"official_viewer"`, `"candidateRole":"primary"`,
		`"attemptGeneration":3`, `"resubscribeGeneration":1`, `"terminalReason":"setup_timeout"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("playback log missing %s: %s", want, logText)
		}
	}
	for _, forbidden := range []string{"rtsp://", "candidate:", "sdp=", "authorization", "password"} {
		if strings.Contains(strings.ToLower(logText), forbidden) {
			t.Fatalf("playback log contains forbidden %q: %s", forbidden, logText)
		}
	}
}

func TestPlaybackDiagnosticsUseSharedOperationalLoggerAndClientCorrelation(t *testing.T) {
	server := newTestRouteServer(t)
	camera, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name: "Gate", StreamName: "gate-live", Enabled: true, State: "streaming",
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	logger, err := opslog.New(opslog.Config{Level: "info", ComponentLevels: "playback=debug", Writer: &output})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	deps := routeDeps{
		db: server.db, playbackEvents: newPlaybackEventSinkWithLogger(logger),
		playbackRateLimit: newPlaybackEventRateLimiter(100, time.Minute),
	}
	mux := http.NewServeMux()
	deps.registerPlaybackDiagnosticRoutes(mux)
	status := postPlaybackEventFrom(t, mux, "192.0.2.40:45000", `{
		"sessionId":"playback-12345678","documentId":"document-12345678","surface":"official_viewer",
		"event":"first_media","streamName":"gate-live","candidateRole":"primary",
		"transport":"webrtc","phase":"connecting","attempt":1,"attemptGeneration":2,
		"resubscribeGeneration":1,"elapsedMs":42,"readyState":4
	}`)
	if status != http.StatusNoContent {
		t.Fatalf("status=%d", status)
	}
	var record opslog.Record
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode shared log: %v: %s", err, output.String())
	}
	if record.Component != "playback" || record.Level != "debug" || record.Event != "first_media" ||
		record.SessionID != "playback-12345678" || record.StreamName != "gate-live" ||
		record.DocumentID != "document-12345678" || record.Surface != "official_viewer" ||
		record.CandidateRole != "primary" || record.AttemptGeneration != 2 || record.ResubscribeGeneration != 1 ||
		record.CameraID != camera.ID || record.ClientIP != "192.0.2.40" ||
		record.Transport != "webrtc" || record.ReadyState != 4 {
		t.Fatalf("record=%+v", record)
	}
}

func TestPlaybackDiagnosticsRejectUnknownFieldsUnregisteredStreamsAndFloods(t *testing.T) {
	server := newTestRouteServer(t)
	if _, err := server.db.UpsertCamera(t.Context(), store.Camera{
		Name: "Gate", StreamName: "gate-live", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	sink, err := newPlaybackEventSink("debug", &output)
	if err != nil {
		t.Fatal(err)
	}
	deps := routeDeps{
		db: server.db, playbackEvents: sink,
		playbackRateLimit: newPlaybackEventRateLimiter(2, time.Minute),
	}
	mux := http.NewServeMux()
	deps.registerPlaybackDiagnosticRoutes(mux)

	unknownField := postPlaybackEventFrom(t, mux, "10.0.0.31:45000", `{
		"sessionId":"playback-12345678","event":"attempt_started","streamName":"gate-live",
		"transport":"webrtc","phase":"connecting","attempt":1,"rawUrl":"rtsp://secret/live"
	}`)
	unregistered := postPlaybackEventFrom(t, mux, "10.0.0.32:45000", `{
		"sessionId":"playback-12345678","event":"attempt_started","streamName":"missing",
		"transport":"webrtc","phase":"connecting","attempt":1
	}`)
	invalidContext := postPlaybackEventFrom(t, mux, "10.0.0.33:45000", `{
		"sessionId":"playback-12345678","documentId":"document-12345678","surface":"pretend_viewer",
		"event":"attempt_started","streamName":"gate-live","transport":"webrtc","phase":"connecting","attempt":1
	}`)
	first := postPlaybackEvent(t, mux, validPlaybackEventBody("socket_open", 1))
	second := postPlaybackEvent(t, mux, validPlaybackEventBody("signaling_answer", 1))
	third := postPlaybackEvent(t, mux, validPlaybackEventBody("first_media", 1))

	if unknownField != http.StatusBadRequest || unregistered != http.StatusNotFound || invalidContext != http.StatusBadRequest {
		t.Fatalf("invalid statuses = unknown:%d stream:%d context:%d", unknownField, unregistered, invalidContext)
	}
	if first != http.StatusNoContent || second != http.StatusNoContent || third != http.StatusTooManyRequests {
		t.Fatalf("rate statuses = %d/%d/%d", first, second, third)
	}
	if strings.Contains(output.String(), "rtsp://") || strings.Contains(output.String(), "missing") {
		t.Fatalf("rejected payload reached log: %s", output.String())
	}
}

func TestPlaybackLogLevelRejectsInvalidConfiguration(t *testing.T) {
	if _, err := newPlaybackEventSink("verbose", &bytes.Buffer{}); err == nil {
		t.Fatal("invalid playback log level was accepted")
	}
}

func postPlaybackEvent(t *testing.T, handler http.Handler, body string) int {
	return postPlaybackEventFrom(t, handler, "10.0.0.30:45000", body)
}

func postPlaybackEventFrom(t *testing.T, handler http.Handler, remoteAddr, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/playback/events", strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

func validPlaybackEventBody(event string, attempt int) string {
	return `{"sessionId":"playback-12345678","event":"` + event + `","streamName":"gate-live",` +
		`"transport":"webrtc","phase":"connecting","attempt":` + strconv.Itoa(attempt) + `}`
}
