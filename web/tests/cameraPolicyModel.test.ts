import assert from "node:assert/strict";
import test from "node:test";
import type { Camera } from "../src/app/cameraTypes.ts";
import {
  CAMERA_POLICY_INVALIDATION_KEYS,
  cameraPolicySurfaceKey,
  hasDistinctLiveSource,
  draftFromCamera,
  policyMutationNotice,
  normalizeUnavailableSources,
  recommendedStreamOutputs,
  reconcilePolicyDraft,
  reloadedPolicyDraft,
  streamOutputUpdateRequest,
  validateStreamOutputs,
} from "../src/pages/cameras/streamOutputPolicyModel.ts";

function policyCamera(revision = 7): Camera {
  const desired = recommendedStreamOutputs(true);
  return {
    name: "소방서5",
    streamName: "fire-station-5",
    state: "streaming",
    controlCapabilities: {
      ptz: { support: "supported", available: true },
      home: { support: "unsupported", available: false },
      presets: { support: "unknown", available: false },
      listen: { support: "unknown", available: false },
      talk: { support: "unknown", available: false },
      siren: { support: "unknown", available: false },
    },
    streams: [],
    streamOutputs: desired.map((output) => ({
      purpose: output.purpose,
      sourceKey: output.sourceKey,
      streamName: `fire-station-5-${output.purpose}`,
      desired: output,
      applied: output,
      source: { label: output.sourceKey },
      effective: null,
      verification: { state: "unverified" },
      runtime: { state: "idle", producerCount: 0, consumerCount: 0, viewerCount: 0 },
    })),
    streamApplyState: { desiredRevision: revision, appliedRevision: revision, state: "applied" },
    createdAt: "2026-07-11T00:00:00Z",
    updatedAt: "2026-07-11T00:00:00Z",
  };
}

test("recommended policies use recording copy, live auto, and capped focus", () => {
  assert.deepEqual(recommendedStreamOutputs(true), [
    { purpose: "recording", sourceKey: "recording", videoMode: "copy", maxWidth: null, maxHeight: null, maxFPS: null, audioMode: "source", activation: "on_demand" },
    { purpose: "live", sourceKey: "live", videoMode: "auto", maxWidth: null, maxHeight: null, maxFPS: null, audioMode: "none", activation: "on_demand" },
    { purpose: "focus", sourceKey: "recording", videoMode: "auto", maxWidth: 1920, maxHeight: 1080, maxFPS: null, audioMode: "none", activation: "on_demand" },
  ]);
  assert.equal(recommendedStreamOutputs(false)[1].sourceKey, "recording");
});

test("policy payload round trips from GET camera desired settings", () => {
  const camera = policyCamera();
  const draft = draftFromCamera(camera);
  assert.deepEqual(streamOutputUpdateRequest(draft), {
    expectedDesiredRevision: 7,
    outputs: camera.streamOutputs.map((output) => output.desired),
  });
});

test("validation rejects incomplete size, odd size, fractional fps, and copy limits", () => {
  const outputs = recommendedStreamOutputs(true);
  assert.equal(validateStreamOutputs(outputs), null);
  assert.match(validateStreamOutputs(outputs.map((output) => output.purpose === "focus" ? { ...output, maxHeight: null } : output)) ?? "", /폭과 높이/);
  assert.match(validateStreamOutputs(outputs.map((output) => output.purpose === "focus" ? { ...output, maxWidth: 1919 } : output)) ?? "", /짝수/);
  assert.match(validateStreamOutputs(outputs.map((output) => output.purpose === "live" ? { ...output, maxFPS: 29.97 } : output)) ?? "", /정수/);
  assert.match(validateStreamOutputs(outputs.map((output) => output.purpose === "recording" ? { ...output, maxFPS: 30 } : output)) ?? "", /원본 복사/);
  assert.match(validateStreamOutputs(outputs, ["recording"]) ?? "", /사용할 수 없는 원본 입력/);
});

