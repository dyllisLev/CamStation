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
- Optimize for stability on a trusted internal network. Do not add client
  login, pairing, command signing, or public-Internet threat machinery to the
  first implementation; retain only the safeguards needed to prevent corrupt
  updates, accidental cross-client actions, and local process confusion.
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
- maintain an update ledger keyed by target version, artifact digest, and
  command generation;
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
- persist command intent before side effects and reconcile interrupted commands
  after restart;
- supervise the Viewer process and its local IPC heartbeat;
- start, stop, and force-restart the Viewer through the registered startup task;
- collect bounded Viewer and stream telemetry;
- download and verify update releases;
- quarantine failed target releases so rollback cannot immediately reinstall
  the same artifact;
- launch the updater helper and report update progress;
- enter explicit degraded or failed states when bounded recovery is exhausted.

Machine state lives under a protected machine-wide application-data location,
not Electron userData. Restart counters, command IDs, update transaction state,
the configured monitoring-user SID, and the last-known-good version survive
process and machine restarts. State records use an explicit schema version and
remain readable by both the active and last-known-good releases.

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

### Local IPC and Windows Session Ownership

The first implementation supports one configured monitoring user and one
interactive console Viewer. The elevated installer records that user's SID and
registers a logon task for that principal. RDP or other user sessions do not
start additional Viewers. When the configured user is not logged in, the Agent
remains controllable and reports the Viewer as `not_logged_in`.

The Agent and Viewer communicate over a local named pipe owned by the service.
Its DACL permits only LocalSystem, the Agent service SID, and the configured
monitoring-user SID; remote pipe clients are rejected. The Agent issues a new
per-launch nonce when it starts the Viewer task, and accepts telemetry only from
the process and console session that register with that nonce. This is a
stability guard against stale or duplicate Viewer processes, not a general
same-user security boundary.

The protocol is versioned, length-bounded JSON with request IDs. It carries
Viewer heartbeat, renderer status, stream telemetry, and narrow commands only.
The Agent assigns the accepted Viewer process tree to a Windows Job Object so a
graceful-stop deadline followed by forced termination cannot leave orphan
Electron children.

The Viewer sends a local heartbeat at least every five seconds. A stale local
heartbeat does not automatically prove a process crash: the Agent distinguishes
process exit, renderer unresponsiveness, IPC loss, and server/network loss.

### Updater Helper

The updater is a short-lived executable staged outside the active install
directory. It can replace the Agent that launched it.

Installed releases live in immutable version-and-digest directories. The SCM
service and Viewer task target stable bootstrap executables whose registration
does not change during an update. Activation changes one protected current-
release pointer; it never rewrites the active release in place.

Updater responsibilities:

- verify the update transaction handed off by the Agent;
- persist every phase in a protected update journal before performing it;
- stop the Viewer startup task and Agent service;
- install the staged release atomically;
- retain the last-known-good release until validation completes;
- restart the Agent service and Viewer startup task;
- roll back when the new Agent fails its validation deadline;
- reconcile an interrupted install or rollback idempotently on the next boot;
- leave a bounded, redacted result for the restarted Agent to report.

The journal records the transaction ID, command generation, old and new release
digests, previous current-release pointer, phase, and rollback state. On every
service start, the stable bootstrap checks this journal and runs the updater
reconciler before launching an Agent release. The installer also registers a
boot recovery task as a secondary path when an incomplete journal exists.
Machine state is backed up before activation; state migrations must either be
backward-readable by the last-known-good Agent or restore that
transaction-bound backup during rollback.

### Windows Installer

The installer requests administrator permission once and performs a complete
per-machine installation. Portable EXE distribution is no longer the primary
deployment model.

The installer must:

- install the Agent, Viewer, updater, and release metadata;
- register the Agent as an automatic Windows service;
- configure Windows Service Control Manager recovery actions;
- register a per-user-session Viewer startup task;
- bind that task to the configured monitoring-user SID and single console
  session policy;
- register the incomplete-update boot recovery task;
- create protected machine settings and named-pipe permissions;
- register uninstall and repair entries;
- start the Agent and the Viewer when an interactive session exists;
- remove or repair all registered components transactionally.

The service does not display UI from Windows session 0. The startup task runs
the Viewer as the interactive user, while the Agent controls that task and the
Viewer process without granting the renderer service privileges.

## Identity and Registration

The installer collects the CamStation server address, Viewer display name, and
monitoring-user SID during the single elevated installation. It defaults the
SID to the console user that launched elevation, allows an administrator to
select another local user, and exposes the same values as unattended installer
properties. No post-install first-run input is required. The Agent creates a
stable random `clientId` once and preserves it across display-name changes,
server reconnects, updates, repairs, and Viewer reinstalls. Identity reset is a
separate explicit administrative operation.

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
- `update_app` with an exact target release, artifact digest, and command
  generation;
