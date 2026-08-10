# Implementation Status

Last updated: 2026-08-09

This document records the current implementation state so the next session can continue without re-discovering the same context.

## Current Branch And Remote

- Repository: `https://github.com/dyllisLev/CamStation.git`
- Active branch used for this work: `camstation2-initial`
- Latest Windows Viewer implementation commit before publication: `ff0dca0 fix(viewer-app): hold updater ownership during recovery`
- Runtime test URL on the camera-reachable server: `http://10.0.0.29:18080/`
- Main monitoring page: `http://10.0.0.29:18080/live`

## Implemented

### 2026-08-09 Windows-local Viewer MSI build entry point

- `scripts/build-viewer-msi.ps1` is the repository-owned one-command entry point for a dedicated
  x64 Windows developer machine or VM. It verifies Windows/x64, Node.js 22+, Go 1.25+, .NET SDK
  8.x, Git, and the explicit `-UnsignedDevelopment` policy before doing build work.
- The pipeline uses an ignored, unique workspace; runs Viewer and service tests; packages Electron
  for Windows; embeds the requested Viewer-service version; generates a fresh WiX fragment; uses
  locked WiX restore; and checks MSI identity, fixed UpgradeCode, File-table count, SHA-256, and
  unsigned status before publishing versioned artifacts and secret-free metadata.
- Linux-side source validation passes: 29 Viewer tests, TypeScript build, Windows Electron package,
  Viewer-service Go tests, PowerShell 7.5 parser, non-Windows fail-closed gate, temporary x64 service
  cross-build, fresh-fragment generation, and tracked-input hash preservation.
- No new MSI has been produced on this Linux host. A dedicated Windows x64 host must still run the
  documented command and prove the resulting MSI database and artifact set. The NUC monitor PC is
  strictly an install/repair/uninstall target and must not receive build tools or source caches.
- Build command, prerequisites, outputs, and troubleshooting are in
  [the installer guide](../installer/README.md).

### 2026-08-09 Docker home-camera canary and dedicated Viewer

- A hardened all-in-one 2.0 image now runs as `camstation2-canary` on production host
  `10.0.0.26`, publishing only HTTP `18081/tcp`. The current immutable image is
  `camstation:2.0.0-rc.20260809.7-canary` with image ID
  `sha256:628da2dbd0a7bbe94280d45284fe975617e3b8a56e02f8389db4ca84d68202e9`.
- The canary DB was created only from the active 1.0 go2rtc YAML through a strict `집-` allowlist.
  It contains `집-마당`, `집-창고1`, and `집-창고2`; every fire-station camera and `염소장`
  are absent. The original YAML hash and all five legacy service PID/restart baselines remained
  unchanged.
- The new top-level `/viewer` is a chrome-free read-only surface matching the operator-designated
  1.0 Viewer contract. At 393×852 it played all three streams through same-origin MSE, had no
  page overflow, supported single-camera focus/return, and survived direct-route reload.
- The container task limit is now 1024, sized for the final eight-camera fleet rather than the
  three-camera subset. The prior 256 limit had reached peak 256 with 343 rejected task creations.
  After recreation, three simultaneous Viewer pages sustained all nine MSE videos for about
  160 seconds at exactly three viewers per live stream; peak PID was 230 with zero new limit hits,
  OOMs, or CPU throttling. This capacity change did not enable any additional camera.
- All three camera states and recorder workers are healthy. Browser, live ffprobe, finalized
  60-second MP4, secret-safe generated-config, log, port, resource, and 1.0 continuity gates passed.
  The canary intentionally has `restart: "no"` and must be started manually after a server reboot.
- On 2026-08-10 KST, the operator-requested test-recording cleanup removed 1,632 finalized canary
  MP4 files totaling 9,245,386,547 bytes through the checked recording DELETE API. The zero-file
  checkpoint retained three growing active temp segments, three running recorder workers, three
  streaming home cameras, container restart 0, and unchanged legacy 1.0 unit PIDs/restart counts.
  The deleted rows are pending-backup tombstones and are not recoverable through CamStation.
  Recording remains enabled, so new one-minute files continue after that checkpoint.
- Exact access, status, log redaction, start/stop, state backup, image update, rollback, and
  troubleshooting instructions are in the
  [Docker canary operations document](2026-08-09_camstation2-docker-canary-operations.md).

### 2026-08-09 production cutover preparation

