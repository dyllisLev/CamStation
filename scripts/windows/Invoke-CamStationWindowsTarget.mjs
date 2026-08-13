#!/usr/bin/env node

import { createHash } from "node:crypto";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
export const repositoryRoot = resolve(scriptDirectory, "../..");
export const profilePath = join(repositoryRoot, "work/windows-control-targets.json");

export const targetAliases = Object.freeze(["test-pc", "monitoring-pc"]);
export const modes = Object.freeze([
  "status",
  "sync",
  "setup",
  "plan",
  "desktop-capture",
  "viewer-capture",
  "viewer-configure",
  "cleanup",
  "system",
  "artifact-pull",
  "artifact-push",
]);

const artifactNamePattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.(?:msi|json|sha256)$/u;
const sha256Pattern = /^[a-f0-9]{64}$/u;

const canonicalScripts = Object.freeze([
  "Install-CamStationWindowsControl.ps1",
  "Invoke-CamStationWindowsControl.ps1",
  "Invoke-CamStationWindowsControlWorker.ps1",
  "Invoke-CamStationViewerGuiCapture.ps1",
  "Capture-CamStationViewerWindow.ps1",
  "Invoke-CamStationViewerConfigure.ps1",
  "Invoke-CamStationViewerConsoleLaunch.ps1",
]);

const controlLauncherName = "Invoke-CamStationWindowsControl.ps1";
const controlWorkerName = "Invoke-CamStationWindowsControlWorker.ps1";
const setupName = "Install-CamStationWindowsControl.ps1";
const viewerLauncherName = "Invoke-CamStationViewerGuiCapture.ps1";
const viewerWorkerName = "Capture-CamStationViewerWindow.ps1";
const viewerConfigureLauncherName = "Invoke-CamStationViewerConfigure.ps1";
const viewerConsoleLaunchName = "Invoke-CamStationViewerConsoleLaunch.ps1";
const runIdPattern = /^[0-9]{8}T[0-9]{9}Z-[a-f0-9]{32}$/u;
const evidenceNamePattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,79}\.(?:png|json)$/u;

function fail(message) {
  throw new Error(message);
}

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function assertExactKeys(value, allowed, label) {
  for (const key of Object.keys(value)) {
    if (!allowed.has(key)) fail(`${label} contains unsupported field '${key}'`);
  }
}

function requireString(value, label, pattern = null) {
  if (typeof value !== "string" || value.length === 0) fail(`${label} must be a nonempty string`);
  if (pattern && !pattern.test(value)) fail(`${label} has an invalid value`);
  return value;
}

function sameText(left, right) {
  return String(left).localeCompare(String(right), undefined, { sensitivity: "accent" }) === 0;
}

