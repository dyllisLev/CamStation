import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const launcherURL = new URL(
  "../../scripts/windows/Invoke-CamStationWindowsControl.ps1",
  import.meta.url,
);
const workerURL = new URL(
  "../../scripts/windows/Invoke-CamStationWindowsControlWorker.ps1",
  import.meta.url,
);
const installerURL = new URL(
  "../../scripts/windows/Install-CamStationWindowsControl.ps1",
  import.meta.url,
);
const viewerConfigureURL = new URL(
  "../../scripts/windows/Invoke-CamStationViewerConfigure.ps1",
  import.meta.url,
);

test("standard Windows control launcher owns one bounded interactive batch", async () => {
  const source = await readFile(launcherURL, "utf8");

  assert.match(source, /ValidateSet\("Status", "Plan", "ViewerCapture", "ViewerConfigure", "Cleanup"\)/u);
  assert.match(source, /C:\\CamStationDev\\windows-control-runs/u);
  assert.match(source, /TargetUser must own exactly one active nonzero Explorer session/u);
  assert.match(source, /CamStationWindowsControlWts/u);
  assert.match(source, /WTSQuerySessionInformation/u);
  assert.match(source, /TargetSessionState\s*=\s*Get-TargetSessionState/u);
  assert.match(source, /\$TASK_LOGON_INTERACTIVE_TOKEN\s*=\s*3/u);
  assert.match(source, /\$TASK_RUNLEVEL_LUA\s*=\s*0/u);
  assert.match(source, /ExecutionTimeLimit\s*=\s*"PT3M"/u);
  assert.match(source, /Start-Sleep -Milliseconds 100/u);
  assert.match(source, /Interactive task exited before completion.*LastTaskResult.*Progress/isu);
  assert.match(source, /Interactive Windows control worker failed:.*FailureCleanup/isu);
  assert.match(source, /DeleteTask\(\$taskName, 0\)/u);
  assert.match(source, /finally\s*\{/u);
  assert.match(source, /TaskDeleted\s*=\s*-not \$taskStillExists/u);
  assert.match(source, /AutoCleanup cannot discard screenshot artifacts/u);
  assert.doesNotMatch(
    source,
    /New-NetFirewallRule|TcpListener|HttpListener|PasswordAuthentication|VNC|RustDesk|AnyDesk|EncodedCommand/iu,
  );
});

test("Viewer configuration uses one bounded interactive task and preserves private identity", async () => {
  const source = await readFile(viewerConfigureURL, "utf8");

  assert.match(source, /CamStation-ViewerConfigure-/u);
  assert.match(source, /TASK_LOGON_INTERACTIVE_TOKEN\s*=\s*3/u);
  assert.match(source, /TASK_RUNLEVEL_LUA\s*=\s*0/u);
  assert.match(source, /Invoke-CamStationViewerConsoleLaunch\.ps1/u);
  assert.match(source, /ConfigurationSha256/u);
  assert.match(source, /ClientIdPreserved/u);
  assert.match(source, /ClientIdCreated/u);
  assert.match(source, /DeleteTask\(\$taskName, 0\)/u);
  assert.match(source, /TaskDeleted\s*=\s*-not \$taskStillExists/u);
  assert.match(source, /Remove-Item -LiteralPath \$runDirectory -Recurse -Force/u);
  assert.doesNotMatch(source, /Write-(Host|Output|Verbose|Information).*serverUrl|\n\s+ClientId\s*=/iu);
  assert.doesNotMatch(source, /New-NetFirewallRule|TcpListener|HttpListener|New-LocalUser/iu);
});

test("interactive worker normalizes UTF-8 JSON, references, evidence, and verification", async () => {
  const source = await readFile(workerURL, "utf8");

  assert.match(source, /try \{ \[Console\]::InputEncoding\s*=\s*\$utf8 \} catch \{\}/u);
  assert.match(source, /try \{ \[Console\]::OutputEncoding\s*=\s*\$utf8 \} catch \{\}/u);
  assert.match(source, /RedirectStandardInput\s*=\s*\$true/u);
  assert.match(source, /StandardInput\.BaseStream\.Write\(\$inputBytes/u);
  assert.match(source, /StandardOutput\.BaseStream\.CopyToAsync\(\$stdoutBuffer\)/u);
  assert.match(source, /DecoderFallbackException/iu);
  assert.match(source, /CurrentCulture\.TextInfo\.ANSICodePage/u);
  assert.match(source, /IsHighSurrogate/iu);
  assert.match(source, /IsLowSurrogate/iu);
  assert.match(source, /Get-SafeStepOutputSummary/iu);
  assert.match(source, /DriverOutputSha256/iu);
  assert.match(source, /OutputSummary\s*=\s*Get-SafeStepOutputSummary/u);
  assert.doesNotMatch(source, /\n\s+Output\s*=\s*\$output/u);
  assert.match(source, /A `\$ref object cannot contain other fields/u);
  assert.match(source, /Reference requires an array index/iu);
  assert.match(source, /A `\$select object cannot contain other fields/u);
  assert.match(source, /function Test-ObjectHasProperty/iu);
  assert.match(
    source,
    /if \(-not \(Test-ObjectHasProperty -Value \$candidate -Name \$condition\.Name\)\)\s*\{\s*return \$false/isu,
  );
  assert.match(source, /exactly one is required/u);
  assert.match(source, /Mutating step.*requires verifyWith/u);
  assert.match(source, /must point to a later observation step/u);
  assert.match(source, /closeWindowOnFailure is a boolean option for launch_app only/u);
  assert.match(source, /CamStationWindowsControlNative/iu);
  assert.match(source, /public static extern bool PostMessage/u);
  assert.match(source, /PostMessage\(/u);
  assert.match(source, /uia-titlebar/u);
  assert.match(source, /RemainingWindowIds/iu);
  assert.match(source, /Get-ControlFailureMessage/iu);
  assert.match(source, /one or more failure cleanup windows remained/u);
  assert.match(source, /FailureCleanup\s*=\s*\$failureCleanupRecords/u);
  assert.match(source, /countEquals must be between 0 and 10000/u);
  assert.match(source, /Assertion where requires countEquals/u);
  assert.match(source, /Count assertion failed/iu);
  assert.match(source, /Assertions\s*=\s*\$assertionRecords/u);
  assert.match(source, /Get-Process -Name "cua-driver"/u);
  assert.doesNotMatch(source, /Get-CimInstance Win32_Process/iu);
  assert.match(source, /\$verifiedDriverProcessId\s*=\s*\[int\]\$driverProcesses\[0\]\.Id/u);
  assert.match(source, /DriverProcessId\s*=\s*\$verifiedDriverProcessId/u);
  assert.match(source, /effect.*unverifiable/isu);
  assert.match(source, /\$requiresVisualVerification\s*=\s*\$true/iu);
  assert.match(source, /function Write-DesktopScreenshotFallback/iu);
  assert.match(source, /GetSystemMetrics\(76\).*GetSystemMetrics\(79\)/isu);
  assert.match(source, /CopyFromScreen/iu);
  assert.match(source, /gdi-interactive-fallback/u);
  assert.match(source, /tool -ne "get_desktop_state".*artifactName.*returned invalid UTF-8 JSON/isu);
  assert.match(source, /Get-FileHash.*artifactPath/isu);
  assert.match(source, /Write-JsonAtomic -Path \$completionPath/u);
  assert.match(source, /Write-ControlProgress -Phase "step_started"/u);
  assert.match(source, /Write-ControlProgress -Phase "step_completed"/u);
  assert.match(source, /end_session/u);
  assert.doesNotMatch(source, /New-NetFirewallRule|TcpListener|HttpListener|ValuePattern|Current\.Value/iu);
});

test("pinned setup is fail-closed, idempotent, and leaves no setup task", async () => {
  const source = await readFile(installerURL, "utf8");

  assert.match(source, /ValidateSet\("0\.19\.3"\)/u);
  assert.match(source, /expectedArchiveSha256\s*=\s*"[a-f0-9]{64}"/u);
  assert.match(source, /ProgressPath must remain C:\\CamStationDev\\windows-control-setup-progress\.jsonl/u);
  assert.match(source, /Write-SetupProgress -Phase "file-report-start"/u);
  assert.match(source, /Write-SetupProgress -Phase "setup-complete"/u);
  assert.equal((source.match(/"[^"]+"\s*=\s*"[a-f0-9]{64}"/gu) ?? []).length, 6);
  assert.match(source, /archive contains an unexpected file set/iu);
  assert.match(source, /expectedArchiveRootName\s*=\s*"cua-driver-rs-\$DriverVersion-windows-x86_64"/u);
  assert.match(source, /\$stagedRootDirectories\.Count\s*-ne\s*1/u);
  assert.match(source, /\$stagedRootDirectories\[0\]\.Name\s*-cne\s*\$expectedArchiveRootName/u);
  assert.match(source, /\$stagedDirectories\.Count\s*-ne\s*0/u);
  assert.match(source, /\(\$stagedNames -join "`n"\)\s*-cne\s*\(\$expectedNames -join "`n"\)/u);
  assert.match(source, /incomplete or hash-mismatched.*instead of overwriting/isu);
  assert.match(source, /\$existingReport\s*=\s*@\(\)\s*\r?\n\s*if \(Test-Path/iu);
  assert.doesNotMatch(source, /\$existingReport\s*=\s*if \(Test-Path/iu);
  assert.match(source, /\.staging-/u);
  assert.match(source, /telemetry disable/u);
  assert.match(source, /\$telemetryAlreadyDisabled[\s\S]*telemetry_enabled\s*-eq\s*\$false/u);
  assert.match(source, /Write-SetupProgress -Phase "telemetry-already-disabled"/u);
  assert.match(source, /\$finalReport\s*=\s*@\(\$validatedFileReport\)/u);
  assert.match(source, /foreach \(\$setupCommand in \$setupCommands\)/u);
  assert.match(source, /setup command '\$\(\$setupCommand\.Name\)' returned/iu);
  assert.match(source, /\$taskRegistered\s*=\s*\$false/iu);
  assert.match(source, /Unregister-ScheduledTask.*-Confirm:\$false/u);
  assert.match(source, /vendor `autostart enable` command registers another Scheduled Task/iu);
  assert.match(source, /New-ScheduledTaskTrigger -AtLogOn -User \$TargetUser/u);
  assert.match(source, /Register-ScheduledTask -TaskName "cua-driver-serve"/u);
  assert.match(source, /autostart task exists but does not match the pinned 0\.19\.3 definition/iu);
  assert.match(source, /Start-ScheduledTask -TaskName "cua-driver-serve"/u);
  assert.match(source, /TemporarySetupTaskCount/u);
  assert.match(source, /Get-AuthenticodeSignature/u);
  assert.doesNotMatch(source, /New-NetFirewallRule|TcpListener|HttpListener|New-LocalUser|ConvertTo-SecureString/iu);
});
