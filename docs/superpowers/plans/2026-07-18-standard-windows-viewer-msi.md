# Standard Windows Viewer MSI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package the direct Viewer runtime and minimal management service as one conventional per-machine x64 MSI that installs, launches, repairs, upgrades, rolls back, and fully uninstalls through Windows Installer.

**Architecture:** A pinned SDK-style WiX project consumes a deterministic manifest of the packaged Electron directory plus the Go service executable; the automatic-update phase later adds the implemented restart helper as another stable component. MSI components own stable Program Files paths, SCM registration, common shortcuts, HKLM auto-start, the configuration key container, ProgramData directories/ACLs, repair, major upgrade, rollback, and exact uninstall cleanup. Application code does not register installation resources. Production signing occurs before package publication, and MSI behavior is tested on real Windows before the old custom installer source is deleted.

**Tech Stack:** WiX Toolset SDK 6.0.2, WiX Util/UI extensions 6.0.2, .NET SDK 8.0.x on Windows, Windows Installer 5, SignTool, Go 1.25, Electron packager.

## Dependency and Licensing Gate

WiX 6 is selected because WiX 5 security support ended on 2026-02-05 and WiX 6 remains supported until 2027-02-05. Pin exactly `WixToolset.Sdk/6.0.2`, `WixToolset.Util.wixext/6.0.2`, and `WixToolset.UI.wixext/6.0.2`; do not float to a new major version during this work.

Before distributing any MSI, the project owner must confirm and record compliance with the Open Source Maintenance Fee terms that apply to WiX v6 use. This is a release gate, not permission to substitute an unsupported WiX version silently. If the terms are not acceptable, stop this plan and revise the approved packaging design before implementation.

Primary references:

- WiX releases and support: <https://docs.firegiant.com/wix/whatsnew/releasenotes/>
- SDK-style WiX projects: <https://docs.firegiant.com/wix/using-wix/>
- WiX MSBuild integration: <https://docs.firegiant.com/wix/tools/msbuild/>
- WiX Util extension schema: <https://docs.firegiant.com/wix/schema/util/>
- WiX OSMF terms: <https://docs.firegiant.com/wix/osmf/>

## Global Constraints

- Execute this plan only after the runtime phase exit gate in `2026-07-18-standard-windows-viewer-runtime.md` passes.
- Produce exactly one user-facing artifact named `CamStationViewer.msi`; do not wrap it in a bootstrap EXE or ZIP.
- Install per-machine and x64 under `C:\Program Files\CamStation Viewer` for all users.
- `CamStationViewer.exe` is the direct target of public desktop and all-users Start Menu shortcuts.
- MSI is the sole owner of installed files, service registration, shortcuts, auto-start, registry key container, ProgramData directory creation, repair, rollback, major upgrade, and uninstall cleanup.
- The MSI must not collect server URL/display name and must not create the mutable `Configuration` registry value.
- Repair restores installer-owned resources but preserves the service-written configuration and client identity.
- Full uninstall removes the exact configuration key and exact `C:\ProgramData\CamStation\Viewer` tree, including dynamic logs and update files.
- No PowerShell, `sc.exe`, `schtasks.exe`, `reg.exe`, `icacls.exe`, or shell command may be used by the installed product for normal install/repair/uninstall.
- No scheduled task, Bootstrap, Host, version directory, `current.json`, release ZIP, or custom transaction journal may be installed.
- The normal path requires no reboot. Reboot-required outcomes fail the packaging acceptance gate.
- Production MSI and all installed EXEs are Authenticode signed. Unsigned builds require an explicit development build property and cannot be published as production.
- Never commit built MSI/EXE files, PFX files, passwords, certificates with private keys, or Windows Installer logs.

## Stable Product Identity

Use these fixed identities in WiX source:

```text
Manufacturer: CamStation
Product name: CamStation Viewer
UpgradeCode: {7D4769BB-89EF-4C36-B4F2-52E33BF8BE87}
Service name: CamStationViewerService
Registry key: HKLM\Software\CamStation\Viewer
Program Files: [ProgramFiles64Folder]\CamStation Viewer\
ProgramData: [CommonAppDataFolder]\CamStation\Viewer\
Public shortcut: CamStation Viewer.lnk
Start Menu folder: CamStation
Auto-start value: HKLM\Software\Microsoft\Windows\CurrentVersion\Run\CamStationViewer
```