export function validateProfileDocument(document, { checkFiles = true } = {}) {
  if (!isRecord(document)) fail("Windows target profile must be a JSON object");
  assertExactKeys(document, new Set(["schemaVersion", "targets"]), "profile document");
  if (document.schemaVersion !== 1) fail("Windows target profile schemaVersion must be 1");
  if (!isRecord(document.targets)) fail("Windows target profile targets must be an object");

  const aliases = Object.keys(document.targets).sort();
  const expectedAliases = [...targetAliases].sort();
  if (JSON.stringify(aliases) !== JSON.stringify(expectedAliases)) {
    fail(`Windows target profile must contain exactly: ${targetAliases.join(", ")}`);
  }

  const machines = new Set();
  const endpoints = new Set();
  const validated = {};
  const allowedProfileKeys = new Set([
    "host",
    "port",
    "maintenanceUser",
    "identityFile",
    "knownHostsFile",
    "expectedMachine",
    "expectedMaintenanceIdentity",
    "targetUser",
    "expectedSessionId",
    "remoteProjectRoot",
  ]);

  for (const alias of targetAliases) {
    const profile = document.targets[alias];
    if (!isRecord(profile)) fail(`${alias} profile must be an object`);
    assertExactKeys(profile, allowedProfileKeys, `${alias} profile`);

    const host = requireString(profile.host, `${alias}.host`, /^[^\s'";|&<>]+$/u);
    const port = profile.port;
    if (!Number.isInteger(port) || port < 1 || port > 65535) fail(`${alias}.port is invalid`);
    const maintenanceUser = requireString(
      profile.maintenanceUser,
      `${alias}.maintenanceUser`,
      /^[A-Za-z0-9._-]+$/u,
    );
    const identityFile = requireString(profile.identityFile, `${alias}.identityFile`);
    const knownHostsFile = requireString(profile.knownHostsFile, `${alias}.knownHostsFile`);
    if (!isAbsolute(identityFile) || !isAbsolute(knownHostsFile)) {
      fail(`${alias} key and known-host paths must be absolute`);
    }
    const expectedMachine = requireString(
      profile.expectedMachine,
      `${alias}.expectedMachine`,
      /^[A-Za-z0-9._-]+$/u,
    );
    const expectedMaintenanceIdentity = requireString(
      profile.expectedMaintenanceIdentity,
      `${alias}.expectedMaintenanceIdentity`,
      /^[^\\]+\\[^\\]+$/u,
    );
    const targetUser = requireString(profile.targetUser, `${alias}.targetUser`, /^[^\\]+\\[^\\]+$/u);
    if (!sameText(expectedMaintenanceIdentity.split("\\", 1)[0], expectedMachine)) {
      fail(`${alias} maintenance identity does not belong to expectedMachine`);
    }
    if (!sameText(targetUser.split("\\", 1)[0], expectedMachine)) {
      fail(`${alias} targetUser does not belong to expectedMachine`);
    }
    if (!sameText(expectedMaintenanceIdentity.split("\\")[1], maintenanceUser)) {
      fail(`${alias} maintenance identity does not match maintenanceUser`);
    }
    const expectedSessionId = profile.expectedSessionId;
    if (!Number.isInteger(expectedSessionId) || expectedSessionId < 1) {
      fail(`${alias}.expectedSessionId must be a positive integer`);
    }
    if (profile.remoteProjectRoot !== "C:\\CamStationDev\\src\\CamStation") {
      fail(`${alias}.remoteProjectRoot must use the canonical project path`);
    }

    const machineKey = expectedMachine.toLowerCase();
    const endpointKey = `${host.toLowerCase()}:${port}`;
    if (machines.has(machineKey)) fail("Windows target profiles must not reuse expectedMachine");
    if (endpoints.has(endpointKey)) fail("Windows target profiles must not reuse an SSH endpoint");
    machines.add(machineKey);
    endpoints.add(endpointKey);

    if (checkFiles) {
      for (const [path, label] of [
        [identityFile, `${alias} identity file`],
        [knownHostsFile, `${alias} known-hosts file`],
      ]) {
        if (!existsSync(path) || !statSync(path).isFile()) fail(`${label} is missing`);
      }
      if ((statSync(identityFile).mode & 0o077) !== 0) {
        fail(`${alias} identity file must not be accessible by group or others`);
      }
    }

    validated[alias] = Object.freeze({
      host,
      port,
      maintenanceUser,
      identityFile,
      knownHostsFile,
      expectedMachine,
      expectedMaintenanceIdentity,
      targetUser,
      expectedSessionId,
      remoteProjectRoot: profile.remoteProjectRoot,
    });
  }
  return Object.freeze(validated);
}

export function loadProfiles() {
  if (!existsSync(profilePath)) {
    fail(`Local Windows target profile is missing: ${profilePath}`);
  }
  return validateProfileDocument(JSON.parse(readFileSync(profilePath, "utf8")));
}

export function parseArguments(argv) {
  const valueFlags = new Map([
    ["--target", "target"],
    ["--mode", "mode"],
    ["--plan", "plan"],
    ["--run-id", "runId"],
    ["--viewer-operation", "viewerOperation"],
    ["--archive", "archive"],
    ["--script", "script"],
    ["--intent", "intent"],
    ["--configuration", "configuration"],
    ["--artifact-name", "artifactName"],
    ["--local-file", "localFile"],
    ["--expected-sha256", "expectedSha256"],
  ]);
  const booleanFlags = new Map([
    ["--full-audit", "fullAudit"],
    ["--help", "help"],
  ]);
  const options = { fullAudit: false, help: false };
  const seen = new Set();

  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (booleanFlags.has(token)) {
      if (seen.has(token)) fail(`Duplicate option: ${token}`);
      seen.add(token);
      options[booleanFlags.get(token)] = true;
      continue;
    }
    if (!valueFlags.has(token)) fail(`Unknown option: ${token}`);
    if (seen.has(token)) fail(`Duplicate option: ${token}`);
    seen.add(token);
    const value = argv[index + 1];
    if (!value || value.startsWith("--")) fail(`Missing value for ${token}`);
    options[valueFlags.get(token)] = value;
    index += 1;
  }

  if (options.help) return options;
  if (!targetAliases.includes(options.target)) fail(`--target must be one of: ${targetAliases.join(", ")}`);
  if (!modes.includes(options.mode)) fail(`--mode must be one of: ${modes.join(", ")}`);
  if (options.mode === "plan" && !options.plan) fail("plan mode requires --plan");
  if (options.mode === "cleanup" && (!options.runId || !runIdPattern.test(options.runId))) {
    fail("cleanup mode requires an exact --run-id");
  }
  if (options.mode === "viewer-capture") {
    options.viewerOperation ??= "Capture";
    if (!["Capture", "LaunchAndCapture"].includes(options.viewerOperation)) {
      fail("--viewer-operation must be Capture or LaunchAndCapture");
    }
  }
  if (options.mode === "viewer-configure" && !options.configuration) {
    fail("viewer-configure mode requires --configuration");
  }
  if (options.mode === "system") {
    if (!options.script) fail("system mode requires --script");
    if (!["read-only", "change"].includes(options.intent)) {
      fail("system mode requires --intent read-only or --intent change");
    }
  }
  if (["artifact-pull", "artifact-push"].includes(options.mode)) {
    if (!options.artifactName || !artifactNamePattern.test(options.artifactName)) {
      fail(`${options.mode} requires a safe --artifact-name`);
    }
    if (!options.expectedSha256 || !sha256Pattern.test(options.expectedSha256)) {
      fail(`${options.mode} requires lowercase --expected-sha256`);
    }
    if (options.mode === "artifact-push" && !options.localFile) {
      fail("artifact-push requires --local-file");
    }
  }
  if (options.archive && options.mode !== "setup") fail("--archive is valid only for setup mode");
  if (options.plan && options.mode !== "plan") fail("--plan is valid only for plan mode");
  if (options.runId && options.mode !== "cleanup") fail("--run-id is valid only for cleanup mode");
  if (options.viewerOperation && options.mode !== "viewer-capture") {
    fail("--viewer-operation is valid only for viewer-capture mode");
  }
  if (options.script && options.mode !== "system") fail("--script is valid only for system mode");
  if (options.intent && options.mode !== "system") fail("--intent is valid only for system mode");
  if (options.configuration && options.mode !== "viewer-configure") {
    fail("--configuration is valid only for viewer-configure mode");
  }
  if (options.artifactName && !["artifact-pull", "artifact-push"].includes(options.mode)) {
    fail("--artifact-name is valid only for artifact transfer modes");
  }
  if (options.localFile && options.mode !== "artifact-push") {
    fail("--local-file is valid only for artifact-push");
  }
  if (options.expectedSha256 && !["artifact-pull", "artifact-push"].includes(options.mode)) {
    fail("--expected-sha256 is valid only for artifact transfer modes");
  }
  if (options.fullAudit && options.mode !== "status") fail("--full-audit is valid only for status mode");
  return options;
}

function sha256Bytes(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function sha256File(path) {
  return sha256Bytes(readFileSync(path));
}

function psLiteral(value) {
  return `'${String(value).replaceAll("'", "''")}'`;
}

function windowsJoin(root, ...parts) {
  return [root.replace(/\\+$/u, ""), ...parts.map((part) => part.replace(/^\\+|\\+$/gu, ""))].join("\\");
}

function remoteScpPath(path) {
  const match = /^([A-Za-z]):\\(.*)$/u.exec(path);
  if (!match) fail("Remote copy path is not an absolute Windows drive path");
  return `/${match[1]}:/${match[2].replaceAll("\\", "/")}`;
}

function transportOptions(profile, { scp = false } = {}) {
  return [
    "-q",
    "-o", "IdentitiesOnly=yes",
    "-o", "BatchMode=yes",
    "-o", "PreferredAuthentications=publickey",
    "-o", "PasswordAuthentication=no",
    "-o", "KbdInteractiveAuthentication=no",
    "-o", "StrictHostKeyChecking=yes",
    "-o", `UserKnownHostsFile=${profile.knownHostsFile}`,
    "-o", "ConnectTimeout=8",
    "-o", "ServerAliveInterval=5",
    "-o", "ServerAliveCountMax=2",
    scp ? "-P" : "-p", String(profile.port),
    "-i", profile.identityFile,
  ];
}

function runProcess(command, args, { input, timeout = 120_000 } = {}) {
  const result = spawnSync(command, args, {
    encoding: "utf8",
    input,
    timeout,
    maxBuffer: 16 * 1024 * 1024,
    windowsHide: true,
  });
  if (result.error) fail(`${command} failed: ${result.error.message}`);
  if (result.status !== 0) {
    const detail = `${result.stderr ?? ""}\n${result.stdout ?? ""}`.trim().slice(-6000);
    fail(`${command} exited ${result.status}${detail ? `: ${detail}` : ""}`);
  }
  return { stdout: result.stdout ?? "", stderr: result.stderr ?? "" };
}

function sshTarget(profile) {
  return `${profile.maintenanceUser}@${profile.host}`;
}

const powerShellStdinBootstrap =
  "$source = [Console]::In.ReadToEnd(); & ([ScriptBlock]::Create($source))";

function invokePowerShell(profile, source, { timeout } = {}) {
  const encodedBootstrap = Buffer.from(powerShellStdinBootstrap, "utf16le").toString("base64");
  return runProcess(
    "ssh",
    [
      ...transportOptions(profile),
      sshTarget(profile),
      "powershell.exe",
      "-NoLogo",
      "-NoProfile",
      "-NonInteractive",
      "-ExecutionPolicy",
      "Bypass",
      "-EncodedCommand",
      encodedBootstrap,
    ],
    { input: source, timeout },
  ).stdout;
}

function copyToRemote(profile, localPath, remotePath, { timeout = 120_000 } = {}) {
  runProcess(
    "scp",
    [
      ...transportOptions(profile, { scp: true }),
      localPath,
      `${sshTarget(profile)}:${remoteScpPath(remotePath)}`,
    ],
    { timeout },
  );
}

function copyFromRemote(profile, remotePath, localPath, { timeout = 120_000 } = {}) {
  runProcess(
    "scp",
    [
      ...transportOptions(profile, { scp: true }),
      `${sshTarget(profile)}:${remoteScpPath(remotePath)}`,
      localPath,
    ],
    { timeout },
  );
}

function parseLastJson(output, label) {
  const lines = output.split(/\r?\n/u).map((line) => line.trim()).filter(Boolean);
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    if (!lines[index].startsWith("{") || !lines[index].endsWith("}")) continue;
    try {
      return JSON.parse(lines[index]);
    } catch {}
  }
  fail(`${label} did not return a final JSON object`);
}

function localScriptHashes() {
  return Object.fromEntries(canonicalScripts.map((name) => {
    const path = join(scriptDirectory, name);
    if (!existsSync(path)) fail(`Canonical Windows script is missing: ${name}`);
    return [name, sha256File(path)];
  }));
}

function preflightSource(profile) {
  const scriptEntries = canonicalScripts.map((name) => {
    const path = windowsJoin(profile.remoteProjectRoot, "scripts", "windows", name);
    return `  ${psLiteral(name)} = ${psLiteral(path)}`;
  }).join("\n");
  return `
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$scriptPaths = [ordered]@{
${scriptEntries}
}
$scriptHashes = [ordered]@{}
foreach ($entry in $scriptPaths.GetEnumerator()) {
  $scriptHashes[$entry.Key] = if (Test-Path -LiteralPath $entry.Value -PathType Leaf) {
    (Get-FileHash -LiteralPath $entry.Value -Algorithm SHA256).Hash.ToLowerInvariant()
  } else { $null }
}
$targetUser = ${psLiteral(profile.targetUser)}
$explorers = @(Get-Process -Name explorer -IncludeUserName -ErrorAction SilentlyContinue |
  Where-Object { [string]::Equals($_.UserName, $targetUser, [StringComparison]::OrdinalIgnoreCase) } |
  ForEach-Object { [ordered]@{ ProcessId = [int]$_.Id; SessionId = [int]$_.SessionId; UserName = [string]$_.UserName } })
$taskNames = @()
$scheduler = $null
$folder = $null
try {
  $scheduler = New-Object -ComObject "Schedule.Service"
  $scheduler.Connect()
  $folder = $scheduler.GetFolder("\\")
  $taskNames = @($folder.GetTasks(1) | ForEach-Object { [string]$_.Name })
} finally {
  if ($null -ne $folder -and [Runtime.InteropServices.Marshal]::IsComObject($folder)) {
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($folder)
  }
  if ($null -ne $scheduler -and [Runtime.InteropServices.Marshal]::IsComObject($scheduler)) {
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($scheduler)
  }
}
function Get-ServiceState([string]$Name) {
  $service = Get-Service -Name $Name -ErrorAction SilentlyContinue
  if ($null -eq $service) { return "Missing" }
  return [string]$service.Status
}
[ordered]@{
  SchemaVersion = 1
  Result = "CAMSTATION_WINDOWS_TARGET_PREFLIGHT"
  Machine = [string]$env:COMPUTERNAME
  MaintenanceIdentity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
  TargetExplorers = $explorers
  Scheduler = Get-ServiceState "Schedule"
  ViewerService = Get-ServiceState "CamStationViewerService"
  AnyDeskService = Get-ServiceState "AnyDesk"
  ControlTaskCount = @($taskNames | Where-Object { $_ -like "CamStation-WindowsControl-*" }).Count
  SetupTaskCount = @($taskNames | Where-Object { $_ -like "CamStation-WindowsControlSetup-*" }).Count
  ViewerCaptureTaskCount = @($taskNames | Where-Object { $_ -like "CamStation-GuiCapture-*" }).Count
  ViewerConfigureTaskCount = @($taskNames | Where-Object { $_ -like "CamStation-ViewerConfigure-*" }).Count
  ScriptHashes = $scriptHashes
} | ConvertTo-Json -Depth 8 -Compress
`;
}

function assertPreflight(profile, preflight) {
  if (preflight?.Result !== "CAMSTATION_WINDOWS_TARGET_PREFLIGHT") fail("Unexpected target preflight result");
  if (!sameText(preflight.Machine, profile.expectedMachine)) fail("Remote machine does not match selected target alias");
  if (!sameText(preflight.MaintenanceIdentity, profile.expectedMaintenanceIdentity)) {
    fail("Remote maintenance identity does not match selected target alias");
  }
  const explorers = Array.isArray(preflight.TargetExplorers)
    ? preflight.TargetExplorers
    : preflight.TargetExplorers ? [preflight.TargetExplorers] : [];
  if (explorers.length !== 1) fail("Target user must own exactly one Explorer process");
  if (!sameText(explorers[0].UserName, profile.targetUser)) fail("Explorer belongs to the wrong target user");
  if (explorers[0].SessionId !== profile.expectedSessionId || explorers[0].SessionId === 0) {
    fail("Interactive session does not match the selected target profile");
  }
  if (preflight.Scheduler !== "Running") fail("Windows Task Scheduler is not running");
  return preflight;
}

function requireActiveDesktop(status) {
  if (status.TargetSessionState !== "Active") {
    fail(`Interactive desktop is ${status.TargetSessionState ?? "unknown"}; reconnect its RDP or VM console before GUI control or capture`);
  }
}

function requireNoTaskResidue(preflight) {
  const counts = [
    preflight.ControlTaskCount,
    preflight.SetupTaskCount,
    preflight.ViewerCaptureTaskCount,
    preflight.ViewerConfigureTaskCount,
  ];
  if (counts.some((count) => count !== 0)) {
    fail("A CamStation one-shot task already exists on the selected target");
  }
}

function getPreflight(profile) {
  return assertPreflight(
    profile,
    parseLastJson(invokePowerShell(profile, preflightSource(profile), { timeout: 60_000 }), "target preflight"),
  );
}

function parityReport(preflight) {
  const expected = localScriptHashes();
  return Object.fromEntries(canonicalScripts.map((name) => [
    name,
    {
      matches: preflight.ScriptHashes?.[name] === expected[name],
      localSha256: expected[name],
      remoteSha256: preflight.ScriptHashes?.[name] ?? null,
    },
  ]));
}

function requireParity(preflight, names) {
  const parity = parityReport(preflight);
  const stale = names.filter((name) => !parity[name]?.matches);
  if (stale.length > 0) {
    fail(`Remote canonical scripts are stale or missing (${stale.join(", ")}); run --mode sync first`);
  }
  return parity;
}

function launcherPath(profile) {
  return windowsJoin(profile.remoteProjectRoot, "scripts", "windows", controlLauncherName);
}

function invokeLauncher(profile, argumentsSource, { timeout = 240_000 } = {}) {
  const source = `
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
& ${psLiteral(launcherPath(profile))} -TargetUser ${psLiteral(profile.targetUser)} ${argumentsSource}
if ($LASTEXITCODE -ne 0) { throw "Canonical Windows control launcher failed" }
`;
  return parseLastJson(invokePowerShell(profile, source, { timeout }), "Windows control launcher");
}

function validateStatus(profile, status) {
  if (status?.Result !== "WINDOWS_CONTROL_STATUS") fail("Unexpected Windows control status result");
  if (!sameText(status.TargetUser, profile.targetUser)) fail("Windows control status returned the wrong user");
  if (status.TargetSessionId !== profile.expectedSessionId) fail("Windows control status returned the wrong session");
  if (typeof status.TargetSessionState !== "string") fail("Windows control status omitted the Terminal Services state");
  if (status.Scheduler !== "Running") fail("Windows control status scheduler is not running");
  if (status.ExistingControlTasks !== 0) fail("A Windows control task is already present");
  if (status.Driver?.Exists !== true || status.Driver?.MatchingProcessCount !== 1 ||
      status.Driver?.SessionProcessCount !== 1 || status.Driver?.TelemetryDisabled !== true) {
    fail("Pinned interactive Cua driver is not ready in the selected session");
  }
  if (status.DriverTcpConnectionCount !== 0 || status.CuaFirewallRuleCount !== 0) {
    fail("Cua driver network or firewall boundary changed");
  }
  return status;
}

function getStatus(profile, { fullAudit = false } = {}) {
  const preflight = getPreflight(profile);
  const parity = requireParity(preflight, [controlLauncherName]);
  const status = validateStatus(
    profile,
    invokeLauncher(profile, `-Mode Status${fullAudit ? " -FullAudit" : ""}`, { timeout: 90_000 }),
  );
  return { preflight, parity, status };
}

function statusSummary(alias, result) {
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_STATUS",
    Target: alias,
    Machine: result.preflight.Machine,
    MaintenanceIdentity: result.preflight.MaintenanceIdentity,
    TargetUser: result.status.TargetUser,
    TargetSessionId: result.status.TargetSessionId,
    TargetSessionState: result.status.TargetSessionState,
    ViewerService: result.status.ViewerService,
    Driver: {
      Version: result.status.Driver.Version,
      ProcessId: result.status.Driver.Processes?.[0]?.ProcessId ?? null,
      TelemetryDisabled: result.status.Driver.TelemetryDisabled,
      TcpConnectionCount: result.status.DriverTcpConnectionCount,
      FirewallRuleCount: result.status.CuaFirewallRuleCount,
    },
    Tasks: {
      Control: result.preflight.ControlTaskCount,
      Setup: result.preflight.SetupTaskCount,
      ViewerCapture: result.preflight.ViewerCaptureTaskCount,
      ViewerConfigure: result.preflight.ViewerConfigureTaskCount,
    },
    ScriptParity: Object.fromEntries(Object.entries(result.parity).map(([name, value]) => [name, value.matches])),
    ElapsedMs: result.status.ElapsedMs,
  };
}

