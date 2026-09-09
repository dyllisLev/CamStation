import assert from "node:assert/strict";
import test from "node:test";
import {
  requestViewerFullscreen,
  reportViewerDiagnostic,
  reportViewerStream,
  safeViewerDiagnostic,
  subscribeViewerFullscreen,
  subscribeViewerCommands,
  type CamStationViewerBridge,
} from "../src/components/live/viewerBridge.ts";

test("reports only allowlisted Viewer diagnostic fields", () => {
  let reported: unknown;
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined,
    reportDiagnostic: (diagnostic) => {
      reported = diagnostic;
    },
    onCommand: () => undefined,
  };

  reportViewerDiagnostic({
    level: "warn",
    component: "viewer.playback",
    event: "attempt_failed",
    sessionId: "playback-12345678",
    documentId: "document-12345678",
    surface: "official_viewer",
    streamName: "yard-live",
    candidateRole: "fallback",
    transport: "webrtc",
    phase: "retrying",
    attempt: 2,
    attemptGeneration: 4,
    resubscribeGeneration: 1,
    durationMs: 5_100,
    attemptElapsedMs: 5_001,
    readyState: 0,
    reconnectCount: 1,
    fallbackCount: 1,
    usingFallback: true,
    errorCode: "setup_timeout",
    terminalReason: "setup_timeout",
    rawUrl: "rtsp://operator:secret@camera/live",
    sdp: "secret-session-description",
  }, bridge);

  assert.deepEqual(reported, {
    level: "warn",
    component: "viewer.playback",
    event: "attempt_failed",
    sessionId: "playback-12345678",
    documentId: "document-12345678",
    streamName: "yard-live",
    transport: "webrtc",
    phase: "retrying",
    surface: "official_viewer",
    candidateRole: "fallback",
    terminalReason: "setup_timeout",
    errorCode: "setup_timeout",
    attempt: 2,
    attemptGeneration: 4,
    resubscribeGeneration: 1,
    durationMs: 5_100,
    attemptElapsedMs: 5_001,
    readyState: 0,
    reconnectCount: 1,
    fallbackCount: 1,
    usingFallback: true,
  });
  assert.doesNotMatch(JSON.stringify(reported), /rtsp|secret|sdp/iu);
});

test("rejects unsafe Viewer diagnostic identities without affecting playback", () => {
  let calls = 0;
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined,
    reportDiagnostic: () => {
      calls++;
      throw new Error("agent pipe is offline");
    },
    onCommand: () => undefined,
  };

  assert.equal(safeViewerDiagnostic({ level: "debug", component: "viewer.playback", event: "first_media", streamName: "rtsp://camera/live" }), null);
  assert.equal(safeViewerDiagnostic({ level: "debug", component: "viewer.playback", event: "first_media", surface: "pretend_viewer" }), null);
  reportViewerDiagnostic({ level: "debug", component: "viewer.playback", event: "first_media", streamName: "rtsp://camera/live" }, bridge);
  assert.equal(calls, 0);
  assert.doesNotThrow(() => reportViewerDiagnostic({ level: "info", component: "viewer.main", event: "live_loaded", state: "running" }, bridge));
  assert.equal(calls, 1);
});

test("reports only bounded stream telemetry fields", () => {
  let reported: unknown;
  const bridge: CamStationViewerBridge = {
    reportStream: (telemetry) => {
      reported = telemetry;
    },
    onCommand: () => undefined,
  };

  reportViewerStream(
    {
      streamName: "yard-live",
      transport: "mse",
      phase: "playing",
      lastBinaryAt: 10,
      lastProgressAt: 11,
      readyState: 4,
      stalledForMs: 0,
      reconnectCount: 1,
      fallbackCount: 1,
      resubscribeCount: 0,
      errorCategory: "none",
      url: "rtsp://admin:secret@127.0.0.1/private",
      go2rtc: { endpoint: "http://127.0.0.1:1984" },
    },
    bridge,
  );

  assert.deepEqual(reported, {
    streamName: "yard-live",
    transport: "mse",
    phase: "playing",
    lastBinaryAt: 10,
    lastProgressAt: 11,
    readyState: 4,
    stalledForMs: 0,
    reconnectCount: 1,
    fallbackCount: 1,
    resubscribeCount: 0,
    errorCategory: "none",
  });
  assert.doesNotMatch(JSON.stringify(reported), /rtsp|secret|127\.0\.0\.1|go2rtc/i);
});

