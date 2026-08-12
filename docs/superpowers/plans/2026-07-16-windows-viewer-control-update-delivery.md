# Windows Viewer Control, Update, and Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the unattended Windows Viewer stack and publish its verified one-click installer through the CamStation settings page.

**Architecture:** CamStation serves immutable release metadata, the installer, durable commands, SSE/long-poll control, and independent Agent/Viewer/stream health. A Go Windows service host runs the versioned Agent outside Electron; a Go per-user bootstrap owns the Electron Job Object; Electron renders `/live?viewer=1`; and a Go installer/updater installs versioned releases, registers the service and logon task, atomically switches `current.json`, and rolls back an unvalidated update.

**Tech Stack:** Go 1.25, SQLite, React 19/Vite 8/TypeScript 6, Electron 43.1.1, `@electron/packager` 20.0.2, Node 22 test runner, Windows Service Control Manager, Task Scheduler, named pipes, and Windows Job Objects.

## Global Constraints

- Target only CamStation 2.0 `/live?viewer=1`; never `/new` or `/viewer`.
- The Windows Agent service owns server heartbeat, commands, update, and Viewer supervision; renderer JavaScript never owns client health.
- One elevated installer run registers the service, Viewer logon task, recovery task, uninstall entry, and starts available components.
- The release is server-specific: `defaults.json` contains the credential-free `http:` or `https:` CamStation base URL used to build it.
- Do not add login, pairing, mTLS, signed commands, remote shell, or arbitrary PC administration.
- Verify installer size, SHA-256, release identity, and Authenticode when a production certificate is configured; an unsigned development release must be labeled `developmentUnsigned: true`.
- Bound SSE and long-poll reads to 25 seconds, stream recovery to 30 seconds, Viewer recovery to 45 seconds, automatic Viewer restarts to once per ten minutes and three per hour, and Agent SCM recovery to three attempts.
- Keep go2rtc API and RTSP on loopback. The browser receives public stream names only.
- Do not commit generated installers, Electron packages, runtime state, DBs, logs, camera data, credentials, or generated go2rtc config.
- Frontend source changes require `cd web && npm run build`, followed by `go build`.
- Daemon lifecycle changes use `scripts/camstationctl.sh`; never use broad process-kill commands.

---

### Task 1: Immutable Viewer release catalog and download API

**Files:**
- Create: `internal/viewerrelease/release.go`
- Create: `internal/viewerrelease/release_test.go`
- Create: `cmd/camstationd/routes_viewer_releases.go`
- Create: `cmd/camstationd/routes_viewer_releases_test.go`
- Modify: `cmd/camstationd/main.go`
- Modify: `cmd/camstationd/routes.go`

**Interfaces:**
- Produces: `viewerrelease.Load(dir string) (viewerrelease.Release, error)` and `Release.OpenVerified() (*os.File, error)`.
- Produces: `GET /api/viewers/app/version` and `GET /api/viewers/app/download`.
- Release directory: `${CAMSTATION_VIEWER_RELEASES_DIR:-./data/viewer-releases}/current/` containing `release.json` and the manifest filename.

- [ ] **Step 1: Write the failing release loader tests**

```go
func TestLoadRejectsMissingAndMismatchedArtifact(t *testing.T) {
    dir := t.TempDir()
    if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrUnavailable) {
        t.Fatalf("missing release error = %v", err)
    }
    writeReleaseFixture(t, dir, []byte("installer"), "00")
    if _, err := viewerrelease.Load(dir); !errors.Is(err, viewerrelease.ErrInvalid) {
        t.Fatalf("bad digest error = %v", err)
    }
}
```

- [ ] **Step 2: Run the loader test and verify RED**

Run: `go test ./internal/viewerrelease -run TestLoadRejectsMissingAndMismatchedArtifact -count=1`

Expected: FAIL because `internal/viewerrelease` does not exist.

- [ ] **Step 3: Implement strict manifest loading and verified open**

```go
type Release struct {
    Version             string    `json:"version"`
    Filename            string    `json:"filename"`
    SizeBytes           int64     `json:"sizeBytes"`
    SHA256              string    `json:"sha256"`
    PublishedAt         time.Time `json:"publishedAt"`
    DevelopmentUnsigned bool      `json:"developmentUnsigned"`
    dir                 string
}

func (r Release) DownloadURL() string { return "/api/viewers/app/download" }
```