function synchronizeScripts(alias, profile) {
  const initialPreflight = getPreflight(profile);
  requireNoTaskResidue(initialPreflight);
  const expected = localScriptHashes();
  const syncId = createHash("sha256").update(`${Date.now()}-${Math.random()}`).digest("hex").slice(0, 24);
  const stagingDirectory = `C:\\CamStationDev\\windows-control-sync-${syncId}`;
  invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
if (Test-Path -LiteralPath ${psLiteral(stagingDirectory)}) { throw "Sync staging path already exists" }
New-Item -ItemType Directory -Path ${psLiteral(stagingDirectory)} | Out-Null
`, { timeout: 30_000 });

  try {
    for (const name of canonicalScripts) {
      copyToRemote(profile, join(scriptDirectory, name), windowsJoin(stagingDirectory, name));
    }
    const expectedEntries = canonicalScripts.map((name) => `  ${psLiteral(name)} = ${psLiteral(expected[name])}`).join("\n");
    const result = parseLastJson(invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
$staging = ${psLiteral(stagingDirectory)}
$destination = ${psLiteral(windowsJoin(profile.remoteProjectRoot, "scripts", "windows"))}
$expected = @{
${expectedEntries}
}
try {
  if (-not (Test-Path -LiteralPath $destination -PathType Container)) { throw "Canonical script directory is missing" }
  foreach ($name in $expected.Keys) {
    $source = Join-Path $staging $name
    $tokens = $null
    $errors = $null
    [void][System.Management.Automation.Language.Parser]::ParseFile($source, [ref]$tokens, [ref]$errors)
    if (@($errors).Count -ne 0) { throw "PowerShell parser rejected $name" }
    $actual = (Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected[$name]) { throw "Staged script hash mismatch for $name" }
  }
  foreach ($name in $expected.Keys) {
    Move-Item -LiteralPath (Join-Path $staging $name) -Destination (Join-Path $destination $name) -Force
  }
  $final = [ordered]@{}
  foreach ($name in $expected.Keys) {
    $final[$name] = (Get-FileHash -LiteralPath (Join-Path $destination $name) -Algorithm SHA256).Hash.ToLowerInvariant()
  }
  [ordered]@{ SchemaVersion = 1; Result = "CAMSTATION_WINDOWS_SCRIPT_SYNC_COMPLETE"; Hashes = $final } |
    ConvertTo-Json -Depth 5 -Compress
} finally {
  if (Test-Path -LiteralPath $staging) { Remove-Item -LiteralPath $staging -Recurse -Force }
}
`, { timeout: 90_000 }), "Windows script synchronization");
    if (result.Result !== "CAMSTATION_WINDOWS_SCRIPT_SYNC_COMPLETE") fail("Unexpected script sync result");
  } finally {
    try {
      invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(stagingDirectory)}) { Remove-Item -LiteralPath ${psLiteral(stagingDirectory)} -Recurse -Force }`, { timeout: 30_000 });
    } catch {}
  }

  const status = getStatus(profile);
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_SYNC_COMPLETE",
    Target: alias,
    Machine: status.preflight.Machine,
    ScriptParity: Object.fromEntries(Object.entries(status.parity).map(([name, value]) => [name, value.matches])),
    Status: statusSummary(alias, status),
  };
}

function validatePlanFile(planPath) {
  const resolvedPath = realpathSync(resolve(planPath));
  if (!statSync(resolvedPath).isFile()) fail("Control plan is not a regular file");
  const bytes = readFileSync(resolvedPath);
  if (bytes.length < 2 || bytes.length > 1024 * 1024) fail("Control plan size is outside the allowed range");
  const plan = JSON.parse(bytes.toString("utf8"));
  if (!isRecord(plan) || plan.schemaVersion !== 1 || !Array.isArray(plan.steps) ||
      plan.steps.length < 1 || plan.steps.length > 32) {
    fail("Control plan must use schemaVersion 1 with 1 to 32 steps");
  }
  return { path: resolvedPath, bytes, sha256: sha256Bytes(bytes) };
}

export function validateViewerConfigurationFile(configurationPath) {
  const resolvedPath = realpathSync(resolve(configurationPath));
  if (!statSync(resolvedPath).isFile()) fail("Viewer configuration is not a regular file");
  const bytes = readFileSync(resolvedPath);
  if (bytes.length < 2 || bytes.length > 64 * 1024) {
    fail("Viewer configuration size is outside the allowed range");
  }
  const configuration = JSON.parse(bytes.toString("utf8"));
  if (!isRecord(configuration)) fail("Viewer configuration must be a JSON object");
  const fields = Object.keys(configuration).sort();
  const expected = ["autoStart", "displayName", "schemaVersion", "serverUrl"].sort();
  if (JSON.stringify(fields) !== JSON.stringify(expected)) {
    fail("Viewer configuration must contain exactly schemaVersion, serverUrl, displayName, and autoStart");
  }
  if (configuration.schemaVersion !== 1 || typeof configuration.serverUrl !== "string" ||
      typeof configuration.displayName !== "string" || typeof configuration.autoStart !== "boolean") {
    fail("Viewer configuration fields have invalid types");
  }
  if (configuration.serverUrl.trim() !== configuration.serverUrl || /[\u0000-\u001f\u007f]/u.test(configuration.serverUrl)) {
    fail("Viewer configuration URL is invalid");
  }
  let server;
  try {
    server = new URL(configuration.serverUrl);
  } catch {
    fail("Viewer configuration URL must be an absolute HTTP or HTTPS origin");
  }
  if (!server || !["http:", "https:"].includes(server.protocol) || !server.hostname) {
    fail("Viewer configuration URL must be an absolute HTTP or HTTPS origin");
  }
  if (server.username || server.password || (server.pathname !== "/" && server.pathname !== "") ||
      server.search || server.hash) {
    fail("Viewer configuration URL must not contain credentials, path, query, or fragment");
  }
  if (configuration.displayName.trim() !== configuration.displayName ||
      configuration.displayName.length < 1 || configuration.displayName.length > 128 ||
      /[\u0000-\u001f\u007f]/u.test(configuration.displayName)) {
    fail("Viewer display name is invalid");
  }
  return Object.freeze({
    path: resolvedPath,
    bytes,
    sha256: sha256Bytes(bytes),
    autoStart: configuration.autoStart,
  });
}

function evidenceDirectory(alias, runId, kind = "control") {
  const directory = join(repositoryRoot, "work", "windows-control-evidence", alias, `${kind}-${runId}`);
  if (existsSync(directory)) fail(`Local evidence directory already exists: ${directory}`);
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  chmodSync(directory, 0o700);
  return directory;
}

function verifyDownloadedHash(path, expected, label) {
  const actual = sha256File(path);
  if (actual !== expected) fail(`${label} SHA-256 does not match the remote completion record`);
  return actual;
}

function fetchControlEvidence(alias, profile, launcherResult, expectedPlanSha256) {
  if (!runIdPattern.test(launcherResult.RunId ?? "")) fail("Control result returned an invalid RunId");
  const expectedDirectory = `C:\\CamStationDev\\windows-control-runs\\${launcherResult.RunId}`;
  if (!sameText(launcherResult.ResultDirectory, expectedDirectory)) fail("Control result directory escaped the canonical root");
  if (launcherResult.Result !== "WINDOWS_CONTROL_PLAN_COMPLETE" || launcherResult.TaskDeleted !== true ||
      launcherResult.Worker?.Success !== true) {
    fail("Windows control plan did not complete with a deleted one-shot task");
  }
  if (!sameText(launcherResult.TargetUser, profile.targetUser) ||
      launcherResult.TargetSessionId !== profile.expectedSessionId ||
      launcherResult.PlanSha256 !== expectedPlanSha256 ||
      launcherResult.Worker.PlanSha256 !== expectedPlanSha256) {
    fail("Windows control result identity or plan hash does not match the request");
  }

  const localDirectory = evidenceDirectory(alias, launcherResult.RunId);
  const completionPath = join(localDirectory, "complete.json");
  copyFromRemote(profile, windowsJoin(expectedDirectory, "complete.json"), completionPath);
  const completion = JSON.parse(readFileSync(completionPath, "utf8"));
  if (completion.Success !== true || completion.PlanSha256 !== expectedPlanSha256 ||
      completion.SessionId !== profile.expectedSessionId) {
    fail("Downloaded control completion record does not match the request");
  }

  const artifacts = [];
  const seen = new Set();
  for (const record of completion.Artifacts ?? []) {
    if (!evidenceNamePattern.test(record.Name ?? "") || !record.Name.endsWith(".png") || seen.has(record.Name)) {
      fail("Control completion record contains an unsafe or duplicate artifact name");
    }
    seen.add(record.Name);
    const localPath = join(localDirectory, record.Name);
    copyFromRemote(profile, windowsJoin(expectedDirectory, record.Name), localPath);
    verifyDownloadedHash(localPath, record.Sha256, record.Name);
    const step = (completion.Steps ?? []).find((candidate) => candidate.Id === record.StepId);
    artifacts.push({
      Name: record.Name,
      Path: localPath,
      Bytes: statSync(localPath).size,
      Sha256: record.Sha256,
      CaptureMode: step?.OutputSummary?.capture_mode ?? "driver",
    });
  }
  return { localDirectory, completionPath, completion, artifacts };
}

function invokeControlPlan(alias, profile, planPath) {
  const readiness = getStatus(profile);
  const preflight = readiness.preflight;
  requireActiveDesktop(readiness.status);
  requireNoTaskResidue(preflight);
  requireParity(preflight, [controlLauncherName, controlWorkerName]);
  if (preflight.ControlTaskCount !== 0) fail("Another Windows control task already exists");
  const plan = validatePlanFile(planPath);
  const requestId = createHash("sha256").update(`${Date.now()}-${Math.random()}`).digest("hex").slice(0, 24);
  const requestDirectory = "C:\\CamStationDev\\windows-control-requests";
  const remotePlan = windowsJoin(requestDirectory, `request-${requestId}.json`);
  invokePowerShell(profile, `New-Item -ItemType Directory -Path ${psLiteral(requestDirectory)} -Force | Out-Null; if (Test-Path -LiteralPath ${psLiteral(remotePlan)}) { throw "Request path already exists" }`, { timeout: 30_000 });
  copyToRemote(profile, plan.path, remotePlan);

  let launcherResult;
  try {
    launcherResult = parseLastJson(invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$planPath = ${psLiteral(remotePlan)}
try {
  $actual = (Get-FileHash -LiteralPath $planPath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne ${psLiteral(plan.sha256)}) { throw "Transferred control plan hash mismatch" }
  & ${psLiteral(launcherPath(profile))} -TargetUser ${psLiteral(profile.targetUser)} -Mode Plan -PlanPath $planPath
  if ($LASTEXITCODE -ne 0) { throw "Canonical Windows control launcher failed" }
} finally {
  if (Test-Path -LiteralPath $planPath) { Remove-Item -LiteralPath $planPath -Force }
}
`, { timeout: 300_000 }), "Windows control plan");
  } finally {
    try {
      invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(remotePlan)}) { Remove-Item -LiteralPath ${psLiteral(remotePlan)} -Force }`, { timeout: 30_000 });
    } catch {}
  }

  const evidence = fetchControlEvidence(alias, profile, launcherResult, plan.sha256);
  const cleanup = invokeLauncher(profile, `-Mode Cleanup -RunId ${psLiteral(launcherResult.RunId)}`, { timeout: 60_000 });
  if (cleanup.Result !== "WINDOWS_CONTROL_CLEANUP_COMPLETE" || cleanup.Remaining !== false ||
      cleanup.ExistingControlTasks !== 0) {
    fail("Exact Windows control run cleanup did not complete");
  }
  const post = getStatus(profile);
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_PLAN_COMPLETE",
    Target: alias,
    Machine: post.preflight.Machine,
    TargetUser: post.status.TargetUser,
    TargetSessionId: post.status.TargetSessionId,
    RunId: launcherResult.RunId,
    ElapsedMs: launcherResult.ElapsedMs,
    TaskDeleted: launcherResult.TaskDeleted,
    RequiresVisualVerification: launcherResult.Worker.RequiresVisualVerification,
    Assertions: launcherResult.Worker.Assertions,
    EvidenceDirectory: evidence.localDirectory,
    CompletionPath: evidence.completionPath,
    Artifacts: evidence.artifacts,
    RemoteRunRemoved: cleanup.Remaining === false,
    PostStatus: statusSummary(alias, post),
  };
}

function captureDesktop(alias, profile) {
  const temporaryDirectory = mkdtempSync(join(tmpdir(), "camstation-desktop-plan-"));
  const planPath = join(temporaryDirectory, "desktop-capture.json");
  try {
    writeFileSync(planPath, `${JSON.stringify({
      schemaVersion: 1,
      steps: [{ id: "desktop", tool: "get_desktop_state", input: {}, screenshot: "desktop.png" }],
    }, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
    return invokeControlPlan(alias, profile, planPath);
  } finally {
    rmSync(temporaryDirectory, { recursive: true, force: true });
  }
}

