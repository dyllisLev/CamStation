# Camera Profile Router Design

## Goal

Replace one-URL camera registration with a camera-aware registration and stream
routing layer.

CamStation should no longer treat a camera as only an RTSP URL. Operators should
register a camera by entering connection details, let CamStation scan the device,
then choose which discovered stream profiles are used for recording, live
monitoring, snapshots, and diagnostics.

The first implementation targets only the currently registered cameras:

- TP-Link Tapo C320WS cameras: `집-창고1`, `집-창고2`, `집-마당`
- Reolink Duo WiFi channels: `소방서3`, `소방서4`

## Product Principles

- A camera remains one user-facing camera in the UI, timeline, layouts, events,
  and recording pages.
- Internally, a camera can have multiple stream roles.
- Recording should use the high-quality/main stream by default.
- Live monitoring should use the low-bandwidth/sub stream by default.
- Initial scanning and profile matching are read-only. CamStation should not
  change camera encoder settings until a future explicit "apply optimization"
  workflow exists.
- The database remains the source of truth. go2rtc config and ffmpeg command
  lines are generated artifacts.
- Camera credentials must remain secret. API responses, events, logs, docs, and
  generated diagnostics must redact them.

## Current Camera Findings

### TP-Link Tapo C320WS

The three home cameras responded to ONVIF using the existing RTSP credentials.

Observed device identities:

| Camera | Host | Manufacturer | Model | Firmware |
| --- | --- | --- | --- | --- |
| `집-창고1` | `192.168.0.4` | `tp-link` | `Tapo C320WS` | `1.4.2 Build 250725 Rel.72234n` |
| `집-창고2` | `192.168.0.9` | `tp-link` | `Tapo C320WS` | `1.4.2 Build 250725 Rel.72234n` |
| `집-마당` | `192.168.0.81` | `tp-link` | `Tapo C320WS` | `1.3.5 Build 250522 Rel.45630n` |

Observed ONVIF media profiles:

| ONVIF profile | Role | RTSP path | Expected use |
| --- | --- | --- | --- |
| `mainStream` | main | `/stream1` | recording |
| `minorStream` | sub | `/stream2` | live monitoring |
| `jpegStream` | jpeg | `/stream8` | snapshot or diagnostics |

Observed resolutions:

| Camera | Main | Sub |
| --- | --- | --- |
| `집-창고1` | `2560x1440`, H.264, 15fps, 2048 kbps | `640x360`, H.264, 15fps, 256 kbps |
| `집-창고2` | `1280x720`, H.264, 15fps, 2048 kbps | `640x360`, H.264, 15fps, 256 kbps |
| `집-마당` | `1280x720`, H.264, 15fps, 1536 kbps | `640x360`, H.264, 15fps, 256 kbps |

Tapo adapter policy:

- Use ONVIF first.
- Query `GetDeviceInformation`, `GetProfiles`, and `GetStreamUri`.
- Select `mainStream` for recording.
- Select `minorStream` for live monitoring.
- Store `jpegStream` as a snapshot/diagnostics candidate.
- Use RTSP over TCP through go2rtc for actual media transport.

### Reolink Duo WiFi

The fire-station camera is one Reolink Duo WiFi device with two channels. The
existing RTSP credentials successfully authenticated against the Reolink HTTP
API.

Observed device identity:

| Host | Model | Type | Channels | Firmware |
| --- | --- | --- | --- | --- |
| `192.168.0.12` | `Reolink Duo WiFi` | `MULTI_IPC` | `2` | `v3.0.0.684_21110101` |

Observed channel mapping:

| Camera | Channel | Main path | Sub path |
| --- | --- | --- | --- |
| `소방서3` | `0` | `/h264Preview_01_main` | `/h264Preview_01_sub` |
| `소방서4` | `1` | `/h264Preview_02_main` | `/h264Preview_02_sub` |

Observed encoder settings for both channels:

| Role | Resolution | H.264 profile | Frame rate | Bitrate |
| --- | --- | --- | --- | --- |
| main | `2560x1440` | High | 15fps | 3072 kbps |
| sub | `640x360` | High | 10fps | 256 kbps |

Reolink adapter policy:

- Use Reolink HTTP API first.
- Query `GetDevInfo`, `GetChannelstatus`, and `GetEnc`.
- Use channel index to derive `h264Preview_0{channel+1}_main` and
  `h264Preview_0{channel+1}_sub`.
- Use main for recording and sub for live monitoring.
- Use ONVIF only as a fallback or secondary validation path.
- Use RTSP over TCP through go2rtc for actual media transport.

## Proposed Architecture

```text
Camera Registration Wizard
  -> Camera Scanner
  -> Profile Adapter
  -> Stream Role Mapper
  -> Camera Store
  -> go2rtc Config Generator
  -> Recorder Manager / Live UI
```

