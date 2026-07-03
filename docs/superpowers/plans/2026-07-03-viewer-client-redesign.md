# viewer-client-redesign - Work Plan

## TL;DR (For humans)
**What you'll get:** A Windows CamStation viewer EXE that asks for the server address and viewer name on first run, opens the CamStation 2.0 live screen by default, and stays controllable from the server even if the web live renderer freezes or crashes.

**Why this approach:** The current 1.0 failure points to a thin webview problem: the renderer stopped sending heartbeat. This plan keeps the useful web live UI, but moves heartbeat, command receiving, recovery, diagnostics, and restart authority into the native Electron main process, with a hard stability gate before accepting the web runtime.

**What it will NOT do:** It will not use legacy `/new`, expose camera/go2rtc secrets, add login/token pairing in the first release, or build a native mpv/libVLC player unless the stability gate fails.

**Effort:** XL
**Risk:** High - desktop packaging, live video recovery, process supervision, and server-client command contracts all have to work together under long-running CCTV conditions.
**Decisions to sanity-check:** Q1=A hardened Electron, but not a plain webview shell. Q2=server address plus viewer name entered by the client, editable in settings, with an internal stable client id. Q3=A stable basics first; auto-update and native player fallback are out of first release.

Your next move: run `$omo:start-work` or ask for high-accuracy plan review first. Full execution detail follows below.

---

> TL;DR (machine): XL / High risk; build a supervised Electron Windows viewer client, 2.0 viewer API/control extensions, `/live?viewer=1` telemetry bridge, Viewers page controls, EXE packaging, and stability soak QA.

## Scope
### Must have
- A new 2.0 viewer client project under `/root/camstation/viewer-app/`.
- First-run setup page:
  - server address input
  - viewer display name input
  - connection test button
  - confirm/start button
  - persisted settings file owned by the EXE
- Settings access in the EXE:
  - change server address
  - change viewer display name
  - preserve generated stable client id across renames
  - explicit reset identity action only if implemented with confirmation
- Default viewer target: CamStation 2.0 `/live?viewer=1`; never legacy `/new?viewer=1`.
- Electron main process owns:
  - stable client id generation
  - viewer display name and server URL persistence
  - process heartbeat to server
  - command/control receive loop
  - command acknowledgement/failure reporting
  - renderer crash and unresponsive recovery
  - controlled app restart
  - diagnostic snapshot collection
- Renderer/preload bridge owns only UI telemetry and stream commands:
  - camera stream `streamName`
  - connected/open state
  - binary receive timestamp
  - video time progress timestamp
  - `readyState`
  - stalled ms
  - reconnect count
  - renderer status
  - per-stream resubscribe command handler
- Server viewer API supports:
  - heartbeat including main-process status and renderer/stream telemetry
  - command creation from admin UI
  - command delivery by SSE control stream, with long-poll fallback
  - command state transitions and redacted result details
  - viewer app version/download metadata
- Server control safety:
  - `clientId` is a stable identifier, not an authentication secret
  - command creation remains inside the existing same-origin/admin console boundary
  - name/client-id spoofing on the trusted LAN is an accepted first-release risk because the user chose no pairing/login
  - heartbeat, command, telemetry, result, and diagnostic payloads have size limits
  - restart/reload/stream-restart/diagnostics commands have per-viewer caps and cooldowns
- Electron security hardening:
  - accept only `http:` and `https:` server URLs without embedded credentials
  - reject `file:`, `javascript:`, `data:`, custom schemes, malformed hosts, and credentials-in-URL
  - BrowserWindow uses `nodeIntegration: false`, `contextIsolation: true`, `sandbox: true`, and `webSecurity: true`
  - unexpected navigation, permission requests, `window.open`, and arbitrary downloads are denied or explicitly allowlisted
- Command types for first release:
  - `ping`
  - `reload_live`
  - `restart_app`
  - `resubscribe_stream`
  - `restart_stream`
  - `capture_diagnostics`
- Viewers page supports:
  - registry/status display
  - display name/hostname/version/client id
  - main process heartbeat age
  - renderer status
  - per-stream health list
  - buttons for ping, reload live, restart app, per-stream resubscribe, per-stream server restart, and diagnostics
  - clear distinction between client-side resubscribe and server-side stream restart
- EXE artifact serving:
  - version endpoint
  - download endpoint
  - version metadata includes `version`, `filename`, `sizeBytes`, and `sha256`
  - no raw local path exposure
- Diagnostics:
  - `capture_diagnostics` produces a whitelist-only bundle
  - include only app version, OS summary, renderer state, heartbeat/control status, recent redacted command results, and stream telemetry
  - exclude settings files, environment values, generated go2rtc config, raw camera URLs, credentials, raw logs unless redacted, local filesystem paths, and recordings
- Stability gate:
  - forced renderer crash recovers
  - forced renderer unresponsive recovers
  - main-process heartbeat continues while renderer is down
  - server command channel remains recoverable
  - per-stream resubscribe affects only the requested stream
  - repeated reload/restart does not orphan processes
  - cctv2 long soak records KST timestamped evidence