function captureViewer(alias, profile, operation) {
  const readiness = getStatus(profile);
  const preflight = readiness.preflight;
  requireActiveDesktop(readiness.status);
  requireNoTaskResidue(preflight);
  requireParity(preflight, [controlLauncherName, viewerLauncherName, viewerWorkerName]);
  if (preflight.ViewerCaptureTaskCount !== 0) fail("Another Viewer capture task already exists");
  const result = invokeLauncher(
    profile,
    `-Mode ViewerCapture -ViewerOperation ${operation}`,
    { timeout: 180_000 },
  );
  if (result.Result !== "INTERACTIVE_GUI_CAPTURE_COMPLETE" || result.TaskDeleted !== true ||
      result.Worker?.Success !== true || !runIdPattern.test(result.RunId ?? "")) {
    fail("Viewer capture did not complete with a deleted one-shot task");
  }
  if (!sameText(result.TargetUser, profile.targetUser) || result.TargetSessionId !== profile.expectedSessionId ||
      result.Worker.SessionId !== profile.expectedSessionId) {
    fail("Viewer capture returned the wrong user or session");
  }
  const remoteDirectory = `C:\\CamStationDev\\gui-evidence\\${result.RunId}`;
  if (!sameText(result.ResultDirectory, remoteDirectory)) fail("Viewer evidence directory escaped the canonical root");
  const localDirectory = evidenceDirectory(alias, result.RunId, "viewer");
  const records = [
    ["viewer-window.png", result.Worker.Screenshot?.Sha256],
    ["uia.json", result.Worker.UIAutomation?.Sha256],
  ];
  const artifacts = [];
  for (const [name, hash] of records) {
    if (typeof hash !== "string" || !/^[a-f0-9]{64}$/u.test(hash)) fail(`Viewer evidence hash is missing for ${name}`);
    const localPath = join(localDirectory, name);
    copyFromRemote(profile, windowsJoin(remoteDirectory, name), localPath);
    verifyDownloadedHash(localPath, hash, name);
    artifacts.push({ Name: name, Path: localPath, Bytes: statSync(localPath).size, Sha256: hash });
  }
  const completionPath = join(localDirectory, "complete.json");
  copyFromRemote(profile, windowsJoin(remoteDirectory, "complete.json"), completionPath);
  const completion = JSON.parse(readFileSync(completionPath, "utf8"));
  if (completion.Success !== true || completion.SessionId !== profile.expectedSessionId ||
      completion.Screenshot?.Sha256 !== result.Worker.Screenshot?.Sha256 ||
      completion.UIAutomation?.Sha256 !== result.Worker.UIAutomation?.Sha256) {
    fail("Downloaded Viewer completion record does not match the selected target");
  }
  const cleanup = parseLastJson(invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$root = [IO.Path]::GetFullPath("C:\\CamStationDev\\gui-evidence").TrimEnd("\\")
$path = [IO.Path]::GetFullPath(${psLiteral(remoteDirectory)}).TrimEnd("\\")
if (-not ($path + "\\").StartsWith($root + "\\", [StringComparison]::OrdinalIgnoreCase)) { throw "Viewer cleanup escaped evidence root" }
if (Get-ScheduledTask -TaskName ${psLiteral(`CamStation-GuiCapture-${result.RunId}`)} -ErrorAction SilentlyContinue) { throw "Viewer capture task still exists" }
if (Test-Path -LiteralPath $path) { Remove-Item -LiteralPath $path -Recurse -Force }
[ordered]@{ Result = "CAMSTATION_VIEWER_EVIDENCE_CLEANUP"; Remaining = [bool](Test-Path -LiteralPath $path) } | ConvertTo-Json -Compress
`, { timeout: 60_000 }), "Viewer evidence cleanup");
  if (cleanup.Result !== "CAMSTATION_VIEWER_EVIDENCE_CLEANUP" || cleanup.Remaining !== false) {
    fail("Viewer evidence cleanup failed");
  }
  const post = getPreflight(profile);
  if (post.ViewerCaptureTaskCount !== 0) fail("Viewer capture task residue remains");
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_VIEWER_CAPTURE_COMPLETE",
    Target: alias,
    Machine: post.Machine,
    TargetUser: result.TargetUser,
    TargetSessionId: result.TargetSessionId,
    Operation: operation,
    RunId: result.RunId,
    TaskDeleted: result.TaskDeleted,
    EvidenceDirectory: localDirectory,
    CompletionPath: completionPath,
    Artifacts: artifacts,
    RemoteRunRemoved: cleanup.Remaining === false,
  };
}

function configureViewer(alias, profile, configurationPath) {
  const readiness = getStatus(profile);
  const preflight = readiness.preflight;
  requireActiveDesktop(readiness.status);
  requireNoTaskResidue(preflight);
  requireParity(preflight, [controlLauncherName, viewerConfigureLauncherName, viewerConsoleLaunchName]);
  if (preflight.ViewerService !== "Running") fail("CamStationViewerService must be running before configuration");
  const configuration = validateViewerConfigurationFile(configurationPath);
  const requestId = createHash("sha256").update(`${Date.now()}-${Math.random()}`).digest("hex").slice(0, 24);
  const requestDirectory = "C:\\CamStationDev\\viewer-configuration-requests";
  const remoteConfiguration = windowsJoin(requestDirectory, `request-${requestId}.json`);
  invokePowerShell(profile, `New-Item -ItemType Directory -Path ${psLiteral(requestDirectory)} -Force | Out-Null; if (Test-Path -LiteralPath ${psLiteral(remoteConfiguration)}) { throw "Viewer configuration request already exists" }`, { timeout: 30_000 });
  copyToRemote(profile, configuration.path, remoteConfiguration);

  let result;
  try {
    result = parseLastJson(invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$configurationPath = ${psLiteral(remoteConfiguration)}
try {
  $actual = (Get-FileHash -LiteralPath $configurationPath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actual -ne ${psLiteral(configuration.sha256)}) { throw "Transferred Viewer configuration hash mismatch" }
  & ${psLiteral(launcherPath(profile))} -TargetUser ${psLiteral(profile.targetUser)} -Mode ViewerConfigure -ViewerConfigurationPath $configurationPath
  if ($LASTEXITCODE -ne 0) { throw "Canonical Windows control launcher failed" }
} finally {
  if (Test-Path -LiteralPath $configurationPath) { Remove-Item -LiteralPath $configurationPath -Force }
}
`, { timeout: 240_000 }), "Viewer configuration");
  } finally {
    try {
      invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(remoteConfiguration)}) { Remove-Item -LiteralPath ${psLiteral(remoteConfiguration)} -Force }`, { timeout: 30_000 });
    } catch {}
  }

  if (result.Result !== "CAMSTATION_VIEWER_CONFIGURATION_COMPLETE" ||
      result.ConfigurationSha256 !== configuration.sha256 ||
      !sameText(result.TargetUser, profile.targetUser) ||
      result.TargetSessionId !== profile.expectedSessionId ||
      result.TaskDeleted !== true || result.RunDirectoryRemoved !== true ||
      result.ViewerService !== "Running" || result.AutoStart !== configuration.autoStart ||
      (result.ClientIdPreserved !== true && result.ClientIdCreated !== true)) {
    fail("Viewer configuration result did not preserve or create its private client identity");
  }
  const post = getStatus(profile);
  if (post.preflight.ViewerConfigureTaskCount !== 0) fail("Viewer configuration task residue remains");
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_VIEWER_CONFIGURE_COMPLETE",
    Target: alias,
    Machine: post.preflight.Machine,
    TargetUser: post.status.TargetUser,
    TargetSessionId: post.status.TargetSessionId,
    ConfigurationSha256: configuration.sha256,
    ServerOriginSha256: result.ServerOriginSha256,
    DisplayNameSha256: result.DisplayNameSha256,
    AutoStart: result.AutoStart,
    ClientIdPreserved: result.ClientIdPreserved,
    ClientIdCreated: result.ClientIdCreated,
    ViewerService: result.ViewerService,
    TaskDeleted: result.TaskDeleted,
    RunDirectoryRemoved: result.RunDirectoryRemoved,
    ElapsedMs: result.ElapsedMs,
    PostStatus: statusSummary(alias, post),
  };
}

function setupControl(alias, profile, archivePath) {
  const preflight = getPreflight(profile);
  requireNoTaskResidue(preflight);
  requireParity(preflight, [setupName, controlLauncherName]);
  let archive = null;
  let remoteArchive = null;
  let stagingDirectory = null;
  if (archivePath) {
    archive = realpathSync(resolve(archivePath));
    if (!statSync(archive).isFile()) fail("Driver archive is not a regular file");
    stagingDirectory = `C:\\CamStationDev\\windows-control-setup-upload-${Date.now()}`;
    remoteArchive = windowsJoin(stagingDirectory, basename(archive));
    invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(stagingDirectory)}) { throw "Setup upload path exists" }; New-Item -ItemType Directory -Path ${psLiteral(stagingDirectory)} | Out-Null`, { timeout: 30_000 });
    copyToRemote(profile, archive, remoteArchive, { timeout: 300_000 });
  }
  let result;
  try {
    const installer = windowsJoin(profile.remoteProjectRoot, "scripts", "windows", setupName);
    const archiveArgument = remoteArchive ? ` -ArchivePath ${psLiteral(remoteArchive)} -RemoveArchiveAfterInstall` : "";
    result = parseLastJson(invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
& ${psLiteral(installer)} -TargetUser ${psLiteral(profile.targetUser)}${archiveArgument}
if ($LASTEXITCODE -ne 0) { throw "Pinned Windows control setup failed" }
`, { timeout: 600_000 }), "Windows control setup");
  } finally {
    if (stagingDirectory) {
      try {
        invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(stagingDirectory)}) { Remove-Item -LiteralPath ${psLiteral(stagingDirectory)} -Recurse -Force }`, { timeout: 30_000 });
      } catch {}
    }
  }
  if (result.Result !== "WINDOWS_CONTROL_SETUP_COMPLETE" || result.TelemetryDisabled !== true ||
      result.DriverSessionId !== profile.expectedSessionId || result.TemporarySetupTaskCount !== 0 ||
      (result.Files ?? []).some((file) => file.Matches !== true)) {
    fail("Pinned Windows control setup did not pass its final audit");
  }
  const status = getStatus(profile);
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_SETUP_COMPLETE",
    Target: alias,
    Machine: status.preflight.Machine,
    InstalledNow: result.InstalledNow,
    DriverVersion: result.DriverVersion,
    DriverProcessId: result.DriverProcessId,
    DriverSessionId: result.DriverSessionId,
    TelemetryDisabled: result.TelemetryDisabled,
    TemporarySetupTaskCount: result.TemporarySetupTaskCount,
    Status: statusSummary(alias, status),
  };
}

