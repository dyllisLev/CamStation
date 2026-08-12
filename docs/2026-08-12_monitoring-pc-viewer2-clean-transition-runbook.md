# Monitoring PC Viewer 2.0 clean-transition runbook

Use this runbook only after explicitly selecting `monitoring-pc`. Use `test-pc` for rehearsal, but
repeat every acceptance check on `monitoring-pc`; the test PC cannot establish the final display or
monitoring-LAN result.

## Define clean state correctly

Start the server from a fresh 2.0 state. Import only the camera connection graph and the approved
layout. The connection graph includes camera identity/name, stable stream key, enabled state, primary
input, and configured substream input. It does not include historical video or operational history.

Require these server invariants before changing the monitoring PC:

- 9 registered cameras, 8 enabled, and 1 disabled (`소방서2`).
- One saved layout with 8 items, a 48×48 grid, and the timeline collapsed.
- No imported recording rows/files, events, incidents, jobs, backup marks/history, Viewer registry,
  Viewer commands/telemetry, diagnostic artifacts, presets, or profile templates; each excluded table
  count is zero in the fresh target.
- Fresh 2.0 defaults for general settings. Do not copy the 1.x settings document as a shortcut.
- Layout fingerprint
  `8caf84d1e2f80db41c9614a49af1ce39f13ff172df7859bcea46b57f55272c6d`.

The layout is server state, not a legacy Windows profile artifact. Viewer 2.0 fetches
`GET /api/layouts`, and its Viewer page resolves the first saved layout. Therefore the fresh target
must expose exactly one layout named `기본`; its eight `streamName` keys must equal the enabled-camera
set exactly, and it must not contain disabled `소방서2`. Do not copy a 1.x Electron local-storage
layout key or cache to make the screen appear correct.

The approved layout geometry is:

| Camera | x | y | w | h | minW | minH |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 집-마당 | 0 | 0 | 24 | 24 | 8 | 8 |
| 집-창고1 | 24 | 0 | 12 | 12 | 8 | 8 |
| 집-창고2 | 36 | 0 | 12 | 12 | 8 | 8 |
| 소방서1 | 0 | 24 | 24 | 24 | 8 | 8 |
| 소방서3 | 24 | 12 | 12 | 12 | 8 | 8 |
| 소방서4 | 36 | 12 | 12 | 12 | 8 | 8 |
| 염소장 | 36 | 24 | 12 | 13 | 8 | 8 |
| 소방서5 | 24 | 24 | 12 | 13 | 8 | 8 |

Camera URLs and credentials remain inside the protected importer/DB boundary. Verify counts,
enabled names, stream keys, and hashes without printing raw inputs. A matching row count with a
different coordinate, stream key, or enabled state is a failed transition.

Clean monitoring-PC state means one accepted Viewer 2.0 product/service/startup path and no active
or auto-starting legacy Viewer. Do not copy legacy Electron profiles, cache, local storage, registry
configuration, shortcuts, startup entries, scheduled tasks, or installation files into Viewer 2.0.
The CamStation Windows-control driver is separate authorized maintenance infrastructure and remains
installed with telemetry off and no listener/firewall rule.

## Gate the server first

1. Freeze camera and layout edits.
2. Run the production camera-layout importer against an online 1.x snapshot and a fresh 2.0 target.
3. Require import/verify/idempotency success, camera `9/8/1`, substream count 9, layout `1/8`, the
   canonical layout fingerprint, and zero excluded-state rows.
4. Start the approved immutable Docker 2.0 image and require exact revision, `healthy`, restart 0,
   streaming 8, recorder running 8, the monitoring-LAN WebRTC listener/candidate, and a public health
   response. Do not configure the PC against a canary three-camera DB.
5. Query the redacted camera/layout APIs and calculate the layout fingerprint again. Never expose raw
   camera URLs while proving this gate. Require `GET /api/cameras` to return 9/8/1 and
   `GET /api/layouts` to return the single `기본` 48×48 layout above. Compare the enabled camera
   `streamName` set to the eight layout item keys in both directions; a missing, duplicate, disabled,
   or extra tile is a blocker.

Stop before touching the monitoring PC if any server invariant fails.

## Observe the selected PC

