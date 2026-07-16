import assert from "node:assert/strict";
import test from "node:test";
import { createPreloadBridge, startRendererHeartbeat } from "../src/preload.ts";

test("preload exposes only the three narrow Viewer methods", () => {
  const sent: unknown[][] = [];
  const listeners = new Map<string, (...args: unknown[]) => void>();
  const bridge = createPreloadBridge({
    send: (...args: unknown[]) => sent.push(args),
    on: (channel, handler) => listeners.set(channel, handler),
    removeListener: (channel) => listeners.delete(channel),
  });

  assert.deepEqual(Object.keys(bridge).sort(), ["onCommand", "reportRenderer", "reportStream"]);
  bridge.reportRenderer({ state: "ready" });
  bridge.reportStream({ streamName: "yard-live", transport: "webrtc", phase: "playing" });
  assert.deepEqual(sent.map((entry) => entry[0]), ["viewer:renderer", "viewer:stream"]);

  const received: unknown[] = [];
  const unsubscribe = bridge.onCommand((command) => received.push(command));
  listeners.get("viewer:command")?.({}, { type: "resubscribe_stream", streamName: "yard-live" });
  assert.deepEqual(received, [{ type: "resubscribe_stream", streamName: "yard-live" }]);
  unsubscribe();
  assert.equal(listeners.has("viewer:command"), false);
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
