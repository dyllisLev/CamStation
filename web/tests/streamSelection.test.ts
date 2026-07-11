import assert from "node:assert/strict";
import test from "node:test";
import { playbackStreamName } from "../src/components/live/streamSelection.ts";

const dualStreamCamera = {
  streamName: "yard",
  liveStreamName: "yard-live",
  recordingStreamName: "yard-recording",
};

test("normal view uses the live stream", () => {
  assert.equal(playbackStreamName(dualStreamCamera), "yard-live");
});

test("focus view uses the recording stream", () => {
  assert.equal(playbackStreamName(dualStreamCamera, true), "yard-recording");
});

test("focus view falls back through live to the stable stream name", () => {
  assert.equal(playbackStreamName({ streamName: "single", liveStreamName: "single-live" }, true), "single-live");
  assert.equal(playbackStreamName({ streamName: "legacy" }, true), "legacy");
});