- Exact transitional compatibility route: `GET /new?viewer=1` returns a non-cacheable `302`
  to `/live?viewer=1`; broader queries, methods, and subpaths have negative tests.
- Shared stable camera-key validation admits bounded Unicode/Hangul identifiers while rejecting
  path, URL, whitespace, control, invalid UTF-8, boundary-hyphen, and oversized values at both
  HTTP persistence and store boundaries.
- `camstation-migrate` supports online `snapshot`, `inspect`, `dry-run`, fresh atomic `import`,
  repeat-safe `already-current`, and read-only `verify` for the 1.x SQLite schema.
- Migration manifests expose camera/layout/settings invariants and URL fingerprints without raw
  stream or ONVIF credentials. Synthetic tests prove `9/8/1`, nine sub streams, one/eight layout,
  30/30/700 settings, three outputs, pending policies, mode 0600, no overwrite, and no secret output.
- Fresh 2.0 settings no longer contain a development backup remote. Disabled backup accepts an
  empty target with `protectUnbacked=true`; enabling backup still requires an explicit target.
- Production packaging now includes a hardened `camstationd-2x.service`, daemon environment
  template, legacy/maintenance/2.0 nginx locations, immutable bundle builder, release staging,
  online state preparation, legacy-preserving nginx preparation, preflight, exact single-active
  switch, and exact server rollback helpers.
- Actual production inspection found three legacy sub values that are local go2rtc ffmpeg
  recipes. The importer maps the exact loopback/self-key H.264 form to a recording-backed live
  output rather than creating a recursive input; all other producer expressions are rejected.
- Release `2.0.0-rc.20260809.5` is installed on the production server with a verified online
  snapshot and inactive 2.0 DB (`9/8/1`, layout 1/8, 30/30/700, blocker 0). Nginx is prepared
  with its active link still on legacy, all 1.x services remain active, and full preflight passes.
  The 2.0 unit remains inactive/disabled and switch approval remains `NO`; switch/rollback also
  transfer boot enablement between the exact generations. Real 2.0 camera,
  recorder, backup, Viewer, and rollback gates remain in the
  [readiness report](2026-08-09_cctv-2x-cutover-readiness-report.md).

- Go backend skeleton under `cmd/camstationd`
- Embedded React/Vite frontend served by the Go daemon
- SQLite store and migrations under `internal/store`
- Camera registration/listing/edit/delete API
- Camera profile-template CRUD API:
  - `GET /api/camera-profiles`
  - `POST /api/camera-profiles`
  - `GET /api/camera-profiles/{id}`
  - `PUT /api/camera-profiles/{id}`
  - `DELETE /api/camera-profiles/{id}`
- Reusable camera profile templates are separate from camera instances:
  - templates store manufacturer/model/adapter match rules and credential-free channel mappings
  - cameras can store `profileTemplateId` provenance
  - camera role streams are saved as a snapshot when the camera is registered or updated
  - deleting a profile template is blocked while a camera references it
- Camera scan now returns device discovery data plus profile-template matches, not a camera-as-profile object.
- Camera management mutations require the trusted console management header/origin/fetch-site guard.
- Camera scan/probe/preview targets are bounded to safe private camera targets and redacted public errors.
- Camera URL redaction in API responses and events
- ffprobe-based camera probe helper
- Persistent per-camera stream-output policy:
  - immutable `recording` / `live` input keys and exactly three `recording` / `live` / `focus` outputs
  - desired/applied revisions stored in SQLite with optimistic revision checks
  - per-output source, `auto` / `copy` / software H.264, resolution, FPS, audio, and activation settings
  - serialized go2rtc/recorder apply, last-good rollback, 200/202/409/503 result separation
  - manual and bulk input probe plus reapply APIs
  - RTSP probe fallback through the current applied private go2rtc input when a single-connection camera is occupied; HTTP-FLV remains original-source probed
  - public DTOs restore desired/applied/effective/verification/runtime state without exposing source URLs or internal endpoints
- go2rtc managed as a child process by `camstationd`
- go2rtc API/RTSP bound locally, with CamStation proxying allowed player paths
- Health, events, stream status, stream restart, camera probe endpoints
- Korean default UI and language setting menu
- Live monitoring workspace at `/live`
- Dedicated monitoring viewer at `/viewer`:
  - bypasses the desktop console shell and fills the available viewport
  - renders every enabled camera in the first saved layout, with a deterministic fallback layout
  - maps saved grid geometry to bounded percentages without exposing layout editing controls
  - uses the applied live output for normal tiles and the applied focus output for single-camera focus
  - reuses bounded MSE playback recovery and returns safely when a focused camera is removed