- `capture_diagnostics`.

Commands are delivered at least once and executed idempotently. The Agent
persists receipt, command ID, payload hash, TTL, and `running` intent before any
side effect. On restart it reconciles an in-flight operation from the durable
ledger and replays its last known result instead of blindly executing it again.
Update, Agent restart, and Viewer restart operations are serialized; an update
supersedes queued reload or automatic restart work. Expired commands never
execute. States include `pending`, `delivered`, `acknowledged`, `running`,
`succeeded`, `failed`, `rejected`, and `expired`.

Side-effecting commands use their command ID as a durable operation key.
`restart_viewer` converges to one Viewer launch generation and nonce;
`restart_agent` records the expected next Agent boot generation before invoking
the helper; and `update_app` uses the transaction journal. After a crash, the
Agent first observes those generation markers and the current process/release
state. It completes a partially applied operation or reports the already
reached state; it does not start a second restart or install transaction.

When connected, command receipt is acknowledged within five seconds. A queued
command remains durable across server and client restarts. The server may send
one explicit forced restart even when the automatic restart budget is
exhausted; that command is still idempotent and audited.

The server sends an SSE keepalive at most every ten seconds. The Agent uses a
25-second read deadline; receiving no event or keepalive by that deadline closes
SSE and sets `control_degraded`. Long polling starts immediately and counts as
healthy only when a request is actively pending or completed successfully
within the same 25-second window. SSE reconnect continues in the background
using the bounded control backoff. A half-open SSE socket never counts as a
healthy command channel merely because the separate heartbeat request works.

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
sizeBytes, SHA-256, Authenticode signer certificate thumbprint, and
compatibility fields. Command generation belongs to each server update command
and its attempt ledger, not to the immutable release.
The server records a desired version per client or selected fleet and signals
the Agent with `update_app`. The heartbeat response also carries the desired
version so a missed SSE message converges after reconnect.

Update flow:

1. Agent acknowledges the exact target version and update command.
2. Agent downloads to a staging path without changing the active installation.
3. Agent verifies size, SHA-256, the Windows Authenticode chain and code-signing
   usage, and the signer thumbprint installed with the Agent. The internal-LAN
   server metadata is not a separate cryptographic trust root.
4. Agent reports `downloaded` and `verified` before launching the updater.
5. Updater stops the Viewer and Agent, installs the staged release, and restarts
   both registered components.
6. The new Agent reconnects with the same `clientId`, transaction ID, command
   generation, and target digest, then starts post-update validation.
7. The server returns a transaction-bound commit token only after observing the
   expected Agent version and digest, healthy Viewer IPC, a ready renderer, and
   30 seconds of stable Viewer heartbeat. If streams were progressing before
   update, at least one must resume progress or be classified by fresh server
   health as source-unavailable.
8. The new Agent writes the protected commit marker only after receiving that
   exact token. The updater accepts a marker only when its transaction ID,
   generation, and digest match the journal.
9. If the matching marker does not appear within 120 seconds, the updater
   restores the last-known-good release and transaction-bound state backup.

The client never presents an update button or confirmation dialog. A failed
download does not stop the active Viewer. A failed signature or hash is a hard
rejection and is never bypassed automatically.

Each target version, artifact digest, and command generation has an independent
attempt ledger. Download failures retry at most three times with 1, 5, and 30
minute delays. Hash, signature, or compatibility failure performs no automatic
retry. Installation runs once per generation. Any rollback or hard rejection
quarantines that target and leaves the last-known-good version active. The
server must publish a new digest or increment an explicitly audited command
generation to rearm it. A normal update never installs a lower version; only the
transaction's own last-known-good rollback or an explicit audited downgrade
generation may do so.

## Playback Architecture

The UI and operational layout remain based on the current CamStation React live
workspace, but playback is not the current MSE-only implementation.

WebRTC signaling uses the same-origin `/player/api/ws` route already proxied by
CamStation. Before proxying, CamStation requires the configured console Origin
and validates `src` against the registered camera's public output names or a
short-lived preview ID. Other go2rtc API paths remain denied. The client
receives stable public stream identifiers, never camera URLs or raw go2rtc
endpoints.

The go2rtc API and RTSP listeners remain loopback-only. WebRTC media uses the
fixed TCP/UDP port 8555 on selected server LAN addresses, with candidates
limited to those addresses and a host-firewall rule limited to the monitoring
LAN. Container, VPN, link-local, and unrelated interface candidates are not
advertised. This is an operational connectivity boundary for a trusted LAN,
not an Internet exposure design.

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

Each WebRTC or MSE connection attempt has a five-second setup/progress deadline.
The complete active recovery episode, including transport changes and the one
isolated resubscribe, is capped at 30 seconds from stall detection. When that
deadline expires, the stream reports a terminal episode result and enters its
cooldown; it does not continue cycling candidates in the background.

