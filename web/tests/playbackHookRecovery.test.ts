import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import test from "node:test";
import * as ts from "typescript";
import * as recoveryModule from "../src/components/live/playbackRecovery.ts";

type Timer = { readonly due: number; readonly callback: () => void };
type ConnectionOptions = {
  readonly video: HTMLVideoElement;
  readonly streamName: string;
  readonly onFailure: (category: "socket") => void;
  readonly onEvent: (event: "socket_open") => void;
  readonly onBinary: () => void;
};
type FailureMode = "refuse" | "blackhole";
type FakeConnection = {
  readonly options: ConnectionOptions;
  readonly openedAt: number;
  active: boolean;
  sourceLost: boolean;
  readonly timerIds: number[];
};

class FakeClock {
  now = 0;
  private nextId = 0;
  private readonly timers = new Map<number, Timer>();

  set = (callback: () => void, delayMs: number): number => {
    const id = ++this.nextId;
    this.timers.set(id, { due: this.now + Math.max(0, delayMs), callback });
    return id;
  };

  clear = (id: number): void => {
    this.timers.delete(id);
  };

  get pendingCount(): number {
    return this.timers.size;
  }

  advance(ms: number): void {
    const end = this.now + ms;
    let callbacksRun = 0;
    for (;;) {
      assert.ok(++callbacksRun <= 100_000, "fake clock callback cap exceeded");
      let next: [number, Timer] | undefined;
      for (const entry of this.timers) {
        if (entry[1].due <= end && (!next || entry[1].due < next[1].due)) next = entry;
      }
      if (!next) break;
      this.timers.delete(next[0]);
      this.now = next[1].due;
      next[1].callback();
    }
    this.now = end;
  }
}

class FakeVideo {
  currentTime = 0;
  readyState = 4;
  private readonly listeners = new Set<() => void>();

  addEventListener(event: string, callback: () => void): void {
    if (event === "timeupdate") this.listeners.add(callback);
  }

  removeEventListener(event: string, callback: () => void): void {
    if (event === "timeupdate") this.listeners.delete(callback);
  }

  progress(): void {
    this.currentTime += 1;
    for (const callback of this.listeners) callback();
  }
}

