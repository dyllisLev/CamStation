import assert from "node:assert/strict";
import test from "node:test";
import { recoveryAttemptPresentation } from "../src/components/live/playbackRecovery.ts";
import { playbackStatusCopy } from "../src/components/live/playbackPresentation.ts";

test("transport fallback on the same live stream is not labeled as a fallback stream", () => {
  const presentation = recoveryAttemptPresentation(
    { transport: "mse", streamName: "yard-live", attempt: 3 },
    "yard-live",
    "webrtc",
  );

  assert.deepEqual(presentation, {
    phase: "retrying",
    usingFallback: false,
    transportChanged: true,
  });
  assert.equal(playbackStatusCopy(presentation.phase, "mse"), "영상 연결 방식 전환 중...");
});

test("only switching from live to focus is labeled as a fallback stream", () => {
  const presentation = recoveryAttemptPresentation(
    { transport: "mse", streamName: "yard-focus", attempt: 4 },
    "yard-live",
    "mse",
  );

  assert.deepEqual(presentation, {
    phase: "fallback",
    usingFallback: true,
    transportChanged: false,
  });
  assert.equal(playbackStatusCopy(presentation.phase, "mse"), "대체 스트림 연결 중...");
});
