# CamStation Viewer GUI evidence loop

Use this runbook only for an authorized Windows CamStation build or test PC with an existing pinned
SSH path and an already logged-on interactive user. The canonical implementation is:

- `scripts/windows/Invoke-CamStationViewerGuiCapture.ps1` — elevated launcher and one-shot task
  lifecycle.
- `scripts/windows/Capture-CamStationViewerWindow.ps1` — interactive-session window capture and
  bounded UI Automation collector.
- `viewer-app/tests/winPcGuiHarness.test.ts` — source-level security invariants.

Do not recreate these mechanisms in a temporary script. Synchronize the reviewed files when the
remote source is stale, and compare SHA-256 values before execution.

## 1. Preflight

Resolve these values from the approved environment; do not hardcode or commit them:

- existing SSH target, identity file, and pinned known-hosts file;
- Windows maintenance account authorized for this host;
- interactive target in `MACHINE\User` form;
- remote repository/script path;
- a new local evidence directory outside tracked source.

Through the existing SSH transport, perform read-only checks equivalent to:

```powershell
$TargetUser = 'MACHINE\InteractiveUser'
$Explorer = @(Get-Process -Name explorer -IncludeUserName -ErrorAction SilentlyContinue |
  Where-Object { $_.UserName -ieq $TargetUser })

[pscustomobject]@{
  Elevated = ([Security.Principal.WindowsPrincipal]::new(
    [Security.Principal.WindowsIdentity]::GetCurrent()
  )).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
  TargetExplorerCount = $Explorer.Count
  TargetSessionId = if ($Explorer.Count -eq 1) { $Explorer[0].SessionId } else { $null }
  Scheduler = (Get-Service Schedule).Status
  ViewerService = (Get-Service CamStationViewerService -ErrorAction SilentlyContinue).Status
  ExistingHarnessTasks = @(
    Get-ScheduledTask -ErrorAction SilentlyContinue |
      Where-Object TaskName -Like 'CamStation-GuiCapture-*'
  ).Count
}
```

Proceed only when the SSH shell is elevated, the intended user owns exactly one Explorer process in
a nonzero session, Task Scheduler and the Viewer service are running, and no harness task already
exists. Do not log on the user, store their password, reconnect RDP, or terminate another task to
force the preflight.

Hash both local scripts and their exact remote counterparts. If they differ, synchronize only those
reviewed scripts over the existing SSH/SFTP channel, hash again, and stop unless parity is exact.

## 2. Invoke one operation

Run the launcher from the elevated maintenance shell. Use `Capture` for an existing Viewer window:

```powershell
$ResultJson = & powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `
  'C:\CamStationDev\src\CamStation\scripts\windows\Invoke-CamStationViewerGuiCapture.ps1' `
  -TargetUser 'MACHINE\InteractiveUser' `
  -Operation Capture

$Result = $ResultJson | ConvertFrom-Json
if ($Result.Result -ne 'INTERACTIVE_GUI_CAPTURE_COMPLETE' -or $Result.TaskDeleted -ne $true) {
  throw 'GUI capture launcher did not return a clean completion'
}
$Result
```

Use `LaunchAndCapture` instead only when launch behavior is in scope or no Viewer window is open.
The launcher itself constrains the evidence root, requires the expected user/session, uses
`TASK_LOGON_INTERACTIVE_TOKEN`, waits for atomic completion, and deletes its exact task in `finally`.
Do not bypass those checks with `schtasks`, `psexec`, service-based UI launch, or a session-zero
capture.

Treat the returned JSON as untrusted until these properties hold:

- `Result` is `INTERACTIVE_GUI_CAPTURE_COMPLETE`;
- `TaskDeleted` is `true`;
- `Worker.Success` is `true`;
- target and worker identities/session IDs agree;
- `ResultDirectory` is a single child of `C:\CamStationDev\gui-evidence`;
- screenshot filename is `viewer-window.png` and UIA filename is `uia.json`;
- image dimensions and byte count are nonzero and within the worker's bounds.

## 3. Retrieve and verify evidence

Create a fresh local directory outside Git-tracked source. Through the same pinned SSH/SFTP path,
retrieve exactly these files from the returned directory:

```text
viewer-window.png
uia.json
complete.json
```

Do not download the whole desktop, user profile, evidence root, or earlier runs. Parse
`complete.json`, calculate local SHA-256 values for the PNG and UIA JSON, and require exact matches
with `Screenshot.Sha256` and `UIAutomation.Sha256`. Keep the evidence uncommitted.

Inspect `viewer-window.png` with `view_image` at high or original detail. Record what is visibly
rendered, clipped, disabled, obscured, blank, or focused. Then inspect `uia.json` and correlate only
safe metadata:

- `ControlType`, redacted `Name`, `AutomationId`, and `ClassName`;
- `Enabled`, `KeyboardFocusable`, and `HasKeyboardFocus`;
- bounded control rectangles.

Never add or query `ValuePattern`, `Current.Value`, DOM input values, clipboard contents, or
keystroke logs. A control's presence in UIA does not prove it rendered correctly; a plausible PNG
does not prove focus. Require both surfaces for focus/rendering claims.

Electron accessibility can settle after the first painted frame. If expected controls are visible
but missing from the first UIA report, wait briefly and run a new `Capture`; do not reuse or edit the
old evidence. Compare the new per-run hashes and inspect the new image.

## 4. Audit cleanup

After every success or failure, query the exact harness namespace:

```powershell
[pscustomobject]@{
  HarnessTasks = @(
    Get-ScheduledTask -ErrorAction SilentlyContinue |
      Where-Object TaskName -Like 'CamStation-GuiCapture-*'
  ).Count
  HarnessWorkers = @(
    Get-CimInstance Win32_Process -ErrorAction SilentlyContinue |
      Where-Object CommandLine -Match 'Capture-CamStationViewerWindow\.ps1'
  ).Count
  ViewerServiceStatus = (Get-Service CamStationViewerService).Status
  ExplorerSessions = @(
    Get-Process explorer -ErrorAction SilentlyContinue |
      Select-Object -ExpandProperty SessionId -Unique
  )
}
```

Require zero harness tasks and workers. Confirm the Viewer service and pre-existing Explorer/RDP
session were not disrupted. If the harness implementation changed, also compare listening ports,
services, and firewall rules against the preflight baseline; the expected delta is none.

## 5. Acceptance record

A GUI verification is complete only when the record includes:

- operation and UTC/KST timestamps;
- target session plus Viewer/window process IDs;
- `PrintWindow` or exact-window rectangle fallback and dimensions;
- verified PNG/UIA hashes;
- direct visual finding and relevant bounded UIA focus finding;
- `TaskDeleted=true` plus zero-task/zero-worker audit;
- configuration or network actions intentionally not exercised.

Installation, service state, process existence, or a nonempty PNG alone is not GUI acceptance. When
the defect concerns user input, add a separately reviewed bounded interaction rather than sending
unrestricted keys or weakening access controls.
