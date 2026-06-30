# Implementation Status

Last updated: 2026-06-30

This document records the current implementation state so the next session can continue without re-discovering the same context.

## Current Branch And Remote

- Repository: `https://github.com/dyllisLev/CamStation.git`
- Active branch used for this work: `camstation2-initial`
- Latest pushed implementation commit at the time of this note: `2c37ada Use focus button for tile zoom`
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

- Timeline UI exists but recording segment and motion data are placeholder/empty.
- Recordings page is a placeholder surface, not a real recording browser.
- Settings page includes language settings, but does not yet cover all legacy settings.
- System/Streams/Logs/Viewers pages are early status surfaces and feature matrices.
- Camera management is basic registration/listing; edit/delete/group/ONVIF are not complete.
- Event log is basic and still needs operational filtering and incident grouping.

## Not Implemented Yet

- Recording worker supervision with ffmpeg
- Recording segment metadata persistence
- Actual timeline segment/motion API data
- Recording playback page
- Clip download/export
- Storage usage and retention enforcement
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

1. Implement real timeline data from recording metadata.
2. Build the recording worker model and segment database table.
3. Finish recordings page playback and segment list.
4. Expand camera management beyond initial registration.
5. Add connection state machine and incident grouping before adding alert delivery.
6. Decide whether `집중 보기` state itself should be saved in layouts; currently wheel zoom state is saved, while active tile enlargement is not.