### Must NOT have (guardrails, anti-slop, scope boundaries)
- Do not implement another thin webview where renderer JavaScript owns heartbeat or command polling.
- Do not target `/new`, `/new?viewer=1`, legacy `/viewer`, or any 1.0 route as the new client default.
- Do not expose raw RTSP URLs, camera credentials, raw go2rtc APIs, generated go2rtc config, or local filesystem paths in public API/UI/logs/evidence.
- Do not bind go2rtc API/RTSP outside `127.0.0.1`.
- Do not add full auth, admin login, or pairing token to the first viewer release.
- Do not require the viewer EXE to know camera credentials or talk to cameras directly.
- Do not implement auto-update in first release; version/download is enough.
- Do not implement native mpv/libVLC playback in first release unless the stability gate fails and the user explicitly approves switching the player layer.
- Do not overwrite unrelated active worktree changes, especially the in-progress non-camera console files and generated web assets.

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: TDD for backend/store/routes, frontend viewer-mode bridge, Electron main/preload logic, and package metadata. Each behavior change needs a failing-first proof before production code.
- Go gates:
  - `go test ./internal/store ./cmd/camstationd -run 'Viewer|ViewerApp|Stream' -count=1`
  - `go test ./...`
  - `go build -o camstationd ./cmd/camstationd`
- Web gates:
  - `cd web`
  - `npm run lint`
  - `npm run build`
- Viewer app gates:
  - `cd viewer-app`
  - `npm test`
  - `npm run build:ts`
  - `npm run pack`
  - `npm run build:portable`
- Runtime gates:
  - `scripts/camstationctl.sh restart`
  - `scripts/camstationctl.sh verify`
  - API curl scenarios against `http://127.0.0.1:18080`
  - Playwright browser QA for `/live?viewer=1` and `/viewers`
  - Electron app QA through `xvfb-run` or a real desktop session; do not substitute CLI-only evidence for GUI behavior
- QA script rule:
  - any task that invokes a new QA script must create that script and package script in the same task, not a later task
- Hard stability acceptance metrics:
  - viewer heartbeat age remains <= 30 seconds while renderer is crashed or unresponsive
  - forced SSE disconnect falls back to long-poll and receives a pending command within 30 seconds
  - after 20 reload/restart cycles, viewer process count returns to baseline within 30 seconds and no orphan Electron child process remains
  - 60-minute soak process RSS stays within max(25% over post-warmup baseline, 300 MB absolute growth) unless a larger threshold is justified in evidence
  - `restart_app`, `reload_live`, `restart_stream`, and diagnostics cooldown/cap violations return visible rejected/skipped command results
- Stability evidence paths:
  - `.omo/evidence/viewer-client-redesign/task-*/commands.txt`
  - `.omo/evidence/viewer-client-redesign/task-*/api/*.json`
  - `.omo/evidence/viewer-client-redesign/task-*/browser/*.png`
  - `.omo/evidence/viewer-client-redesign/task-*/electron/*.png`
  - `.omo/evidence/viewer-client-redesign/task-*/electron/*.log`
  - `.omo/evidence/viewer-client-redesign/task-*/soak/*.txt`
- Secret check:
  - run a redacted no-output scan against source, built web assets, viewer app bundle, and `.omo/evidence`
  - record only pass/fail and non-secret fingerprints
- Stability fallback rule:
  - If any hard stability criterion fails twice after targeted fixes, stop implementation and write a native-player fallback plan before further viewer client coding.

