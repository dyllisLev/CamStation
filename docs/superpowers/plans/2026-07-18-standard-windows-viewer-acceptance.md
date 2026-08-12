# Standard Windows Viewer Real-Windows Acceptance Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to execute this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove on real Windows that the redesigned CamStation Viewer behaves like a conventional desktop application across installation, direct launch, configuration, sessions, service/server failure, repair, update, rollback, stability, and complete uninstall.

**Architecture:** A bounded PowerShell acceptance harness gathers machine-readable evidence from an explicit scratch root while an interactive local/RDP operator performs UI assertions that cannot be proven from SSH. SSH may stage artifacts and inspect files/services/registry/logs, but it is never accepted as proof that the Viewer launched in the active user's desktop or that UAC/first-run/update UI behaved correctly. Each scenario starts from a declared machine snapshot and ends with exact absence/preservation assertions.

**Tech Stack:** Windows 10/11 x64, Windows Installer logging, PowerShell 5.1+, local/RDP desktop sessions, optional restricted OpenSSH for evidence collection, CamStation non-production server, signed MSI fixtures.

## Global Constraints

- Execute only after runtime, MSI, and automatic-update phase gates pass.
- Do not claim installed, fixed, working, or complete from Linux tests, cross-compilation, source review, or SSH-only process inspection.
- Run UI scenarios from the active `console` or `rdp-tcp#...` interactive user session. Reject Session 0, service, SSH-only, and disconnected sessions for UI evidence.
- SSH is allowed for artifact transfer, MSI log collection, service/registry/file inspection, and server-side correlation only.
- Use a non-production CamStation server and non-production cameras/streams that may be interrupted safely.
- Never expose passwords, private keys, camera URLs, authorization headers, update tokens, raw server bodies, or signing private material in evidence.
- Use explicit absolute artifact and scratch paths. Never recursively delete a drive root, user profile, `C:\ProgramData\CamStation`, or another broad parent.
- Legacy dev cleanup is allowed only for the exact rejected Viewer artifacts listed in Task 2 and only after a read-only inventory is saved.
- Restore each VM from a known snapshot between localization/OS clean-install rows. The existing physical Windows test PC is supplementary, not the only clean-environment proof.
- Production acceptance uses a production-policy signed MSI. Isolated unsigned packages are limited to explicit negative/development-policy tests.
- Any reboot request, orphaned process/resource, raw secret in logs, duplicate update, or unexplained MSI nonzero code fails the release gate.
- Store raw test output outside Git. Commit only a redacted evidence index with hashes, timestamps, scenario result, and raw-evidence location controlled by the operator.
- Explain Windows/runtime timestamps in KST in the final evidence index.

## Required Environment Matrix

| Matrix ID | Windows | Language | Session | Install user | Server condition |
|---|---|---|---|---|---|
| W10-KO-L | Windows 10 x64 | Korean | local console | standard user + UAC admin approval | available/unavailable/restarted |
| W10-EN-R | Windows 10 x64 | English | RDP | standard user + UAC admin approval | available |
| W11-KO-R | Windows 11 x64 | Korean | RDP | standard user + UAC admin approval | available/unavailable |
| W11-EN-L | Windows 11 x64 | English | local console | standard user + UAC admin approval | available/restarted |

The current PC at `10.0.0.3` may cover one matching row after its OS/language/session are recorded. Its SSH access from `10.0.0.29` may collect system evidence, but GUI checks must still run in `dyllislev`'s active console/RDP session. Do not send or reconstruct private keys through chat.

## Pass/Fail Evidence Schema

Every scenario writes a redacted `summary.json`:

```json
{
  "schemaVersion": 1,
  "scenario": "clean-install-first-run",
  "matrixId": "W10-KO-L",
  "startedAtUtc": "2026-07-18T00:00:00Z",
  "finishedAtUtc": "2026-07-18T00:05:00Z",
  "operatorSession": { "userSid": "S-1-5-21-...", "sessionId": 1, "protocol": "console" },
  "msi": { "sha256": "...", "productVersion": "2.0.0", "signerSpkiSha256": "..." },
  "assertions": [{ "id": "shortcut.direct-target", "status": "pass", "evidence": "shortcut.json" }],
  "result": "pass"
}
```

Evidence files contain normalized paths, SIDs, session IDs, versions, exit codes, state transitions, shortcut targets, ACL summaries, and correlation IDs. Redact hostname/user names if the operator requires; preserve SID/session distinction needed for proof.

