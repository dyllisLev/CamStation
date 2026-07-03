# STORE GUIDE

## OVERVIEW
`internal/store/` is the SQLite source-of-truth layer for cameras, layouts, recordings, backup/jobs, settings, incidents, viewers, diagnostics, and events.

## STRUCTURE
```text
internal/store/
|-- schema.go                 # migrations and compatibility columns
|-- models.go                 # public/domain structs
|-- store.go                  # DB wrapper and scanner interface
|-- jobs*.go                  # queued/running/succeeded/failed/cancelled state machine
|-- settings*.go              # recording/backup/alert settings, validation, secret masking
|-- recordings*.go            # segment open/close/list/delete/backup markers
|-- camera*.go                # camera rows, streams, redaction boundaries
`-- *_test.go                 # real SQLite migrations, validation, redaction, repair coverage
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add column/table | `schema.go`, relevant scan file | Include idempotent migration for existing DBs. |
| Change API shape | `models.go`, domain file, route DTO | Keep DB structs and public DTOs separate when redaction matters. |
| Recording segment behavior | `recordings.go`, `recording_segments_ops.go` | Status and backup state are separate contracts. |
| Job lifecycle | `jobs.go`, `jobs_transitions.go`, `jobs_scan.go` | Single-flight and terminal states matter. |
| Secret handling | `settings_jobs.go`, `settings_validation.go`, `jobs_redaction.go` | Public reads mask; delivery helpers may return private values. |
| Query/filter work | `events_query.go`, incidents files | Preserve limit bounds and stable ordering. |

## CONVENTIONS
- Run migrations in `Migrate`; make them safe against old runtime DBs.
- Add scan fields in the same order as SELECT columns.
- Use `sql.Null*` for nullable DB fields and normalize defaults at scan/load boundaries.
- Tests should open a temp DB, call `Migrate`, and assert persisted behavior.
- Public JSON must not leak camera credentials, webhook URLs, auth headers, runtime paths, or localhost transport URLs.
- Legacy settings JSON must be normalized so old rows get safe defaults.

## ANTI-PATTERNS
- Do not store derived runtime config as canonical state.
- Do not weaken redaction tests to pass a new DTO shape.
- Do not mark a job active without a terminal transition path.
- Do not reset backup markers when listing or reading rows; only segment close/reopen and backup success should change them.
- Do not bypass schema migrations by assuming a fresh DB.

## TESTS
```bash
go test ./internal/store
```