Component GUIDs are generated once and then committed. Never regenerate GUIDs for unchanged component key paths. ProductCode and PackageCode change for each major-upgrade build.

## File Map

- Create `installer/CamStationViewer.wixproj`: pinned WiX SDK/extensions and build inputs.
- Create `installer/Package.wxs`: package identity, media, major upgrade, features, UI.
- Create `installer/Directories.wxs`: stable Program Files, ProgramData, Desktop, and Start Menu directory tree.
- Create `installer/Components.wxs`: service, registry, shortcuts, auto-start, ACLs, and cleanup.
- Create `installer/Files.generated.wxs`: deterministic generated Electron file components; generated but committed only if the generator is deterministic and reviewable.
- Create `installer/ProductVersion.props`: build-injected numeric MSI version contract.
- Create `installer/README.md`: Windows prerequisites and reproducible build/sign commands.
- Create `scripts/generate-viewer-msi-files.mjs`: deterministic Electron component authoring.
- Create `scripts/build-viewer-msi.ps1`: package Electron, build Go PE files, generate WiX source, build MSI.
- Create `scripts/sign-viewer-msi.ps1`: sign/verify installed EXEs and MSI.
- Create `scripts/test-viewer-msi.ps1`: bounded clean install/repair/uninstall assertions on Windows.
- Modify `viewer-app/package.json` and lock file: add deterministic MSI build entry points without adding an installer framework.
- Modify `cmd/camstation-viewer-service`: add the required narrow SYSTEM-only pre-uninstall request mode for best-effort unregister.
- Modify `viewer-app/tests/installerBuild.test.ts` and `packagePolicy.test.ts`.
- Delete after MSI smoke passes: custom installer/Bootstrap/Host commands and packages listed in Task 7.
- Modify `docs/07-implementation-status.md`.

---

### Task 1: Pin the WiX Toolchain and Generate a Deterministic File Manifest

**Files:**
- Create: `installer/CamStationViewer.wixproj`
- Create: `installer/ProductVersion.props`
- Create: `scripts/generate-viewer-msi-files.mjs`
- Create: `viewer-app/tests/msiManifest.test.ts`
- Modify: `viewer-app/package.json`

- [ ] **Step 1: Write failing deterministic-manifest tests**

Tests must create a temporary sample Electron tree and assert:

- paths are sorted case-insensitively with a stable tie-breaker;
- one file maps to one WiX `Component` and one `File` key path;
- IDs and GUIDs are stable for the same normalized relative path;
- absolute paths, `..`, symlinks/reparse points, control characters, and duplicate case-insensitive paths are rejected;
- `CamStationViewer.exe` is excluded from the generated bulk fragment because it has a hand-authored stable component;
- forbidden old runtime artifacts fail generation.

```ts
test("same relative path produces stable component identity", () => {
  const first = componentIdentity("resources/app.asar");
  const second = componentIdentity("resources/app.asar");
  assert.deepEqual(first, second);
  assert.match(first.guid, /^\{[0-9A-F-]{36}\}$/u);
});
```

- [ ] **Step 2: Run the focused test and verify RED**

```bash
cd viewer-app && npm test -- msiManifest.test.ts
```

Expected: FAIL because the manifest generator does not exist.

- [ ] **Step 3: Implement deterministic WiX fragment generation**

Generate IDs from SHA-256 of the lowercase slash-normalized relative path. Generate RFC 4122 version-5 UUID component GUIDs from the fixed namespace `{FA58E97A-3341-5A49-8D67-2132A7E6E99A}` and the same normalized path. XML-escape every value. Fail closed on any forbidden artifact from the runtime plan.

The output fragment uses `SourceDir=$(var.ViewerPayloadDir)` and never embeds a developer machine's absolute path.

- [ ] **Step 4: Create the pinned SDK project**