function runSystemScript(alias, profile, scriptPath, intent) {
  const preflight = getPreflight(profile);
  const localPath = realpathSync(resolve(scriptPath));
  if (!statSync(localPath).isFile() || !localPath.toLowerCase().endsWith(".ps1")) {
    fail("System control script must be a regular .ps1 file");
  }
  const size = statSync(localPath).size;
  if (size < 1 || size > 1024 * 1024) fail("System control script size is outside the allowed range");
  const hash = sha256File(localPath);
  const runToken = createHash("sha256").update(`${Date.now()}-${Math.random()}`).digest("hex").slice(0, 24);
  const remoteDirectory = `C:\\CamStationDev\\windows-system-run-${runToken}`;
  const remoteScript = windowsJoin(remoteDirectory, "operation.ps1");
  invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(remoteDirectory)}) { throw "System run path exists" }; New-Item -ItemType Directory -Path ${psLiteral(remoteDirectory)} | Out-Null`, { timeout: 30_000 });
  copyToRemote(profile, localPath, remoteScript);
  let result;
  try {
    result = parseLastJson(invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$runDirectory = ${psLiteral(remoteDirectory)}
$scriptPath = ${psLiteral(remoteScript)}
$stdoutPath = Join-Path $runDirectory "stdout.txt"
$stderrPath = Join-Path $runDirectory "stderr.txt"
try {
  $actualHash = (Get-FileHash -LiteralPath $scriptPath -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($actualHash -ne ${psLiteral(hash)}) { throw "System script hash mismatch" }
  $tokens = $null
  $errors = $null
  [void][System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$errors)
  if (@($errors).Count -ne 0) { throw "System script has PowerShell parser errors" }
  $process = Start-Process -FilePath "powershell.exe" -ArgumentList @("-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", $scriptPath) -Wait -PassThru -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
  $stdout = if (Test-Path -LiteralPath $stdoutPath) { [IO.File]::ReadAllText($stdoutPath) } else { "" }
  $stderr = if (Test-Path -LiteralPath $stderrPath) { [IO.File]::ReadAllText($stderrPath) } else { "" }
  if ($stdout.Length -gt 65536 -or $stderr.Length -gt 65536) { throw "System script output exceeded 64 KiB" }
  [ordered]@{
    SchemaVersion = 1
    Result = "CAMSTATION_WINDOWS_SYSTEM_SCRIPT_COMPLETE"
    Machine = [string]$env:COMPUTERNAME
    MaintenanceIdentity = [Security.Principal.WindowsIdentity]::GetCurrent().Name
    Intent = ${psLiteral(intent)}
    ScriptSha256 = $actualHash
    ExitCode = [int]$process.ExitCode
    Stdout = $stdout.TrimEnd()
    Stderr = $stderr.TrimEnd()
  } | ConvertTo-Json -Depth 5 -Compress
} finally {
  if (Test-Path -LiteralPath $runDirectory) { Remove-Item -LiteralPath $runDirectory -Recurse -Force }
}
`, { timeout: 600_000 }), "Windows system control script");
  } finally {
    try {
      invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(remoteDirectory)}) { Remove-Item -LiteralPath ${psLiteral(remoteDirectory)} -Recurse -Force }`, { timeout: 30_000 });
    } catch {}
  }
  if (result.Result !== "CAMSTATION_WINDOWS_SYSTEM_SCRIPT_COMPLETE" ||
      !sameText(result.Machine, profile.expectedMachine) ||
      !sameText(result.MaintenanceIdentity, profile.expectedMaintenanceIdentity) ||
      result.ScriptSha256 !== hash || result.Intent !== intent) {
    fail("System control result did not match the selected target and script");
  }
  const post = getPreflight(profile);
  if (result.ExitCode !== 0) fail(`System control script exited ${result.ExitCode}: ${result.Stderr}`);
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_SYSTEM_COMPLETE",
    Target: alias,
    Machine: post.Machine,
    MaintenanceIdentity: post.MaintenanceIdentity,
    TargetUser: profile.targetUser,
    TargetSessionId: profile.expectedSessionId,
    Intent: intent,
    ScriptSha256: hash,
    Stdout: result.Stdout,
    Stderr: result.Stderr,
    Residue: {
      ControlTasks: post.ControlTaskCount,
      SetupTasks: post.SetupTaskCount,
      ViewerCaptureTasks: post.ViewerCaptureTaskCount,
      ViewerConfigureTasks: post.ViewerConfigureTaskCount,
    },
  };
}

function artifactDirectory(profile) {
  return windowsJoin(profile.remoteProjectRoot, "work", "windows-control-artifacts");
}

function localArtifactDirectory(alias) {
  const root = join(repositoryRoot, "work", "windows-control-artifacts", alias);
  mkdirSync(root, { recursive: true, mode: 0o700 });
  return root;
}

function inspectRemoteArtifact(profile, artifactName, expectedSha256, { createDirectory = false } = {}) {
  const directory = artifactDirectory(profile);
  const path = windowsJoin(directory, artifactName);
  const result = parseLastJson(invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$directory = ${psLiteral(directory)}
$path = ${psLiteral(path)}
${createDirectory ? 'New-Item -ItemType Directory -Path $directory -Force | Out-Null' : 'if (-not (Test-Path -LiteralPath $directory -PathType Container)) { throw "Artifact directory is missing" }'}
if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Artifact file is missing" }
$item = Get-Item -LiteralPath $path -ErrorAction Stop
if ($item.Length -lt 1 -or $item.Length -gt 536870912) { throw "Artifact size is outside the allowed range" }
$sha256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
if ($sha256 -ne ${psLiteral(expectedSha256)}) { throw "Artifact SHA-256 mismatch" }
[ordered]@{ Result = "WINDOWS_ARTIFACT_VERIFIED"; Name = ${psLiteral(artifactName)}; SizeBytes = [long]$item.Length; SHA256 = $sha256 } | ConvertTo-Json -Compress
`, { timeout: 180_000 }), "Windows artifact verification");
  if (result.Result !== "WINDOWS_ARTIFACT_VERIFIED" || result.Name !== artifactName ||
      result.SHA256 !== expectedSha256 || !Number.isSafeInteger(result.SizeBytes)) {
    fail("Windows artifact verification result did not match its contract");
  }
  return result;
}