- Live PTZ control for capability-advertised cameras:
  - guarded ONVIF continuous pan/tilt/zoom and explicit Stop
  - Stop is the final ordered command with a 2-second HTTP and device timeout backstop
  - home navigation and confirmation-gated home-setting action
  - camera-owned preset list/create/goto/delete
  - operator preset names persisted in SQLite by camera and opaque preset token across refreshes and daemon restarts
  - `/live` toolbar capability gating and full right-panel replacement
  - listen/talk/siren controls remain disabled until their transport or protocol is implemented
  - final verification used one bounded real-camera movement and temporary-preset session
- Live page based on the existing CamStation monitoring screen concept:
  - top command bar
  - live/recordings/settings navigation
  - saved layout selector
  - layout save
  - save as new layout
  - saved layout deletion with confirmation and deterministic fallback selection
  - saved layout initialization waits for both camera and layout queries, preventing camera-first navigation races
  - right panel toggle
  - timeline toggle
  - fullscreen toggle
  - right panel with saved layouts and camera status
  - bottom two-row timeline shell
- MSE live video playback without browser video controls
- Browser MSE errors, initial-media silence, and media stalls trigger bounded reconnects
- Normal tiles fall back from the live output to the browser-safe focus output without mutating camera policy
- Tile status reflects browser media receipt and identifies fallback playback
- go2rtc URL-only producer placeholders are reported idle instead of running
- Video progress/control overlays hidden and avoided by direct MSE `<video>` use
- Camera tile movement and resizing through `react-grid-layout`
- Visible resize handles
- Unified UI styling between the monitoring workspace and console pages:
  - shared dark monitoring palette
  - cyan/teal active controls
  - matching panel, table, button, and form styling
- Layout persistence API:
  - `GET /api/layouts`
  - `POST /api/layouts`
  - `PUT /api/layouts/{id}`
  - `DELETE /api/layouts/{id}`
- Layout state saves:
  - tile position
  - tile size
  - timeline collapsed state
  - per-camera video wheel zoom state
- Focus behavior:
  - `집중 보기` no longer opens a new player window
  - clicking `집중 보기` toggles in-page tile enlargement
  - normal `/live` tiles use the applied per-camera live output
  - the enlarged focus tile uses the applied per-camera focus output
  - the focused camera's normal live MSE component is unmounted while focus is active, avoiding simultaneous live/focus transcodes
  - enlarged tile button changes to `집중 보기 종료`
  - double-click on a tile also toggles in-page tile enlargement
  - `Escape` exits the in-page tile enlargement
- Video wheel zoom behavior:
  - mouse wheel over a video zooms the video content
  - drag while zoomed pans the video content
  - double-click on the video frame resets video zoom
  - zoom badge such as `1.3x` appears while zoomed
  - `videoZoom: { scale, tx, ty }` is stored in each layout item
  - page refresh restores saved video zoom and pan
- Initial recording foundation:
  - `recording_segments` SQLite table
  - recorder manager package
  - ffmpeg segment command builder
  - temp-to-recordings finalization flow
  - recorder status/start/stop API
  - single-stream start/stop using `?stream={streamName}`
  - `/api/timeline` now reads recording segment rows
- Recording capacity cleanup:
  - `-max-storage-gb` / `CAMSTATION_MAX_STORAGE_GB` enables automatic cleanup
  - startup cleanup runs once when max storage is configured
  - segment-complete cleanup runs after a temp segment is moved to recordings
  - `GET /api/recordings/storage` exposes recordings/temp usage and configured max storage
  - `POST /api/recordings/cleanup` can run a manual capacity cleanup with `maxBytes` or `maxStorageGB`
  - only completed `ready` segments are deleted
  - active `recording` temp segments are not deletion candidates
  - deleted segments are marked `deleted` in SQLite so timeline queries exclude them
- Interrupted recording recovery:
  - startup recovery finds leftover `recording` / `finalizing` segment rows before workers start
  - temp files from interrupted rows are moved to `data/quarantine/temp/{date}/{stream}/`
  - interrupted rows are marked `failed` with `interrupted recorder recovered on startup`
  - a `recorder.recovery` event records recovered/quarantined counts
