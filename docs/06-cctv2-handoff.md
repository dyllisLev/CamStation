# Handoff: CamStation 2.0 Complete Rewrite

## Goal

Continue designing and then implementing a new single-program NVR/CCTV system, tentatively named CamStation 2.0.

The user wants a complete rewrite, not incremental patches to the current CamStation. The new system should feel like one program:

- One service/daemon to install and run.
- One web console where all configuration and operations are controlled.
- go2rtc, ffmpeg, backup, logs, camera state, alarms, viewer state, and system diagnostics should be managed by the new program, not scattered across independent scripts and config files.

## Current User Direction

- Development code should be written in the current local folder: `/Users/dyllislev/Documents/dev/camstation`.
- Real camera integration testing should happen on `cctv2`; camera connections only work from that server.
- The current local folder only had `migration-plan.html` at the beginning of the analysis.
- The user wants to think through how to build the new program before implementation.

## Existing System Observed

Primary live system inspected earlier:

- SSH target used during analysis: `root@10.0.0.26`.
- Current app path: `/opt/camstation`.
- Current stack:
  - Python FastAPI backend.
  - React/Vite frontend.
  - Electron Windows viewer app.
  - nginx reverse proxy.
  - go2rtc service.
  - ffmpeg processes for recording and keepalive.
  - rclone/inotify shell backup service.
  - SQLite DB.
- Current systemd services include:
  - `camstation-backend.service`
  - `camstation-backup.service`
  - `go2rtc.service`
  - `nginx.service`
  - `vstarcam-tls-proxy.service`

Important current findings:

- The current setup has 8 enabled cameras and 18 ffmpeg-related processes:
  - 8 recording ffmpeg processes.
  - 8 sub-stream keepalive ffmpeg processes.
  - 2 go2rtc internal transcoding ffmpeg processes.
- Settings are split across DB, `go2rtc.yaml`, Python config, systemd env, shell backup script, nginx, and service files.
- Camera URLs are duplicated in DB and `go2rtc.yaml`.
- `go2rtc` raw API can expose original RTSP URLs, so the new web console must not expose raw go2rtc data without redaction.
- Recent logs showed many alerts and DB lock symptoms:
  - `viewer_health_failed`
  - `viewer_camera_not_receiving`
  - `viewer_stream_degraded`
  - `database is locked`
  - `recording_process_failed`
- SQLite write contention already caused viewer heartbeat 500 errors.
- The existing UI is split across `/`, `/new`, `/new?viewer=1`, `/viewer`, and `/mobile`.
- `NewCamStation.tsx` is large and mixes live, recording, settings, and viewer mode concerns.

## Migration Plan Document Summary

Local file: `/Users/dyllislev/Documents/dev/camstation/migration-plan.html`

That document described:

- Server: `10.0.0.26`
- Cameras: 8
- ffmpeg processes: 18
- 7-day alarms: 377
- Problems:
  - fragmented settings
  - unstable camera streams
  - duplicated/legacy web viewers
  - missing UI controls for alarms, backup, network, go2rtc status
- Proposed earlier patch-level fixes:
  - add ffmpeg reconnect options
  - set go2rtc `on_demand: false`
  - remove sub keepalive ffmpeg
  - route `/` to new UI
  - add settings panels

Important correction:

- Server ffmpeg 6.1.1 shows `-reconnect*` options under HTTP protocol, not RTSP demuxer.
- Therefore `ffmpeg -reconnect` should not be treated as the core RTSP fix.
- Prefer camera connection state machine, go2rtc supervision, transport policy, keepalive, and process backoff.

## Surveillance Station Analysis Integrated

Another Codex thread was read: `019f15db-d97b-72f3-b096-57109fa762d2`.

The useful high-level findings:

- Synology Surveillance Station treats camera connection as an operational system, not a one-time RTSP connect.
- It uses:
  - camera/device profiles
  - RTSP/ONVIF/generic support
  - transport options such as auto, UDP, TCP, HTTP
  - RTSP keepalive via `OPTIONS` or `GET_PARAMETER`
  - frame time correction options
  - event/log/alert dampening
  - separate device pack for camera model compatibility
- For CamStation 2.0, copy the philosophy, not the implementation:
  - camera profiles
  - transport fallback
  - keepalive policy
  - camera state machine
  - incident-based alerting
  - model-specific defaults

## Current Design Direction

The recommended new system is a single daemon, probably named `camstationd`.

Proposed high-level architecture:

