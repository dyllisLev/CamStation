import assert from "node:assert/strict";
import test from "node:test";
import { forbiddenRuntimeArtifact, ignoredPackagePath } from "../scripts/package-win.mjs";

test("Windows package keeps runtime files and excludes source, tests, and tooling", () => {
  for (const runtimePath of ["/build/main.js", "/build/preload.cjs", "/package.json"]) {
    assert.equal(ignoredPackagePath(runtimePath), false, runtimePath);
  }
  for (const privatePath of [
    "/src/main.ts",
    "/tests/connection.test.ts",
    "/scripts/package-win.mjs",
    "/node_modules/.package-lock.json",
    "/tsconfig.json",
    "/tsconfig.preload.json",
    "/package-lock.json",
  ]) {
    assert.equal(ignoredPackagePath(privatePath), true, privatePath);
  }
});

test("Windows package rejects every rejected Agent-era runtime artifact", () => {
  for (const artifact of [
    "CamStationViewerAgent.exe",
    "CamStationViewerBootstrap.exe",
    "CamStationViewerHost.exe",
    "current.json",
    "release.zip",
    "schtasks.exe",
    "CamStationViewerRecovery",
    "--agent-generation",
    "--agent-nonce",
  ]) {
    assert.equal(forbiddenRuntimeArtifact(artifact), true, artifact);
  }
  assert.equal(forbiddenRuntimeArtifact("CamStationViewer.exe"), false);
});
