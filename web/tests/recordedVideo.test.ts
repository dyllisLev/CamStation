import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import * as ts from "typescript";
import * as playbackClock from "../src/components/playback/playbackClock.ts";

// Exercise the adapter's actual effects. The media element deliberately stays
// at HAVE_METADATA until a seek reaches a buffered sample, as in the failure.
function adapter(nativeHls = false, growing = false) {
  const effects: Array<() => void | (() => void)> = [];
  const timers = new Map<number, { callback: () => void; delay: number }>();
  let timerId = 0;
  let ready = 0;
  let failures = 0;
  const hlsInstances: FakeHls[] = [];
  const video = new class extends EventTarget {
    src = "";
    readyState = 0;
    duration = 60;
    seeking = false;
    position = 0;
    ranges: number[][] = [];
    get currentTime() { return this.position; }
    set currentTime(value: number) { this.position = value; this.seeking = true; }
    get buffered() {
      return { length: this.ranges.length, start: (i: number) => this.ranges[i][0], end: (i: number) => this.ranges[i][1] };
    }
    canPlayType() { return nativeHls ? "probably" : ""; }
    pause() {}
    load() {}
    removeAttribute(name: string) { if (name === "src") this.src = ""; }
  }();
  class FakeHls {
    static Events = { ERROR: "error", FRAG_BUFFERED: "buffered" };
    static isSupported() { return true; }
    listeners = new Map<string, () => void>();
    destroyed = false;
    source = "";
    constructor() { hlsInstances.push(this); }
    on(event: string, listener: () => void) { this.listeners.set(event, listener); }
    loadSource(url: string) { this.source = url; }
    attachMedia() {}
    destroy() { this.destroyed = true; }
  }
  const workspace = {
    cameras: { "camera:7": { requestId: 1, media: {
      id: "fmp4-7", kind: "hls", url: "/api/playback/media/fmp4-7/manifest.m3u8",
      startMs: 100_000, endMs: 160_000, mediaStartSeconds: 0, growing,
    } } },
    attachVideo: () => () => undefined,
    currentTime: () => 100_000,
    setVideoMuted: () => undefined,
    videoReady: () => { ready++; },
    videoError: () => { failures++; },
    videoEnded: () => undefined,
    muted: true,
  };
  const source = readFileSync(new URL("../src/components/playback/RecordedVideo.tsx", import.meta.url), "utf8");
  const compiled = ts.transpileModule(source, { compilerOptions: {
    module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX,
  } }).outputText;
  const require = (name: string) => {
    if (name === "react") return { useRef: () => ({ current: video }), useEffect: (effect: () => void | (() => void)) => effects.push(effect) };
    if (name === "react/jsx-runtime") return { jsx: () => null };
    if (name === "hls.js") return { __esModule: true, default: FakeHls };
    if (name === "../../app/basePath") return { withAppBase: (url: string) => url };
    if (name === "./playbackClock") return playbackClock;
    throw new Error(`unexpected adapter import: ${name}`);
  };
  const module = { exports: {} as { RecordedVideo: (props: unknown) => unknown } };
  const window = {
    setTimeout: (callback: () => void, delay: number) => { const id = ++timerId; timers.set(id, { callback, delay }); return id; },
    clearTimeout: (id: number) => timers.delete(id),
  };
  new Function("require", "module", "exports", "window", compiled)(require, module, module.exports, window);
  module.exports.RecordedVideo({ workspace, cameraKey: "camera:7" });
  const cleanups = effects.map(effect => effect());
  return {
    video, get hls() { return hlsInstances.at(-1); }, get ready() { return ready; }, get failures() { return failures; },
    flushBuffer() {
      for (const [id, timer] of timers) if (timer.delay === 0) { timers.delete(id); timer.callback(); }
    },
    cleanup() { cleanups.reverse().forEach(cleanup => cleanup?.()); },
    get pending() { return timers.size; },
  };
}

test("native HLS is selected when the browser supports it, even with MSE available", () => {
  const a = adapter(true);
  assert.equal(a.hls, undefined);
  assert.equal(a.video.src, "/api/playback/media/fmp4-7/manifest.m3u8");
  a.video.readyState = 4;
  a.video.dispatchEvent(new Event("loadeddata"));
  assert.equal(a.ready, 1);
  a.cleanup();
  assert.equal(a.video.src, "");
  assert.equal(a.pending, 0);
});

test("MSE preparation leaves an empty lead-in when the fragment arrives before canplay", () => {
  const a = adapter();
  assert.ok(a.hls);
  a.video.readyState = 1;
  a.video.dispatchEvent(new Event("loadedmetadata"));
  assert.equal(a.ready, 0);
  a.video.ranges = [[2.313216, 6.3569]];
  a.hls.listeners.get("buffered")?.();
  a.flushBuffer();
  assert.equal(a.video.currentTime, 2.313216, "reach the available sample without waiting for canplay");
  assert.equal(a.ready, 0, "a seek request is not a decoded frame");
  a.video.seeking = false;
  a.video.readyState = 4;
  a.video.dispatchEvent(new Event("seeked"));
  assert.equal(a.ready, 1);
  a.video.dispatchEvent(new Event("canplay"));
  assert.equal(a.ready, 1);
  assert.equal(a.failures, 0);
  a.cleanup();
  assert.equal(a.hls.destroyed, true);
  assert.equal(a.pending, 0);
});

test("a growing recording retains explicit MSE positioning instead of native live-edge startup", () => {
  const a = adapter(true, true);
  assert.ok(a.hls);
  assert.equal(a.video.src, "");
  a.cleanup();
});

test("an obsolete adapter clears pending buffer preparation and ignores late events", () => {
  const a = adapter();
  a.video.readyState = 1;
  a.video.ranges = [[2, 4]];
  a.hls?.listeners.get("buffered")?.();
  a.cleanup();
  a.flushBuffer();
  a.video.dispatchEvent(new Event("canplay"));
  assert.equal(a.video.currentTime, 0);
  assert.equal(a.ready, 0);
  assert.equal(a.pending, 0);
});