`CamStationViewer.wixproj` contains:

```xml
<Project Sdk="WixToolset.Sdk/6.0.2">
  <PropertyGroup>
    <Platform>x64</Platform>
    <OutputName>CamStationViewer</OutputName>
    <OutputType>Package</OutputType>
    <SuppressValidation>false</SuppressValidation>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="WixToolset.Util.wixext" Version="6.0.2" />
    <PackageReference Include="WixToolset.UI.wixext" Version="6.0.2" />
  </ItemGroup>
</Project>
```

Lock NuGet restore through `packages.lock.json` and CI `--locked-mode`. Set MSI version only through validated numeric `ViewerMsiVersion`; reject missing version or prerelease text rather than silently using `1.0.0`.

- [ ] **Step 5: Verify generator and restore**

On Linux, run the Node generator tests. On the Windows build host, run:

```powershell
dotnet restore .\installer\CamStationViewer.wixproj --locked-mode
```

Expected: deterministic source generation passes and locked restore exits 0 without changing `packages.lock.json`.

- [ ] **Step 6: Commit Task 1**

```bash
git add installer/CamStationViewer.wixproj installer/ProductVersion.props installer/packages.lock.json scripts/generate-viewer-msi-files.mjs viewer-app/tests/msiManifest.test.ts viewer-app/package.json viewer-app/package-lock.json
git commit -m "build(viewer): pin WiX MSI toolchain"
```

### Task 2: Author Stable Files, Service, Direct Shortcuts, and Auto-Start

**Files:**
- Create: `installer/Package.wxs`
- Create: `installer/Directories.wxs`
- Create: `installer/Components.wxs`
- Generate: `installer/Files.generated.wxs`
- Modify: `scripts/build-viewer-msi.ps1`
- Test: `viewer-app/tests/installerBuild.test.ts`

- [ ] **Step 1: Write failing package-source assertions**

Extend `installerBuild.test.ts` to parse WiX XML and require:

- `Scope="perMachine"`, x64 package, fixed UpgradeCode, embedded cabinet, visible ARP entry;
- `MajorUpgrade` rejects downgrades and schedules removal after successful install initialization;
- service starts automatically as LocalSystem and is stopped/removed by MSI;
- recovery restarts the service after first/second/third unexpected failure and resets after one day;
- desktop and Start Menu shortcuts target `CamStationViewer.exe` directly;
- HKLM Run value is the quoted stable path plus `--autostart`;
- no server address, display name, scheduled task, PowerShell, EXE custom installer, or versioned install directory appears.

- [ ] **Step 2: Run the policy tests and verify RED**

```bash
cd viewer-app && npm test -- installerBuild.test.ts packagePolicy.test.ts
```

Expected: FAIL because the WiX package source does not exist.

- [ ] **Step 3: Author the package and stable file components**

`Package.wxs` uses a versioned ProductCode (`Id="*"`), fixed UpgradeCode, per-machine x64 scope, compressed media, `MajorUpgrade` with downgrade error, and `WixUI_Minimal`. Do not expose INSTALLDIR selection; the approved path is fixed.

Hand-author stable components for:

```text
CamStationViewer.exe
CamStationViewerService.exe
```

The Electron resource tree comes from `Files.generated.wxs`. Build fails if any manifest source is absent or changed after generation. `CamStationViewerRestart.exe` is added as a stable component by the automatic-update plan when its behavior is implemented and tested; this phase does not package a stub helper.

- [ ] **Step 4: Author service registration and recovery**

Use WiX `ServiceInstall`, `ServiceControl`, and `ServiceConfig` entries. Required behavior:

```text
Name/DisplayName: CamStationViewerService / CamStation Viewer Service
Account: LocalSystem
Type: own process
Start: auto
Error control: normal
Start on install: yes
Stop on uninstall/upgrade: yes
Remove on uninstall: yes
Unexpected failure 1/2/3: restart after 60 seconds
Reset period: 1 day
```

No application command registers or repairs SCM state.

- [ ] **Step 5: Author direct shortcuts and login auto-start**

