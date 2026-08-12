# Viewer recordings access and native fullscreen

## Goal

The installed Windows Viewer must let an operator open and play recorded video,
while keeping the administrative settings surface unavailable. Its fullscreen
control must place the native Electron window in fullscreen, with no title bar,
application menu, or Windows taskbar visible.

## Navigation boundary

The Viewer starts on the live route. Its hardened navigation policy allows only
the configured server origin and these application routes:

- `/live?viewer=1` for the live workspace;
- `/recordings` (with its same-origin recording playback navigation).

All other paths, origins, downloads, pop-up windows, and permission requests
remain denied. In particular, `/settings` remains unavailable from the Viewer.
Viewer mode renders a compact shared header with only `라이브` and `녹화` tabs,
so either Viewer route can return to the other without exposing settings or
other administrative navigation. The settings link is not shown in Viewer mode.

## Fullscreen behavior

The live workspace uses the Viewer preload bridge to request native window
fullscreen. The Electron main process owns the `BrowserWindow` state change.
When fullscreen is entered, the complete application frame and Windows shell
chrome are hidden. The existing button label follows the native fullscreen
state, and either the button or Escape exits fullscreen.

## Failure handling

If a fullscreen request cannot be delivered through the preload bridge, the
live screen remains usable and does not attempt a DOM-only fullscreen fallback.
The current navigation and service-reconnect rules remain unchanged.

## Verification

- Unit tests prove the allowed Viewer routes and the native fullscreen IPC
  contract.
- Viewer package tests and TypeScript builds pass.
- A new MSI is installed on the Windows VM.
- On the VM, the 녹화 link opens the recordings list and a recording can be
  selected for playback; settings remains unavailable.
- On the VM, fullscreen hides the window title, app menu, and Windows taskbar;
  Escape returns to the normal window.
