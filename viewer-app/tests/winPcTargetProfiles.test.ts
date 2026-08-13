import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  parseArguments,
  targetAliases,
  validateViewerConfigurationFile,
  validateProfileDocument,
} from "../../scripts/windows/Invoke-CamStationWindowsTarget.mjs";

const wrapperURL = new URL(
  "../../scripts/windows/Invoke-CamStationWindowsTarget.mjs",
  import.meta.url,
);
const exampleURL = new URL(
  "../../scripts/windows/windows-control-targets.example.json",
  import.meta.url,
);

test("the tracked profile schema defines exactly two non-secret target aliases", async () => {
  const document = JSON.parse(await readFile(exampleURL, "utf8"));
  const profiles = validateProfileDocument(document, { checkFiles: false });

  assert.deepEqual(Object.keys(profiles), [...targetAliases]);
  assert.equal(profiles["test-pc"].expectedMachine, "WIN11-DELL");
  assert.equal(profiles["monitoring-pc"].expectedMachine, "NUC");
  assert.equal(profiles["test-pc"].expectedSessionId, 1);
  assert.equal(profiles["monitoring-pc"].expectedSessionId, 1);

  const duplicateMachine = structuredClone(document);
  duplicateMachine.targets["monitoring-pc"].expectedMachine = "WIN11-DELL";
  duplicateMachine.targets["monitoring-pc"].expectedMaintenanceIdentity =
    "WIN11-DELL\\CamStationOps";
  duplicateMachine.targets["monitoring-pc"].targetUser = "WIN11-DELL\\dyllislev";
  assert.throws(
    () => validateProfileDocument(duplicateMachine, { checkFiles: false }),
    /must not reuse expectedMachine/u,
  );

  const missingAlias = structuredClone(document);
  delete missingAlias.targets["monitoring-pc"];
  assert.throws(
    () => validateProfileDocument(missingAlias, { checkFiles: false }),
    /must contain exactly/u,
  );
});

