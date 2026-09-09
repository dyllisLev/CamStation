import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

test("Viewer recordings omit external playback and download actions", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "../src/pages/recordings/RecordingSegmentsPanel.tsx"), "utf8");
  const page = await readFile(path.resolve(import.meta.dirname, "../src/pages/RecordingsPage.tsx"), "utf8");
  const workspace = await readFile(path.resolve(import.meta.dirname, "../src/components/playback/RecordingBrowserWorkspace.tsx"), "utf8");
  // Playback now stays in the shared workspace; no surface opens an external player.
  assert.doesNotMatch(source, /href=\{playHref\}|target="_blank"/u);
  // The Viewer renders the playback-only workspace; downloads remain in the operator tab.
  const viewerPage = page.slice(page.indexOf("function ViewerRecordingsPage"), page.indexOf("function OperatorRecordingsPage"));
  assert.match(viewerPage, /<RecordingBrowserWorkspace/u);
  assert.doesNotMatch(viewerPage, /RecordingSegmentsWorkspace|RecordingSegmentsPanel/u);
  assert.doesNotMatch(workspace, /downloadHref|downloadUrl|<a\s|onDeleteSegment/u);
});