## Execution strategy
### Parallel execution waves
> Target 5-8 todos per wave. Fewer than 3 (except the final) means you under-split.
- Wave 0: Reconcile current dirty worktree and lock the viewer contracts without changing behavior outside the viewer domain.
- Wave 1: Backend viewer identity, command/control, and artifact endpoints.
- Wave 2: Web `/live?viewer=1` telemetry bridge and `/viewers` admin controls.
- Wave 3: Electron client setup/settings and native main-process supervision/control.
- Wave 4: Packaging, integration, stability harness, cctv2 runtime QA, and final review.

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| 1 | none | 2,3,4,5,6,7,8,9,10 | none |
| 2 | 1 | 3,4,5,7,9,10 | none |
| 3 | 1,2 | 5,6,7,9,10 | 4 |
| 4 | 1,2 | 6,8,9,10 | 3 |
| 5 | 2,3 | 7,9,10 | 6,8 |
| 6 | 2,3,4 | 8,9,10 | 5,7 |
| 7 | 2,3,5 | 8,9,10 | 6 |
| 8 | 4,6,7 | 9,10 | none |
| 9 | 5,6,7,8 | 10 | none |
| 10 | 2,3,4,5,6,7,8,9 | F1-F4 | none |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->
- [ ] 1. Worktree reconciliation and viewer contract baseline
  What to do / Must NOT do: Start by recording `git status --short --branch` and the current viewer-related files because this workspace already has active non-camera console changes. Treat `cmd/camstationd/routes_viewers.go`, `internal/store/viewers.go`, `internal/store/viewer_commands.go`, `web/src/pages/ViewersPage.tsx`, `web/src/pages/viewers/`, and `web/src/app/streamsViewersSystemApi.ts` as in-progress assets to reconcile, not blank-slate future files. Check existing non-camera-console evidence for unresolved `/viewers` loading/empty-state findings before modifying that page. Do not revert, delete, or rewrite unrelated active changes. Create `.omo/evidence/viewer-client-redesign/task-1/baseline.md` summarizing exact current files, route shapes, unresolved evidence, and owned paths for this plan. Mark `/opt/camstation/...` references as external legacy `ssh cctv` evidence, not local repo files.
  Parallelization: Wave 0 | Blocked by: none | Blocks: 2,3,4,5,6,7,8,9,10
  References (executor has NO interview context - be exhaustive): `/root/camstation/AGENTS.md`; `/root/camstation/docs/00-project-summary.md`; `/root/camstation/docs/03-camstationd-architecture.md`; `/root/camstation/docs/07-implementation-status.md`; `/root/camstation/.omo/drafts/viewer-client-redesign.md`; `/root/camstation/cmd/camstationd/routes_viewers.go`; `/root/camstation/internal/store/viewers.go`; `/root/camstation/internal/store/viewer_commands.go`; `/root/camstation/web/src/pages/ViewersPage.tsx`; `/root/camstation/web/src/app/streamsViewersSystemApi.ts`
  Acceptance criteria (agent-executable): `git status --short --branch` is captured before and after; `rg -n "registerViewerRoutes|ViewerHeartbeat|ViewerCommand|useViewerHeartbeat|viewer=1|useMseStream" cmd internal web .omo/drafts/viewer-client-redesign.md` output is captured; `test -d viewer-app && rg -n "viewer=1|useViewerHeartbeat|render-process-gone|unresponsive" viewer-app || true` output is captured separately so the task does not fail before `viewer-app/` exists; baseline file lists owned paths, external legacy references, unresolved `/viewers` evidence, and dirty-worktree risks; the after-status differs from the before-status only by `.omo/evidence/viewer-client-redesign/task-1/` files.
  QA scenarios (name the exact tool + invocation): happy: `rg -n "registerViewerRoutes|ViewerHeartbeat|ViewerCommand" cmd internal web` records current viewer foundation in `.omo/evidence/viewer-client-redesign/task-1/rg-viewer.txt`; failure: compare `.omo/evidence/viewer-client-redesign/task-1/git-status-before.txt` and `git-status-after.txt` and fail if any non-evidence path changed during this task. Evidence `.omo/evidence/viewer-client-redesign/task-1/`.
  Commit: N | chore(plan): record viewer client baseline

- [ ] 2. Backend viewer identity and heartbeat contract
  What to do / Must NOT do: Extend or finalize the server-side viewer heartbeat schema around the approved identity model: generated stable `id`, operator-entered `displayName`, app version, hostname, device label, route, mode, main-process status, renderer status, and per-stream telemetry. Treat `id` as an identifier, not an authentication secret; document that spoofing is accepted only within the current trusted LAN/admin boundary because the first release has no pairing/login. The server must allow the same stable id to be renamed without creating a second viewer. It must reject missing id, missing displayName, missing route/mode, oversized strings, oversized stream arrays/payload JSON, too-frequent heartbeats outside configured bounds, and raw-secret-looking values through existing redaction helpers. Do not add login, pairing token, camera credentials, or public raw go2rtc data.
  Parallelization: Wave 1 | Blocked by: 1 | Blocks: 3,4,5,7,9,10
  References: `/root/camstation/internal/store/viewers.go`; `/root/camstation/internal/store/viewer_system_schema.go`; `/root/camstation/internal/store/schema.go`; `/root/camstation/cmd/camstationd/routes_viewers.go`; `/root/camstation/cmd/camstationd/routes_streams_viewers_system_test.go`; `/root/camstation/web/src/app/streamsViewersSystemApi.ts`; `/opt/camstation/backend/models.py:174`; `/opt/camstation/frontend/src/viewerHealth.ts`
  Acceptance criteria: Add failing-first Go tests, then pass `go test ./internal/store ./cmd/camstationd -run 'Viewer.*Heartbeat|Viewer.*Identity|Viewer.*Rename' -count=1`. API response for a rename heartbeat keeps the same `id`, updates `displayName`, and stores `lastHeartbeatAt`. Public JSON contains no raw RTSP-looking URL or credential.
  QA scenarios: happy: `curl -i -X POST http://127.0.0.1:18080/api/viewers/heartbeat -H 'Content-Type: application/json' --data '{"id":"viewer-qa-stable","displayName":"QA 관제 클라이언트","appVersion":"2.0.0","hostname":"qa-host","deviceLabel":"control-room","route":"/live?viewer=1","mode":"live","mainStatus":"running","rendererStatus":"ready","streams":[{"streamName":"camera-1","state":"streaming","latencyMs":120}]}'` returns 200 and then `GET /api/viewers` shows one renamed row after a second heartbeat with a different displayName. Failure: same curl with empty `displayName` returns 400 and creates no viewer row. Evidence `.omo/evidence/viewer-client-redesign/task-2/api/`.
  Commit: N | feat(viewers): define stable viewer identity heartbeat

