# Task 6 Report: Electron Viewer and per-user Job bootstrap

## Scope

- Added the Electron 43 Windows x64 Viewer. It registers with the local Agent pipe before creating a BrowserWindow, receives the accepted generation, launch nonce, and bounded CamStation server URL, then loads the exact `/live?viewer=1` route.
- Hardened the renderer with Node integration disabled, context isolation, sandboxing, web security, packaged DevTools denial, exact-route navigation, no new windows, no permissions, and no downloads or external protocol handling.
- Added a sandbox-safe preload exposing only `reportRenderer`, `reportStream`, and `onCommand`; the pipe is versioned newline JSON capped at 64 KiB, sends a five-second local heartbeat, and accepts only `reload_live`, `resubscribe_stream`, and graceful shutdown commands with bounded operation keys.
- Added Agent-side one-generation Electron registration, runtime-only redacted stream telemetry, independent renderer progress, and one-at-a-time command delivery/result completion with a 45-second deadline. Runtime telemetry deliberately does not change the durable machine-state schema, preserving rollback readability.
- Added the Windows per-user bootstrap. It reads the current Viewer pointer, obtains one Agent launch grant, creates a kill-on-close Job, creates Electron suspended, assigns the Job before resume, requests graceful CTRL_BREAK shutdown for five seconds, and closes the Job for bounded tree cleanup. Startup work is bounded by 45 seconds.
- Did not start or restart CamStation services and did not change camera media settings.

## TDD evidence

Initial Electron RED:

```text
$ cd viewer-app && npm test
ERR_MODULE_NOT_FOUND: Cannot find module .../src/agentPipe.ts
ERR_MODULE_NOT_FOUND: Cannot find module .../src/navigation.ts
ERR_MODULE_NOT_FOUND: Cannot find module .../src/preload.ts
tests 3, pass 0, fail 3
```

Electron GREEN:

```text
$ cd viewer-app && npm test
tests 9, pass 9, fail 0
```

Initial Agent integration RED:

```text
$ go test ./internal/vieweragent -run 'TestBootstrapGrantRegistersExactlyOneElectronForGeneration|TestViewerPipeStoresBoundedStreamTelemetry|TestViewerCommandBrokerDeliversAndCompletesOneCommand' -count=1
state.Streams undefined
agent.executeViewerCommand undefined
FAIL
```

The later RED/GREEN cycles caught duplicate command delivery during long reloads, missing renderer progress separation, an unbounded Viewer command wait, missing operation keys, and durable-state forward-compatibility risk.

Initial bootstrap RED:

```text
$ go test ./internal/viewerbootstrap -count=1
undefined: BuildLaunchSpec
undefined: LaunchGrant
undefined: GenerationGate
undefined: ManagedProcess
FAIL
```

Focused bootstrap/Agent GREEN, including races:

```text
$ go test ./internal/viewerbootstrap ./internal/vieweragent -count=1
ok camstation/internal/viewerbootstrap
ok camstation/internal/vieweragent

$ go test -race ./internal/viewerbootstrap ./internal/vieweragent -count=1
ok camstation/internal/viewerbootstrap
ok camstation/internal/vieweragent
```

## Fresh final verification

```text
$ go test ./... -count=1
PASS (camstationd 57.789s, store 38.900s, vieweragent 5.357s, viewerbootstrap 0.012s)

$ go vet ./...
PASS

$ go build -o /tmp/camstationd-task6-final ./cmd/camstationd
PASS

$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-bootstrap-task6-final.exe ./cmd/camstation-viewer-bootstrap
$ file /tmp/camstation-viewer-bootstrap-task6-final.exe
PE32+ executable (console) x86-64, for MS Windows

$ cd viewer-app && npm test && npm run build && npm run package:win
tests 9, pass 9, fail 0
Wrote new app to: dist/CamStationViewer-win32-x64

$ file viewer-app/dist/CamStationViewer-win32-x64/CamStationViewer.exe
PE32+ executable (GUI) x86-64, for MS Windows

$ git diff --check
PASS
```

Generated `viewer-app/build`, `viewer-app/dist`, and `viewer-app/node_modules` are ignored and are not part of the commit.

## Focused Task 6 review fixes

The Task 6-only review found and fixed the following lifecycle and packaging gaps without changing service or camera runtime state:

- Unexpected Agent pipe closure now remains observable even when it races lifecycle subscription, exits Electron nonzero exactly once, and does not misclassify explicit shutdown. Registration/request timeouts, write/encoding failures, and pipe closure remove all pending requests.
- `restart_viewer` now performs the real Agent-controlled transition: authorize a command-specific next generation, request and acknowledge graceful Viewer shutdown, clear the old identity synchronously, permit exactly one bootstrap grant, and complete only after that exact generation reports both Viewer `running` and renderer `ready`. Failure is bounded and persists `recovery_failed` without an automatic retry loop.
- Bootstrap generations must increase strictly. A running Viewer cannot be replaced without explicit restart authorization, an old identity cannot claim the next generation, and a grant cannot be replaced or reused.
- The 45-second startup/recovery budget covers current-release lookup, grant/registration, suspended launch, renderer readiness, and Job cleanup observation; cleanup no longer adds another timeout after the total deadline.
- Renderer state is a fixed enum (`ready`, `not_ready`, `unresponsive`, `failed`), resolved Viewer paths cannot escape the install root through symlinks or Windows reparse points, and distinct forced command IDs remain independently actionable while duplicate IDs remain idempotent.
- Windows packaging now has an executable policy and post-package assertion. The ASAR contains only `build/*` and `package.json`; source, tests, scripts, `node_modules`, TypeScript configuration, and lock files are excluded.

Focused RED/GREEN evidence included an unexpected-close-before-subscription regression, pending-request leak checks, old-generation identity rejection, a never-ready process whose Job wait would previously exceed the total deadline, resolved symlink escape rejection, and package allow/exclude policy checks.

Fresh focused-review verification:

```text
$ go test ./... -count=1
PASS (including vieweragent and viewerbootstrap)

$ go test -race ./internal/viewerbootstrap ./internal/vieweragent -count=1
PASS

$ go vet ./...
PASS

$ go build -o /tmp/camstationd-task6-review-final ./cmd/camstationd
$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-bootstrap-task6-review-final.exe ./cmd/camstation-viewer-bootstrap
PASS; bootstrap is a PE32+ Windows x86-64 executable

$ cd viewer-app && npm test && npm run build && npm run package:win
tests 13, pass 13, fail 0; build and packaging PASS
ASAR: /build/* and /package.json only

$ git diff --check
PASS
```
