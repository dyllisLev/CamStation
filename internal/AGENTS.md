# BACKEND PACKAGE GUIDE

## OVERVIEW
`internal/` owns the reusable backend packages behind CamStation state, go2rtc, ffmpeg recording, cleanup, backup, camera probing, and profile matching.

## STRUCTURE
```text
internal/
|-- store/          # SQLite schema, migrations, persistence, jobs, redaction
|-- backup/         # rclone job runner, validation, cancel/retry, scheduling logic
|-- recorder/       # ffmpeg workers, segment lifecycle, interrupted recovery
|-- cleanup/        # safe capacity cleanup for finalized recordings
|-- stream/         # go2rtc config/process/status adapter
|-- camera/         # ffprobe adapter and credential redaction helpers
`-- cameraprofile/  # profile parsing and camera-type matching
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Schema or data shape | `store/` | Update schema, models, scans, and tests together. |
| Job/settings state | `store/jobs*.go`, `store/settings*.go` | Public settings mask secrets; delivery helpers may load private values. |
| Backup execution | `backup/runner.go`, `backup/request.go`, `backup/scheduler.go` | rclone copy, target/prefix validation, due calculation. |
| Backup deletion safety | `backup/runner.go`, `cleanup/cleanup.go` | Successful backup marks ready segments `backed_up`; cleanup can require it. |
| Recorder workers | `recorder/recorder.go` | Local go2rtc RTSP input, temp/final paths, segment close hook. |
| Recovery/quarantine | `recorder/recovery.go` | Startup repair of interrupted temp files. |
| Stream runtime | `stream/go2rtc.go` | Local listeners, generated config, runtime consumers. |
| Camera probing | `camera/probe.go`, `cameraprofile/profile.go` | Never leak credentials in public output. |

## CONVENTIONS
- `store.DB` is the persistence boundary; tests should use real SQLite migrations and `t.TempDir()`.
- Raw camera URLs are for internal process setup only. Public responses should expose redacted or derived values.
- Segment states are meaningful: `recording`, `finalizing`, `ready`, `failed`, `deleted`; backup state is separate.
- Cleanup should only consider finalized safe paths and must respect unbacked protection when enabled.
- Async jobs should use fakes and explicit wait helpers in tests, not blind sleeps.
- Prefer small domain files over rebuilding monolithic package files.

## ANTI-PATTERNS
- Do not add another credential-redaction path without tests proving no leakage.
- Do not record directly from raw camera URLs as the default path.
- Do not delete paths outside the recordings root or active temp/finalizing files.
- Do not silently change segment or job state semantics; UI and cleanup depend on them.
- Do not put HTTP handler logic into `internal/`; keep it transport-agnostic.

## TESTS
```bash
go test ./internal/...
go test ./internal/backup ./internal/cleanup ./internal/recorder ./internal/store
```
