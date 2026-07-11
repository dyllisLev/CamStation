# Live PTZ Camera Control Design

**Date:** 2026-07-11 KST
**Status:** Review updates applied; awaiting re-approval

## Goal

Add safe ONVIF PTZ control for the registered `염소장` camera and place the
operator controls in the `/live` workspace. A selected camera advertises its
control availability immediately. The toolbar PTZ button is enabled only when
CamStation can control the selected camera.

The first delivery includes:

- continuous pan, tilt, and zoom
- explicit stop
- go to home position
- set the current position as home
- list, create, go to, and delete camera presets
- a full replacement PTZ view for the existing right panel
- capability-gated placeholders for listening, talking, and siren controls

The first delivery does not implement browser audio playback, talk-back, or
siren actuation. Those controls remain visible but unavailable until their
transport or device protocol is verified.

## Confirmed Device Evidence

Read-only queries were run against the registered camera without exposing its
host, credentials, or ONVIF endpoint.

The camera identifies as a VStarcam/VeePai device in the V400D product family.
It accepted the existing camera credentials through ONVIF WS-Security
UsernameToken authentication.

### PTZ

The device returned one PTZ node and one PTZ configuration. It advertised:

- absolute pan/tilt and zoom
- relative pan/tilt and zoom
- continuous pan/tilt and zoom
- position and speed ranges normalized to ONVIF generic spaces
- move timeouts from 1 to 60 seconds, with a 5-second device default
- home-position support
- up to 100 presets
- current position and move status reporting

The device had no saved presets at investigation time. No movement or position
mutation was performed during discovery.

### Audio and siren

The camera exposes one ONVIF audio input. The live RTSP stream contains an
8 kHz, mono G.711 A-law track, confirming that the camera microphone is usable
at the media source.

The current browser MSE negotiation returns H.264 video only. The camera audio
track is therefore not yet available to the live `<video>` element even though
the source stream contains audio.

The V400D product specification advertises two-way talk, which is evidence of a
physical speaker. This device does not advertise an ONVIF Device I/O service and
returns `Action Not Implemented` for ONVIF audio-output and audio-decoder
operations. Talk-back is therefore not available through the verified standard
path.

The device advertises zero relay outputs, no PTZ auxiliary commands, and no
standard siren operation. Siren support remains unknown and unavailable.

## Product Behavior

### Camera selection and toolbar state

The live workspace keeps one selected camera using its stable internal
`streamName`, matching the current live grid, saved layout items, and camera API.

- The `PTZ 제어` toolbar button is always visible.
- It is enabled only when the selected camera reports PTZ as supported and
  available and the camera is online.
- No selection, an unsupported camera, an unavailable controller, or an offline
  camera disables the button.
- A disabled button exposes a concise Korean reason through accessible help
  text and a tooltip.
- Selecting a PTZ-capable camera does not open the panel automatically. The
  operator explicitly opens it with the toolbar button.

If the PTZ panel is open and the operator selects an unsupported or offline
camera, CamStation sends a best-effort stop for the previous target, returns to
the normal right panel, and disables the PTZ button.

### Right-panel replacement

Opening PTZ control replaces the entire right-panel body. It does not create a
modal, popup, or overlay over video.

The panel contains, in order:

1. a back button, selected camera name, protocol label, and readiness state
2. a circular four-direction pad
3. zoom-in and zoom-out press controls
4. a movement-speed slider, defaulting to 60 percent
5. a prominent immediate-stop button
6. home-position actions
7. preset management
8. device-feature controls

The movement controls stay at the top. The panel body scrolls when the viewport
cannot show the lower sections. The back button sends a best-effort stop before
returning to the existing saved-layout and camera-status panel.

### Press-and-hold movement

Direction and zoom buttons use pointer events so the same behavior works with a
mouse, touch, or pen.

- Pointer down sends the first continuous-move command.
- The client allows only one move request to be in flight. It targets a renewal
  one second after the previous dispatch: if the previous request has completed,
  it waits until that deadline; if the deadline passes while the request is in
  flight, it renews immediately after completion without overlapping requests.
- The command uses a 2-second device timeout, so a disconnected browser cannot
  leave the camera moving indefinitely.
- Pointer up, pointer cancel, lost pointer capture, pointer leave, window blur,
  page visibility loss, camera selection change, panel close, and component
  unmount all stop renewal, abort the current browser request, and trigger stop.
- Space or Enter keydown/keyup provides the same press-and-hold behavior for a
  focused direction or zoom button. Escape triggers stop while the panel is
  open.
