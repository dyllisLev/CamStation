import assert from "node:assert/strict";
import test from "node:test";
import { PlaybackRecovery } from "../src/components/live/playbackRecovery.ts";

test("one episode stops after WebRTC, reconnect, MSE primary, fallback, and resubscribe", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"], 0);

  assert.deepEqual(episode.nextFailure(1_000), {
    transport: "webrtc",
    streamName: "yard-live",
    attempt: 2,
  });
  assert.deepEqual(episode.nextFailure(5_000), {
    transport: "mse",
    streamName: "yard-live",
    attempt: 3,
  });
  assert.deepEqual(episode.nextFailure(10_000), {
    transport: "mse",
    streamName: "yard-focus",
    attempt: 4,
  });
  assert.deepEqual(episode.nextFailure(20_000), { action: "resubscribe", attempt: 5 });
  assert.deepEqual(episode.nextFailure(30_001), { action: "cooldown", until: 330_001 });
});

test("a single-candidate episode skips the missing fallback and still terminates", () => {
  const episode = new PlaybackRecovery(["yard-live"], 0);

  assert.equal(episode.nextFailure(1_000).attempt, 2);
  assert.equal(episode.nextFailure(2_000).attempt, 3);
  assert.deepEqual(episode.nextFailure(3_000), { action: "resubscribe", attempt: 4 });
  assert.deepEqual(episode.nextFailure(4_000), { action: "cooldown", until: 304_000 });
});

test("only five minutes of continuous progress resets the finite episode", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"], 0);
  episode.nextFailure(1_000);
  episode.nextFailure(2_000);

  assert.equal(episode.recordProgress(3_000), false);
  for (let now = 12_000; now < 303_000; now += 9_000) {
    assert.equal(episode.recordProgress(now), false);
  }
  assert.equal(episode.recordProgress(303_000), true);
  assert.deepEqual(episode.nextFailure(304_000), {
    transport: "webrtc",
    streamName: "yard-live",
    attempt: 2,
  });
});

test("a media stall breaks the stable-progress reset interval", () => {
  const episode = new PlaybackRecovery(["yard-live"], 0);

  assert.equal(episode.recordProgress(1_000), false);
  assert.equal(episode.recordProgress(12_001), false);
  for (let now = 21_000; now < 312_001; now += 9_000) {
    assert.equal(episode.recordProgress(now), false);
  }
  assert.equal(episode.recordProgress(312_001), true);
});

test("a new stall starts a fresh 30-second deadline without resetting attempts", () => {
  const episode = new PlaybackRecovery(["yard-live"], 0);
  episode.recordProgress(20_000);

  assert.deepEqual(episode.nextFailure(40_000), {
    transport: "webrtc",
    streamName: "yard-live",
    attempt: 2,
  });
});
