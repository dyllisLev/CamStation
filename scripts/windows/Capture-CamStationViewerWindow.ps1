[CmdletBinding()]
param(
  [Parameter(Mandatory)]
  [ValidateSet("LaunchAndCapture", "Capture")]
  [string]$Operation,

  [Parameter(Mandatory)]
  [string]$ResultDirectory,

  [Parameter(Mandatory)]
  [ValidatePattern("^S-1-")]
  [string]$ExpectedUserSid,

  [ValidateRange(5, 60)]
  [int]$TimeoutSeconds = 30
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$viewerPath = "C:\Program Files\CamStation Viewer\CamStationViewer.exe"
$viewerProcessName = "CamStationViewer"
$allowedEvidenceRoot = [IO.Path]::GetFullPath("C:\CamStationDev\gui-evidence").TrimEnd("\")
$resolvedResultDirectory = [IO.Path]::GetFullPath($ResultDirectory).TrimEnd("\")

function Write-JsonAtomic {
  param(
    [Parameter(Mandatory)] [string]$Path,
    [Parameter(Mandatory)] [object]$Value
  )

  $temporary = "$Path.tmp-$PID"
  $json = $Value | ConvertTo-Json -Depth 10
  [IO.File]::WriteAllText($temporary, $json, [Text.UTF8Encoding]::new($false))
  Move-Item -LiteralPath $temporary -Destination $Path -Force
}

function Get-SafeAutomationText {
  param([AllowNull()] [string]$Value)

  $text = ([string]$Value).Trim()
  if ($text.Length -gt 128) { $text = $text.Substring(0, 128) }
  $text = [regex]::Replace($text, "(?i)\bhttps?://\S+", "[redacted-url]")
  return [regex]::Replace($text, "(?<!\d)(?:\d{1,3}\.){3}\d{1,3}(?!\d)", "[redacted-ip]")
}

function Find-ViewerWindowProcess {
  param([int]$SessionId)

  $candidates = @(Get-Process -Name $viewerProcessName -ErrorAction SilentlyContinue |
      Where-Object { $_.SessionId -eq $SessionId -and $_.MainWindowHandle -ne 0 })
  if ($candidates.Count -eq 0) { return $null }
  return $candidates | Sort-Object StartTime | Select-Object -First 1
}

if (-not ($resolvedResultDirectory + "\").StartsWith($allowedEvidenceRoot + "\", [StringComparison]::OrdinalIgnoreCase)) {
  throw "ResultDirectory must be a child of $allowedEvidenceRoot"
}
if (-not (Test-Path -LiteralPath $resolvedResultDirectory -PathType Container)) {
  throw "ResultDirectory does not exist"
}

$completionPath = Join-Path $resolvedResultDirectory "complete.json"
$startedAt = [DateTimeOffset]::UtcNow
$sessionId = [Diagnostics.Process]::GetCurrentProcess().SessionId
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$launchedProcessId = 0

try {
  if ($sessionId -eq 0) { throw "GUI worker must run in a nonzero interactive session" }
  if ($null -eq $identity.User -or $identity.User.Value -ne $ExpectedUserSid) {
    throw "GUI worker identity does not match ExpectedUserSid"
  }
  if (-not (Get-Process -Name explorer -ErrorAction SilentlyContinue | Where-Object { $_.SessionId -eq $sessionId })) {
    throw "No Explorer process exists in the GUI worker session"
  }
  if (-not (Test-Path -LiteralPath $viewerPath -PathType Leaf)) {
    throw "Installed Viewer executable is missing"
  }

  Add-Type -AssemblyName System.Drawing
  Add-Type -AssemblyName UIAutomationClient
  Add-Type -AssemblyName UIAutomationTypes
  Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public static class CamStationGuiNative {
    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left; public int Top; public int Right; public int Bottom; }

    [DllImport("user32.dll")]
    public static extern bool SetProcessDPIAware();

    [DllImport("user32.dll")]
    public static extern bool GetWindowRect(IntPtr hWnd, out RECT rect);

    [DllImport("user32.dll")]
    public static extern bool IsIconic(IntPtr hWnd);

    [DllImport("user32.dll")]
    public static extern bool IsZoomed(IntPtr hWnd);

    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);

    [DllImport("user32.dll")]
    public static extern bool PrintWindow(IntPtr hWnd, IntPtr hdc, uint flags);
}
"@
  [void][CamStationGuiNative]::SetProcessDPIAware()

  $windowProcess = Find-ViewerWindowProcess -SessionId $sessionId
  if ($Operation -eq "LaunchAndCapture" -and $null -eq $windowProcess) {
    $launched = Start-Process -FilePath $viewerPath -WorkingDirectory (Split-Path -Parent $viewerPath) -PassThru
    $launchedProcessId = $launched.Id
  }

  $deadline = [DateTimeOffset]::UtcNow.AddSeconds($TimeoutSeconds)
  do {
    $windowProcess = Find-ViewerWindowProcess -SessionId $sessionId
    if ($null -ne $windowProcess) { break }
    Start-Sleep -Milliseconds 250
  } while ([DateTimeOffset]::UtcNow -lt $deadline)
  if ($null -eq $windowProcess) { throw "Viewer top-level window did not appear before timeout" }

  $processRecord = Get-CimInstance Win32_Process -Filter "ProcessId=$($windowProcess.Id)"
  if ($null -eq $processRecord -or
      -not [string]::Equals($processRecord.ExecutablePath, $viewerPath, [StringComparison]::OrdinalIgnoreCase) -or
      [int]$processRecord.SessionId -ne $sessionId) {
    throw "Resolved Viewer window does not belong to the expected executable and session"
  }

  $handle = [IntPtr]$windowProcess.MainWindowHandle
  if ([CamStationGuiNative]::IsIconic($handle)) {
    throw "Viewer window is minimized; exact capture refuses to change its placement"
  }
  $wasMaximized = [CamStationGuiNative]::IsZoomed($handle)
  [void][CamStationGuiNative]::SetForegroundWindow($handle)
  Start-Sleep -Milliseconds 900

  $rectangle = [CamStationGuiNative+RECT]::new()
  if (-not [CamStationGuiNative]::GetWindowRect($handle, [ref]$rectangle)) {
    throw "GetWindowRect failed for Viewer"
  }
  $width = $rectangle.Right - $rectangle.Left
  $height = $rectangle.Bottom - $rectangle.Top
  if ($width -lt 320 -or $height -lt 240 -or $width -gt 7680 -or $height -gt 4320) {
    throw "Viewer window dimensions are outside the accepted bounds: ${width}x${height}"
  }

  $screenshotPath = Join-Path $resolvedResultDirectory "viewer-window.png"
  $bitmap = [Drawing.Bitmap]::new($width, $height, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
  $graphics = $null
  $captureMode = "PrintWindow"
  try {
    $graphics = [Drawing.Graphics]::FromImage($bitmap)
    $deviceContext = $graphics.GetHdc()
    try {
      $printed = [CamStationGuiNative]::PrintWindow($handle, $deviceContext, 2)
    } finally {
      $graphics.ReleaseHdc($deviceContext)
    }
    $graphics.Dispose()
    $graphics = $null

    $sampleColors = [Collections.Generic.HashSet[int]]::new()
    for ($x = 0; $x -lt $width; $x += [Math]::Max(1, [int]($width / 12))) {
      for ($y = 0; $y -lt $height; $y += [Math]::Max(1, [int]($height / 12))) {
        [void]$sampleColors.Add($bitmap.GetPixel([Math]::Min($x, $width - 1), [Math]::Min($y, $height - 1)).ToArgb())
      }
    }
    if (-not $printed -or $sampleColors.Count -lt 4) {
      $captureMode = "WindowRectangleFallback"
      $graphics = [Drawing.Graphics]::FromImage($bitmap)
      $graphics.CopyFromScreen($rectangle.Left, $rectangle.Top, 0, 0, [Drawing.Size]::new($width, $height), [Drawing.CopyPixelOperation]::SourceCopy)
    }
    $bitmap.Save($screenshotPath, [Drawing.Imaging.ImageFormat]::Png)
  } finally {
    if ($graphics) { $graphics.Dispose() }
    $bitmap.Dispose()
  }

  $automationRoot = [Windows.Automation.AutomationElement]::FromHandle($handle)
  if ($null -eq $automationRoot) { throw "UI Automation could not resolve the Viewer window" }
  $elements = [Collections.Generic.List[object]]::new()
  foreach ($controlType in @(
      [Windows.Automation.ControlType]::Edit,
      [Windows.Automation.ControlType]::Button,
      [Windows.Automation.ControlType]::CheckBox)) {
    $condition = [Windows.Automation.PropertyCondition]::new(
      [Windows.Automation.AutomationElement]::ControlTypeProperty,
      $controlType)
    $matches = $automationRoot.FindAll([Windows.Automation.TreeScope]::Descendants, $condition)
    $limit = [Math]::Min($matches.Count, 64 - $elements.Count)
    for ($index = 0; $index -lt $limit; $index++) {
      $element = $matches.Item($index)
      try {
        $bounds = $element.Current.BoundingRectangle
        $elements.Add([ordered]@{
          ControlType = $element.Current.ControlType.ProgrammaticName
          Name = Get-SafeAutomationText -Value $element.Current.Name
          AutomationId = Get-SafeAutomationText -Value $element.Current.AutomationId
          ClassName = Get-SafeAutomationText -Value $element.Current.ClassName
          Enabled = [bool]$element.Current.IsEnabled
          KeyboardFocusable = [bool]$element.Current.IsKeyboardFocusable
          HasKeyboardFocus = [bool]$element.Current.HasKeyboardFocus
          Bounds = [ordered]@{
            X = [Math]::Round($bounds.X, 2)
            Y = [Math]::Round($bounds.Y, 2)
            Width = [Math]::Round($bounds.Width, 2)
            Height = [Math]::Round($bounds.Height, 2)
          }
        })
      } catch {
        # A renderer can invalidate an element while it is enumerated. Skip only that element.
      }
    }
    if ($elements.Count -ge 64) { break }
  }

  $uiaPath = Join-Path $resolvedResultDirectory "uia.json"
  $uiaReport = [ordered]@{
    SchemaVersion = 1
    WindowTitle = Get-SafeAutomationText -Value $automationRoot.Current.Name
    ElementCount = $elements.Count
    Elements = $elements
  }
  Write-JsonAtomic -Path $uiaPath -Value $uiaReport

  $viewerProcesses = @(Get-Process -Name $viewerProcessName -ErrorAction SilentlyContinue |
      Where-Object { $_.SessionId -eq $sessionId } | Select-Object -ExpandProperty Id | Sort-Object)
  $result = [ordered]@{
    SchemaVersion = 1
    Success = $true
    Operation = $Operation
    StartedAtUtc = $startedAt.ToString("o")
    FinishedAtUtc = [DateTimeOffset]::UtcNow.ToString("o")
    UserSid = $identity.User.Value
    UserName = $identity.Name
    SessionId = $sessionId
    LaunchedProcessId = $launchedProcessId
    WindowProcessId = $windowProcess.Id
    ViewerProcessIds = $viewerProcesses
    WindowTitle = Get-SafeAutomationText -Value $windowProcess.MainWindowTitle
    Window = [ordered]@{
      Left = $rectangle.Left
      Top = $rectangle.Top
      Width = $width
      Height = $height
      WasMaximized = $wasMaximized
    }
    CaptureMode = $captureMode
    Screenshot = [ordered]@{
      Filename = "viewer-window.png"
      Bytes = (Get-Item -LiteralPath $screenshotPath).Length
      Sha256 = (Get-FileHash -LiteralPath $screenshotPath -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    UIAutomation = [ordered]@{
      Filename = "uia.json"
      ElementCount = $elements.Count
      Sha256 = (Get-FileHash -LiteralPath $uiaPath -Algorithm SHA256).Hash.ToLowerInvariant()
    }
  }
  Write-JsonAtomic -Path $completionPath -Value $result
  $result | ConvertTo-Json -Depth 10 -Compress
  exit 0
} catch {
  $failure = [ordered]@{
    SchemaVersion = 1
    Success = $false
    Operation = $Operation
    StartedAtUtc = $startedAt.ToString("o")
    FinishedAtUtc = [DateTimeOffset]::UtcNow.ToString("o")
    UserSid = if ($identity.User) { $identity.User.Value } else { $null }
    UserName = $identity.Name
    SessionId = $sessionId
    ErrorType = $_.Exception.GetType().FullName
    Error = $_.Exception.Message
  }
  try { Write-JsonAtomic -Path $completionPath -Value $failure } catch {}
  $failure | ConvertTo-Json -Depth 6 -Compress
  exit 1
}