function createHookHarness({
  initialSourceAvailable = false,
  failureMode = "refuse" as FailureMode,
  streamNames = "yard-live",
}: {
  readonly initialSourceAvailable?: boolean;
  readonly failureMode?: FailureMode;
  readonly streamNames?: string | readonly string[];
} = {}) {
  const clock = new FakeClock();
  const video = new FakeVideo();
  const connections: FakeConnection[] = [];
  const diagnostics: unknown[] = [];
  const state: { current: Record<string, unknown> } = { current: {} };
  let sourceAvailable = initialSourceAvailable;
  let healthyProgressForMs = Number.POSITIVE_INFINITY;
  let cleanup: (() => void) | undefined;
  let maxActiveConnections = 0;
  const refs: Array<{ current: unknown }> = [];
  let hookIndex = 0;
  const effects: Array<() => void | (() => void)> = [];

  const react = {
    useRef<T>(initial: T) {
      const index = hookIndex++;
      return refs[index] ??= { current: initial } as { current: T };
    },
    useState<T>(initial: T | (() => T)) {
      const index = hookIndex++;
      if (!(index in refs)) refs[index] = { current: typeof initial === "function" ? (initial as () => T)() : initial };
      const set = (next: T | ((previous: T) => T)) => {
        const previous = refs[index].current as T;
        refs[index].current = typeof next === "function" ? (next as (value: T) => T)(previous) : next;
        state.current = refs[index].current as Record<string, unknown>;
      };
      state.current = refs[index].current as Record<string, unknown>;
      return [refs[index].current as T, set] as const;
    },
    useEffect(effect: () => void | (() => void)) {
      effects.push(effect);
    },
  };

  const openPlaybackConnection = (options: ConnectionOptions) => {
    const connection: FakeConnection = { options, openedAt: clock.now, active: true, sourceLost: false, timerIds: [] };
    connections.push(connection);
    const active = connections.filter((candidate) => candidate.active).length;
    maxActiveConnections = Math.max(maxActiveConnections, active);
    assert.equal(active, 1, "a recovery attempt must close its predecessor before opening another connection");
    options.onEvent("socket_open");
    const schedule = (delay: number, callback: () => void) => {
      const id = clock.set(() => {
        if (connection.active) callback();
      }, delay);
      connection.timerIds.push(id);
    };
    if (!sourceAvailable) {
      if (failureMode === "refuse") schedule(100, () => options.onFailure("socket"));
    } else {
      const progress = () => {
        if (!sourceAvailable || connection.sourceLost) return;
        options.onBinary();
        video.progress();
        if (clock.now - connection.openedAt < healthyProgressForMs) schedule(1_000, progress);
      };
      schedule(100, progress);
    }
    return () => {
      if (!connection.active) return;
      connection.active = false;
      for (const id of connection.timerIds) clock.clear(id);
    };
  };

  const source = readFileSync(new URL("../src/components/live/useWebRtcMseStream.ts", import.meta.url), "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    fileName: "useWebRtcMseStream.ts",
  }).outputText;
  const localRequire = createRequire(import.meta.url);
  const module = { exports: {} as Record<string, unknown> };
  const require = (name: string): unknown => {
    if (name === "react") return react;
    if (name === "../../app/playbackDiagnosticsApi") return { reportPlaybackDiagnostic: (event: unknown) => diagnostics.push(event) };
    if (name === "./playbackConnection") return { openPlaybackConnection, probePlaybackProgress: async () => false };
    if (name === "./playbackRecovery") return recoveryModule;
    if (name === "./videoFrameProgress") return { observePresentedVideoFrames: () => () => undefined };
    return localRequire(name);
  };
  const originalDate = globalThis.Date;
  const originalSetTimeout = globalThis.setTimeout;
  const originalClearTimeout = globalThis.clearTimeout;
  const originalWindow = globalThis.window;
  class ClockDate extends originalDate {
    static now(): number { return clock.now; }
  }
  Object.defineProperty(globalThis, "Date", { configurable: true, value: ClockDate });
  Object.defineProperty(globalThis, "setTimeout", { configurable: true, value: clock.set });
  Object.defineProperty(globalThis, "clearTimeout", { configurable: true, value: clock.clear });
  Object.defineProperty(globalThis, "window", { configurable: true, value: { setTimeout: clock.set, clearTimeout: clock.clear } });
  try {
    new Function("require", "exports", "module", compiled)(require, module.exports, module);
    hookIndex = 0;
    const hook = module.exports.useWebRtcMseStream as (streams: string | readonly string[], generation?: number, transport?: "webrtc", surface?: "unknown") => { videoRef: { current: unknown } };
    const result = hook(streamNames, 0, "webrtc", "unknown");
    result.videoRef.current = video as unknown as HTMLVideoElement;
    cleanup = effects.map((effect) => effect()).find((value): value is () => void => typeof value === "function");
  } catch (error) {
    Object.defineProperty(globalThis, "Date", { configurable: true, value: originalDate });
    Object.defineProperty(globalThis, "setTimeout", { configurable: true, value: originalSetTimeout });
    Object.defineProperty(globalThis, "clearTimeout", { configurable: true, value: originalClearTimeout });
    Object.defineProperty(globalThis, "window", { configurable: true, value: originalWindow });
    throw error;
  }

  return {
    clock,
    video,
    connections,
    diagnostics,
    state,
    setSource(available: boolean, progressForMs = Number.POSITIVE_INFINITY) {
      if (!available && sourceAvailable) {
        for (const connection of connections) {
          if (connection.active) connection.sourceLost = true;
        }
      }
      sourceAvailable = available;
      healthyProgressForMs = progressForMs;
    },
    maxActiveConnections: () => maxActiveConnections,
    unmount() {
      cleanup?.();
      Object.defineProperty(globalThis, "Date", { configurable: true, value: originalDate });
      Object.defineProperty(globalThis, "setTimeout", { configurable: true, value: originalSetTimeout });
      Object.defineProperty(globalThis, "clearTimeout", { configurable: true, value: originalClearTimeout });
      Object.defineProperty(globalThis, "window", { configurable: true, value: originalWindow });
    },
  };
}

