import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { mkdtemp, mkdir, readFile, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { componentIdentity, generateViewerMsiFragment } from "../../scripts/generate-viewer-msi-files.mjs";

test("same relative path produces stable component identity", () => {
  const first = componentIdentity("resources/app.asar", "2.0.9");
  const second = componentIdentity("resources/app.asar", "2.0.9");
  const nextRelease = componentIdentity("resources/app.asar", "2.0.10");
  assert.deepEqual(first, second);
  assert.notEqual(first.guid, nextRelease.guid);
  assert.match(first.guid, /^\{[0-9A-F-]{36}\}$/u);
  assert.match(first.componentId, /^Cmp_[A-F0-9]+$/u);
});

test("manifest sorts files, excludes the hand-authored Viewer EXE, and rejects unsafe input", async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), "camstation-msi-manifest-"));
  t.after(async () => { await import("node:fs/promises").then(({ rm }) => rm(root, { recursive: true, force: true })); });
  await mkdir(path.join(root, "resources"));
  await writeFile(path.join(root, "z.dat"), "z");
  await writeFile(path.join(root, "CamStationViewer.exe"), "viewer");
  await writeFile(path.join(root, "resources", "app.asar"), "app");

  const fragment = await generateViewerMsiFragment(root, "2.0.9");
  assert.ok(fragment.indexOf("resources\\app.asar") < fragment.indexOf("z.dat"));
  assert.doesNotMatch(fragment, /CamStationViewer\.exe/u);
  assert.match(fragment, /Source="\$\(var\.ViewerPayloadDir\)\\resources\\app\.asar"/u);
  assert.match(fragment, /<Directory Id="Dir_[A-F0-9]+" Name="resources">/u);
  assert.match(fragment, /<DirectoryRef Id="Dir_[A-F0-9]+">\n      <Component Id="Cmp_[A-F0-9]+"[^>]*>\n        <File Id="File_[A-F0-9]+" Source="\$\(var\.ViewerPayloadDir\)\\resources\\app\.asar"/u);
  assert.throws(() => componentIdentity("../escape"));
  await symlink(path.join(root, "z.dat"), path.join(root, "linked.dat"));
  await assert.rejects(() => generateViewerMsiFragment(root, "2.0.9"), /symlink/u);
});

test("manifest rejects rejected Agent-era artifacts", async (t) => {
  const root = await mkdtemp(path.join(tmpdir(), "camstation-msi-forbidden-"));
  t.after(async () => { await import("node:fs/promises").then(({ rm }) => rm(root, { recursive: true, force: true })); });
  await writeFile(path.join(root, "CamStationViewerAgent.exe"), "agent");
  await assert.rejects(() => generateViewerMsiFragment(root, "2.0.9"), /rejected runtime artifact/u);
});

test("WiX source owns the direct Viewer service, shortcuts, and auto-start", async () => {
  const root = path.resolve(import.meta.dirname, "..", "..");
  const [packageSource, componentSource] = await Promise.all([
    readFile(path.join(root, "installer", "Package.wxs"), "utf8"),
    readFile(path.join(root, "installer", "Components.wxs"), "utf8"),
  ]);
  assert.match(packageSource, /Scope="perMachine"/u);
  assert.match(packageSource, /UpgradeCode="\{7D4769BB-89EF-4C36-B4F2-52E33BF8BE87\}"/u);
  assert.match(packageSource, /<MajorUpgrade[^>]*DowngradeErrorMessage=/u);
  assert.match(packageSource, /<MajorUpgrade[^>]*Schedule="afterInstallValidate"/u);
  const serviceInstall = componentSource.match(/<ServiceInstall[^>]*>/u)?.[0] ?? "";
  assert.match(serviceInstall, /Name="CamStationViewerService"/u);
  assert.match(serviceInstall, /Start="auto"/u);
  assert.match(serviceInstall, /Type="ownProcess"/u);
  assert.match(componentSource, /<RegistryValue Root="HKLM" Key="Software\\CamStation\\ViewerInstaller" Name="InstallMarker" Type="integer" Value="1"/u);
  assert.doesNotMatch(componentSource, /<RegistryValue Root="HKLM" Key="Software\\CamStation\\Viewer" Name="InstallMarker"/u);
  assert.match(componentSource, /<DirectoryRef Id="ViewerLogs">\s*<Component Id="ViewerLogsComponent"[^>]*>\s*<CreateFolder\s*\/>/u);
  assert.match(componentSource, /<DirectoryRef Id="ViewerUpdates">\s*<Component Id="ViewerUpdatesComponent"[^>]*>\s*<CreateFolder\s*\/>/u);
  assert.match(componentSource, /<ServiceControl[^>]*Start="install"[^>]*Stop="both"[^>]*Remove="uninstall"/u);
  assert.match(componentSource, /<util:ServiceConfig[^>]*FirstFailureActionType="restart"[^>]*SecondFailureActionType="restart"[^>]*ThirdFailureActionType="restart"[^>]*RestartServiceDelayInSeconds="60"[^>]*ResetPeriodInDays="1"/u);
  assert.match(componentSource, /<File Id="CamStationViewerExe"[^>]*>[\s\S]*<Shortcut[^>]*Directory="DesktopFolder"/u);
  assert.match(componentSource, /<File Id="CamStationViewerExe"[^>]*>[\s\S]*<Shortcut[^>]*Directory="CamStationStartMenu"/u);
  assert.doesNotMatch(componentSource, /<Shortcut[^>]*Target=/u);
  assert.match(componentSource, /Value="&quot;\[INSTALLFOLDER\]CamStationViewer\.exe&quot; --autostart"/u);
  assert.doesNotMatch(`${packageSource}\n${componentSource}`, /CamStationViewerAgent|CamStationViewerBootstrap|CamStationViewerHost|schtasks\.exe|release\.zip/u);
});

test("console launch helper configures the direct service without logging its configuration", async () => {
  const root = path.resolve(import.meta.dirname, "..", "..");
  const helperPath = path.join(root, "scripts", "windows", "Invoke-CamStationViewerConsoleLaunch.ps1");
  assert.ok(existsSync(helperPath), "the Windows console launch helper must be present");

  const source = await readFile(helperPath, "utf8");
  assert.match(source, /NamedPipeClientStream\]::new\("\.", "CamStationViewerService"/u);
  assert.match(source, /type\s*=\s*"configure"/u);
  assert.match(source, /Start-Process\s+-FilePath\s+\$viewerPath\s+-PassThru/u);
  assert.match(source, /Start-Sleep\s+-Seconds\s+3/u);
  assert.match(source, /\[string\]\$ResultPath/u);
  assert.match(source, /Set-Content\s+-LiteralPath\s+\$ResultPath/u);
  assert.match(source, /\[System\.Text\.UTF8Encoding\]::new\(\$false\)/u);
  assert.doesNotMatch(source, /Write-(Host|Output|Verbose|Information|Warning|Error)/u);
});