- [ ] 3. Backend command lifecycle and control delivery
  What to do / Must NOT do: Implement a durable viewer command contract for `ping`, `reload_live`, `restart_app`, `resubscribe_stream`, `restart_stream`, and `capture_diagnostics`. Commands must include id, viewer id, type, optional streamName, optional route/mode/message, state, timestamps, redacted result/error, and idempotent ACK/failure updates. Add command delivery with `GET /api/viewers/{id}/control` as a stdlib SSE stream first, and `GET /api/viewers/{id}/commands?wait=25` as the explicit fallback long-poll path. `GET /api/viewers/{id}/commands` without `wait` remains a non-blocking command list/dequeue endpoint for compatibility. Add per-viewer pending command caps, command creation rate limits, payload/result/error size limits, long-poll timeout caps, and cooldowns for `restart_app`, `reload_live`, `restart_stream`, and `capture_diagnostics`. Dangerous commands must be creatable only from the existing admin/same-origin console boundary. The client main process must be able to receive commands while renderer JS is dead. Do not add a WebSocket dependency for this control plane.
  Parallelization: Wave 1 | Blocked by: 1,2 | Blocks: 5,6,7,9,10
  References: `/root/camstation/internal/store/viewer_commands.go`; `/root/camstation/cmd/camstationd/routes_viewers.go`; `/root/camstation/web/src/pages/viewers/ViewerCommandPanel.tsx`; `/opt/camstation/backend/routers/viewers.py:211`; `/opt/camstation/backend/models.py:204`; `/opt/camstation/frontend/src/useViewerHeartbeat.ts:96`
  Acceptance criteria: Add failing-first Go tests for command validation, command creation, delivery marking, SSE delivery, long-poll fallback, ACK, failure, cancel/delete, unknown command rejection, streamName-required validation for stream commands, command caps/cooldowns, oversized payload rejection, and no raw-secret result leakage. Pass `go test ./internal/store ./cmd/camstationd -run 'Viewer.*Command|Viewer.*Control' -count=1`.
  QA scenarios: happy: create heartbeat for `viewer-qa-stable`, then run `CMD_ID=$(curl -fsS -X POST http://127.0.0.1:18080/api/viewers/viewer-qa-stable/commands -H 'Content-Type: application/json' --data '{"type":"resubscribe_stream","streamName":"camera-1","message":"QA resubscribe"}' | tee .omo/evidence/viewer-client-redesign/task-3/api/create-command.json | jq -r '.id')`; run `curl -i http://127.0.0.1:18080/api/viewers/viewer-qa-stable/commands`; ACK with `curl -i -X PATCH "http://127.0.0.1:18080/api/viewers/viewer-qa-stable/commands/${CMD_ID}" -H 'Content-Type: application/json' --data '{"state":"acknowledged"}'`. SSE/fallback: run `curl -N --max-time 5 http://127.0.0.1:18080/api/viewers/viewer-qa-stable/control > .omo/evidence/viewer-client-redesign/task-3/api/control-sse.txt || true`, then create another command and verify `curl -i "http://127.0.0.1:18080/api/viewers/viewer-qa-stable/commands?wait=25"` returns it within 30 seconds. Failure: `restart_stream` without `streamName` returns 400 and leaves no pending command; over-cap command creation returns a rejected/429-style response. Evidence `.omo/evidence/viewer-client-redesign/task-3/api/`.
  Commit: N | feat(viewers): add durable viewer command delivery