### Camera Scanner

The scanner receives host, credentials, and optional ports. It performs
read-only discovery:

- TCP reachability for known ports: RTSP, HTTPS, HTTP, ONVIF.
- ONVIF device and media queries when available.
- Vendor API queries for supported adapters.
- RTSP probes for candidate streams.

Scanner output is normalized into a device profile report:

```json
{
  "manufacturer": "tp-link",
  "model": "Tapo C320WS",
  "host": "192.168.0.4",
  "channels": [
    {
      "index": 0,
      "name": "main",
      "profiles": [
        {
          "id": "mainStream",
          "roleHint": "recording",
          "url": "rtsp://redacted@192.168.0.4:554/stream1",
          "codec": "H264",
          "width": 2560,
          "height": 1440,
          "fps": 15,
          "bitrateKbps": 2048
        }
      ]
    }
  ]
}
```

### Profile Adapters

Adapters turn device-specific discovery into normalized stream candidates.

Initial adapters:

- `generic-rtsp`
- `tplink-tapo-c320ws`
- `reolink-duo-wifi`

Adapter responsibilities:

- identify whether the device matches
- query available profiles without changing camera settings
- generate RTSP URL candidates
- propose default role assignments
- describe known limitations and warnings

### Stream Role Mapper

The mapper assigns discovered stream candidates to CamStation roles:

- `recording`: high-quality stream used by ffmpeg workers
- `live`: low-bandwidth stream used by `/live` and preview modals
- `snapshot`: JPEG or low-cost still stream, optional
- `diagnostics`: future health/probe stream, optional

Default assignments:

| Adapter | Recording | Live | Snapshot |
| --- | --- | --- | --- |
| Tapo C320WS | `mainStream` / `stream1` | `minorStream` / `stream2` | `jpegStream` / `stream8` |
| Reolink Duo WiFi | channel main | channel sub | none in v1 |
| Generic RTSP | manual URL | same as recording unless supplied | none |

## Data Model

The existing `cameras` table currently stores one URL and one `stream_name`.
The new model should preserve camera identity while adding role-specific
streams.

Conceptual tables:

### `cameras`

- `id`
- `name`
- `manufacturer`
- `model`
- `profile_adapter`
- `host`
- `rtsp_port`
- `http_port`
- `onvif_port`
- `channel_index`
- credential storage reference using current secret handling in v1
- `state`
- `last_scan_json`
- `last_probe_json`
- timestamps

### `camera_streams`

- `id`
- `camera_id`
- `role`
- `label`
- `source`
- `url`
- `go2rtc_stream_name`
- `codec`
- `width`
- `height`
- `fps`
- `bitrate_kbps`
- `profile_token`
- `state`
- `last_probe_json`
- timestamps

Role uniqueness:

- A camera can have at most one active `recording` stream.
- A camera can have at most one active `live` stream.
- A camera may have zero or one `snapshot` stream in v1.

### Compatibility Fields

For the first migration, existing API shapes can expose:

- `camera.streamName`: the stable legacy camera key for layout compatibility
- `camera.recordingStreamName`: the recording stream name
- `camera.liveStreamName`: the live stream name
- `camera.layoutKey`: the stable camera key, equal to the legacy
  `streamName` in v1

Frontend code must stop treating `camera.streamName` as both the layout key and
the playable go2rtc stream. The live workspace should use the camera key for
layout identity and `liveStreamName` for playback. The recorder manager should
use `recordingStreamName`.

## Generated go2rtc Streams

go2rtc should receive one generated stream per active role, not one stream per
camera.

Example output:

```yaml
streams:
  fire-station-3-recording:
    - "rtsp://redacted@192.168.0.12:554/h264Preview_01_main"
  fire-station-3-live:
    - "rtsp://redacted@192.168.0.12:554/h264Preview_01_sub"
  warehouse-1-recording:
    - "rtsp://redacted@192.168.0.4:554/stream1"
  warehouse-1-live:
    - "rtsp://redacted@192.168.0.4:554/stream2"
```

The recorder manager must use `recording` role streams:

```text
rtsp://127.0.0.1:8554/{recordingStreamName}
```

The live UI must use `live` role streams:

```text
/player/api/ws?src={liveStreamName}
```

Recording segment metadata should still use the camera identity and a recording
stream name. The UI should group segments under the camera name, not under the
role stream name.

Timeline APIs should migrate toward camera-based queries. During v1 migration,
legacy stream-name queries can be translated through the camera key to the
camera's active recording stream.

## Registration Wizard

Replace the single RTSP URL form on `/cameras` with a staged wizard.

### Step 1: Connection Details

Fields:

- display name
- host or IP
- username
- password
- optional RTSP port, default `554`
- optional ONVIF port, default auto
- optional HTTP/HTTPS port, default auto
- manufacturer/profile selection: `Auto`, `Tapo C320WS`, `Reolink Duo WiFi`,
  `Generic RTSP`