function pullArtifact(alias, profile, artifactName, expectedSha256) {
  const preflight = getPreflight(profile);
  requireNoTaskResidue(preflight);
  const remote = inspectRemoteArtifact(profile, artifactName, expectedSha256);
  const output = join(localArtifactDirectory(alias), artifactName);
  if (existsSync(output)) fail("Local artifact output already exists");
  copyFromRemote(profile, windowsJoin(artifactDirectory(profile), artifactName), output, { timeout: 600_000 });
  if (sha256File(output) !== expectedSha256 || statSync(output).size !== remote.SizeBytes) {
    rmSync(output, { force: true });
    fail("Retrieved artifact did not match the verified remote file");
  }
  const post = getPreflight(profile);
  requireNoTaskResidue(post);
  return {
    SchemaVersion: 1, Result: "CAMSTATION_WINDOWS_ARTIFACT_PULLED", Target: alias,
    Machine: post.Machine, Name: artifactName, SizeBytes: remote.SizeBytes,
    SHA256: expectedSha256, LocalPath: output,
  };
}

function pushArtifact(alias, profile, artifactName, expectedSha256, localFile) {
  const preflight = getPreflight(profile);
  requireNoTaskResidue(preflight);
  const localPath = realpathSync(resolve(localFile));
  const allowedRoot = realpathSync(localArtifactDirectory(alias));
  if (!localPath.startsWith(`${allowedRoot}${process.platform === "win32" ? "\\" : "/"}`) ||
      !statSync(localPath).isFile() || basename(localPath) !== artifactName ||
      statSync(localPath).size < 1 || statSync(localPath).size > 536870912 ||
      sha256File(localPath) !== expectedSha256) {
    fail("Local artifact is outside the verified artifact directory or does not match its contract");
  }
  const directory = artifactDirectory(profile);
  const remotePath = windowsJoin(directory, artifactName);
  invokePowerShell(profile, `
$ErrorActionPreference = "Stop"
$directory = ${psLiteral(directory)}
$path = ${psLiteral(remotePath)}
New-Item -ItemType Directory -Path $directory -Force | Out-Null
if (Test-Path -LiteralPath $path) { throw "Remote artifact already exists" }
`, { timeout: 30_000 });
  try {
    copyToRemote(profile, localPath, remotePath, { timeout: 600_000 });
    const remote = inspectRemoteArtifact(profile, artifactName, expectedSha256, { createDirectory: true });
    if (remote.SizeBytes !== statSync(localPath).size) fail("Remote artifact size mismatch");
  } catch (error) {
    try {
      invokePowerShell(profile, `if (Test-Path -LiteralPath ${psLiteral(remotePath)}) { Remove-Item -LiteralPath ${psLiteral(remotePath)} -Force }`, { timeout: 30_000 });
    } catch {}
    throw error;
  }
  const post = getPreflight(profile);
  requireNoTaskResidue(post);
  return {
    SchemaVersion: 1, Result: "CAMSTATION_WINDOWS_ARTIFACT_PUSHED", Target: alias,
    Machine: post.Machine, Name: artifactName, SizeBytes: statSync(localPath).size,
    SHA256: expectedSha256,
  };
}

