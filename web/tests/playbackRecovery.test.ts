import assert from "node:assert/strict";
import test from "node:test";
import {
  PLAYBACK_COOLDOWN_MS,
  PLAYBACK_PRIMARY_PROBE_INTERVAL_MS,
  PLAYBACK_STALL_MS,
  PLAYBACK_STABLE_RESET_MS,
  PlaybackPrimaryPromoter,
  PlaybackProbeScheduler,
  PlaybackRecovery,
} from "../src/components/live/playbackRecovery.ts";

test("a healthy fallback keeps probing both primary transports until one recovers", async () => {
  let now = 1_000;
  let timerCallback: (() => void) | null = null;
  const attempts: string[] = [];
  const events: string[] = [];
  const results = [false, false, true];
  const fireTimer = () => {
    const callback = timerCallback;
    timerCallback = null;
    callback?.();
  };
  const promoter = new PlaybackPrimaryPromoter(
    async (transport) => {
      attempts.push(transport);
      return results.shift() ?? false;
    },
    {
      now: () => now,
      set: (callback, delayMs) => {
        assert.equal(delayMs, PLAYBACK_PRIMARY_PROBE_INTERVAL_MS);
        timerCallback = callback;
        return 1;
      },
      clear: () => { timerCallback = null; },
    },
  );

  promoter.start("webrtc", {
    onProbeStarted: (transport) => events.push(`started:${transport}`),
    onProbeFailed: (transport) => events.push(`failed:${transport}`),
    onRecovered: (transport) => events.push(`recovered:${transport}`),
  });
  assert.equal(promoter.active, true);
  assert.ok(timerCallback);
  fireTimer();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(attempts, ["webrtc", "mse"]);
  assert.deepEqual(events, [
    "started:webrtc",
    "failed:webrtc",
    "started:mse",
    "failed:mse",
  ]);
  assert.equal(promoter.active, true);
  assert.ok(timerCallback, "both failures schedule another probe cycle without disturbing fallback");

  now += PLAYBACK_PRIMARY_PROBE_INTERVAL_MS;
  fireTimer();
  await Promise.resolve();
  await Promise.resolve();
  assert.deepEqual(attempts, ["webrtc", "mse", "webrtc"]);
  assert.equal(events.at(-1), "recovered:webrtc");
  assert.equal(promoter.active, false);
  assert.equal(timerCallback, null);
});

test("stopping primary promotion aborts an in-flight probe and prevents rescheduling", async () => {
  let timerCallback: (() => void) | null = null;
  let aborted = false;
  const promoter = new PlaybackPrimaryPromoter(
    (_transport, signal) => new Promise<boolean>((resolve) => {
      signal.addEventListener("abort", () => { aborted = true; resolve(false); }, { once: true });
    }),
    {
      now: () => 1_000,
      set: (callback) => { timerCallback = callback; return 1; },
      clear: () => { timerCallback = null; },
    },
  );

  promoter.start("mse", {
    onProbeStarted: () => undefined,
    onProbeFailed: () => undefined,
    onRecovered: () => assert.fail("a stopped probe cannot promote"),
  });
  const callback = timerCallback;
  timerCallback = null;
  callback?.();
  await Promise.resolve();
  promoter.stop();
  await Promise.resolve();

  assert.equal(aborted, true);
  assert.equal(promoter.active, false);
  assert.equal(timerCallback, null);
});

test("clearing or rearming a probe scheduler makes stale callbacks harmless", () => {
  let now = 1_000;
  let nextTimer = 0;
  const callbacks = new Map<number, () => void>();
  const cleared: number[] = [];
  const delays: number[] = [];
  const scheduler = new PlaybackProbeScheduler({
    now: () => now,
    set: (callback, delayMs) => {
      const id = ++nextTimer;
      callbacks.set(id, callback);
      delays.push(delayMs);
      return id;
    },
    // Retain callbacks: one already queued when clearTimeout runs must be harmless.
    clear: (id) => cleared.push(id),
  });
  let probes = 0;

  scheduler.arm(now + 5_000, () => probes++);
  const staleAfterRearm = callbacks.get(1);
  now += 1_000;
  scheduler.arm(now + 5_000, () => probes++);
  staleAfterRearm?.();
  assert.equal(probes, 0);
  callbacks.get(2)?.();
  callbacks.get(2)?.();
  assert.equal(probes, 1);

  scheduler.arm(now + 5_000, () => probes++);
  const staleAfterClear = callbacks.get(3);
  scheduler.clear();
  staleAfterClear?.();
  assert.equal(probes, 1);
  assert.deepEqual(cleared, [1, 3]);
  assert.deepEqual(delays, [5_000, 5_000, 5_000]);
});