test("the command line has no implicit or arbitrary target path", () => {
  assert.throws(() => parseArguments(["--mode", "status"]), /--target must be one of/u);
  assert.throws(
    () => parseArguments(["--target", "some-host", "--mode", "status"]),
    /--target must be one of/u,
  );
  assert.throws(
    () => parseArguments(["--target", "test-pc", "--host", "example.invalid", "--mode", "status"]),
    /Unknown option/u,
  );
  assert.equal(
    parseArguments(["--target", "monitoring-pc", "--mode", "desktop-capture"]).target,
    "monitoring-pc",
  );
  assert.equal(
    parseArguments([
      "--target", "monitoring-pc", "--mode", "viewer-configure",
      "--configuration", "viewer-config.json",
    ]).configuration,
    "viewer-config.json",
  );
  assert.throws(
    () => parseArguments(["--target", "monitoring-pc", "--mode", "viewer-configure"]),
    /requires --configuration/u,
  );
  assert.throws(
    () => parseArguments([
      "--target", "test-pc", "--mode", "status", "--configuration", "viewer-config.json",
    ]),
    /valid only for viewer-configure/u,
  );
  assert.throws(
    () => parseArguments([
      "--target", "test-pc", "--mode", "system", "--script", "check.ps1",
    ]),
    /requires --intent/u,
  );
  assert.throws(
    () => parseArguments([
      "--target", "test-pc", "--mode", "status", "--run-id",
      "20260812T100000000Z-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    ]),
    /valid only for cleanup/u,
  );
  assert.equal(
    parseArguments([
      "--target", "test-pc", "--mode", "artifact-pull",
      "--artifact-name", "CamStationViewer-2.0.25.msi",
      "--expected-sha256", "a".repeat(64),
    ]).artifactName,
    "CamStationViewer-2.0.25.msi",
  );
  assert.throws(
    () => parseArguments([
      "--target", "test-pc", "--mode", "artifact-push",
      "--artifact-name", "../Viewer.msi", "--expected-sha256", "a".repeat(64),
    ]),
    /safe --artifact-name/u,
  );
  assert.throws(
    () => parseArguments([
      "--target", "test-pc", "--mode", "artifact-push",
      "--artifact-name", "Viewer.msi", "--expected-sha256", "a".repeat(64),
    ]),
    /requires --local-file/u,
  );
});

test("Viewer configuration accepts only a strict public four-field document", async () => {
  const directory = await mkdtemp(join(tmpdir(), "camstation-viewer-config-"));
  const path = join(directory, "viewer.json");
  try {
    const valid = {
      schemaVersion: 1,
      serverUrl: "http://monitoring-origin.example:18081",
      displayName: "monitoring-pc",
      autoStart: true,
    };
    await writeFile(path, `${JSON.stringify(valid)}\n`, { encoding: "utf8", mode: 0o600 });
    const result = validateViewerConfigurationFile(path);
    assert.equal(result.autoStart, true);
    assert.match(result.sha256, /^[a-f0-9]{64}$/u);
    assert.equal("serverUrl" in result, false);
    assert.equal("displayName" in result, false);

    for (const [label, invalid, expectedError] of [
      ["extra field", { ...valid, clientId: "must-not-be-supplied" }, /contain exactly/u],
      ["credential", { ...valid, serverUrl: "http://user:pass@monitoring-origin.example" }, /credentials/u],
      ["path", { ...valid, serverUrl: "http://monitoring-origin.example/live" }, /path/u],
      ["wrong type", { ...valid, autoStart: "true" }, /invalid types/u],
    ]) {
      await writeFile(path, `${JSON.stringify(invalid)}\n`, { encoding: "utf8", mode: 0o600 });
      assert.throws(
        () => validateViewerConfigurationFile(path),
        expectedError,
        label,
      );
    }
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("the wrapper pins SSH, proves Windows identities, hashes scripts, and cleans exact runs", async () => {
  const source = await readFile(wrapperURL, "utf8");

  for (const option of [
    "IdentitiesOnly=yes",
    "BatchMode=yes",
    "PreferredAuthentications=publickey",
    "PasswordAuthentication=no",
    "KbdInteractiveAuthentication=no",
    "StrictHostKeyChecking=yes",
    "UserKnownHostsFile=",
  ]) {
    assert.match(source, new RegExp(option.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&"), "u"));
  }
  assert.match(source, /Remote machine does not match selected target alias/u);
  assert.match(source, /Remote maintenance identity does not match selected target alias/u);
  assert.match(source, /Target user must own exactly one Explorer process/u);
  assert.match(source, /Interactive session does not match the selected target profile/u);
  assert.match(source, /Windows control status omitted the Terminal Services state/u);
  assert.match(source, /Interactive desktop is.*reconnect its RDP or VM console/isu);
  assert.match(source, /A CamStation one-shot task already exists/u);
  assert.match(source, /PowerShell parser rejected/u);
  assert.match(source, /Transferred control plan hash mismatch/u);
  assert.match(source, /SHA-256 does not match the remote completion record/u);
  assert.match(source, /RemoteRunRemoved/u);
  assert.match(source, /CAMSTATION_WINDOWS_TARGET_VIEWER_CONFIGURE_COMPLETE/u);
  assert.match(source, /Viewer configuration must contain exactly/iu);
  assert.match(source, /Viewer configuration URL must not contain credentials, path, query, or fragment/iu);
  assert.match(source, /Viewer configuration result did not preserve or create its private client identity/iu);
  assert.match(source, /--intent <read-only\|change>/u);
  assert.match(source, /powerShellStdinBootstrap.*Console\]::In\.ReadToEnd/isu);
  assert.match(source, /Buffer\.from\(powerShellStdinBootstrap, "utf16le"\)/u);
  assert.doesNotMatch(source, /Buffer\.from\(source, "utf16le"\)/u);
  assert.match(source, /\{ input: source, timeout \}/u);
  assert.doesNotMatch(source, /execSync|shell\s*:\s*true/u);
  assert.match(source, /CAMSTATION_WINDOWS_ARTIFACT_PULLED/u);
  assert.match(source, /CAMSTATION_WINDOWS_ARTIFACT_PUSHED/u);
  assert.match(source, /Artifact SHA-256 mismatch/u);
});