function cleanupControlRun(alias, profile, runId) {
  const preflight = getPreflight(profile);
  if (preflight.SetupTaskCount !== 0 || preflight.ViewerCaptureTaskCount !== 0 ||
      preflight.ViewerConfigureTaskCount !== 0) {
    fail("An unrelated CamStation one-shot task already exists on the selected target");
  }
  requireParity(preflight, [controlLauncherName]);
  const cleanup = invokeLauncher(profile, `-Mode Cleanup -RunId ${psLiteral(runId)}`, { timeout: 60_000 });
  if (cleanup.Result !== "WINDOWS_CONTROL_CLEANUP_COMPLETE" || cleanup.Remaining !== false ||
      cleanup.ExistingControlTasks !== 0) fail("Windows control cleanup failed");
  const post = getStatus(profile);
  return {
    SchemaVersion: 1,
    Result: "CAMSTATION_WINDOWS_TARGET_CLEANUP_COMPLETE",
    Target: alias,
    Machine: post.preflight.Machine,
    RunId: runId,
    Removed: cleanup.Removed,
    Remaining: cleanup.Remaining,
    Status: statusSummary(alias, post),
  };
}

function usage() {
  return `Usage:
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <test-pc|monitoring-pc> --mode status [--full-audit]
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode sync
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode setup [--archive <pinned.zip>]
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode plan --plan <plan.json>
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode desktop-capture
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode viewer-capture [--viewer-operation Capture|LaunchAndCapture]
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode viewer-configure --configuration <viewer-config.json>
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode cleanup --run-id <exact-run-id>
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode system --intent <read-only|change> --script <operation.ps1>
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode artifact-pull --artifact-name <file> --expected-sha256 <sha256>
  node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode artifact-push --artifact-name <file> --expected-sha256 <sha256> --local-file <path>`;
}