- Recordings page at `/recordings`:
  - shows recordings/temp/total storage usage
  - shows automatic cleanup threshold and usage bar
  - runs manual capacity cleanup from the UI
  - lists recorder workers, current temp segment, and local go2rtc RTSP input
  - shows segment length and temp-to-recordings policy
- Camera administration page at `/cameras`:
  - shows registered cameras and their role streams
  - stores a persistent active/inactive state per camera and exposes an activation toggle in the registered-camera table
  - inactive cameras remain editable but are removed from go2rtc configuration, playback, preview, probe, PTZ, recorder reconciliation, and automatic retry paths
  - operational dashboards, stream lists, counts, and `/live` render only active cameras; disabling an open camera closes its preview/focus/PTZ session without retargeting controls
  - activation applies serially with bounded request-independent recovery, and startup fails closed instead of restoring a last-good configuration containing an inactive camera
  - uses one active camera workflow for registration or editing
  - scans a device and shows saved profile-template matches
  - lets an operator save a camera from selected recording/live streams
  - exposes camera-focused update/delete actions
  - provides a separate profile library for reusable manufacturer/model templates
  - profile-template editing never asks for camera IP, username, or password
  - registration and editing share the same three-output stream policy form and validation model
  - policy drafts survive the 10-second camera refetch, expose revision conflicts, and reload fresh server values after 409
  - 202 saved-but-pending state is shown as a warning instead of ordinary success
  - each policy card shows advertised/detected input plus desired/applied/effective/runtime state
- Windows monitoring client delivery:
  - the product contract requires server-side monitoring and remote control even when the renderer is frozen or crashed; monitoring and command-channel health are separate planes ([analysis](2026-08-10_viewer-command-feature-analysis.md))
  - the Viewer registry stores independent service/Agent, control-channel, Viewer, renderer, update, and stream-progress health instead of treating visible video as client health
  - the current standard MSI packages `CamStationViewerService.exe` plus a directly launched Electron Viewer; it does not package the older Agent/Host/Bootstrap supervision chain
  - the standard Service owns the stable identity, heartbeat, SSE/long-poll management connection, single Viewer lease, and lease-derived Viewer/renderer telemetry; it now forwards bounded per-stream telemetry and lease/renderer/progress timestamps instead of acknowledging and dropping those reports
  - the operator API accepts only `ping`, `reload_live`, `resubscribe_stream`, `restart_viewer`, and `restart_service`; legacy `restart_agent` is normalized only as a compatibility alias, while arbitrary types, URLs, routes, modes, update fields, irrelevant fields, and out-of-range TTLs are rejected
  - the server queue distinguishes pending, delivered, acknowledged, running, succeeded, failed, rejected, expired, and cancelled; cancellation is limited to pending commands and active rows poll automatically
  - the Service uses a bounded atomic command journal, persists acknowledgement/running before side effects, suppresses exact duplicates, rejects changed payloads for an existing identity, and re-reports unreported terminal results after reconnect without repeating the action
  - management-pipe protocol v2 separates unsolicited command events from responses; Electron reloads only its approved live URL, forwards one safe stream name to the renderer, and returns a lease-authorized result
  - Viewer restart uses Electron relaunch for an active lease or starts only the adjacent MSI Viewer in the active console session, and succeeds only after a replacement lease plus renderer-ready proof
  - Service restart uses only the fixed installed service and private helper; the next boot generation reconciles and reports the original command instead of claiming success before restart
  - `/viewers` uses real Viewer/action selectors with Korean labels, only relevant stream/reason inputs, two-step confirmation for disruptive actions, exact state timestamps, and safe localized result categories
  - all five actions were proven end to end on the authorized Windows 11 PC with unsigned development MSI `2.0.24`: the Viewer process set changed on `restart_viewer`, the Service PID/boot generation changed on `restart_service`, and control, Viewer, renderer, and offline-stream telemetry recovered independently
  - `restart_stream` remains the existing Streams-page server control and `capture_diagnostics` is not implemented or advertised as a Viewer command
  - the older `internal/vieweragent` source contains durable command execution, Viewer generation restart, service restart reconciliation, and transactional custom updates; it is historical implementation evidence, not the active standard-MSI runtime
  - general automatic process supervision remains intentionally excluded; explicit audited server lifecycle commands are the narrow recovery boundary
  - Electron opens only the CamStation 2.0 `/live?viewer=1` route and reports renderer/stream telemetry through the Service lease; local playback recovery and server-command execution remain separate state machines
  - `scripts/build-viewer-msi.ps1` builds the standard MSI locally, while the current Settings release channel and `/api/viewers/app/*` endpoints still publish the older size/SHA-256-verified `CamStationViewerSetup.exe` format
  - production rollout still requires Authenticode signing and broader Windows fault/soak coverage; the WinPC proof used a checksum-verified unsigned development MSI and disposable server/stream state
  - `scripts/publish-viewer-release.sh` serializes legacy EXE publishers with `flock`, fsyncs immutable `releases/<version>-<sha>` directories, and atomically replaces stable `current/active` and `previous/active` pointers
  - legacy `current` files remain readable during one-time migration, and the release loader pins the selected immutable directory through an `os.Root` boundary so concurrent pointer changes cannot escape the release root or invalidate an in-flight download

