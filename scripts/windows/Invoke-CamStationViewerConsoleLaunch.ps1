[CmdletBinding(DefaultParameterSetName = "Direct")]
param(
  [Parameter(Mandatory, ParameterSetName = "Direct")]
  [ValidatePattern("^https?://")]
  [string]$ServerUrl,

  [Parameter(Mandatory, ParameterSetName = "Direct")]
  [ValidateLength(1, 128)]
  [string]$DisplayName,

  [bool]$AutoStart = $true,

  [Parameter(Mandatory, ParameterSetName = "ConfigFile")]
  [string]$ConfigPath,

  [string]$ResultPath,

  [switch]$ConfigureOnly
)

$ErrorActionPreference = "Stop"
$pipe = $null
$reader = $null
$writer = $null
$phase = 10
$utf8 = [System.Text.UTF8Encoding]::new($false, $true)

function Complete-ConsoleLaunch([int]$Code) {
  if ($ResultPath) { Set-Content -LiteralPath $ResultPath -Value $Code -NoNewline }
  exit $Code
}

try {
  if ($PSCmdlet.ParameterSetName -eq "ConfigFile") {
    $allowedRoot = [IO.Path]::GetFullPath("C:\CamStationDev\viewer-configure-runs").TrimEnd("\")
    $resolvedConfigPath = [IO.Path]::GetFullPath($ConfigPath)
    if (-not ($resolvedConfigPath + "\").StartsWith($allowedRoot + "\", [StringComparison]::OrdinalIgnoreCase)) {
      Complete-ConsoleLaunch 11
    }
    if (-not (Test-Path -LiteralPath $resolvedConfigPath -PathType Leaf)) { Complete-ConsoleLaunch 12 }
    $configuration = [IO.File]::ReadAllText($resolvedConfigPath, $utf8) | ConvertFrom-Json
    $configurationFields = @($configuration.PSObject.Properties.Name | Sort-Object)
    if (($configurationFields -join "`n") -cne ((@("autoStart", "displayName", "schemaVersion", "serverUrl") | Sort-Object) -join "`n")) {
      throw "Viewer configuration file contains unsupported fields"
    }
    if ([int]$configuration.schemaVersion -ne 1 -or
        $configuration.serverUrl -isnot [string] -or
        $configuration.displayName -isnot [string] -or
        $configuration.autoStart -isnot [bool]) {
      Complete-ConsoleLaunch 13
    }
    $ServerUrl = [string]$configuration.serverUrl
    $DisplayName = [string]$configuration.displayName
    $AutoStart = [bool]$configuration.autoStart
  }

  if ([string]::IsNullOrWhiteSpace($ServerUrl) -or $ServerUrl -ne $ServerUrl.Trim() -or
      $ServerUrl.IndexOfAny([char[]](0..31)) -ge 0) { Complete-ConsoleLaunch 14 }
  $serverUri = $null
  if (-not [Uri]::TryCreate($ServerUrl, [UriKind]::Absolute, [ref]$serverUri) -or
      $serverUri.Scheme -notin @("http", "https") -or
      [string]::IsNullOrWhiteSpace($serverUri.Host) -or
      -not [string]::IsNullOrEmpty($serverUri.UserInfo) -or
      $serverUri.AbsolutePath -notin @("", "/") -or
      -not [string]::IsNullOrEmpty($serverUri.Query) -or
      -not [string]::IsNullOrEmpty($serverUri.Fragment)) { Complete-ConsoleLaunch 15 }
  if ($DisplayName -ne $DisplayName.Trim() -or $DisplayName.Length -lt 1 -or
      $DisplayName.Length -gt 128 -or $DisplayName.IndexOfAny([char[]](0..31)) -ge 0) {
    Complete-ConsoleLaunch 16
  }

  $pipe = [System.IO.Pipes.NamedPipeClientStream]::new(".", "CamStationViewerService", [System.IO.Pipes.PipeDirection]::InOut, [System.IO.Pipes.PipeOptions]::None)
  $phase = 20
  $pipe.Connect(10000)
  $wireUtf8 = [System.Text.UTF8Encoding]::new($false)
  $reader = [System.IO.StreamReader]::new($pipe, $wireUtf8, $false, 65537, $true)
  $writer = [System.IO.StreamWriter]::new($pipe, $wireUtf8, 65537, $true)
  $writer.AutoFlush = $true

  $request = [ordered]@{
    version = 2
    requestId = [guid]::NewGuid().ToString()
    type = "configure"
    payload = [ordered]@{
      serverUrl = $ServerUrl
      displayName = $DisplayName
      autoStart = $AutoStart
    }
  }
  $writer.WriteLine(($request | ConvertTo-Json -Compress -Depth 4))
  $phase = 30
  $responseLine = $reader.ReadLine()
  if ([string]::IsNullOrWhiteSpace($responseLine)) { Complete-ConsoleLaunch 30 }
  $response = $responseLine | ConvertFrom-Json
  if (-not $response.ok) { Complete-ConsoleLaunch 31 }
  if ($response.payload.configured -ne $true -or
      [string]$response.payload.config.serverUrl -ne $ServerUrl.TrimEnd("/") -or
      [string]$response.payload.config.displayName -ne $DisplayName -or
      [bool]$response.payload.autoStart -ne $AutoStart) { Complete-ConsoleLaunch 32 }
  if ($ConfigureOnly) { Complete-ConsoleLaunch 0 }

  $phase = 40
  $viewerPath = Join-Path $env:ProgramFiles "CamStation Viewer\\CamStationViewer.exe"
  if (-not (Test-Path -LiteralPath $viewerPath -PathType Leaf)) { Complete-ConsoleLaunch 33 }
  $viewerProcess = Start-Process -FilePath $viewerPath -PassThru
  Start-Sleep -Seconds 3
  if ($viewerProcess.HasExited) { Complete-ConsoleLaunch 34 }
  Complete-ConsoleLaunch 0
} catch {
  Complete-ConsoleLaunch (100 + $phase)
} finally {
  if ($writer) { $writer.Dispose() }
  if ($reader) { $reader.Dispose() }
  if ($pipe) { $pipe.Dispose() }
}
