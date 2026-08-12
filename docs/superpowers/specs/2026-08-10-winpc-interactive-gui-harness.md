# WIN11-DELL interactive GUI harness specification

## Goal

Let the Linux-side maintainer directly observe the installed CamStation Viewer window in the
already logged-on Windows RDP session. The evidence loop must not require the operator to supply
screenshots and must not add a persistent remote-control service or credential.

## Architecture

The elevated SSH maintenance account registers a uniquely named Task Scheduler job with logon type
`TASK_LOGON_INTERACTIVE_TOKEN`. Windows runs the worker as the already logged-on `dyllislev` user in
that user's nonzero desktop session. The worker launches or locates only the installed
`CamStationViewer.exe`, verifies its executable path and session, captures its window rectangle, and
writes a PNG plus bounded UI Automation metadata to a per-run evidence directory. The launcher
waits for an atomic completion record and deletes the exact task in `finally`. Linux retrieves the
evidence through the existing pinned SSH connection.

## Security properties

- No password, new account, listening socket, firewall rule, VNC/RDP service, or Viewer pipe ACL
  change.
- Least-privileged interactive task, two-minute task limit, unique task name, and exact cleanup.
- Per-run ACL limited to SYSTEM, Administrators, and the target user.
- Exact installed Viewer executable and matching desktop session are required.
- Window rectangle only; full-desktop capture is not an available default.
- UI Automation records control metadata but never reads ValuePattern/text-field values. URLs and
  IPv4-like text in accessible names are redacted.

## First acceptance

Run `LaunchAndCapture`, retrieve the PNG and JSON, inspect the PNG directly, confirm the setup form
state, and prove no task or worker remains. Keep Viewer configuration empty. A later operation can
add a separately reviewed focus/type action after this observation path is proven.

## Reuse

From the pinned Linux SSH environment, invoke the synchronized launcher on WIN11-DELL:

```powershell
pwsh -NoProfile -NonInteractive -ExecutionPolicy Bypass -File `
  C:\CamStationDev\src\CamStation\scripts\windows\Invoke-CamStationViewerGuiCapture.ps1 `
  -TargetUser 'WIN11-DELL\dyllislev' `
  -Operation Capture
```

The launcher returns a JSON `ResultDirectory`. Retrieve only `viewer-window.png`, `uia.json`, and
`complete.json` from that exact directory over the existing pinned SSH connection, verify their
SHA-256 values, and inspect the PNG directly. Use `LaunchAndCapture` when Viewer is not already
open. The operation fails closed when the intended user has no active Explorer session.

## Verified result — 2026-08-10

- `LaunchAndCapture` ran as `WIN11-DELL\dyllislev` in interactive session `1`, launched Viewer PID
  `10308`, and captured only the 1600x1200 Viewer window with `PrintWindow`.
- The captured setup screen rendered the server-address field, Viewer-name field, auto-start option,
  connect button, and retry button. No server value was typed and no CCTV endpoint was contacted.
- A later independent `Capture` produced the same image SHA-256
  `e06bfb0520eb12ebf6b13c4de298a985155f665a5aa1b658418959b5027511b8`. Its settled UIA tree
  identified both edit controls and reported `server-url` with `HasKeyboardFocus=true`.
- Both one-shot tasks reported `TaskDeleted=true`; the follow-up audit found zero remaining harness
  tasks, worker processes, or GUI bridge services. `CamStationViewerService` remained running and
  automatic, and Explorer remained in session `1`.