Reject absolute filenames, separators, non-`.exe` filenames, non-positive sizes, non-64-character lowercase hex digests, missing files, size mismatches, and digest mismatches. Do not include the directory in public errors.

- [ ] **Step 4: Run release package tests and verify GREEN**

Run: `go test ./internal/viewerrelease -count=1`

Expected: PASS.

- [ ] **Step 5: Write failing route tests for metadata and attachment delivery**

```go
func TestViewerReleaseRoutesServeVerifiedInstaller(t *testing.T) {
    server := newTestRouteServer(t)
    publishViewerFixture(t, filepath.Join(filepath.Dir(server.recordingsDir), "viewer-releases", "current"))
    status, metadata := requestJSON(t, server.handler, http.MethodGet, "/api/viewers/app/version", "")
    if status != http.StatusOK || metadata["downloadUrl"] != "/api/viewers/app/download" {
        t.Fatalf("metadata = %d %#v", status, metadata)
    }
    response := performRequest(t, server.handler, http.MethodGet, "/api/viewers/app/download", "")
    if response.Header().Get("Content-Disposition") != `attachment; filename="CamStationViewerSetup.exe"` {
        t.Fatalf("content disposition = %q", response.Header().Get("Content-Disposition"))
    }
}
```

- [ ] **Step 6: Run route tests and verify RED**

Run: `go test ./cmd/camstationd -run ViewerRelease -count=1`

Expected: FAIL with route returning the SPA or 404.

- [ ] **Step 7: Register release routes and runtime directory**

Add `-viewer-releases-dir` / `CAMSTATION_VIEWER_RELEASES_DIR`, defaulting to `./data/viewer-releases`. Return `503` for unavailable/invalid releases, `Cache-Control: no-store` for metadata, `application/vnd.microsoft.portable-executable`, fixed content length, `X-Content-Type-Options: nosniff`, and an attachment filename for downloads.

- [ ] **Step 8: Run focused and full Go tests**

Run: `go test ./internal/viewerrelease ./cmd/camstationd -run 'ViewerRelease|ViewersAPI' -count=1`

Expected: PASS.

- [ ] **Step 9: Commit Task 1**

```bash
git add internal/viewerrelease cmd/camstationd/main.go cmd/camstationd/routes.go cmd/camstationd/routes_viewer_releases.go cmd/camstationd/routes_viewer_releases_test.go
git commit -m "feat(viewers): serve verified Windows release"
```

### Task 2: Settings-page installer download surface

**Files:**
- Create: `web/src/app/viewerReleaseApi.ts`
- Create: `web/src/app/viewerReleaseQueries.ts`
- Create: `web/src/pages/settings/ViewerClientDownloadCard.tsx`
- Create: `web/src/pages/settings/viewerReleaseModel.ts`
- Create: `web/tests/viewerReleaseModel.test.ts`
- Modify: `web/src/app/api.ts`
- Modify: `web/src/app/queries.ts`
- Modify: `web/src/pages/settings/SettingsConsole.tsx`

**Interfaces:**
- Produces: `ViewerRelease` with `version`, `filename`, `sizeBytes`, `sha256`, `publishedAt`, `developmentUnsigned`, and `downloadUrl`.
- Produces: `useViewerRelease()` and a Korean-first settings card.

- [ ] **Step 1: Write the failing safe-download model test**

```ts
test("accepts only the fixed viewer installer download route", () => {
  assert.equal(viewerDownloadHref("/api/viewers/app/download"), "/api/viewers/app/download");
  assert.equal(viewerDownloadHref("https://evil.example/setup.exe"), null);
  assert.equal(formatReleaseSize(1048576), "1.0 MB");
});
```

- [ ] **Step 2: Run the model test and verify RED**

Run: `cd web && npm test -- viewerReleaseModel.test.ts`

Expected: FAIL because `viewerReleaseModel.ts` does not exist.

- [ ] **Step 3: Implement the release API, query, and model**

```ts
export type ViewerRelease = {
  readonly version: string;
  readonly filename: string;
  readonly sizeBytes: number;
  readonly sha256: string;
  readonly publishedAt: string;
  readonly developmentUnsigned: boolean;
  readonly downloadUrl: "/api/viewers/app/download";
};
```

Use the existing `request` helper and `withAppBase`. Do not expose a server filesystem path.

- [ ] **Step 4: Run the model test and verify GREEN**

