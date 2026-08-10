[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+(?:\.\d+)?$')]
    [string]$Version,

    [ValidateSet("Release", "Debug")]
    [string]$Configuration = "Release",

    [string]$OutputDirectory,

    [switch]$UnsignedDevelopment,

    [switch]$SkipDependencyInstall,

    [switch]$KeepBuildWorkspace
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$expectedProductName = "CamStation Viewer"
$expectedUpgradeCode = "{7D4769BB-89EF-4C36-B4F2-52E33BF8BE87}"
$workspace = $null
$windowsInstaller = $null
$database = $null

function Assert-LastExitCode {
    param([Parameter(Mandatory = $true)][string]$Step)
    if ($LASTEXITCODE -ne 0) {
        throw "$Step failed with exit code $LASTEXITCODE."
    }
}

function Release-ComObject {
    param([AllowNull()][object]$Value)

    if ($null -ne $Value -and [System.Runtime.InteropServices.Marshal]::IsComObject($Value)) {
        [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($Value)
    }
}

function Get-MsiProperty {
    param(
        [Parameter(Mandatory = $true)][object]$Database,
        [Parameter(Mandatory = $true)][string]$Name
    )

    $query = "SELECT ``Value`` FROM ``Property`` WHERE ``Property``='$Name'"
    $view = $null
    $record = $null
    $view = $Database.GetType().InvokeMember(
        "OpenView",
        [System.Reflection.BindingFlags]::InvokeMethod,
        $null,
        $Database,
        @([string]$query)
    )
    try {
        $null = $view.GetType().InvokeMember(
            "Execute",
            [System.Reflection.BindingFlags]::InvokeMethod,
            $null,
            $view,
            $null
        )
        $record = $view.GetType().InvokeMember(
            "Fetch",
            [System.Reflection.BindingFlags]::InvokeMethod,
            $null,
            $view,
            $null
        )
        if ($null -eq $record) {
            throw "MSI property '$Name' is missing."
        }
        return [string]($record.GetType().InvokeMember(
            "StringData",
            [System.Reflection.BindingFlags]::GetProperty,
            $null,
            $record,
            @([int]1)
        ))
    }
    finally {
        Release-ComObject -Value $record
        if ($null -ne $view) {
            try {
                $null = $view.GetType().InvokeMember(
                    "Close",
                    [System.Reflection.BindingFlags]::InvokeMethod,
                    $null,
                    $view,
                    $null
                )
            }
            finally {
                Release-ComObject -Value $view
            }
        }
    }
}

function Get-MsiFileCount {
    param([Parameter(Mandatory = $true)][object]$Database)

    $view = $null
    $record = $null
    $view = $Database.GetType().InvokeMember(
        "OpenView",
        [System.Reflection.BindingFlags]::InvokeMethod,
        $null,
        $Database,
        @('SELECT `File` FROM `File`')
    )
    try {
        $null = $view.GetType().InvokeMember(
            "Execute",
            [System.Reflection.BindingFlags]::InvokeMethod,
            $null,
            $view,
            $null
        )
        $count = 0
        while ($true) {
            $record = $view.GetType().InvokeMember(
                "Fetch",
                [System.Reflection.BindingFlags]::InvokeMethod,
                $null,
                $view,
                $null
            )
            if ($null -eq $record) { break }
            try {
                $count++
            }
            finally {
                Release-ComObject -Value $record
                $record = $null
            }
        }
        return $count
    }
    finally {
        Release-ComObject -Value $record
        if ($null -ne $view) {
            try {
                $null = $view.GetType().InvokeMember(
                    "Close",
                    [System.Reflection.BindingFlags]::InvokeMethod,
                    $null,
                    $view,
                    $null
                )
            }
            finally {
                Release-ComObject -Value $view
            }
        }
    }
}

if ($env:OS -ne "Windows_NT") {
    throw "CamStation Viewer MSI builds require a dedicated Windows build host. Linux may run source tests only."
}
if (-not [System.Environment]::Is64BitOperatingSystem) {
    throw "CamStation Viewer MSI builds require 64-bit Windows."
}
if (-not [System.Environment]::Is64BitProcess) {
    throw "CamStation Viewer MSI builds require a 64-bit PowerShell process."
}
if (-not $UnsignedDevelopment) {
    throw "Production signing is not configured. Pass -UnsignedDevelopment for an explicitly unsigned local build."
}

$nodeCommand = Get-Command "node.exe" -CommandType Application -ErrorAction Stop
$npmCommand = Get-Command "npm.cmd" -CommandType Application -ErrorAction Stop
$goCommand = Get-Command "go.exe" -CommandType Application -ErrorAction Stop
$dotnetCommand = Get-Command "dotnet.exe" -CommandType Application -ErrorAction Stop
$gitCommand = Get-Command "git.exe" -CommandType Application -ErrorAction Stop

$env:DOTNET_NOLOGO = "1"
$env:DOTNET_CLI_TELEMETRY_OPTOUT = "1"
$env:DOTNET_SKIP_FIRST_TIME_EXPERIENCE = "1"

$nodeVersionText = (& $nodeCommand.Source --version).Trim().TrimStart("v")
Assert-LastExitCode "Node.js version check"
$nodeVersion = [version]$nodeVersionText
if ($nodeVersion.Major -lt 22) {
    throw "Node.js 22 or newer is required; found $nodeVersionText."
}

$npmVersionText = (& $npmCommand.Source --version).Trim()
Assert-LastExitCode "npm version check"

$goVersionText = (& $goCommand.Source version).Trim()
Assert-LastExitCode "Go version check"
if ($goVersionText -notmatch 'go version go(?<version>\d+\.\d+(?:\.\d+)?)') {
    throw "Unable to parse Go version: $goVersionText"
}
$goVersion = [version]$Matches.version
if ($goVersion -lt [version]"1.25") {
    throw "Go 1.25 or newer is required; found $goVersion."
}

$dotnetVersionText = (& $dotnetCommand.Source --version).Trim()
Assert-LastExitCode ".NET SDK version check"
$dotnetVersion = [version]$dotnetVersionText
if ($dotnetVersion.Major -ne 8) {
    throw ".NET SDK 8.x is required; found $dotnetVersionText."
}

$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$viewerRoot = Join-Path $repoRoot "viewer-app"
$payloadRoot = Join-Path $viewerRoot "dist\CamStationViewer-win32-x64"
$installerSource = Join-Path $repoRoot "installer"
$buildRoot = Join-Path $repoRoot "artifacts\viewer-msi"

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $resolvedOutputDirectory = Join-Path $buildRoot $Version
}
elseif ([System.IO.Path]::IsPathRooted($OutputDirectory)) {
    $resolvedOutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
}
else {
    $resolvedOutputDirectory = [System.IO.Path]::GetFullPath((Join-Path $repoRoot $OutputDirectory))
}
if (Test-Path -LiteralPath $resolvedOutputDirectory) {
    throw "Output directory already exists; choose a new directory: $resolvedOutputDirectory"
}

$protectedInputs = @(
    (Join-Path $installerSource "Files.generated.wxs"),
    (Join-Path $installerSource "CamStationViewerService.exe")
)
$protectedHashes = @{}
foreach ($protectedInput in $protectedInputs) {
    if (-not (Test-Path -LiteralPath $protectedInput -PathType Leaf)) {
        throw "Required tracked installer input is missing: $protectedInput"
    }
    $protectedHashes[$protectedInput] = (Get-FileHash -LiteralPath $protectedInput -Algorithm SHA256).Hash
}

$sourceCommit = (& $gitCommand.Source -C $repoRoot rev-parse HEAD).Trim()
Assert-LastExitCode "Git commit discovery"
$sourceDirty = @(& $gitCommand.Source -C $repoRoot status --porcelain --untracked-files=normal).Count -gt 0
Assert-LastExitCode "Git dirty-state discovery"

New-Item -ItemType Directory -Path $buildRoot -Force | Out-Null
$workspace = Join-Path $buildRoot (".work-{0}-{1}" -f $Version, [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $workspace | Out-Null

try {
    Push-Location $viewerRoot
    try {
        if (-not $SkipDependencyInstall) {
            & $npmCommand.Source ci
            Assert-LastExitCode "Viewer dependency install"
        }
        & $npmCommand.Source test
        Assert-LastExitCode "Viewer tests"
        & $npmCommand.Source run package:win
        Assert-LastExitCode "Viewer Windows packaging"
    }
    finally {
        Pop-Location
    }

    $requiredPayloadFiles = @(
        (Join-Path $payloadRoot "CamStationViewer.exe"),
        (Join-Path $payloadRoot "resources\app.asar")
    )
    foreach ($requiredPayloadFile in $requiredPayloadFiles) {
        if (-not (Test-Path -LiteralPath $requiredPayloadFile -PathType Leaf)) {
            throw "Packaged Viewer file is missing: $requiredPayloadFile"
        }
    }

    $serviceOutput = Join-Path $workspace "CamStationViewerService.exe"
    $previousGOOS = $env:GOOS
    $previousGOARCH = $env:GOARCH
    $previousCGOEnabled = $env:CGO_ENABLED
    try {
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        $env:CGO_ENABLED = "0"
        Push-Location $repoRoot
        try {
            & $goCommand.Source test ./cmd/camstation-viewer-service ./internal/viewerservice
            Assert-LastExitCode "Viewer service tests"
            & $goCommand.Source build -trimpath "-ldflags=-s -w -X camstation/internal/viewerservice.InstalledVersion=$Version" -o $serviceOutput ./cmd/camstation-viewer-service
            Assert-LastExitCode "Viewer service build"
        }
        finally {
            Pop-Location
        }
    }
    finally {
        $env:GOOS = $previousGOOS
        $env:GOARCH = $previousGOARCH
        $env:CGO_ENABLED = $previousCGOEnabled
    }
    if (-not (Test-Path -LiteralPath $serviceOutput -PathType Leaf)) {
        throw "Viewer service build did not produce an executable."
    }

    $installerWorkspace = Join-Path $workspace "installer"
    New-Item -ItemType Directory -Path $installerWorkspace | Out-Null
    foreach ($installerFile in @(
        "CamStationViewer.wixproj",
        "ProductVersion.props",
        "Package.wxs",
        "Directories.wxs",
        "Components.wxs",
        "packages.lock.json"
    )) {
        Copy-Item -LiteralPath (Join-Path $installerSource $installerFile) -Destination $installerWorkspace
    }

    $generatedFragment = Join-Path $installerWorkspace "Files.generated.wxs"
    & $nodeCommand.Source (Join-Path $repoRoot "scripts\generate-viewer-msi-files.mjs") $payloadRoot $generatedFragment $Version
    Assert-LastExitCode "WiX payload manifest generation"
    if (-not (Test-Path -LiteralPath $generatedFragment -PathType Leaf)) {
        throw "WiX payload manifest was not generated."
    }

    $wixProject = Join-Path $installerWorkspace "CamStationViewer.wixproj"
    $wixOutput = Join-Path $workspace "wix-output"
    New-Item -ItemType Directory -Path $wixOutput | Out-Null
    & $dotnetCommand.Source restore $wixProject --locked-mode
    Assert-LastExitCode "Locked WiX restore"
    & $dotnetCommand.Source build $wixProject --no-restore -c $Configuration "-p:ViewerMsiVersion=$Version" "-p:ViewerPayloadDir=$payloadRoot" "-p:ServicePayloadPath=$serviceOutput" -o $wixOutput
    Assert-LastExitCode "WiX MSI build"

    $builtMsi = Join-Path $wixOutput "CamStationViewer.msi"
    $builtSymbols = Join-Path $wixOutput "CamStationViewer.wixpdb"
    foreach ($builtFile in @($builtMsi, $builtSymbols)) {
        if (-not (Test-Path -LiteralPath $builtFile -PathType Leaf)) {
            throw "Expected WiX output is missing: $builtFile"
        }
    }

    try {
        $windowsInstaller = New-Object -ComObject WindowsInstaller.Installer
        $database = $windowsInstaller.GetType().InvokeMember(
            "OpenDatabase",
            [System.Reflection.BindingFlags]::InvokeMethod,
            $null,
            $windowsInstaller,
            @([string]$builtMsi, [int]0)
        )
    }
    catch {
        throw "MSI OpenDatabase inspection failed: $($_.Exception.Message)"
    }
    try { $productName = Get-MsiProperty -Database $database -Name "ProductName" }
    catch { throw "MSI ProductName inspection failed: $($_.Exception.Message)" }
    try { $productVersion = Get-MsiProperty -Database $database -Name "ProductVersion" }
    catch { throw "MSI ProductVersion inspection failed: $($_.Exception.Message)" }
    try { $productCode = Get-MsiProperty -Database $database -Name "ProductCode" }
    catch { throw "MSI ProductCode inspection failed: $($_.Exception.Message)" }
    try { $upgradeCode = Get-MsiProperty -Database $database -Name "UpgradeCode" }
    catch { throw "MSI UpgradeCode inspection failed: $($_.Exception.Message)" }
    try { $msiFileCount = Get-MsiFileCount -Database $database }
    catch { throw "MSI File table inspection failed: $($_.Exception.Message)" }
    if ($productName -ne $expectedProductName) {
        throw "MSI ProductName mismatch: $productName"
    }
    if ($productVersion -ne $Version) {
        throw "MSI ProductVersion mismatch: expected $Version, found $productVersion."
    }
    if (-not $upgradeCode.Equals($expectedUpgradeCode, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "MSI UpgradeCode mismatch: $upgradeCode"
    }
    $payloadFileCount = @(Get-ChildItem -LiteralPath $payloadRoot -File -Recurse).Count
    if ($msiFileCount -ne ($payloadFileCount + 1)) {
        throw "MSI File table count mismatch: expected $($payloadFileCount + 1), found $msiFileCount."
    }

    $signature = Get-AuthenticodeSignature -LiteralPath $builtMsi
    if ($signature.Status -ne [System.Management.Automation.SignatureStatus]::NotSigned) {
        throw "Unsigned development MSI unexpectedly reports signature status $($signature.Status)."
    }

    foreach ($protectedInput in $protectedInputs) {
        $currentHash = (Get-FileHash -LiteralPath $protectedInput -Algorithm SHA256).Hash
        if ($currentHash -ne $protectedHashes[$protectedInput]) {
            throw "Build modified tracked installer input: $protectedInput"
        }
    }

    $publication = Join-Path $workspace "publication"
    New-Item -ItemType Directory -Path $publication | Out-Null
    Copy-Item -LiteralPath $builtMsi -Destination (Join-Path $publication "CamStationViewer.msi")
    Copy-Item -LiteralPath $builtSymbols -Destination (Join-Path $publication "CamStationViewer.wixpdb")
    $publishedMsi = Join-Path $publication "CamStationViewer.msi"
    $msiHash = (Get-FileHash -LiteralPath $publishedMsi -Algorithm SHA256).Hash.ToLowerInvariant()
    $msiSize = (Get-Item -LiteralPath $publishedMsi).Length

    $metadata = [ordered]@{
        schemaVersion = 1
        productName = $productName
        requestedVersion = $Version
        productVersion = $productVersion
        buildConfiguration = $Configuration
        productCode = $productCode
        upgradeCode = $upgradeCode
        architecture = "x64"
        msiFileCount = $msiFileCount
        sizeBytes = [long]$msiSize
        sha256 = $msiHash
        developmentUnsigned = $true
        signatureStatus = $signature.Status.ToString()
        sourceCommit = $sourceCommit
        sourceDirty = $sourceDirty
        builtAtUtc = [DateTime]::UtcNow.ToString("o")
        toolVersions = [ordered]@{
            node = $nodeVersionText
            npm = $npmVersionText
            go = $goVersion.ToString()
            dotnetSdk = $dotnetVersionText
            wix = "6.0.2"
            powershell = $PSVersionTable.PSVersion.ToString()
        }
    }
    $utf8WithoutBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText(
        (Join-Path $publication "build-metadata.json"),
        ($metadata | ConvertTo-Json -Depth 5),
        $utf8WithoutBom
    )
    [System.IO.File]::WriteAllText(
        (Join-Path $publication "CamStationViewer.msi.sha256"),
        "$msiHash  CamStationViewer.msi`n",
        $utf8WithoutBom
    )

    $outputParent = Split-Path -Parent $resolvedOutputDirectory
    New-Item -ItemType Directory -Path $outputParent -Force | Out-Null
    Move-Item -LiteralPath $publication -Destination $resolvedOutputDirectory

    [pscustomobject]@{
        Result = "MSI_BUILT"
        Version = $productVersion
        OutputDirectory = $resolvedOutputDirectory
        MSI = Join-Path $resolvedOutputDirectory "CamStationViewer.msi"
        SizeBytes = [long]$msiSize
        SHA256 = $msiHash
        DevelopmentUnsigned = $true
    } | Format-List
}
finally {
    Release-ComObject -Value $database
    $database = $null
    Release-ComObject -Value $windowsInstaller
    $windowsInstaller = $null
    if ($null -ne $workspace -and (Test-Path -LiteralPath $workspace)) {
        if ($KeepBuildWorkspace) {
            Write-Warning "Build workspace retained at $workspace"
        }
        else {
            Remove-Item -LiteralPath $workspace -Recurse -Force
        }
    }
}
