# CamStation Agent Guide

This file is the working index for future agents. It summarizes the project
direction, session history, development rules, and verification habits learned
from the previous Codex sessions in this workspace.

## Read First

Start every non-trivial task by checking these files:

- `README.md` for the current build/run commands.
- `docs/07-implementation-status.md` for implemented, partial, and missing work.
- `docs/00-project-summary.md` for the product goal.
- `docs/03-camstationd-architecture.md` for the single-daemon architecture.
- `docs/04-cctv2-test-plan.md` for real-camera test safety rules.
- `docs/superpowers/specs/` and `docs/superpowers/plans/` for active feature specs/plans.

Before editing, run `git status --short --branch`. This workspace often has
active user/agent changes. Do not revert unrelated changes.

## Session Index

These are the relevant prior sessions that informed this guide:

- `2026-06-30T01-15-17`, `/root/.ssh`, id `019f1618-47a5-76e1-bc20-2b6ee241d95e`: created SSH key material for the `cctv` host alias.
- `2026-06-30T01-18-25`, `/root/camstation`, id `019f161b-2356-7cc1-bdf7-7df21e69ab30`: initial environment setup, docs review, Go/React skeleton, live monitoring, recording foundation, real-camera tests, cleanup tests, status documentation.
- `2026-06-30T01-41-10`, `/root/camstation`, id `019f162f-f8fc-7451-b5c8-668ba9d42648`: GitHub secret review, repository cleanup/publication discussion, Codex OSS application text.
- `2026-06-30T23-01-37`, `/root/camstation`, id `019f1ac4-40d5-7d11-b9e2-060a3bc55e65`: production/development server confusion, restore check, KST clarification.
- `2026-06-30T23-15-20`, `/root/camstation`, id `019f1ad0-d0dc-7b72-aa0e-2f355fc42906`: server status check and Superpowers skill installation.
- `2026-06-30T23-17-36`, `/root/camstation`, id `019f1ad2-e3c8-7401-be94-2c1b7c43e989`: server access debugging, `/new` confusion correction, control room versus live page separation, restart-control removal.
- `2026-07-01T00-40-30`, `/root/camstation`, id `019f1b1e-c934-7df1-bd18-0463eb98096a`: camera display names versus internal stream names, recording folder/file naming concern.
- `2026-07-01T00-42-58`, `/root/camstation`, id `019f1b21-0d53-70b1-b559-3c7568edae34`: creation of this guide.

Raw sources live under `/root/.codex/session_index.jsonl` and
`/root/.codex/sessions/2026/...`. `jq` is installed on this machine for
structured session inspection.

## Product Direction

CamStation 2.0 is a new implementation, but not a new product category. The
user wants an upgraded CCTV/NVR monitoring system, not a generic dashboard.

Core direction:

- One `camstationd` daemon controls the web UI, API, SQLite state, go2rtc,
  ffmpeg recording workers, backup workers, logs, alerts, and diagnostics.
- The database is the source of truth. go2rtc config, ffmpeg commands, and
  runtime files are generated artifacts.
- Operators should use the web UI, not manually edit scattered config files.
- Real camera tests happen on `cctv2`; camera access is available from this
  server. Do not disrupt the existing CCTV operation.

## Server And Route Safety

Be very careful with environment identity:

- `cctv` is the legacy/production server and should continue serving the old
  operating system unless the user explicitly says otherwise.
- `cctv2` is the development/test target for CamStation 2.0.
- The current test URL is usually `http://10.0.0.29:18080/`.
- The current app does not use `/new`. Treat `/new` as a legacy CamStation 1.0
  reference only when the user explicitly asks to inspect it.
- Use KST when explaining times to the user if server timestamps matter.

If restarting the development server, use `scripts/camstationctl.sh`; do not
hand-roll `kill`, `nohup`, `setsid`, `pkill`, or `killall` commands during
normal development. The control script scopes process management to this
workspace and follows the fixed lifecycle: status, stop, cleanup managed
leftovers, start, verify. Do not kill unrelated production services.