- [ ] 4. Viewer app version/download endpoints and artifact safety
  What to do / Must NOT do: Add server endpoints `GET /api/viewers/app/version` and `GET /api/viewers/app/download`. The version endpoint reads configured viewer artifact metadata containing `version`, `filename`, `sizeBytes`, and `sha256`; the download endpoint streams only the configured EXE artifact. Configure defaults under a non-runtime committed path only for tests and under a reviewed deployment artifact path for real use. Do not serve from `data/`, runtime logs, recordings, temp directories, or arbitrary request paths. Do not expose filesystem paths, directory listings, missing-file internals, or raw upload capability. Do not commit EXE binaries.
  Parallelization: Wave 1 | Blocked by: 1,2 | Blocks: 6,8,9,10
  References: `/root/camstation/cmd/camstationd/routes_viewers.go`; `/root/camstation/cmd/camstationd/routes_settings_jobs.go`; `/root/camstation/cmd/camstationd/main.go`; `/root/camstation/docs/03-camstationd-architecture.md`; `/opt/camstation/backend/routers/settings.py:33`; `/opt/camstation/deploy/deploy-viewer.sh`
  Acceptance criteria: Add failing-first Go tests for missing version metadata, version read, missing EXE, SHA-256 mismatch, size mismatch, fixed download headers, `Content-Disposition: attachment`, path traversal rejection, disallowed runtime/data/log directory roots, and no raw local path in error JSON. Pass `go test ./cmd/camstationd -run 'ViewerApp|ViewerDownload|ViewerVersion' -count=1`.
  QA scenarios: happy: create a temp fixture `CamViewer.exe` and metadata through test configuration, then `curl -i http://127.0.0.1:18080/api/viewers/app/version` returns JSON with `version`, `filename`, `sizeBytes`, and `sha256`, and `curl -i http://127.0.0.1:18080/api/viewers/app/download` returns `application/octet-stream` with attachment headers. Failure: point config to missing artifact or wrong hash and verify error JSON contains no path. Evidence `.omo/evidence/viewer-client-redesign/task-4/api/`.
  Commit: N | feat(viewers): serve viewer app artifact metadata

- [ ] 5. `/live?viewer=1` renderer telemetry and stream command bridge
  What to do / Must NOT do: Add a viewer-mode bridge to the React live workspace. When URL has `viewer=1`, the live page must still render the monitoring workspace but additionally report renderer status and stream telemetry through a narrow preload-safe API if present. Extend `useMseStream` or a nearby helper to report binary receive time, video currentTime progress, `readyState`, stalled ms, reconnect count, and error per `streamName`. Add a command handler that can resubscribe/recreate one stream pipeline without reloading the whole page. Do not enable native video controls, do not break wheel zoom/pan, do not target `/new`, and do not expose raw camera URLs.
  Parallelization: Wave 2 | Blocked by: 2,3 | Blocks: 7,9,10
  References: `/root/camstation/web/src/app/App.tsx`; `/root/camstation/web/src/layouts/ConsoleLayout.tsx`; `/root/camstation/web/src/pages/LivePage.tsx`; `/root/camstation/web/src/components/live/LiveWorkspace.tsx`; `/root/camstation/web/src/components/live/useMseStream.ts`; `/root/camstation/web/src/app/basePath.ts`; `/opt/camstation/frontend/src/hooks/useMSE.ts`; `/opt/camstation/frontend/src/viewerHealth.ts`; `/root/camstation/web/AGENTS.md`
  Acceptance criteria: Add failing-first TypeScript/unit tests for viewer-mode detection, telemetry payload shaping, and resubscribe event dispatch. Add `web/scripts/qa/viewer-live-bridge.mjs` in this task. Pass `cd web` then `npm run lint` and `npm run build`. Browser QA proves `/live?viewer=1` opens the live workspace and does not navigate to `/new`.
  QA scenarios: happy: `node web/scripts/qa/viewer-live-bridge.mjs --url http://127.0.0.1:18080/live?viewer=1 --evidence .omo/evidence/viewer-client-redesign/task-5/browser` opens the page, injects a mock `window.camstationViewer` telemetry sink before app load, waits for stream telemetry from at least one tile or a no-camera empty state marker, and captures screenshot. Failure: the same script with `--resubscribe camera-1 --assert-isolated` dispatches `resubscribe_stream` and verifies only that stream's MSE generation/reconnect marker changes while other tiles remain mounted. Evidence `.omo/evidence/viewer-client-redesign/task-5/browser/`.
  Commit: N | feat(live): add viewer-mode telemetry bridge

- [ ] 6. Viewers page control surface
  What to do / Must NOT do: Extend the Viewers page so an operator can select a viewer and send first-release commands. The UI must show displayName, stable id, hostname/device label, app version, main heartbeat age, renderer status, route/mode, and per-stream health. Buttons must use explicit labels in Korean and distinguish client-side `재송신`/resubscribe from server-side stream restart. Restart app must require confirmation. Diagnostics must show job/result status without raw paths or secrets. Do not hide API errors; show stale/offline state clearly.
  Parallelization: Wave 2 | Blocked by: 2,3,4 | Blocks: 8,9,10
  References: `/root/camstation/web/src/pages/ViewersPage.tsx`; `/root/camstation/web/src/pages/viewers/ViewerCommandPanel.tsx`; `/root/camstation/web/src/pages/viewers/ViewerRegistryPanel.tsx`; `/root/camstation/web/src/app/streamsViewersSystemApi.ts`; `/root/camstation/web/src/app/streamsViewersSystemQueries.ts`; `/root/camstation/web/src/app/i18n.tsx`; `/root/camstation/web/src/styles/index.css`; `/root/camstation/DESIGN.md`
  Acceptance criteria: Add failing-first component or integration tests where available, then pass `cd web` then `npm run lint` and `npm run build`. The UI can create `ping`, `reload_live`, `restart_app`, `resubscribe_stream`, `restart_stream`, and `capture_diagnostics` commands with correct payloads and invalidation, and fixes any unresolved `/viewers` loading-state evidence from Task 1.
  QA scenarios: happy: `node web/scripts/qa/viewers-control-surface.mjs --server http://127.0.0.1:18080 --viewer viewer-qa-stable --stream camera-1 --evidence .omo/evidence/viewer-client-redesign/task-6/browser` opens `/viewers`, registers a QA viewer via API, selects it, sends ping, resubscribe for `camera-1`, and restart app with confirmation; screenshots show loading, command states, and stream health. Failure: sending resubscribe with no selected stream is disabled or returns a visible validation message; restart app cannot be sent without confirmation. Evidence `.omo/evidence/viewer-client-redesign/task-6/browser/`.
  Commit: N | feat(viewers): add stable client control surface