## Stream And Recording Policy

- go2rtc is the local stream hub.
- Recorder workers must read local go2rtc RTSP inputs, not camera URLs directly:

```text
rtsp://127.0.0.1:8554/{streamName}
```

- The default recording path remains compatible with the existing system concept:
  - active ffmpeg segment writes to temp
  - completed segment moves to recordings
  - timeline reads finalized segment metadata
- Direct camera recording should only be an explicit troubleshooting/special-camera option later, not the default.
- Recording workers do not auto-start unless `CAMSTATION_RECORDING_ENABLED=true` or `-recording-enabled` is set. They can be started manually through the recorder API.

## Verified

Commands run successfully:

```bash
cd web && npm test
cd web && node --experimental-strip-types --test tests/streamSelection.test.ts
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
```

Viewer route verification:

- The frontend route test confirms `/viewer` is registered before the desktop console shell.
- The embedded SPA test confirms a direct `/viewer` request returns the generated app entry with `Cache-Control: no-store`.
- The layout model tests cover saved-layout selection, bounded percentage geometry, all-camera retention,
  and deterministic fallback placement.

Focused stream selection verification:

- Normal tiles select the applied live output.
- `집중 보기` selects the applied focus output and suspends that camera's normal tile connection.
- Changing focus view does not reconfigure or restart recorder workers.

Browser/Playwright verification performed:

- `/live` loads live MSE videos.
- Browser video controls are not enabled.
- Hovering video does not show native progress controls.
- Wheel zoom applies `scale(...) translate(...)` to the video.
- Dragging while wheel-zoomed changes pan offset.
- Double-clicking the video resets wheel zoom.
- `videoZoom` is included in `/api/layouts` after saving a layout.
- Refreshing `/live` restores saved wheel zoom state.
- `집중 보기` toggles in-page tile enlargement.
- `집중 보기` does not call `window.open` or create a popup.
- The removed top-level `타일 확대` button is no longer present.
- Recorder API status returns no workers by default.
- A short single-camera recorder smoke test confirmed ffmpeg uses `rtsp://127.0.0.1:8554/{streamName}`.
- Smoke-test recording output and DB row were removed afterward.
- Three-camera 5-minute recording test on the camera-reachable server:
  - all recorders used local go2rtc RTSP inputs
  - completed segments moved from `data/temp` to `data/recordings`
  - live RTSP probes stayed healthy during segment rollover
  - moved files were playable with ffprobe
- Capacity cleanup test on the camera-reachable server:
  - manual cleanup at `maxBytes=320000000` reduced recordings from `499747035` to `310851459` bytes and deleted 11 oldest ready segments
  - automatic startup cleanup with `-max-storage-gb 0.30` reduced recordings from `423656322` to `297866145` bytes
  - automatic segment-complete cleanup after the next 5-minute rollover reduced recordings from `337207565` to `282317129` bytes
  - all three recorders stayed `running`
  - local RTSP ffprobe stayed healthy for streams `camera-1`, `1`, and `2`
- Camera/profile redesign verification on 2026-07-06:
  - `go test ./internal/store ./internal/cameraprofile ./cmd/camstationd -count=1`
  - `go test ./...`
  - `cd web && npm run lint`
  - `cd web && npm run build`
  - `go build -o camstationd ./cmd/camstationd`
  - `scripts/camstationctl.sh restart`
  - `scripts/camstationctl.sh verify`
  - `/api/camera-profiles` returns JSON instead of SPA HTML
  - runtime CRUD QA used the currently registered `염소장/goat-yard` camera with DB backup/restore:
    - update changed only the camera registration metadata
    - delete removed the camera from the public listing
    - re-register restored `goat-yard-recording` and `goat-yard-live`
    - the original DB was restored afterward so `염소장` returned to id `6`