test("a verified primary promotion receives a fresh bounded recovery episode", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  episode.recordFailure(1_000);
  assert.equal(episode.remainingMs(31_001), 0);
  episode.resetForPrimaryPromotion();
  assert.equal(episode.remainingMs(31_001), 30_000);
  assert.deepEqual(episode.nextFailure(31_001), {
    transport: "webrtc", streamName: "yard-live", attempt: 2,
  });
});

test("every exhausted episode cools down for exactly five seconds", () => {
  assert.equal(PLAYBACK_COOLDOWN_MS, 5_000);
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  const cooldowns: number[] = [];

  for (let cycle = 0, now = 1_000; cycle < 8; cycle++) {
    if (cycle > 0) episode.restartEpisode(now);
    const attempts = [
      episode.nextFailure(now),
      episode.nextFailure(now + 5_000),
      episode.nextFailure(now + 10_000),
      episode.nextFailure(now + 20_000),
    ];
    assert.deepEqual(attempts.map((step) => step.attempt), [2, 3, 4, 5]);
    const cooldown = episode.nextFailure(now + 30_000);
    assert.deepEqual(cooldown, { action: "cooldown", until: now + 35_000 });
    cooldowns.push(cooldown.until - (now + 30_000));
    now = cooldown.until;
  }
  assert.deepEqual(cooldowns, Array(8).fill(5_000));
});

test("a recovered cooldown retry that runs for 183 seconds gets fresh WebRTC, MSE, fallback steps", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  episode.recordFailure(1_000);
  episode.nextFailure(1_000);
  episode.nextFailure(6_000);
  episode.nextFailure(11_000);
  episode.nextFailure(21_000);
  assert.deepEqual(episode.nextFailure(31_000), { action: "cooldown", until: 36_000 });

  episode.restartEpisode(36_000);
  for (let now = 36_100; now <= 219_100; now += 1_000) episode.recordProgress(now);
  assert.equal(episode.remainingMs(219_100), 30_000, "sustained media progress clears the prior deadline");

  episode.recordFailure(220_100);
  assert.deepEqual(episode.nextFailure(220_100), { transport: "webrtc", streamName: "yard-live", attempt: 2 });
  assert.deepEqual(episode.nextFailure(225_100), { transport: "mse", streamName: "yard-live", attempt: 3 });
  assert.deepEqual(episode.nextFailure(230_100), { transport: "mse", streamName: "yard-focus", attempt: 4 });
});

test("five seconds of continuous genuine progress clears the deadline and retry step", () => {
  assert.equal(PLAYBACK_STABLE_RESET_MS, 5_000);
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  episode.recordFailure(1_000);
  episode.nextFailure(1_000);
  episode.nextFailure(2_000);

  assert.equal(episode.recordProgress(3_000), false);
  assert.equal(episode.recordProgress(8_000), true);
  assert.equal(episode.remainingMs(8_001), 30_000);
  episode.recordFailure(9_000);
  assert.deepEqual(episode.nextFailure(9_000), { transport: "webrtc", streamName: "yard-live", attempt: 2 });
});

test("a reported failure breaks the continuous five-second stable-progress interval", () => {
  const episode = new PlaybackRecovery(["yard-live"]);

  assert.equal(episode.recordProgress(1_000), false);
  assert.equal(episode.recordProgress(4_000), false);
  episode.recordFailure(5_000);
  assert.equal(episode.recordProgress(6_000), false);
  assert.equal(episode.recordProgress(10_999), false);
  assert.equal(episode.recordProgress(11_000), true);
});

test("a progress gap longer than the stall threshold restarts the stable interval", () => {
  const episode = new PlaybackRecovery(["yard-live"]);

  assert.equal(episode.recordProgress(1_000), false);
  assert.equal(episode.recordProgress(1_000 + PLAYBACK_STALL_MS), true);
  assert.equal(episode.recordProgress(1_001 + PLAYBACK_STALL_MS * 2), false);
  assert.equal(episode.recordProgress(6_000 + PLAYBACK_STALL_MS * 2), false);
  assert.equal(episode.recordProgress(6_001 + PLAYBACK_STALL_MS * 2), true);
});

