# Viewer MSI local build specification

> Warning: WiX 6.0.2 must run on Windows. The monitoring NUC is an installation target, not a build
> host. The current Linux workspace may validate source and package policy but cannot certify an MSI.

## Goal

Provide one reproducible command that a dedicated x64 Windows developer machine or VM can run from
a CamStation checkout to produce an inspectable Viewer MSI. The command packages the current local
source, so an intentionally dirty checkout is allowed and disclosed in build metadata.

The public development-build interface is:

```powershell
pwsh -NoProfile -File .\scripts\build-viewer-msi.ps1 `
  -Version 2.0.21 `
  -UnsignedDevelopment
```

## Build contract

| Stage | Input | Required output or gate |
|---|---|---|
| Preflight | Windows x64, Node, npm, Go, .NET SDK | Node 22+, Go 1.25+, .NET SDK 8.x; fail before publication otherwise |
| Viewer | `viewer-app` and lock file | Windows x64 Electron payload with package-policy checks passing |
| Service | Go source | `CamStationViewerService.exe` with `InstalledVersion` set to the requested MSI version |
| Manifest | Packaged Viewer tree | Fresh `Files.generated.wxs` in an ignored build workspace |
| MSI | Copied WiX source, payload and service | Locked NuGet restore and validated `CamStationViewer.msi` |
| Inspection | MSI database | Exact product name, requested product version and fixed UpgradeCode |
| Publication | Inspected MSI | MSI, WiX symbols and secret-free SHA-256 build metadata |

The script must never overwrite `installer/Files.generated.wxs` or
`installer/CamStationViewerService.exe`. Temporary build paths stay under
`artifacts/viewer-msi/.work-*`; release output stays under the caller-selected output directory or
`artifacts/viewer-msi/<version>`.

## Trust and failure boundaries

- `-UnsignedDevelopment` is mandatory until a production signing identity and reviewed signing
  workflow are configured. A missing switch fails closed.
- The script never installs the MSI, invokes `msiexec`, contacts the NUC, reads Viewer configuration,
  or handles a certificate private key.
- Windows Installer database inspection happens before publication. A mismatched product name,
  version, or UpgradeCode fails the build.
- Build metadata contains only version, source commit, dirty-state boolean, artifact size/hash,
  signature policy, tool versions, and UTC build time. It contains no absolute paths or credentials.
- Cleanup is limited to the exact build workspace created by the current invocation. Published
  artifacts are never deleted automatically.

## Verification

Linux verification covers TypeScript tests/builds, Electron Windows packaging, manifest generation,
static build-policy tests, and PowerShell parsing. Completion still requires running the quick-start
command on a dedicated Windows machine and inspecting the emitted MSI. Installation, repair,
upgrade, and uninstall tests are separate release gates and must run on disposable Windows test
machines before the artifact is called production-ready.
