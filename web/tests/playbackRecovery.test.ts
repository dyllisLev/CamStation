import assert from "node:assert/strict";
import test from "node:test";
import { PlaybackRecovery } from "../src/components/live/playbackRecovery.ts";

test("one episode stops after WebRTC, reconnect, MSE primary, fallback, and resubscribe", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);

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
  assert.deepEqual(episode.nextFailure(31_001), { action: "cooldown", until: 331_001 });
});

test("the first stall after sixty healthy seconds gets a fresh finite recovery episode", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);

  assert.equal(episode.recordProgress(1_000), false);
  assert.equal(episode.recordProgress(60_000), false);
  episode.recordFailure(70_001);

  assert.deepEqual(episode.nextFailure(70_001), {
    transport: "webrtc",
    streamName: "yard-live",
    attempt: 2,
  });
  assert.deepEqual(episode.nextFailure(75_000), {
    transport: "mse",
    streamName: "yard-live",
    attempt: 3,
  });
  assert.deepEqual(episode.nextFailure(80_000), {
    transport: "mse",
    streamName: "yard-focus",
    attempt: 4,
  });
  assert.deepEqual(episode.nextFailure(90_000), { action: "resubscribe", attempt: 5 });
  assert.deepEqual(episode.nextFailure(100_002), { action: "cooldown", until: 400_002 });
});

test("a single-candidate episode skips the missing fallback and still terminates", () => {
  const episode = new PlaybackRecovery(["yard-live"]);

  assert.equal(episode.nextFailure(1_000).attempt, 2);
  assert.equal(episode.nextFailure(2_000).attempt, 3);
  assert.deepEqual(episode.nextFailure(3_000), { action: "resubscribe", attempt: 4 });
  assert.deepEqual(episode.nextFailure(4_000), { action: "cooldown", until: 304_000 });
});

test("only five minutes of continuous progress resets the finite episode", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  episode.nextFailure(1_000);
  episode.nextFailure(2_000);

  assert.equal(episode.recordProgress(3_000), false);
  for (let now = 12_000; now < 303_000; now += 9_000) {
    assert.equal(episode.recordProgress(now), false);
  }
  assert.equal(episode.recordProgress(303_000), true);
  assert.equal(episode.remainingMs(304_000), 30_000);
  assert.deepEqual(episode.nextFailure(304_000), {
    transport: "webrtc",
    streamName: "yard-live",
    attempt: 2,
  });
});

test("a media stall breaks the stable-progress reset interval", () => {
  const episode = new PlaybackRecovery(["yard-live"]);

  assert.equal(episode.recordProgress(1_000), false);
  assert.equal(episode.recordProgress(12_001), false);
  for (let now = 21_000; now < 312_001; now += 9_000) {
    assert.equal(episode.recordProgress(now), false);
  }
  assert.equal(episode.recordProgress(312_001), true);
});

test("brief progress cannot rearm the original 30-second episode", () => {
  const episode = new PlaybackRecovery(["yard-live"]);
  assert.equal(episode.nextFailure(1_000).attempt, 2);
  episode.recordProgress(20_000);

  assert.deepEqual(episode.nextFailure(31_001), { action: "cooldown", until: 331_001 });
});

test("late attempts are bounded by the original remaining deadline", () => {
  const episode = new PlaybackRecovery(["yard-live"]);
  episode.recordFailure(0);

  assert.equal(episode.remainingMs(28_000), 2_000);
  assert.equal(episode.boundedDelayMs(28_000, 5_000), 2_000);
  assert.equal(episode.remainingMs(30_000), 0);
  assert.deepEqual(episode.nextFailure(30_000), { action: "cooldown", until: 330_000 });
});

test("stall duration spans retry transitions and terminal cooldown", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);

  episode.recordFailure(1_000);
  assert.equal(episode.stalledForMs(1_000), 0);
  assert.equal(episode.nextFailure(1_000).attempt, 2);

  episode.recordFailure(5_000);
  assert.equal(episode.stalledForMs(5_000), 4_000);
  assert.equal(episode.nextFailure(5_000).attempt, 3);

  episode.recordFailure(10_000);
  assert.equal(episode.nextFailure(10_000).attempt, 4);
  episode.recordFailure(20_000);
  assert.equal(episode.nextFailure(20_000).attempt, 5);
  episode.recordFailure(30_000);

  assert.deepEqual(episode.nextFailure(30_000), { action: "cooldown", until: 330_000 });
  assert.equal(episode.stalledForMs(30_000), 29_000);
});

test("genuine media progress clears stall telemetry without rearming the episode", () => {
  const episode = new PlaybackRecovery(["yard-live"]);

  episode.recordFailure(1_000);
  assert.equal(episode.stalledForMs(4_000), 3_000);
  assert.equal(episode.recordProgress(5_000), false);

  assert.equal(episode.stalledForMs(6_000), 0);
  assert.equal(episode.remainingMs(6_000), 25_000);
  episode.recordFailure(8_000);
  assert.equal(episode.stalledForMs(10_000), 2_000);
});