Run: `cd web && npm test -- viewerReleaseModel.test.ts`

Expected: PASS.

- [ ] **Step 5: Add the settings card**

The card title is `Windows 모니터링 클라이언트`; it shows version, file size, first 12 digest characters, `개발용 미서명 빌드` when applicable, and a `Windows 설치 파일 다운로드` anchor with the `download` attribute. An unavailable release shows `설치 파일이 아직 게시되지 않았습니다.` and no dead link.

- [ ] **Step 6: Verify frontend**

Run: `cd web && npm test && npm run lint && npm run build`

Expected: all tests pass, lint exits 0, and Vite writes embedded assets.

- [ ] **Step 7: Commit Task 2**

```bash
git add web/src web/tests/viewerReleaseModel.test.ts cmd/camstationd/web
git commit -m "feat(settings): add Windows viewer download"
```

### Task 3: Durable control and independent health contract

**Files:**
- Modify: `internal/store/viewer_system_schema.go`
- Modify: `internal/store/viewers.go`
- Modify: `internal/store/viewer_commands.go`
- Create: `internal/store/viewer_control_test.go`
- Modify: `cmd/camstationd/routes_viewers.go`
- Create: `cmd/camstationd/routes_viewer_control_test.go`
- Modify: `web/src/app/streamsViewersSystemApi.ts`
- Modify: `web/src/pages/viewers/ViewerCommandPanel.tsx`
- Modify: `web/src/pages/viewers/ViewerRegistryPanel.tsx`

**Interfaces:**
- Heartbeat request includes independent `agent`, `control`, `viewer`, `renderer`, `update`, and stream timestamps.
- Heartbeat response returns `{ viewer, desiredRelease, commitToken? }`.
- Produces `GET /api/viewers/{id}/control` (SSE) and `GET /api/viewers/{id}/commands/next?wait=25` (long poll).
- Commands use states `pending`, `delivered`, `acknowledged`, `running`, `succeeded`, `failed`, `rejected`, `expired`, `cancelled`, `deleted`.

- [ ] **Step 1: Write failing store migration and idempotency tests**

```go
func TestViewerCommandResultIsIdempotent(t *testing.T) {
    db := openMigratedTestDB(t)
    seedViewer(t, db, "viewer-1")
    command := createCommand(t, db, "viewer-1", "restart_viewer")
    first, err := db.ApplyViewerCommandResult(t.Context(), "viewer-1", command.ID, store.ViewerCommandResult{State: "succeeded", OperationKey: "op-1"})
    if err != nil { t.Fatal(err) }
    second, err := db.ApplyViewerCommandResult(t.Context(), "viewer-1", command.ID, store.ViewerCommandResult{State: "succeeded", OperationKey: "op-1"})
    if err != nil || second.UpdatedAt != first.UpdatedAt { t.Fatalf("duplicate changed result") }
}
```

- [ ] **Step 2: Run store tests and verify RED**

Run: `go test ./internal/store -run ViewerCommandResultIsIdempotent -count=1`

Expected: FAIL because the result API and migrated columns do not exist.

- [ ] **Step 3: Implement additive schema migration and durable transitions**

Use `addColumnIfMissing` for existing installations. Persist payload hash, TTL, operation key, generation, delivered/running/result timestamps, control state, Viewer state, renderer state, last control success, and update state. Admin list reads must not mark commands delivered; only SSE/next delivery may do that.

- [ ] **Step 4: Run store tests and verify GREEN**

Run: `go test ./internal/store -run Viewer -count=1`

Expected: PASS.

- [ ] **Step 5: Write failing SSE and blackholed-long-poll route tests**

Tests must prove keepalive arrives within ten seconds, delivery marks one command once, a 25-second empty poll returns `204`, admin listing does not deliver, duplicate results do not repeat side effects, and heartbeat can be `online` while control is `control_degraded`.

- [ ] **Step 6: Implement SSE and long-poll handlers with standard-library timers**

Poll the SQLite queue at one-second intervals, emit `event: command` JSON, emit `: keepalive` no less often than ten seconds, stop on request cancellation, and use no background goroutine after the handler returns.

- [ ] **Step 7: Run backend and frontend control verification**

Run: `go test ./internal/store ./cmd/camstationd -run 'Viewer|Control' -count=1 && cd web && npm run lint && npm run build`

Expected: PASS.

