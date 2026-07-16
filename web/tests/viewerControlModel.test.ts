import assert from "node:assert/strict";
import test from "node:test";
import {
  canCancelViewerCommand,
  viewerAgentState,
  viewerControlState,
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
  assert.equal(canCancelViewerCommand("delivered"), true);
  for (const state of ["acknowledged", "running", "succeeded", "failed", "rejected", "expired", "cancelled", "deleted"]) {
    assert.equal(canCancelViewerCommand(state), false, state);
  }
});
