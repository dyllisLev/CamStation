import assert from "node:assert/strict";
import test from "node:test";
import { resolveInitialLayout, resolveLayoutAfterDelete } from "../src/components/live/liveLayoutState.ts";

const cameras = [{ streamName: "yard" }, { streamName: "gate" }];
const saved = {
  id: "saved",
  name: "저장 배치",
  data: [
    { i: "yard", x: 12, y: 4, w: 20, h: 16 },
    { i: "gate", x: 32, y: 4, w: 16, h: 16 },
  ],
  timeline_collapsed: true,
};

test("waits for layouts when cameras arrive first", () => {
  assert.equal(resolveInitialLayout(cameras, [], "saved", false), null);
  const result = resolveInitialLayout(cameras, [saved], "saved", true);
  assert.equal(result?.currentId, "saved");
  assert.deepEqual(result?.layout.map(({ i, x, y, w, h }) => ({ i, x, y, w, h })), saved.data);
  assert.equal(result?.timelineCollapsed, true);
});

test("falls back to the newest saved layout when the remembered id is absent", () => {
  const newest = { ...saved, id: "newest", name: "최신" };
  assert.equal(resolveInitialLayout(cameras, [newest, saved], "missing", true)?.currentId, "newest");
});

test("deleting a non-current layout leaves selection unchanged", () => {
  assert.equal(resolveLayoutAfterDelete("saved", "other", [saved], cameras), null);
});

test("deleting the current layout selects the newest remaining layout", () => {
  const next = { ...saved, id: "next", name: "다음" };
  assert.equal(resolveLayoutAfterDelete("saved", "saved", [saved, next], cameras)?.currentId, "next");
});

test("deleting the final layout returns an unsaved default", () => {
  const result = resolveLayoutAfterDelete("saved", "saved", [saved], cameras);
  assert.equal(result?.currentId, "");
  assert.equal(result?.timelineCollapsed, undefined);
  assert.deepEqual(result?.layout.map((item) => item.i), ["yard", "gate"]);
});
