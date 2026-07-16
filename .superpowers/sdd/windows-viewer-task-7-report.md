# Task 7 Report: Transactional Windows installer and automatic updater

## Scope

- Added immutable `<version>-<digest>` release directories, a protected atomic `current.json`, a durable fsynced transaction journal, transaction-bound pointer/machine-state backups, one machine-wide owner, idempotent boot recovery, explicit rollback, and quarantine keyed by version, digest, and command generation.
- Added power-loss recovery after staging, pointer backup, activation, service start, validation, and rollback. Recovery always selects one complete old or new release; an existing immutable release ID is accepted only when every file is byte-identical.
- Added the Windows per-machine installer with default UAC elevation, `/S`, `--update`, `--rollback`, `--recover`, and `--uninstall`. It installs stable launchers under `%ProgramFiles%\CamStation Viewer`, protected state under `%ProgramData%\CamStation\Viewer`, and removes transient extracted/embedded payloads.
- Added `CamStationViewerAgent` automatic LocalSystem service registration, exact SCM recovery actions at 5/30/120 seconds with 86400-second reset and no fourth restart, configured-active-user `IgnoreNew` logon task, SYSTEM boot recovery task, ACLs, and uninstall registration. Stopping the logon task closes the bootstrap-owned Job and terminates its Electron tree.
- Added a real default `update_app` path: fixed same-origin metadata/download endpoints, exact target version/size/SHA-256, installer-owned Authenticode thumbprint, explicit bounded development-unsigned policy, durable initial-plus-three download attempts with 1/5/30-minute waits, fresh Viewer/renderer 30-second readiness gate, detached updater handoff, and planned Agent replacement.
- The updater rereads its own PE SHA-256 and exact durable Agent handoff before activation. Interrupted pre-launch work resumes once; an `installer_launched` transaction is never relaunched. Successful local validation commits the exact transaction; failure rolls back and quarantines it.
- Added a server-specific build that cross-compiles Host, bootstrap, Agent, and installer, packages Electron without Wine, creates and verifies a file-hashed ZIP payload, embeds it in the PE, and removes the transient embed file in `finally`.
- Did not start/restart CamStation services or change camera media state.

## TDD evidence

Initial transaction RED:

```text
$ go test ./internal/viewerinstall -count=1
undefined: Journal, Manager, Request, Current, Layout
FAIL
```

Transaction GREEN:

```text
$ go test ./internal/viewerinstall -count=1
ok camstation/internal/viewerinstall
```

Automatic updater RED began with missing `UpdateRunner`, `UpdateTarget`, launch/rejection states, and durable retry fields. Later RED cycles reproduced a reset retry budget after Agent restart, an unresumed waiting update, stale-green Viewer activation, immutable-directory content reuse, and missing updater self-hash/handoff verification.

Focused GREEN, including races:

```text
$ go test -race ./internal/viewerinstall ./internal/vieweragent ./cmd/camstation-viewer-installer -count=1
ok camstation/internal/viewerinstall
ok camstation/internal/vieweragent
ok camstation/cmd/camstation-viewer-installer
```

Windows registration/payload/CLI RED covered missing exact recovery actions, task XML, payload extraction, explicit modes, rollback, and unsafe update-input rejection. All focused tests now pass.

## Validation seam

The current server has no transaction-bound post-update commit-token endpoint. Task 7 therefore uses the approved first-release local seam: the updater validates the exact activated pointer, required release files, running Agent service, and registered Viewer task within the bounded installer deadline, then writes `committed` only when transaction ID, version, artifact digest, and generation match the Agent handoff.

Task 8 must publish the exact generated installer and matching size/SHA metadata. A later server protocol task is still required to add the design's server commit token and 30-second post-update remote health observation; no `update_app` download, verification, readiness, install, rollback, or quarantine step is left unimplemented while that endpoint is absent.

## Fresh verification

