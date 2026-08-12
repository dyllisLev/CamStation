import assert from "node:assert/strict";
import test from "node:test";
import type { LayoutProfile } from "../src/app/api.ts";
import { resolveViewerLayout, viewerRect } from "../src/components/viewer/viewerLayout.ts";

const cameras = [
  { streamName: "집-마당" },
  { streamName: "집-창고1" },
  { streamName: "집-창고2" },
];

const saved: LayoutProfile = {
  id: "home",
  name: "집 카메라",
  data: [
    { i: "집-마당", x: 0, y: 0, w: 24, h: 24 },
    { i: "집-창고1", x: 24, y: 0, w: 12, h: 12 },
    { i: "집-창고2", x: 36, y: 0, w: 12, h: 12 },
  ],
  timeline_collapsed: false,
  grid_cols: 48,
  grid_rows: 48,
  created_at: 1,
  updated_at: 1,
};

test("viewer uses the first saved layout and keeps every enabled camera", () => {
  const resolved = resolveViewerLayout(cameras, [saved]);
  assert.equal(resolved.cols, 48);
  assert.equal(resolved.rows, 48);
  assert.deepEqual(resolved.items.map((item) => item.i), cameras.map((camera) => camera.streamName));
});

test("viewer geometry maps saved grid units to bounded percentages", () => {
  assert.deepEqual(viewerRect(saved.data[0], 48, 48), {
    left: 0,
    top: 0,
    width: 50,
    height: 50,
  });
  const bounded = viewerRect({ i: "bad", x: 99, y: -1, w: 99, h: 0 }, 48, 48);
  assert.deepEqual({ ...bounded, height: 0 }, { left: 0, top: 0, width: 100, height: 0 });
  assert.ok(Math.abs(bounded.height - 100 / 48) < 1e-12);
});

test("viewer falls back to the deterministic camera layout", () => {
  const resolved = resolveViewerLayout(cameras, []);
  assert.equal(resolved.cols, 48);
  assert.equal(resolved.rows, 48);
  assert.deepEqual(resolved.items.map((item) => item.i), cameras.map((camera) => camera.streamName));
});
