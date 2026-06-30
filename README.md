# CamStation 2.0

CamStation 2.0 is a planned complete rewrite of the current CamStation CCTV/NVR system.

The goal is a single-program NVR:

- one daemon/service to run
- one web console to control everything
- camera, stream, recording, backup, logs, alerts, viewer, and system settings managed from the web UI
- go2rtc, ffmpeg, and backup workers supervised by the program instead of being manually scattered across services and scripts

## Documents

- `docs/00-project-summary.md` — current direction and build goal
- `docs/01-current-system-findings.md` — findings from the existing CamStation system
- `docs/02-surveillance-station-lessons.md` — lessons from Synology Surveillance Station analysis
- `docs/03-camstationd-architecture.md` — proposed single-daemon architecture
- `docs/04-cctv2-test-plan.md` — real-camera testing plan for cctv2
- `docs/05-next-decisions.md` — decisions to make before coding

## Existing Reference Files

- `migration-plan.html` — earlier patch-level improvement plan
- `handoff-cctv2-camstation2.md` — handoff summary copied to `cctv2:/root/handoff-cctv2-camstation2.md`
- `file/` — local Surveillance Station package files used only for high-level comparison

## Current Development Environment

This workspace now contains the first `camstationd` skeleton:

- Go daemon entrypoint: `cmd/camstationd`
- SQLite store and migrations: `internal/store`
- ffprobe-based single-camera smoke test: `internal/camera`
- Minimal embedded web console: `cmd/camstationd/web`

Installed tools on this server:

- Go
- Node/npm
- SQLite CLI
- ffmpeg/ffprobe
- rclone

go2rtc is not required for the first smoke test yet.

## Run Locally

```bash
make test
make build
./camstationd -addr :18080 -db ./data/camstation.db
```

Open:

```text
http://SERVER_IP:18080/
```

Health and events:

```bash
curl http://127.0.0.1:18080/api/health
curl http://127.0.0.1:18080/api/events
```

## Single Camera Smoke Test

Use one camera URL through an environment variable so credentials do not enter shell history-heavy commands more than necessary:

```bash
export CAMSTATION_CAMERA_URL='rtsp://user:pass@camera-host:554/path'
make probe
```

Or test through the running web console. Probe results and stored events redact `user:pass@` by default.
