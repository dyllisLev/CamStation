# Current System Findings

## Existing System

The current CamStation system inspected earlier runs under `/opt/camstation` on the CCTV server.

Observed components:

- FastAPI backend
- React/Vite frontend
- Electron Windows viewer app
- nginx
- go2rtc
- ffmpeg recording processes
- ffmpeg sub-stream keepalive processes
- rclone/inotify backup shell service
- SQLite database
- systemd services

## Main Problems

### Fragmented Configuration

Settings are split across:

- SQLite DB
- `go2rtc.yaml`
- Python config
- systemd environment variables
- backup shell script
- nginx config
- service files

Camera URLs are duplicated in both DB and `go2rtc.yaml`.

### Process Sprawl

The current runtime showed 18 ffmpeg-related processes:

- 8 recording processes
- 8 sub-stream keepalive processes
- 2 go2rtc internal transcoding processes

This makes system status hard to reason about from the web UI.

### Alert Noise

Recent logs showed repeated alert patterns:

- `viewer_health_failed`
- `viewer_camera_not_receiving`
- `viewer_stream_degraded`
- `recording_process_failed`

Alerts are too event-like and not enough incident-like. A single ongoing problem can generate many notifications.

### SQLite Locking

The current system has multiple writers:

- recorder
- viewer heartbeat
- backup script
- API updates

Logs showed `database is locked` and viewer heartbeat 500 responses. The rewrite needs a more deliberate write model.

### UI Duplication

Existing routes are split:

- `/` classic UI
- `/new` new UI
- `/new?viewer=1` viewer mode
- `/viewer` legacy viewer
- `/mobile` mobile UI

The new program should reduce this to clear surfaces:

- main console
- viewer mode
- mobile view

### Secret Exposure Risk

go2rtc raw API can expose RTSP URLs. The new program must never expose raw camera credentials through the UI or public API. All connection strings and secrets must be redacted by default.

## Important Correction

The earlier plan suggested ffmpeg `-reconnect` options as a key fix.

Server ffmpeg 6.1.1 shows `-reconnect*` under HTTP protocol options, not RTSP demuxer options. RTSP reliability should be handled through:

- camera connection state machine
- transport policy
- RTSP keepalive
- go2rtc supervision
- ffmpeg process supervision
- backoff/retry policy
- incident-based alerting