test("cooldown restart preserves stalled telemetry until real media progress", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  episode.recordFailure(1_000);
  assert.equal(episode.stalledForMs(1_000), 0);
  assert.equal(episode.nextFailure(1_000).attempt, 2);
  assert.equal(episode.stalledForMs(6_000), 5_000);
  assert.equal(episode.nextFailure(6_000).attempt, 3);
  assert.equal(episode.nextFailure(11_000).attempt, 4);
  assert.equal(episode.nextFailure(21_000).attempt, 5);
  assert.deepEqual(episode.nextFailure(31_000), { action: "cooldown", until: 36_000 });
  assert.equal(episode.stalledForMs(31_000), 30_000);

  episode.restartEpisode(36_000);
  assert.equal(episode.stalledForMs(36_000), 35_000);
  assert.equal(episode.nextFailure(36_000).attempt, 2);
  assert.equal(episode.stalledForMs(40_000), 39_000);
  assert.equal(episode.recordProgress(41_000), false);
  assert.equal(episode.stalledForMs(41_001), 0);
});

test("every cooldown restart revisits the exact transport, fallback, and deadline schedule", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  const assertEpisode = (startedAt: number) => {
    assert.deepEqual(episode.nextFailure(startedAt), {
      transport: "webrtc", streamName: "yard-live", attempt: 2,
    });
    assert.deepEqual(episode.nextFailure(startedAt + 5_000), {
      transport: "mse", streamName: "yard-live", attempt: 3,
    });
    assert.deepEqual(episode.nextFailure(startedAt + 10_000), {
      transport: "mse", streamName: "yard-focus", attempt: 4,
    });
    assert.deepEqual(episode.nextFailure(startedAt + 20_000), { action: "resubscribe", attempt: 5 });
    assert.equal(episode.remainingMs(startedAt + 29_999), 1);
    assert.deepEqual(episode.nextFailure(startedAt + 30_000), {
      action: "cooldown", until: startedAt + 35_000,
    });
  };

  episode.recordFailure(1_000);
  assertEpisode(1_000);
  episode.restartEpisode(36_000);
  assertEpisode(36_000);
});

test("one-frame intermittency cannot reset the finite recovery budget", () => {
  const episode = new PlaybackRecovery(["yard-live"]);
  episode.recordFailure(1_000);
  assert.equal(episode.nextFailure(1_000).attempt, 2);
  assert.equal(episode.recordProgress(20_000), false, "one decoded frame only starts the stable interval");
  episode.recordFailure(21_000);
  assert.equal(episode.remainingMs(30_999), 1);
  assert.deepEqual(episode.nextFailure(31_000), { action: "cooldown", until: 36_000 });
});

test("a single-candidate episode is bounded and uses the same cooldown", () => {
  const episode = new PlaybackRecovery(["yard-live"]);
  assert.equal(episode.nextFailure(1_000).attempt, 2);
  assert.equal(episode.nextFailure(2_000).attempt, 3);
  assert.deepEqual(episode.nextFailure(3_000), { action: "resubscribe", attempt: 4 });
  assert.deepEqual(episode.nextFailure(31_000), { action: "cooldown", until: 36_000 });
});

test("late retry delay stays bounded by the original 30-second episode deadline", () => {
  const episode = new PlaybackRecovery(["yard-live"]);
  episode.recordFailure(0);
  assert.equal(episode.remainingMs(28_000), 2_000);
  assert.equal(episode.boundedDelayMs(28_000, 5_000), 2_000);
  assert.equal(episode.remainingMs(30_000), 0);
  assert.deepEqual(episode.nextFailure(30_000), { action: "cooldown", until: 35_000 });
});

test("genuine media progress clears stall telemetry before it has been stable long enough to reset", () => {
  const episode = new PlaybackRecovery(["yard-live"]);
  episode.recordFailure(1_000);
  assert.equal(episode.stalledForMs(4_000), 3_000);
  assert.equal(episode.recordProgress(5_000), false);
  assert.equal(episode.stalledForMs(6_000), 0);
  assert.equal(episode.remainingMs(6_000), 25_000);
});
