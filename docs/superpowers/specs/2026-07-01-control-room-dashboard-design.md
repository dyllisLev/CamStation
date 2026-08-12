# Control Room Dashboard Design

## Goal

Separate the CamStation console home page from the live monitoring workspace.

The `Live` page keeps the current multi-camera video workspace. The `Control Room`
page becomes an operator dashboard that shows the whole system state at a glance
without continuously playing video.

## Page Roles

### Control Room (`/`)

The control room is the default status dashboard. It should answer:

- Are cameras connected?
- Are streams being served?
- How many viewers or stream consumers are connected?
- Are recordings running?
- Is storage healthy?
- What recently failed or changed?

The page does not auto-play camera video. A camera row may open an inner modal
preview for temporary single-camera viewing. Closing the modal stops that preview.

### Live (`/live`)

The live page remains the current video-first workspace:

- Multi-camera grid
- Saved layouts
- Timeline
- Zoom and fullscreen controls
- Side panel
- Manual video operation

## Control Room Layout

### Summary Band

Show compact status tiles across the top:

- Total cameras
- Online and offline cameras
- Streams running
- Recording workers running
- Active viewer or stream connections
- Recording storage usage
- Recent errors

Tiles should be dense and scannable. They are operational counters, not marketing
cards.

### Camera Status Table

The main body is a camera-by-camera status table with these columns:

- Camera name
- Connection state
- Stream state
- Active connection count
- Recording state
- Last recording segment time
- Recent error or event
- Actions

Actions:

- `View`: opens a modal preview for this camera.
- `Live`: navigates to `/live` and selects or focuses the camera when that is
  available.
- `Restart stream`: restarts stream management for the camera or falls back to
  the existing all-stream restart API until per-camera restart exists.

### Operations Panel

Show supporting system information beside or below the table:

- Recent events
- go2rtc process status
- Recorder worker status
- Storage usage

The layout should work at desktop sizes with the existing left console sidebar.
On mobile, the summary appears first, then the camera table, then operations.

## Data Sources

Initial implementation should use existing APIs where possible:

- `GET /api/cameras`
- `GET /api/streams/status`
- `GET /api/recorders/status`
- `GET /api/recordings/storage`
- `GET /api/events`
- `POST /api/streams/restart`

Connection counts may not be available yet. If the backend cannot provide them
accurately, show `-` or `unknown` in the first pass and keep the UI column in
place. A later backend increment can add go2rtc-derived consumer counts without
redesigning the page.

## Preview Modal

The modal is an on-demand single-camera viewer:

- It opens from a camera row.
- It uses the same playback path as the live workspace.
- It shows camera name and stream state.
- It closes cleanly and stops the temporary player.
- It should not replace `/live`; it is for quick inspection only.

## Error Handling

- If a data source fails, show a degraded state in that section instead of
  blanking the whole dashboard.
- Use existing status dot semantics for `running`, `offline`, `warning`, and
  `unknown`.
- Recent errors should be visible without requiring the logs page.

## Testing

Add focused coverage for:

- `/` renders a dashboard component, not `LiveWorkspace`.
- `/live` continues to render `LiveWorkspace`.
- The control room can render with empty API data.
- Camera preview modal opens and closes without changing the route.

Use the existing build and Go test flow as final verification:

- `npm run build`
- `go test ./...`

## Out of Scope

- Replacing the current live workspace.
- Always-on video thumbnails on the control room.
- Full per-camera stream restart if the backend does not already support it.
- Accurate viewer connection counts if go2rtc consumer data is not yet exposed.
