# Windows Viewer Control, Update, and Playback Design

## Status

Approved design record. This document supersedes
`2026-07-03-viewer-client-redesign.md`. The existing implementation plan is
stale until it is reconciled with this design.

## Goal

Build an unattended Windows monitoring client that the CamStation server can
observe, update, and control even when the video renderer is frozen, crashed,
or still showing video without a healthy control channel. Preserve the
CamStation 2.0 live workspace while making long-running playback and recovery
explicit operational systems.

The client must install completely through one elevated installer run. After
installation, ordinary operation, recovery, and updates require no button,
confirmation dialog, or local user action.

## Product Decisions

- Use a machine-wide Windows Agent service as the control authority.
- Keep the Electron Viewer as the interactive monitoring surface, not as the
  heartbeat, update, or process-supervision authority.
- Start the Agent at Windows boot and start the Viewer in the interactive user
  session at login.
- Keep the CamStation 2.0 `/live?viewer=1` workspace and never target legacy
  `/new` or `/viewer` routes.
- Use WebRTC as the primary live transport and MSE/WebSocket as the automatic
  fallback.
- Isolate recovery by stream before escalating to a full Viewer restart.
- Make application updates server-directed, silent, verified, transactional,
  and rollback-capable.
- Bound every automatic retry and restart. A persistent failure is visible to
  the server and never becomes a tight or infinite restart loop.
- Keep native libVLC/mpv playback out of the first implementation. If the
  approved WebRTC/MSE design fails the hard stability gate twice after targeted
  fixes, stop and write a native-player replacement design.

## System Architecture

### CamStation Server

The server owns the client registry, desired application version, durable
commands, update artifacts, and operational status. It exposes only redacted
client and stream state to the console.

Server responsibilities:

- register a stable machine client identity;
- record Agent, Viewer, renderer, and per-stream state independently;
- accept heartbeats and command results idempotently;
- deliver commands through SSE with bounded long-poll fallback;
- maintain a per-client desired version and durable `update_app` command;
- serve signed release metadata and the exact configured release artifact;
- show update, recovery, retry-budget, and last-known-good details;
- derive `offline` when the Agent heartbeat becomes stale.

### Windows Agent Service

The Agent is a machine-wide Windows service that starts automatically at boot.
It remains active without an interactive login and does not depend on Electron
renderer JavaScript.

Agent responsibilities:

- own the stable `clientId`, server URL, machine settings, and recovery budget;
- maintain heartbeat and the server control channel;
- receive, deduplicate, execute, and report commands;
- supervise the Viewer process and its local IPC heartbeat;
- start, stop, and force-restart the Viewer through the registered startup task;
- collect bounded Viewer and stream telemetry;
- download and verify update releases;
- launch the updater helper and report update progress;
- enter explicit degraded or failed states when bounded recovery is exhausted.

Machine state lives under a protected machine-wide application-data location,
not Electron userData. Restart counters, command IDs, update transaction state,
and the last-known-good version survive process and machine restarts.

### Electron Viewer

The Viewer runs in the logged-in user's desktop session and renders the
existing React live workspace. It has no authority to update the installation
or claim that the client is healthy.

Viewer responsibilities:

- open `/live?viewer=1` in a hardened BrowserWindow;
- render camera layout, focus view, zoom/pan, PTZ, and operator controls;
- run one independent playback state machine per visible camera;
- report renderer liveness and per-stream playback telemetry to the Agent;
- accept narrow local commands for reload and per-stream resubscription;
- expose no camera URL, credential, raw go2rtc endpoint, or privileged Node API.

### Local IPC

The Agent and Viewer communicate over an authenticated local named pipe with a
restrictive ACL. The protocol is versioned, length-bounded JSON with request
IDs. It carries Viewer heartbeat, renderer status, stream telemetry, and narrow
commands only.

The Viewer sends a local heartbeat at least every five seconds. A stale local
heartbeat does not automatically prove a process crash: the Agent distinguishes
process exit, renderer unresponsiveness, IPC loss, and server/network loss.

### Updater Helper

The updater is a short-lived executable staged outside the active install
directory. It can replace the Agent that launched it.

Updater responsibilities:

- verify the update transaction handed off by the Agent;
- stop the Viewer startup task and Agent service;
- install the staged release atomically;
- retain the last-known-good release until validation completes;
- restart the Agent service and Viewer startup task;
- roll back when the new Agent fails its validation deadline;
- leave a bounded, redacted result for the restarted Agent to report.

### Windows Installer

The installer requests administrator permission once and performs a complete
per-machine installation. Portable EXE distribution is no longer the primary
deployment model.

The installer must:

- install the Agent, Viewer, updater, and release metadata;
- register the Agent as an automatic Windows service;
- configure Windows Service Control Manager recovery actions;
- register a per-user-session Viewer startup task;
- create protected machine settings and named-pipe permissions;
- register uninstall and repair entries;
- start the Agent and the Viewer when an interactive session exists;
- remove or repair all registered components transactionally.