export function execute(options, profiles = loadProfiles()) {
  const profile = profiles[options.target];
  switch (options.mode) {
    case "status": return statusSummary(options.target, getStatus(profile, { fullAudit: options.fullAudit }));
    case "sync": return synchronizeScripts(options.target, profile);
    case "setup": return setupControl(options.target, profile, options.archive);
    case "plan": return invokeControlPlan(options.target, profile, options.plan);
    case "desktop-capture": return captureDesktop(options.target, profile);
    case "viewer-capture": return captureViewer(options.target, profile, options.viewerOperation);
    case "viewer-configure": return configureViewer(options.target, profile, options.configuration);
    case "cleanup": return cleanupControlRun(options.target, profile, options.runId);
    case "system": return runSystemScript(options.target, profile, options.script, options.intent);
    case "artifact-pull": return pullArtifact(options.target, profile, options.artifactName, options.expectedSha256);
    case "artifact-push": return pushArtifact(options.target, profile, options.artifactName, options.expectedSha256, options.localFile);
    default: fail("Unsupported mode");
  }
}

function main() {
  try {
    const options = parseArguments(process.argv.slice(2));
    if (options.help) {
      console.log(usage());
      return;
    }
    console.log(JSON.stringify(execute(options), null, 2));
  } catch (error) {
    console.error(`CamStation Windows target control failed: ${error.message}`);
    process.exitCode = 1;
  }
}

const invoked = process.argv[1] ? pathToFileURL(resolve(process.argv[1])).href : "";
if (import.meta.url === invoked) main();