Run:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target monitoring-pc --mode status
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target monitoring-pc --mode desktop-capture
```

Inspect the desktop PNG directly. Then use a read-only `system` script to inventory, without broad
name matching:

- installed Viewer products by product code, version, publisher, and install location;
- `CamStationViewerService` status, start mode, PID, and executable path;
- current interactive Viewer windows/process tree and their exact executable paths;
- startup-folder entries, Run/RunOnce values, Viewer scheduled tasks, shortcuts, and legacy product
  uninstall records; and
- current CPU/memory/display/session state.

Record exact legacy window IDs, PIDs, executable paths, product identity, and startup targets. Do not
turn those observed PIDs into a profile rule, and do not use a broad process-name stop.

## Apply Viewer 2.0 configuration through the management pipe

Create a local ignored UTF-8 JSON file with exactly four fields:

```json
{
  "schemaVersion": 1,
  "serverUrl": "https://approved-production-origin.example",
  "displayName": "approved-display-name",
  "autoStart": true
}
```

Invoke:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs \
  --target monitoring-pc \
  --mode viewer-configure \
  --configuration <local-viewer-config.json>
```

The wrapper validates the exact schema and HTTP(S) origin, hashes and transfers the file, and calls
the canonical Windows launcher. A one-shot `TASK_LOGON_INTERACTIVE_TOKEN` task runs
`Invoke-CamStationViewerConsoleLaunch.ps1` in the selected user's active session. That helper sends
protocol version 2 `configure` to the local `CamStationViewerService` management pipe. The service
validates server compatibility/registration and writes `HKLM\Software\CamStation\Viewer`.

Require the requested public fields, running service, matching target/session, matching configuration
SHA-256, `TaskDeleted=true`, and removed run/request paths. Never supply, print, or overwrite
`clientId`; the service preserves the private Viewer 2.0 identity when one exists and creates it for a
fresh configuration otherwise.

## Switch the visible application

1. Close the exact legacy top-level window gracefully with a fresh PID/window ID and verify its window
   count reaches zero. Stop an exact remaining PID only after re-resolving path, session, and parent.
2. Disable/remove the exact legacy startup entry and uninstall the exact legacy product when the clean
   transition has been approved. Re-enumerate each location and require zero legacy startup/product
   records. Do not preserve a CamViewer PID set as a success condition.
3. Launch the installed Viewer 2.0 in the active session with `viewer-capture --viewer-operation
   LaunchAndCapture`. Require one top-level Viewer window and a live renderer rather than a setup page.
4. Use a fresh Plan/UIA observation to invoke the visible `전체화면` control. This is Electron native
   fullscreen through `viewer:set-fullscreen`, not Windows maximize. Verify fullscreen state and fresh
   desktop geometry; never reuse remembered coordinates.

Do not uninstall legacy software before the server gate and Viewer 2.0 configuration succeed. Once
the user chooses a clean final state, rollback material belongs in the server/package archive, not as
an active legacy process or startup entry on the monitoring PC.

## Prove the operational result

Capture both the exact Viewer window and the complete desktop:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target monitoring-pc --mode viewer-capture --viewer-operation Capture
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target monitoring-pc --mode desktop-capture
```

Open both PNGs with `view_image`. Require the approved geometry above and eight distinct live video
frames; green labels or DOM/UIA nodes alone are insufficient. No tile may show connecting,
reconnecting, transport fallback, alternate/focus stream, blank, or frozen state.

Correlate the same session with server-side safe logs and Viewer telemetry:

- eight `live` streams in `playing` state;
- WebRTC attempt 1 for all eight;
- retry 0, MSE switch 0, alternate/focus fallback 0;
- renderer `ready`, Viewer `running`, and recent progress timestamps; and
- actual monitoring-PC UDP media traffic to the advertised WebRTC port when packet proof is needed.

Then perform one explicitly approved logoff/logon or reboot. Require the Viewer 2.0 service to return
Running/Automatic, one auto-started Viewer 2.0 window in native fullscreen, the same 8-tile layout,
and eight live streams again.

Final clean-state audit requires zero legacy Viewer processes, windows, startup entries, scheduled
tasks, shortcuts, product registrations, and legacy profile/cache roots; exactly one accepted Viewer
2.0 product and service; zero CamStation one-shot tasks/runs; and unchanged SSH/Cua listener and
firewall counts. Remove only exact observed legacy targets, never unrelated Electron or user data.
