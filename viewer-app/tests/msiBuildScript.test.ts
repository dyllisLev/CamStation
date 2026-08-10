import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

const root = path.resolve(import.meta.dirname, "..", "..");

async function source(relativePath: string): Promise<string> {
  return readFile(path.join(root, relativePath), "utf8");
}

test("MSI build entry point is Windows-only and fails closed without explicit unsigned policy", async () => {
  const script = await source("scripts/build-viewer-msi.ps1");

  assert.match(script, /\[ValidatePattern\([^\n]+\)\]\s*\[string\]\$Version/u);
  assert.match(script, /\[switch\]\$UnsignedDevelopment/u);
  assert.match(script, /\$env:OS\s+-ne\s+"Windows_NT"/u);
  assert.match(script, /Is64BitOperatingSystem/u);
  assert.match(script, /if\s*\(-not\s+\$UnsignedDevelopment\)/u);
  assert.match(script, /Get-Command[^\n]+node/u);
  assert.match(script, /Get-Command[^\n]+npm/u);
  assert.match(script, /Get-Command[^\n]+go/u);
  assert.match(script, /Get-Command[^\n]+dotnet/u);
  assert.doesNotMatch(script, /192\.168\.0\.13|100\.64\.23\.125|msiexec|ProgramData\\CamStation/iu);
});

test("MSI build uses the deterministic isolated pipeline", async () => {
  const script = await source("scripts/build-viewer-msi.ps1");

  assert.match(script, /&\s+\$npmCommand\.Source\s+ci/u);
  assert.match(script, /&\s+\$npmCommand\.Source\s+test/u);
  assert.match(script, /&\s+\$npmCommand\.Source\s+run\s+package:win/u);
  assert.match(script, /&\s+\$goCommand\.Source\s+test[^\n]+internal\/viewerservice/u);
  assert.match(script, /&\s+\$goCommand\.Source\s+build/u);
  assert.match(script, /camstation\/internal\/viewerservice\.InstalledVersion=\$Version/u);
  assert.match(script, /generate-viewer-msi-files\.mjs/u);
  assert.match(script, /&\s+\$dotnetCommand\.Source\s+restore[^\n]+--locked-mode/u);
  assert.match(script, /&\s+\$dotnetCommand\.Source\s+build[^\n]+--no-restore/u);
  assert.match(script, /artifacts[\\/]viewer-msi/u);
  assert.match(script, /Files\.generated\.wxs/u);
  assert.match(script, /Remove-Item\s+-LiteralPath\s+\$workspace/u);
  assert.doesNotMatch(script, /Set-Content[^\n]+installer[\\/]Files\.generated\.wxs/iu);
});

test("MSI publication inspects identity and writes secret-free hash metadata", async () => {
  const script = await source("scripts/build-viewer-msi.ps1");

  assert.match(script, /WindowsInstaller\.Installer/u);
  assert.match(script, /@\(\[string\]\$builtMsi, \[int\]0\)/u);
  assert.match(script, /ProductName/u);
  assert.match(script, /ProductVersion/u);
  assert.match(script, /UpgradeCode/u);
  assert.match(script, /\{7D4769BB-89EF-4C36-B4F2-52E33BF8BE87\}/u);
  assert.match(script, /Get-FileHash[^\n]+SHA256/u);
  assert.match(script, /build-metadata\.json/u);
  assert.match(script, /developmentUnsigned/u);
  assert.match(script, /sourceCommit/u);
  assert.match(script, /sourceDirty/u);
  assert.match(script, /toolVersions/u);
  assert.match(script, /SELECT `File` FROM `File`/u);
  assert.doesNotMatch(script, /SELECT COUNT\(\*\) FROM `File`/u);
  assert.match(script, /FinalReleaseComObject\(\$database\)/u);
  assert.match(script, /FinalReleaseComObject\(\$windowsInstaller\)/u);
  assert.doesNotMatch(script, /clientId|serverUrl|displayName/u);
});

test("installer guide keeps the monitoring PC outside the build boundary", async () => {
  const guide = await source("installer/README.md");

  assert.match(guide, /build-viewer-msi\.ps1/u);
  assert.match(guide, /-UnsignedDevelopment/u);
  assert.match(guide, /Node\.js 22/u);
  assert.match(guide, /Go 1\.25/u);
  assert.match(guide, /\.NET SDK 8/u);
  assert.match(guide, /NUC/u);
  assert.match(guide, /NUC는[^\n]+빌드 호스트가 아닙니다|빌드[^\n]+금지/u);
});