```text
$ go test ./...
PASS

$ go vet ./...
PASS

$ go test -race ./internal/viewerinstall ./internal/vieweragent ./cmd/camstation-viewer-installer -count=1
PASS

$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ... Host/Bootstrap/Agent/Installer
PASS; all four are PE32+ x86-64 Windows executables

$ cd viewer-app && npm test && npm run build && npm run package:win
tests 15, pass 15, fail 0; TypeScript build and Electron Windows package PASS

$ SERVER_URL=http://10.0.0.29:18080; npm run build:installer -- --server-url "$SERVER_URL" --version 2.0.0-dev.1
embedded production extractor test PASS
/root/camstation/viewer-app/dist/CamStationViewerSetup.exe

$ file viewer-app/dist/CamStationViewerSetup.exe
PE32+ executable (console) x86-64, for MS Windows

$ sha256sum viewer-app/dist/CamStationViewerSetup.exe
57ff35248f54a6fa299a5c213a60207201ef37fb922edb4e30d0a3cd24d3d27b

$ test ! -e cmd/camstation-viewer-installer/payload/release.zip
PASS

$ git diff --check
PASS
```

Generated `viewer-app/build`, `viewer-app/dist`, `viewer-app/node_modules`, and the transient embedded `release.zip` are ignored and are not part of the commit.

## Focused Task 7 review remediation

This pass reviewed and changed only the Task 7 installer/updater delta. It did not perform a whole-project review.

- SCM recovery now has an explicit fourth `none/0` action, with an exact action-to-argument mapping test. The configured monitoring-user SID is present in both the logon trigger and task principal.
- First-install recovery has a real no-release outcome: it clears `current.json`, removes the incomplete target and staging data, never starts the target, and remains idempotent across repeated recovery. Failed targets are quarantined; successful first installs still commit.
- The updater owns and atomically promotes an exact `launching_installer` handoff under the machine-wide mutex before applying. A duplicate process cannot apply concurrently, and an already committed exact transaction is a no-op.
- Transaction commit and `update.json` now reconcile across the power-loss boundary from the Agent path, installer path, and boot recovery path. Transaction ID, generation, version, and artifact digest must all match; mismatches never reconcile.
- Release metadata and installer download requests have injectable per-attempt deadlines (15 seconds and 30 minutes by default). Both ledgers persist an initial attempt plus three retries with 1/5/30-minute waits, and restart tests prove exhausted budgets are not reset.
- Uninstall disables and stops both scheduled tasks and the service before deleting registrations. Any disable/stop failure aborts before the first delete, so the outer installer cannot remove files.
- `/S` is case-insensitive, survives the unchanged elevation argument handoff, and suppresses all progress output. The default path reports phases but never waits for another confirmation after UAC.

Focused review verification:

```text
$ go test -race ./internal/viewerinstall ./internal/vieweragent ./cmd/camstation-viewer-installer -count=1
PASS

$ go test ./...
PASS

$ go vet ./...
PASS

$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ... Host/Bootstrap/Agent/Installer
PASS; all four are PE32+ x86-64 Windows executables

$ go build -o /tmp/camstationd-task7-review ./cmd/camstationd
PASS

$ cd viewer-app && npm test && npm run build && npm run package:win
tests 15, pass 15, fail 0; build/package PASS

$ npm run build:installer -- --server-url http://10.0.0.29:18080 --version 2.0.0-dev.1
embedded production extractor test PASS

$ file viewer-app/dist/CamStationViewerSetup.exe
PE32+ executable (console) x86-64, for MS Windows

$ stat -c '%s bytes' viewer-app/dist/CamStationViewerSetup.exe
383843328 bytes

$ sha256sum viewer-app/dist/CamStationViewerSetup.exe
bdafdb66dd46d2a18335e06eef8196e4d51d9079437d5f79634fa04df4b05074

$ test ! -e cmd/camstation-viewer-installer/payload/release.zip
PASS

$ git diff --check
PASS
```
