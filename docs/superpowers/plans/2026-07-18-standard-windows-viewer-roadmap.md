# Standard Windows Viewer Redesign Roadmap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to execute the linked plans in order. Do not skip a phase exit gate.

**Goal:** Execute the approved standard Windows Viewer redesign without mixing the old custom installer architecture into the replacement.

**Architecture:** Four dependent plans establish the runtime boundary first, make Windows Installer the lifecycle owner second, add immediate signed MSI updates third, and require real-Windows evidence last. Each phase has an exit gate; a later phase may not be used to excuse a failed earlier one.

**Tech Stack:** Go, Electron/TypeScript, WiX MSI, Windows SCM/named pipes/Installer/Authenticode, real Windows 10/11 acceptance.

## Source of Truth

- Approved design: `docs/superpowers/specs/2026-07-18-standard-windows-viewer-installer-design.md`
- Existing `2026-07-16` and `2026-07-17` Windows Viewer plans describe the rejected custom installer/supervision implementation and are historical context only.
- This roadmap and its four linked plans supersede those implementation paths for Windows packaging and lifecycle behavior.

## Execution Order

1. `2026-07-18-standard-windows-viewer-runtime.md`
   - Direct Electron launch.
   - Minimal LocalSystem management service.
   - Service-owned HKLM configuration, server control, local IPC, one Viewer lease, bounded logs.
   - No MSI/update execution yet.

2. `2026-07-18-standard-windows-viewer-msi.md`
   - Pinned WiX project and one per-machine x64 MSI.
   - Stable Program Files layout, SCM service, public/Start Menu direct shortcuts, HKLM Run auto-start.
   - Repair/major upgrade/full uninstall and signing policy.
   - Delete old custom installer/Bootstrap/Host/Agent source only after Windows MSI smoke passes.

3. `2026-07-18-standard-windows-viewer-update.md`
   - Server publication changes from EXE to MSI.
   - Exact download/hash/Authenticode/publisher verification before Viewer shutdown.
   - User-session restart helper and short-lived SYSTEM MSI broker.
   - Idempotent result reconciliation, Windows Installer rollback, failure injection.

4. `2026-07-18-standard-windows-viewer-acceptance.md`
   - Windows 10/11, Korean/English, local/RDP, standard user + UAC matrix.
   - Clean install, first run, settings, sessions, auto-start, outage/restart, repair/update/uninstall.
   - Two-hour soak on Windows 10 and 11 and ten complete lifecycle cycles.
   - Exact production candidate evidence before completion status.

## Cross-Phase Invariants

- Never restore scheduled tasks, Bootstrap/Host indirection, version directories, `current.json`, embedded release ZIPs, or application-owned install rollback.
- Never let Viewer or MSI compete as writers of mutable configuration. Only the service writes the one `Configuration` registry value; MSI owns the key container and full-uninstall removal.
- Never erase configuration/ProgramData during repair or removal of the old product in a major upgrade. Full cleanup conditions exclude `UPGRADINGPRODUCTCODE`.
- Never close Viewer before the desired MSI passes size, SHA-256, Authenticode trust, timestamp, and publisher checks.
- Never report update success from a marker alone. Reconcile the actual MSI-authored/linked installed version and service health.
- Never accept SSH-only evidence for GUI/session behavior. Use SSH only for staging and machine-state collection.
- Never claim completion while a required Windows matrix, rollback injection, soak, or lifecycle cycle is missing.

## Approved-Design Coverage

| Approved design section | Owning plan/tasks | Release proof |
|---|---|---|
| Removed architecture and direct launch | Runtime Tasks 5-6; MSI Tasks 2, 7 | Direct shortcut/process evidence; forbidden-artifact scan |
| Stable Program Files/ProgramData layout | MSI Tasks 2-3 | MSI manifest and install/uninstall inventory |
| Minimal service responsibilities | Runtime Tasks 3-4 | Service-without-Viewer heartbeat and no-supervision tests |
| One machine configuration/client ID | Runtime Task 1; MSI Task 3 | Repair/upgrade preservation and uninstall/reinstall identity evidence |
| Versioned local IPC and single lease | Runtime Task 2 | Multi-user/local/RDP lease evidence |
| First run, settings, failure retry | Runtime Task 5 | Clean-install and disconnected UX matrix |
| Public/Start Menu shortcut and auto-start | MSI Task 2; Runtime Task 5 | Direct target, login enabled/disabled evidence |
| Server status and UI commands | Runtime Task 4 | Separate service/Viewer/renderer heartbeat evidence |
| Immediate signed MSI update | Update Tasks 1-5 | Normal server-directed A-to-B update evidence |
| Failure boundary and Windows Installer rollback | Update Task 6 | Bad hash/signature/publisher and failing-MSI evidence |
| Repair and full uninstall | MSI Tasks 3, 6 | Preserved config on repair; exact absence after uninstall |
| Bounded redacted logs | Runtime Task 3; MSI Task 3 | Rotation/ACL/secret-scan evidence |
| Build/signing and immutable publication | MSI Task 5; Update Task 1 | Exact candidate signature/hash/server metadata audit |
| OS/language/session/stability gates | Acceptance Tasks 3-7 | Four-row matrix, two-hour soaks, ten lifecycle cycles |
| Rejected dev release transition | MSI Task 7; Update Task 1; Acceptance Task 2 | Inventory-first cleanup and no old client artifacts |

## Required Checkpoint After Each Plan

At each exit gate:

```bash
git status --short --branch
git diff --check
go test ./... -count=1
cd web && npm test && npm run lint && npm run build
cd ../viewer-app && npm test && npm run build
```

Run only commands relevant to files that exist at that phase; once `cmd/camstation-viewer-service` and `cmd/camstation-viewer-restart` exist, add their Windows cross-builds. Frontend source changes must leave rebuilt embedded assets and a successful Go build.

Each phase ends with:

- focused tests from every task;
- full repository verification;
- the phase-specific real-Windows gate;
- a truthful `docs/07-implementation-status.md` update;
- small commits matching the task boundaries in the detailed plan.

## Stop Conditions

Stop execution and revise the design before continuing if:

- WiX 6 licensing terms are not accepted for this project;
- direct Viewer launch cannot meet the all-users/single-lease requirement without service process supervision;
- MSI repair or major upgrade erases the machine configuration/client ID;
- full uninstall cannot remove the exact dynamic ProgramData/config roots without broad deletion;
- the update broker cannot survive service replacement without owning installed-file rollback;
- production publisher verification cannot be performed before Viewer shutdown;
- a real-Windows phase gate repeatedly contradicts the unit/cross-build model.

These are architecture failures, not reasons to add another scheduled task, shell script, custom installer transaction, or silent fallback.
