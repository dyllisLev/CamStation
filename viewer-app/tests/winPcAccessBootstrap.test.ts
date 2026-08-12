import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const scriptPath = path.resolve(
  import.meta.dirname,
  "..",
  "..",
  "scripts",
  "windows",
  "Bootstrap-CamStationBuildOpsSsh.ps1",
);

test("paste bootstrap is exact-target, dedicated-key, and source restricted", async () => {
  const script = await readFile(scriptPath, "utf8");

  assert.match(script, /\$target = '10\.0\.0\.30'/u);
  assert.match(script, /\$source = '10\.0\.0\.16'/u);
  assert.match(script, /\$user = 'CamStationBuildOps'/u);
  assert.match(script, /administrators_authorized_keys/u);
  assert.match(script, /-LocalAddress \$target -RemoteAddress \$source/u);
  assert.doesNotMatch(script, /PRIVATE KEY|10\.0\.0\.0\/24|0\.0\.0\.0\/0/iu);
});

test("paste bootstrap stops on account/key conflicts and returns the host key", async () => {
  const script = await readFile(scriptPath, "utf8");

  assert.match(script, /OpenSSH\.Server~~~~0\.0\.1\.0/u);
  assert.match(script, /Add-WindowsCapability/u);
  assert.match(script, /S-1-5-32-544/u);
  assert.match(script, /S-1-5-18/u);
  assert.match(script, /already exists; stop and return this message/u);
  assert.match(script, /An administrator SSH key already exists/u);
  assert.match(script, /Disable-NetFirewallRule/u);
  assert.match(script, /Remove-NetFirewallRule/u);
  assert.match(script, /SSH_BOOTSTRAP_READY/u);
  assert.match(script, /HostKeyFingerprint/u);
  assert.doesNotMatch(script, /sshd_config|PasswordAuthentication|AllowUsers/u);
});
