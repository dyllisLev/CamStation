---
name: verify-windows-viewer-gui
description: Verify the real CamStation Viewer GUI on an authorized Windows build or test PC from Linux/SSH. Use when asked to launch, observe, capture, screenshot, visually inspect, or diagnose the installed Viewer; verify Electron rendering, setup-form controls, keyboard focus, or an interactive Windows session; or prove GUI behavior that install, process, and service checks cannot establish. Also trigger for Korean requests such as "Windows GUI 캡처", "화면 직접 확인", "포커스 확인", or "실행 화면 확인". Do not use for generic desktop surveillance or to expand remote-access privileges.
---

# Verify Windows Viewer GUI

Observe the installed Viewer through the existing authorized SSH channel without making the
operator capture screenshots. Treat command-line health and desktop acceptance as separate facts.

## Load the Runbook

Read [references/evidence-loop.md](references/evidence-loop.md) completely before operating on a
Windows host. Use the repository scripts named there as the canonical implementation; do not copy
their logic into ad hoc remote commands.

## Establish Scope

1. Confirm that the user has authorized the Windows host, maintenance account, and intended
   interactive desktop user.
2. Resolve connection details from the current approved environment or operator-provided values.
   Never commit an IP address, username, private-key path, fingerprint, password, or captured
   runtime artifact to this skill.
3. Confirm that exactly one nonzero Explorer session belongs to the intended desktop user. Stop if
   the session is absent or ambiguous.
4. Preserve the installed Viewer, its service, the existing RDP session, and saved configuration
   unless the user explicitly authorizes a change.

## Select the Operation

- Use `LaunchAndCapture` when the installed Viewer is not open or launch behavior is under test.
- Use `Capture` when the Viewer is already open and its current frame or focus state is under test.
- Repeat `Capture` after the renderer settles when the first UI Automation scan lacks expected
  controls. Do not declare an Electron control unavailable from only the initial frame.

## Require an Evidence Loop

1. Verify that the remote copies of
   `scripts/windows/Invoke-CamStationViewerGuiCapture.ps1` and
   `scripts/windows/Capture-CamStationViewerWindow.ps1` match the reviewed local source.
2. Run the launcher through the existing pinned SSH transport and require its
   `INTERACTIVE_GUI_CAPTURE_COMPLETE` result. Reject a failed worker, identity/session mismatch,
   unexpected result directory, or `TaskDeleted` other than `true`.
3. Retrieve only `viewer-window.png`, `uia.json`, and `complete.json` from the exact per-run
   directory. Recalculate the PNG and UIA SHA-256 values and compare them with `complete.json`.
4. Open `viewer-window.png` with the local image-inspection tool. Do not infer the visible state
   solely from file size, process state, OCR, or UI Automation metadata.
5. Correlate the visible controls with the bounded UIA records. Use `AutomationId`, enabled state,
   keyboard-focusability, focus state, and bounds; never collect an edit control's value.
6. Prove that the one-shot task and worker exited. Report Viewer process/service changes separately
   from the evidence harness cleanup.

## Preserve the Security Boundary

- Capture only the verified CamStation Viewer top-level window. Never substitute a full-desktop
  screenshot or unrelated window capture.
- Do not install VNC, AnyDesk, RustDesk, a GUI bridge, or another persistent remote-control agent.
- Do not add a listener, firewall rule, account, stored credential, scheduled persistent task, or
  weakened Viewer named-pipe ACL.
- Do not type or save a server address, connect to CCTV, change auto-start, or exercise destructive
  UI actions unless that interaction is explicitly in scope.
- Keep UIA output bounded and secret-safe. Preserve URL/IP redaction and the ban on `ValuePattern`
  or equivalent text-field reads.
- Fail closed rather than changing the session, identity, ACL, evidence root, timeout, or task
  execution model to make a capture succeed.

## Handle Defects at the Source

When capture fails, identify whether the failure is transport, active-session discovery, installed
Viewer state, task execution, window selection, rendering, UIA timing, artifact integrity, or
cleanup. Patch only the canonical project scripts when the harness itself is defective. Run the
Viewer source-policy tests and the skill validator after any change; then repeat the real Windows
capture before claiming the defect fixed.

## Report What Was Proved

Record the operation, target identity in non-secret form, session ID, Viewer/window PID, capture
mode and dimensions, artifact hashes, visible UI state, relevant UIA focus state, and cleanup
result. State explicitly what was not exercised. Never include credentials, camera URLs, captured
input values, or unrelated desktop content.