test("dirty draft survives same-camera refetch and exposes server revision change", () => {
  const initial = draftFromCamera(policyCamera(7));
  const dirty = { ...initial, dirty: true, outputs: initial.outputs.map((output) => output.purpose === "live" ? { ...output, maxFPS: 15 } : output) };
  const reconciled = reconcilePolicyDraft(dirty, policyCamera(8));
  assert.equal(reconciled.baseRevision, 7);
  assert.equal(reconciled.serverRevision, 8);
  assert.equal(reconciled.outputs[1].maxFPS, 15);
  assert.equal(reconciled.dirty, true);
});

test("camera policy surfaces get a new instance key when mode or camera changes", () => {
  assert.equal(cameraPolicySurfaceKey("edit", "fire-station-1"), "edit:fire-station-1");
  assert.notEqual(cameraPolicySurfaceKey("edit", "fire-station-1"), cameraPolicySurfaceKey("edit", "fire-station-5"));
  assert.notEqual(cameraPolicySurfaceKey("edit", "fire-station-1"), cameraPolicySurfaceKey("create"));
});

test("server reload rebuilds the draft from the freshly fetched camera revision", () => {
  const reloaded = reloadedPolicyDraft([policyCamera(9)], "fire-station-5");
  assert.equal(reloaded?.baseRevision, 9);
  assert.equal(reloaded?.dirty, false);
});

test("live source is available only for a distinct backend producer", () => {
  const sameProducer = [
    { profileToken: "main", redactedUrl: "rtsp://camera/main", roleHint: "recording", label: "main", source: "rtsp" },
    { profileToken: "sub", redactedUrl: "rtsp://camera/main", roleHint: "live", label: "sub", source: "rtsp" },
  ];
  const distinctProducer = [
    sameProducer[0],
    { ...sameProducer[1], redactedUrl: "rtsp://camera/sub" },
  ];
  assert.equal(hasDistinctLiveSource(sameProducer, "main", "sub"), false);
  assert.equal(hasDistinctLiveSource(distinctProducer, "main", "sub"), true);
  assert.equal(hasDistinctLiveSource(distinctProducer, "main", "main"), false);
});

test("rescan keeps manual policy fields while remapping an unavailable live source", () => {
  const outputs = recommendedStreamOutputs(true);
  outputs[1] = { ...outputs[1], videoMode: "h264", maxFPS: 15, activation: "always" };
  const normalized = normalizeUnavailableSources(outputs, ["recording"]);
  assert.deepEqual(normalized[1], { ...outputs[1], sourceKey: "recording" });
  assert.equal(outputs[1].sourceKey, "live");
});

test("recommended reset creates a local dirty draft without mutating the server snapshot", () => {
  const camera = policyCamera();
  camera.streamOutputs[1].desired = { ...camera.streamOutputs[1].desired, videoMode: "h264" };
  const draft = draftFromCamera(camera);
  const reset = { ...draft, outputs: recommendedStreamOutputs(true), dirty: true };
  assert.equal(reset.outputs[1].videoMode, "auto");
  assert.equal(camera.streamOutputs[1].desired.videoMode, "h264");
});

test("mutation notice preserves applied, pending warning, and revision conflict states", () => {
  const camera = policyCamera();
  assert.deepEqual(policyMutationNotice({ saved: true, applied: true, camera }), { state: "applied", message: "저장 및 적용이 완료되었습니다." });
  assert.deepEqual(policyMutationNotice({ saved: true, applied: false, camera, warning: "rollback active" }), { state: "pending", message: "rollback active" });
  assert.deepEqual(policyMutationNotice(undefined, 409), { state: "conflict", message: "서버 설정이 변경되었습니다. 서버값을 다시 불러오세요." });
});

test("all policy mutations invalidate the five UI and runtime query surfaces", () => {
  assert.deepEqual(CAMERA_POLICY_INVALIDATION_KEYS, [
    ["cameras"],
    ["stream-status"],
    ["streams", "status"],
    ["recorder-status"],
    ["events"],
  ]);
});