- Live PTZ verification on 2026-07-11:
  - `go test ./...`
  - `cd web && npm run lint`
  - `cd web && npm run build`
  - `go build -o camstationd ./cmd/camstationd`
  - `scripts/camstationctl.sh restart`
  - `scripts/camstationctl.sh verify`
  - `염소장/goat-yard` advertised continuous PTZ, home support, and 100 presets through guarded capability refresh
  - one 20% left/Stop/right/Stop sequence ended with pan/tilt and zoom both `IDLE`
  - one `CamStation QA` preset was created, listed, deleted, and confirmed absent
  - home setting was not invoked; home navigation was intentionally skipped because the saved destination was not operator-confirmed
  - `/live` showed the capability-enabled PTZ button and full replacement panel; selecting a non-PTZ camera closed the panel and disabled the button
  - wheel zoom/reset, focus view, layout-save presence, timeline presence, and disabled listen/talk/siren states were checked in the same browser session
  - screenshot evidence: `data/diagnostics/live-ptz-panel.png` (runtime evidence, intentionally untracked)
- PTZ preset-name persistence verification on 2026-07-12:
  - a temporary Korean alias was created once on the `소방서5/fire-station-5` VStarcam and returned as the exact token/name pair
  - the same pair remained after a controlled `camstationctl.sh restart` and healthy daemon recovery
  - goto and delete both returned HTTP 200; the final list confirmed the temporary token and alias were absent
  - final `camstationctl.sh verify` passed and no temporary preset remained
- Per-camera stream policy rollout on 2026-07-11:
  - full Go tests, web tests (16/16), lint, production build, daemon build, controlled restart, and `camstationctl.sh verify` passed
  - all eight registered cameras have three DB-backed outputs and an `applied` desired/applied revision
  - `소방서1` recording/live outputs are H.264 copy with `transcoding=false`; focus remains the intentional capped software-H.264 path
  - `소방서5` recording is HEVC 3840x2160 copy, live is H.264 640x360, and focus is H.264 1920x1080
  - `소방서5` recording/live/focus output verification is healthy after reapply and restart
  - PTZ/home capability state survived migration and restart (`소방서5` home remains unavailable)
  - 202 rollback and 503 unsafe-recovery behavior were verified with non-disruptive route/coordinator tests
  - public APIs, events, runtime logs, and embedded assets contain no unredacted camera credentials
- Live source lifecycle recovery on 2026-07-12 08:44 KST:
  - private inputs referenced by applied live outputs are preloaded once with video and audio while public transform outputs remain on demand
  - controlled `camstationctl.sh` restart and verify passed on `cctv2`; generated policy contained eight private live-source preload entries and no public always-output entries
  - all eight live outputs reported one producer and one browser MSE consumer after reconnect, sustained for at least 30 seconds
  - the post-restart runtime log contained no ordinary-live `404` or `Invalid data found when processing input` signature
  - the legacy `cctv` server and all camera URLs, credentials, and profile settings were left unchanged
- Common camera source relay recovery on 2026-07-12 09:31 KST:
  - every private camera input now uses an FFmpeg video/audio copy relay; RTSP/RTSPS inputs add a five-second input timeout while HTTP-FLV keeps its protocol-native input path
  - full Go tests, daemon build, controlled `camstationctl.sh` restart, and managed verify passed on `cctv2`
  - all eight private live-source byte counters increased across the final sample and all eight public live outputs had one browser MSE consumer
  - a bounded 소방서1 relay termination produced a replacement and stable byte growth in 11.18 seconds without restarting camstationd or go2rtc
  - local output probes received H.264 640x360 from 소방서1 in 9.7 seconds after fault recovery and from 소방서5 in 2.0 seconds
  - post-restart logs contained no stale-connection or invalid-input signature; the legacy `cctv` server and camera settings were untouched
