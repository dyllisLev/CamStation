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