- The center of the direction pad and the separate stop button both issue an
  immediate stop.

The server maintains one command gate and active transport cancellation function
per camera. Stop increments the camera command generation, cancels the active
non-stop transport call, waits for that call to exit within the short request
timeout, and then sends ONVIF Stop while still holding the gate. A stale move
generation cannot send another device request. This makes Stop the final wire
command rather than merely a concurrent request that could arrive before a
delayed move.

Movement and stop transport calls use a 2-second HTTP timeout. Read-only
capability discovery may use the existing longer scanner timeout.

The server clamps all velocity components to the advertised generic range. The
UI speed slider changes the magnitude but never bypasses server validation.

### Home position

The panel provides:

- `홈으로 이동`
- `현재 위치를 홈으로 설정`

Setting home changes persistent device state and requires a confirmation dialog.
Going home does not require confirmation. Neither operation is run as a repeated
automated test.

### Presets

The panel shows the current count and device maximum. It supports:

- listing camera presets
- saving the current position with an operator-provided name
- moving to a selected preset
- deleting a selected preset after confirmation

Preset names are trimmed, required, and limited to 64 characters. Preset tokens
are opaque device values and never become URL path segments. The browser sends a
token obtained from the preset list in a bounded JSON body. The server requires
valid UTF-8, rejects empty values and values longer than 64 characters, and XML
escapes the exact token before the camera validates it. It does not add a second
device-list request before every preset action.

The camera is the source of truth for preset positions. CamStation does not
duplicate preset coordinates in SQLite.

### Device features

The bottom section reserves three capability-driven controls:

- `소리 듣기`
- `말하기`
- `사이렌`

Each feature distinguishes device support from CamStation availability:

| Feature | Device support | CamStation availability in this delivery | UI state |
| --- | --- | --- | --- |
| Listen | Supported | Unavailable until browser audio transport is added | Disabled with reason |
| Talk | Physical speaker indicated; standard path unknown | Unavailable | Disabled with reason |
| Siren | Unknown | Unavailable | Disabled with reason |

This keeps the panel stable when future media or vendor adapters make a feature
available. The UI never enables a control from product marketing evidence alone.

## Architecture

### Shared ONVIF transport

Create a small internal ONVIF transport that owns:

- SOAP 1.2 requests
- WS-Security UsernameToken digest generation
- request timeouts
- safe SOAP fault normalization
- device, media, PTZ, and Device I/O service paths

The existing camera scanner and the new controller reuse this transport. Raw
passwords, envelopes, endpoints, and camera URLs never cross the package's
public result boundary.

### Camera control package

Create a focused camera-control package with a concrete controller. It owns:

- control capability discovery
- PTZ status
- continuous move and stop
- go-to-home and set-home
- preset listing and mutations
- normalization of device errors into safe domain errors

The scanner remains read-oriented. Control methods do not become methods on the
scanner.

The controller obtains a target from the stored camera record. Routes never
accept a host, port, username, password, ONVIF endpoint, or raw camera URL from
the browser.

Non-stop commands are serialized per camera. Stop preempts an active non-stop
transport call by cancellation, waits for the canceled call to leave the command
gate, and then becomes the final serialized device command. The wait is bounded
by the controller request timeout.

### Persisted capability summary

Add a `control_capabilities_json TEXT NOT NULL DEFAULT '{}'` column to each camera
record. It stores a credential-free normalized summary and discovery timestamp,
not raw ONVIF responses. Existing rows receive `{}`, which decodes to all
features `unknown` and unavailable rather than guessing support.

Each feature has:

- device support: `supported`, `unsupported`, or `unknown`
- CamStation availability: boolean
- a stable public reason code when unavailable

The public camera DTO includes this summary so the live toolbar can update
without probing the camera on every tile selection.

Camera scan/save refreshes the summary. A guarded control-refresh operation is
available for an existing camera whose summary is missing or stale. When the
operator selects a camera with an unknown summary, the live workspace attempts
one guarded refresh for that camera during the page session, persists the
result, and invalidates the camera query. A failed refresh leaves the button
disabled with a reason and is not automatically retried in a loop. This gives
the existing `염소장` row a defined lazy migration path without a manual
post-deployment database edit.

## HTTP API

All control APIs use the existing trusted-console management guard. They accept
only a registered stable `streamName` target. If the existing camera lookup also
matches a recording or live role-stream alias, the route compares the resolved
camera's `streamName` with the path value and rejects an alias target.