- [ ] **Step 8: Commit Task 3**

```bash
git add internal/store cmd/camstationd/routes_viewers.go cmd/camstationd/routes_viewer_control_test.go web/src
git commit -m "feat(viewers): add durable Agent control plane"
```

### Task 4: Finite WebRTC-to-MSE playback and Viewer telemetry bridge

**Files:**
- Create: `web/src/components/live/playbackRecovery.ts`
- Create: `web/src/components/live/useWebRtcMseStream.ts`
- Create: `web/src/components/live/viewerBridge.ts`
- Create: `web/tests/playbackRecovery.test.ts`
- Create: `web/tests/viewerBridge.test.ts`
- Modify: `web/src/components/live/LiveWorkspace.tsx`
- Modify: `web/src/components/live/useMseStream.ts`
- Modify: `cmd/camstationd/spa_proxy.go`
- Create: `cmd/camstationd/spa_proxy_viewer_test.go`

**Interfaces:**
- Produces `useWebRtcMseStream(streamNames)` returning the existing video ref plus `transport`, `phase`, `lastProgressAt`, and finite episode counts.
- Produces `window.camstationViewer.reportStream(telemetry)` and `onCommand(handler)` only when exposed by the preload.
- Accepts only registered public stream names at `/player/api/ws`.

- [ ] **Step 1: Write failing finite recovery tests**

```ts
test("one episode stops after WebRTC, reconnect, MSE primary, fallback, and resubscribe", () => {
  const episode = new PlaybackRecovery(["yard-live", "yard-focus"], 0);
  assert.deepEqual(episode.nextFailure(1_000), { transport: "webrtc", streamName: "yard-live", attempt: 2 });
  assert.deepEqual(episode.nextFailure(5_000), { transport: "mse", streamName: "yard-live", attempt: 3 });
  assert.deepEqual(episode.nextFailure(10_000), { transport: "mse", streamName: "yard-focus", attempt: 4 });
  assert.deepEqual(episode.nextFailure(20_000), { action: "resubscribe", attempt: 5 });
  assert.deepEqual(episode.nextFailure(30_001), { action: "cooldown", until: 330_001 });
});
```

- [ ] **Step 2: Run playback tests and verify RED**

Run: `cd web && npm test -- playbackRecovery.test.ts viewerBridge.test.ts`

Expected: FAIL because the model and bridge do not exist.

- [ ] **Step 3: Implement the pure recovery model and bridge shaping**

No timer recursively resets its own budget. Stable media progress for five minutes creates a fresh episode. Telemetry never contains URLs or go2rtc details.

- [ ] **Step 4: Run model tests and verify GREEN**

Run: `cd web && npm test -- playbackRecovery.test.ts viewerBridge.test.ts`

Expected: PASS.

- [ ] **Step 5: Implement WebRTC offer/candidate signaling and MSE fallback**

Use the existing same-origin `/player/api/ws?src=` WebSocket. WebRTC sends `webrtc/offer` and `webrtc/candidate`; MSE uses the current codec negotiation. Each setup attempt has a five-second deadline, media stalls at ten seconds, and the episode stops by 30 seconds.

- [ ] **Step 6: Add isolated resubscribe and telemetry reporting**

Each tile has a generation keyed by `streamName`. A `resubscribe_stream` command increments only the matching generation. Report binary receive and `currentTime` progress separately.

- [ ] **Step 7: Verify proxy allowlist and frontend**

Run: `go test ./cmd/camstationd -run Go2RTCProxy -count=1 && cd web && npm test && npm run lint && npm run build`

Expected: PASS.

- [ ] **Step 8: Commit Task 4**

```bash
git add web/src/components/live web/tests cmd/camstationd/spa_proxy.go cmd/camstationd/spa_proxy_viewer_test.go cmd/camstationd/web
git commit -m "feat(live): add finite WebRTC playback recovery"
```

### Task 5: Windows Agent core and service host

