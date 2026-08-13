import assert from "node:assert/strict";
import test from "node:test";
import {
  playbackDiagnosticLevel,
  reportPlaybackDiagnostic,
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

test("playback diagnostic uses the same session in Viewer-local and server records", async () => {
  const windowDescriptor = Object.getOwnPropertyDescriptor(globalThis, "window");
  const originalFetch = globalThis.fetch;
  let viewerRecord: Record<string, unknown> | undefined;
  let serverRecord: Record<string, unknown> | undefined;
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: {
      camstationViewer: {
        reportStream: () => undefined,
        reportDiagnostic: (record: Record<string, unknown>) => {
          viewerRecord = record;
        },
        onCommand: () => undefined,
      },
    },
  });
  globalThis.fetch = (async (_input: string | URL | Request, init?: RequestInit) => {
    serverRecord = JSON.parse(String(init?.body)) as Record<string, unknown>;
    return new Response(null, { status: 204 });
  }) as typeof fetch;

  try {
    reportPlaybackDiagnostic({
      sessionId: "playback-join-1234",
      event: "first_media",
      streamName: "yard-live",
      transport: "webrtc",
      phase: "playing",
      attempt: 2,
      elapsedMs: 1_250,
      attemptElapsedMs: 450,
      errorCategory: "none",
      readyState: 4,
      usingFallback: false,
      reconnectCount: 1,
      fallbackCount: 0,
    });
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(viewerRecord?.sessionId, "playback-join-1234");
    assert.equal(serverRecord?.sessionId, "playback-join-1234");
    assert.equal(viewerRecord?.event, serverRecord?.event);
    assert.equal(viewerRecord?.attemptElapsedMs, 450);
  } finally {
    globalThis.fetch = originalFetch;
    if (windowDescriptor) Object.defineProperty(globalThis, "window", windowDescriptor);
    else Reflect.deleteProperty(globalThis, "window");
  }
});