## Architecture Map

Important backend locations:

- `cmd/camstationd/main.go`: daemon entrypoint, flags, routes, API wiring,
  static web serving.
- `internal/store`: SQLite schema, migrations, camera/layout/recording/event
  persistence, redaction helpers.
- `internal/stream`: managed go2rtc process/config/status adapter.
- `internal/recorder`: ffmpeg recording manager, segment finalization, recovery.
- `internal/cleanup`: capacity cleanup for finalized recordings.
- `internal/camera`: ffprobe camera probing and redaction.
- `scripts/hourly-recording-monitor.sh`: operational recording monitor.

Important frontend locations:

- `web/src/app`: routing, API client, query hooks, i18n, base path helpers.
- `web/src/layouts/ConsoleLayout.tsx`: left console layout and shell behavior.
- `web/src/pages/ControlRoomPage.tsx`: control room overview. It should not
  render `LiveWorkspace` directly.
- `web/src/pages/LivePage.tsx`: owns the live monitoring workspace.
- `web/src/components/live/LiveWorkspace.tsx`: live grid, timeline shell,
  layout persistence, focus view, wheel zoom.
- `web/src/styles/index.css`: shared monitoring/console styling.
- `cmd/camstationd/web`: embedded frontend build output generated by Vite.

## UI Rules Learned From Review

The user strongly prefers preserving the CCTV monitoring workflow:

- `/live` is the main monitoring screen with camera tiles, timeline, saved
  layouts, focus view, fullscreen, and wheel zoom.
- `/` is the control room dashboard: status overview, recording state, stream
  state, storage, events, and optional single-camera inner preview. It should
  not auto-play the full live grid.
- Keep the existing left sidebar console model. Do not accidentally replace it
  with top-only navigation.
- Default UI language is Korean.
- The visual language should be operational and dense: dark monitoring palette,
  matching panels/tables/buttons/forms, clear status indicators.
- Native browser video controls/progress bars should not appear on live tiles.
  Use the direct MSE video path already in the code.
- The user's "video zoom" means mouse-wheel zoom on the video content with pan
  and persisted `videoZoom`, not object-fit changes.
- `focus view` is separate from video wheel zoom. It enlarges a camera tile
  in-page and should not open a popup window.

Use existing libraries instead of hand-rolling:

- React 19, React Router 7, TanStack Query, lucide-react.
- Radix UI components where already used.
- `react-grid-layout` for live tile move/resize behavior.

## Recording And Streaming Policy

Default policy:

- go2rtc is the local stream hub.
- Recorder workers should read local go2rtc RTSP inputs:

```text
rtsp://127.0.0.1:8554/{streamName}
```

- Do not make direct camera recording the default. It can become a later
  troubleshooting/special-camera option.
- Active ffmpeg segments write under `data/temp`.
- Completed segments move under `data/recordings`.
- SQLite recording segment metadata drives timelines and cleanup decisions.
- Capacity cleanup deletes only finalized `ready` segments; never delete active
  temp recordings.
- Recording workers do not auto-start unless
  `CAMSTATION_RECORDING_ENABLED=true` or `-recording-enabled` is set.

Camera identity rule:

- `streamName` is an internal stable key for go2rtc, workers, API lookup, and
  layout IDs.
- `name` is the human-facing camera name.
- Do not expose `streamName` as the primary user-facing camera label when
  `name` is available.
- Per the latest user correction, recording folder/file naming should preserve
  a recognizable camera name so files remain understandable outside the app.

## Security Rules

This project handles camera credentials, RTSP URLs, webhook secrets, local
network details, and recordings. Treat secret hygiene as a product feature.

- Never commit `.env`, `*.secret`, DB files, generated `config/go2rtc.yaml`,
  runtime logs, or recording data.
