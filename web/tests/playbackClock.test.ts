import assert from "node:assert/strict";
import test from "node:test";
import type { PlaybackMedia } from "../src/app/playbackTypes.ts";
import { absoluteTimeAt, containsPlaybackTime, continuationSeekAt, isPlaybackMediaUrl, mediaTimeAt, resumeBoundaryClock } from "../src/components/playback/playbackClock.ts";

const media: PlaybackMedia = {
  id: "fmp4-7", kind: "hls", url: "/api/playback/media/fmp4-7/manifest.m3u8",
  startMs: 1_700_000_000_000, endMs: 1_700_000_010_000, mediaStartSeconds: 0.08,
  requestedOffsetSeconds: 0, growing: false, timeBasis: "server_wallclock",
};

test("absolute seeks retain the media origin and map decoded time back to the shared clock", () => {
  assert.equal(mediaTimeAt(media, media.startMs + 4200), 4.28);
  assert.equal(absoluteTimeAt(media, 4.28), media.startMs + 4200);
  assert.equal(mediaTimeAt({ ...media, kind: "file", mediaStartSeconds: 0 }, media.startMs + 4200), 4.2);
});

test("file end belongs to the next resolve, including a growing recording boundary", () => {
  assert.equal(containsPlaybackTime(media, media.startMs), true);
  assert.equal(containsPlaybackTime(media, media.endMs - 1), true);
  assert.equal(containsPlaybackTime(media, media.endMs), false);
  assert.equal(containsPlaybackTime({ ...media, growing: true }, media.endMs), false);
});

test("normal file rotation bridges observed tiny gaps only after media ends", () => {
  for (const gapMs of [28, 175, 1000]) {
    const result = { status: "gap" as const, requestedAtMs: media.endMs, nextAtMs: media.endMs + gapMs };
    assert.equal(continuationSeekAt(result, media.endMs), media.endMs + gapMs);
    assert.equal(continuationSeekAt(result), undefined, "an explicit seek must retain the requested gap");
  }
  assert.equal(continuationSeekAt({ status: "gap", requestedAtMs: media.endMs, nextAtMs: media.endMs + 1001 }, media.endMs), undefined);
  assert.equal(continuationSeekAt({ status: "edge_wait", requestedAtMs: media.endMs }, media.endMs), undefined);
});

test("a prepared continuation never advances another playable camera's clock", () => {
  const next = { ...media, startMs: media.endMs + 175, endMs: media.endMs + 10_000 };
  assert.equal(resumeBoundaryClock(media.endMs, [next]), next.startMs);
  const other = { ...media, endMs: media.endMs + 5000 };
  assert.equal(resumeBoundaryClock(media.endMs, [next, other]), media.endMs);
  assert.equal(resumeBoundaryClock(media.endMs, []), media.endMs);
  assert.equal(resumeBoundaryClock(media.endMs, [{ ...next, startMs: media.endMs + 1001 }]), media.endMs);
});

test("recorded playback accepts public media routes without external or traversal URLs", () => {
  assert.equal(isPlaybackMediaUrl(media.url), true);
  assert.equal(isPlaybackMediaUrl("/api/recordings/segments/71/play"), true);
  for (const url of ["https://camera.invalid/video", "//camera.invalid/video", "/api/playback/media/../manifest.m3u8", "/api/playback/media/x/manifest.m3u8?url=private", "/api/recordings/segments/7/download"]) {
    assert.equal(isPlaybackMediaUrl(url), false);
  }
});
