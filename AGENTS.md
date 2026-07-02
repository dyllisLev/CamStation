# PROJECT KNOWLEDGE BASE

**Generated:** 2026-07-01T07:32:40Z
**Commit:** 588ad86
**Branch:** camstation2-initial

## OVERVIEW
CamStation 2.0 is a single-daemon CCTV/NVR rewrite: Go `camstationd`, SQLite state, supervised go2rtc/ffmpeg workers, and an embedded React/Vite console. The product is an upgraded monitoring system, not a generic dashboard.

## STRUCTURE
```text
/root/camstation/
|-- cmd/camstationd/      # daemon entrypoint, API routes, embedded web output
|-- internal/             # store, stream, recorder, cleanup, camera packages
|-- web/                  # React/Vite source package
|-- docs/                 # product docs, status, specs, plans
|-- scripts/              # dev lifecycle and operational monitoring scripts
|-- data/                 # runtime DB, recordings, temp, logs; do not commit
`-- cmd/camstationd/web/  # generated Vite build served by camstationd
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Current status | `docs/07-implementation-status.md` | Implemented, partial, verified, and next tasks. |
| Product goal | `docs/00-project-summary.md` | Single control-center NVR direction. |
| Architecture | `docs/03-camstationd-architecture.md` | Database source of truth and worker ownership. |
| Real-camera safety | `docs/04-cctv2-test-plan.md` | `cctv2` only; do not disrupt legacy CCTV. |
| Daemon/routes | `cmd/camstationd/main.go` | Startup, API wiring, static serving, proxy guard. |
| Backend state | `internal/store/store.go` | SQLite schema and persistence contracts. |
| Streaming | `internal/stream/go2rtc.go` | Local go2rtc config/process/status adapter. |
| Recording | `internal/recorder/` | ffmpeg workers, finalization, recovery, names. |
| Cleanup | `internal/cleanup/` | Capacity cleanup for finalized recordings only. |
| Frontend API | `web/src/app/api.ts`, `web/src/app/queries.ts` | Typed fetches and TanStack Query hooks. |
| Live UI | `web/src/components/live/LiveWorkspace.tsx` | Grid, timeline, focus view, wheel zoom. |
| Console shell | `web/src/layouts/ConsoleLayout.tsx` | Left sidebar shell and `/live` bypass. |
| Active specs/plans | `docs/superpowers/specs/`, `docs/superpowers/plans/` | Required for larger feature work. |
| Runtime lifecycle | `scripts/camstationctl.sh` | Use this for status/start/stop/restart/verify. |

## CODE MAP
LSP servers were unavailable and no codegraph tool was exposed during generation; refs are static hotspots from source and ast-grep inspection.

| Symbol | Type | Location | Refs | Role |
|--------|------|----------|------|------|
| `main` | Go func | `cmd/camstationd/main.go` | hotspot | Flags, DB, recovery, workers, server lifecycle. |
| `routes` | Go func | `cmd/camstationd/main.go` | hotspot | HTTP API and SPA route registration. |
| `go2RTCProxy` | Go func | `cmd/camstationd/main.go` | hotspot | Allows only safe `/player/` go2rtc paths. |
| `store.DB` | Go type | `internal/store/store.go` | hotspot | SQLite source of truth. |
| `Go2RTC` | Go type | `internal/stream/go2rtc.go` | hotspot | go2rtc config, process, runtime status. |
| `recorder.Manager` | Go type | `internal/recorder/recorder.go` | hotspot | ffmpeg worker and segment lifecycle. |
| `cleanup.Cleaner` | Go type | `internal/cleanup/cleanup.go` | focused | Deletes old ready segments within recordings dir. |
| `api` | TS object | `web/src/app/api.ts` | hotspot | Frontend API contract. |
| `use*` query hooks | TS funcs | `web/src/app/queries.ts` | hotspot | Cache refresh and mutation invalidation. |
| `ConsoleLayout` | React component | `web/src/layouts/ConsoleLayout.tsx` | hotspot | Console navigation and `/live` shell behavior. |
| `LiveWorkspace` | React component | `web/src/components/live/LiveWorkspace.tsx` | hotspot | Main monitoring surface and persisted layout state. |
| `useMseStream` | React hook | `web/src/components/live/useMseStream.ts` | focused | Direct MSE playback path without native controls. |

## CONVENTIONS
- Before editing, run `git status --short --branch`; this worktree often has active user/agent changes.
- Database rows are canonical. Generated go2rtc config, ffmpeg commands, embedded assets, and runtime files are derived.
- Default UI language is Korean. Preserve the dense dark monitoring console and left sidebar model outside `/live`.
- `/` is the control room dashboard. `/live` is the main monitoring workspace.
- `streamName` is the stable internal key; `name` is the human-facing camera label.
- Recorder workers read `rtsp://127.0.0.1:8554/{streamName}` by default.
- Frontend source changes must be built into `cmd/camstationd/web` when the embedded daemon should serve them.
- Larger features follow `docs/superpowers/specs/` then `docs/superpowers/plans/`, then implementation and applied verification.

## ANTI-PATTERNS
- Do not expose raw go2rtc APIs or camera URLs through public UI/API responses.
- Do not bind go2rtc API/RTSP listeners outside `127.0.0.1` without an explicit reviewed reason.
- Do not edit or commit `.env`, `*.secret`, DB files, `data/`, logs, recordings, or generated `config/go2rtc.yaml`.
- Do not manage the dev daemon with ad hoc `kill`, `pkill`, `killall`, `nohup`, or manual `setsid`; use `scripts/camstationctl.sh`.
- Do not treat `/new` as a current route; it is legacy CamStation 1.0 context only.
- Do not merge the full live grid into the control room page.
- Do not replace video wheel zoom with `object-fit` changes; wheel zoom is transform + pan + persisted `videoZoom`.
- Do not delete active temp/recording segments during cleanup.

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
- `cctv` is legacy/production. `cctv2` is the development/test target, usually at `http://10.0.0.29:18080/`.
- Use KST when explaining server/runtime timestamps that matter to the user.
- Source-only success is not enough for behavior changes. Verify through the matching surface: API response, process command line, generated file, DB row, UI smoke check, or log evidence.
- Runtime diagnostics live under `data/runtime-logs` and `data/monitoring`; inspect them, do not commit them.