test("rejects telemetry that tries to use a URL as a stream name", () => {
  let calls = 0;
  const bridge: CamStationViewerBridge = {
    reportStream: () => {
      calls++;
    },
    onCommand: () => undefined,
  };

  reportViewerStream({ streamName: "rtsp://user:pass@camera/live", transport: "webrtc", phase: "playing" }, bridge);
  reportViewerStream({ streamName: "  rtsp://user:pass@camera/live", transport: "webrtc", phase: "playing" }, bridge);
  assert.equal(calls, 0);
});

test("uses the preload bridge when present and filters renderer commands", () => {
  let rawHandler: ((command: unknown) => void) | undefined;
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined,
    onCommand: (handler) => {
      rawHandler = handler;
      return () => {
        rawHandler = undefined;
      };
    },
  };
  const received: unknown[] = [];
  const unsubscribe = subscribeViewerCommands((command) => received.push(command), bridge, ["yard-live"]);

  rawHandler?.({ type: "resubscribe_stream", streamName: "yard-live" });
  rawHandler?.({ type: "resubscribe_stream", streamName: "porch-live" });
  rawHandler?.({ type: "resubscribe_stream", streamName: "rtsp://admin:secret@camera/live" });
  rawHandler?.({ type: "restart_agent", streamName: "yard-live" });

  assert.deepEqual(received, [{ type: "resubscribe_stream", streamName: "yard-live" }]);
  unsubscribe();
  assert.equal(rawHandler, undefined);
});

test("accepts registered Korean and space stream identifiers only", () => {
  let rawHandler: ((command: unknown) => void) | undefined;
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined,
    onCommand: (handler) => {
      rawHandler = handler;
    },
  };
  const received: unknown[] = [];
  subscribeViewerCommands((command) => received.push(command), bridge, ["마당 카메라"]);

  rawHandler?.({ type: "resubscribe_stream", streamName: "마당 카메라" });
  rawHandler?.({ type: "resubscribe_stream", streamName: "다른 카메라" });
  rawHandler?.({ type: "resubscribe_stream", streamName: "마당\u0000카메라" });
  rawHandler?.({ type: "resubscribe_stream", streamName: "rtsp://camera/live" });

  assert.deepEqual(received, [{ type: "resubscribe_stream", streamName: "마당 카메라" }]);
});

test("reports a registered non-ASCII stream identifier without widening fields", () => {
  let reported: unknown;
  const bridge: CamStationViewerBridge = {
    reportStream: (telemetry) => {
      reported = telemetry;
    },
    onCommand: () => undefined,
  };

  reportViewerStream({ streamName: "마당 카메라", transport: "webrtc", phase: "playing" }, bridge);
  assert.deepEqual(reported, { streamName: "마당 카메라", transport: "webrtc", phase: "playing" });
});

test("does nothing when Electron preload did not expose the bridge", () => {
  assert.doesNotThrow(() => reportViewerStream({ streamName: "yard-live", transport: "webrtc", phase: "playing" }, undefined));
  assert.doesNotThrow(() => subscribeViewerCommands(() => undefined, undefined)());
});

test("a failed preload IPC bridge cannot break video playback", () => {
  const bridge: CamStationViewerBridge = {
    reportStream: () => {
      throw new Error("agent pipe is offline");
    },
    onCommand: () => {
      throw new Error("agent pipe is offline");
    },
  };

  assert.doesNotThrow(() => reportViewerStream({ streamName: "yard-live", transport: "webrtc", phase: "playing" }, bridge));
  assert.doesNotThrow(() => subscribeViewerCommands(() => undefined, bridge)());
});

test("uses a narrow native fullscreen bridge when present", async () => {
  const requested: boolean[] = [];
  let listener: ((fullscreen: boolean) => void) | undefined;
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined,
    onCommand: () => undefined,
    setFullscreen: async (fullscreen) => {
      requested.push(fullscreen);
    },
    onFullscreenChange: (handler) => {
      listener = handler;
      return () => {
        listener = undefined;
      };
    },
  };

  const observed: boolean[] = [];
  const unsubscribe = subscribeViewerFullscreen((fullscreen) => observed.push(fullscreen), bridge);
  await requestViewerFullscreen(true, bridge);
  listener?.(true);
  assert.deepEqual(requested, [true]);
  assert.deepEqual(observed, [true]);
  unsubscribe();
  assert.equal(listener, undefined);
});

