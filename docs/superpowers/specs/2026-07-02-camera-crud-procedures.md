# Camera CRUD Procedures

## Goal

Camera administration must satisfy basic CRUD behavior for profile-based cameras.
Registration, modification, and deletion are separate operator procedures, and
each procedure must be verified through the UI/API/runtime surface instead of
only by source inspection.

## TASK: Register Camera

1. Operator enters camera name, internal key, host/IP, account, password, and ports.
2. Operator scans the device profile.
3. CamStation matches manufacturer/model/adapter and presents channel candidates.
4. Operator reviews and may change recording and live profile selections.
5. Operator can preview recording and live selections.
6. Operator registers the selected profiles as a camera.
7. CamStation persists the camera, role streams, generated go2rtc streams, and
   recorder input without exposing credentials in public API responses.

Acceptance evidence:

- `POST /api/cameras/scan` returns a redacted profile.
- `POST /api/cameras/preview` returns a temporary preview stream.
- `POST /api/cameras` persists recording/live role streams.
- `/cameras` shows the registered camera and role streams.
- go2rtc/recorder verification shows the recording stream active.

## TASK: Edit Camera

1. Operator selects a registered camera.
2. CamStation opens a profile edit procedure for that camera.
3. Operator enters or confirms host/IP, account, password, and ports.
4. Operator rescans profile matching for the selected camera.
5. Operator reviews and may change recording and live profile selections.
6. Operator can preview recording and live selections.
7. Operator saves the selected profiles as an update to the same stable camera key.
8. CamStation updates metadata, role streams, generated go2rtc streams, and recorder
   input without exposing credentials in public API responses.

Acceptance evidence:

- `PUT /api/cameras/{streamName}` updates the selected camera.
- `/cameras` lets a selected registered camera rescan, preview, and save profile selections.
- The camera keeps its stable `streamName`.
- go2rtc/recorder verification reflects the updated role streams.

## TASK: Delete Camera

1. Operator selects a registered camera.
2. Operator explicitly confirms deletion.
3. CamStation deletes the camera and role stream rows while preserving existing
   recording files and historical segment rows.
4. CamStation regenerates go2rtc configuration and reconciles recorder workers.
5. `/cameras` no longer shows the deleted camera.

Acceptance evidence:

- `DELETE /api/cameras/{streamName}` removes the camera and role streams.
- Public camera listing no longer contains the deleted camera.
- go2rtc/recorder verification no longer includes that camera's active stream.
