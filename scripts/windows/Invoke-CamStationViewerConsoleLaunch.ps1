[CmdletBinding()]
param(
  [Parameter(Mandatory)]
  [ValidatePattern("^https?://")]
  [string]$ServerUrl,

  [Parameter(Mandatory)]
  [ValidateLength(1, 128)]
  [string]$DisplayName,

  [string]$ResultPath
)

$ErrorActionPreference = "Stop"
$pipe = $null
$reader = $null
$writer = $null
$phase = 10

function Complete-ConsoleLaunch([int]$Code) {
  if ($ResultPath) { Set-Content -LiteralPath $ResultPath -Value $Code -NoNewline }
  exit $Code
}

try {
  $pipe = [System.IO.Pipes.NamedPipeClientStream]::new(".", "CamStationViewerService", [System.IO.Pipes.PipeDirection]::InOut, [System.IO.Pipes.PipeOptions]::None)
  $phase = 20
  $pipe.Connect(10000)
  $utf8 = [System.Text.UTF8Encoding]::new($false)
  $reader = [System.IO.StreamReader]::new($pipe, $utf8, $false, 65537, $true)
  $writer = [System.IO.StreamWriter]::new($pipe, $utf8, 65537, $true)
  $writer.AutoFlush = $true

  $request = [ordered]@{
    version = 2
    requestId = [guid]::NewGuid().ToString()
    type = "configure"
    payload = [ordered]@{
      serverUrl = $ServerUrl
      displayName = $DisplayName
      autoStart = $true
    }
  }
  $writer.WriteLine(($request | ConvertTo-Json -Compress -Depth 4))
  $phase = 30
  $responseLine = $reader.ReadLine()
  if ([string]::IsNullOrWhiteSpace($responseLine)) { Complete-ConsoleLaunch 30 }
  $response = $responseLine | ConvertFrom-Json
  if (-not $response.ok) { Complete-ConsoleLaunch 31 }

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
