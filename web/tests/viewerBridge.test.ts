import assert from "node:assert/strict";
import test from "node:test";
import {
  reportViewerStream,
  subscribeViewerCommands,
  type CamStationViewerBridge,
} from "../src/components/live/viewerBridge.ts";

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
  const unsubscribe = subscribeViewerCommands((command) => received.push(command), bridge);

  rawHandler?.({ type: "resubscribe_stream", streamName: "yard-live" });
  rawHandler?.({ type: "resubscribe_stream", streamName: "rtsp://admin:secret@camera/live" });
  rawHandler?.({ type: "restart_agent", streamName: "yard-live" });

  assert.deepEqual(received, [{ type: "resubscribe_stream", streamName: "yard-live" }]);
  unsubscribe();
  assert.equal(rawHandler, undefined);
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