- [ ] 7. Electron viewer app setup, settings, and identity
  What to do / Must NOT do: Create `/root/camstation/viewer-app/` as the 2.0 viewer client. Use Electron + TypeScript + electron-builder unless the user changed Q1. Implement setup UI with server address and viewer display name, connection test, start button, and settings menu/screen for later edits. Persist `serverUrl`, `displayName`, `fullscreenOnStart` if included, and generated stable `clientId` in the app userData settings file. Changing displayName/serverUrl must not change `clientId`. Harden URL handling: allow only `http:` and `https:` server URLs without credentials; reject `file:`, `javascript:`, `data:`, custom schemes, malformed hosts, and credentials-in-URL. Harden BrowserWindow with `nodeIntegration: false`, `contextIsolation: true`, `sandbox: true`, and `webSecurity: true`; expose only a narrow preload API. Deny unexpected navigation, permission requests, `window.open`, and arbitrary downloads unless explicitly allowlisted. Define `npm run qa:setup` in this task. Do not copy legacy route builder that targets `/new?viewer=1`; target `/live?viewer=1`.
  Parallelization: Wave 3 | Blocked by: 2,3,5 | Blocks: 8,9,10
  References: `/opt/camstation/viewer-app/package.json`; `/opt/camstation/viewer-app/electron/main.ts`; `/opt/camstation/viewer-app/electron/preload.ts`; `/opt/camstation/viewer-app/electron/viewerNavigation.ts`; `/opt/camstation/viewer-app/src/setup.html`; `/opt/camstation/viewer-app/src/setup.js`; `/root/camstation/web/src/app/App.tsx`; `/root/camstation/.omo/drafts/viewer-client-redesign.md`
  Acceptance criteria: Add failing-first Node tests for URL normalization, URL rejection cases, BrowserWindow security settings, navigation/window-open/permission/download denial, viewer URL builder, settings persistence, stable id preservation on rename, and validation of empty displayName/serverUrl. Pass `cd viewer-app` then `npm test` and `npm run build:ts`.
  QA scenarios: happy: `xvfb-run -a npm run qa:setup -- --server http://127.0.0.1:18080 --name "QA Viewer"` starts the app, fills server/name, tests connection, saves settings, and lands on `/live?viewer=1`; screenshot and app log saved. Failure: run the same QA script with `--server http://127.0.0.1:1 --name ""` and verify the start button stays disabled and a Korean error appears. Evidence `.omo/evidence/viewer-client-redesign/task-7/electron/`.
  Commit: N | feat(viewer-app): add setup and stable identity

- [ ] 8. Electron main-process heartbeat, command control, and recovery
  What to do / Must NOT do: Implement main-process viewer supervision. The main process sends heartbeat on a fixed interval even if renderer is stuck, opens the command control stream/long-poll, executes `ping`, `reload_live`, `restart_app`, `resubscribe_stream`, `restart_stream`, and `capture_diagnostics`, and ACKs/fails commands with redacted results. Renderer telemetry reaches main through preload IPC. Use `render-process-gone`, `unresponsive`, `responsive`, and load failure events to record renderer state and recover by reload, then BrowserWindow recreation, then app restart only after concrete thresholds: max 3 renderer reloads in 5 minutes, max 1 BrowserWindow recreation in 5 minutes, max 1 app restart in 10 minutes, then enter persistent failed state with server-visible reason until manual/server command intervention. Add SSE reconnect backoff with jitter, long-poll fallback timeout cap, command cooldown enforcement, and diagnostics size cap. Define `capture_diagnostics` as the whitelist-only bundle described in Scope. Define `npm run qa:control` in this task. Do not rely on renderer JS for command polling. Do not create recursive restart loops.
  Parallelization: Wave 3 | Blocked by: 4,6,7 | Blocks: 9,10
  References: `/opt/camstation/viewer-app/electron/main.ts:146`; `/opt/camstation/viewer-app/electron/preload.ts`; `/opt/camstation/frontend/src/useViewerHeartbeat.ts`; `/root/camstation/cmd/camstationd/routes_viewers.go`; `/root/camstation/internal/store/viewer_commands.go`; Electron docs `https://electronjs.org/docs/latest/api/web-contents`
  Acceptance criteria: Add failing-first Node tests for heartbeat scheduling, command ACK/failure, renderer status transitions, restart script generation, control-stream reconnect/fallback, cooldown rejection, diagnostics whitelist/redaction, restart-loop thresholds, and command redaction. Pass `cd viewer-app` then `npm test` and `npm run build:ts`. API evidence shows heartbeat continues after test-triggered renderer reload, crash, and hang commands.
  QA scenarios: happy: `xvfb-run -a npm run qa:control -- --server http://127.0.0.1:18080 --name "QA Control Viewer"` starts viewer, server sends ping/reload/resubscribe/restart-stream commands via curl, and command results become acknowledged/failed as expected. Failure: `xvfb-run -a npm run qa:control -- --server http://127.0.0.1:18080 --name "QA Crash Viewer" --inject-renderer-crash --inject-renderer-hang --inject-sse-drop --assert-heartbeat-max-age 30` verifies main-process heartbeat still updates `/api/viewers`, renderer status changes to recovering, SSE reconnects or falls back to long-poll, and window reload/recreate succeeds within thresholds. Evidence `.omo/evidence/viewer-client-redesign/task-8/electron/`.
  Commit: N | feat(viewer-app): supervise renderer and command control

