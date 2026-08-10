import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";
import {
  canCancelViewerCommand,
  canDeleteViewer,
  viewerAgentState,
  viewerControlState,
  viewerDeleteBlockedMessage,
} from "../src/pages/viewers/viewerFormat.ts";

test("keeps Agent and control health independent", () => {
  const viewer = {
    status: "online",
    agent: { state: "online" },
    control: { state: "control_degraded" },
  };
  assert.equal(viewerAgentState(viewer), "online");
  assert.equal(viewerControlState(viewer), "control_degraded");
});

test("server liveness overrides stale stored Agent health", () => {
  for (const status of ["offline", "stale"] as const) {
    const viewer = {
      status,
      agent: { state: "online" },
      control: { state: "healthy" },
    };
    assert.equal(viewerAgentState(viewer), status);
    assert.equal(viewerControlState(viewer), "healthy");
  }
});

test("allows cancellation only while the backend accepts it", () => {
  assert.equal(canCancelViewerCommand("pending"), true);
  for (const state of ["delivered", "acknowledged", "running", "succeeded", "failed", "rejected", "expired", "cancelled", "deleted"]) {
    assert.equal(canCancelViewerCommand(state), false, state);
  }
});

test("allows Viewer deletion only for server-deletable states", () => {
  assert.equal(canDeleteViewer("offline"), true);
  assert.equal(canDeleteViewer("stale"), true);
  for (const status of [undefined, "online", "running", "control_degraded", "failed"]) {
    assert.equal(canDeleteViewer(status), false, status);
  }
  assert.match(viewerDeleteBlockedMessage(), /오프라인/);
  assert.match(viewerDeleteBlockedMessage(), /30초/);
});

test("does not expose synthetic heartbeat registration in the operator Viewer UI", () => {
  const pageSource = readFileSync(new URL("../src/pages/ViewersPage.tsx", import.meta.url), "utf8");
  const panelDirectory = new URL("../src/pages/viewers/", import.meta.url);
  const panelSources = readdirSync(panelDirectory)
    .filter((name) => name.endsWith(".tsx"))
    .map((name) => readFileSync(new URL(name, panelDirectory), "utf8"))
    .join("\n");
  const operatorSources = `${pageSource}\n${panelSources}`;

  assert.doesNotMatch(operatorSources, /ViewerHeartbeatPanel|하트비트 등록|viewer-qa-01|QA Viewer/);
  assert.match(operatorSources, /Viewer 앱을 설치하고 서버에 연결하면 자동으로 등록됩니다/);
});
