import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

test("Viewer recordings omit external playback and download actions", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "../src/pages/recordings/RecordingSegmentsPanel.tsx"), "utf8");
  assert.match(source, /\{!readOnly && \(playHref \?/u);
  assert.match(source, /\{!readOnly && \(downloadHref \?/u);
});