**Files:**
- Create: `internal/vieweragent/config.go`
- Create: `internal/vieweragent/state.go`
- Create: `internal/vieweragent/control.go`
- Create: `internal/vieweragent/agent.go`
- Create: `internal/vieweragent/*_test.go`
- Create: `cmd/camstation-viewer-agent/main.go`
- Create: `cmd/camstation-viewer-host/main_windows.go`
- Create: `cmd/camstation-viewer-host/main_other.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Agent command: `run --config <path>` as the versioned worker process.
- Stable SCM host: `camstation-viewer-host.exe` reads `current.json`, launches
  that release's Agent, forwards stop/shutdown, and never loads Electron.
- Install support: `configure --server-url <url> --display-name <name> --install-dir <dir>`.
- Machine files: `%ProgramData%\CamStation\Viewer\config.json`, `state.json`, `commands.json`, `update.json`.
- Named pipe: `\\.\pipe\CamStationViewerAgent`, newline-delimited versioned JSON capped at 64 KiB.

- [ ] **Step 1: Write failing config, state, retry, and command-ledger tests**

Tests cover URL rejection, stable client ID, atomic state replacement, 25-second read deadline, SSE-to-long-poll fallback, durable `running` intent before execution, restart generation reconciliation, update quarantine, and bounded restart budgets.

- [ ] **Step 2: Run Agent tests and verify RED**

Run: `go test ./internal/vieweragent -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement the cross-platform Agent core**

Use `net/http`, `encoding/json`, filesystem atomic rename, and injected clocks/executors. Add no retry framework. The control loop is one state machine with explicit deadlines and the delays `1s, 2s, 5s, 10s, 30s`, then one five-minute probe.

- [ ] **Step 4: Run Agent tests and verify GREEN**

Run: `go test ./internal/vieweragent -count=1`

Expected: PASS.

- [ ] **Step 5: Add Windows service and named-pipe adapters**

Use `golang.org/x/sys/windows/svc` in the stable host and
`github.com/Microsoft/go-winio v0.6.2` in the Agent. The host reports SCM state,
forwards stop/shutdown to exactly one versioned Agent, and uses a bounded child
restart budget. The Agent sends server heartbeat without Viewer IPC and does not
restart Viewer for server/network loss.

- [ ] **Step 6: Cross-compile and inspect the Agent**

