# Implementation Status

Last updated: 2026-07-16

This document records the current implementation state so the next session can continue without re-discovering the same context.

## Current Branch And Remote

- Repository: `https://github.com/dyllisLev/CamStation.git`
- Active branch used for this work: `camstation2-initial`
- Latest Windows Viewer implementation commit before publication: `ff0dca0 fix(viewer-app): hold updater ownership during recovery`
- Runtime test URL on the camera-reachable server: `http://10.0.0.29:18080/`
- Main monitoring page: `http://10.0.0.29:18080/live`

## Implemented

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
  - the Settings page shows the current Windows installer version, size, digest prefix, development-unsigned marker, and a fixed download link
  - `GET /api/viewers/app/version` serves no-store release metadata and `GET /api/viewers/app/download` serves only a size/SHA-256-verified `CamStationViewerSetup.exe`
  - the Viewer registry stores independent Agent, control-channel, Viewer, renderer, update, and stream-progress health instead of treating visible video as client health
  - durable idempotent commands, bounded SSE/long-poll recovery, force restart, and server-directed `update_app` control are implemented
  - the Viewer command selector exposes only `ping`, `reload_live`, `restart_viewer`, `restart_agent`, and `resubscribe_stream`; the Agent additionally accepts server-directed `update_app`
  - `restart_stream` remains the existing Streams-page server control and `capture_diagnostics` is not implemented or advertised as a Viewer command
  - the Windows Agent runs behind a stable automatic SCM service host; the per-user bootstrap owns the Electron Viewer process tree through a Windows Job Object
  - Electron opens only the CamStation 2.0 `/live?viewer=1` route, emits renderer-context liveness pulses, and uses finite WebRTC-primary/MSE-fallback playback recovery whose 30-second budget begins at failure detection
  - the Agent supervises Viewer IPC and renderer liveness independently of server/control health, persists the automatic restart budget, and serializes Viewer restart, Agent restart, and update side effects
  - the installer registers the automatic Agent service, SCM recovery actions, configured-user logon task, and boot recovery task in one unattended installation flow
  - update activation, durable retry budgets, exact artifact verification, ownership, rollback, quarantine, and restart recovery are transactional
  - `scripts/publish-viewer-release.sh` serializes publishers with `flock`, fsyncs immutable `releases/<version>-<sha>` directories, and atomically replaces stable `current/active` and `previous/active` pointers
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
cd web && node --experimental-strip-types --test tests/streamSelection.test.ts
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
```

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
- Windows Viewer publication and application on 2026-07-16 19:33-20:11 KST:
  - publisher contract tests, full Go tests, all 46 web tests, web lint/build, all 15 Viewer app tests, Viewer app build, and daemon build passed
  - published development release `2.0.0-dev.1` is a PE32+ x86-64 installer built for `http://10.0.0.29:18080`
  - source, published, and downloaded installers were each `383880704` bytes with SHA-256 `17fef68a70041b1f6d99f6c8f524fad46e5047da0c8b6a1f5f06959de619bf84`
  - metadata returned HTTP 200 with `Cache-Control: no-store`; download returned HTTP 200 with the fixed attachment filename, PE content type, exact content length, and `X-Content-Type-Options: nosniff`
  - `/settings` served the generated hashed asset containing the Windows installer card and fixed download route
  - the controlled restart used `CAMSTATION_RECORDING_ENABLED=false`; recorder workers, managed go2rtc, and managed ffmpeg all remained empty before and after restart
  - browser screenshot automation was unavailable because the local Chrome process was denied socket creation; API, generated-asset, and download verification completed without opening `/live`
  - focused publication review replaced directory rotation with a continuously available atomic pointer layout, added publisher serialization and checked rollback after post-switch durability failure, and migrated the same installer without moving the legacy files
  - after the pointer-aware daemon restart, the source, active immutable release, and newly downloaded installer still had the exact recorded size and SHA-256; managed go2rtc and managed ffmpeg remained empty

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