Actions:

- `Scan`
- `Cancel`

### Step 2: Scan Results

Show:

- detected manufacturer and model
- firmware
- channels
- ONVIF status
- vendor API status
- RTSP probe status
- discovered stream candidates

For multi-channel devices such as Reolink Duo WiFi, the operator must choose
which channel the new CamStation camera represents.

### Step 3: Role Assignment

Show stream candidates in a compact table:

- role
- candidate name
- source
- codec
- resolution
- fps
- bitrate
- probe result

Controls:

- recording stream selector
- live stream selector
- snapshot stream selector, optional
- direct URL override for generic or troubleshooting cases

Default role assignment should be preselected by the adapter.

### Step 4: Verify And Save

Show:

- recording probe result
- live probe result
- go2rtc stream names
- expected recorder input
- expected live player source

Saving should:

- write camera and stream role records
- regenerate go2rtc config
- restart managed go2rtc through the existing safe lifecycle
- reconcile recorder workers when recording is enabled
- append a redacted camera event

## Camera Detail / Profile Settings

Each camera row should expose `프로파일 설정`.

The detail panel or page should show:

- identity: name, manufacturer, model, firmware, host, channel
- credentials status without revealing secrets
- stream role assignments
- current go2rtc runtime status per role
- recorder worker status for the recording role
- last scan and last probe results
- `다시 스캔`
- `역할 변경`
- `저장 후 적용`

The detail UI must not expose raw credentials. RTSP URLs must be redacted by
default, with any future reveal action requiring explicit interaction and audit.

## Migration Plan For Current Cameras

Migration should be deterministic and testable.

Existing cameras:

| Camera | Adapter | Channel | Recording role | Live role |
| --- | --- | --- | --- | --- |
| `집-창고1` | `tplink-tapo-c320ws` | `0` | `/stream1` | `/stream2` |
| `집-창고2` | `tplink-tapo-c320ws` | `0` | `/stream1` | `/stream2` |
| `집-마당` | `tplink-tapo-c320ws` | `0` | `/stream1` | `/stream2` |
| `소방서3` | `reolink-duo-wifi` | `0` | `/h264Preview_01_main` | `/h264Preview_01_sub` |
| `소방서4` | `reolink-duo-wifi` | `1` | `/h264Preview_02_main` | `/h264Preview_02_sub` |

Migration should preserve:

- camera IDs where possible
- camera display names
- existing layout camera identity
- recording history grouped by camera
- existing recording folder names based on camera names

The old `stream_name` should remain the stable camera key for backward
compatibility. New code should use explicit camera keys and role stream names
instead of assuming the camera key is directly playable.

## Error Handling

Registration scan can partially succeed.

Examples:

- ONVIF succeeds but vendor API fails: use ONVIF profile candidates.
- Vendor API succeeds but ONVIF fails: use vendor adapter candidates.
- RTSP probe fails for one candidate: show candidate with warning and prevent it
  from being the default role.
- Multi-channel device detected: require channel selection before saving.
- Existing camera with same host/channel exists: warn and require confirmation.

go2rtc restart failure should not erase camera settings. The API should return
the saved camera plus an apply warning, as the current camera registration path
already does.

## Verification

Automated tests:

- Tapo adapter parses ONVIF device info, profiles, and stream URIs.
- Reolink adapter parses `GetDevInfo`, `GetChannelstatus`, and `GetEnc`.
- Stream role mapper selects main for recording and sub for live.
- go2rtc config generation emits role streams.
- recorder manager uses recording role stream names.
- live workspace uses live role stream names while layouts remain camera-based.
- camera API redacts all credentials.
- migration creates expected role streams for existing cameras.

Applied runtime verification:

- Register or migrate the five current cameras.
- Confirm generated `data/go2rtc.yaml` contains recording/live role streams.
- Confirm `/api/streams/status` reports live and recording role streams.
- Confirm `/live` plays sub streams for all cameras.
- Confirm recorder workers use local go2rtc recording inputs.
- Confirm finalized recordings still use recognizable camera names.
- Confirm Reolink main recordings continue while live uses sub streams.
- Confirm Tapo live uses `stream2` and recording uses `stream1`.

## Out Of Scope For V1

- Changing camera encoder settings.
- Automatic bitrate/frame-rate optimization writes.
- ONVIF discovery broadcast across the LAN.
- User authentication/authorization.
- Secret encryption redesign beyond preserving current secret hygiene.
- Full device fleet management.
- Motion event integration.
- Backup integration.

## References

- TP-Link Tapo third-party/NVR guidance: `https://www.tp-link.com/us/support/faq/2680/`
- Local ONVIF observations from current Tapo C320WS cameras.
- Local Reolink HTTP API observations from the current Reolink Duo WiFi device.