test("the actual hook recovers short and prolonged camera outages promptly without overlapping connections", () => {
  for (const outageMs of [1_000, 15 * 60_000]) {
    const harness = createHookHarness({ initialSourceAvailable: true });
    try {
      harness.clock.advance(1_200);
      assert.equal(harness.state.current.phase, "playing", "the tile must start healthy before the outage");
      const originalConnection = harness.connections.at(-1);
      assert.ok(originalConnection?.active);
      harness.setSource(false);
      harness.clock.advance(outageMs);
      const availableAt = harness.clock.now;
      harness.setSource(true);
      harness.clock.advance(15_000);

      assert.equal(harness.state.current.phase, "playing", `a ${outageMs}ms outage must reconnect after the source returns`);
      assert.ok((harness.state.current.lastProgressAt as number) - availableAt <= 15_000);
      assert.equal(originalConnection?.sourceLost, true, "a dropped source must stop the already-open video's frames");
      assert.equal(harness.maxActiveConnections(), 1);
      assert.ok(
        harness.connections.every((connection, index, attempts) => (
          index === 0 || connection.openedAt - attempts[index - 1].openedAt >= 3_000
        )),
        "every failed recovery attempt must start at least three seconds after its predecessor",
      );
      assert.ok(
        harness.connections.length <= Math.ceil(outageMs / 1_500) + 5,
        "rapid connection refusals must be paced instead of spinning through attempts",
      );
    } finally {
      harness.unmount();
    }
  }
});

test("blackholed setup attempts time out across primary and focus phases, then recover within fifteen seconds", () => {
  for (const returnAt of [1_000, 7_000, 16_000]) {
    const harness = createHookHarness({
      failureMode: "blackhole",
      streamNames: ["yard-live", "yard-focus"],
    });
    try {
      harness.clock.advance(returnAt);
      const availableAt = harness.clock.now;
      harness.setSource(true);
      harness.clock.advance(15_000);

      assert.equal(harness.state.current.phase, "playing", `a source returning at ${returnAt}ms must recover`);
      assert.ok((harness.state.current.lastProgressAt as number) - availableAt <= 15_000);
      assert.ok(harness.connections.length >= 2, "an unresponsive connection must wait for the five-second setup timeout");
      if (returnAt === 16_000) {
        assert.ok(
          harness.connections.some((connection) => connection.options.streamName === "yard-focus"),
          "the blackholed recovery must exercise the focus candidate before the source returns",
        );
      }
    } finally {
      harness.unmount();
    }
  }
});

test("a recovered cooldown attempt resets quickly, while stale callbacks cannot revive cooldown or leak timers", () => {
  const harness = createHookHarness();
  try {
    while (harness.state.current.phase !== "cooldown") harness.clock.advance(100);
    assert.equal(harness.state.current.phase, "cooldown");
    const staleConnection = harness.connections.at(-1);
    assert.ok(staleConnection);

    harness.setSource(true, 183_000);
    harness.clock.advance(5_100);
    assert.equal(harness.state.current.phase, "playing", "the cooldown probe should connect once the source returns");
    const recoveredConnection = harness.connections.at(-1);
    assert.ok(recoveredConnection && recoveredConnection !== staleConnection);

    harness.clock.advance(194_000);
    assert.notEqual(harness.state.current.phase, "cooldown", "183 seconds of continuous progress must reset the recovery episode before its next stall");
    assert.ok(harness.connections.length > 2, "the post-stall recovery must begin rather than waiting for a minutes-long cooldown");

    harness.setSource(false);
    harness.clock.advance(14_000);
    assert.equal(harness.state.current.phase, "cooldown");
    const timersBeforeLateCallbacks = harness.clock.pendingCount;
    staleConnection.options.onFailure("socket");
    harness.video.progress();
    assert.equal(harness.state.current.phase, "cooldown", "late failure and queued timeupdate must not claim playback recovered");
    assert.equal(harness.clock.pendingCount, timersBeforeLateCallbacks, "late callbacks must not rearm a stall timer during cooldown");

    harness.unmount();
    assert.equal(harness.clock.pendingCount, 0);
    assert.equal(harness.connections.filter((connection) => connection.active).length, 0);
    harness.video.progress();
    assert.equal(harness.clock.pendingCount, 0, "cleanup removes the timeupdate listener as well as timer callbacks");
  } finally {
    harness.unmount();
  }
});
