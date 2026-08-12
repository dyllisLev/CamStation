# camstationd Architecture

## Target Shape

CamStation 2.0 should run as one service:

```text
camstation.service
└── camstationd
```

Internally, `camstationd` manages workers:

```text
camstationd
├─ embedded web UI
├─ HTTP/API server
├─ config manager
├─ camera manager
├─ connection engine
├─ stream manager
├─ recorder manager
├─ storage/backup manager
├─ alert/incident manager
├─ log/event manager
├─ viewer manager
└─ process supervisor
   ├─ go2rtc
   ├─ ffmpeg workers
   └─ backup/rclone workers
```

## Recommended Runtime

A Go daemon is the leading option because the user wants a single program.

Recommended stack:

- Go for `camstationd`
- embedded React build for the web UI
- SQLite for local state
- explicit migrations
- single writer or write queue for DB writes
- supervised child processes for go2rtc, ffmpeg, and backup workers

## Source Of Truth

The database should be the source of truth.

Generated artifacts:

- go2rtc config
- ffmpeg command lines
- backup worker config
- exported YAML/JSON config backups

Operators should not manually edit generated runtime files.

## Logging

All important logs should flow into one event/log system:

- app logs
- go2rtc logs
- ffmpeg logs
- backup logs
- alert logs
- camera state transitions
- settings changes

The web UI should provide search and filters.

## Configuration Ownership

The web UI should control:

- cameras
- streams
- ONVIF
- transport
- keepalive
- recording
- retention
- backup
- alerts
- WebRTC candidates
- viewer app behavior
- system restart/update/export/import

## Security Defaults

- redact RTSP credentials
- redact webhook secrets
- never expose raw go2rtc API output directly
- separate public viewer APIs from admin APIs
- keep export/import explicit

