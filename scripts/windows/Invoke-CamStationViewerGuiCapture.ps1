[CmdletBinding()]
param(
  [Parameter(Mandatory)]
  [ValidatePattern("^[^\\]+\\[^\\]+$")]
  [string]$TargetUser,

  [ValidateSet("LaunchAndCapture", "Capture")]
  [string]$Operation = "LaunchAndCapture",

  [string]$WorkerScript = "",

  [string]$EvidenceRoot = "C:\CamStationDev\gui-evidence",

  [ValidateRange(15, 120)]
  [int]$TimeoutSeconds = 75,

  [ValidateRange(5, 60)]
  [int]$WorkerTimeoutSeconds = 30
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($WorkerScript)) {
  $WorkerScript = Join-Path $PSScriptRoot "Capture-CamStationViewerWindow.ps1"
}

$TASK_CREATE = 2
$TASK_ACTION_EXEC = 0
$TASK_LOGON_INTERACTIVE_TOKEN = 3
$TASK_RUNLEVEL_LUA = 0
$TASK_STATE_RUNNING = 4
$taskPrefix = "CamStation-GuiCapture-"
$scheduler = $null
$rootFolder = $null
$definition = $null
$action = $null
$registeredTask = $null
$runningTask = $null
$taskName = $null
$runDirectory = $null
$capturedFailure = $null
$workerResult = $null
$taskRegistered = $false

function Release-ComObject {
  param([AllowNull()] [object]$Value)
  if ($null -ne $Value -and [Runtime.InteropServices.Marshal]::IsComObject($Value)) {
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($Value)
  }
}

$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw "GUI capture launcher requires an elevated administrator session"
}