The service does not display UI from Windows session 0. The startup task runs
the Viewer as the interactive user, while the Agent controls that task and the
Viewer process without granting the renderer service privileges.

## Identity and Registration

The installer or first-run setup collects the CamStation server address and a
viewer display name. The Agent creates a stable random `clientId` once and
preserves it across display-name changes, server reconnects, updates, repairs,
and Viewer reinstalls. Identity reset is a separate explicit administrative
operation.

The first release continues to assume a trusted LAN and does not treat
`clientId` as an authentication secret. Server-side command creation remains
inside the existing administrative console boundary. This accepted risk must
remain visible in the implementation plan and release notes.

## Control Plane

The Agent, not the Viewer, owns the control connection. SSE is the primary
delivery mechanism. Bounded long polling is the fallback when SSE cannot be
maintained. Every command has a unique ID, type, target client, creation time,
optional stream, desired version or route, and state.

First-release command types:

- `ping`;
- `reload_live`;
- `restart_viewer`;
- `restart_agent` through the helper/SCM recovery path;
- `resubscribe_stream` for one Viewer pipeline;
- `restart_stream` for one server-side stream;
- `update_app` with an exact target release;
- `capture_diagnostics`.

Commands are delivered at least once and executed idempotently. The Agent
persists the last completed command IDs so a reconnect or restart cannot repeat
an update or forced restart. States include `pending`, `delivered`,
`acknowledged`, `running`, `succeeded`, `failed`, `rejected`, and `expired`.

When connected, command receipt is acknowledged within five seconds. A queued
command remains durable across server and client restarts. The server may send
one explicit forced restart even when the automatic restart budget is
exhausted; that command is still idempotent and audited.

## Heartbeat and Status Model

The Agent sends a server heartbeat at least every ten seconds containing:

- Agent and Viewer versions;
- Agent service and control-channel state;
- Viewer PID/lifecycle and renderer state;
- current route and mode;
- per-stream transport, playback state, and bounded telemetry;
- update phase and target version;
- automatic recovery counters and cooldown deadlines;
- last completed command and error category.

The server must not infer control health from visible video. It derives and
shows these axes separately:

- Agent: `online`, `control_degraded`, `updating`, `recovering`,
  `recovery_failed`, `offline`;
- Viewer: `running`, `unresponsive`, `crashed`, `restarting`, `failed`;
- stream: `connecting`, `playing`, `stalled`, `fallback`, `recovering`,
  `offline`.

An Agent heartbeat older than 30 seconds makes the client `offline`. A live
Agent heartbeat with an unhealthy command channel makes it
`control_degraded`, even if every video remains visible. The console always
shows the last Agent heartbeat, last command-channel success, and last video
progress as different timestamps.

## Server-Directed Automatic Update

Publishing a release creates immutable metadata with version, filename,
sizeBytes, SHA-256, Authenticode publisher identity, and compatibility fields.
The server records a desired version per client or selected fleet and signals
the Agent with `update_app`. The heartbeat response also carries the desired
version so a missed SSE message converges after reconnect.

Update flow:

1. Agent acknowledges the exact target version and update command.
2. Agent downloads to a staging path without changing the active installation.
3. Agent verifies size, SHA-256, Authenticode signature, and approved publisher.
4. Agent reports `downloaded` and `verified` before launching the updater.
5. Updater stops the Viewer and Agent, installs the staged release, and restarts
   both registered components.
6. The new Agent reconnects with the same `clientId`, command ID, and target
   version, reports post-update validation, and writes a protected local
   success marker only after the server acknowledges that heartbeat.
7. The server marks success only after the new-version heartbeat and Viewer
   health arrive.
8. The updater waits for that success marker. If it does not appear within 120
   seconds, the updater restores the last-known-good release and restarts it.

The client never presents an update button or confirmation dialog. A failed
download does not stop the active Viewer. A failed signature or hash is a hard
rejection and is never bypassed automatically.

## Playback Architecture

The UI and operational layout remain based on the current CamStation React live
workspace, but playback is not the current MSE-only implementation.

WebRTC signaling and media use a reviewed CamStation client-facing path. The
client receives stable public stream identifiers, never camera URLs or raw
go2rtc endpoints. The go2rtc API and RTSP listeners remain loopback-only; any
required WebRTC media listener and candidate configuration is explicitly
scoped, firewall-reviewed, and verified on the trusted monitoring network.

Each visible camera owns an independent state machine with these ordered paths:

1. connect to the camera's public live output through WebRTC;
2. if WebRTC setup or media progress fails, switch that camera to the existing
   same-origin MSE/WebSocket path;
3. if the preferred live source remains unhealthy, try the approved fallback
   stream candidate without exposing its source URL;
4. resubscribe only that stream pipeline;
5. request a bounded server-side stream restart when client resubscription
   cannot restore a server stream known to be unhealthy;