- Windows Viewer publication and application on 2026-07-16 19:33-22:51 KST:
  - publisher contract tests, full Go tests, all 46 web tests, web lint/build, all 15 Viewer app tests, Viewer app build, and daemon build passed
  - published development release `2.0.0-dev.1` is a PE32+ x86-64 installer built for `http://10.0.0.29:18080`
  - the final source, published, and downloaded installers were each `384014848` bytes with SHA-256 `1ea12f3e33302f7f3a1fa973a0d13c0cbde7d3d93ec210634541e8eca1ed69f9`
  - metadata returned HTTP 200 with `Cache-Control: no-store`; download returned HTTP 200 with the fixed attachment filename, PE content type, exact content length, and `X-Content-Type-Options: nosniff`
  - `/settings` served the generated hashed asset containing the Windows installer card and fixed download route
  - the controlled restart used `CAMSTATION_RECORDING_ENABLED=false`; recorder workers, managed go2rtc, and managed ffmpeg all remained empty before and after restart
  - browser screenshot automation was unavailable because the local Chrome process was denied socket creation; API, generated-asset, and download verification completed without opening `/live`
  - focused publication review replaced directory rotation with a continuously available atomic pointer layout, added publisher serialization and checked rollback after post-switch durability failure, and migrated the same installer without moving the legacy files
  - the Agent now converges missed SSE updates from heartbeat, reports the exact installed artifact identity, and keeps forced restart control responsive while an update is pending
  - the server issues an update commit token only after 30 continuous seconds of exact Agent/control/Viewer/renderer health; update installs wait up to 120 seconds for the exact marker and otherwise roll back and quarantine the failed target
  - after the pointer-aware daemon restart, the source, active immutable release, and newly downloaded installer still had the exact recorded size and SHA-256; recorder workers, managed go2rtc, and managed ffmpeg remained empty

## Important Corrections Learned

- The user's "영상 확대" meant mouse wheel zoom on the video content, not `object-fit: cover`.
- `집중 보기` is not video wheel zoom. It is a larger in-page view of a camera tile.
- The old `/new` source on disk did not clearly show the wheel zoom implementation, but the running production bundle did. Runtime DOM inspection showed:
  - wrapper receives wheel/mouse events
  - video gets inline `transform: scale(...) translate(...)`
  - wrapper uses `overflow: hidden`
  - zoom badge appears while scale is greater than 1
- Existing monitoring behavior should be upgraded, not replaced with a generic dashboard concept.

## Partially Implemented

- Timeline UI can read recording segment metadata, but aggregate timeline and motion data are still incomplete.
- Recordings page shows storage/cleanup/recorder state, but does not yet include playback or segment browsing.
- Settings includes language settings and Windows Viewer delivery, but does not yet cover all legacy settings.
- System/Streams/Logs pages are early status surfaces and feature matrices.
- Windows Viewer production rollout still needs Authenticode signing, installation on the target monitoring PCs, and the planned long-running Windows soak.
- Camera grouping and advanced ONVIF management are not complete.
- Event log is basic and still needs operational filtering and incident grouping.

## Not Implemented Yet

- Full recording worker supervision lifecycle
- Motion data API
- Recording playback page
- Clip download/export
- Retention-by-days settings
- Motion event detection/storage
- Camera sort/group management
- ONVIF discovery/reboot/status management
- Connection engine state machine:
  - connecting
  - streaming
  - degraded
  - reconnecting
  - offline
  - recovering
- Transport fallback policy
- RTSP keepalive policy
- Incident/alert dampening
- Alert acknowledge/snooze
- Backup/rclone orchestration
- User authentication/authorization
- systemd install/service packaging
- Server-issued post-update commit-token validation; the first development release currently uses the documented exact local transaction validation seam
- Production Authenticode signing and signer-thumbprint publication
- Diagnostic bundle export

## Current Runtime Notes

- The current test server can reach cameras.
- Use `0.0.0.0:18080` when testing from another browser:

```bash
./camstationd -addr 0.0.0.0:18080 -db ./data/camstation.db
```

- The embedded frontend build output lives in `cmd/camstationd/web`.
- Always run `cd web && npm run build` before `go build` when frontend files change.
- Do not expose raw go2rtc APIs. Use CamStation proxy paths only.

## Suggested Next Tasks

1. Add recording segment list/playback/download APIs and connect the recordings page.
2. Improve the live aggregate timeline so it loads all camera segments, not only the selected camera.
3. Add recording settings UI for segment length, auto-start, storage path, and retention.
4. Expand recovery to reconcile final files already moved but not reflected in SQLite.
5. Expand camera management beyond initial registration.
6. Add connection state machine and incident grouping before alert delivery.