Create all-users Start Menu and public desktop shortcuts with target `[#CamStationViewerExe]`, working directory `INSTALLFOLDER`, and the Viewer icon. Author this exact HKLM Run value as an MSI component:

```text
"[INSTALLFOLDER]CamStationViewer.exe" --autostart
```

The service-owned `autoStart` boolean controls whether that invocation opens a window; settings never delete the MSI registry value.

- [ ] **Step 6: Verify source policy and build on Windows**

```bash
cd viewer-app && npm test -- installerBuild.test.ts packagePolicy.test.ts
```

```powershell
.\scripts\build-viewer-msi.ps1 -Version 2.0.0 -Configuration Release -UnsignedDevelopment
```

Expected: tests PASS and `artifacts\viewer-msi\CamStationViewer.msi` is produced as an explicitly unsigned development package.

- [ ] **Step 7: Commit Task 2**

```bash
git add installer/Package.wxs installer/Directories.wxs installer/Components.wxs installer/Files.generated.wxs scripts/build-viewer-msi.ps1 viewer-app/tests/installerBuild.test.ts viewer-app/tests/packagePolicy.test.ts
git commit -m "feat(viewer): package direct runtime as MSI"
```

### Task 3: Preserve Configuration on Repair and Remove It on Full Uninstall

**Files:**
- Modify: `installer/Components.wxs`
- Modify: `installer/Package.wxs`
- Modify: `cmd/camstation-viewer-service/main.go`
- Modify: `cmd/camstation-viewer-service/main_test.go`
- Create: `scripts/test-viewer-msi.ps1`
- Test: `viewer-app/tests/installerBuild.test.ts`

- [ ] **Step 1: Write failing ownership tests**

Require WiX source to:

- create `HKLM\Software\CamStation\Viewer` with SYSTEM/Administrators full control and authenticated users read-only;
- author only a stable installer marker; never author `Configuration`;
- remove the exact Viewer key on uninstall;
- create ProgramData `Logs` and `Updates` with explicit ACLs;
- preserve both roots on repair/major upgrade;
- recursively remove only `[CommonAppDataFolder]CamStation\Viewer` during a full user uninstall, never during a major upgrade;
- never target `[CommonAppDataFolder]CamStation` or a variable/unresolved parent for recursive removal.

- [ ] **Step 2: Run policy tests and verify RED**

```bash
cd viewer-app && npm test -- installerBuild.test.ts
```

Expected: FAIL until registry/ProgramData ownership rules exist.

- [ ] **Step 3: Author registry and ProgramData ownership**

Use WiX/MSI permission tables through the Util extension; do not shell out to ACL tools. Required effective ACLs:

```text
HKLM\Software\CamStation\Viewer:
  SYSTEM, Administrators: Full Control
  Authenticated Users: Read

ProgramData\CamStation\Viewer\Updates:
  SYSTEM, Administrators: Full Control
  interactive users: no write

ProgramData\CamStation\Viewer\Logs:
  SYSTEM, Administrators: Full Control
  Authenticated Users: traverse/read attributes only
```

The service creates each per-lease Viewer log file and applies a file-specific ACL for the verified user SID as defined in the runtime plan. MSI must not grant directory-wide create, delete, overwrite, or modify access to interactive users.

Use a fixed-path `util:RemoveFolderEx` conditioned strictly on `REMOVE="ALL" AND NOT UPGRADINGPRODUCTCODE`. The property feeding recursive cleanup must be initialized from the fixed CommonAppData path and validated in source tests. Put the exact `RemoveRegistryKey` cleanup in a dedicated component with the same full-uninstall-only condition so removal of the old product during a major upgrade cannot erase configuration, identity, broker state, logs, or update results.

- [ ] **Step 4: Add best-effort unregister before service stop**

Add a narrow `CamStationViewerService.exe --prepare-uninstall` mode that connects to the running service locally and requests a bounded best-effort server unregister. It is accepted only from LocalSystem/Administrators, never changes install state, and returns success when the server is unavailable.

