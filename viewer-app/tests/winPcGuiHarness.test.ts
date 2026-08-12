import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const launcherURL = new URL("../../scripts/windows/Invoke-CamStationViewerGuiCapture.ps1", import.meta.url);
const workerURL = new URL("../../scripts/windows/Capture-CamStationViewerWindow.ps1", import.meta.url);

test("Windows GUI capture uses a bounded one-shot interactive-token task", async () => {
  const source = await readFile(launcherURL, "utf8");

  assert.doesNotMatch(source, /\[string\]\$WorkerScript\s*=\s*\(Join-Path\s+\$PSScriptRoot/u);
  assert.match(source, /\[string\]\$WorkerScript\s*=\s*""/u);
  assert.match(source, /IsNullOrWhiteSpace\(\$WorkerScript\)[\s\S]+Join-Path\s+\$PSScriptRoot/u);
  assert.match(source, /\$TASK_LOGON_INTERACTIVE_TOKEN\s*=\s*3/u);
  assert.match(source, /\$TASK_RUNLEVEL_LUA\s*=\s*0/u);
  assert.match(source, /ExecutionTimeLimit\s*=\s*"PT2M"/u);
  assert.match(source, /TargetUser must own exactly one active nonzero Explorer session/u);
  assert.match(source, /DeleteTask\(\$taskName, 0\)/u);
  assert.match(source, /finally\s*\{/u);
  assert.match(source, /\*S-1-5-18:\(OI\)\(CI\)\(F\)/u);
  assert.match(source, /\*S-1-5-32-544:\(OI\)\(CI\)\(F\)/u);
  assert.match(source, /\*\$\{targetSid\}:\(OI\)\(CI\)\(M\)/u);
  assert.doesNotMatch(source, /New-NetFirewallRule|TcpListener|HttpListener|PasswordAuthentication|VNC|RustDesk|AnyDesk/iu);
});

test("Windows GUI worker captures only the verified Viewer window and omits field values", async () => {
  const source = await readFile(workerURL, "utf8");

  assert.match(source, /C:\\Program Files\\CamStation Viewer\\CamStationViewer\.exe/u);
  assert.match(source, /SessionId\s*-ne\s*\$sessionId/u);
  assert.match(source, /GetWindowRect/u);
  assert.match(source, /PrintWindow/u);
  assert.match(source, /CopyFromScreen\(\$rectangle\.Left, \$rectangle\.Top/u);
  assert.match(source, /UIAutomationClient/u);
  assert.match(source, /ControlType\]::Edit/u);
  assert.match(source, /Get-FileHash[^\n]+viewer-window\.png|Screenshot\s*=\s*\[ordered\]/u);
  assert.doesNotMatch(source, /SystemInformation\]::VirtualScreen|CopyFromScreen\(0,\s*0|ValuePattern|Current\.Value/iu);
  assert.doesNotMatch(source, /TcpListener|HttpListener|New-NetFirewallRule/iu);
});
