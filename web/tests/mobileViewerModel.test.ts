import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import {
  clampMobileViewerPage,
  mobileViewerPageAfterSwipe,
  mobileViewerPageCount,
  mobileViewerPageItems,
  mobileViewerSwipeDirection,
} from "../src/pages/viewer/mobileViewerModel.ts";

test("mobile viewer divides cameras into four-camera pages", () => {
  const cameras = Array.from({ length: 7 }, (_, index) => `camera-${index}`);

  assert.equal(mobileViewerPageCount(0), 1);
  assert.equal(mobileViewerPageCount(4), 1);
  assert.equal(mobileViewerPageCount(5), 2);
  assert.deepEqual(mobileViewerPageItems(cameras, 0), cameras.slice(0, 4));
  assert.deepEqual(mobileViewerPageItems(cameras, 1), cameras.slice(4));
  assert.deepEqual(mobileViewerPageItems(cameras, 5), []);
});

test("mobile viewer uses the legacy strict fifty-pixel swipe threshold", () => {
  assert.equal(mobileViewerSwipeDirection(-51), "left");
  assert.equal(mobileViewerSwipeDirection(51), "right");
  assert.equal(mobileViewerSwipeDirection(-50), null);
  assert.equal(mobileViewerSwipeDirection(50), null);
  assert.equal(mobileViewerSwipeDirection(0), null);
});

test("mobile viewer swipe navigation stays within valid page bounds", () => {
  assert.equal(mobileViewerPageAfterSwipe(0, 2, -80), 1);
  assert.equal(mobileViewerPageAfterSwipe(1, 2, -80), 1);
  assert.equal(mobileViewerPageAfterSwipe(1, 2, 80), 0);
  assert.equal(mobileViewerPageAfterSwipe(0, 2, 80), 0);
  assert.equal(clampMobileViewerPage(4, 2), 1);
  assert.equal(clampMobileViewerPage(1, 1), 0);
});

test("mobile viewer is registered as a top-level route before the console shell", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "../src/app/App.tsx"), "utf8");
  const viewerRoute = source.indexOf('<Route path="viewer" element={<ViewerPage />} />');
  const consoleRoute = source.indexOf("<Route element={<ConsoleLayout />}");

  assert.notEqual(viewerRoute, -1);
  assert.notEqual(consoleRoute, -1);
  assert.ok(viewerRoute < consoleRoute);
});