Invoke it before `StopServices` through a signed, deferred, no-impersonate WiX custom action conditioned on `REMOVE="ALL" AND NOT UPGRADINGPRODUCTCODE`. This is the only installation custom action allowed in this phase. It must be hidden from normal UI, have a 10-second application-level timeout, and never block uninstall. MSI still owns all resource deletion.

- [ ] **Step 5: Implement the Windows smoke assertion script**

`test-viewer-msi.ps1` takes an explicit absolute MSI path and explicit scratch directory. It records commands and exit codes but never stores credentials. It must:

1. reject an elevated shell with no active interactive user for launch checks;
2. install with `/l*v` and require exit 0;
3. assert product registration, files, service running/automatic, direct shortcut targets, Run value, directories, ACLs, and zero scheduled tasks named `CamStationViewer*`;
4. seed a valid service configuration through IPC;
5. run `msiexec /fa` and require config/client ID unchanged;
6. uninstall with `/x` and require exit 0;
7. assert exact absence of product, service, shortcuts, Run value, application key, Program Files root, ProgramData Viewer root, processes, and scheduled tasks.

The script must not delete anything outside the fixed owned paths.

- [ ] **Step 6: Verify repair/uninstall on a disposable Windows snapshot**

```powershell
.\scripts\test-viewer-msi.ps1 -MsiPath C:\build\CamStationViewer.msi -ScratchDirectory C:\CamStationMsiTest
```

Expected: install, repair, and uninstall exit 0; identity survives repair and is absent after uninstall; no reboot is requested.

- [ ] **Step 7: Commit Task 3**

```bash
git add installer/Package.wxs installer/Components.wxs cmd/camstation-viewer-service scripts/test-viewer-msi.ps1 viewer-app/tests/installerBuild.test.ts
git commit -m "feat(viewer): add MSI repair and full uninstall ownership"
```

### Task 4: Add Normal-Token Finish Launch and Restart Manager Cooperation

**Files:**
- Modify: `installer/Package.wxs`
- Modify: `viewer-app/src/main.ts`
- Modify: `viewer-app/src/viewerLifecycle.ts`
- Modify: `viewer-app/tests/mainLifecycle.test.ts`
- Modify: `scripts/test-viewer-msi.ps1`

- [ ] **Step 1: Write failing launch/lifecycle tests**

