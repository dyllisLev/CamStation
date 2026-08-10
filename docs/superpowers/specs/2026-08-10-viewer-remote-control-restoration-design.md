# Viewer Remote Control Restoration Design

**Date:** 2026-08-10
**Status:** Implemented and WinPC-verified

## Goal

Restore the server-driven Viewer controls that were lost when the standard MSI runtime replaced the
Agent-era Electron pipe. An operator must be able to select a registered Viewer, run one of the five
approved recovery actions, and observe a truthful terminal result without routinely operating the
Windows PC.

The restoration keeps monitoring and control independent. A healthy Windows Service heartbeat does
not imply that the Viewer, renderer, or a stream is healthy; a renderer failure does not stop the
Service from receiving or completing lifecycle commands.

## Supported operator actions

| Korean UI label | Canonical command | Executor | Required input | Success proof |
|---|---|---|---|---|
| 제어 연결 확인 | `ping` | Viewer Service | none | the Service durably accepts and reports the same command ID as succeeded |
| 라이브 화면 새로고침 | `reload_live` | Electron main | none | the approved live URL finishes loading |
| 카메라 영상 다시 연결 | `resubscribe_stream` | Web renderer | one registered stream name | the renderer accepts the targeted resubscribe request |
| Viewer 앱 시작 또는 다시 시작 | `restart_viewer` | Service lifecycle adapter + Electron | audit reason, optional | a different Viewer lease is acquired and the new renderer is ready |
| Viewer 관리 서비스 다시 시작 | `restart_service` | Service restart helper + SCM | audit reason, optional | a later Service boot reconciles the original command and reconnects control |

`restart_agent` remains accepted only as an API/ledger compatibility alias and is normalized to
`restart_service`. It is not shown in the UI. `update_app` remains an internal release command and is
not accepted from the operator command form.

## Command schema and server state

The operator API accepts exactly one JSON object and rejects unknown fields.

- `type` is one of the five canonical operator commands; `restart_agent` is the only accepted alias.
- `streamName` is required only for `resubscribe_stream`, is length bounded, contains no control
  characters, and cannot be URL-like.
- `message` is an optional, bounded audit reason only for the two restart commands.
- `route`, `mode`, update metadata, and caller-selected generation are rejected for operator commands.
- TTL defaults to 300 seconds and is bounded to 30–900 seconds.
- The payload hash is computed after normalization, so duplicate delivery cannot alter semantics.

The server queue remains authoritative for operator-visible state:

```text
pending -> delivered -> acknowledged -> running -> succeeded|failed|rejected|expired
   |           |
   +-> cancelled (pending only)
```

The server may accept a later state directly when a client reconnects and replays a durable result,
but it never moves backward. An operation key, once set, is immutable. `delivered` means transport
delivery only and is never presented as execution. Cancellation is allowed only before delivery;
once a command may have entered the local ledger, the operator cannot claim that it was revoked.

## Monitoring plane

The existing Service heartbeat remains independent from command execution and continues to report:

- management Service/agent state;
- server control connection state;
- Viewer process state;
- renderer state;
- per-stream state and progress;
- installed/update state.

Command handling must not block the heartbeat loop or SSE reader. A bounded worker serializes local
side effects while heartbeat and reconnect loops continue.

## Control plane and durable execution

The Viewer Service stores a bounded command journal below the MSI-owned Viewer ProgramData root. The
journal contains only command identity, payload hash, operation key, state, timestamps, and restart
generations; it contains no server URL, camera URL, credential, raw failure, or arbitrary payload.
Writes use a size-bounded atomic replacement and a SYSTEM/Administrators-only Windows DACL.

For each delivery the Service:

1. validates command identity, schema, TTL, and payload hash;
2. returns a previously persisted terminal result for an exact duplicate;
3. rejects an ID whose payload hash changed;
4. persists acceptance before reporting `acknowledged`;
5. persists `running` before a side effect;
6. executes through the command-specific adapter with a bounded deadline;
7. persists and reports a safe terminal state.

Interrupted UI commands fail safely on redelivery instead of being executed twice. A running
`restart_service` is the exception: the next Service boot generation is its success proof. The new
Service instance reconciles and reports the original command as succeeded from the durable journal;
it does not require the server to redeliver the command.

## Local IPC

Protocol v2 gains an unsolicited event envelope distinct from request responses:

```json
{"version":2,"event":"viewer_command","eventId":"command-42","payload":{...}}
```

Only the active, verified interactive lease connection receives an event. Electron validates the
command and operation key, performs the approved action, and sends a lease-authorized
`command_result` request. Responses and events remain newline framed and limited to 64 KiB.