## File Map

- Create `scripts/windows/Invoke-CamStationViewerAcceptance.ps1`: scenario orchestrator and evidence writer.
- Create `scripts/windows/Get-CamStationViewerInventory.ps1`: read-only exact resource inventory.
- Create `scripts/windows/Remove-LegacyCamStationViewerDev.ps1`: opt-in exact legacy cleanup.
- Create `scripts/windows/Test-CamStationViewerInstallState.ps1`: files/service/registry/shortcut/product/task/process assertions.
- Create `scripts/windows/Test-CamStationViewerSession.ps1`: interactive user/session/process ownership assertions.
- Create `scripts/windows/Test-CamStationViewerLogs.ps1`: bounded secret/error scan.
- Create `scripts/windows/README.md`: local/RDP versus SSH instructions and safe invocation.
- Create `scripts/windows/tests/AcceptanceScripts.Tests.ps1`: Pester/unit policy tests for exact targets and redaction.
- Create `docs/test-evidence/windows-viewer-acceptance/README.md`: redacted index template and release-gate table.
- Modify `docs/04-cctv2-test-plan.md`: add Viewer acceptance routing without disturbing legacy `cctv`.
- Modify `docs/07-implementation-status.md` only after evidence exists.

---

### Task 1: Build a Safe, Redacted Acceptance Harness

**Files:**
- Create: `scripts/windows/Invoke-CamStationViewerAcceptance.ps1`
- Create: `scripts/windows/Get-CamStationViewerInventory.ps1`
- Create: `scripts/windows/Test-CamStationViewerInstallState.ps1`
- Create: `scripts/windows/Test-CamStationViewerSession.ps1`
- Create: `scripts/windows/Test-CamStationViewerLogs.ps1`
- Create: `scripts/windows/tests/AcceptanceScripts.Tests.ps1`
- Create: `scripts/windows/README.md`

- [ ] **Step 1: Write failing script-policy tests**

Tests must prove:

