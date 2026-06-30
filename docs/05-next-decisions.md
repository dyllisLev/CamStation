# Next Decisions

These decisions should be made before writing the first real implementation plan.

## 1. Runtime

Recommended: Go single-binary daemon with embedded web UI.

Alternatives:

- Go daemon plus React UI served from disk
- Transitional FastAPI/React prototype

The user's stated goal favors Go single-binary.

## 2. Repository Shape

Decide whether to create:

```text
cmd/camstationd/
internal/
web/
docs/
```

or a simpler first structure:

```text
server/
web/
docs/
```

## 3. First Milestone

Recommended first milestone:

```text
camstationd starts locally, serves web UI, runs SQLite migrations, stores settings, and shows unified logs in fake mode.
```

This proves the single-program foundation before touching real cameras.

## 4. cctv2 Deployment Method

Options:

- scp binary manually during early testing
- build script that copies to `cctv2:/opt/camstation2-test`
- later systemd test service

Recommendation: manual/scp first, systemd test service after the daemon is stable.

## 5. Existing Data Import

Decide what to import from current CamStation:

- camera list
- recording metadata
- layout profiles
- viewer clients
- settings

Recommendation: import cameras first, recordings later.

## 6. Admin/Auth Scope

Initial LAN-only prototype can start without complex auth, but the design should not assume permanent unauthenticated admin access.

Minimum future shape:

- admin session/token
- viewer mode separated from admin APIs
- secret redaction everywhere