- [ ] 9. Packaging, artifact deployment, and stability harness
  What to do / Must NOT do: Add viewer-app package scripts for TypeScript build, unit tests, unpacked pack, and Windows portable EXE build. Add a deployment helper that copies only the built EXE and version metadata to the configured viewer artifact directory for camstationd, with cctv2/test defaults and no production `cctv` mutation unless explicitly requested. The helper computes SHA-256 and size metadata. Add QA scripts for renderer crash, unresponsive renderer, SSE drop/long-poll fallback, per-stream WebSocket stall/resubscribe, repeated reloads, memory/process growth sampling, and long soak. State first-release signing policy: require Authenticode signing before production release, or explicitly mark the portable EXE as unsigned and publish SHA-256 verification instructions as the minimum accepted test-release control. Do not commit binaries, runtime settings, logs, or recordings.
  Parallelization: Wave 4 | Blocked by: 5,6,7,8 | Blocks: 10
  References: `/opt/camstation/viewer-app/package.json`; `/opt/camstation/deploy/deploy-viewer.sh`; `/root/camstation/scripts/AGENTS.md`; `/root/camstation/scripts/camstationctl.sh`; `/root/camstation/cmd/camstationd/web`; `/root/camstation/.gitignore`; `/root/camstation/web/.gitignore`
  Acceptance criteria: `cd viewer-app` then `npm test`, `npm run build:ts`, `npm run pack`, and `npm run build:portable` pass or, if the current Linux host lacks Windows build prerequisites, the worker records the exact prerequisite failure and still verifies `npm run pack`; final success still requires a Windows portable EXE artifact before this todo can be checked complete. Deployment helper dry-run prints source/destination/version/hash/size without copying; non-dry-run to a temp artifact dir makes version/download endpoints work and hash/size match.
  QA scenarios: happy: `viewer-app/scripts/deploy-viewer.sh --artifact-dir .omo/evidence/viewer-client-redesign/task-9/artifacts --version 2.0.0 --exe viewer-app/dist/CamViewer.exe --dry-run` records planned copy/hash/size, then a temp non-dry run makes `curl -i http://127.0.0.1:18080/api/viewers/app/version` return `2.0.0` plus `sha256` and `sizeBytes`. Failure: deploy with missing EXE or tampered hash exits nonzero and does not update version metadata. Evidence `.omo/evidence/viewer-client-redesign/task-9/`.
  Commit: N | build(viewer-app): package stable Windows viewer