Server stream health is authoritative only when sampled within the last 15
seconds. `healthy` requires an active producer and increasing media-byte or
packet counters across two samples; an on-demand stream with no client is
`idle`, not unhealthy. A client stall with fresh healthy server evidence uses
client recovery. Fresh non-progressing server evidence may request one bounded
server restart. Missing or stale server evidence produces `unknown` and cannot
trigger a Viewer-wide or server-stream restart.

## Bounded Recovery

Recovery counters and cooldowns persist across process restarts so restarting
cannot reset a loop budget.

Control reconnect within one recovery episode uses 1, 2, 5, 10, and 30 second
delays with jitter. After the bounded sequence, the Agent moves to a five-minute
low-frequency probe. A server or network outage does not repeatedly restart the
Viewer.

Escalation rules:

- stale stream: attempt the initial WebRTC connection and one WebRTC reconnect,
  then try MSE primary and one approved MSE fallback candidate once each;
- exhausted stream attempts: perform one isolated resubscribe, then enter a
  five-minute per-stream cooldown with only one low-frequency probe;
- stable playback for five minutes resets that stream recovery episode;
- unhealthy server stream: request at most one server-side stream restart per
  ten minutes;
- stale Viewer IPC or renderer-wide failure: restart the Viewer;
- multi-stream escalation: restart the Viewer only after isolated episodes are
  exhausted for at least two streams and at least half of visible streams, with
  fresh server evidence that each affected stream is healthy;
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

- The first implementation assumes a trusted internal LAN. It does not add
  client login, pairing tokens, mutual TLS, signed commands, or a general remote
  administration security layer.
- Accept only credential-free `http:` or `https:` CamStation server URLs.
- Reject file, script, data, custom, malformed, and credential-bearing URLs.
- Run the Electron renderer with Node integration disabled, context isolation,
  sandboxing, and web security enabled.
- Deny unexpected navigation, permissions, new windows, and arbitrary
  downloads.
- Restrict the Agent named pipe to the documented local SIDs and launch nonce,
  and validate protocol version, message length, type, request ID, PID, and
  console session. Same-user hostile-process resistance is an accepted risk.
- Require Authenticode signer-thumbprint and SHA-256 verification so a corrupt
  or unintended update cannot replace a stable client.
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
- server address, display name, and monitoring user are all committed by that
  installer, with no required first-run setup;
- the Agent starts after reboot without login;
- the Viewer starts in the interactive session after login;
- repair and uninstall leave no orphan service, task, or process.
- only the configured monitoring user starts one Viewer in the console session;
  additional RDP/user sessions create none.

Control:

- stale heartbeat or control loss is visible on the server within 30 seconds;
- a silent or half-open SSE connection transitions to long-poll fallback and
  `control_degraded` within 25 seconds;
- online commands are acknowledged within five seconds;
- Agent heartbeat continues through renderer crash and unresponsiveness;
- video playback can never make a control-disconnected client appear healthy;
- duplicate delivery cannot repeat a restart or update.
- crash after a durable command enters `running` reconciles that operation and
  cannot repeat its side effect.

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
- a fault-free 24-hour interval has no unexplained Agent or Viewer restart and
  no server-healthy stream remains stalled longer than 30 seconds;
- current infinite MSE candidate cycling is replaced by the finite episode and
  cooldown defined above.

Update:

- version N updates silently to N+1 and reports every phase;
- interrupted download leaves N running;
- wrong size, hash, signature, or publisher is rejected;
- installation failure or missing new-version heartbeat rolls back to N;
- the failed target remains quarantined and is not retried until a new digest or
  audited command generation arrives;
- service and Viewer registration survive update and rollback;
- no update path requires an interactive confirmation.
- simulated power loss at download, staging, pre-stop, post-stop, activation,
  Agent restart, validation, and rollback boundaries always recovers either the
  new committed release or the last-known-good release;
- versioned machine state remains readable after both update and rollback.

Recovery-loop prevention:

- network, server, renderer, and stream faults each respect their own budget;
- counters survive restarts;
- budget exhaustion stops automatic restart and reports `recovery_failed`;
- a forced server command performs at most one additional audited attempt.
- unauthorized local pipe users are rejected, stale launch nonces cannot
  register, and terminating the Viewer leaves no Electron process tree behind;
- four forced Agent failures verify the three configured SCM recovery actions
  and no fourth automatic service restart.

## Out of Scope for the First Implementation

- native libVLC/mpv playback before the WebRTC/MSE stability gate fails;
- direct client access to cameras or camera credentials;
- legacy CamStation routes;
- interactive client-side update approval;
- unrestricted PC administration beyond the documented CamStation Agent,
  Viewer, stream, diagnostic, and update commands;
- general remote desktop, shell execution, or arbitrary command execution.
