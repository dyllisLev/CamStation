import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { isViewerMode, viewerRoute } from "../src/app/viewerMode.ts";

test("Viewer mode requires the exact viewer query", () => {
  assert.equal(isViewerMode("?viewer=1"), true);
  assert.equal(isViewerMode("?viewer=0"), false);
  assert.equal(isViewerMode(""), false);
  assert.equal(viewerRoute("/recordings"), "/recordings?viewer=1");
});

test("Viewer layout retains live and recordings navigation only", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "../src/layouts/ConsoleLayout.tsx"), "utf8");
  assert.match(source, /viewerRoute\("\/live"\)/u);
  assert.match(source, /viewerRoute\("\/recordings"\)/u);
  assert.doesNotMatch(source, /viewerMode[\s\S]*viewerRoute\("\/settings"\)/u);
});