6. restart the whole Viewer only for renderer-wide failure or when at least
   half of visible streams stall while their corresponding server streams are
   healthy.

Stream telemetry includes streamName, transport, connection phase, last binary
receive time, last video currentTime progress, readyState, stalled duration,
reconnect count, fallback use, and bounded error category. Raw URLs and internal
go2rtc details are forbidden.

A stream with no media progress for ten seconds becomes `stalled`. If the
server stream is healthy, isolated client recovery must restore it within 30
seconds or report a terminal per-stream failure. One camera failure must not
remount or reset unrelated camera tiles.

## Bounded Recovery

Recovery counters and cooldowns persist across process restarts so restarting
cannot reset a loop budget.

Control reconnect within one recovery episode uses 1, 2, 5, 10, and 30 second
delays with jitter. After the bounded sequence, the Agent moves to a five-minute
low-frequency probe. A server or network outage does not repeatedly restart the
Viewer.

Escalation rules:

- stale stream: reconnect that transport, switch transport, then resubscribe
  that stream;
- unhealthy server stream: request one bounded server-side stream restart;
- stale Viewer IPC or renderer-wide failure: restart the Viewer;
- simultaneous server-side stream failures: report server degradation and do
  not restart the Viewer;
- Viewer automatic restart budget: at most once per ten minutes and three times
  per hour;
- exhausted Viewer budget: enter `recovery_failed` and stop automatic restarts;
- Agent crash: SCM restarts the service after 5, 30, and 120 second delays for
  the first three failures, performs no fourth automatic restart, and resets
  the failure count only after 24 hours;
- explicit server forced restart: allow one audited, idempotent attempt outside
  the automatic budget;
- successful stable operation resets a recovery episode only after the defined
  30-minute healthy interval, not immediately after process launch.

No recovery path recursively launches itself, and no failure path tight-loops
process creation, downloads, or network requests.

## Security and Data Hygiene

- Accept only credential-free `http:` or `https:` CamStation server URLs.
- Reject file, script, data, custom, malformed, and credential-bearing URLs.
- Run the Electron renderer with Node integration disabled, context isolation,
  sandboxing, and web security enabled.
- Deny unexpected navigation, permissions, new windows, and arbitrary
  downloads.
- Restrict the Agent named pipe to the expected local principals and validate
  protocol version, message length, type, and request ID.
- Require Authenticode and SHA-256 verification for production updates.
- Never expose raw camera URLs, credentials, go2rtc APIs, environment values,
  application-data paths, recordings, or unrestricted logs.
- Diagnostics are whitelist-only, size-bounded, and redacted before leaving the
  client.

## Diagnostics

An approved diagnostic bundle may contain application versions, an OS summary,
service state, Viewer/renderer state, control-channel timings, restart budgets,
recent redacted command results, and per-stream telemetry. It excludes settings
files, environment values, camera URLs and credentials, generated go2rtc
configuration, recordings, arbitrary logs, and raw local paths.

## Acceptance and Stability Gates

Installation and lifecycle:

- one elevated installer run registers and starts every required component;
- the Agent starts after reboot without login;
- the Viewer starts in the interactive session after login;
- repair and uninstall leave no orphan service, task, or process.

Control:

- stale heartbeat or control loss is visible on the server within 30 seconds;
- online commands are acknowledged within five seconds;
- Agent heartbeat continues through renderer crash and unresponsiveness;
- video playback can never make a control-disconnected client appear healthy;
- duplicate delivery cannot repeat a restart or update.

Playback:

- eight cameras complete a 24-hour Windows soak;
- a single camera outage and recovery do not disturb other tiles;
- forced WebRTC failure switches that stream to MSE;
- a stall is detected within ten seconds and isolated recovery completes within
  30 seconds when the server stream is healthy;
- renderer crash, hang, GPU failure, and process termination recover within the
  bounded budgets;
- after warm-up, the 24-hour soak leaves no orphan process and keeps total
  client RSS growth within the larger of 25 percent of baseline or 300 MB.

Update:

- version N updates silently to N+1 and reports every phase;
- interrupted download leaves N running;
- wrong size, hash, signature, or publisher is rejected;
- installation failure or missing new-version heartbeat rolls back to N;
- service and Viewer registration survive update and rollback;
- no update path requires an interactive confirmation.

Recovery-loop prevention:

- network, server, renderer, and stream faults each respect their own budget;
- counters survive restarts;
- budget exhaustion stops automatic restart and reports `recovery_failed`;
- a forced server command performs at most one additional audited attempt.

## Out of Scope for the First Implementation

- native libVLC/mpv playback before the WebRTC/MSE stability gate fails;
- direct client access to cameras or camera credentials;
- legacy CamStation routes;
- interactive client-side update approval;
- unrestricted PC administration beyond the documented CamStation Agent,
  Viewer, stream, diagnostic, and update commands;
- general remote desktop, shell execution, or arbitrary command execution.
