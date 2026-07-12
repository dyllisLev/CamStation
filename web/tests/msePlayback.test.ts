import assert from "node:assert/strict";
import test from "node:test";
import { nextPlaybackAttempt, parseMseControlMessage } from "../src/components/live/msePlayback.ts";

test("parses mse and error control messages without exposing unknown payloads", () => {
  assert.deepEqual(parseMseControlMessage('{"type":"mse","value":"video/mp4"}'), {
    type: "mse",
    value: "video/mp4",
  });
  assert.deepEqual(parseMseControlMessage('{"type":"error","value":"secret upstream detail"}'), {
    type: "error",
    value: "",
  });
  assert.deepEqual(parseMseControlMessage("not-json"), { type: "invalid", value: "" });
});

test("advances quickly to fallback and waits before a new preferred cycle", () => {
  assert.deepEqual(nextPlaybackAttempt(0, 2), { candidateIndex: 1, delayMs: 500 });
  assert.deepEqual(nextPlaybackAttempt(1, 2), { candidateIndex: 0, delayMs: 3000 });
  assert.deepEqual(nextPlaybackAttempt(0, 1), { candidateIndex: 0, delayMs: 3000 });
});
