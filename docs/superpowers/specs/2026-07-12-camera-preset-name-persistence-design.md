# Camera Preset Name Persistence Design

## Problem

The PTZ form sends the entered name through the React API and Go route to the
standard ONVIF `SetPreset` request. Some VStarcam firmware accepts the preset
position but ignores `PresetName`. A subsequent `GetPresets` response then
returns generated names such as `PRESET_0`, and the query refresh replaces the
name the operator entered.

CamStation currently stores no preset metadata, so it cannot preserve an
operator name when the camera does not.

## Considered approaches

1. **Persist a CamStation name by camera and preset token (selected).** This
   survives daemon and browser restarts, is shared by every console, and works
   with both compliant and non-compliant cameras.
2. Add a VStarcam-specific rename command. This depends on undocumented
   firmware behavior and would not solve the same issue on other cameras.
3. Keep names in browser storage. This is temporary, browser-specific, and
   contradicts SQLite being the system source of truth.

## Data model

Add an idempotently-created `camera_preset_names` table:

```sql
CREATE TABLE IF NOT EXISTS camera_preset_names (
    camera_id INTEGER NOT NULL,
    preset_token TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (camera_id, preset_token),
    FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE
);
```

The preset token remains the device identity used by goto and delete. The
stored name is only the operator-facing label. Names use the route's existing
trim, UTF-8, control-character, and 64-character validation.

## Request and response flow

### Create

1. The existing UI sends `{name}`.
2. CamStation calls ONVIF `SetPreset` and obtains the camera token.
3. CamStation upserts `(camera_id, token, requested name)` in SQLite.
4. The response returns the token with the stored name.
5. The existing query invalidation reloads the list.

If SQLite persistence fails after the camera creates the preset, the route
returns an error and makes a best-effort `RemovePreset` call for that new token.
The database error is not exposed verbatim.

### List

1. CamStation obtains the current preset list from the camera.
2. It loads stored names for that camera.
3. A stored name replaces the camera-reported name for the same token.
4. Names reported by a compliant camera remain visible when no stored name
   exists.
5. After a successful full camera list, stored rows for tokens that no longer
   exist are removed so an externally deleted and later reused token cannot
   inherit a stale label.

The public `CameraPreset {token, name}` DTO and frontend component remain
unchanged.

### Delete

1. CamStation deletes the preset from the camera using its token.
2. Only after camera success, it deletes the matching stored name.
3. The existing query invalidation reloads the list.

Deleting a camera cascades its preset-name rows. A failed camera deletion keeps
the stored name because the preset still exists.

## UI behavior

No new control is needed. The current input remains the only name entry point.
After save and every later reload, the list displays the SQLite name even when
the camera reports `PRESET_0`. Goto and delete continue using the opaque token,
not the display name.

## Boundaries and security

- Raw camera URLs and credentials never enter the new table or public DTO.
- Tokens and names continue through JSON and parameterized SQL only.
- React continues to render names as escaped text.
- This change does not add preset renaming, offline preset management, or
  vendor-specific PTZ commands.

## Tests and completion criteria

- Migration is idempotent and camera deletion cascades stored names.
- Store upsert, list, delete, and stale-token reconciliation are covered.
- Route create persists the requested name for the returned token.
- Route list overlays `PRESET_0` with the stored operator name and preserves a
  camera name when no override exists.
- Route delete removes the stored name only after camera success.
- DB failure after creation triggers best-effort camera cleanup and a sanitized
  error.
- Existing API types and frontend form/query behavior remain compatible.
- A real VStarcam preset created with a Korean name still shows that name after
  query refresh and daemon restart; goto and delete continue to work.
