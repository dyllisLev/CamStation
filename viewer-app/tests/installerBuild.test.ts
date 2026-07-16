import assert from "node:assert/strict";
import test from "node:test";

import { parseInstallerOptions } from "../scripts/build-installer.mjs";

test("installer build requires and preserves the provided LAN server URL", () => {
  const options = parseInstallerOptions([
    "--server-url",
    "http://192.168.10.20:18080",
    "--version",
    "2.0.0-dev.1",
  ]);
  assert.equal(options.serverUrl, "http://192.168.10.20:18080");
  assert.equal(options.version, "2.0.0-dev.1");
  assert.equal(options.allowDevelopmentUnsigned, true);
});

test("installer build rejects unsafe or incomplete server-specific inputs", () => {
  for (const args of [
    ["--version", "2.0.0"],
    ["--server-url", "http://192.168.1.2:18080"],
    ["--server-url", "file:///tmp/server", "--version", "2.0.0"],
    ["--server-url", "http://user:pass@192.168.1.2", "--version", "2.0.0"],
    ["--server-url", "http://192.168.1.2/path", "--version", "2.0.0"],
    ["--server-url", "http://192.168.1.2", "--version", "../escape"],
  ]) {
    assert.throws(() => parseInstallerOptions(args));
  }
});
