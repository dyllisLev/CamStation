import assert from "node:assert/strict";
import test from "node:test";
import {
  playbackDiagnosticLevel,
  safePlaybackDiagnostic,
} from "../src/app/playbackDiagnosticsApi.ts";

test("playback diagnostic events have stable operator log levels", () => {
  assert.equal(playbackDiagnosticLevel("socket_open"), "debug");
  assert.equal(playbackDiagnosticLevel("attempt_started"), "info");
  assert.equal(playbackDiagnosticLevel("primary_probe_started"), "debug");
  assert.equal(playbackDiagnosticLevel("primary_probe_succeeded"), "info");
  assert.equal(playbackDiagnosticLevel("attempt_failed"), "warn");
  assert.equal(playbackDiagnosticLevel("episode_exhausted"), "error");
});

test("playback diagnostics retain only bounded structured fields", () => {
  const event = safePlaybackDiagnostic({
    sessionId: "playback-12345678",
    event: "attempt_failed",
    streamName: "yard-live",
    transport: "webrtc",
    phase: "connecting",
    attempt: 1,
    elapsedMs: 5_100,
    attemptElapsedMs: 5_001,
    errorCategory: "setup_timeout",
    readyState: 0,
    rawUrl: "rtsp://operator:secret@camera/live",
    sdp: "secret-session-description",
  });

  assert.deepEqual(event, {
    sessionId: "playback-12345678",
    event: "attempt_failed",
    streamName: "yard-live",
    transport: "webrtc",
    phase: "connecting",
    attempt: 1,
    elapsedMs: 5_100,
    attemptElapsedMs: 5_001,
    errorCategory: "setup_timeout",
    readyState: 0,
    usingFallback: false,
    reconnectCount: 0,
    fallbackCount: 0,
  });
  assert.doesNotMatch(JSON.stringify(event), /rtsp|secret|sdp/iu);
});
