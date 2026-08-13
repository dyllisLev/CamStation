import assert from "node:assert/strict";
import test from "node:test";
import {
  PLAYBACK_PRIMARY_PROBE_INTERVAL_MS,
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
      clear: () => {
        timerCallback = null;
      },
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
  assert.ok(timerCallback, "both failures must schedule another cycle without disturbing fallback");

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
  const fireTimer = () => {
    const callback = timerCallback;
    timerCallback = null;
    callback?.();
  };
  const promoter = new PlaybackPrimaryPromoter(
    (_transport, signal) => new Promise<boolean>((resolve) => {
      signal.addEventListener("abort", () => {
        aborted = true;
        resolve(false);
      }, { once: true });
    }),
    {
      now: () => 1_000,
      set: (callback) => {
        timerCallback = callback;
        return 1;
      },
      clear: () => {
        timerCallback = null;
      },
    },
  );

  promoter.start("mse", {
    onProbeStarted: () => undefined,
    onProbeFailed: () => undefined,
    onRecovered: () => assert.fail("a stopped probe cannot promote"),
  });
  fireTimer();
  await Promise.resolve();
  promoter.stop();
  await Promise.resolve();

  assert.equal(aborted, true);
  assert.equal(promoter.active, false);
  assert.equal(timerCallback, null);
});

test("a verified primary promotion receives a fresh bounded recovery episode", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);
  episode.recordFailure(1_000);
  assert.equal(episode.remainingMs(31_001), 0);

  episode.resetForPrimaryPromotion();

  assert.equal(episode.remainingMs(31_001), 30_000);
  assert.deepEqual(episode.nextFailure(31_001), {
    transport: "webrtc",
    streamName: "yard-live",
    attempt: 2,
  });
});

test("each cooldown probe starts a fresh bounded episode that revisits fallback", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"]);

  episode.recordFailure(1_000);
  episode.nextFailure(1_000);
  episode.nextFailure(5_000);
  episode.nextFailure(10_000);
  episode.nextFailure(20_000);
  assert.deepEqual(episode.nextFailure(31_001), { action: "cooldown", until: 331_001 });

  episode.restartEpisode(331_001);

  assert.equal(episode.remainingMs(331_001), 30_000);
  assert.equal(episode.stalledForMs(331_001), 330_001);
  assert.deepEqual(episode.nextFailure(331_001), {
    transport: "webrtc",
    streamName: "yard-live",
    attempt: 2,
  });
  assert.deepEqual(episode.nextFailure(336_000), {
    transport: "mse",
    streamName: "yard-live",
    attempt: 3,
  });
  assert.deepEqual(episode.nextFailure(341_000), {
    transport: "mse",
    streamName: "yard-focus",
    attempt: 4,
  });
  assert.deepEqual(episode.nextFailure(351_000), { action: "resubscribe", attempt: 5 });
  assert.deepEqual(episode.nextFailure(361_002), { action: "cooldown", until: 661_002 });
});

test("the low-frequency probe scheduler can re-arm after every failed probe", () => {
  let now = 1_000;
  let nextTimer = 0;
  let pending: { readonly id: number; readonly callback: () => void; readonly delayMs: number } | null = null;
  const delays: number[] = [];
  const scheduler = new PlaybackProbeScheduler({
    now: () => now,
    set: (callback, delayMs) => {
      const timer = { id: ++nextTimer, callback, delayMs };
      pending = timer;
      delays.push(delayMs);
      return timer.id;
    },
    clear: (timerId) => {
      if (pending?.id === timerId) pending = null;
    },
  });
  let probes = 0;

  scheduler.arm(now + 300_000, () => probes++);
  assert.equal(pending?.delayMs, 300_000);
  pending?.callback();
  assert.equal(probes, 1);

  now += 305_000;
  scheduler.arm(now + 300_000, () => probes++);
  assert.equal(pending?.delayMs, 300_000);
  pending?.callback();

  assert.equal(probes, 2);
  assert.deepEqual(delays, [300_000, 300_000]);
});

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

test("a reported failure resets the continuous stable-progress interval", () => {
  const episode = new PlaybackRecovery(["yard-live"]);

  assert.equal(episode.recordProgress(1_000), false);
  for (let now = 10_000; now <= 280_000; now += 9_000) {
    assert.equal(episode.recordProgress(now), false);
  }
  episode.recordFailure(281_000);
  assert.equal(episode.recordProgress(282_000), false);
  for (let now = 291_000; now < 582_000; now += 9_000) {
    assert.equal(episode.recordProgress(now), false);
  }
  assert.equal(episode.recordProgress(582_000), true);
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
