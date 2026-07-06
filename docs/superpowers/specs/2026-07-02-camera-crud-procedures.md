# Camera CRUD Procedures

## Goal

Camera administration must satisfy CRUD behavior for camera instances while
keeping reusable profile templates as a separate resource. Registration,
modification, and deletion are camera procedures. Profile-template create/edit
delete is managed separately in the profile library and never asks for camera
credentials.

## TASK: Register Camera

1. Operator enters camera name, internal key, host/IP, account, password, and ports.
2. Operator scans the device.
3. CamStation returns a device scan report, matches it against stored profile
   templates, and presents channel candidates.
4. Operator reviews and may change recording and live profile selections.
5. Operator can preview recording and live selections.
6. Operator registers the selected streams as a camera. If no existing template
   matches, the operator can save the selected mapping as a reusable profile
   template as a separate action.
7. CamStation persists the camera, role streams, generated go2rtc streams, and
   recorder input without exposing credentials in public API responses.

Acceptance evidence:

- `POST /api/cameras/scan` returns a redacted scan report plus profile-template matches.
- `POST /api/cameras/preview` returns a temporary preview stream.
- `POST /api/cameras` persists recording/live role streams.
- `/cameras` shows the registered camera and role streams.
- go2rtc/recorder verification shows the recording stream active.

## TASK: Edit Camera

1. Operator selects a registered camera.
2. CamStation opens the same camera workflow in edit mode for that camera.
3. Operator enters or confirms host/IP, account, password, and ports.
4. Operator rescans the selected camera and reviews profile-template matches.
5. Operator reviews and may change recording and live profile selections.
6. Operator can preview recording and live selections.
7. Operator saves the selected streams as an update to the same stable camera key.
8. CamStation updates metadata, role streams, generated go2rtc streams, and recorder
   input without exposing credentials in public API responses.

Acceptance evidence:

- `PUT /api/cameras/{streamName}` updates the selected camera.
- `/cameras` lets a selected registered camera rescan, preview, and save profile selections.
- The camera keeps its stable `streamName`.
- go2rtc/recorder verification reflects the updated role streams.

## TASK: Manage Profile Templates

1. Operator opens the profile library.
2. Operator creates, edits, or deletes a reusable manufacturer/model profile
   template.
3. The profile-template form contains profile name, manufacturer, model,
   adapter, capabilities, and credential-free channel mappings.
4. The form does not contain camera host/IP, account, password, or raw
   credentialed URLs.
5. Delete is blocked when a registered camera still references the template.

Acceptance evidence:

- `/api/camera-profiles` returns JSON CRUD responses.
- `/cameras` shows profile-template CRUD separately from the camera workflow.
- A temporary unreferenced template can be created, edited, and deleted.
- A referenced template delete returns an operational Korean error.

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
