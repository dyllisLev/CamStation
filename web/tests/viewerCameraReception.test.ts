import assert from "node:assert/strict";
import test from "node:test";
import type { Camera, Viewer } from "../src/app/api.ts";
import { viewerCameraReceptionSummary } from "../src/pages/viewers/viewerCameraReception.ts";

const referenceAt = "2026-08-13T03:30:30.000Z";

function camera(name: string, streamName: string): Camera {
  return {
    name,
    streamName,
    enabled: true,
    liveStreamName: `${streamName}-live`,
    focusStreamName: `${streamName}-focus`,
    state: "streaming",
    controlCapabilities: {
      ptz: { support: "unknown", available: false },
      home: { support: "unknown", available: false },
      presets: { support: "unknown", available: false },
      listen: { support: "unknown", available: false },
      talk: { support: "unknown", available: false },
      siren: { support: "unknown", available: false },
    },
    streamOutputs: [],
    streamApplyState: {
      desiredRevision: 1,
      appliedRevision: 1,
      state: "applied",
    },
    createdAt: "2026-08-13T00:00:00Z",
    updatedAt: "2026-08-13T00:00:00Z",
  };
}

function viewer(overrides: Partial<Viewer> = {}): Viewer {
  return {
    id: "viewer-1",
    displayName: "관제실",
    appVersion: "2.0.25",
    hostname: "viewer-pc",
    deviceLabel: "",
    route: "/live?viewer=1",
    mode: "live",
    status: "online",
    viewer: { state: "running" },
    renderer: { state: "ready", lastHeartbeatAt: referenceAt },
    streams: [],
    createdAt: "2026-08-13T00:00:00Z",
    lastHeartbeatAt: referenceAt,
    updatedAt: referenceAt,
    ...overrides,
  };
}

test("counts one current progressing live or focus stream per enabled camera", () => {
  const cameras = [camera("마당", "yard"), camera("창고", "store")];
  const summary = viewerCameraReceptionSummary(viewer({
    streams: [
      {
        streamName: "yard-live",
        state: "playing",
        transport: "webrtc",
        lastProgressAt: "2026-08-13T03:30:29.000Z",
        updatedAt: "2026-08-13T03:30:30.000Z",
      },
      {
        streamName: "store-focus",
        state: "playing",
        transport: "mse",
        lastProgressAt: "2026-08-13T03:30:28.000Z",
        updatedAt: "2026-08-13T03:30:30.000Z",
      },
    ],
  }), cameras);

  assert.equal(summary.receivingCount, 2);
  assert.equal(summary.totalCount, 2);
  assert.deepEqual(summary.missing, []);
  assert.equal(summary.receptions[1]?.activeStreamName, "store-focus");
  assert.equal(summary.receptions[1]?.transport, "mse");
});

test("identifies missing cameras and provides the normal live stream as the isolated retry target", () => {
  const cameras = [camera("마당", "yard"), camera("창고", "store")];
  const summary = viewerCameraReceptionSummary(viewer({
    streams: [
      {
        streamName: "yard-live",
        state: "playing",
        lastProgressAt: "2026-08-13T03:30:29.000Z",
        updatedAt: "2026-08-13T03:30:30.000Z",
      },
      {
        streamName: "store-live",
        state: "cooldown",
        lastProgressAt: "2026-08-13T03:29:00.000Z",
        updatedAt: "2026-08-13T03:30:30.000Z",
      },
    ],
  }), cameras);

  assert.equal(summary.receivingCount, 1);
  assert.equal(summary.totalCount, 2);
  assert.deepEqual(summary.missing.map((item) => item.cameraName), ["창고"]);
  assert.equal(summary.missing[0]?.resubscribeStreamName, "store-live");
});

test("does not count stale playing rows or rows without video progress", () => {
  const cameras = [camera("마당", "yard"), camera("창고", "store")];
  const summary = viewerCameraReceptionSummary(viewer({
    streams: [
      {
        streamName: "yard-live",
        state: "playing",
        lastProgressAt: "2026-08-13T03:29:00.000Z",
        updatedAt: "2026-08-13T03:29:00.000Z",
      },
      {
        streamName: "store-live",
        state: "playing",
        updatedAt: "2026-08-13T03:30:30.000Z",
      },
    ],
  }), cameras);

  assert.equal(summary.receivingCount, 0);
  assert.deepEqual(summary.missing.map((item) => item.cameraName), ["마당", "창고"]);
});

test("prefers the camera's newest candidate state over a recently playing historical candidate", () => {
  const summary = viewerCameraReceptionSummary(viewer({
    streams: [
      {
        streamName: "yard-live",
        state: "playing",
        lastProgressAt: "2026-08-13T03:30:27.000Z",
        updatedAt: "2026-08-13T03:30:27.000Z",
      },
      {
        streamName: "yard-focus",
        state: "cooldown",
        lastProgressAt: "2026-08-13T03:30:27.000Z",
        updatedAt: "2026-08-13T03:30:30.000Z",
      },
    ],
  }), [camera("마당", "yard")]);

  assert.equal(summary.receivingCount, 0);
  assert.equal(summary.receptions[0]?.activeStreamName, "yard-focus");
});

test("ignores disabled cameras and reports no reception when the Viewer renderer is unavailable", () => {
  const disabled = { ...camera("퇴역", "retired"), enabled: false };
  const summary = viewerCameraReceptionSummary(viewer({
    status: "offline",
    streams: [{
      streamName: "yard-live",
      state: "playing",
      lastProgressAt: "2026-08-13T03:30:29.000Z",
      updatedAt: "2026-08-13T03:30:30.000Z",
    }],
  }), [camera("마당", "yard"), disabled]);

  assert.equal(summary.totalCount, 1);
  assert.equal(summary.receivingCount, 0);
  assert.deepEqual(summary.missing.map((item) => item.cameraName), ["마당"]);
});

test("uses the stable stream for legacy cameras without role outputs", () => {
  const legacy = { ...camera("구형", "legacy"), liveStreamName: undefined, focusStreamName: undefined };
  const summary = viewerCameraReceptionSummary(viewer({
    streams: [{
      streamName: "legacy",
      state: "playing",
      lastProgressAt: "2026-08-13T03:30:29.000Z",
      updatedAt: "2026-08-13T03:30:30.000Z",
    }],
  }), [legacy]);

  assert.equal(summary.receivingCount, 1);
  assert.equal(summary.receptions[0]?.resubscribeStreamName, "legacy");
});
