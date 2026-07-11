# Implementation Status

Last updated: 2026-07-12

This document records the current implementation state so the next session can continue without re-discovering the same context.

## Current Branch And Remote

- Repository: `https://github.com/dyllisLev/CamStation.git`
- Active branch used for this work: `camstation2-initial`
- Latest implementation commit at the time of this note: `f3dee5c guard rtsp probe fallback by applied graph`
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
  - right panel toggle
  - timeline toggle
  - fullscreen toggle
  - right panel with saved layouts and camera status
  - bottom two-row timeline shell
- MSE live video playback without browser video controls
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
- Settings page includes language settings, but does not yet cover all legacy settings.
- System/Streams/Logs/Viewers pages are early status surfaces and feature matrices.
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
- Viewer app fleet management
- User authentication/authorization
- systemd install/service packaging
- Update workflow
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
