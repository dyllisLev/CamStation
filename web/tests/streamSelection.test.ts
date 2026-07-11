import assert from "node:assert/strict";
import test from "node:test";
import { playbackStreamName, shouldRenderLiveTile } from "../src/components/live/streamSelection.ts";

const dualStreamCamera = {
  streamName: "yard",
  liveStreamName: "yard-live",
  recordingStreamName: "yard-recording",
  focusStreamName: "yard-focus",
};

test("normal view uses the live stream", () => {
  assert.equal(playbackStreamName(dualStreamCamera), "yard-live");
});

test("focus view uses the applied focus stream", () => {
  assert.equal(playbackStreamName(dualStreamCamera, true), "yard-focus");
});

test("focused camera suspends only its normal live tile", () => {
  assert.equal(shouldRenderLiveTile("yard", "yard"), false);
  assert.equal(shouldRenderLiveTile("porch", "yard"), true);
  assert.equal(shouldRenderLiveTile("yard", null), true);
});

test("focus view falls back through live to the stable stream name", () => {
  assert.equal(playbackStreamName({ streamName: "single", liveStreamName: "single-live" }, true), "single-live");
  assert.equal(playbackStreamName({ streamName: "legacy" }, true), "legacy");
});
