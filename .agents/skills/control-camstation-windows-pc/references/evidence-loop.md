# Exact CamStation Viewer evidence mode

Use this mode when a claim concerns the real Viewer window: launch/paint behavior, visible controls,
clipping, blank rendering, keyboard focus, or an exact-window screenshot. Ordinary Windows control
uses the Plan runbook instead.

Canonical implementation:

- `scripts/windows/Invoke-CamStationWindowsTarget.mjs --mode viewer-capture` — target selection,
  transport, retrieval, hash verification, and cleanup.
- `scripts/windows/Invoke-CamStationWindowsControl.ps1 -Mode ViewerCapture` — unified Windows-side
  interactive entry point.
- `scripts/windows/Invoke-CamStationViewerGuiCapture.ps1` — exact Viewer launcher and one-shot task.
- `scripts/windows/Capture-CamStationViewerWindow.ps1` — exact-window capture and bounded UIA.
- `viewer-app/tests/winPcGuiHarness.test.ts` — Viewer evidence security invariants.

Do not recreate or inline these mechanisms. The target wrapper verifies local and remote script
SHA-256 parity before running a remote copy.

## Invoke

Use `Capture` for an existing Viewer. Use `LaunchAndCapture` only when Viewer launch is part of the
request or no Viewer window is open. Invoke through the explicit target alias:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs \
  --target <alias> --mode viewer-capture --viewer-operation Capture
```

Require `INTERACTIVE_GUI_CAPTURE_COMPLETE`, `TaskDeleted=true`, worker success, and matching target
and worker identity/session. The result directory must be one direct child of
`C:\CamStationDev\gui-evidence`.

Electron accessibility can settle after the first painted frame. Repeat `Capture` after the
renderer settles when expected visible controls are missing from the first bounded UIA scan. Never
edit or reuse the prior run as if it were fresh evidence.

## Retrieve and inspect

The wrapper retrieves exactly `viewer-window.png`, `uia.json`, and `complete.json` into a fresh
ignored alias/run directory, recalculates the PNG and UIA SHA-256 values, then removes the exact
remote evidence run. Open `viewer-window.png` with `view_image`; process state, OCR, file size, or
UIA alone cannot prove the visible state.

Correlate visible controls only with safe UIA metadata: redacted name, control type, AutomationId,
class, enabled/focusable/focused flags, and bounded rectangle. Never collect an edit control's
value, `ValuePattern`, clipboard data, or keys. A UIA node does not prove correct rendering, and a
plausible screenshot does not prove focus.

Capture only the verified CamStation Viewer top-level window. Never substitute a full-desktop
screenshot or unrelated window. Exact capture preserves a maximized window and fails closed if the
window is minimized rather than restoring/repositioning it. Do not type a server address, connect
CCTV, change saved settings, or exercise destructive UI actions unless separately and explicitly
requested.

## Audit and acceptance

After success or failure, require zero `CamStation-GuiCapture-*` tasks, zero capture workers, and no
remote evidence directory. Confirm the Viewer service and pre-existing Explorer session remain
intact.

The acceptance record includes operation, UTC/KST timestamp, session plus Viewer/window PID,
capture method and dimensions, verified PNG/UIA hashes, direct visible finding, relevant focus
finding, `TaskDeleted=true`, zero-task/zero-worker audit, and actions intentionally not exercised.
Installation, service state, process existence, or a nonempty PNG alone is not Viewer GUI proof.
