# Viewer Client Redesign Spec

## Status

Approved planning record. Implementation has not started from this spec.

## Goal

Build a CamStation 2.0 Windows viewer EXE that asks for a server address and viewer display name, then opens the live monitoring workspace at `/live?viewer=1` by default. The server must be able to monitor and control the client even when the web renderer freezes, crashes, or becomes unresponsive.

## Decisions

- Client runtime: hardened Electron first, not a thin webview shell.
- Live surface: keep the CamStation 2.0 web live UI for first release.
- Liveness/control owner: Electron main process, not renderer JavaScript.
- Identity: operator-entered display name plus generated stable internal `clientId`.
- First release scope: setup, settings, live load, main-process heartbeat, command delivery/ack, restart/reload, per-stream resubscribe, diagnostics, EXE version/download.
- Excluded first release scope: login/pairing token, auto-update, native mpv/libVLC player, legacy `/new`.

## Stability Requirements

- Main-process heartbeat continues while renderer is crashed or unresponsive.
- Server command channel remains recoverable through SSE with long-poll fallback.
- `resubscribe_stream` affects only the requested stream pipeline.
- Repeated reload/restart does not leave orphan Electron processes.
- cctv2 or a named camera-reachable substitute must provide KST-timestamped soak evidence.
- If hard stability criteria fail twice after targeted fixes, stop implementation and write a native-player fallback plan.

## Security Requirements

- `clientId` is an identifier, not an authentication secret.
- Name/client-id spoofing is an accepted trusted-LAN first-release risk because the user chose no pairing/login.
- Command creation stays inside the existing same-origin/admin console boundary.
- Electron accepts only `http:` and `https:` server URLs without credentials.
- BrowserWindow must use `nodeIntegration: false`, `contextIsolation: true`, `sandbox: true`, and `webSecurity: true`.
- Diagnostics are whitelist-only and redacted.
- EXE artifact metadata must include `version`, `filename`, `sizeBytes`, and `sha256`.

## Implementation Plan

Use the detailed task plan at:

- `docs/superpowers/plans/2026-07-03-viewer-client-redesign.md`

That plan includes the reviewed task breakdown, QA commands, stability thresholds, security gates, and final review wave.