test("native fullscreen acknowledgements publish the actual boolean rather than the requested value", async () => {
  const observed: boolean[] = [];
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined,
    onCommand: () => undefined,
    setFullscreen: async () => false,
    onFullscreenChange: () => undefined,
  };
  const unsubscribe = subscribeViewerFullscreen((value) => observed.push(value), bridge);
  assert.equal(await requestViewerFullscreen(true, bridge), true, "the return value retains the command-delivery contract");
  assert.deepEqual(observed, [false]);
  unsubscribe();
});

test("confirmed native fullscreen survives document navigation in the same session", (context) => {
  const previousWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  const stored = new Map<string, string>();
  Object.defineProperty(globalThis, "window", { configurable: true, value: {
    sessionStorage: { getItem: (key: string) => stored.get(key) ?? null, setItem: (key: string, value: string) => stored.set(key, value) },
  } });
  context.after(() => {
    if (previousWindow) Object.defineProperty(globalThis, "window", previousWindow);
    else Reflect.deleteProperty(globalThis, "window");
  });
  const documentBridge = () => {
    let listener: ((fullscreen: boolean) => void) | undefined;
    const bridge: CamStationViewerBridge = {
      reportStream: () => undefined, onCommand: () => undefined,
      onFullscreenChange: (handler) => { listener = handler; return () => { listener = undefined; }; },
    };
    return { bridge, event: (value: boolean) => listener?.(value) };
  };
  const first = documentBridge();
  const firstObserved: boolean[] = [];
  const stopFirst = subscribeViewerFullscreen((value) => firstObserved.push(value), first.bridge);
  assert.deepEqual(firstObserved, [], "an unobserved initial state is not guessed");
  first.event(true);
  stopFirst();

  const second = documentBridge();
  const secondObserved: boolean[] = [];
  const stopSecond = subscribeViewerFullscreen((value) => secondObserved.push(value), second.bridge);
  assert.deepEqual(secondObserved, [true], "the new document receives the confirmed previous native state");
  second.event(false);
  assert.deepEqual(secondObserved, [true, false]);
  stopSecond();

  const third = documentBridge();
  const thirdObserved: boolean[] = [];
  const stopThird = subscribeViewerFullscreen((value) => thirdObserved.push(value), third.bridge);
  assert.deepEqual(thirdObserved, [false]);
  stopThird();
});

test("failed or unacknowledged fullscreen requests do not invent an observed state", async () => {
  const observed: boolean[] = [];
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined, onCommand: () => undefined,
    onFullscreenChange: () => undefined,
    setFullscreen: async () => undefined,
  };
  const unsubscribe = subscribeViewerFullscreen((value) => observed.push(value), bridge);
  assert.equal(await requestViewerFullscreen(true, bridge), true);
  bridge.setFullscreen = async () => { throw new Error("IPC unavailable"); };
  assert.equal(await requestViewerFullscreen(true, bridge), false);
  assert.deepEqual(observed, []);
  unsubscribe();
});

test("a late native command reply cannot overwrite a newer fullscreen event", async () => {
  let reply: ((value: boolean) => void) | undefined;
  let event: ((value: boolean) => void) | undefined;
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined, onCommand: () => undefined,
    onFullscreenChange: (handler) => { event = handler; },
    setFullscreen: () => new Promise<boolean>((resolve) => { reply = resolve; }),
  };
  const observed: boolean[] = [];
  const unsubscribe = subscribeViewerFullscreen((value) => observed.push(value), bridge);
  const pending = requestViewerFullscreen(true, bridge);
  event?.(true);
  reply?.(false);
  await pending;
  assert.deepEqual(observed, [true]);
  unsubscribe();
});

test("native fullscreen observations still work when session storage is denied", async (context) => {
  const previousWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", { configurable: true, value: {
    get sessionStorage() { throw new Error("storage denied"); },
  } });
  context.after(() => {
    if (previousWindow) Object.defineProperty(globalThis, "window", previousWindow);
    else Reflect.deleteProperty(globalThis, "window");
  });
  const bridge: CamStationViewerBridge = {
    reportStream: () => undefined, onCommand: () => undefined,
    onFullscreenChange: () => undefined, setFullscreen: async () => true,
  };
  const observed: boolean[] = [];
  const unsubscribe = subscribeViewerFullscreen((value) => observed.push(value), bridge);
  assert.equal(await requestViewerFullscreen(true, bridge), true);
  assert.deepEqual(observed, [true]);
  unsubscribe();
});
