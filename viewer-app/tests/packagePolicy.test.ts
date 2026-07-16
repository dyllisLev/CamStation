import assert from "node:assert/strict";
import test from "node:test";
import { ignoredPackagePath } from "../scripts/package-win.mjs";

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