$resolvedWorker = (Resolve-Path -LiteralPath $WorkerScript).Path
if (-not (Test-Path -LiteralPath $resolvedWorker -PathType Leaf)) {
  throw "GUI capture worker script is missing"
}
$resolvedEvidenceRoot = [IO.Path]::GetFullPath($EvidenceRoot).TrimEnd("\")
if (-not [string]::Equals($resolvedEvidenceRoot, "C:\CamStationDev\gui-evidence", [StringComparison]::OrdinalIgnoreCase)) {
  throw "EvidenceRoot must remain C:\CamStationDev\gui-evidence"
}
$targetAccount = [Security.Principal.NTAccount]::new($TargetUser)
$targetSid = $targetAccount.Translate([Security.Principal.SecurityIdentifier]).Value
$explorers = @(Get-Process -Name explorer -IncludeUserName -ErrorAction SilentlyContinue |
    Where-Object { [string]::Equals($_.UserName, $TargetUser, [StringComparison]::OrdinalIgnoreCase) })
if ($explorers.Count -ne 1 -or $explorers[0].SessionId -eq 0) {
  throw "TargetUser must own exactly one active nonzero Explorer session"
}
$targetSessionId = $explorers[0].SessionId
if ((Get-Service -Name Schedule).Status -ne [System.ServiceProcess.ServiceControllerStatus]::Running) {
  throw "Windows Task Scheduler service is not running"
}
if ((Get-Service -Name CamStationViewerService).Status -ne [System.ServiceProcess.ServiceControllerStatus]::Running) {
  throw "CamStation Viewer service is not running"
}
if (Get-ScheduledTask -ErrorAction SilentlyContinue | Where-Object { $_.TaskName -like "$taskPrefix*" }) {
  throw "Another CamStation GUI capture task already exists"
}

$runId = "{0}-{1}" -f [DateTimeOffset]::UtcNow.ToString("yyyyMMddTHHmmssfffZ"), ([guid]::NewGuid().ToString("N"))
$runDirectory = Join-Path $resolvedEvidenceRoot $runId
$taskName = "$taskPrefix$runId"
$completionPath = Join-Path $runDirectory "complete.json"
$workerCopy = Join-Path $runDirectory "Capture-CamStationViewerWindow.ps1"

try {
  New-Item -ItemType Directory -Path $runDirectory | Out-Null
  Copy-Item -LiteralPath $resolvedWorker -Destination $workerCopy
  & "$env:SystemRoot\System32\icacls.exe" $runDirectory /inheritance:r /grant:r `
    "*S-1-5-18:(OI)(CI)(F)" `
    "*S-1-5-32-544:(OI)(CI)(F)" `
    "*${targetSid}:(OI)(CI)(M)" | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "Failed to apply the GUI evidence directory ACL" }

  $scheduler = New-Object -ComObject "Schedule.Service"
  $scheduler.Connect()
  $rootFolder = $scheduler.GetFolder("\")
  $definition = $scheduler.NewTask(0)
  $definition.RegistrationInfo.Author = "CamStation"
  $definition.RegistrationInfo.Description = "One-shot CamStation Viewer window evidence capture"
  $definition.Principal.UserId = $TargetUser
  $definition.Principal.LogonType = $TASK_LOGON_INTERACTIVE_TOKEN
  $definition.Principal.RunLevel = $TASK_RUNLEVEL_LUA
  $definition.Settings.Enabled = $true
  $definition.Settings.Hidden = $true
  $definition.Settings.AllowDemandStart = $true
  $definition.Settings.DisallowStartIfOnBatteries = $false
  $definition.Settings.StopIfGoingOnBatteries = $false
  $definition.Settings.ExecutionTimeLimit = "PT2M"
  $definition.Settings.MultipleInstances = 2

  $action = $definition.Actions.Create($TASK_ACTION_EXEC)
  $action.Path = "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe"
  $action.WorkingDirectory = $runDirectory
  $action.Arguments = "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$workerCopy`" -Operation $Operation -ResultDirectory `"$runDirectory`" -ExpectedUserSid `"$targetSid`" -TimeoutSeconds $WorkerTimeoutSeconds"

  $registeredTask = $rootFolder.RegisterTaskDefinition(
    $taskName,
    $definition,
    $TASK_CREATE,
    $TargetUser,
    $null,
    $TASK_LOGON_INTERACTIVE_TOKEN,
    $null)
  $taskRegistered = $true
  $runningTask = $registeredTask.Run($null)

  $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
  while (-not (Test-Path -LiteralPath $completionPath -PathType Leaf)) {
    if ([DateTimeOffset]::UtcNow -ge $deadline) {
      throw "Interactive GUI capture did not finish before the launcher timeout"
    }
    Start-Sleep -Milliseconds 250
  }
  Start-Sleep -Milliseconds 150
  $workerResult = Get-Content -LiteralPath $completionPath -Raw | ConvertFrom-Json
  if ($workerResult.Success -ne $true) {
    throw "Interactive GUI worker failed: $($workerResult.Error)"
  }
  if ([int]$workerResult.SessionId -ne $targetSessionId -or $workerResult.UserSid -ne $targetSid) {
    throw "Interactive GUI worker returned the wrong user or session identity"
  }
} catch {
  $capturedFailure = $_
} finally {
  if ($taskRegistered -and $null -ne $registeredTask) {
    try {
      if ([int]$registeredTask.State -eq $TASK_STATE_RUNNING) { $registeredTask.Stop(0) }
    } catch {}
    try { $rootFolder.DeleteTask($taskName, 0) } catch {
      if ($null -eq $capturedFailure) { $capturedFailure = $_ }
    }
  }
  Release-ComObject -Value $runningTask
  Release-ComObject -Value $registeredTask
  Release-ComObject -Value $action
  Release-ComObject -Value $definition
  Release-ComObject -Value $rootFolder
  Release-ComObject -Value $scheduler
}

$taskStillExists = $false
try {
  $taskStillExists = $null -ne (Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue)
} catch {}
if ($taskStillExists -and $null -eq $capturedFailure) {
  $capturedFailure = [InvalidOperationException]::new("One-shot GUI capture task was not deleted")
}
if ($null -ne $capturedFailure) { throw $capturedFailure }

$launcherResult = [ordered]@{
  SchemaVersion = 1
  Result = "INTERACTIVE_GUI_CAPTURE_COMPLETE"
  RunId = $runId
  ResultDirectory = $runDirectory
  TargetUser = $TargetUser
  TargetUserSid = $targetSid
  TargetSessionId = $targetSessionId
  TaskName = $taskName
  TaskDeleted = -not $taskStillExists
  WorkerScriptSha256 = (Get-FileHash -LiteralPath $workerCopy -Algorithm SHA256).Hash.ToLowerInvariant()
  Worker = $workerResult
}
$launcherResult | ConvertTo-Json -Depth 12 -Compress