- [ ] 10. Full integration, cctv2 soak, and fallback decision
  What to do / Must NOT do: Run full integration after backend, web, viewer app, and packaging tasks land. Build web into embedded assets, build daemon, restart through `scripts/camstationctl.sh`, run API/browser/Electron scenarios, and run the stability soak on cctv2 or a named camera-reachable substitute recorded in evidence. Prefer cctv2; do not silently substitute local no-camera evidence for real-camera stability. Use KST in timestamps. If any hard stability scenario fails twice after targeted fixes, do not mark this plan complete; write a native-player fallback plan instead. Do not use ad hoc kill/pkill/nohup for daemon lifecycle.
  Parallelization: Wave 4 | Blocked by: 2,3,4,5,6,7,8,9 | Blocks: F1-F4
  References: `/root/camstation/scripts/camstationctl.sh`; `/root/camstation/docs/04-cctv2-test-plan.md`; `/root/camstation/docs/06-cctv2-handoff.md`; `/root/camstation/web/vite.config.ts`; `/root/camstation/cmd/camstationd/web`; `/root/camstation/viewer-app/`; `/root/camstation/.omo/drafts/viewer-client-redesign.md`
  Acceptance criteria: `go test ./...`, `cd web && npm run lint`, `cd web && npm run build`, `go build -o camstationd ./cmd/camstationd`, `cd viewer-app && npm test`, `cd viewer-app && npm run build:portable`, `scripts/camstationctl.sh restart`, and `scripts/camstationctl.sh verify` all pass. Browser QA covers `/live?viewer=1` and `/viewers`; Electron QA covers setup, settings rename/server change, command control, forced renderer crash, forced unresponsive renderer, per-stream resubscribe, app restart, and diagnostics. Soak evidence records heartbeat freshness, renderer recovery count, memory/process growth, and stream health over the configured duration.
  QA scenarios: happy: `viewer-app/scripts/qa/full-stability-run.mjs --server http://127.0.0.1:18080 --name "QA Stability Viewer" --duration-minutes 60 --assert-heartbeat-max-age 30 --assert-memory-growth-pct 25 --assert-memory-growth-mb 300 --assert-no-orphans --evidence .omo/evidence/viewer-client-redesign/task-10/soak` completes with all hard criteria green on cctv2 or the named substitute. Failure: `viewer-app/scripts/qa/full-stability-run.mjs --server http://127.0.0.1:18080 --name "QA Failure Viewer" --inject-renderer-crash --inject-renderer-hang --inject-sse-drop --duration-minutes 5 --assert-heartbeat-max-age 30 --evidence .omo/evidence/viewer-client-redesign/task-10/failure` proves the main process heartbeat continues, fallback delivery works, and recovery completes; if not, the task fails and triggers fallback planning. Evidence `.omo/evidence/viewer-client-redesign/task-10/`.
  Commit: N | test(viewer-app): verify stable viewer integration

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [ ] F1. Plan compliance audit
  Owner: independent reviewer or root self-review if subagent spawning is unavailable.
  Acceptance: verifies every todo acceptance criterion and QA scenario has evidence under `.omo/evidence/viewer-client-redesign/`; checks the implementation does not target `/new`, does not make renderer JS own heartbeat/control, and honors Q1/Q2/Q3 decisions.
- [ ] F2. Code quality review
  Owner: independent reviewer or root self-review if subagent spawning is unavailable.
  Acceptance: reviews Go/TS/Electron diffs for type safety, redaction, lifecycle cleanup, process restart loops, command idempotency, no `as any`, no `@ts-ignore`, and no unrelated worktree overwrite.
- [ ] F3. Real manual QA
  Owner: QA executor or root if no subagent is available.
  Acceptance: re-runs the full browser/API/Electron stability scenarios, including forced renderer crash/unresponsive and per-stream resubscribe, and attaches screenshots/logs/API outputs.
- [ ] F4. Scope fidelity
  Owner: independent reviewer or root self-review if subagent spawning is unavailable.
  Acceptance: confirms no auth/login/token system was added, no auto-update/native player was added, no raw secrets are exposed, no cctv production mutation occurred, generated artifacts are handled only through the planned build/deploy paths, Electron hardening is active, command/rate/cooldown limits are enforced, diagnostics are whitelist-only/redacted, and EXE hash/signing metadata policy is satisfied.

## Commit strategy
- Do not auto-commit unless the user explicitly asks.
- If the user asks for commits later, keep them atomic:
  - `feat(viewers): add stable viewer control contract`
  - `feat(live): add viewer-mode telemetry bridge`
  - `feat(viewer-app): add supervised Windows viewer client`
  - `build(viewer-app): package viewer artifact`
  - `test(viewer-app): verify stability gate`
- Keep generated `cmd/camstationd/web` assets in the final integration commit only.
- Never stage runtime `data/`, logs, `.env`, secrets, recordings, viewer settings files, or EXE binaries unless the user explicitly asks for binary artifact handling outside git.

## Success criteria
- First-run EXE setup accepts server address and viewer display name, verifies connection, and opens `/live?viewer=1`.
- EXE settings can change server address and viewer display name later without changing the stable client id.
- Main process sends heartbeat independently of renderer JS.
- Server shows viewer display name, app version, hostname/device label, route/mode, renderer status, and per-stream health.
- Server can send and receive ACK/failure for `ping`, `reload_live`, `restart_app`, `resubscribe_stream`, `restart_stream`, and `capture_diagnostics`.
- Server command creation stays within the existing same-origin/admin boundary and enforces size, cap, and cooldown controls.
- `resubscribe_stream` recreates only the requested stream pipeline.
- Renderer crash and unresponsive states recover without losing main-process heartbeat.
- Repeated reload/restart tests do not leave orphan processes.
- Electron rejects unsafe server URLs and runs with hardened BrowserWindow/preload settings.
- Diagnostics are whitelist-only and redacted.
- Viewer app version/download endpoints serve only configured artifact metadata/content, include `sizeBytes` and `sha256`, and reveal no filesystem paths.
- `/new` is not used anywhere in the 2.0 viewer client.
- Full Go/web/viewer-app build and test gates pass.
- cctv2/camera-reachable stability soak passes or the work is explicitly rerouted to a native-player fallback plan.