Run: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-agent.exe ./cmd/camstation-viewer-agent && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-host.exe ./cmd/camstation-viewer-host && file /tmp/camstation-viewer-agent.exe /tmp/camstation-viewer-host.exe`

Expected: PE32+ executable for MS Windows x86-64.

- [ ] **Step 7: Commit Task 5**

```bash
git add internal/vieweragent cmd/camstation-viewer-agent cmd/camstation-viewer-host go.mod go.sum
git commit -m "feat(viewer-agent): add Windows control service"
```

### Task 6: Electron Viewer and per-user Job bootstrap

**Files:**
- Create: `viewer-app/package.json`
- Create: `viewer-app/package-lock.json`
- Create: `viewer-app/tsconfig.json`
- Create: `viewer-app/src/main.ts`
- Create: `viewer-app/src/preload.ts`
- Create: `viewer-app/src/navigation.ts`
- Create: `viewer-app/src/agentPipe.ts`
- Create: `viewer-app/tests/*.test.ts`
- Create: `cmd/camstation-viewer-bootstrap/main_windows.go`
- Create: `cmd/camstation-viewer-bootstrap/main_other.go`
- Create: `internal/viewerbootstrap/job_windows.go`
- Create: `internal/viewerbootstrap/job_other.go`
- Create: `internal/viewerbootstrap/bootstrap_test.go`

**Interfaces:**
- Electron main receives server URL and launch nonce from the Agent pipe and opens `/live?viewer=1`.
- Preload exposes only `reportRenderer`, `reportStream`, and `onCommand`.
- Bootstrap command: `--install-dir <dir>`, obtains the accepted launch generation, creates a kill-on-close Job, launches Electron suspended, assigns it, and resumes it.

- [ ] **Step 1: Write failing Electron security/navigation/IPC tests**

```ts
test("viewer URL always targets the 2.0 live route", () => {
  assert.equal(viewerURL("http://10.0.0.5:18080"), "http://10.0.0.5:18080/live?viewer=1");
  assert.throws(() => viewerURL("file:///tmp/index.html"));
});
```

Tests also assert `nodeIntegration: false`, `contextIsolation: true`, `sandbox: true`, `webSecurity: true`, navigation denial, permission denial, and 64 KiB pipe message cap.

- [ ] **Step 2: Run Electron tests and verify RED**

Run: `cd viewer-app && npm test`

Expected: FAIL because the source modules do not exist.

- [ ] **Step 3: Implement the hardened Electron main/preload**

The main process connects the Agent pipe before creating the BrowserWindow, sends a five-second local heartbeat, forwards renderer telemetry, and handles only `reload_live`, `resubscribe_stream`, and graceful shutdown. It never downloads or installs updates.

- [ ] **Step 4: Run Electron tests and TypeScript build**

Run: `cd viewer-app && npm test && npm run build`

Expected: PASS.

- [ ] **Step 5: Write failing bootstrap policy tests**

Test the launch argument builder, one-generation policy, five-second graceful deadline, and 45-second total deadline using a fake process adapter.

- [ ] **Step 6: Implement the Windows Job bootstrap and cross-compile**

Run: `go test ./internal/viewerbootstrap -count=1 && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-bootstrap.exe ./cmd/camstation-viewer-bootstrap`

Expected: tests pass and a PE32+ executable is produced.

- [ ] **Step 7: Package the unpacked Windows Electron app**

Run: `cd viewer-app && npm run package:win`

Expected: `viewer-app/dist/CamStationViewer-win32-x64/CamStationViewer.exe` exists without requiring Wine.

- [ ] **Step 8: Commit Task 6**

```bash
git add viewer-app cmd/camstation-viewer-bootstrap internal/viewerbootstrap
git commit -m "feat(viewer-app): add supervised Electron monitor"
```

### Task 7: Transactional installer, updater, service, and task registration

**Files:**
- Create: `internal/viewerinstall/layout.go`
- Create: `internal/viewerinstall/transaction.go`
- Create: `internal/viewerinstall/transaction_test.go`
- Create: `internal/viewerinstall/windows.go`
- Create: `internal/viewerinstall/other.go`
- Create: `cmd/camstation-viewer-installer/main.go`
- Create: `cmd/camstation-viewer-installer/elevate_windows.go`
- Create: `cmd/camstation-viewer-installer/elevate_other.go`
- Create: `cmd/camstation-viewer-installer/payload/.gitkeep`
- Create: `viewer-app/scripts/build-installer.mjs`
- Modify: `.gitignore`

**Interfaces:**
- Installer commands: default interactive/UAC install, `/S` silent install, `--update`, `--rollback <transaction-id>`, `--uninstall`.
- Installs under `%ProgramFiles%\CamStation Viewer`; machine state under `%ProgramData%\CamStation\Viewer`.
- Stable service/task launchers read `current.json`; releases are immutable `<version>-<digest>` directories.

- [ ] **Step 1: Write failing power-loss transaction tests**

Use a temp filesystem and inject failure after staging, pointer backup, activation, service start, validation, and rollback. Reopening the journal must produce exactly the new committed release or the prior release, never a mixed directory or missing pointer.

- [ ] **Step 2: Run transaction tests and verify RED**

Run: `go test ./internal/viewerinstall -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement versioned layout, journal, mutex ownership, and rollback**

Journal writes use temp-file sync and atomic rename. Only one machine-wide update mutex owns the transaction. A failed target digest is quarantined until the server publishes a new digest/generation.

- [ ] **Step 4: Run transaction tests and verify GREEN**

Run: `go test ./internal/viewerinstall -count=1`

Expected: PASS.

- [ ] **Step 5: Implement Windows registration**

Register `CamStationViewerAgent` as automatic service, SCM recovery delays 5/30/120 seconds with reset after 86400 seconds, a configured-user logon task with `IgnoreNew`, and a boot recovery task. Uninstall removes all three registrations and stops the Job-owned Viewer tree.

- [ ] **Step 6: Build one server-specific installer**

`viewer-app/scripts/build-installer.mjs` writes a bounded `defaults.json`, builds
the stable service host plus the versioned Agent, Viewer bootstrap, and Electron
package, zips the release payload, copies it into the embed directory, builds
`CamStationViewerSetup.exe`, then removes the transient embedded payload. The
installer self-elevates with `runas` and uses the active console user SID and
computer name as defaults.

- [ ] **Step 7: Cross-build and inspect the installer**

Run: `SERVER_URL="${CAMSTATION_VIEWER_SERVER_URL:-http://$(hostname -I | awk '{print $1}'):18080}"; cd viewer-app && npm run build:installer -- --server-url "$SERVER_URL" --version 2.0.0-dev.1`

Expected: `viewer-app/dist/CamStationViewerSetup.exe` is PE32+, contains the release payload, and no payload file remains tracked.

- [ ] **Step 8: Commit Task 7**

```bash
git add internal/viewerinstall cmd/camstation-viewer-installer viewer-app/scripts .gitignore
git commit -m "build(viewer-app): add transactional Windows installer"
```

### Task 8: Publish, apply, and verify the downloadable release

**Files:**
- Create: `scripts/publish-viewer-release.sh`
- Create: `scripts/publish-viewer-release_test.sh`
- Modify: `docs/07-implementation-status.md`

**Interfaces:**
- Publisher: `scripts/publish-viewer-release.sh --installer <path> --version <version> --release-dir <dir> [--development-unsigned]`.
- Publishes by staging a complete `current.new` directory, fsyncing files where supported, and atomically renaming to `current` while retaining `previous`.

- [ ] **Step 1: Write the failing publisher shell test**

The test verifies missing input fails, metadata size/hash match the installer, a failed publish preserves the prior `current`, filenames cannot traverse, and `--dry-run` changes nothing.

- [ ] **Step 2: Run publisher test and verify RED**

Run: `bash scripts/publish-viewer-release_test.sh`

Expected: FAIL because the publisher does not exist.

- [ ] **Step 3: Implement the minimal atomic publisher**

Use `sha256sum`, `stat`, `mktemp`, and `mv`; do not introduce a deployment framework. The manifest filename is always `CamStationViewerSetup.exe`.

- [ ] **Step 4: Run publisher and full static verification**

Run: `bash scripts/publish-viewer-release_test.sh && go test ./... && cd web && npm test && npm run lint && npm run build && cd ../viewer-app && npm test && npm run build`

Expected: PASS.

- [ ] **Step 5: Build and publish the development installer**

Run: `SERVER_URL="${CAMSTATION_VIEWER_SERVER_URL:-http://$(hostname -I | awk '{print $1}'):18080}"; cd viewer-app && npm run build:installer -- --server-url "$SERVER_URL" --version 2.0.0-dev.1 && cd .. && scripts/publish-viewer-release.sh --installer viewer-app/dist/CamStationViewerSetup.exe --version 2.0.0-dev.1 --release-dir data/viewer-releases --development-unsigned`

Expected: `data/viewer-releases/current/release.json` and installer exist and match.

- [ ] **Step 6: Rebuild and restart CamStation with camera media still disabled**

Run: `cd web && npm run build && cd .. && go build -o camstationd ./cmd/camstationd && env PATH=/usr/bin:/bin CAMSTATION_RECORDING_ENABLED=false scripts/camstationctl.sh restart && scripts/camstationctl.sh verify`

Expected: daemon and HTTP verification pass; recorder/go2rtc camera media remains disabled for this deployment check.

- [ ] **Step 7: Verify the live API and settings download**

Run: `curl -fsS http://127.0.0.1:18080/api/viewers/app/version && curl -fsS -o /tmp/CamStationViewerSetup.exe http://127.0.0.1:18080/api/viewers/app/download && sha256sum /tmp/CamStationViewerSetup.exe data/viewer-releases/current/CamStationViewerSetup.exe`

Expected: metadata reports `2.0.0-dev.1`; both digests are identical. Browser verification confirms `/settings` shows the Windows installer card and the link downloads the same file.

- [ ] **Step 8: Update implementation status and commit Task 8**

```bash
git add scripts/publish-viewer-release.sh scripts/publish-viewer-release_test.sh docs/07-implementation-status.md cmd/camstationd/web
git commit -m "feat(viewers): publish downloadable Windows client"
```

## Final Verification

- [ ] `git diff --check` reports no whitespace errors.
- [ ] `go test ./...` passes.
- [ ] `cd web && npm test && npm run lint && npm run build` passes.
- [ ] `cd viewer-app && npm test && npm run build && npm run package:win` passes.
- [ ] Windows Agent, bootstrap, and installer cross-compile to PE32+ x86-64 executables.
- [ ] Live version metadata, installer download, manifest size, and SHA-256 match.
- [ ] `/settings` exposes the installer and no server filesystem path.
- [ ] `scripts/camstationctl.sh status` and `verify` pass with camera media disabled.
- [ ] No generated installer, Electron package, runtime state, DB, log, recording, or credential is staged.
- [ ] Review the final diff against `docs/superpowers/specs/2026-07-16-windows-viewer-control-and-playback-design.md` before claiming completion.
