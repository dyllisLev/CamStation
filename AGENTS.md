# PROJECT KNOWLEDGE BASE

**Generated:** 2026-07-03T01:42:29Z
**Commit:** 720bd2a
**Branch:** camstation2-initial

## OVERVIEW
CamStation 2.0 is a single-daemon CCTV/NVR system: Go `camstationd`, SQLite as source of truth, supervised go2rtc/ffmpeg workers, backup/cleanup jobs, and an embedded React/Vite console.

## STRUCTURE
```text
/root/camstation/
|-- cmd/camstationd/      # daemon composition root, domain route files, embedded web output
|-- internal/             # store, stream, recorder, cleanup, backup, camera packages
|-- web/                  # React/Vite console source; builds into cmd/camstationd/web
|-- docs/                 # product docs, implementation status, active specs/plans
|-- scripts/              # daemon lifecycle and operational monitoring helpers
|-- data/                 # runtime DB, recordings, temp, logs, diagnostics; never commit
`-- DESIGN.md            # current UI/design constraints for console work
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Current status | `docs/07-implementation-status.md` | Confirm shipped vs planned behavior. |
| Product/architecture | `docs/00-project-summary.md`, `docs/03-camstationd-architecture.md` | Single control-center NVR direction. |
| Real-camera safety | `docs/04-cctv2-test-plan.md` | `cctv2` is the dev target; do not disrupt legacy `cctv`. |
| Daemon startup | `cmd/camstationd/main.go` | Flags, DB migrate, recovery, workers, backup scheduler, server. |
| HTTP APIs | `cmd/camstationd/routes.go`, `cmd/camstationd/routes_*.go` | Domain route registration and public DTO hygiene. |
| Persistence | `internal/store/` | SQLite schema, migrations, rows, jobs, settings, redaction. |
| Recording | `internal/recorder/`, `cmd/camstationd/routes_recordings.go` | ffmpeg workers, segment finalization, repair, UI APIs. |
| Backup/delete balance | `internal/backup/`, `internal/cleanup/` | rclone jobs, schedule state, backed-up deletion protection. |
| Frontend API | `web/src/app/*Api.ts`, `web/src/app/*Queries.ts` | Typed fetches and TanStack Query invalidation. |
| Console pages | `web/src/pages/` | Korean operational UI outside `/live`. |
| Live UI | `web/src/components/live/LiveWorkspace.tsx` | Grid, focus view, timeline, wheel zoom/pan. |
| Runtime lifecycle | `scripts/camstationctl.sh` | Use for status/start/stop/restart/verify. |

## CODE MAP
LSP is unavailable in this workspace; codegraph is available and should be preferred over grep for source navigation.

| Symbol | Type | Location | Refs | Role |
|--------|------|----------|------|------|
| `main` | Go func | `cmd/camstationd/main.go` | root | Wires DB, go2rtc, recorder, cleanup, backup, HTTP. |
| `routes` | Go func | `cmd/camstationd/routes.go` | root | Builds `routeDeps`, registers domain APIs, serves SPA. |
| `routeDeps` | Go struct | `cmd/camstationd/routes.go` | central | Shared route dependency boundary. |
| `store.DB` | Go type | `internal/store/store.go` | central | SQLite source-of-truth wrapper. |
| `backup.Runner` | Go type | `internal/backup/runner.go` | focused | rclone job execution, retry/cancel, backed-up marking. |
| `cleanup.Cleaner` | Go type | `internal/cleanup/cleanup.go` | focused | Deletes finalized recordings, protecting unbacked rows by setting. |
| `recorder.Manager` | Go type | `internal/recorder/recorder.go` | central | ffmpeg workers and segment lifecycle. |
| `Go2RTC` | Go type | `internal/stream/go2rtc.go` | focused | Local go2rtc config/process/status adapter. |
| `App` | React component | `web/src/app/App.tsx` | root | Client routes: `/`, `/live`, recordings, backup, system, etc. |
| `api` | TS object | `web/src/app/api.ts` | root | Composes domain API modules. |
| `LiveWorkspace` | React component | `web/src/components/live/LiveWorkspace.tsx` | hotspot | Main video workspace and persisted layout state. |

## CONVENTIONS
- Run `git status --short --branch` before edits; this worktree often has other active changes.
- Database rows are canonical. Generated go2rtc config, ffmpeg commands, embedded assets, runtime logs, and recordings are derived.
- Default UI language is Korean. Keep the console dense, dark, operational, and left-sidebar based.
- `/` is the control room dashboard. `/live` is the video workspace. `/` must not become the full live grid or auto-play video.
- `streamName` is the stable internal key; `name` is the human camera label and preferred archive directory label.
- Recorder workers read local go2rtc RTSP, not raw camera URLs.
- Frontend source changes that affect served UI must be followed by `cd web && npm run build`, then `go build`.
- Larger feature work should use `docs/superpowers/specs/` then `docs/superpowers/plans/`, reconciled with `docs/07-implementation-status.md`.

## ANTI-PATTERNS
- Do not expose raw camera URLs, credentials, go2rtc APIs, localhost transport URLs, runtime file paths, or webhook secrets in public APIs/UI/events/docs.
- Do not bind go2rtc API/RTSP listeners outside `127.0.0.1` without reviewed justification.
- Do not edit or commit `.env`, `*.secret`, DB files, `data/`, logs, recordings, monitoring output, or generated `config/go2rtc.yaml`.
- Do not manage the daemon with ad hoc `kill`, `pkill`, `killall`, `nohup`, manual `setsid`, or broad PID matching; use `scripts/camstationctl.sh`.
- Do not treat `/new` as current product surface; it is legacy context.
- Do not change live video wheel zoom into `object-fit`; zoom is transform + pan + persisted `videoZoom`.
- Do not delete active temp/recording segments. Cleanup must stay limited to safe finalized rows, and unbacked protection is the default.

## COMMANDS
```bash
make test
go test ./...
cd web && npm run lint
cd web && npm run build
go build -o camstationd ./cmd/camstationd
scripts/camstationctl.sh status
scripts/camstationctl.sh restart
scripts/camstationctl.sh verify
```

## NOTES
- Runtime dev defaults from `scripts/camstationctl.sh`: `0.0.0.0:18080`, `./data/camstation.db`, recording enabled, 5-minute segments, `CAMSTATION_MAX_STORAGE_GB=0.30`.
- Use KST when explaining server/runtime timestamps to the user.
- Behavior changes need surface verification: API response, process command, generated file, DB row, UI screenshot, or log evidence.
- Web has lint/build but no dedicated component test suite; use Playwright screenshots for UI-sensitive work when possible.