The frontend adds a camera-management request wrapper around the existing JSON
request helper. It explicitly adds `X-CamStation-Management: 1` for guarded GET
requests as well as mutations; the existing global GET behavior does not change.
Control and preset queries disable automatic retry, focus refetch, and polling.
They refresh only when the panel opens, the operator explicitly refreshes, or a
successful mutation invalidates the relevant query.

### Capability and state

- `GET /api/cameras/{streamName}/controls`
  - returns the safe persisted capability summary and current PTZ status
- `POST /api/cameras/{streamName}/controls/refresh`
  - performs read-only capability discovery and persists the safe summary

### Movement

- `POST /api/cameras/{streamName}/ptz/move`
  - body: `{ "pan": number, "tilt": number, "zoom": number }` using normalized
    velocities
  - server timeout is fixed at 2 seconds
- `POST /api/cameras/{streamName}/ptz/stop`
  - stops both pan/tilt and zoom

### Home

- `POST /api/cameras/{streamName}/ptz/home/goto`
- `POST /api/cameras/{streamName}/ptz/home/set`

### Presets

- `GET /api/cameras/{streamName}/ptz/presets`
- `POST /api/cameras/{streamName}/ptz/presets`
  - body: `{ "name": string }`
- `POST /api/cameras/{streamName}/ptz/presets/goto`
  - body: `{ "token": string }`
- `POST /api/cameras/{streamName}/ptz/presets/delete`
  - body: `{ "token": string }`

Successful responses expose only normalized state. They never echo credentials,
camera endpoints, SOAP payloads, or raw device faults.

## Security and Failure Handling

- Reuse the trusted-console management header, Origin, and Fetch Metadata guard.
- Resolve all targets from the SQLite camera row.
- Bound request bodies and reject unknown fields.
- Clamp movement values and reject a command with no nonzero movement.
- Validate preset names and tokens by type and length, then XML escape all ONVIF
  values before constructing a SOAP body.
- Apply short network and route timeouts.
- On a movement error, attempt one best-effort stop without retrying the move.
- On a stop error, show the failure and allow another explicit stop; do not start
  a retry loop.
- Disable ordinary controls while the camera is unavailable, but keep the stop
  action enabled.
- Redact credentials, URLs, endpoints, SOAP messages, and device faults from API
  responses, events, and logs.
- Do not log every movement renewal.
- Record home changes, preset create/delete operations, and control errors as
  bounded operational events.

Public errors use stable Korean messages for unavailable, authentication failed,
unsupported, invalid command, and camera timeout states. Detailed errors stay in
the process boundary only when they are safe to log.

## Verification Policy

Verification is real-device-first but deliberately non-repetitive.

### During implementation

- Run only the narrow unit or package test covering the code just changed.
- Do not run `go test ./...`, frontend lint/build, or `go build` after every
  edit or task.
- After a narrow test failure, fix the failure and rerun only that scope.
- Use synthetic ONVIF responses for parser, validation, redaction, and route
  tests; these tests must not move the real camera.

### One final integration pass

Run the full software verification once after the implementation is assembled:

```bash
go test ./...
cd web && npm run lint
cd web && npm run build
go build -o camstationd ./cmd/camstationd
```

If that final pass reveals a defect, run the failing narrow scope while fixing
it, then rerun the affected final command once. Do not repeatedly run the full
matrix without a relevant change.

### One bounded real-camera session

Perform one final real-device session rather than an actuation test after each
code change:

1. refresh and read the capability summary
2. send stop and confirm an idle state
3. perform one low-speed left/stop/right/stop sequence
4. create one temporary preset at the current position, list it, then delete it
5. verify the live PTZ button, right-panel replacement, press/release stop, home
   controls, preset controls, and capability-disabled device buttons in one
   browser session

Do not automatically change the home position because the previous persistent
home value cannot be restored reliably. If the operator confirms that the
existing home target is safe, invoke `홈으로 이동` once during the supervised
session; otherwise record home actuation as intentionally not performed and rely
on the verified capability response plus the focused SOAP and route tests. Never
invoke `현재 위치를 홈으로 설정` as an automated test. Do not repeat movement
merely to collect duplicate evidence. Capture one API trace or log excerpt and
one UI screenshot for the final handoff.

## Non-Goals

- proprietary VStarcam speaker or siren protocol implementation
- browser audio transport or audio transcoding
- talk-back media capture
- automatic patrol, cruise, or tracking
- scheduled PTZ movement
- storing preset coordinates in CamStation
- exposing camera credentials or ONVIF endpoints to the browser
