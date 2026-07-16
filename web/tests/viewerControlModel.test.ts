import assert from "node:assert/strict";
import test from "node:test";
import {
  canCancelViewerCommand,
  viewerAgentState,
  viewerControlState,
} from "../src/pages/viewers/viewerFormat.ts";

test("keeps Agent and control health independent", () => {
  const viewer = {
    agent: { state: "online" },
    control: { state: "control_degraded" },
  };
  assert.equal(viewerAgentState(viewer), "online");
  assert.equal(viewerControlState(viewer), "control_degraded");
});

test("allows cancellation only while the backend accepts it", () => {
  assert.equal(canCancelViewerCommand("pending"), true);
  assert.equal(canCancelViewerCommand("delivered"), true);
  for (const state of ["acknowledged", "running", "succeeded", "failed", "rejected", "expired", "cancelled", "deleted"]) {
    assert.equal(canCancelViewerCommand(state), false, state);
  }
});
