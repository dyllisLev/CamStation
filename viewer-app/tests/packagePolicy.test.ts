import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { forbiddenRuntimeArtifact, ignoredPackagePath, normalizePackageEntry } from "../scripts/package-win.mjs";

test("ASAR entry paths normalize identically on Windows and Unix", () => {
  assert.equal(normalizePackageEntry(String.raw`\build\main.js`), "/build/main.js");
  assert.equal(normalizePackageEntry("build/main.js"), "/build/main.js");
  assert.equal(normalizePackageEntry("/build/main.js"), "/build/main.js");
});

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

test("Viewer build file operations are portable to native Windows npm", async () => {
  const packageJson = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8")) as {
    scripts: Record<string, string>;
  };
  assert.doesNotMatch(packageJson.scripts.build, /(?:^|&&)\s*(?:rm|mv)(?:\s|$)/);
  assert.match(packageJson.scripts.build, /node scripts\/build-files\.mjs prepare/);
  assert.match(packageJson.scripts.build, /node scripts\/build-files\.mjs finalize/);
});