- `reload_live` can load only the already-derived `/live?viewer=1` URL.
- `resubscribe_stream` forwards only a safe stream name through the preload bridge.
- `restart_viewer` asks Electron to relaunch and exit only after its local acceptance was returned.
- no event carries an executable, argument list, URL, path, script, credential, or shell command.

## Narrow lifecycle adapters

### Viewer restart

When a lease exists, Electron uses its own `app.relaunch()` path. The Service waits for the old lease
to disappear, a different lease to be acquired, and the renderer to report `ready`.

When no lease exists, a Windows-only launcher starts exactly the MSI-installed
`CamStationViewer.exe` adjacent to the running Service executable in the active console user's
session. It rejects missing/non-interactive sessions and does not accept a caller-supplied path or
arguments. Success still requires the new lease and renderer-ready proof.

### Service restart

The Service starts a detached private mode of its own installed executable. That helper can target
only the fixed `CamStationViewerService` SCM name. It requests a normal stop, waits for `Stopped`
within a deadline, starts the same service, waits for `Running`, and exits. The current Service leaves
the journal in `running` with the next boot generation before starting the helper. The restarted
Service increments the boot generation and reconciles the unreported result from the journal.
Arbitrary service names or commands are not accepted.

## Operator UI

The target is a real `<select>` populated from the Viewer registry, not a free-form ID input. Actions
use Korean labels and short descriptions. Only `resubscribe_stream` shows a stream selector populated
from the selected Viewer's reported streams. Restart actions show an optional audit-reason field and
require explicit confirmation. Route and generic message fields are removed.

Rows display the exact Korean state label and timestamp for delivery, acknowledgement, running, and
result. Active rows refresh automatically. Safe error categories are localized; raw local errors are
not returned.

## Failure behavior

- no active Viewer lease for a UI-only action: `rejected/viewer_unavailable`;
- no logged-in interactive session for Viewer start: `rejected/interactive_session_unavailable`;
- renderer or relaunch deadline exceeded: `failed/viewer_restart_timeout`;
- local IPC result timeout: `failed/viewer_command_timeout`;
- expired before acceptance: `expired/command_expired`;
- journal read/write failure: no side effect and `failed/command_journal_unavailable` when reportable;
- service helper launch or initial handoff failure: `failed/service_restart_failed` while the current
  Service can still report it; if Windows accepts the stop but cannot start the Service again, no
  stopped component can claim success and external SCM recovery is required.

All errors exposed to the server are fixed categories. Detailed local failures stay in redacted,
bounded Service logs.

## Verification

Source verification includes store/route transition tests, Service journal/duplicate/restart tests,
IPC framing and Electron action tests, Web model tests, full Go tests, Viewer tests, Web lint/build,
Windows cross-build, MSI manifest checks, secret scan, and `git diff --check`.

Real WinPC acceptance installs the exact locally reviewed MSI and issues each of the five commands
through the normal server API. Evidence must pair the final server row with bounded process/UIA
observations. Viewer and Service restart proofs require changed process/boot identity, reconnect, and
renderer-ready/control-online respectively. The existing one-shot Viewer-window capture harness is
used; no new remote desktop or persistent automation service is installed.

## Implementation result

Implemented on 2026-08-10 and verified with unsigned development MSI `2.0.24` on the authorized
Windows 11 PC.

- The full repository check passed: all Go packages, 58 Web tests, 36 Viewer tests, Web lint/build,
  Viewer build, embedded Web regeneration, and daemon build.
- Native Windows Viewer tests, Service Windows tests, Electron packaging, and WiX MSI validation
  passed. The artifact was `124436480` bytes with SHA-256
  `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160`.
- All five commands reached a truthful terminal success through the normal server control path.
  Viewer restart changed the entire Viewer process set and recovered a new lease/renderer-ready
  state. Service restart changed the Service PID and boot generation, re-reported the original
  durable result without duplicate execution, and recovered control while keeping the Viewer alive.
- A real `/viewers` interaction selected the registered Viewer and `ping` action, submitted it with
  keyboard focus, received HTTP 201, and automatically displayed the succeeded row and timestamps.
- The implementation review found and closed a monitoring migration gap: stream telemetry previously
  received a local success response but was discarded by the Service. Lease, renderer, progress, and
  bounded stream state now flow through the independent heartbeat path; a displayed offline stream
  was selectable and successfully targeted during WinPC acceptance.

Production signing and long-running Windows soak remain release work. `restart_stream` stays on the
server Streams surface, and diagnostics collection is not advertised as a Viewer command.
