[CmdletBinding()]
param(
  [Parameter(Mandatory)]
  [string]$PlanPath,

  [Parameter(Mandatory)]
  [string]$ResultDirectory,

  [Parameter(Mandatory)]
  [ValidatePattern("^S-1-")]
  [string]$ExpectedUserSid,

  [Parameter(Mandatory)]
  [string]$DriverPath,

  [ValidateRange(5, 120)]
  [int]$ToolTimeoutSeconds = 30
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$utf8 = [Text.UTF8Encoding]::new($false)
$strictUtf8 = [Text.UTF8Encoding]::new($false, $true)
$fallbackCodePage = [Globalization.CultureInfo]::CurrentCulture.TextInfo.ANSICodePage
$fallbackEncoding = [Text.Encoding]::GetEncoding($fallbackCodePage)
try { [Console]::InputEncoding = $utf8 } catch {}
try { [Console]::OutputEncoding = $utf8 } catch {}
$OutputEncoding = $utf8

$allowedRoot = [IO.Path]::GetFullPath("C:\CamStationDev\windows-control-runs").TrimEnd("\")
$resolvedResultDirectory = [IO.Path]::GetFullPath($ResultDirectory).TrimEnd("\")
$resolvedPlanPath = [IO.Path]::GetFullPath($PlanPath)
$completionPath = Join-Path $resolvedResultDirectory "complete.json"
$progressPath = Join-Path $resolvedResultDirectory "progress.json"
$startedAt = [DateTimeOffset]::UtcNow
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$sessionId = [Diagnostics.Process]::GetCurrentProcess().SessionId
$stepOutputs = @{}
$stepRecords = [Collections.Generic.List[object]]::new()
$assertionRecords = [Collections.Generic.List[object]]::new()
$artifactRecords = [Collections.Generic.List[object]]::new()
$failureCleanupTargets = [Collections.Generic.List[object]]::new()
$failureCleanupRecords = [Collections.Generic.List[object]]::new()
$requiresVisualVerification = $false
$managedDesktopSession = $null
$capturedFailure = $null
$driverProcesses = @()
$verifiedDriverProcessId = $null
$lastDriverOutputEncoding = $null
$lastDriverOutputSha256 = $null

$readOnlyTools = @(
  "debug_window_info",
  "get_accessibility_tree",
  "get_agent_cursor_state",
  "get_config",
  "get_cursor_position",
  "get_desktop_state",
  "get_screen_size",
  "get_session_state",
  "get_window_state",
  "health_report",
  "list_apps",
  "list_windows",
  "verify_state",
  "zoom"
)
$mutatingTools = @(
  "bring_to_front",
  "click",
  "double_click",
  "drag",
  "hotkey",
  "launch_app",
  "move_cursor",
  "press_key",
  "right_click",
  "scroll",
  "set_value",
  "set_window_frame",
  "type_text"
)
$verificationTools = @(
  "get_accessibility_tree",
  "get_desktop_state",
  "get_window_state",
  "list_windows",
  "verify_state"
)
$desktopTools = @("get_desktop_state")

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public static class CamStationWindowsControlNative {
  [DllImport("user32.dll", SetLastError = true)]
  [return: MarshalAs(UnmanagedType.Bool)]
  public static extern bool PostMessage(IntPtr hWnd, uint msg, IntPtr wParam, IntPtr lParam);

  [DllImport("user32.dll")]
  [return: MarshalAs(UnmanagedType.Bool)]
  public static extern bool SetProcessDPIAware();

  [DllImport("user32.dll")]
  public static extern int GetSystemMetrics(int index);
}
"@

function Write-DesktopScreenshotFallback {
  param([Parameter(Mandatory)] [string]$Path)

  Add-Type -AssemblyName System.Drawing
  [void][CamStationWindowsControlNative]::SetProcessDPIAware()
  $left = [CamStationWindowsControlNative]::GetSystemMetrics(76)
  $top = [CamStationWindowsControlNative]::GetSystemMetrics(77)
  $width = [CamStationWindowsControlNative]::GetSystemMetrics(78)
  $height = [CamStationWindowsControlNative]::GetSystemMetrics(79)
  if ($width -lt 320 -or $height -lt 240 -or $width -gt 7680 -or $height -gt 4320) {
    throw "Interactive desktop dimensions are outside the fallback capture bounds: ${width}x${height}"
  }

  $bitmap = [Drawing.Bitmap]::new($width, $height, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
  $graphics = $null
  try {
    $graphics = [Drawing.Graphics]::FromImage($bitmap)
    $graphics.CopyFromScreen(
      $left,
      $top,
      0,
      0,
      [Drawing.Size]::new($width, $height),
      [Drawing.CopyPixelOperation]::SourceCopy)
    $bitmap.Save($Path, [Drawing.Imaging.ImageFormat]::Png)
  } finally {
    if ($null -ne $graphics) { $graphics.Dispose() }
    $bitmap.Dispose()
  }

  return [ordered]@{
    screenshot_width = $width
    screenshot_height = $height
    screenshot_mime_type = "image/png"
    capture_mode = "gdi-interactive-fallback"
    fallback_reason = "driver-desktop-json-invalid"
  }
}

function Write-JsonAtomic {
  param(
    [Parameter(Mandatory)] [string]$Path,
    [Parameter(Mandatory)] [object]$Value
  )

  $temporaryPath = "$Path.tmp-$PID"
  $json = $Value | ConvertTo-Json -Depth 40
  [IO.File]::WriteAllText($temporaryPath, $json, $utf8)
  Move-Item -LiteralPath $temporaryPath -Destination $Path -Force
}

function Write-ControlProgress {
  param(
    [Parameter(Mandatory)] [string]$Phase,
    [AllowNull()] [string]$StepId,
    [AllowNull()] [string]$Tool
  )

  Write-JsonAtomic -Path $progressPath -Value ([ordered]@{
      SchemaVersion = 1
      Phase = $Phase
      StepId = $StepId
      Tool = $Tool
      Timestamp = [DateTimeOffset]::UtcNow.ToString("o")
    })
}

function Get-ObjectProperty {
  param(
    [AllowNull()] [object]$Value,
    [Parameter(Mandatory)] [string]$Name
  )

  if ($null -eq $Value) { throw "Cannot resolve '$Name' from null" }
  if ($Value -is [Collections.IDictionary]) {
    if (-not $Value.Contains($Name)) { throw "Object does not contain '$Name'" }
    return $Value[$Name]
  }
  $property = $Value.PSObject.Properties[$Name]
  if ($null -eq $property) { throw "Object does not contain '$Name'" }
  return $property.Value
}

function ConvertTo-SafeUnicodeString {
  param([Parameter(Mandatory)] [AllowEmptyString()] [string]$Value)

  $builder = [Text.StringBuilder]::new($Value.Length)
  for ($index = 0; $index -lt $Value.Length; $index++) {
    $character = $Value[$index]
    if ([char]::IsHighSurrogate($character)) {
      if ($index + 1 -lt $Value.Length -and [char]::IsLowSurrogate($Value[$index + 1])) {
        [void]$builder.Append($character)
        [void]$builder.Append($Value[$index + 1])
        $index++
      } else {
        [void]$builder.Append([char]0xFFFD)
      }
    } elseif ([char]::IsLowSurrogate($character)) {
      [void]$builder.Append([char]0xFFFD)
    } else {
      [void]$builder.Append($character)
    }
  }
  return $builder.ToString()
}

function ConvertTo-SafeJsonValue {
  param([AllowNull()] [object]$Value)

  if ($null -eq $Value -or $Value -is [ValueType]) { return $Value }
  if ($Value -is [string]) { return ConvertTo-SafeUnicodeString -Value $Value }
  if ($Value -is [Collections.IDictionary]) {
    $safeDictionary = [ordered]@{}
    foreach ($key in $Value.Keys) {
      $safeDictionary[$key] = ConvertTo-SafeJsonValue -Value $Value[$key]
    }
    return $safeDictionary
  }
  if ($Value -is [Collections.IEnumerable] -and $Value -isnot [pscustomobject]) {
    $safeArray = @()
    foreach ($item in $Value) { $safeArray += ,(ConvertTo-SafeJsonValue -Value $item) }
    return ,$safeArray
  }

  $safeObject = [ordered]@{}
  foreach ($property in $Value.PSObject.Properties) {
    $safeObject[$property.Name] = ConvertTo-SafeJsonValue -Value $property.Value
  }
  return [pscustomobject]$safeObject
}

function Get-OptionalObjectProperty {
  param(
    [AllowNull()] [object]$Value,
    [Parameter(Mandatory)] [string]$Name
  )

  if ($null -eq $Value) { return $null }
  if ($Value -is [Collections.IDictionary]) {
    if ($Value.Contains($Name)) { return $Value[$Name] }
    return $null
  }
  $property = $Value.PSObject.Properties[$Name]
  if ($null -ne $property) { return $property.Value }
  return $null
}

function Test-ObjectHasProperty {
  param(
    [AllowNull()] [object]$Value,
    [Parameter(Mandatory)] [string]$Name
  )

  if ($null -eq $Value) { return $false }
  if ($Value -is [Collections.IDictionary]) { return $Value.Contains($Name) }
  return $null -ne $Value.PSObject.Properties[$Name]
}

function Get-ControlFailureMessage {
  param([AllowNull()] [object]$Failure)

  if ($null -eq $Failure) { return $null }
  if ($Failure -is [Management.Automation.ErrorRecord]) {
    return [string]$Failure.Exception.Message
  }
  if ($Failure -is [Exception]) { return [string]$Failure.Message }
  return [string]$Failure
}

function Get-BytesSha256 {
  param([Parameter(Mandatory)] [byte[]]$Bytes)

  $sha256 = [Security.Cryptography.SHA256]::Create()
  try {
    return ([BitConverter]::ToString($sha256.ComputeHash($Bytes))).Replace("-", "").ToLowerInvariant()
  } finally {
    $sha256.Dispose()
  }
}

function Get-SafeStepOutputSummary {
  param(
    [Parameter(Mandatory)] [string]$Tool,
    [Parameter(Mandatory)] [object]$Output
  )

  $summary = [ordered]@{}
  foreach ($name in @("effect", "success", "route", "pid", "window_id", "snapshot_id",
      "running", "active", "element_count", "returned_element_count", "total_element_count",
      "elements_complete", "screenshot_width", "screenshot_height", "screenshot_mime_type",
      "capture_mode", "fallback_reason")) {
    $value = Get-OptionalObjectProperty -Value $Output -Name $name
    if ($null -ne $value -and ($value -is [ValueType] -or $value -is [string])) {
      $summary[$name] = ConvertTo-SafeJsonValue -Value $value
    }
  }

  $delivery = Get-OptionalObjectProperty -Value $Output -Name "delivery"
  $deliveryMode = Get-OptionalObjectProperty -Value $delivery -Name "mode"
  if ($null -ne $deliveryMode) { $summary["delivery_mode"] = [string]$deliveryMode }

  $windows = Get-OptionalObjectProperty -Value $Output -Name "windows"
  if ($null -ne $windows) {
    $safeWindows = @()
    foreach ($window in @($windows)) {
      $bounds = Get-OptionalObjectProperty -Value $window -Name "bounds"
      $safeWindows += ,[ordered]@{
        Pid = Get-OptionalObjectProperty -Value $window -Name "pid"
        WindowId = Get-OptionalObjectProperty -Value $window -Name "window_id"
        AppName = ConvertTo-SafeJsonValue -Value ([string](
            Get-OptionalObjectProperty -Value $window -Name "app_name"))
        OnScreen = Get-OptionalObjectProperty -Value $window -Name "is_on_screen"
        Minimized = Get-OptionalObjectProperty -Value $window -Name "minimized"
        ZIndex = Get-OptionalObjectProperty -Value $window -Name "z_index"
        Bounds = if ($null -ne $bounds) {
          [ordered]@{
            X = Get-OptionalObjectProperty -Value $bounds -Name "x"
            Y = Get-OptionalObjectProperty -Value $bounds -Name "y"
            Width = Get-OptionalObjectProperty -Value $bounds -Name "width"
            Height = Get-OptionalObjectProperty -Value $bounds -Name "height"
          }
        } else { $null }
      }
    }
    $summary["window_count"] = $safeWindows.Count
    $summary["windows"] = $safeWindows
  }

  if ($summary.Count -eq 0) {
    $summary["field_names"] = @($Output.PSObject.Properties.Name |
        Where-Object { $_ -notin @("value", "elements", "tree_markdown", "_legacy_windows") } |
        Sort-Object -Unique)
  }
  return $summary
}

function Resolve-Reference {
  param([Parameter(Mandatory)] [string]$Path)

  if ($Path -notmatch "^[A-Za-z][A-Za-z0-9_-]*(?:\.(?:[A-Za-z][A-Za-z0-9_-]*|[0-9]+))*$") {
    throw "Invalid reference path: $Path"
  }
  $segments = $Path.Split(".")
  if (-not $stepOutputs.ContainsKey($segments[0])) {
    throw "Reference points to an unavailable step: $Path"
  }
  $value = $stepOutputs[$segments[0]]
  for ($index = 1; $index -lt $segments.Count; $index++) {
    $segment = $segments[$index]
    if ($segment -match "^[0-9]+$") {
      $collection = @($value)
      $arrayIndex = 0
      if (-not [int]::TryParse($segment, [ref]$arrayIndex)) {
        throw "Reference requires an array index at '$segment': $Path"
      }
      if ($arrayIndex -lt 0 -or $arrayIndex -ge $collection.Count) {
        throw "Reference array index is out of range: $Path"
      }
      $value = $collection[$arrayIndex]
    } elseif ($value -is [Array] -or $value -is [Collections.IList]) {
      throw "Reference requires an array index at '$segment': $Path"
    } else {
      $value = Get-ObjectProperty -Value $value -Name $segment
    }
  }
  return $value
}

function Resolve-Selection {
  param([Parameter(Mandatory)] [object]$Selection)

  $reference = [string](Get-ObjectProperty -Value $Selection -Name "ref")
  $collection = @(Resolve-Reference -Path $reference)
  $whereProperty = $Selection.PSObject.Properties["where"]
  if ($null -ne $whereProperty) {
    $conditions = @($whereProperty.Value.PSObject.Properties)
    $collection = @($collection | Where-Object {
      $candidate = $_
      foreach ($condition in $conditions) {
        if (-not (Test-ObjectHasProperty -Value $candidate -Name $condition.Name)) {
          return $false
        }
        $actual = Get-ObjectProperty -Value $candidate -Name $condition.Name
        if ($actual -is [string] -or $condition.Value -is [string]) {
          if (-not [string]::Equals([string]$actual, [string]$condition.Value, [StringComparison]::Ordinal)) {
            return $false
          }
        } elseif ($actual -ne $condition.Value) {
          return $false
        }
      }
      return $true
    })
  }
  if ($collection.Count -ne 1) {
    throw "Selection '$reference' resolved $($collection.Count) items; exactly one is required"
  }
  $property = $Selection.PSObject.Properties["property"]
  if ($null -eq $property) { return $collection[0] }
  return Get-ObjectProperty -Value $collection[0] -Name ([string]$property.Value)
}

function Resolve-PlanValue {
  param([AllowNull()] [object]$Value)

  if ($null -eq $Value -or $Value -is [string] -or $Value -is [ValueType]) { return $Value }
  if ($Value -is [Collections.IDictionary]) {
    $resolvedDictionary = [ordered]@{}
    foreach ($key in $Value.Keys) { $resolvedDictionary[$key] = Resolve-PlanValue -Value $Value[$key] }
    return $resolvedDictionary
  }
  if ($Value -is [Collections.IEnumerable] -and $Value -isnot [pscustomobject]) {
    $resolvedArray = @()
    foreach ($item in $Value) { $resolvedArray += ,(Resolve-PlanValue -Value $item) }
    return ,$resolvedArray
  }

  $properties = @($Value.PSObject.Properties)
  $referenceProperty = $Value.PSObject.Properties["`$ref"]
  if ($null -ne $referenceProperty) {
    if ($properties.Count -ne 1) { throw "A `$ref object cannot contain other fields" }
    return Resolve-Reference -Path ([string]$referenceProperty.Value)
  }
  $selectionProperty = $Value.PSObject.Properties["`$select"]
  if ($null -ne $selectionProperty) {
    if ($properties.Count -ne 1) { throw "A `$select object cannot contain other fields" }
    return Resolve-Selection -Selection $selectionProperty.Value
  }

  $resolvedObject = [ordered]@{}
  foreach ($property in $properties) {
    $resolvedObject[$property.Name] = Resolve-PlanValue -Value $property.Value
  }
  return $resolvedObject
}

function Invoke-DriverTool {
  param(
    [Parameter(Mandatory)] [string]$Tool,
    [Parameter(Mandatory)] [object]$InputValue
  )

  $inputJson = $InputValue | ConvertTo-Json -Depth 30 -Compress
  $startInfo = [Diagnostics.ProcessStartInfo]::new()
  $startInfo.FileName = $DriverPath
  $startInfo.Arguments = "call $Tool"
  $startInfo.WorkingDirectory = $resolvedResultDirectory
  $startInfo.UseShellExecute = $false
  $startInfo.CreateNoWindow = $true
  $startInfo.RedirectStandardInput = $true
  $startInfo.RedirectStandardOutput = $true
  $startInfo.RedirectStandardError = $true

  $process = [Diagnostics.Process]::new()
  $stdoutBuffer = [IO.MemoryStream]::new()
  $stderrBuffer = [IO.MemoryStream]::new()
  $process.StartInfo = $startInfo
  try {
    if (-not $process.Start()) { throw "Failed to start cua-driver for $Tool" }
    $inputBytes = $utf8.GetBytes($inputJson)
    $process.StandardInput.BaseStream.Write($inputBytes, 0, $inputBytes.Length)
    $process.StandardInput.BaseStream.Close()
    $stdoutTask = $process.StandardOutput.BaseStream.CopyToAsync($stdoutBuffer)
    $stderrTask = $process.StandardError.BaseStream.CopyToAsync($stderrBuffer)
    if (-not $process.WaitForExit($ToolTimeoutSeconds * 1000)) {
      try { $process.Kill() } catch {}
      throw "cua-driver tool '$Tool' timed out after $ToolTimeoutSeconds seconds"
    }
    if (-not $stdoutTask.Wait(5000) -or -not $stderrTask.Wait(5000)) {
      throw "cua-driver tool '$Tool' output pipes did not close"
    }
    $stdoutBytes = $stdoutBuffer.ToArray()
    $stderrBytes = $stderrBuffer.ToArray()
    $script:lastDriverOutputSha256 = Get-BytesSha256 -Bytes $stdoutBytes
    try {
      $stdout = $strictUtf8.GetString($stdoutBytes).Trim()
      $script:lastDriverOutputEncoding = "utf-8"
    } catch [Text.DecoderFallbackException] {
      $stdout = $fallbackEncoding.GetString($stdoutBytes).Trim()
      $script:lastDriverOutputEncoding = "windows-$fallbackCodePage"
    }
    try {
      $stderr = $strictUtf8.GetString($stderrBytes).Trim()
    } catch [Text.DecoderFallbackException] {
      $stderr = $fallbackEncoding.GetString($stderrBytes).Trim()
    }
    if ($process.ExitCode -ne 0) {
      throw "cua-driver tool '$Tool' exited $($process.ExitCode): $stderr $stdout".Trim()
    }
    if ([string]::IsNullOrWhiteSpace($stdout)) { throw "cua-driver tool '$Tool' returned no JSON" }
    try {
      return ConvertTo-SafeJsonValue -Value ($stdout | ConvertFrom-Json)
    } catch {
      throw "cua-driver tool '$Tool' returned invalid UTF-8 JSON: $($_.Exception.Message)"
    }
  } finally {
    $stdoutBuffer.Dispose()
    $stderrBuffer.Dispose()
    $process.Dispose()
  }
}

function Test-ArtifactName {
  param([Parameter(Mandatory)] [string]$Name)
  return $Name -match "^[A-Za-z0-9][A-Za-z0-9._-]{0,79}\.png$" -and -not $Name.Contains("..")
}

try {
  Write-ControlProgress -Phase "preflight_started" -StepId $null -Tool $null
  if (-not ($resolvedResultDirectory + "\").StartsWith($allowedRoot + "\", [StringComparison]::OrdinalIgnoreCase)) {
    throw "ResultDirectory must be a child of $allowedRoot"
  }
  if (-not (Test-Path -LiteralPath $resolvedResultDirectory -PathType Container)) {
    throw "ResultDirectory does not exist"
  }
  if (-not ($resolvedPlanPath + "\").StartsWith($resolvedResultDirectory + "\", [StringComparison]::OrdinalIgnoreCase)) {
    throw "PlanPath must be inside ResultDirectory"
  }
  if (-not (Test-Path -LiteralPath $resolvedPlanPath -PathType Leaf)) { throw "PlanPath is missing" }
  if ($sessionId -eq 0) { throw "Windows control worker must run in a nonzero interactive session" }
  if ($null -eq $identity.User -or $identity.User.Value -ne $ExpectedUserSid) {
    throw "Windows control worker identity does not match ExpectedUserSid"
  }
  if (-not (Get-Process -Name explorer -ErrorAction SilentlyContinue | Where-Object { $_.SessionId -eq $sessionId })) {
    throw "No Explorer process exists in the Windows control worker session"
  }
  if (-not (Test-Path -LiteralPath $DriverPath -PathType Leaf)) { throw "Cua driver is missing" }
  if (-not ([IO.Path]::GetFullPath($DriverPath).StartsWith("C:\Program Files\Cua Driver\", [StringComparison]::OrdinalIgnoreCase))) {
    throw "DriverPath must stay below C:\Program Files\Cua Driver"
  }
  $driverProcesses = @(Get-Process -Name "cua-driver" -ErrorAction SilentlyContinue |
      Where-Object { [int]$_.SessionId -eq $sessionId })
  if ($driverProcesses.Count -ne 1) {
    throw "Expected exactly one cua-driver daemon in session $sessionId"
  }
  $verifiedDriverProcessId = [int]$driverProcesses[0].Id

  Write-ControlProgress -Phase "plan_loading" -StepId $null -Tool $null
  $plan = [IO.File]::ReadAllText($resolvedPlanPath, $utf8) | ConvertFrom-Json
  if ([int]$plan.schemaVersion -ne 1) { throw "Unsupported plan schemaVersion" }
  $steps = @($plan.steps)
  if ($steps.Count -lt 1 -or $steps.Count -gt 32) { throw "Plan must contain 1 to 32 steps" }
  $assertions = @()
  $assertionsProperty = $plan.PSObject.Properties["assertions"]
  if ($null -ne $assertionsProperty) { $assertions = @($assertionsProperty.Value) }
  foreach ($assertion in $assertions) {
    $assertionProperties = @($assertion.PSObject.Properties)
    $referenceProperty = $assertion.PSObject.Properties["ref"]
    $equalsProperty = $assertion.PSObject.Properties["equals"]
    $existsProperty = $assertion.PSObject.Properties["exists"]
    $countEqualsProperty = $assertion.PSObject.Properties["countEquals"]
    $whereProperty = $assertion.PSObject.Properties["where"]
    $operatorCount = 0
    foreach ($operator in @($equalsProperty, $existsProperty, $countEqualsProperty)) {
      if ($null -ne $operator) { $operatorCount++ }
    }
    $expectedAssertionPropertyCount = if ($null -ne $whereProperty) { 3 } else { 2 }
    if ($null -eq $referenceProperty -or
        $operatorCount -ne 1 -or
        $assertionProperties.Count -ne $expectedAssertionPropertyCount) {
      throw "Each assertion requires only ref plus exactly one of equals, exists, or countEquals"
    }
    if ($null -ne $whereProperty -and $null -eq $countEqualsProperty) {
      throw "Assertion where requires countEquals"
    }
    if ($null -ne $countEqualsProperty -and
        ([int]$countEqualsProperty.Value -lt 0 -or [int]$countEqualsProperty.Value -gt 10000)) {
      throw "countEquals must be between 0 and 10000"
    }
    if ([string]$referenceProperty.Value -notmatch
        "^[A-Za-z][A-Za-z0-9_-]*(?:\.(?:[A-Za-z][A-Za-z0-9_-]*|[0-9]+))*$") {
      throw "Invalid assertion reference: $($referenceProperty.Value)"
    }
  }
  $stepById = @{}
  for ($index = 0; $index -lt $steps.Count; $index++) {
    $step = $steps[$index]
    $stepId = [string]$step.id
    $tool = [string]$step.tool
    if ($stepId -notmatch "^[A-Za-z][A-Za-z0-9_-]{0,39}$") { throw "Invalid step id: $stepId" }
    if ($stepById.ContainsKey($stepId)) { throw "Duplicate step id: $stepId" }
    if ($tool -notin $readOnlyTools -and $tool -notin $mutatingTools) { throw "Tool is not allowed by the standard control runner: $tool" }
    $closeOnFailureProperty = $step.PSObject.Properties["closeWindowOnFailure"]
    if ($null -ne $closeOnFailureProperty -and
        ($tool -ne "launch_app" -or $closeOnFailureProperty.Value -isnot [bool])) {
      throw "closeWindowOnFailure is a boolean option for launch_app only"
    }
    $stepById[$stepId] = [ordered]@{ Index = $index; Tool = $tool; Step = $step }
  }
  foreach ($entry in $stepById.GetEnumerator()) {
    if ($entry.Value.Tool -notin $mutatingTools) { continue }
    $verificationProperty = $entry.Value.Step.PSObject.Properties["verifyWith"]
    if ($null -eq $verificationProperty) { throw "Mutating step '$($entry.Key)' requires verifyWith" }
    $verificationId = [string]$verificationProperty.Value
    if (-not $stepById.ContainsKey($verificationId)) { throw "verifyWith points to an unknown step: $verificationId" }
    $verification = $stepById[$verificationId]
    if ($verification.Index -le $entry.Value.Index -or $verification.Tool -notin $verificationTools) {
      throw "verifyWith for '$($entry.Key)' must point to a later observation step"
    }
    $verificationHasScreenshot = $null -ne $verification.Step.PSObject.Properties["screenshot"]
    $verificationHasAssertion = @($assertions | Where-Object {
        $assertionReference = [string]$_.ref
        $assertionReference -eq $verificationId -or $assertionReference.StartsWith("$verificationId.")
      }).Count -gt 0
    if (-not $verificationHasScreenshot -and -not $verificationHasAssertion) {
      throw "verifyWith for '$($entry.Key)' requires a screenshot or assertion on '$verificationId'"
    }
    if ($verificationHasScreenshot) { $requiresVisualVerification = $true }
  }

  Write-ControlProgress -Phase "plan_validated" -StepId $null -Tool $null

  if (@($steps | Where-Object { [string]$_.tool -in $desktopTools }).Count -gt 0) {
    $managedDesktopSession = "camstation-control-$([guid]::NewGuid().ToString('N'))"
    [void](Invoke-DriverTool -Tool "start_session" -InputValue ([ordered]@{
          session = $managedDesktopSession
          capture_scope = "desktop"
        }))
  }

  foreach ($step in $steps) {
    $stepStartedAt = [DateTimeOffset]::UtcNow
    $stepId = [string]$step.id
    $tool = [string]$step.tool
    Write-ControlProgress -Phase "step_started" -StepId $stepId -Tool $tool
    $inputProperty = $step.PSObject.Properties["input"]
    $resolvedInput = if ($null -eq $inputProperty) { [ordered]@{} } else { Resolve-PlanValue -Value $inputProperty.Value }
    if ($resolvedInput -isnot [Collections.IDictionary]) { throw "Step '$stepId' input must resolve to an object" }

    $artifactName = $null
    $screenshotProperty = $step.PSObject.Properties["screenshot"]
    if ($null -ne $screenshotProperty) {
      $artifactName = [string]$screenshotProperty.Value
      if ($tool -notin @("get_desktop_state", "get_window_state", "zoom")) {
        throw "Step '$stepId' cannot produce a screenshot"
      }
      if (-not (Test-ArtifactName -Name $artifactName)) { throw "Invalid screenshot name for '$stepId'" }
      $resolvedInput["screenshot_out_file"] = Join-Path $resolvedResultDirectory $artifactName
      $requiresVisualVerification = $true
    }
    if ($tool -eq "get_desktop_state") { $resolvedInput["session"] = $managedDesktopSession }

    try {
      $output = Invoke-DriverTool -Tool $tool -InputValue $resolvedInput
    } catch {
      $driverFailureMessage = Get-ControlFailureMessage -Failure $_
      if ($tool -ne "get_desktop_state" -or $null -eq $artifactName -or
          $driverFailureMessage -notmatch "returned invalid UTF-8 JSON") {
        throw
      }
      $output = Write-DesktopScreenshotFallback -Path (Join-Path $resolvedResultDirectory $artifactName)
    }
    $stepOutputs[$stepId] = $output
    $closeOnFailureProperty = $step.PSObject.Properties["closeWindowOnFailure"]
    if ($tool -eq "launch_app" -and $null -ne $closeOnFailureProperty -and
        [bool]$closeOnFailureProperty.Value) {
      $launchedPid = Get-OptionalObjectProperty -Value $output -Name "pid"
      $launchedWindows = @(Get-OptionalObjectProperty -Value $output -Name "windows")
      if ($null -eq $launchedPid -or $launchedWindows.Count -lt 1) {
        throw "launch_app with closeWindowOnFailure must return a pid and at least one window"
      }
      foreach ($launchedWindow in $launchedWindows) {
        $launchedWindowId = Get-OptionalObjectProperty -Value $launchedWindow -Name "window_id"
        $launchedBounds = Get-OptionalObjectProperty -Value $launchedWindow -Name "bounds"
        if ($null -eq $launchedWindowId) {
          throw "launch_app with closeWindowOnFailure returned a window without window_id"
        }
        $failureCleanupTargets.Add([ordered]@{
            StepId = $stepId
            Pid = [int]$launchedPid
            WindowId = [long]$launchedWindowId
            Bounds = if ($null -ne $launchedBounds) {
              [ordered]@{
                X = Get-OptionalObjectProperty -Value $launchedBounds -Name "x"
                Y = Get-OptionalObjectProperty -Value $launchedBounds -Name "y"
                Width = Get-OptionalObjectProperty -Value $launchedBounds -Name "width"
                Height = Get-OptionalObjectProperty -Value $launchedBounds -Name "height"
              }
            } else { $null }
          })
      }
    }
    $effectProperty = $output.PSObject.Properties["effect"]
    $unverifiable = $null -ne $effectProperty -and [string]$effectProperty.Value -eq "unverifiable"
    if ($tool -in $mutatingTools -and $null -ne $effectProperty -and
        [string]$effectProperty.Value -in @("failed", "failure", "error", "not_applied", "no_effect")) {
      throw "Mutating step '$stepId' reported effect '$($effectProperty.Value)'"
    }
    $successProperty = $output.PSObject.Properties["success"]
    if ($tool -in $mutatingTools -and $null -ne $successProperty -and
        [bool]$successProperty.Value -ne $true) {
      throw "Mutating step '$stepId' reported success=false"
    }
    if ($unverifiable) { $requiresVisualVerification = $true }

    if ($null -ne $artifactName) {
      $artifactPath = Join-Path $resolvedResultDirectory $artifactName
      if (-not (Test-Path -LiteralPath $artifactPath -PathType Leaf)) {
        throw "Step '$stepId' did not create screenshot '$artifactName'"
      }
      $file = Get-Item -LiteralPath $artifactPath
      if ($file.Length -le 0) { throw "Step '$stepId' created an empty screenshot" }
      $artifactRecords.Add([ordered]@{
          StepId = $stepId
          Name = $artifactName
          Bytes = $file.Length
          Sha256 = (Get-FileHash -LiteralPath $artifactPath -Algorithm SHA256).Hash.ToLowerInvariant()
        })
    }

    $delayProperty = $step.PSObject.Properties["delayAfterMs"]
    if ($null -ne $delayProperty) {
      $delay = [int]$delayProperty.Value
      if ($delay -lt 0 -or $delay -gt 5000) { throw "delayAfterMs for '$stepId' is out of range" }
      if ($delay -gt 0) { Start-Sleep -Milliseconds $delay }
    }
    $stepRecords.Add([ordered]@{
        Id = $stepId
        Tool = $tool
        StartedAt = $stepStartedAt.ToString("o")
        FinishedAt = [DateTimeOffset]::UtcNow.ToString("o")
        Mutating = $tool -in $mutatingTools
        Unverifiable = $unverifiable
        DriverOutputEncoding = $lastDriverOutputEncoding
        DriverOutputSha256 = $lastDriverOutputSha256
        VerifyWith = if ($null -ne $step.PSObject.Properties["verifyWith"]) { [string]$step.verifyWith } else { $null }
        OutputSummary = Get-SafeStepOutputSummary -Tool $tool -Output $output
      })
    Write-ControlProgress -Phase "step_completed" -StepId $stepId -Tool $tool
  }

  if ($assertions.Count -gt 0) {
    foreach ($assertion in $assertions) {
      $path = [string](Get-ObjectProperty -Value $assertion -Name "ref")
      $actual = Resolve-Reference -Path $path
      $equalsProperty = $assertion.PSObject.Properties["equals"]
      $existsProperty = $assertion.PSObject.Properties["exists"]
      $countEqualsProperty = $assertion.PSObject.Properties["countEquals"]
      $whereProperty = $assertion.PSObject.Properties["where"]
      if ($null -ne $whereProperty) {
        $conditions = Resolve-PlanValue -Value $whereProperty.Value
        $actual = @(@($actual) | Where-Object {
            $candidate = $_
            foreach ($condition in $conditions.Keys) {
              $candidateValue = Get-ObjectProperty -Value $candidate -Name $condition
              $expectedValue = $conditions[$condition]
              if ($candidateValue -is [string] -or $expectedValue -is [string]) {
                if (-not [string]::Equals(
                    [string]$candidateValue, [string]$expectedValue, [StringComparison]::Ordinal)) {
                  return $false
                }
              } elseif ($candidateValue -ne $expectedValue) {
                return $false
              }
            }
            return $true
          })
      }
      if ($null -ne $equalsProperty) {
        if ($actual -is [string] -or $equalsProperty.Value -is [string]) {
          if (-not [string]::Equals([string]$actual, [string]$equalsProperty.Value, [StringComparison]::Ordinal)) {
            throw "Assertion failed for '$path'"
          }
        } elseif ($actual -ne $equalsProperty.Value) {
          throw "Assertion failed for '$path'"
        }
        $assertionRecords.Add([ordered]@{ Ref = $path; Operator = "equals"; Passed = $true })
      } elseif ($null -ne $existsProperty) {
        if ([bool]$existsProperty.Value -ne ($null -ne $actual)) { throw "Existence assertion failed for '$path'" }
        $assertionRecords.Add([ordered]@{
            Ref = $path
            Operator = "exists"
            Expected = [bool]$existsProperty.Value
            Passed = $true
          })
      } elseif ($null -ne $countEqualsProperty) {
        if (@($actual).Count -ne [int]$countEqualsProperty.Value) {
          throw "Count assertion failed for '$path'"
        }
        $assertionRecords.Add([ordered]@{
            Ref = $path
            Operator = "countEquals"
            Expected = [int]$countEqualsProperty.Value
            Actual = @($actual).Count
            Filtered = $null -ne $whereProperty
            Passed = $true
          })
      } else {
        throw "Assertion for '$path' requires equals, exists, or countEquals"
      }
    }
  }
} catch {
  $capturedFailure = $_
  try {
    Write-ControlProgress -Phase "failed" -StepId $null -Tool $null
  } catch {}
} finally {
  if ($null -ne $capturedFailure -and $failureCleanupTargets.Count -gt 0) {
    foreach ($target in $failureCleanupTargets) {
      $closeMethods = [Collections.Generic.List[string]]::new()
      $cleanupErrors = [Collections.Generic.List[string]]::new()
      $remainingWindows = @()
      try {
        for ($attempt = 0; $attempt -lt 2; $attempt++) {
          $windowOutput = Invoke-DriverTool -Tool "list_windows" -InputValue ([ordered]@{
              pid = [int]$target.Pid
            })
          $remainingWindows = @(@(Get-OptionalObjectProperty -Value $windowOutput -Name "windows") |
              Where-Object {
                $candidateWindowId = [long](Get-OptionalObjectProperty -Value $_ -Name "window_id")
                if ($candidateWindowId -eq [long]$target.WindowId) { return $true }
                if ($null -eq $target.Bounds) { return $false }
                $candidateBounds = Get-OptionalObjectProperty -Value $_ -Name "bounds"
                if ($null -eq $candidateBounds) { return $false }
                $xMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "x") -eq [double]$target.Bounds.X
                $yMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "y") -eq [double]$target.Bounds.Y
                $widthMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "width") -eq [double]$target.Bounds.Width
                $heightMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "height") -eq [double]$target.Bounds.Height
                return $xMatches -and $yMatches -and $widthMatches -and $heightMatches
              })
          if ($remainingWindows.Count -eq 0) { break }

          foreach ($remainingWindow in $remainingWindows) {
            $currentWindowId = [long](Get-OptionalObjectProperty -Value $remainingWindow -Name "window_id")
            $closedWithUia = $false
            try {
              $windowState = Invoke-DriverTool -Tool "get_window_state" -InputValue ([ordered]@{
                  pid = [int]$target.Pid
                  window_id = $currentWindowId
                  include_screenshot = $false
                  max_elements = 10
                })
              $closeCandidates = @(@(Get-OptionalObjectProperty -Value $windowState -Name "elements") |
                  Where-Object {
                    $candidateRole = [string](Get-OptionalObjectProperty -Value $_ -Name "role")
                    $candidateIndex = [int](Get-OptionalObjectProperty -Value $_ -Name "element_index")
                    $candidateRole -eq "Button" -and $candidateIndex -eq 4
                  })
              if ($closeCandidates.Count -eq 1) {
                $closeToken = [string](Get-OptionalObjectProperty -Value $closeCandidates[0] -Name "element_token")
                [void](Invoke-DriverTool -Tool "click" -InputValue ([ordered]@{
                      pid = [int]$target.Pid
                      window_id = $currentWindowId
                      element_token = $closeToken
                    }))
                $closeMethods.Add("uia-titlebar")
                $closedWithUia = $true
              }
            } catch {
              $cleanupErrors.Add((ConvertTo-SafeUnicodeString -Value $_.Exception.Message))
            }
            if (-not $closedWithUia) {
              $posted = [CamStationWindowsControlNative]::PostMessage(
                [IntPtr]$currentWindowId, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero)
              $closeMethod = if ($posted) { "wm-close" } else { "wm-close-failed" }
              $closeMethods.Add($closeMethod)
            }
          }
          Start-Sleep -Milliseconds 300
        }

        $finalWindowOutput = Invoke-DriverTool -Tool "list_windows" -InputValue ([ordered]@{
            pid = [int]$target.Pid
          })
        $remainingWindows = @(@(Get-OptionalObjectProperty -Value $finalWindowOutput -Name "windows") |
            Where-Object {
              $candidateWindowId = [long](Get-OptionalObjectProperty -Value $_ -Name "window_id")
              if ($candidateWindowId -eq [long]$target.WindowId) { return $true }
              if ($null -eq $target.Bounds) { return $false }
              $candidateBounds = Get-OptionalObjectProperty -Value $_ -Name "bounds"
              if ($null -eq $candidateBounds) { return $false }
              $xMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "x") -eq [double]$target.Bounds.X
              $yMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "y") -eq [double]$target.Bounds.Y
              $widthMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "width") -eq [double]$target.Bounds.Width
              $heightMatches = [double](Get-OptionalObjectProperty -Value $candidateBounds -Name "height") -eq [double]$target.Bounds.Height
              return $xMatches -and $yMatches -and $widthMatches -and $heightMatches
            })
      } catch {
        $cleanupErrors.Add((ConvertTo-SafeUnicodeString -Value $_.Exception.Message))
      }
      $failureCleanupRecords.Add([ordered]@{
          StepId = $target.StepId
          Pid = $target.Pid
          WindowId = $target.WindowId
          CloseMethods = $closeMethods
          RemainingWindowIds = @($remainingWindows | ForEach-Object {
              [long](Get-OptionalObjectProperty -Value $_ -Name "window_id")
            })
          Passed = $remainingWindows.Count -eq 0
          Errors = $cleanupErrors
        })
    }
    if (@($failureCleanupRecords | Where-Object { -not $_.Passed }).Count -ne 0) {
      $capturedFailure = [InvalidOperationException]::new(
        "$(Get-ControlFailureMessage -Failure $capturedFailure); one or more failure cleanup windows remained")
    }
  }
  $desktopSessionEnded = $null
  if ($null -ne $managedDesktopSession) {
    try {
      [void](Invoke-DriverTool -Tool "end_session" -InputValue ([ordered]@{ session = $managedDesktopSession }))
      $desktopSessionEnded = $true
    } catch {
      $desktopSessionEnded = $false
      if ($null -eq $capturedFailure) { $capturedFailure = $_ }
    }
  }
  $completion = [ordered]@{
    SchemaVersion = 1
    Result = "WINDOWS_CONTROL_COMPLETE"
    Success = $null -eq $capturedFailure
    Error = if ($null -ne $capturedFailure) {
      ConvertTo-SafeUnicodeString -Value (Get-ControlFailureMessage -Failure $capturedFailure)
    } else { $null }
    StartedAt = $startedAt.ToString("o")
    FinishedAt = [DateTimeOffset]::UtcNow.ToString("o")
    UserSid = if ($null -ne $identity.User) { $identity.User.Value } else { $null }
    SessionId = $sessionId
    PlanSha256 = if (Test-Path -LiteralPath $resolvedPlanPath) {
      (Get-FileHash -LiteralPath $resolvedPlanPath -Algorithm SHA256).Hash.ToLowerInvariant()
    } else { $null }
    DriverPath = $DriverPath
    DriverSha256 = if (Test-Path -LiteralPath $DriverPath) {
      (Get-FileHash -LiteralPath $DriverPath -Algorithm SHA256).Hash.ToLowerInvariant()
    } else { $null }
    DriverProcessId = $verifiedDriverProcessId
    ManagedDesktopSession = $managedDesktopSession
    ManagedDesktopSessionEnded = $desktopSessionEnded
    RequiresVisualVerification = $requiresVisualVerification
    Steps = $stepRecords
    Assertions = $assertionRecords
    Artifacts = $artifactRecords
    FailureCleanup = $failureCleanupRecords
  }
  try { Write-JsonAtomic -Path $completionPath -Value $completion } catch {
    if ($null -eq $capturedFailure) { $capturedFailure = $_ }
  }
}

if ($null -ne $capturedFailure) { exit 1 }
