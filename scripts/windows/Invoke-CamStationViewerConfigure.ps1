[CmdletBinding()]
param(
  [Parameter(Mandatory)]
  [ValidatePattern("^[^\\]+\\[^\\]+$")]
  [string]$TargetUser,

  [Parameter(Mandatory)]
  [string]$ConfigurationPath,

  [string]$ConsoleLaunchScript = "",

  [ValidateRange(15, 180)]
  [int]$TimeoutSeconds = 90,

  [string]$WorkRoot = "C:\CamStationDev\viewer-configure-runs"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$utf8 = [Text.UTF8Encoding]::new($false, $true)
$startedAt = [DateTimeOffset]::UtcNow

if ([string]::IsNullOrWhiteSpace($ConsoleLaunchScript)) {
  $ConsoleLaunchScript = Join-Path $PSScriptRoot "Invoke-CamStationViewerConsoleLaunch.ps1"
}

$TASK_CREATE = 2
$TASK_ACTION_EXEC = 0
$TASK_LOGON_INTERACTIVE_TOKEN = 3
$TASK_RUNLEVEL_LUA = 0
$TASK_STATE_QUEUED = 2
$TASK_STATE_RUNNING = 4
$taskPrefix = "CamStation-ViewerConfigure-"
$allowedWorkRoot = [IO.Path]::GetFullPath("C:\CamStationDev\viewer-configure-runs").TrimEnd("\")
$resolvedWorkRoot = [IO.Path]::GetFullPath($WorkRoot).TrimEnd("\")

function Release-ComObject {
  param([AllowNull()] [object]$Value)
  if ($null -ne $Value -and [Runtime.InteropServices.Marshal]::IsComObject($Value)) {
    [void][Runtime.InteropServices.Marshal]::FinalReleaseComObject($Value)
  }
}

function Get-Configuration {
  param([Parameter(Mandatory)] [string]$Path)

  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Viewer configuration file is missing" }
  $configuration = [IO.File]::ReadAllText([IO.Path]::GetFullPath($Path), $utf8) | ConvertFrom-Json
  $fields = @($configuration.PSObject.Properties.Name | Sort-Object)
  $expectedFields = @("autoStart", "displayName", "schemaVersion", "serverUrl") | Sort-Object
  if (($fields -join "`n") -cne ($expectedFields -join "`n")) {
    throw "Viewer configuration must contain exactly schemaVersion, serverUrl, displayName, and autoStart"
  }
  if ([int]$configuration.schemaVersion -ne 1 -or
      $configuration.serverUrl -isnot [string] -or
      $configuration.displayName -isnot [string] -or
      $configuration.autoStart -isnot [bool]) {
    throw "Viewer configuration fields have invalid types"
  }
  $serverUrl = [string]$configuration.serverUrl
  $displayName = [string]$configuration.displayName
  if ([string]::IsNullOrWhiteSpace($serverUrl) -or $serverUrl -ne $serverUrl.Trim() -or
      $serverUrl.IndexOfAny([char[]](0..31)) -ge 0) { throw "Viewer configuration URL is invalid" }
  $uri = $null
  if (-not [Uri]::TryCreate($serverUrl, [UriKind]::Absolute, [ref]$uri) -or
      $uri.Scheme -notin @("http", "https") -or [string]::IsNullOrWhiteSpace($uri.Host)) {
    throw "Viewer configuration URL must be an absolute HTTP or HTTPS origin"
  }
  if (-not [string]::IsNullOrEmpty($uri.UserInfo) -or $uri.AbsolutePath -notin @("", "/") -or
      -not [string]::IsNullOrEmpty($uri.Query) -or -not [string]::IsNullOrEmpty($uri.Fragment)) {
    throw "Viewer configuration URL must not contain credentials, path, query, or fragment"
  }
  if ($displayName -ne $displayName.Trim() -or $displayName.Length -lt 1 -or
      $displayName.Length -gt 128 -or $displayName.IndexOfAny([char[]](0..31)) -ge 0) {
    throw "Viewer display name is invalid"
  }
  return [ordered]@{
    SchemaVersion = 1
    ServerUrl = $serverUrl.TrimEnd("/")
    DisplayName = $displayName
    AutoStart = [bool]$configuration.autoStart
  }
}

function Get-PersistedViewerConfiguration {
  $registryPath = "HKLM:\Software\CamStation\Viewer"
  if (-not (Test-Path -LiteralPath $registryPath)) { return $null }
  $property = Get-ItemProperty -LiteralPath $registryPath -ErrorAction Stop
  $value = $property.PSObject.Properties["Configuration"]
  if ($null -eq $value -or $value.Value -isnot [string]) { return $null }
  return ([string]$value.Value | ConvertFrom-Json)
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

$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw "Viewer configuration launcher requires an elevated administrator session"
}
if (-not [string]::Equals($resolvedWorkRoot, $allowedWorkRoot, [StringComparison]::OrdinalIgnoreCase)) {
  throw "WorkRoot must remain $allowedWorkRoot"
}
if (-not (Test-Path -LiteralPath $ConsoleLaunchScript -PathType Leaf)) {
  throw "Invoke-CamStationViewerConsoleLaunch.ps1 is missing"
}
if ((Get-Service -Name "CamStationViewerService" -ErrorAction Stop).Status -ne
    [System.ServiceProcess.ServiceControllerStatus]::Running) {
  throw "CamStationViewerService must be running before configuration"
}

$configuration = Get-Configuration -Path $ConfigurationPath
$configurationSha256 = (Get-FileHash -LiteralPath $ConfigurationPath -Algorithm SHA256).Hash.ToLowerInvariant()
$before = Get-PersistedViewerConfiguration
$beforeClientId = if ($null -ne $before -and $before.PSObject.Properties["clientId"] -and
    -not [string]::IsNullOrWhiteSpace([string]$before.clientId)) { [string]$before.clientId } else { $null }
$explorers = @(Get-Process -Name explorer -IncludeUserName -ErrorAction SilentlyContinue |
    Where-Object { [string]::Equals($_.UserName, $TargetUser, [StringComparison]::OrdinalIgnoreCase) })
if ($explorers.Count -ne 1 -or [int]$explorers[0].SessionId -eq 0) {
  throw "TargetUser must own exactly one active nonzero Explorer session"
}
$targetSessionId = [int]$explorers[0].SessionId
$targetSid = ([Security.Principal.NTAccount]::new($TargetUser)).Translate(
  [Security.Principal.SecurityIdentifier]).Value

$runId = "{0}-{1}" -f [DateTimeOffset]::UtcNow.ToString("yyyyMMddTHHmmssfffZ"), ([guid]::NewGuid().ToString("N"))
$runDirectory = Join-Path $resolvedWorkRoot $runId
$taskName = "$taskPrefix$runId"
$configurationCopy = Join-Path $runDirectory "configuration.json"
$resultPath = Join-Path $runDirectory "result.txt"
$scheduler = $null
$rootFolder = $null
$definition = $null
$action = $null
$registeredTask = $null
$runningTask = $null
$taskRegistered = $false
$capturedFailure = $null

try {
  New-Item -ItemType Directory -Path $runDirectory -Force | Out-Null
  Copy-Item -LiteralPath $ConfigurationPath -Destination $configurationCopy
  if ((Get-FileHash -LiteralPath $configurationCopy -Algorithm SHA256).Hash.ToLowerInvariant() -ne
      $configurationSha256) { throw "Copied Viewer configuration hash mismatch" }
  & "$env:SystemRoot\System32\icacls.exe" $runDirectory /inheritance:r /grant:r `
    "*S-1-5-18:(OI)(CI)(F)" `
    "*S-1-5-32-544:(OI)(CI)(F)" `
    "*${targetSid}:(OI)(CI)(M)" | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "Failed to apply Viewer configuration run ACL" }

  $scheduler = New-Object -ComObject "Schedule.Service"
  $scheduler.Connect()
  $rootFolder = $scheduler.GetFolder("\")
  $definition = $scheduler.NewTask(0)
  $definition.RegistrationInfo.Author = "CamStation"
  $definition.RegistrationInfo.Description = "One-shot CamStation Viewer configuration"
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
  $action.Arguments = "-NoLogo -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$ConsoleLaunchScript`" -ConfigPath `"$configurationCopy`" -ResultPath `"$resultPath`" -ConfigureOnly"

  $registeredTask = $rootFolder.RegisterTaskDefinition(
    $taskName, $definition, $TASK_CREATE, $TargetUser, $null, $TASK_LOGON_INTERACTIVE_TOKEN, $null)
  $taskRegistered = $true
  $runningTask = $registeredTask.Run($null)

  $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
  while (-not (Test-Path -LiteralPath $resultPath -PathType Leaf)) {
    if ([DateTimeOffset]::UtcNow -ge $deadline) { throw "Viewer configuration task timed out" }
    $state = [int]$registeredTask.State
    if ($state -notin @($TASK_STATE_QUEUED, $TASK_STATE_RUNNING) -and
        ([DateTime]$registeredTask.LastRunTime).Year -gt 2000) {
      Start-Sleep -Milliseconds 200
      if (-not (Test-Path -LiteralPath $resultPath -PathType Leaf)) {
        throw "Viewer configuration task exited without a result; LastTaskResult=$([int]$registeredTask.LastTaskResult)"
      }
    }
    Start-Sleep -Milliseconds 100
  }
  $resultCode = 0
  if (-not [int]::TryParse([IO.File]::ReadAllText($resultPath).Trim(), [ref]$resultCode) -or $resultCode -ne 0) {
    throw "Viewer configuration helper returned code $resultCode"
  }
} catch {
  $capturedFailure = $_
} finally {
  if ($taskRegistered -and $null -ne $registeredTask) {
    try { if ([int]$registeredTask.State -eq $TASK_STATE_RUNNING) { $registeredTask.Stop(0) } } catch {}
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
  $capturedFailure = [InvalidOperationException]::new("One-shot Viewer configuration task was not deleted")
}
if ($null -ne $capturedFailure) {
  if (-not $taskStillExists -and (Test-Path -LiteralPath $runDirectory)) {
    try { Remove-Item -LiteralPath $runDirectory -Recurse -Force } catch {}
  }
  throw "Viewer configuration failed; TaskDeleted=$(-not $taskStillExists); $($capturedFailure.Exception.Message)"
}

try {
  $after = Get-PersistedViewerConfiguration
  if ($null -eq $after -or [int]$after.schemaVersion -ne 1 -or
      [string]$after.serverUrl -ne $configuration.ServerUrl -or
      [string]$after.displayName -ne $configuration.DisplayName -or
      [bool]$after.autoStart -ne $configuration.AutoStart -or
      [string]::IsNullOrWhiteSpace([string]$after.clientId)) {
    throw "Persisted Viewer configuration does not match the requested public fields"
  }
  $clientIdPreserved = $null -ne $beforeClientId -and
    [string]::Equals($beforeClientId, [string]$after.clientId, [StringComparison]::Ordinal)
  $clientIdCreated = $null -eq $beforeClientId -and -not [string]::IsNullOrWhiteSpace([string]$after.clientId)
  if (-not $clientIdPreserved -and -not $clientIdCreated) {
    throw "Viewer configuration did not preserve or create its private client identity"
  }
} finally {
  if (Test-Path -LiteralPath $runDirectory) { Remove-Item -LiteralPath $runDirectory -Recurse -Force }
}

[ordered]@{
  SchemaVersion = 1
  Result = "CAMSTATION_VIEWER_CONFIGURATION_COMPLETE"
  RunId = $runId
  TargetUser = $TargetUser
  TargetSessionId = $targetSessionId
  ConfigurationSha256 = $configurationSha256
  ServerOriginSha256 = ([BitConverter]::ToString(
      [Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($configuration.ServerUrl))
    ).Replace("-", "").ToLowerInvariant())
  DisplayNameSha256 = ([BitConverter]::ToString(
      [Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($configuration.DisplayName))
    ).Replace("-", "").ToLowerInvariant())
  AutoStart = $configuration.AutoStart
  ClientIdPreserved = $clientIdPreserved
  ClientIdCreated = $clientIdCreated
  ViewerService = [string](Get-Service -Name "CamStationViewerService").Status
  TaskDeleted = -not $taskStillExists
  RunDirectoryRemoved = -not (Test-Path -LiteralPath $runDirectory)
  ElapsedMs = [Math]::Round(([DateTimeOffset]::UtcNow - $startedAt).TotalMilliseconds)
} | ConvertTo-Json -Depth 5 -Compress
