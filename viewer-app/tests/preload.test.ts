import assert from "node:assert/strict";
import test from "node:test";
import { createPreloadBridge, startRendererHeartbeat } from "../src/preload.ts";

test("preload exposes only narrow Viewer methods including native fullscreen", () => {
  const sent: unknown[][] = [];
  const listeners = new Map<string, (...args: unknown[]) => void>();
  const bridge = createPreloadBridge({
    send: (...args: unknown[]) => sent.push(args),
    on: (channel, handler) => listeners.set(channel, handler),
    removeListener: (channel) => listeners.delete(channel),
  });

  assert.deepEqual(Object.keys(bridge).sort(), ["getSetupState", "onCommand", "onFullscreenChange", "reportRenderer", "reportStream", "retryConnection", "saveConfiguration", "setFullscreen"]);
  bridge.reportRenderer({ state: "ready" });
  bridge.reportStream({ streamName: "yard-live", transport: "webrtc", phase: "playing" });
  assert.deepEqual(sent.map((entry) => entry[0]), ["viewer:renderer", "viewer:stream"]);

  const received: unknown[] = [];
  const unsubscribe = bridge.onCommand((command) => received.push(command));
  listeners.get("viewer:command")?.({}, { type: "resubscribe_stream", streamName: "yard-live" });
  assert.deepEqual(received, [{ type: "resubscribe_stream", streamName: "yard-live" }]);
  unsubscribe();
  assert.equal(listeners.has("viewer:command"), false);

  const fullscreen: boolean[] = [];
  const unsubscribeFullscreen = bridge.onFullscreenChange((value) => fullscreen.push(value));
  listeners.get("viewer:fullscreen-changed")?.({}, true);
  assert.deepEqual(fullscreen, [true]);
  unsubscribeFullscreen();
  assert.equal(listeners.has("viewer:fullscreen-changed"), false);
});

test("preload emits a genuine renderer-context liveness pulse periodically", (t) => {
  t.mock.timers.enable({ apis: ["setInterval"] });
  const sent: unknown[][] = [];
  const ipc = {
    send: (...args: unknown[]) => sent.push(args),
    on: () => undefined,
    removeListener: () => undefined,
  };

  const stop = startRendererHeartbeat(ipc);
  try {
    t.mock.timers.tick(10_000);
  } finally {
    stop();
  }

  assert.ok(sent.length >= 2, `renderer pulses=${sent.length}`);
  assert.ok(sent.every((entry) => entry[0] === "viewer:renderer"));
  assert.ok(sent.every((entry) => JSON.stringify(entry[1]) === JSON.stringify({ state: "ready" })));
});