- Redact RTSP credentials in logs, API responses, events, docs, and examples.
- Never expose raw go2rtc API output to the public web UI/API; it can include
  camera URLs.
- CamStation should proxy only the minimal safe player paths under `/player/`.
- Keep go2rtc API/RTSP listeners bound to `127.0.0.1` unless there is an
  explicit, reviewed reason to change that.
- Before making a public release or pushing sensitive changes, inspect GitHub
  history and tracked files for secrets, not just local ignored files.

## Development Workflow

For larger features, follow the documented spec-plan pattern already used in
`docs/superpowers/`:

- Follow the user's required development sequence: analysis, design,
  development, then testing. Testing means real applied-behavior verification,
  not only "build passes".
- Clarify the product behavior first.
- Write or update a design spec in `docs/superpowers/specs/`.
- Write or update an implementation plan in `docs/superpowers/plans/`.
- Prefer TDD for behavior changes and regression tests for user corrections.
- Keep changes scoped to the relevant backend/frontend modules.
- Update `docs/07-implementation-status.md` when implemented state or next
  tasks change.

For small fixes, still inspect the surrounding code and add a focused
regression test when the bug is likely to recur.

## Build And Verification

Common commands:

```bash
make test
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
```

Code edits are not complete until they are applied to the artifact/runtime the
user is using. After backend changes, run `go build -o camstationd
./cmd/camstationd`; if a development daemon is already running, restart that
daemon with the same flags and verify the live API/process state reflects the
new behavior. After frontend changes, run the Vite build and verify the
embedded `cmd/camstationd/web` assets are the ones being served. Do not claim a
runtime behavior was tested from source-level tests alone.

Build tests are not a substitute for actual applied confirmation. For every
behavior change, define and run at least one verification that proves the
changed behavior in the running system or an equivalent executable environment:
API response, process command line, generated file/path, database row, UI smoke
check, log line, or another concrete observable. Report that evidence to the
user.

When frontend source changes, run `cd web && npm run build` and include the
updated `cmd/camstationd/web` assets if the change is meant to run in the
embedded daemon.

Local/dev run command:

```bash
scripts/camstationctl.sh start
```

Recording test run commonly uses:

```bash
scripts/camstationctl.sh restart
```

Development daemon lifecycle:

```bash
scripts/camstationctl.sh status
scripts/camstationctl.sh stop
scripts/camstationctl.sh start
scripts/camstationctl.sh restart
scripts/camstationctl.sh verify
```

The script starts `camstationd` with recording enabled, 5-minute segments, and
the current test storage cap unless overridden through environment variables.
It treats `go2rtc` and `ffmpeg` as `camstationd`-managed children and only
cleans up leftovers that match this workspace's config, local RTSP input, and
recording paths.

Operational smoke checks:

```bash
curl http://127.0.0.1:18080/api/health
curl http://127.0.0.1:18080/api/events
curl http://127.0.0.1:18080/api/recorders/status
curl http://127.0.0.1:18080/api/recordings/storage
```

Real-camera verification should go beyond "build passes":

- Confirm `/live` plays camera video.
- Confirm native video controls do not appear.
- Confirm recording segments are written to temp, finalized into recordings,
  and are playable with `ffprobe`.
- Confirm live streaming stays healthy during 5-minute segment rollover tests.
- Confirm all intended recorder workers stay running.
- Confirm cleanup thresholds delete oldest ready segments without touching
  active temp segments.
- Check logs after runtime tests and separate old restart noise from new errors.

## Current Runtime Notes

- The branch is `camstation2-initial`.
- The repository has had active uncommitted work during recent sessions; inspect
  before editing and avoid overwriting it.
- The latest documented runtime monitoring page is
  `http://10.0.0.29:18080/live`.
- Use `scripts/hourly-recording-monitor.sh` and files under
  `data/runtime-logs` or `data/monitoring` only as runtime diagnostics; do not
  commit runtime output.
