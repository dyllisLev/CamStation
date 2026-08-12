import assert from "node:assert/strict";
import test from "node:test";
import { formatReleaseSize, viewerDownloadHref } from "../src/pages/settings/viewerReleaseModel.ts";

test("accepts only the fixed viewer installer download route", () => {
  assert.equal(viewerDownloadHref("/api/viewers/app/download"), "/api/viewers/app/download");
  assert.equal(viewerDownloadHref("https://evil.example/setup.exe"), null);
  assert.equal(formatReleaseSize(1048576), "1.0 MB");
});
