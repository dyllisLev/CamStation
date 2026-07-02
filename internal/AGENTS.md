# BACKEND PACKAGE GUIDE

## OVERVIEW
`internal/` owns the reusable backend packages behind CamStation state, streaming, recording, cleanup, and probing.

## STRUCTURE
```text
internal/
|-- store/     # SQLite schema, migrations, persistence, redaction on rows
|-- stream/    # go2rtc config/process/status adapter
|-- recorder/  # ffmpeg workers, segment lifecycle, recovery
|-- cleanup/   # safe capacity cleanup for finalized recordings
`-- camera/    # ffprobe adapter and credential redaction helpers
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Schema or API data shape | `store/store.go` | Update migrations and scan helpers together. |
| Camera list redaction | `store/store.go`, `camera/probe.go` | `includeSecrets` boundaries matter. |
| go2rtc config/status | `stream/go2rtc.go` | Keeps API/RTSP local and parses runtime consumers. |
| Recorder input/segments | `recorder/recorder.go` | Uses local go2rtc RTSP and moves temp to final. |
| Interrupted recording repair | `recorder/recovery.go` | Quarantines leftover temp files on startup. |
| Retention/capacity | `cleanup/cleanup.go` | Deletes only safe finalized segment paths. |

## CONVENTIONS
- `store.DB` is the package boundary for SQLite; keep SQL schema and Go structs aligned.
- Raw camera URLs may be loaded for internal process setup, but public responses should use redacted URLs.
- Recorder status and worker keys use `streamName`; archive/final file names should prefer the camera `name` when available.
- Segment rows move through `recording`, `finalizing`, `ready`, `failed`, or `deleted`; cleanup should only target `ready`.
- `recorder.Manager.SetAfterSegmentClosed` is the hook for cleanup after finalization.
- Prefer table-driven Go tests with `t.TempDir()` and real SQLite migrations for persistence/filesystem behavior.

## ANTI-PATTERNS
- Do not add a second credential-redaction implementation without a strong reason.
- Do not record directly from camera URLs as the default path.
- Do not delete paths unless they pass cleanup safety checks against the recordings root.
- Do not silently change segment status semantics; timeline and cleanup depend on them.

## TESTS
```bash
go test ./internal/...
go test ./internal/recorder ./internal/cleanup ./internal/stream ./internal/camera
```
