import assert from "node:assert/strict";
import test from "node:test";
import { parseMseControlMessage } from "../src/components/live/msePlayback.ts";

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
