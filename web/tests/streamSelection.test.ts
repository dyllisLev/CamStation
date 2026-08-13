import assert from "node:assert/strict";
import test from "node:test";
import { playbackStreamCandidates, playbackStreamName, tileFocusPresentation } from "../src/components/live/streamSelection.ts";

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

test("focus changes tile presentation without removing any playback tile", () => {
  assert.equal(tileFocusPresentation("yard", "yard"), "focused");
  assert.equal(tileFocusPresentation("porch", "yard"), "background");
  assert.equal(tileFocusPresentation("yard", null), "grid");
});

test("focus view falls back through live to the stable stream name", () => {
  assert.equal(playbackStreamName({ streamName: "single", liveStreamName: "single-live" }, true), "single-live");
  assert.equal(playbackStreamName({ streamName: "legacy" }, true), "legacy");
});

test("normal playback falls back from live to focus without duplicates", () => {
  assert.deepEqual(playbackStreamCandidates(dualStreamCamera), ["yard-live", "yard-focus"]);
  assert.deepEqual(
    playbackStreamCandidates({ ...dualStreamCamera, focusStreamName: "yard-live" }),
    ["yard-live"],
  );
});

test("focused playback falls back from focus to live", () => {
  assert.deepEqual(playbackStreamCandidates(dualStreamCamera, true), ["yard-focus", "yard-live"]);
});

test("stable stream is used only without role outputs", () => {
  assert.deepEqual(playbackStreamCandidates({ streamName: "legacy" }), ["legacy"]);
});
