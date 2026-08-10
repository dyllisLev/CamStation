import assert from "node:assert/strict";
import test from "node:test";
import type { Viewer } from "../src/app/api.ts";
import {
  VIEWER_COMMAND_ACTIONS,
  viewerCommandAction,
  viewerCommandIsActive,
  viewerCommandTypeLabel,
  viewerCommandUnavailableReason,
} from "../src/pages/viewers/viewerCommandModel.ts";

const viewer = {
  id: "viewer-1",
  displayName: "관제실",
  appVersion: "2.0.22",
  hostname: "viewer-pc",
  deviceLabel: "",
  route: "/live?viewer=1",
  mode: "live",
  status: "online",
  createdAt: "2026-08-10T00:00:00Z",
  lastHeartbeatAt: "2026-08-10T00:00:00Z",
  updatedAt: "2026-08-10T00:00:00Z",
  control: { state: "online" },
  viewer: { state: "running" },
  renderer: { state: "ready" },
  streams: [{ streamName: "gate-main", state: "playing" }],
} satisfies Viewer;

test("operator Viewer controls are a fixed Korean-labelled allowlist", () => {
  assert.deepEqual(VIEWER_COMMAND_ACTIONS.map((action) => action.type), [
    "ping", "reload_live", "resubscribe_stream", "restart_viewer", "restart_service",
  ]);
  assert.ok(VIEWER_COMMAND_ACTIONS.every((action) => action.label.length > 2 && action.description.length > 5));
  assert.equal(viewerCommandTypeLabel("restart_agent"), "Viewer 관리 서비스 다시 시작");
});

test("command availability follows independent control, Viewer, renderer, and stream axes", () => {
  assert.equal(viewerCommandUnavailableReason(viewer, viewerCommandAction("ping"), ""), "");
  assert.equal(viewerCommandUnavailableReason(viewer, viewerCommandAction("resubscribe_stream"), ""), "다시 연결할 카메라를 선택하세요.");
  assert.equal(viewerCommandUnavailableReason(viewer, viewerCommandAction("resubscribe_stream"), "gate-main"), "");
  assert.match(viewerCommandUnavailableReason({ ...viewer, control: { state: "degraded" } }, viewerCommandAction("ping"), ""), /제어 채널/);
  assert.match(viewerCommandUnavailableReason({ ...viewer, renderer: { state: "failed" } }, viewerCommandAction("reload_live"), ""), /화면/);
});

test("only nonterminal commands trigger active polling", () => {
  assert.equal(viewerCommandIsActive("pending"), true);
  assert.equal(viewerCommandIsActive("delivered"), true);
  assert.equal(viewerCommandIsActive("running"), true);
  assert.equal(viewerCommandIsActive("succeeded"), false);
  assert.equal(viewerCommandIsActive("cancelled"), false);
});
