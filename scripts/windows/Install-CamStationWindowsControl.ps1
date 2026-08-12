[CmdletBinding()]
param(
  [Parameter(Mandatory)]
  [ValidatePattern("^[^\\]+\\[^\\]+$")]
  [string]$TargetUser,

  [string]$ArchivePath = "",

  [ValidateSet("0.19.3")]
  [string]$DriverVersion = "0.19.3",

  [string]$InstallRoot = "C:\Program Files\Cua Driver",

  [ValidateRange(15, 120)]
  [int]$TimeoutSeconds = 60,

  [string]$ProgressPath = "",

  [switch]$RemoveArchiveAfterInstall
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$startedAt = [DateTimeOffset]::UtcNow
$expectedArchiveSha256 = "e48b0117e343cec2577fc12693c741e094f389f8d4aef91e06284960bb03bce1"
$expectedFiles = [ordered]@{
  "cua_driver_abi.h" = "c17169f41da321baa5e7e953323c3ad660b00790176ba381e93189fba3506587"
  "cua_driver_node_runtime.node" = "fa9231bfa0c3c9d6deb8ed32d29dd0ef96921b51ea6864c963fd36c99b716b1c"
  "cua_driver_sdk.dll" = "d7e67baef87fac1a315d86113eeb485fbf8e03e5906797f0bf79d24a990fa38b"
  "cua-cursor-theme.exe" = "dfb77cf50e5ffa2966b7223431f2495217c2ac267516e6e5168a1cc5ed1290d9"
  "cua-driver.exe" = "ad717d644d81d5c0610ed95ce144d98ea8be85aa78f530bba2e845429ce227bb"
  "cua-driver-uia.exe" = "d444dc440338233e1157cecb0ce22eefd3d28e3c4250ff58045a0e47daa071fc"
}
$resolvedInstallRoot = [IO.Path]::GetFullPath($InstallRoot).TrimEnd("\")
if (-not [string]::Equals($resolvedInstallRoot, "C:\Program Files\Cua Driver", [StringComparison]::OrdinalIgnoreCase)) {
  throw "InstallRoot must remain C:\Program Files\Cua Driver"
}
$versionDirectory = Join-Path $resolvedInstallRoot $DriverVersion
$driverPath = Join-Path $versionDirectory "cua-driver.exe"
$expectedArchiveRootName = "cua-driver-rs-$DriverVersion-windows-x86_64"
$stagingDirectory = Join-Path $resolvedInstallRoot (".staging-{0}" -f [guid]::NewGuid().ToString("N"))
$taskName = ""
$taskRegistered = $false
$installedNow = $false
$archiveRemoved = $false
$validatedFileReport = @()
$resolvedProgressPath = $null

if (-not [string]::IsNullOrWhiteSpace($ProgressPath)) {
  $resolvedProgressPath = [IO.Path]::GetFullPath($ProgressPath)
  if (-not [string]::Equals(
      $resolvedProgressPath,
      "C:\CamStationDev\windows-control-setup-progress.jsonl",
      [StringComparison]::OrdinalIgnoreCase)) {
    throw "ProgressPath must remain C:\CamStationDev\windows-control-setup-progress.jsonl"
  }
}

function Write-SetupProgress {
  param(
    [Parameter(Mandatory)] [string]$Phase,
    [string]$Detail = ""
  )

  if ($null -eq $resolvedProgressPath) { return }
  $line = [ordered]@{
    At = [DateTimeOffset]::UtcNow.ToString("o")
    Phase = $Phase
    Detail = $Detail
  } | ConvertTo-Json -Compress
  [IO.File]::AppendAllText(
    $resolvedProgressPath,
    "$line`r`n",
    [Text.UTF8Encoding]::new($false))
}

function Get-InstalledFileReport {
  param([Parameter(Mandatory)] [string]$Directory)

  $report = [Collections.Generic.List[object]]::new()
  foreach ($name in $expectedFiles.Keys) {
    Write-SetupProgress -Phase "file-report-start" -Detail $name
    $path = Join-Path $Directory $name
    $hash = if (Test-Path -LiteralPath $path -PathType Leaf) {
      (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    } else { $null }
    $signature = if ($null -ne $hash -and $name -match "\.(exe|dll|node)$") {
      [string](Get-AuthenticodeSignature -LiteralPath $path).Status
    } else { "not-applicable" }
    $report.Add([ordered]@{
        Name = $name
        Sha256 = $hash
        Matches = $hash -eq $expectedFiles[$name]
        Signature = $signature
      })
    Write-SetupProgress -Phase "file-report-complete" -Detail $name
  }
  return $report
}

$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
Write-SetupProgress -Phase "setup-start"
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
  throw "Windows control setup requires an elevated administrator session"
}
if ((Get-Service -Name Schedule).Status -ne [System.ServiceProcess.ServiceControllerStatus]::Running) {
  throw "Windows Task Scheduler service is not running"
}
$explorers = @(Get-Process -Name explorer -IncludeUserName -ErrorAction SilentlyContinue |
    Where-Object { [string]::Equals($_.UserName, $TargetUser, [StringComparison]::OrdinalIgnoreCase) })
if ($explorers.Count -ne 1 -or $explorers[0].SessionId -eq 0) {
  throw "TargetUser must own exactly one active nonzero Explorer session"
}
$targetSessionId = [int]$explorers[0].SessionId
$targetSid = ([Security.Principal.NTAccount]::new($TargetUser)).Translate(
  [Security.Principal.SecurityIdentifier]).Value
$targetProfile = Get-CimInstance Win32_UserProfile -Filter "SID='$targetSid'" -ErrorAction SilentlyContinue
if ($null -eq $targetProfile -or [string]::IsNullOrWhiteSpace($targetProfile.LocalPath)) {
  throw "TargetUser Windows profile could not be resolved"
}
$configPath = Join-Path $targetProfile.LocalPath ".cua-driver\config.json"
Write-SetupProgress -Phase "target-resolved" -Detail "session-$targetSessionId"
$resolvedArchive = $null
if ($RemoveArchiveAfterInstall -and [string]::IsNullOrWhiteSpace($ArchivePath)) {
  throw "RemoveArchiveAfterInstall requires ArchivePath"
}
if (-not [string]::IsNullOrWhiteSpace($ArchivePath)) {
  Write-SetupProgress -Phase "archive-hash-start"
  $resolvedArchive = (Resolve-Path -LiteralPath $ArchivePath).Path
  $archiveSha256 = (Get-FileHash -LiteralPath $resolvedArchive -Algorithm SHA256).Hash.ToLowerInvariant()
  if ($archiveSha256 -ne $expectedArchiveSha256) {
    throw "Driver archive SHA-256 does not match the pinned official release"
  }
  Write-SetupProgress -Phase "archive-hash-complete"
}

try {
  Write-SetupProgress -Phase "existing-report-start"
  $existingReport = @()
  if (Test-Path -LiteralPath $versionDirectory -PathType Container) {
    $existingReport = @(Get-InstalledFileReport -Directory $versionDirectory)
  }
  $existingExactFiles = if ($existingReport.Count -eq $expectedFiles.Count) {
    @(Get-ChildItem -LiteralPath $versionDirectory -File -ErrorAction SilentlyContinue).Count -eq $expectedFiles.Count -and
      @($existingReport | Where-Object { -not $_.Matches }).Count -eq 0
  } else { $false }
  if ($existingExactFiles) { $validatedFileReport = $existingReport }
  Write-SetupProgress -Phase "existing-report-complete" -Detail "exact-$existingExactFiles"

  if (-not $existingExactFiles) {
    if (Test-Path -LiteralPath $versionDirectory) {
      throw "Existing driver directory is incomplete or hash-mismatched; stop instead of overwriting it"
    }
    if ([string]::IsNullOrWhiteSpace($ArchivePath)) {
      throw "Driver is not installed; provide the pinned official release archive"
    }
    New-Item -ItemType Directory -Path $resolvedInstallRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $stagingDirectory | Out-Null
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    [IO.Compression.ZipFile]::ExtractToDirectory($resolvedArchive, $stagingDirectory)
    $stagedRootFiles = @(Get-ChildItem -LiteralPath $stagingDirectory -File)
    $stagedRootDirectories = @(Get-ChildItem -LiteralPath $stagingDirectory -Directory)
    if ($stagedRootFiles.Count -ne 0 -or $stagedRootDirectories.Count -ne 1 -or
        $stagedRootDirectories[0].Name -cne $expectedArchiveRootName) {
      throw "Driver archive contains an unexpected file set"
    }
    $payloadDirectory = $stagedRootDirectories[0].FullName
    $stagedFiles = @(Get-ChildItem -LiteralPath $payloadDirectory -File)
    $stagedDirectories = @(Get-ChildItem -LiteralPath $payloadDirectory -Directory -Recurse)
    $stagedNames = @($stagedFiles | ForEach-Object { $_.Name } | Sort-Object)
    $expectedNames = @($expectedFiles.Keys | Sort-Object)
    if ($stagedFiles.Count -ne $expectedFiles.Count -or $stagedDirectories.Count -ne 0 -or
        ($stagedNames -join "`n") -cne ($expectedNames -join "`n")) {
      throw "Driver archive contains an unexpected file set"
    }
    $stagedReport = @(Get-InstalledFileReport -Directory $payloadDirectory)
    if (@($stagedReport | Where-Object { -not $_.Matches }).Count -ne 0) {
      throw "One or more extracted driver files failed the pinned hash check"
    }
    $validatedFileReport = $stagedReport
    Move-Item -LiteralPath $payloadDirectory -Destination $versionDirectory
    $installedNow = $true
    Write-SetupProgress -Phase "archive-promoted"
  }

  $taskPrincipal = New-ScheduledTaskPrincipal -UserId $TargetUser -LogonType Interactive -RunLevel Highest
  $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Minutes 2) `
    -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
  $telemetryAlreadyDisabled = (Test-Path -LiteralPath $configPath -PathType Leaf) -and
    ((Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json).telemetry_enabled -eq $false)
  $setupCommands = @()
  if (-not $telemetryAlreadyDisabled) {
    $setupCommands = @(
      [pscustomobject]@{ Name = "telemetry-disable"; Arguments = "telemetry disable" }
    )
    Write-SetupProgress -Phase "setup-task-preflight-start"
    if (Get-ScheduledTask -TaskName "CamStation-WindowsControlSetup-*" -ErrorAction SilentlyContinue) {
      throw "Another Windows control setup task already exists"
    }
    Write-SetupProgress -Phase "setup-task-preflight-complete"
  } else {
    Write-SetupProgress -Phase "telemetry-already-disabled"
  }
  foreach ($setupCommand in $setupCommands) {
    Write-SetupProgress -Phase "setup-command-start" -Detail $setupCommand.Name
    $taskName = "CamStation-WindowsControlSetup-$([guid]::NewGuid().ToString('N'))"
    $action = New-ScheduledTaskAction -Execute $driverPath -Argument $setupCommand.Arguments
    try {
      Register-ScheduledTask -TaskName $taskName -Action $action -Principal $taskPrincipal `
        -Settings $settings | Out-Null
      $taskRegistered = $true
      Write-SetupProgress -Phase "setup-command-registered" -Detail $setupCommand.Name
      Start-ScheduledTask -TaskName $taskName
      Write-SetupProgress -Phase "setup-command-launched" -Detail $setupCommand.Name
      $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
      do {
        Start-Sleep -Milliseconds 200
        $task = Get-ScheduledTask -TaskName $taskName
      } while ($task.State -eq "Running" -and [DateTimeOffset]::UtcNow -lt $deadline)
      if ($task.State -eq "Running") {
        throw "Windows control setup command '$($setupCommand.Name)' timed out"
      }
      $taskInfo = Get-ScheduledTaskInfo -TaskName $taskName
      if ($taskInfo.LastTaskResult -ne 0) {
        throw "Windows control setup command '$($setupCommand.Name)' returned $($taskInfo.LastTaskResult)"
      }
      Write-SetupProgress -Phase "setup-command-complete" -Detail $setupCommand.Name
    } finally {
      if ($taskRegistered) {
        $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        if ($null -ne $task) {
          if ($task.State -eq "Running") {
            Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
          }
          Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
        }
        $taskRegistered = $false
        Write-SetupProgress -Phase "setup-command-cleaned" -Detail $setupCommand.Name
      }
    }
  }

  # The vendor `autostart enable` command registers another Scheduled Task. Running it from inside
  # a temporary Scheduled Task can deadlock the scheduler on Windows 11. Reproduce the pinned
  # 0.19.3 vendor definition from the elevated maintenance session, then start that task directly.
  $profilePath = [IO.Path]::GetFullPath([string]$targetProfile.LocalPath).TrimEnd("\")
  $quotedDriverPath = $driverPath.Replace("'", "''")
  $quotedProfilePath = $profilePath.Replace("'", "''")
  $autostartArguments = "-NoProfile -WindowStyle Hidden -NonInteractive -Command `"Start-Process " +
    "-FilePath '$quotedDriverPath' -ArgumentList 'serve' -WindowStyle Hidden " +
    "-WorkingDirectory '$quotedProfilePath'`""
  Write-SetupProgress -Phase "autostart-query-start"
  $autostart = Get-ScheduledTask -TaskName "cua-driver-serve" -ErrorAction SilentlyContinue
  Write-SetupProgress -Phase "autostart-query-complete" -Detail "exists-$($null -ne $autostart)"
  if ($null -eq $autostart) {
    $autostartAction = New-ScheduledTaskAction -Execute "powershell.exe" `
      -Argument $autostartArguments -WorkingDirectory $profilePath
    $autostartTrigger = New-ScheduledTaskTrigger -AtLogOn -User $TargetUser
    $autostartSettings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit ([TimeSpan]::Zero) `
      -MultipleInstances IgnoreNew -StartWhenAvailable -AllowStartIfOnBatteries `
      -DontStopIfGoingOnBatteries
    Register-ScheduledTask -TaskName "cua-driver-serve" -Action $autostartAction `
      -Trigger $autostartTrigger -Principal $taskPrincipal -Settings $autostartSettings | Out-Null
    Write-SetupProgress -Phase "autostart-registered"
    $autostart = Get-ScheduledTask -TaskName "cua-driver-serve" -ErrorAction Stop
  }

  $autostartActions = @($autostart.Actions)
  $autostartTriggers = @($autostart.Triggers)
  $autostartUser = [string]$autostart.Principal.UserId
  $autostartSid = try {
    $autostartAccount = if ($autostartUser.Contains("\")) {
      [Security.Principal.NTAccount]::new($autostartUser)
    } else {
      [Security.Principal.NTAccount]::new($env:COMPUTERNAME, $autostartUser)
    }
    $autostartAccount.Translate([Security.Principal.SecurityIdentifier]).Value
  } catch { $null }
  $triggerUser = if ($autostartTriggers.Count -eq 1) { [string]$autostartTriggers[0].UserId } else { "" }
  $triggerSid = try {
    $triggerAccount = if ($triggerUser.Contains("\")) {
      [Security.Principal.NTAccount]::new($triggerUser)
    } else {
      [Security.Principal.NTAccount]::new($env:COMPUTERNAME, $triggerUser)
    }
    $triggerAccount.Translate([Security.Principal.SecurityIdentifier]).Value
  } catch { $null }
  $autostartDefinitionMatches = $autostartActions.Count -eq 1 -and
    [string]::Equals([string]$autostartActions[0].Execute, "powershell.exe", [StringComparison]::OrdinalIgnoreCase) -and
    [string]::Equals([string]$autostartActions[0].Arguments, $autostartArguments, [StringComparison]::Ordinal) -and
    [string]::Equals([string]$autostartActions[0].WorkingDirectory, $profilePath, [StringComparison]::OrdinalIgnoreCase) -and
    $autostartTriggers.Count -eq 1 -and $autostartTriggers[0].Enabled -and $triggerSid -eq $targetSid -and
    $autostartSid -eq $targetSid -and [string]$autostart.Principal.LogonType -eq "Interactive" -and
    [string]$autostart.Principal.RunLevel -eq "Highest" -and
    [string]$autostart.Settings.MultipleInstances -eq "IgnoreNew" -and
    [string]$autostart.Settings.ExecutionTimeLimit -eq "PT0S" -and
    $autostart.Settings.StartWhenAvailable -and
    -not $autostart.Settings.DisallowStartIfOnBatteries -and
    -not $autostart.Settings.StopIfGoingOnBatteries
  if (-not $autostartDefinitionMatches) {
    throw "Driver autostart task exists but does not match the pinned 0.19.3 definition"
  }
  Write-SetupProgress -Phase "autostart-validated"

  Write-SetupProgress -Phase "driver-query-start"
  $driverProcesses = @(Get-CimInstance Win32_Process -Filter "Name='cua-driver.exe'" |
      Where-Object {
        [int]$_.SessionId -eq $targetSessionId -and
        [string]::Equals($_.ExecutablePath, $driverPath, [StringComparison]::OrdinalIgnoreCase)
      })
  if ($driverProcesses.Count -eq 0) {
    Start-ScheduledTask -TaskName "cua-driver-serve"
    Write-SetupProgress -Phase "driver-start-launched"
    $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
      Start-Sleep -Milliseconds 200
      $driverProcesses = @(Get-CimInstance Win32_Process -Filter "Name='cua-driver.exe'" |
          Where-Object {
            [int]$_.SessionId -eq $targetSessionId -and
            [string]::Equals($_.ExecutablePath, $driverPath, [StringComparison]::OrdinalIgnoreCase)
          })
    } while ($driverProcesses.Count -eq 0 -and [DateTimeOffset]::UtcNow -lt $deadline)
  }
  if ($driverProcesses.Count -ne 1) {
    throw "Expected exactly one driver daemon in the target session after starting autostart"
  }
  Write-SetupProgress -Phase "driver-ready" -Detail "pid-$($driverProcesses[0].ProcessId)"
} finally {
  if ($taskRegistered) {
    $task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($null -ne $task) {
      if ($task.State -eq "Running") { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue }
      Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
    }
  }
  if (Test-Path -LiteralPath $stagingDirectory) { Remove-Item -LiteralPath $stagingDirectory -Recurse -Force }
}

Write-SetupProgress -Phase "final-report-start"
$finalReport = @($validatedFileReport)
if (@($finalReport | Where-Object { -not $_.Matches }).Count -ne 0 -or
    @(Get-ChildItem -LiteralPath $versionDirectory -File).Count -ne $expectedFiles.Count) {
  throw "Final installed driver file set failed verification"
}
Write-SetupProgress -Phase "final-report-complete"
$autostart = Get-ScheduledTask -TaskName "cua-driver-serve" -ErrorAction SilentlyContinue
if ($null -eq $autostart) { throw "Driver autostart task is missing" }
$autostartUser = [string]$autostart.Principal.UserId
$autostartAccount = if ($autostartUser.Contains("\")) {
  [Security.Principal.NTAccount]::new($autostartUser)
} else {
  [Security.Principal.NTAccount]::new($env:COMPUTERNAME, $autostartUser)
}
$autostartSid = try {
  $autostartAccount.Translate([Security.Principal.SecurityIdentifier]).Value
} catch { $null }
if ($autostartSid -ne $targetSid -or [string]$autostart.Principal.LogonType -ne "Interactive") {
  throw "Driver autostart task is missing or belongs to the wrong user"
}
$driverProcesses = @(Get-CimInstance Win32_Process -Filter "Name='cua-driver.exe'" |
    Where-Object {
      [int]$_.SessionId -eq $targetSessionId -and
      [string]::Equals($_.ExecutablePath, $driverPath, [StringComparison]::OrdinalIgnoreCase)
    })
if ($driverProcesses.Count -ne 1) { throw "Expected exactly one driver daemon in the target session" }
$telemetryDisabled = (Test-Path -LiteralPath $configPath -PathType Leaf) -and
  ((Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json).telemetry_enabled -eq $false)
if (-not $telemetryDisabled) { throw "Driver telemetry is not disabled for TargetUser" }
Write-SetupProgress -Phase "final-state-validated"

if ($RemoveArchiveAfterInstall -and $null -ne $resolvedArchive -and
    (Test-Path -LiteralPath $resolvedArchive -PathType Leaf)) {
  Remove-Item -LiteralPath $resolvedArchive -Force
  $archiveRemoved = $true
}
Write-SetupProgress -Phase "setup-complete"

[ordered]@{
  SchemaVersion = 1
  Result = "WINDOWS_CONTROL_SETUP_COMPLETE"
  DriverVersion = $DriverVersion
  DriverPath = $driverPath
  InstalledNow = $installedNow
  ArchiveSha256 = $expectedArchiveSha256
  ArchiveRemoved = $archiveRemoved
  Files = $finalReport
  TelemetryDisabled = $telemetryDisabled
  DriverProcessId = [int]$driverProcesses[0].ProcessId
  DriverSessionId = [int]$driverProcesses[0].SessionId
  Autostart = [ordered]@{
    TaskName = $autostart.TaskName
    State = [string]$autostart.State
    UserId = $autostart.Principal.UserId
    LogonType = [string]$autostart.Principal.LogonType
    RunLevel = [string]$autostart.Principal.RunLevel
  }
  TemporarySetupTaskCount = @(Get-ScheduledTask -ErrorAction SilentlyContinue |
      Where-Object { $_.TaskName -like "CamStation-WindowsControlSetup-*" }).Count
  ElapsedMs = [Math]::Round(([DateTimeOffset]::UtcNow - $startedAt).TotalMilliseconds)
} | ConvertTo-Json -Depth 12 -Compress