- scratch root must be an explicit absolute child below an operator-selected test root and cannot be `C:\`, a profile root, Program Files, ProgramData, or CamStation parent;
- cleanup functions accept only exact Viewer paths/keys/services/tasks/shortcut names;
- UI scenarios reject `$env:SESSIONNAME` beginning with `SSH`, empty/disconnected sessions, and service identity;
- evidence writer redacts URL userinfo, query strings, bearer/basic auth, camera RTSP URLs, tokens, PEM/private-key blocks, and raw response bodies;
- scripts do not call `taskkill`, broad `Stop-Process`, wildcard recursive deletion, `Win32_Product`, or arbitrary WMI repair;
- all MSI invocations enable verbose logging and capture exit code/reboot state.

```powershell
It 'rejects broad cleanup roots' {
  { Assert-SafeScratchRoot 'C:\' } | Should -Throw
  { Assert-SafeScratchRoot 'C:\ProgramData\CamStation' } | Should -Throw
}

It 'does not accept an SSH session as GUI proof' {
  { Assert-InteractiveViewerSession -SessionName 'SSH-Server' -SessionId 0 } | Should -Throw
}
```

- [ ] **Step 2: Run tests and verify RED**

On Windows:

```powershell
Invoke-Pester .\scripts\windows\tests\AcceptanceScripts.Tests.ps1 -Output Detailed
```

Expected: FAIL because the harness scripts do not exist.

- [ ] **Step 3: Implement read-only inventory**

Inventory these exact resources without changing them:

```text
Windows product registration matching UpgradeCode/Product name
C:\Program Files\CamStation Viewer
C:\ProgramData\CamStation\Viewer
CamStationViewerService SCM entry/config/status/recovery
HKLM\Software\CamStation\Viewer
HKLM ...\Run\CamStationViewer
Public Desktop\CamStation Viewer.lnk
All Users Start Menu\CamStation\CamStation Viewer.lnk
scheduled tasks exactly CamStationViewer and CamStationViewerRecovery plus CamStationViewer* detection
processes whose executable path is under the exact install root
active console/RDP users and process session/owner/integrity
```

Resolve `.lnk` target/arguments/working directory through Windows Shell COM in read-only mode. Do not infer direct launch from shortcut filename.

- [ ] **Step 4: Implement scenario/evidence orchestration**

Each scenario gets a unique scratch child and manifest. The harness refuses to overwrite an existing scenario directory. It hashes artifacts/logs, records commands/exit codes, and outputs a final redacted summary. It does not make PASS from command exit alone; every scenario has explicit assertions.

- [ ] **Step 5: Implement bounded log scan**

Scan only Viewer service/Viewer/MSI logs for forbidden patterns and file-size growth. Report matching category and line hash, not the secret text. Require service/Viewer correlation IDs for injected failures. Reject logs over configured rotation limits.

- [ ] **Step 6: Verify policy tests and commit**

```powershell
Invoke-Pester .\scripts\windows\tests\AcceptanceScripts.Tests.ps1 -Output Detailed
```

Expected: PASS.

```bash
git add scripts/windows
git commit -m "test(viewer): add safe Windows acceptance harness"
```

### Task 2: Inventory and Remove Only the Rejected Development Installation

**Files:**
- Create: `scripts/windows/Remove-LegacyCamStationViewerDev.ps1`
- Modify: `scripts/windows/tests/AcceptanceScripts.Tests.ps1`
- Modify: `scripts/windows/README.md`

**Exact legacy targets:**

```text
Services: CamStationViewerAgent (only if ImagePath points below C:\Program Files\CamStation Viewer)
Tasks: CamStationViewer, CamStationViewerRecovery
Directories: C:\Program Files\CamStation Viewer, C:\ProgramData\CamStation\Viewer
ARP entry: only rejected CamStation Viewer dev product/uninstall entry whose uninstall command/path matches the old installer
Shortcuts: exact public desktop and CamStation Start Menu Viewer links
Registry: exact HKLM\Software\CamStation\Viewer and exact Run value CamStationViewer
Processes: only executables resolved below the exact Viewer install root
```

- [ ] **Step 1: Write failing exact-target cleanup tests**

The cleanup script must require both `-ConfirmLegacyDevCleanup` and an inventory file captured immediately before cleanup. It refuses unknown service image paths, extra directories, symlink/reparse roots, non-dev production MSI registration, unresolved environment variables, and wildcard task/process names.

- [ ] **Step 2: Run policy tests and verify RED**

```powershell
Invoke-Pester .\scripts\windows\tests\AcceptanceScripts.Tests.ps1 -Tag LegacyCleanup -Output Detailed
```

Expected: FAIL until cleanup is implemented.

- [ ] **Step 3: Implement recoverable inventory-first cleanup**

Save service/task/registry/shortcut/product inventory and copy only small text configuration/log evidence to the explicit scratch backup. Stop only exact legacy processes/services, unregister exact tasks/service, invoke old uninstaller when safely resolved, then remove exact remaining rejected roots/keys. Never remove OpenSSH, its firewall rule, unrelated CamStation server data, or any parent CamStation directory.

This cleanup is for the unreleased development artifact only; it is not packaged into the new MSI.

- [ ] **Step 4: Run on the current development PC when implementation is ready**

From an elevated interactive Windows session:

```powershell
.\scripts\windows\Get-CamStationViewerInventory.ps1 -OutputPath C:\CamStationAcceptance\pre-legacy-inventory.json
.\scripts\windows\Remove-LegacyCamStationViewerDev.ps1 `
  -InventoryPath C:\CamStationAcceptance\pre-legacy-inventory.json `
  -BackupDirectory C:\CamStationAcceptance\legacy-backup `
  -ConfirmLegacyDevCleanup
```

Expected: exact rejected resources are absent; unrelated CamStation/OpenSSH resources are unchanged.

- [ ] **Step 5: Commit the cleanup tool, not machine output**

```bash
git add scripts/windows/Remove-LegacyCamStationViewerDev.ps1 scripts/windows/tests/AcceptanceScripts.Tests.ps1 scripts/windows/README.md
git commit -m "test(viewer): bound legacy dev cleanup"
```

### Task 3: Validate Clean Install, First Run, Direct Launch, and Configuration UX

**Files:**
- Modify: `scripts/windows/Invoke-CamStationViewerAcceptance.ps1`
- Modify: `scripts/windows/Test-CamStationViewerInstallState.ps1`
- Modify: `scripts/windows/Test-CamStationViewerSession.ps1`
- Modify: `docs/test-evidence/windows-viewer-acceptance/README.md`

- [ ] **Step 1: Define clean-install assertions**

For every matrix row require:

- normal MSI UI/UAC flow with no console/PowerShell window;
- installation completion checkbox selected by default;
- Viewer launched in installing interactive user's SID/session at medium integrity;
- service LocalSystem, automatic, running, and distinct session 0 process;
- exact stable Program Files/ProgramData/registry layout;
- public desktop and Start Menu targets directly equal `CamStationViewer.exe` with no arguments;
- Run value equals quoted stable EXE plus `--autostart`;
- no scheduled task or Bootstrap/Host/Agent/custom installer artifact;
- first-run screen appears because MSI did not ask/store server settings.

- [ ] **Step 2: Execute first-run validation cases**

In the Viewer:

1. enter malformed URL and require `invalid_input`, values preserved;
2. enter unreachable server and require `server_unreachable`, values preserved and automatic retry no faster than the bounded backoff;
3. enter incompatible/rejecting test endpoint and require distinct messages;
4. enter valid server URL/display name and require one successful configuration/client registration;
5. verify `/live?viewer=1` opens and playback/renderer telemetry reaches server;
6. open `연결 설정`, edit display name, save, and verify same client ID;
7. attempt invalid replacement server and verify old live config remains active;
8. save valid replacement server and verify same client ID with new server heartbeat.

- [ ] **Step 3: Capture machine/server correlation**

Capture service/Viewer PID, user SID/session, MSI product/version, config schema/client ID hash, heartbeat display name/status, and correlation IDs. Do not capture server credentials or raw registry JSON; store a hash plus redacted fields.

- [ ] **Step 4: Run all four matrix rows**

From each interactive desktop:

```powershell
.\scripts\windows\Invoke-CamStationViewerAcceptance.ps1 `
  -Scenario CleanInstallFirstRun `
  -MatrixId W10-KO-L `
  -MsiPath C:\CamStationArtifacts\CamStationViewer.msi `
  -ScratchRoot C:\CamStationAcceptance\W10-KO-L
```

Use the matching matrix ID on other machines. Expected: every assertion PASS.

- [ ] **Step 5: Commit redacted evidence index**

```bash
git add docs/test-evidence/windows-viewer-acceptance/README.md
git commit -m "docs(viewer): record clean Windows install evidence"
```

### Task 4: Validate Auto-Start, Multiple Users, RDP, and Failure Recovery

**Files:**
- Modify: `scripts/windows/Invoke-CamStationViewerAcceptance.ps1`
- Modify: `scripts/windows/Test-CamStationViewerSession.ps1`
- Modify: `docs/test-evidence/windows-viewer-acceptance/README.md`

- [ ] **Step 1: Test default and disabled login auto-start**

With auto-start enabled, sign out/in and require one Viewer in the new interactive session after service lease acquisition. Disable it in Viewer settings, sign out/in, and require zero Viewer windows/processes despite the MSI-owned Run value remaining present. Manually launch and re-enable it; no registry install resource may be deleted/recreated by Viewer.

- [ ] **Step 2: Test one machine-wide lease across users**

Keep user A Viewer active. Sign in user B through RDP/fast user switching and launch Viewer. Require B to exit quietly with no live window and no termination of A. Close A normally; launch B and require it acquires the lease with the same PC-wide config/client ID. Never launch/kill another user's Viewer from the service.

- [ ] **Step 3: Test service restart while Viewer remains open**

From an elevated admin terminal, restart only `CamStationViewerService` through SCM. Require Viewer PID/window to remain, local UI to show temporary service unavailability, IPC reconnect/lease reacquisition, and server heartbeat recovery. A service restart must not trigger Viewer process recovery or exit code 1.

- [ ] **Step 4: Test server outage and recovery**

Stop the non-production server through `scripts/camstationctl.sh`, not broad process commands. Require disconnected form prefilled, config/client ID unchanged, retry capped at 30 seconds, service/Viewer alive, and automatic recovery after server restart. Verify malformed/rejected failures remain distinguishable.

- [ ] **Step 5: Test manual Viewer close semantics**

Close Viewer normally. Require service remains healthy and server shows Viewer `closed`, not installation/Agent failure. Wait beyond prior recovery windows and prove Viewer stays closed. Relaunch from each direct shortcut and require success.

- [ ] **Step 6: Run local and RDP matrix scenarios**

```powershell
.\scripts\windows\Invoke-CamStationViewerAcceptance.ps1 -Scenario SessionsAndRecovery -MatrixId W11-KO-R -ScratchRoot C:\CamStationAcceptance\W11-KO-R
```

Expected: all session/auto-start/recovery assertions PASS.

- [ ] **Step 7: Commit redacted evidence index**

```bash
git add docs/test-evidence/windows-viewer-acceptance/README.md
git commit -m "docs(viewer): record session and recovery evidence"
```

### Task 5: Validate Repair, Major Upgrade, Rollback, and Full Uninstall

**Files:**
- Modify: `scripts/windows/Invoke-CamStationViewerAcceptance.ps1`
- Modify: `scripts/windows/Test-CamStationViewerInstallState.ps1`
- Modify: `docs/test-evidence/windows-viewer-acceptance/README.md`

- [ ] **Step 1: Test standard repair**

After valid configuration, record client ID/config hash. Delete one test-safe installed resource at a time (public shortcut, Start Menu shortcut, copied non-running resource file), then run `msiexec /fa` with verbose log. Require restoration of MSI-owned resources and unchanged client ID/config/auto-start preference. Do not delete the running service executable for repair injection.

- [ ] **Step 2: Test manual major upgrade A to B**

Install signed A, configure, then run signed B through normal MSI UI. Require one B product, B files/service, preserved identity/config, direct shortcuts, no reboot, and no A product/orphans. When Viewer was open, Restart Manager requests normal exit; no forced taskkill is allowed.

- [ ] **Step 3: Test server-directed healthy update and failure injection**

Repeat the update plan's valid, wrong-size/hash, unsigned, wrong-publisher, interrupted-download, duplicate-command, Viewer-closed, and failing-MSI rollback scenarios through the normal server command path. Require exact pre/post-shutdown boundaries and no retry loop.

- [ ] **Step 4: Test full uninstall**

From Apps & Features/standard MSI UI, uninstall while Viewer is open and server once available, once unavailable. Require no reboot and exact absence of:

```text
registered CamStation Viewer product
CamStationViewerService
CamStation Viewer processes
Program Files\CamStation Viewer
ProgramData\CamStation\Viewer
HKLM\Software\CamStation\Viewer
HKLM Run\CamStationViewer
public desktop/Start Menu shortcuts
CamStationViewer* scheduled tasks
Bootstrap/Host/Agent/release/current state artifacts
```

Server unavailability must not block uninstall. Server registry may retain an offline historical record under normal TTL until an operator removes it.

- [ ] **Step 5: Test clean reinstall identity**

Reinstall after full uninstall. Require first-run screen and a new client ID only after valid configuration. The prior identity must not reappear from repair/cache/state files.

- [ ] **Step 6: Commit redacted lifecycle evidence**

```bash
git add docs/test-evidence/windows-viewer-acceptance/README.md
git commit -m "docs(viewer): record MSI lifecycle and rollback evidence"
```

### Task 6: Run Two-Hour Soak and Ten Complete Lifecycle Cycles

**Files:**
- Modify: `scripts/windows/Invoke-CamStationViewerAcceptance.ps1`
- Modify: `scripts/windows/Test-CamStationViewerLogs.ps1`
- Modify: `docs/test-evidence/windows-viewer-acceptance/README.md`

- [ ] **Step 1: Define soak cadence and failure schedule**

Run at least 120 continuous minutes on one Windows 10 and one Windows 11 target. Every 10 minutes record service/Viewer/renderer/stream heartbeat freshness, memory/handle counts, log sizes, current lease/session, installed version, and server command state. Inject:

- three service restarts;
- three server outages/recoveries;
- repeated pipe reconnects through service restart only;
- Viewer normal close/manual relaunch;
- one RDP disconnect/reconnect without sign-out;
- one valid update during the soak when fixtures permit.

- [ ] **Step 2: Define soak pass thresholds**

Require:

- no unexpected Viewer termination from service/pipe restart;
- no duplicate Viewer lease/window;
- no stopped heartbeat/control loop after recovery;
- no repeated automatic MSI attempt for one generation;
- bounded log rotation and no forbidden secret pattern;
- no monotonically unbounded handle/process growth after returning to baseline;
- no scheduled task or orphan broker/helper/msiexec process at the end.

- [ ] **Step 3: Run soak from interactive target**

```powershell
.\scripts\windows\Invoke-CamStationViewerAcceptance.ps1 `
  -Scenario Soak `
  -MatrixId W11-EN-L `
  -DurationMinutes 120 `
  -ScratchRoot C:\CamStationAcceptance\W11-EN-L-Soak
```

Expected: PASS after full duration; early harness exit is failure.

- [ ] **Step 4: Run ten lifecycle cycles**

Each cycle performs clean install, first-run/config (or scripted service IPC after UI is proven), direct launch, repair, A-to-B or B-to-A-newer major upgrade as version ordering permits, full uninstall, and absence assertions. Alternate Viewer-open/closed update and server available/unavailable uninstall. Restore clean server state between cycles; do not reuse failed command generation.

```powershell
.\scripts\windows\Invoke-CamStationViewerAcceptance.ps1 `
  -Scenario LifecycleCycles `
  -Cycles 10 `
  -MsiPath C:\CamStationArtifacts\viewer-a\CamStationViewer.msi `
  -UpgradeMsiPath C:\CamStationArtifacts\viewer-b\CamStationViewer.msi `
  -ScratchRoot C:\CamStationAcceptance\Cycles
```

Expected: ten of ten PASS with no reboot/orphan.

- [ ] **Step 5: Commit redacted stability evidence**

```bash
git add docs/test-evidence/windows-viewer-acceptance/README.md
git commit -m "docs(viewer): record Windows Viewer stability gate"
```

### Task 7: Final Release Audit and Status Update

**Files:**
- Modify: `docs/test-evidence/windows-viewer-acceptance/README.md`
- Modify: `docs/07-implementation-status.md`
- Modify: `docs/04-cctv2-test-plan.md`

- [ ] **Step 1: Audit every design requirement against evidence**

Create a table mapping every section of `docs/superpowers/specs/2026-07-18-standard-windows-viewer-installer-design.md` to one or more passing matrix/scenario IDs. No `source reviewed`, `unit test`, `cross-build`, or `not applicable` entry may substitute for a required Windows gate.

- [ ] **Step 2: Audit production artifact identity**

For the exact candidate MSI, record SHA-256, size, ProductCode, UpgradeCode, ProductVersion, signer identity/timestamp, embedded CAB integrity, installed EXE signatures, and server release metadata. Require all values to agree.

- [ ] **Step 3: Audit negative findings**

Run final searches/inventory for secrets and forbidden architecture:

```bash
rg -n 'CamStationViewerSetup|CamStationViewerBootstrap|CamStationViewerHost|CamStationViewerRecovery|schtasks|current\.json|release\.zip' cmd internal viewer-app installer scripts
```

```powershell
Get-ScheduledTask | Where-Object TaskName -Like 'CamStationViewer*'
Get-ChildItem 'C:\Program Files\CamStation Viewer' -Recurse
Get-CimInstance Win32_Service -Filter "Name='CamStationViewerService'"
```

Expected: source search finds only explicit negative tests/documentation, no tasks exist, installed tree matches MSI manifest, exactly one service exists.

- [ ] **Step 4: Update implementation status only after all gates pass**

Mark the standard Windows Viewer installation/update work complete only when all required rows, failure injections, two-hour soak, and ten cycles pass for the exact candidate artifact. If any required evidence is missing, status remains `implemented, acceptance pending` with the missing scenario named.

- [ ] **Step 5: Run final repository verification**

```bash
make test
go test ./... -count=1
cd web && npm test && npm run lint && npm run build
cd ../viewer-app && npm test && npm run build && npm run package:win
cd .. && go build ./cmd/camstationd ./cmd/camstation-viewer-service ./cmd/camstation-viewer-restart
git diff --check
```

Expected: all checks PASS.

- [ ] **Step 6: Commit the release audit**

```bash
git add docs/test-evidence/windows-viewer-acceptance/README.md docs/07-implementation-status.md docs/04-cctv2-test-plan.md
git commit -m "docs(viewer): complete real-Windows release audit"
```

## Final Acceptance Gate

The work is complete only when:

- every required OS/language/session matrix row passes;
- normal UAC install launches Viewer in the actual interactive user's session;
- first-run, settings, preserved failure input, and 30-second maximum retry behavior pass;
- default/disabled auto-start and one machine-wide lease pass across users/RDP;
- service/server failure recovery does not terminate or reconfigure Viewer;
- repair and major upgrade preserve identity; full uninstall removes it and every exact owned resource;
- signed immediate update, pre-shutdown rejection, Windows Installer rollback, Viewer-open/closed behavior, and idempotency pass;
- two-hour soak on Windows 10 and 11 and ten complete lifecycle cycles pass;
- no reboot, scheduled task, Bootstrap/Host/Agent chain, console/PowerShell window, secret leak, duplicate update, or orphan remains;
- evidence belongs to the exact production candidate MSI published by the server.
