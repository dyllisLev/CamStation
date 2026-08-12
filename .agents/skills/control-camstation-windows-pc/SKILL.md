---
name: control-camstation-windows-pc
description: Install, audit, observe, and control the authorized CamStation Windows test PC or monitoring PC through an explicit target profile, pinned SSH management plane, and logged-on interactive session. Use for Windows PC setup, status or performance checks, service/process control, full-desktop or exact-window screenshots, app launch/close, click, typing, hotkeys, scroll, window move/resize/maximize/fullscreen, CamStation Viewer configuration, GUI capture, and visual diagnosis. Also trigger for Korean requests such as "WinPC 제어", "테스트 PC 조작", "모니터링 PC 제어", "화면 확인", "전체 화면 캡처", "창 최대화", "전체화면", or "Viewer 실행 화면 확인". Do not use for an unapproved host, application rollout planning, or adding a remote-access service.
---

# Control CamStation Windows PC

Use one project skill and one deterministic target wrapper for both authorized PCs. The aliases select
connection and Windows-session facts only; they never encode an application version, rollout stage,
or different control capability.

## Load the applicable references

- Always read [references/targets.md](references/targets.md) completely before selecting a PC.
- Read [references/control-plan.md](references/control-plan.md) completely for desktop/window
  observation, input, app/window control, or cleanup.
- Read [references/system-control.md](references/system-control.md) completely for processes,
  services, files, packages, performance diagnostics, or another elevated PowerShell operation.
- Also read [references/setup.md](references/setup.md) completely when the interactive driver is
  missing, stale, installed, or repaired.
- Also read [references/evidence-loop.md](references/evidence-loop.md) completely when the claim
  concerns the exact CamStation Viewer window, rendering, controls, or keyboard focus.

## Select the target before doing anything

Invoke `scripts/windows/Invoke-CamStationWindowsTarget.mjs` with exactly one explicit alias:
`test-pc` or `monitoring-pc`. There is no default target. If the request does not identify which PC,
ask; do not infer it from the currently running application.

The wrapper is the only Linux-side SSH/SCP entry point. It resolves the ignored local profile,
enforces public-key-only host-key-pinned SSH, then compares the remote computer name, maintenance
identity, interactive `MACHINE\User`, Explorer count, and session ID with the selected profile.
It rejects a mismatch before staging or executing a mutating operation. Never paste a host, key path,
ad hoc SSH command, `schtasks`, or hand-built `EncodedCommand` in place of the wrapper.

An Explorer process in a disconnected RDP session is not an active desktop. The wrapper reads the
numeric Windows Terminal Services state and permits Plan, desktop capture, and Viewer capture only
when the selected session is `Active`. Reconnect that PC's RDP or VM console when Status reports
`Disconnected`; do not try screenshots or foreground input against a headless surface.

Start every request with:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode status
```

Proceed only when the pinned driver is in the expected nonzero interactive session, telemetry is
disabled, driver TCP/firewall counts are zero, control-task counts are zero, and required canonical
script hashes match. Use `--mode sync` for a known stale project script and `--mode setup` only under
the setup runbook.

## Choose the technical plane

- `status`: read-only SSH identity, session, driver, service, task, listener, firewall, and script
  parity audit.
- `system --intent read-only|change --script <file.ps1>`: one hash-checked, parser-checked elevated
  maintenance script for process/service/file/package/performance work. This runs in the SSH
  maintenance context, not the interactive desktop.
- `plan --plan <file.json>`: one bounded Cua/UIA batch in the logged-on user's desktop. Put the
  observation, actions, and post-action observations in the same plan.
- `desktop-capture`: capture the entire current interactive desktop. Do not substitute an exact
  application window when the user asked for the PC screen.
- `viewer-capture`: capture and inspect only the verified installed Viewer top-level window. Do not
  substitute the desktop when the claim is about Viewer rendering or focus.
- `viewer-configure --configuration <file.json>`: validate a four-field configuration document,
  apply it through the Viewer service management pipe in the selected interactive session, preserve
  or create the private client identity without printing it, and delete the exact task/run.
- `cleanup --run-id <exact-id>`: recover one retained control run; ordinary successful wrapper runs
  download and hash-check evidence, then clean themselves.

`scripts/windows/Invoke-CamStationWindowsControl.ps1` remains the only Windows-side interactive
launcher. It owns active-session discovery, a one-shot `TASK_LOGON_INTERACTIVE_TOKEN` task, UTF-8
stdin JSON, atomic completion, and exact task cleanup. Never recreate that bridge.

## Control with a closed evidence loop

Observe immediately before acting. Address windows by current PID plus `window_id`, and UIA elements
by a unique `element_token` selected from that observation. Treat a missing selector field as a
non-match and zero or multiple matches as failure. The verified title-bar selector field is
`element_index`, not `index`.

Prefer background UIA input. Escalate to foreground input only after the background path reports
unavailable or a fresh post-state proves no effect. Every mutating plan step must point to a later
`verifyWith` observation with a screenshot or assertion. Exit code zero and `effect=unverifiable`
are not success.

Windows maximize/restore and an application's native fullscreen are different operations. Verify
window placement with fresh geometry/state; enter native Viewer fullscreen only through its visible
Viewer control and verify that title bar and taskbar behavior changed. Never reuse remembered screen
coordinates or resolution, because an RDP display can resize without reconnecting.

## Finish cleanly

Require matching target/session identities, `TaskDeleted=true`, passing assertions, nonempty
artifacts whose local SHA-256 matches `complete.json`, and direct `view_image` inspection for every
visual claim. A screenshot file size or UIA tree alone is not visual proof.

Close only a window or exact PID created by the current request unless the user explicitly names an
existing target. A closed UWP window may leave a windowless `ApplicationFrameHost`; verify zero target
windows before stopping only the recorded launch PID. Never use broad process-name termination.

After retrieval, require zero control/setup/capture tasks, no run/staging residue, and no unintended
session, service, process, listener, or firewall change. Report target alias and machine, non-secret
user/session, elapsed time, pre/post state, inspected artifact path and hash, cleanup result, and any
operation that was intentionally not exercised.

## Preserve the boundary

- Do not add a listener, firewall rule, account, stored credential, persistent task, or another
  remote-control service. The local Cua driver must keep telemetry disabled and zero network
  listeners.
- Do not read clipboard data, passwords, camera URLs, document/edit-field values, or unrelated
  desktop content. Use a clean disposable app/file for typing tests.
- Do not turn an observed Viewer version or process into a permanent PC-profile rule. Observe
  application state fresh and follow the separate deployment request when application state changes.
- Do not infer an application migration, removal, upgrade, or desired version from the selected PC
  alias. Those are separate deployment decisions; execute them only when the current request
  explicitly authorizes the exact application change.