```text
camstationd
├─ embedded Web UI
├─ config manager
├─ camera manager
├─ connection engine
├─ stream manager
├─ recorder manager
├─ storage/backup manager
├─ alert/incident manager
├─ log/event manager
├─ viewer manager
└─ child process supervisor
   ├─ go2rtc
   ├─ ffmpeg workers
   └─ backup/rclone workers
```

User-facing principle:

```text
Install one program.
Run one service.
Open one web console.
Control everything there.
```

Implementation preference discussed:

- A Go daemon is a strong fit for the "single program" requirement.
- The UI can be a built React app embedded into the Go binary.
- SQLite is still reasonable, but the new design must use explicit migrations and a single writer/write queue pattern.
- go2rtc and ffmpeg should still be used, but as supervised child processes controlled by `camstationd`, not independent user-managed services.

## Required Web Console Areas

The new web UI should include:

- Dashboard:
  - global health
  - camera online/offline/degraded counts
  - active incidents
  - recent logs
  - disk/CPU/RAM
  - backup status
  - viewer status
- Live:
  - camera grid
  - layout management
  - viewer mode
- Recordings:
  - date/camera timeline
  - playback
  - gaps/offline intervals
  - download/export
- Cameras:
  - camera add/edit/delete
  - main/sub RTSP URLs
  - ONVIF settings
  - test connection
  - reboot if supported
- Connection:
  - camera profile
  - transport policy: `auto`, `tcp`, `udp`, `http`
  - keepalive policy: `OPTIONS`, `GET_PARAMETER`, `none`
  - timeout/backoff settings
  - last packet / last keepalive / producer status
- Streams/go2rtc:
  - generated config preview
  - safe stream status summary
  - WebRTC candidates
  - no raw secret exposure
- Recording:
  - segment length
  - retention
  - max storage
  - ffmpeg policy
  - recorder state
- Backup:
  - rclone target
  - queue/status
  - failures/retry
  - local deletion policy
- Alerts:
  - webhook URL/secret
  - incident rules
  - cooldown
  - test alert
  - snooze/acknowledge
- Logs:
  - app logs
  - go2rtc logs
  - ffmpeg logs
  - backup logs
  - alert/incident logs
  - searchable/filterable
- System:
  - service restart
  - update
  - config export/import
  - diagnostic bundle

## Camera Connection Engine Requirements

This is central.

Each camera should have:

- profile/vendor/model metadata
- main stream and sub stream config
- preferred transport
- fallback transport order
- keepalive method
- timeout
- reconnect backoff policy
- current state
- last successful connection
- last packet timestamp
- last keepalive response
- recent failure history

States:

- `disabled`
- `connecting`
- `streaming`
- `degraded`
- `reconnecting`
- `offline`
- `recovering`

Incident rules:

- Do not send an alert for every transient reconnect.
- Open incident after sustained failure.
- Mark flapping when repeated failure/recovery happens.
- Resolve after stable recovery period.
- Allow web UI acknowledge/snooze.

## Safety and Migration Constraints

- Do not disrupt the current CCTV operation during early development.
- Since camera connections only work on `cctv2`, local development must support fake/simulated camera and process modes.
- For actual cctv2 tests:
  - use a separate install path and port first
  - connect only 1-2 cameras initially
  - avoid touching current production services until the new program is stable
- No secrets should be committed or placed in handoff docs.
- RTSP URLs and tokens should be redacted in logs/API output by default.

## Suggested Build Strategy

Phase 1: Design/spec

- Decide exact language/runtime.
- Decide process model.
- Define DB schema and config ownership.
- Define migration/import path from current CamStation.
- Define test strategy for local fake mode and cctv2 real mode.

Phase 2: Core prototype

- `camstationd` starts.
- Web UI serves.
- SQLite migrations run.
- Settings can be saved.
- Logs/events are visible.
- Fake camera mode works locally.

Phase 3: Process supervisor

- Manage go2rtc as child process.
- Generate go2rtc config from DB.
- Summarize go2rtc state safely.
- Manage ffmpeg recording workers.

Phase 4: Real camera test on cctv2

- Deploy to separate path.
- Bind to non-production port.
- Import 1 camera.
- Verify live stream, reconnect, logs, and recorder.

Phase 5: Full NVR features

- Recording timeline.
- Backup manager.
- Alert incident manager.
- Viewer app integration.
- Migration tooling.

## Immediate Next Question for User

Before writing code, clarify:

Should the first implementation target be:

1. A Go single-binary daemon with embedded web UI, or
2. A simpler transitional local prototype using existing FastAPI/React patterns before converting to Go?

Recommendation: choose Go single-binary if the user truly wants a single program and complete rewrite.

