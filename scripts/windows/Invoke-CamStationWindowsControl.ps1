[CmdletBinding()]
param(
  [Parameter(Mandatory)]
  [ValidatePattern("^[^\\]+\\[^\\]+$")]
  [string]$TargetUser,

  [ValidateSet("Status", "Plan", "ViewerCapture", "ViewerConfigure", "Cleanup")]
  [string]$Mode = "Status",

  [string]$PlanPath = "",

  [string]$RunId = "",

  [ValidateSet("LaunchAndCapture", "Capture")]
  [string]$ViewerOperation = "Capture",

  [string]$DriverPath = "C:\Program Files\Cua Driver\0.19.3\cua-driver.exe",

  [string]$WorkerScript = "",

  [string]$ViewerLauncherScript = "",

  [string]$ViewerConfigureLauncherScript = "",

  [string]$ViewerConfigurationPath = "",

  [string]$EvidenceRoot = "C:\CamStationDev\windows-control-runs",

  [ValidateRange(15, 180)]
  [int]$TimeoutSeconds = 90,

  [ValidateRange(5, 120)]
  [int]$ToolTimeoutSeconds = 30,

  [switch]$FullAudit,

  [switch]$AutoCleanup
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$startedAt = [DateTimeOffset]::UtcNow

if ([string]::IsNullOrWhiteSpace($WorkerScript)) {
  $WorkerScript = Join-Path $PSScriptRoot "Invoke-CamStationWindowsControlWorker.ps1"
}
if ([string]::IsNullOrWhiteSpace($ViewerLauncherScript)) {
  $ViewerLauncherScript = Join-Path $PSScriptRoot "Invoke-CamStationViewerGuiCapture.ps1"
}
if ([string]::IsNullOrWhiteSpace($ViewerConfigureLauncherScript)) {
  $ViewerConfigureLauncherScript = Join-Path $PSScriptRoot "Invoke-CamStationViewerConfigure.ps1"
}

$TASK_CREATE = 2
$TASK_ACTION_EXEC = 0
$TASK_LOGON_INTERACTIVE_TOKEN = 3
$TASK_RUNLEVEL_LUA = 0
$TASK_STATE_QUEUED = 2
$TASK_STATE_RUNNING = 4
$taskPrefix = "CamStation-WindowsControl-"
$allowedEvidenceRoot = [IO.Path]::GetFullPath("C:\CamStationDev\windows-control-runs").TrimEnd("\")
$resolvedEvidenceRoot = [IO.Path]::GetFullPath($EvidenceRoot).TrimEnd("\")

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public static class CamStationWindowsControlWts {
  [DllImport("wtsapi32.dll", EntryPoint = "WTSQuerySessionInformationW", SetLastError = true)]
  public static extern bool WTSQuerySessionInformation(
    IntPtr server, int sessionId, int infoClass, out IntPtr buffer, out int bytesReturned);

  [DllImport("wtsapi32.dll")]
  public static extern void WTSFreeMemory(IntPtr memory);
}
"@

function Release-ComObject {
  param([AllowNull()] [object]$Value)
  if ($null -ne $Value -and [Runtime.InteropServices.Marshal]::IsComObject($Value)) {
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($Value)
  }
}

function Get-TargetSession {
  param([Parameter(Mandatory)] [string]$User)

  $explorers = @(Get-Process -Name explorer -IncludeUserName -ErrorAction SilentlyContinue |
      Where-Object { [string]::Equals($_.UserName, $User, [StringComparison]::OrdinalIgnoreCase) })
  if ($explorers.Count -ne 1 -or $explorers[0].SessionId -eq 0) {
    throw "TargetUser must own exactly one active nonzero Explorer session"
  }
  return $explorers[0]
}

function Get-ControlTaskCount {
  $service = $null
  $folder = $null
  try {
    $service = New-Object -ComObject "Schedule.Service"
    $service.Connect()
    $folder = $service.GetFolder("\")
    return @($folder.GetTasks(1) | Where-Object { $_.Name -like "$taskPrefix*" }).Count
  } finally {
    Release-ComObject -Value $folder
    Release-ComObject -Value $service
  }
}

function Get-TargetSessionState {
  param([Parameter(Mandatory)] [int]$SessionId)

  $buffer = [IntPtr]::Zero
  $bytesReturned = 0
  if (-not [CamStationWindowsControlWts]::WTSQuerySessionInformation(
      [IntPtr]::Zero, $SessionId, 8, [ref]$buffer, [ref]$bytesReturned)) {
    throw "Unable to query the target Windows Terminal Services state"
  }
  try {
    if ($bytesReturned -lt 4) { throw "Windows Terminal Services returned an invalid state record" }
    $state = [Runtime.InteropServices.Marshal]::ReadInt32($buffer)
    $stateNames = @("Active", "Connected", "ConnectQuery", "Shadow", "Disconnected", "Idle", "Listen", "Reset", "Down", "Init")
    if ($state -lt 0 -or $state -ge $stateNames.Count) { return [string]$state }
    return $stateNames[$state]
  } finally {
    if ($buffer -ne [IntPtr]::Zero) { [CamStationWindowsControlWts]::WTSFreeMemory($buffer) }
  }
}

function Test-TaskExists {
  param([Parameter(Mandatory)] [string]$Name)

  $service = $null
  $folder = $null
  $task = $null
  try {
    $service = New-Object -ComObject "Schedule.Service"
    $service.Connect()
    $folder = $service.GetFolder("\")
    try { $task = $folder.GetTask($Name) } catch { return $false }
    return $null -ne $task
  } finally {
    Release-ComObject -Value $task
    Release-ComObject -Value $folder
    Release-ComObject -Value $service
  }
}

function Get-AutostartTaskStatus {
  $service = $null
  $folder = $null
  $task = $null
  $definition = $null
  $principal = $null
  try {
    $service = New-Object -ComObject "Schedule.Service"
    $service.Connect()
    $folder = $service.GetFolder("\")
    try { $task = $folder.GetTask("cua-driver-serve") } catch { return $null }
    $definition = $task.Definition
    $principal = $definition.Principal
    $stateNames = @("Unknown", "Disabled", "Queued", "Ready", "Running")
    $logonNames = @("None", "Password", "S4U", "Interactive", "Group", "ServiceAccount", "InteractiveOrPassword")
    $state = [int]$task.State
    $logonType = [int]$principal.LogonType
    return [ordered]@{
      State = if ($state -ge 0 -and $state -lt $stateNames.Count) { $stateNames[$state] } else { [string]$state }
      UserId = [string]$principal.UserId
      LogonType = if ($logonType -ge 0 -and $logonType -lt $logonNames.Count) {
        $logonNames[$logonType]
      } else { [string]$logonType }
      RunLevel = if ([int]$principal.RunLevel -eq 1) { "Highest" } else { "Limited" }
    }
  } finally {
    Release-ComObject -Value $principal
    Release-ComObject -Value $definition
    Release-ComObject -Value $task
    Release-ComObject -Value $folder
    Release-ComObject -Value $service
  }
}

function Get-CuaFirewallRegistryMatchCount {
  $registryPaths = @(
    "HKLM:\SYSTEM\CurrentControlSet\Services\SharedAccess\Parameters\FirewallPolicy\FirewallRules",
    "HKLM:\SOFTWARE\Policies\Microsoft\WindowsFirewall\FirewallRules"
  )
  $matches = 0
  foreach ($registryPath in $registryPaths) {
    if (-not (Test-Path -LiteralPath $registryPath)) { continue }
    $properties = (Get-ItemProperty -LiteralPath $registryPath).PSObject.Properties |
      Where-Object { $_.MemberType -eq "NoteProperty" -and $_.Name -notlike "PS*" }
    $matches += @($properties | Where-Object {
        $_.Name -match "cua" -or [string]$_.Value -match "cua"
      }).Count
  }
  return $matches
}

function Get-DriverStatus {
  param(
    [Parameter(Mandatory)] [string]$User,
    [Parameter(Mandatory)] [int]$SessionId,
    [Parameter(Mandatory)] [string]$Path,
    [AllowNull()] [object]$Autostart
  )

  $driverExists = Test-Path -LiteralPath $Path -PathType Leaf
  $driverProcesses = @()
  $sessionDriverProcesses = @()
  if ($driverExists) {
    $sessionDriverProcesses = @(Get-CimInstance Win32_Process -Filter "Name='cua-driver.exe'" -ErrorAction SilentlyContinue |
      Where-Object {
        [int]$_.SessionId -eq $SessionId
      })
    $driverProcesses = @($sessionDriverProcesses | Where-Object {
        [string]::Equals($_.ExecutablePath, $Path, [StringComparison]::OrdinalIgnoreCase)
      } |
      Select-Object ProcessId, SessionId, ExecutablePath, CommandLine)
  }
  $userSid = ([Security.Principal.NTAccount]::new($User)).Translate(
    [Security.Principal.SecurityIdentifier]).Value
  $profile = Get-CimInstance Win32_UserProfile -Filter "SID='$userSid'" -ErrorAction SilentlyContinue
  $configPath = if ($null -ne $profile -and -not [string]::IsNullOrWhiteSpace($profile.LocalPath)) {
    Join-Path $profile.LocalPath ".cua-driver\config.json"
  } else { $null }
  $telemetryDisabled = $false
  if ($null -ne $configPath -and (Test-Path -LiteralPath $configPath -PathType Leaf)) {
    try { $telemetryDisabled = (Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json).telemetry_enabled -eq $false } catch {}
  }

  return [ordered]@{
    Exists = $driverExists
    Version = if ($driverExists) { (& $Path --version | Out-String).Trim() } else { $null }
    Sha256 = if ($driverExists) { (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant() } else { $null }
    Signature = if ($driverExists) { [string](Get-AuthenticodeSignature -LiteralPath $Path).Status } else { $null }
    SessionProcessCount = $sessionDriverProcesses.Count
    MatchingProcessCount = $driverProcesses.Count
    Processes = $driverProcesses
    Autostart = $Autostart
    TelemetryDisabled = $telemetryDisabled
  }
}

$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw "Windows control launcher requires an elevated administrator session"
}
if (-not [string]::Equals($resolvedEvidenceRoot, $allowedEvidenceRoot, [StringComparison]::OrdinalIgnoreCase)) {
  throw "EvidenceRoot must remain $allowedEvidenceRoot"
}
if ((Get-Service -Name Schedule).Status -ne [System.ServiceProcess.ServiceControllerStatus]::Running) {
  throw "Windows Task Scheduler service is not running"
}

$targetExplorer = Get-TargetSession -User $TargetUser
$targetSessionId = [int]$targetExplorer.SessionId
$targetAccount = [Security.Principal.NTAccount]::new($TargetUser)
$targetSid = $targetAccount.Translate([Security.Principal.SecurityIdentifier]).Value

if ($Mode -eq "Status") {
  $autostartStatus = Get-AutostartTaskStatus
  $driverStatus = Get-DriverStatus -User $TargetUser -SessionId $targetSessionId `
    -Path $DriverPath -Autostart $autostartStatus
  $driverPids = @($driverStatus.Processes | Select-Object -ExpandProperty ProcessId)
  $netstatLines = if ($driverPids.Count -gt 0) {
    @(& "$env:SystemRoot\System32\netstat.exe" -ano -p tcp)
  } else { @() }
  $driverTcpCount = 0
  foreach ($processId in $driverPids) {
    $driverTcpCount += @($netstatLines | Where-Object { $_ -match "\s$processId\s*$" }).Count
  }
  $firewallCount = if ($FullAudit) {
    @(Get-NetFirewallRule -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match "cua" -or $_.DisplayName -match "cua" }).Count
  } else {
    Get-CuaFirewallRegistryMatchCount
  }
  $status = [ordered]@{
    SchemaVersion = 1
    Result = "WINDOWS_CONTROL_STATUS"
    TargetUser = $TargetUser
    TargetUserSid = $targetSid
    TargetSessionId = $targetSessionId
    TargetSessionState = Get-TargetSessionState -SessionId $targetSessionId
    Scheduler = [string](Get-Service -Name Schedule).Status
    ViewerService = if (Get-Service -Name CamStationViewerService -ErrorAction SilentlyContinue) {
      [string](Get-Service -Name CamStationViewerService).Status
    } else { $null }
    Driver = $driverStatus
    ExistingControlTasks = Get-ControlTaskCount
    DriverTcpConnectionCount = $driverTcpCount
    CuaFirewallRuleCount = $firewallCount
    FirewallAuditMode = if ($FullAudit) { "ActiveStore" } else { "RegistryFastPath" }
    ElapsedMs = [Math]::Round(([DateTimeOffset]::UtcNow - $startedAt).TotalMilliseconds)
  }
  $status | ConvertTo-Json -Depth 10 -Compress
  exit 0
}

if ($Mode -eq "Cleanup") {
  if ($RunId -notmatch "^[0-9]{8}T[0-9]{9}Z-[a-f0-9]{32}$") { throw "Cleanup requires an exact RunId" }
  $runDirectory = Join-Path $resolvedEvidenceRoot $RunId
  $resolvedRunDirectory = [IO.Path]::GetFullPath($runDirectory).TrimEnd("\")
  if (-not ($resolvedRunDirectory + "\").StartsWith($resolvedEvidenceRoot + "\", [StringComparison]::OrdinalIgnoreCase)) {
    throw "Cleanup target escaped EvidenceRoot"
  }
  if (Test-TaskExists -Name "$taskPrefix$RunId") {
    throw "Cannot clean a run while its task exists"
  }
  $removed = Test-Path -LiteralPath $resolvedRunDirectory
  if ($removed) { Remove-Item -LiteralPath $resolvedRunDirectory -Recurse -Force }
  [ordered]@{
    SchemaVersion = 1
    Result = "WINDOWS_CONTROL_CLEANUP_COMPLETE"
    RunId = $RunId
    Removed = $removed
    Remaining = [bool](Test-Path -LiteralPath $resolvedRunDirectory)
    ExistingControlTasks = Get-ControlTaskCount
  } | ConvertTo-Json -Compress
  exit 0
}

if ($Mode -eq "ViewerCapture") {
  if (-not (Test-Path -LiteralPath $ViewerLauncherScript -PathType Leaf)) {
    throw "Viewer capture launcher is missing"
  }
  & powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $ViewerLauncherScript `
    -TargetUser $TargetUser -Operation $ViewerOperation
  if ($LASTEXITCODE -ne 0) { throw "Viewer capture launcher failed" }
  exit 0
}

if ($Mode -eq "ViewerConfigure") {
  if (-not (Test-Path -LiteralPath $ViewerConfigureLauncherScript -PathType Leaf)) {
    throw "Viewer configuration launcher is missing"
  }
  if ([string]::IsNullOrWhiteSpace($ViewerConfigurationPath) -or
      -not (Test-Path -LiteralPath $ViewerConfigurationPath -PathType Leaf)) {
    throw "ViewerConfigure requires ViewerConfigurationPath"
  }
  & powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass `
    -File $ViewerConfigureLauncherScript -TargetUser $TargetUser `
    -ConfigurationPath $ViewerConfigurationPath
  if ($LASTEXITCODE -ne 0) { throw "Viewer configuration launcher failed" }
  exit 0
}

if ([string]::IsNullOrWhiteSpace($PlanPath)) { throw "Plan mode requires PlanPath" }
if ([string]::IsNullOrWhiteSpace($RunId) -eq $false) { throw "RunId is generated by Plan mode" }
if (-not (Test-Path -LiteralPath $PlanPath -PathType Leaf)) { throw "PlanPath is missing" }
$sourcePlan = [IO.File]::ReadAllText(
  [IO.Path]::GetFullPath($PlanPath), [Text.UTF8Encoding]::new($false)) | ConvertFrom-Json
if ($AutoCleanup -and @($sourcePlan.steps | Where-Object {
      $null -ne $_.PSObject.Properties["screenshot"]
    }).Count -gt 0) {
  throw "AutoCleanup cannot discard screenshot artifacts"
}
if (-not (Test-Path -LiteralPath $WorkerScript -PathType Leaf)) { throw "Windows control worker is missing" }
if (-not (Test-Path -LiteralPath $DriverPath -PathType Leaf)) { throw "Cua driver is missing; run the standard setup first" }
if (-not ([IO.Path]::GetFullPath($DriverPath).StartsWith("C:\Program Files\Cua Driver\", [StringComparison]::OrdinalIgnoreCase))) {
  throw "DriverPath must stay below C:\Program Files\Cua Driver"
}
$driverStatus = Get-DriverStatus -User $TargetUser -SessionId $targetSessionId `
  -Path $DriverPath -Autostart $null
if ($driverStatus.SessionProcessCount -ne 1 -or $driverStatus.MatchingProcessCount -ne 1) {
  throw "Plan mode requires exactly one Cua driver daemon from DriverPath in the target session"
}
if ((Get-ControlTaskCount) -ne 0) {
  throw "Another CamStation Windows control task already exists"
}

$generatedRunId = "{0}-{1}" -f [DateTimeOffset]::UtcNow.ToString("yyyyMMddTHHmmssfffZ"), ([guid]::NewGuid().ToString("N"))
$runDirectory = Join-Path $resolvedEvidenceRoot $generatedRunId
$taskName = "$taskPrefix$generatedRunId"
$completionPath = Join-Path $runDirectory "complete.json"
$progressPath = Join-Path $runDirectory "progress.json"
$workerCopy = Join-Path $runDirectory "Invoke-CamStationWindowsControlWorker.ps1"
$planCopy = Join-Path $runDirectory "plan.json"
$scheduler = $null
$rootFolder = $null
$definition = $null
$action = $null
$registeredTask = $null
$runningTask = $null
$taskRegistered = $false
$capturedFailure = $null
$workerResult = $null

try {
  New-Item -ItemType Directory -Path $runDirectory -Force | Out-Null
  Copy-Item -LiteralPath $WorkerScript -Destination $workerCopy
  Copy-Item -LiteralPath $PlanPath -Destination $planCopy
  [void]([IO.File]::ReadAllText($planCopy, [Text.UTF8Encoding]::new($false)) | ConvertFrom-Json)
  & "$env:SystemRoot\System32\icacls.exe" $runDirectory /inheritance:r /grant:r `
    "*S-1-5-18:(OI)(CI)(F)" `
    "*S-1-5-32-544:(OI)(CI)(F)" `
    "*${targetSid}:(OI)(CI)(M)" | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "Failed to apply the Windows control run ACL" }

  $scheduler = New-Object -ComObject "Schedule.Service"
  $scheduler.Connect()
  $rootFolder = $scheduler.GetFolder("\")
  $definition = $scheduler.NewTask(0)
  $definition.RegistrationInfo.Author = "CamStation"
  $definition.RegistrationInfo.Description = "One-shot CamStation Windows control plan"
  $definition.Principal.UserId = $TargetUser
  $definition.Principal.LogonType = $TASK_LOGON_INTERACTIVE_TOKEN
  $definition.Principal.RunLevel = $TASK_RUNLEVEL_LUA
  $definition.Settings.Enabled = $true
  $definition.Settings.Hidden = $true
  $definition.Settings.AllowDemandStart = $true
  $definition.Settings.DisallowStartIfOnBatteries = $false
  $definition.Settings.StopIfGoingOnBatteries = $false
  $definition.Settings.ExecutionTimeLimit = "PT3M"
  $definition.Settings.MultipleInstances = 2

  $action = $definition.Actions.Create($TASK_ACTION_EXEC)
  $action.Path = "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe"
  $action.WorkingDirectory = $runDirectory
  $action.Arguments = "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$workerCopy`" -PlanPath `"$planCopy`" -ResultDirectory `"$runDirectory`" -ExpectedUserSid `"$targetSid`" -DriverPath `"$DriverPath`" -ToolTimeoutSeconds $ToolTimeoutSeconds"

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
      throw "Interactive Windows control plan did not finish before the launcher timeout"
    }
    $taskState = [int]$registeredTask.State
    $lastRunTime = [DateTime]$registeredTask.LastRunTime
    if ($taskState -notin @($TASK_STATE_QUEUED, $TASK_STATE_RUNNING) -and
        $lastRunTime.Year -gt 2000) {
      Start-Sleep -Milliseconds 200
      if (-not (Test-Path -LiteralPath $completionPath -PathType Leaf)) {
        $progress = if (Test-Path -LiteralPath $progressPath -PathType Leaf) {
          (Get-Content -LiteralPath $progressPath -Raw | ConvertFrom-Json | ConvertTo-Json -Compress)
        } else { "missing" }
        throw "Interactive task exited before completion; State=$taskState; LastTaskResult=$([int]$registeredTask.LastTaskResult); Progress=$progress"
      }
    }
    Start-Sleep -Milliseconds 100
  }
  Start-Sleep -Milliseconds 100
  $workerResult = Get-Content -LiteralPath $completionPath -Raw | ConvertFrom-Json
  if ($workerResult.Success -ne $true) {
    $failureCleanup = @($workerResult.FailureCleanup) | ConvertTo-Json -Depth 8 -Compress
    throw "Interactive Windows control worker failed: $($workerResult.Error); FailureCleanup=$failureCleanup"
  }
  if ([int]$workerResult.SessionId -ne $targetSessionId -or $workerResult.UserSid -ne $targetSid) {
    throw "Interactive Windows control worker returned the wrong user or session identity"
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

$taskStillExists = Test-TaskExists -Name $taskName
if ($taskStillExists -and $null -eq $capturedFailure) {
  $capturedFailure = [InvalidOperationException]::new("One-shot Windows control task was not deleted")
}
if ($null -ne $capturedFailure) {
  if (-not $taskStillExists -and (Test-Path -LiteralPath $runDirectory -PathType Container)) {
    try { Remove-Item -LiteralPath $runDirectory -Recurse -Force } catch {}
  }
  $runRetained = Test-Path -LiteralPath $runDirectory -PathType Container
  throw "Windows control plan '$generatedRunId' failed; RunDirectoryRetained=$runRetained; $($capturedFailure.Exception.Message)"
}

$launcherResult = [ordered]@{
  SchemaVersion = 1
  Result = "WINDOWS_CONTROL_PLAN_COMPLETE"
  RunId = $generatedRunId
  ResultDirectory = $runDirectory
  TargetUser = $TargetUser
  TargetUserSid = $targetSid
  TargetSessionId = $targetSessionId
  TaskName = $taskName
  TaskDeleted = -not $taskStillExists
  WorkerScriptSha256 = (Get-FileHash -LiteralPath $workerCopy -Algorithm SHA256).Hash.ToLowerInvariant()
  PlanSha256 = (Get-FileHash -LiteralPath $planCopy -Algorithm SHA256).Hash.ToLowerInvariant()
  ElapsedMs = [Math]::Round(([DateTimeOffset]::UtcNow - $startedAt).TotalMilliseconds)
  Worker = $workerResult
}

if ($AutoCleanup) {
  Remove-Item -LiteralPath $runDirectory -Recurse -Force
  $launcherResult.ResultDirectory = $null
  $launcherResult.AutoCleanup = $true
} else {
  $launcherResult.AutoCleanup = $false
}
$launcherResult | ConvertTo-Json -Depth 40 -Compress
