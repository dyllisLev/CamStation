# Implementation Status

Last updated: 2026-06-30

This document records the current implementation state so the next session can continue without re-discovering the same context.

## Current Branch And Remote

- Repository: `https://github.com/dyllisLev/CamStation.git`
- Active branch used for this work: `camstation2-initial`
- Latest implementation commit at the time of this note: `Add capacity cleanup for recordings`
- Runtime test URL on the camera-reachable server: `http://10.0.0.29:18080/`
- Main monitoring page: `http://10.0.0.29:18080/live`

## Implemented

- Go backend skeleton under `cmd/camstationd`
- Embedded React/Vite frontend served by the Go daemon
- SQLite store and migrations under `internal/store`
- Camera registration/listing API
- Camera URL redaction in API responses and events
- ffprobe-based camera probe helper
- go2rtc managed as a child process by `camstationd`
- go2rtc API/RTSP bound locally, with CamStation proxying allowed player paths
- Health, events, stream status, stream restart, camera probe endpoints
- Korean default UI and language setting menu
- Live monitoring workspace at `/live`
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
- Recordings page at `/recordings`:
  - shows recordings/temp/total storage usage
  - shows automatic cleanup threshold and usage bar
  - runs manual capacity cleanup from the UI
  - lists recorder workers, current temp segment, and local go2rtc RTSP input
  - shows segment length and temp-to-recordings policy

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
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
```

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
- Camera management is basic registration/listing; edit/delete/group/ONVIF are not complete.
- Event log is basic and still needs operational filtering and incident grouping.

## Not Implemented Yet

- Full recording worker supervision lifecycle
- Recording segment recovery for stale temp/orphan files
- Motion data API
- Recording playback page
- Clip download/export
- Retention-by-days settings and stale-temp recovery
- Motion event detection/storage
- Camera edit/delete/sort/group management
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

1. Add recorder recovery for stale temp files, orphaned DB rows, and final files already moved.
2. Add recording segment list/playback/download APIs and connect the recordings page.
3. Improve the live aggregate timeline so it loads all camera segments, not only the selected camera.
4. Add recording settings UI for segment length, auto-start, storage path, and retention.
5. Expand camera management beyond initial registration.
6. Add connection state machine and incident grouping before alert delivery.
