# CamStation 2.0

CamStation 2.0 is the Go-based complete rewrite of the CamStation CCTV/NVR system.

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
- `docs/07-implementation-status.md` — current implemented/not-implemented task status and handoff notes
- `docs/deployment.md` — Forgejo Registry and OpenShip production deployment, verification, backup, and rollback runbook
- `docs/2026-08-09_cctv-1x-to-2x-production-cutover-strategy.md` — same-host 1.x-to-2.0 production replacement, validation, and rollback strategy
- `docs/2026-08-09_cctv-2x-cutover-readiness-report.md` — implemented preparation, current Go/No-Go matrix, and exact next actions
- `docs/2026-08-09_camstation2-docker-canary-operations.md` — running home-camera Docker canary, `/viewer` URL, verification, maintenance, and rollback

## Existing Reference Files

- `migration-plan.html` — earlier patch-level improvement plan
- `handoff-cctv2-camstation2.md` — handoff summary copied to `cctv2:/root/handoff-cctv2-camstation2.md`
- `file/` — local Surveillance Station package files used only for high-level comparison

## Architecture

This workspace contains:

- Go daemon entrypoint: `cmd/camstationd`
- SQLite store and migrations: `internal/store`
- supervised go2rtc/ffmpeg, recording, cleanup, and backup packages: `internal/`
- React/Vite web console source: `web`
- Embedded web console build output: `cmd/camstationd/web`
- Electron Viewer and Windows service/installer components: `viewer-app/`, `cmd/camstation-viewer-*`, `installer/`

The database is the source of truth. Generated go2rtc configuration, recordings, logs,
and other runtime state stay under the ignored `data/` directory.

## Development Setup

Required host tools:

- Linux x86-64, `curl`, `tar`, and `sha256sum` for the automatic Go install
- Node.js 22 and npm

Bootstrap the checkout with:

```bash
./scripts/setup-dev.sh
```

The script installs the official Go 1.25.12 toolchain under `.tools/`, verifies its
SHA-256 checksum, downloads Go modules, runs `npm ci` for the web console and Viewer,
and prepares `data/`. Both directories are ignored by Git. It does not change a system
Go installation.

Optional integration tools:

- `ffmpeg`/`ffprobe` for camera probing and recording
- `go2rtc` for live playback and camera stream supervision
- `rclone` for backup jobs
- `sqlite3` only for manual database inspection; the daemon uses an embedded Go driver

The daemon and web console start without cameras, go2rtc, or rclone. Those integrations
report unavailable until their binaries and configuration are supplied.

## Run Locally

Start the daemon and Vite in separate terminals:

```bash
./scripts/dev-daemon.sh
./scripts/dev-web.sh
```

Open:

```text
http://127.0.0.1:5173/
```

The Vite server proxies `/api` and `/player` to `camstationd`. The React `/live` route
stays on Vite so source changes are visible during frontend development.

Direct daemon health and events:

```bash
curl http://127.0.0.1:18080/api/health
curl http://127.0.0.1:18080/api/events
```

Useful overrides:

```bash
CAMSTATION_DEV_PORT=28080 ./scripts/dev-daemon.sh
CAMSTATION_DEV_SERVER_URL=http://127.0.0.1:28080 ./scripts/dev-web.sh
```

Recording is disabled by default in the development launcher. Enable it only for an
intentional camera test:

```bash
CAMSTATION_RECORDING_ENABLED=true ./scripts/dev-daemon.sh
```

## Paseo

`paseo.json` configures worktree bootstrap and two services:

- `daemon`: `camstationd` on the Paseo-assigned port with worktree-local `data/`
- `web`: Vite on a separate assigned port, using `PASEO_SERVICE_DAEMON_PORT`

In a new Paseo worktree, setup runs automatically. In the Paseo Scripts UI, start
`daemon` and then `web`, or use:

```bash
paseo script start daemon --cwd "$PWD"
paseo script start web --cwd "$PWD"
paseo script ls --cwd "$PWD"
```

Open the URL shown for the `web` service. `setup`, `test`, and `check` are also exposed
as Paseo scripts. Assigned service ports come from `20000-20999`, so the normal
development ports remain available.

The Project Settings screen reads the root `paseo.json`. If that screen was already
open when the file changed, return to Projects and reopen CamStation (or reload the
Paseo client) to fetch the current values. Teardown is intentionally unset because
the safe development setup creates only worktree-local ignored files; archiving the
worktree owns their removal. Commit `paseo.json` on the selected base branch before
creating a new worktree, because uncommitted source-checkout changes are not inherited.

## Verify

```bash
./scripts/test-dev.sh   # Go, web, and Viewer tests
./scripts/check-dev.sh  # tests, lint, all builds
```

Equivalent focused commands remain available through `make test`, `make build`, and
`make check`.

## Single Camera Smoke Test

Use one camera URL through an environment variable so credentials do not enter shell history-heavy commands more than necessary:

```bash
export CAMSTATION_CAMERA_URL='rtsp://user:pass@camera-host:554/path'
make probe
```

Or test through the running web console. Probe results and stored events redact `user:pass@` by default.

## Live View Prototype

After registering a camera in the web console, CamStation writes a generated go2rtc config and starts go2rtc as a managed child process.

Open live video through CamStation, not through the raw go2rtc port:

```text
http://SERVER_IP:18080/player/stream.html?src=STREAM_NAME
```

Security rule for the prototype:

- go2rtc API and RTSP listeners are bound to `127.0.0.1`.
- CamStation exposes only the minimal live-player paths under `/player/`.
- Raw go2rtc status APIs such as `/api/streams` are not exposed because they can include camera credentials.