Prove the finish checkbox defaults on, launches the stable Viewer path with no elevated token, and is absent in quiet mode. Prove Electron handles Windows shutdown/session-end and the MSI close request by releasing the lease and exiting normally.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
cd viewer-app && npm test -- mainLifecycle.test.ts installerBuild.test.ts
```

Expected: FAIL until finish-launch and close behavior are authored.

- [ ] **Step 3: Author an impersonated finish-dialog launch**

Use WiX UI/Util's impersonated shell execution pattern from the official schema. `WixShellExecTarget` is `[#CamStationViewerExe]`; launch only after a successful full-UI clean install when the checkbox is selected, with conditions excluding `Installed` and `WIX_UPGRADE_DETECTED`. Do not run from a deferred LocalSystem custom action. Do not launch after repair, uninstall, quiet install, or major upgrade.

- [ ] **Step 4: Cooperate with Windows Installer/Restart Manager**

Electron must release its lease and quit on the normal close/session-end path. Let Restart Manager detect the file-in-use process and request normal closure. Do not add broad taskkill/TerminateProcess behavior. If the Viewer refuses to close within the standard MSI window, MSI presents the normal FilesInUse/Restart Manager handling and the test fails rather than hiding it.

- [ ] **Step 5: Verify user token and no-console behavior on Windows**

The smoke script records the launched process owner SID, session ID, and integrity level. They must match the installing shell user, not LocalSystem or the elevated administrator token. Confirm no console, PowerShell, or scheduled-task window appears.

- [ ] **Step 6: Commit Task 4**

```bash
git add installer/Package.wxs viewer-app/src/main.ts viewer-app/src/viewerLifecycle.ts viewer-app/tests/mainLifecycle.test.ts scripts/test-viewer-msi.ps1
git commit -m "feat(viewer): launch MSI-installed app in user session"
```

### Task 5: Sign and Verify the Production Artifact

**Files:**
- Create: `scripts/sign-viewer-msi.ps1`
- Modify: `scripts/build-viewer-msi.ps1`
- Modify: `installer/README.md`
- Modify: `viewer-app/tests/installerBuild.test.ts`

- [ ] **Step 1: Write failing build-policy tests**

Require production builds to fail when signing certificate thumbprint or timestamp URL is absent. Require unsigned builds to pass only with the explicit `-UnsignedDevelopment` switch and to write `developmentUnsigned: true` into build metadata. Reject deriving unsigned policy from version text or environment type.

- [ ] **Step 2: Run tests and verify RED**

```bash
cd viewer-app && npm test -- installerBuild.test.ts
```

Expected: FAIL until signing policy is implemented.

- [ ] **Step 3: Implement signing order and verification**

Production build order:

1. build Go PEs and Electron package;
2. sign every installed EXE with SHA-256 and RFC 3161 timestamp;
3. verify every EXE with `signtool verify /pa /all /tw`;
4. generate and build MSI from those exact files;
5. sign MSI;
6. verify MSI with `signtool verify /pa /all /tw`;
7. calculate exact MSI size and lowercase SHA-256 for publication.

Accept certificate selection only by explicit thumbprint from the LocalMachine/CurrentUser certificate store. Never accept a PFX password argument or write private material to disk.

- [ ] **Step 4: Verify production and development policies on Windows**

```powershell
.\scripts\build-viewer-msi.ps1 -Version 2.0.0 -Configuration Release -SigningCertificateThumbprint $env:CAMSTATION_SIGNING_CERT_THUMBPRINT -TimestampUrl $env:CAMSTATION_TIMESTAMP_URL
Get-AuthenticodeSignature .\artifacts\viewer-msi\CamStationViewer.msi | Format-List Status,SignerCertificate
```

Expected: production build is valid and timestamped. A production build with either variable absent exits nonzero before publication. An explicit isolated development build succeeds but is labeled unsigned.

- [ ] **Step 5: Commit Task 5**

```bash
git add scripts/build-viewer-msi.ps1 scripts/sign-viewer-msi.ps1 installer/README.md viewer-app/tests/installerBuild.test.ts
git commit -m "build(viewer): sign and verify MSI releases"
```

### Task 6: Prove Clean Install, Major Upgrade, Repair, and Full Uninstall

**Files:**
- Modify: `scripts/test-viewer-msi.ps1`
- Create: `docs/test-evidence/windows-viewer-msi/.gitkeep`
- Modify: `docs/07-implementation-status.md`

- [ ] **Step 1: Extend smoke assertions to two MSI versions**

Test version A then version B with the same UpgradeCode and different ProductCodes. Require:

- clean install A succeeds;
- Viewer launches directly and first-run form appears;
- configuration creates one client ID;
- major upgrade B succeeds and reports exactly B;
- configuration/client ID survive;
- only B remains registered in ARP;
- repair B restores a deliberately removed shortcut and executable while preserving config;
- uninstall B removes every owned resource and requests no reboot.

- [ ] **Step 2: Build two development-signed or isolated unsigned packages**

```powershell
.\scripts\build-viewer-msi.ps1 -Version 2.0.0 -OutputDirectory C:\build\viewer-a -UnsignedDevelopment
.\scripts\build-viewer-msi.ps1 -Version 2.0.1 -OutputDirectory C:\build\viewer-b -UnsignedDevelopment
```

Expected: ProductCodes differ, UpgradeCode matches, file versions match requested versions.

- [ ] **Step 3: Run the phase gate on Windows 10 and Windows 11**

```powershell
.\scripts\test-viewer-msi.ps1 -MsiPath C:\build\viewer-a\CamStationViewer.msi -UpgradeMsiPath C:\build\viewer-b\CamStationViewer.msi -ScratchDirectory C:\CamStationMsiTest
```

Expected: all assertions PASS on both target versions. Preserve redacted MSI log paths and assertion summary; do not commit raw machine logs.

- [ ] **Step 4: Update implementation status without overclaiming**

Mark MSI install/repair/major-upgrade/uninstall and direct shortcuts as implemented and smoke-tested. State that server publication, immediate automatic update, failure injection, two-hour soak, and ten-cycle acceptance remain pending.

- [ ] **Step 5: Commit Task 6**

```bash
git add scripts/test-viewer-msi.ps1 docs/test-evidence/windows-viewer-msi/.gitkeep docs/07-implementation-status.md
git commit -m "test(viewer): gate standard MSI lifecycle on Windows"
```

### Task 7: Delete the Rejected Custom Installer and Supervision Chain

**Files:**
- Delete: `cmd/camstation-viewer-installer/`
- Delete: `cmd/camstation-viewer-agent/`
- Delete: `cmd/camstation-viewer-bootstrap/`
- Delete: `cmd/camstation-viewer-host/`
- Delete: `internal/viewerinstall/`
- Delete: `internal/viewerbootstrap/`
- Delete: obsolete supervision/state/custom-update files under `internal/vieweragent/`, then delete the package if no neutral code remains.
- Delete: `viewer-app/scripts/build-installer.mjs`
- Modify: `viewer-app/package.json`, `package-lock.json`, tests, `Makefile`, and documentation references.

- [ ] **Step 1: Prove the replacement no longer imports old packages**

```bash
rg -n 'viewerinstall|viewerbootstrap|CamStationViewerAgent|CamStationViewerBootstrap|CamStationViewerHost|CamStationViewerRecovery|current\.json|release\.zip|schtasks' cmd/camstation-viewer-* internal/viewerinstall internal/viewerbootstrap internal/vieweragent viewer-app Makefile
```

Expected: only obsolete client source/tests remain; no replacement client build dependency exists. The server's existing EXE release endpoint and publisher are converted to MSI in Task 1 of the immediately following automatic-update plan, so they are outside this client-runtime deletion search and must not be used to publish a new release in the interim.

- [ ] **Step 2: Delete obsolete code in one bounded change**

Delete only the listed installer/runtime packages. If reusable HTTP control or update verification was moved to `viewerservice`/`viewercontrol`, verify the new package owns it before deletion. Do not delete server Viewer APIs, database records, live UI, or release catalog.

- [ ] **Step 3: Replace old artifact tests with forbidden-artifact tests**

The build suite must fail if any future change reintroduces `CamStationViewerSetup.exe`, embedded release ZIP, scheduled tasks, indirect shortcuts, Bootstrap/Host, or Program Files version directories.

- [ ] **Step 4: Run the full phase verification**

```bash
go test ./... -count=1
cd viewer-app && npm test && npm run build && npm run package:win
git diff --check
rg -n 'CamStationViewerBootstrap|CamStationViewerHost|CamStationViewerRecovery|schtasks' cmd/camstation-viewer-* internal viewer-app Makefile
```

Expected: all tests/builds PASS and final client-runtime search returns no active implementation reference. `CamStationViewerSetup.exe` may remain only in the server release endpoint/publisher tests until the next plan changes that contract to `CamStationViewer.msi`; the evidence/status must name this pending transition.

- [ ] **Step 5: Commit Task 7**

```bash
git add -A cmd/camstation-viewer-* internal/viewerinstall internal/viewerbootstrap internal/vieweragent viewer-app scripts Makefile docs
git commit -m "refactor(viewer): remove custom Windows installer chain"
```

## MSI Phase Exit Gate

Do not begin automatic update work until:

- WiX licensing/toolchain gate is recorded;
- signed production-policy build and explicit unsigned-development build behave as specified;
- Windows 10 and Windows 11 clean install, direct launch, repair, major upgrade, and uninstall pass;
- public desktop and Start Menu shortcuts target the stable Viewer EXE directly;
- HKLM Run auto-start is MSI-owned and setting disable causes quiet Viewer exit;
- config/client ID survive repair and major upgrade but disappear on uninstall;
- no reboot, scheduled task, PowerShell window, custom installer EXE, version directory, or orphaned resource remains;
- old installer/supervision source has been deleted only after replacement evidence exists.
