# CamStation 2.0 Paseo development environment

## Scope and decisions

- Target the checked-out `camstation2-initial` branch and its Go single-daemon +
  React/Vite + Electron Viewer architecture.
- Keep the legacy `main` branch and production CCTV services untouched.
- Install the Go toolchain inside the ignored repository-local `.tools/` directory;
  pin Go 1.25.12 and verify the official Linux amd64 SHA-256 before extraction.
- Paseo's default daemon service uses its assigned port, a worktree-local `data/`
  directory, and recording disabled. Real cameras, go2rtc, and rclone remain explicit
  integration-test dependencies rather than worktree bootstrap requirements.
- Paseo's web service uses its assigned port and discovers the sibling daemon through
  `PASEO_SERVICE_DAEMON_PORT`.

## Acceptance criteria

- [x] Add an idempotent bootstrap that installs the pinned Go toolchain, downloads Go
      modules, installs web/Viewer npm lockfiles, and prepares ignored runtime folders.
- [x] Add reusable Go, daemon, web, test, and full-check launchers with safe defaults.
- [x] Make existing Make targets use the repository-local Go wrapper.
- [x] Make the Vite proxy target configurable, use the Paseo daemon port, proxy `/api`
      and `/player`, and leave the React `/live` route to Vite.
- [x] Add a Paseo 0.3.0-compatible `paseo.json` with setup, daemon/web services, tests,
      and complete verification commands.
- [x] Confirm the registered CamStation project actually loads the lifecycle hooks and
      scripts in Paseo 0.3.0; placeholder-only UI fields are not acceptance.
- [x] Update the root README with the actual 2.0 prerequisites, local workflow, Paseo
      workflow, optional integration tools, and secret/runtime safety constraints.
- [x] Run the bootstrap in this checkout and confirm all required tool versions.
- [x] Prove a daemon health request through the Paseo-style Vite proxy and prove `/live`
      is served by Vite rather than proxied to the daemon.
- [x] Run Go tests/build, web tests/lint/build, and Viewer tests/build.
- [x] Validate shell/JSON/Paseo schemas, inspect the final diff, and document review results.

## Review

- Branch: `camstation2-initial`, tracking `origin/camstation2-initial`; legacy `main`
  and production services were not modified.
- Bootstrap: ran `./scripts/setup-dev.sh` twice successfully. The second run reused
  the verified local Go installation and completed cleanly, proving idempotency.
- Toolchain: Go 1.25.12 (`linux/amd64`), Node 22.22.1, npm 11.12.1. The Go archive
  checksum matched the checksum published by go.dev.
- Paseo: `paseo.json` passed JSON parsing and the installed Paseo 0.3.0 raw config
  schema. `daemon` and `web` commands were exercised with Paseo-style environment
  variables on ports 20580 and 20581.
- Service smoke test:
  - direct `GET /api/health` returned `ok: true`;
  - Vite-proxied `GET /api/health` returned the same daemon response;
  - Vite `/live` returned `/@vite/client` and `/src/main.tsx` markers;
  - `/player` reached the daemon and returned the expected 502 because optional
    go2rtc is not installed, rather than a 403 origin rejection;
  - both smoke-test ports were released after graceful terminal shutdown.
- Verification: final `./scripts/check-dev.sh` passed all Go packages, 52 web tests,
  web lint/build, 23 Viewer tests, Viewer build, and the final daemon build.
- Flake audit: one cold first-run Viewer Agent integration test exceeded its tight
  timing window. It then passed 20 focused repetitions, three full-package repetitions,
  and two subsequent complete checks; no unsupported product/test change was made.
- Scoped SCA: `npm audit` findings were remediated with semver-compatible lockfile
  updates only. Web now uses nanoid 3.3.18, postcss 8.5.26, and React Router 7.18.2;
  Viewer tooling uses brace-expansion 5.0.9 and undici 7.29.0. Both audits report zero
  vulnerabilities, and the web/Viewer tests and builds passed afterward.
- Integrity: shell syntax, JSON, Paseo schema, `git diff --check`, embedded asset
  reference, and changed-file credential scans passed.
- Known optional gaps: go2rtc, rclone, and the SQLite CLI are not installed on this
  host. Real-camera, live-stream, recording, and backup integration were intentionally
  not exercised by the safe default worktree setup. Vite continues to report the
  existing large-chunk advisory for the roughly 659 kB main bundle.
- Host note: Paseo 0.3.0's daemon is running, but direct CLI RPCs require the host's
  `PASEO_PASSWORD`. Project configuration and service behavior were validated without
  weakening that daemon authentication.

## UI integration follow-up

- The 2026-08-09 project-settings screenshot shows empty lifecycle fields and no scripts;
  the visible `npm install` and `docker compose down` strings are placeholders.
- `paseo.json` currently parses and passes the local schema, but is untracked. A new
  worktree created from the selected base therefore cannot inherit it.
- Completion now requires evidence from the registered project/daemon path: lifecycle
  values loaded, five scripts listed, and at least the safe setup or service path started
  through Paseo rather than only by invoking wrappers directly.
- The daemon's own project-config reader returned `./scripts/setup-dev.sh` and all five
  configured scripts. The registered Workspace projection also returned exactly `check`,
  `daemon`, `setup`, `test`, and `web`.
- Paseo ran `setup` successfully with exit code 0. It then assigned ports 20869 and 20765
  to `daemon` and `web`; both reached `healthy`, the web proxy returned the daemon health
  payload, and `/live` returned the CamStation 2.0 Vite page.
- Paseo stopped both services with exit code 0, and neither assigned port remained open.
  The Desktop settings page must be re-entered or reloaded to replace its stale empty draft.
- The config remains intentionally uncommitted. A newly created worktree cannot inherit it
  until the user chooses to commit it on the selected base branch.
- Final audit parsed the config and found all five scripts/two services; JSON, shell syntax,
  and `git diff --check` passed. Its port-forwarding warnings are static wrapper heuristics:
  actual Paseo-assigned ports reached both processes. The state marker is worktree-relative
  `data/`; parallel managed-worktree isolation remains untested until the config is committed.

---

# 2026-08-09 CCTV operations and monitor-PC maintenance audit

## Scope and decisions

- Treat the user's request as authorization for the directly connected
  `192.168.0.0/24` CCTV environment, limited to locating the CCTV server and inspecting
  the named monitor PC at `192.168.0.13`.
- Use passive inspection and low-rate, read-only network/service checks. Do not guess
  passwords, exploit vulnerabilities, change configuration, restart services, install
  software, reboot hosts, or contact camera endpoints directly.
- Reuse only already-provisioned credentials or host keys if they are available to this
  maintenance environment; never record secret values in evidence or documentation.
- Preserve all pre-existing worktree changes and keep this audit limited to case records
  and maintenance documentation.

## Acceptance criteria

- [x] Create a granted scope, rules, timeline, evidence, findings, and execution plan.
- [x] Identify the CCTV server using at least two independent signals.
- [x] Verify server identity, service/process health, camera status, recording activity,
      storage pressure, backup/cleanup state, and recent operational errors where access permits.
- [x] Verify `192.168.0.13` reachability, operating-system/service fingerprints, installed
      monitoring-client evidence, supported remote-management methods, and effective access level.
- [x] Distinguish verified facts from inferences and explicitly record credential or
      reachability blockers without exposing secrets.
- [x] Publish a Korean maintenance runbook/report with a topology, access prerequisites,
      safe inspection commands, troubleshooting flow, and escalation checklist.
- [x] Validate links, redaction, command syntax, evidence references, and final diff.

## Review

- Current production was validated as dual-homed host `cctv`: management address
  `10.0.0.26`, CCTV/monitor-LAN address `192.168.0.160`, identical SSH host key, health
  API `ok`, and working existing-key root access. The documented `cctv2` candidate is offline.
- All five core services were active, the database quick-check passed, 8/9 cameras were
  enabled/online, and the one disabled camera was confirmed intentional. A final per-camera
  ten-second sample proved all eight open recorder files grew.
- The backup chain was verified across log, database marks, local deletion, and remote state:
  392 successful cycles/zero failures in 24 hours and 32 matching remote objects in two hours.
- `192.168.0.13` was proven active through current Camviewer 1.0.4 traffic. It is the same
  Windows `NUC` exposed as Tailscale peer `nuc-moniter`; the direct Tailscale path and matching
  AnyDesk certificate establish the identity mapping.
- The Viewer UI is live but the control-agent heartbeat is stale since 2026-07-01 KST.
  Ten pending commands include five obsolete restarts, so the report requires expiring the
  queue before repairing the agent.
- Monitor-PC management surfaces are AnyDesk plus Tailscale Windows OpenSSH/SMB/RPC.
  RDP and WinRM are closed. The initial `dyllislev` key attempt was correctly denied because
  `sshd_config` permits only `CamStationOps`; the operator later registered the dedicated key
  for that allowed administrator account and strict pinned-host SSH now works.
- The Korean report is `docs/2026-08-09_operations-cctv-maintenance-report.md`; the evidence
  chain is under `work/20260809-cctv-operations/`. Report SHA-256 is
  `a45f99cc974229aa0817c04fa8860c2198bb9e2ed6edcee561c86e7eddc59178`.
- Final validation passed: links, code fences, Evidence references, scope guard fields,
  sensitive-pattern scan, runtime-path scan, and `git diff --check`. Existing unrelated
  dirty-worktree changes were preserved.

## External follow-up required

- [x] Register and verify the dedicated maintenance key for the allowed `CamStationOps`
      administrator without broadening `AllowUsers` or enabling password authentication.
- [ ] Restore the intended cctv2 server, prove whether `10.0.0.29` and `192.168.0.172` are the
      same host, then execute the documented 1.0-to-2.0 cutover with GUI and rollback evidence.
- [ ] Record AnyDesk and break-glass ownership only in the organization password manager.

---

# 2026-08-09 NUC remote-control bootstrap

## Scope and decisions

- Run commands only in an elevated Windows PowerShell session physically or interactively
  approved on monitor PC `192.168.0.13` (`NUC`).
- Honor the active `AllowUsers CamStationOps` restriction and provision only the existing
  maintenance **public** key for that dedicated account; do not broaden SSH access to
  `dyllislev`, copy the private key, or place a password/AnyDesk code in project artifacts.
- Detect whether the target account is an Administrators member and honor Windows OpenSSH's
  effective `AuthorizedKeysFile`; preserve existing keys and avoid changing unrelated services.
- Do not revive the stale Viewer agent or begin the 2.0 upgrade until SSH access is proven and
  the obsolete server-side Viewer command queue is safely expired.

## Acceptance criteria

- [x] Confirm `dyllislev` identity, SID, profile, Administrators membership, `sshd` service,
      and the applicable OpenSSH rules; active `AllowUsers` selects `CamStationOps` instead.
- [x] Add the maintenance key idempotently to the correct file with restrictive Windows ACLs
      while preserving any existing authorized keys.
- [x] Prove a non-interactive SSH login over Tailscale from the maintenance environment and
      record the Windows identity/hostname returned by that authenticated session.
- [x] Document rollback, verification evidence, and the next gated steps for stale-command
      cleanup and the CamStation 2.0 client upgrade.

## Review

- User-provided output proves host `NUC`, active automatic `sshd`, and an unelevated
  `NUC\dyllislev` session. `dyllislev` is an Administrators member, but active configuration
  permits only `CamStationOps`; its administrator match uses
  `%ProgramData%\ssh\administrators_authorized_keys`.
- The user ran the guarded registration successfully. The dedicated key fingerprint and ACL
  were verified, and strict host-key-pinned SSH returned `nuc\camstationops` with an
  administrator token on Windows 11 Pro.
- Windows inventory shows Viewer 2.0.20 MSI registrations and its automatic service, but this
  does not supersede the operator-confirmed 1.0 monitoring baseline. Interactive process and
  local IPC evidence must distinguish staged installation from completed cutover.
- Final pinned-SSH recheck returned six active CamViewer 1.0 processes, zero interactive
  CamStationViewer 2.0 processes, and running `CamStationViewerService`/`sshd`. The staged 2.0
  endpoint and documented cctv2 address are both offline, so no cutover action was taken.

---

# 2026-08-09 cctv same-host 1.x-to-2.0 replacement strategy

## Scope and decisions

- The production destination is the existing dual-homed `cctv` host, not the separate
  historical cctv2 development host.
- Merge `camstation2-initial` into `main` as a source-control release decision, then deploy the
  resulting 2.0 release to a separate same-host runtime slot before switching production.
- Preserve the legacy runtime, database, camera configuration, recordings, backup evidence,
  nginx configuration, and Viewer 1.0 launch assets until the 2.0 acceptance window passes.
- Do not perform the merge, data import, service stop/start, nginx switch, Viewer reconfigure,
  or deletion during strategy work; all production changes require a later approved window.

## Plan

- [x] Confirm `main`/`camstation2-initial` ancestry, divergent commits, changed surfaces, and
      dry-run merge-conflict risk.
- [x] Compare 1.x and 2.0 camera/settings/recording/backup/Viewer schemas and identify the
      required idempotent import contract.
- [x] Design isolated same-host staging paths, ports, DB, services, secrets, and resource limits.
- [x] Define preflight, freeze/import, service and nginx switch, Viewer transition, soak,
      rollback triggers, and post-cutover retention with verifiable gates.
- [x] Publish and validate a Korean strategy document with topology, data mapping, timeline,
      command context, Evidence → Finding → Path, and explicit operator decisions.

## Acceptance criteria

- [x] Quantify branch ancestry, divergent commits, changed surfaces, and merge-conflict risk.
- [x] Map legacy camera/settings/recording/backup/Viewer data to the 2.0 source-of-truth model,
      identifying fields that require a purpose-built idempotent importer.
- [x] Define a same-host deployment that cannot collide with the active 1.x runtime.
- [x] Define go/no-go and rollback evidence for server, cameras, recordings, backup, and Viewer.
- [x] Validate document links, commands, evidence references, sensitive-data handling, and diff.

## Review

- `main` and `camstation2-initial` have no merge base. Their tips contain 165 and 195 unique
  commits, their trees contain 142 and 500 paths with only four common paths, and the direct
  tree comparison spans 631 changed files. A normal content merge is therefore rejected.
- A temporary-clone simulation proved the documented two-parent replacement merge produces
  a result tree exactly equal to the current 2.0 tree while retaining both branch parents.
- The legacy and 2.0 schemas require a purpose-built importer. The strategy preserves the
  stable camera key, `9/8/1` activation invariant, role streams, layouts, secrets, settings,
  and explicitly separates recording archive and Viewer registry decisions.
- Because both generations own the same fixed go2rtc ports, the design stages code/data in
  separate slots but uses a single-active runtime handoff. It retains the 1.x slot and Viewer
  1.0 as rollback assets and writes new 2.0 recordings to a separate root.
- The formal strategy is published at
  `docs/2026-08-09_cctv-1x-to-2x-production-cutover-strategy.md` and linked from `README.md`.
  All 14 relative links resolve; heading depth, placeholder scan, raw credential URL scan, and
  whitespace checks pass.
- `scripts/test-dev.sh` passed the Go suite, all 52 Web tests, and all 23 Viewer tests. This is
  recorded only as a source baseline; production build, service, camera, backup, and Windows
  GUI evidence remain execution gates.
- No production service, database, camera, nginx, Viewer configuration, Git branch, tag, or
  remote was changed during this strategy task.

---

# 2026-08-09 server-first cutover with transitional Viewer 1.0 shell

## Scope and decision to evaluate

- Evaluate the operator-proposed sequence: stage/install 2.0 on `cctv`, import camera data,
  stop the 1.x server and activate 2.0, stabilize the server while leaving the WebView-based
  CamViewer 1.0 client installed, then activate Viewer 2.0 and retire Viewer 1.0 separately.
- Distinguish leaving the 1.x Windows shell installed from leaving the 1.x server runtime
  active. Only the former is under consideration during the 2.0 server phase.
- Do not change the production server, nginx, NUC configuration, services, or Git branches
  while validating and documenting this refinement.

## Plan

- [x] Inspect the 1.x Viewer startup URL, navigation rules, heartbeat/control behavior, and
      the 2.0 SPA routes to determine whether the old shell can display the new `/live` page.
- [x] Define the smallest compatibility bridge and its limits; do not assume API compatibility
      merely because both clients embed a browser.
- [x] Revise the production strategy to make server transition and client transition explicit
      release phases with independent go/no-go and rollback points.
- [x] Validate the revised document, links, sensitive-data handling, and source-backed claims.

## Acceptance criteria

- [x] State whether CamViewer 1.0 works unchanged, works only through a bounded redirect, or
      cannot be used against the 2.0 server, with exact source evidence.
- [x] Keep the 1.x server stopped while 2.0 is active and preserve it only as rollback state.
- [x] Preserve Viewer 1.0 installation/startup assets until Viewer 2.0 passes interactive
      console, telemetry, auto-start, and reconnect acceptance.
- [x] Publish an unambiguous revised sequence and rollback boundary in the strategy document.

## Review

- CamViewer 1.0.4 hard-codes `/new?viewer=1`; the 2.0 SPA's intended Viewer route is
  `/live?viewer=1`, while an unhandled `/new` falls through the SPA wildcard to `/`. The old
  shell therefore is not unchanged-compatible but can follow a bounded same-origin redirect
  because its navigation guard does not restrict `/live`.
- The revised strategy requires a tested server route for `/new?viewer=1` → `/live?viewer=1`
  before production. In that mode the old Electron window is display-only: its preload lacks
  the 2.0 `camstationViewer` bridge, and the old heartbeat/command code came from the retired
  1.x web UI, so 2.0 telemetry, control, and managed updates are not acceptance signals.
- Server and client now have independent gates. Server completion requires 60 minutes, one
  30-minute rollover, one end-to-end backup, camera `9/8/1`, eight live streams, and manual
  eight-camera display through the transitional shell before Viewer 2.0 is launched.
- Server-gate failure restores the preserved 1.x runtime; Viewer-2.0-only failure leaves the
  healthy 2.0 server active and relaunches CamViewer 1.0 through the bridge.
- The strategy document passed local-link, code-fence, heading-depth, placeholder,
  sensitive-pattern, README-reference, whitespace, and source-claim checks. No production
  service, database, camera, nginx, Viewer setting, process, or Git branch was changed.

---

# 2026-08-09 accepted display-only server-first cutover preparation

## Scope and decisions

- Treat the operator's server-first sequence as approved: during server acceptance,
  CamViewer 1.0 only needs to render the 2.0 live video workspace; Viewer management,
  telemetry, remote control, and managed updates are deferred to the later Viewer 2.0 phase.
- Prepare the 2.0 source tree, tests, migration/deployment readiness record, and executable
  preflight artifacts. Do not merge branches or change the production `cctv` runtime, nginx,
  database, cameras, NUC settings, or processes in this preparation task.
- Preserve all unrelated dirty-worktree changes and limit implementation to files confirmed
  clean or to new preparation artifacts.

## Plan

- [x] Re-review the accepted sequence against current 1.x/2.0 Viewer routes, daemon routing,
      camera schemas, lifecycle scripts, implementation status, and existing strategy.
- [x] Implement and test the bounded legacy entry route so only `GET /new?viewer=1` reaches
      `/live?viewer=1`, without exposing or reviving the legacy `/new` application.
- [x] Audit camera-import, production service, configuration, backup, archive, and rollback
      preparation; classify each as ready, implementable now, or requiring an operator/runtime gate.
- [x] Implement the safest self-contained preparation slice(s) needed before a production
      rehearsal, with dry-run behavior and secret-safe evidence.
- [x] Update the cutover document and readiness record to reflect the accepted display-only
      criterion, exact commands, remaining gates, and next authorized production action.
- [x] Run focused tests followed by the repository's full validation path; inspect generated
      assets, diffs, links, and sensitive-pattern output before declaring preparation complete.

## Acceptance criteria

- [x] The compatibility route has automated positive and negative tests and preserves the
      existing SPA/API behavior outside the exact legacy Viewer request.
- [x] Preparation can be executed without modifying the active 1.x database or leaking camera,
      backup, Viewer, or SSH credentials into Git, logs, manifests, or test output.
- [x] The readiness review gives every pre-cutover dependency an owner, proof command, current
      state, and explicit Go/No-Go result.
- [x] Server and client cutovers remain independent: missing legacy-shell telemetry is accepted,
      but eight-camera visual playback remains a production gate.
- [x] No production or NUC state changes occur during preparation, and unrelated user changes
      remain intact.

## Review

- The accepted server-first/display-only transition is implemented as an exact compatibility
  route. Viewer 1.0 management telemetry remains deferred; visible eight-camera playback is
  still a production gate.
- Bounded Hangul camera keys now survive API and store persistence. A new maintenance binary
  performs SQLite online snapshot, schema/quick-check, redacted dry-run, fresh atomic import,
  repeat-safe verification, and strict `9/8/1` plus layout/settings expectation checks.
- Fresh 2.0 DBs no longer inherit the development backup remote. Backup starts disabled with
  an empty target and `protectUnbacked=true`; enabling it requires an explicit target.
- Hardened systemd/nginx packaging and root-guarded release, state, preflight, switch, and
  rollback helpers were added. They use exact services and refuse unresolved paths, hashes,
  symlinks, port collisions, active-generation overlap, and unknown nginx include state.
- Read-only production review confirmed all five legacy services active, go2rtc media ports
  occupied, port 18080 free, required runtime binaries present, and no SQLite CLI. It also found
  two existing nginx server location sets that must be converted to preserved legacy includes
  during an approved staging step.
- Verification passed: Go full suite and vet, Web 52 tests/lint/build, Viewer 23 tests/build,
  daemon and migrator builds, production shell policy, and diff whitespace checks.
- Final documentation audit passed 49 local-link checks, 61 Markdown code-fence checks,
  cutover-scoped credential-pattern and trailing-whitespace scans. The readiness report
  SHA-256 is `e892417451e0250ed8e78a40bf9140a1a05e94b69b4e88ecdb0d358c13b89d3a`.
- No production service, DB, nginx file, camera, NUC process/configuration, Git branch, tag, or
  remote was changed. Operational execution remains No-Go until the readiness report's R1 and
  R4-R10 gates pass.

---

# 2026-08-09 inactive production staging and camera-state preparation

## Scope and decisions

- Treat the operator's latest instruction as approval to mutate the production `cctv` host only
  for reversible, inactive 2.0 staging: immutable release files, a disabled systemd unit,
  root-only configuration, an online legacy snapshot, and an imported 2.0 database.
- Keep nginx's active configuration and all 1.x units unchanged and active. Do not start 2.0,
  stop 1.x, alter cameras, enable backup, touch the NUC, or execute the final handoff.
- Build from an isolated clean source candidate; do not package the mixed dirty worktree or
  overwrite unrelated user changes.

## Plan

- [x] Re-resolve the exact legacy DB, service, nginx, filesystem, account, and port state using
      read-only production checks; stop if it differs from the reviewed topology.
- [x] Create a clean, reproducible 2.0 release candidate containing the approved cutover
      preparation changes and record its commit/tree and complete file hashes.
- [x] Run the full source validation path on that candidate before uploading any artifact.
- [x] Install the immutable release, service account, unit, environment, runtime directories,
      and inactive nginx candidate files without switching the active include.
- [x] Create an online legacy snapshot and import the 9-camera/8-enabled camera graph, layout,
      and 30/30/700 recording settings into an inactive 2.0 DB.
- [x] Prove source/snapshot/target parity, file ownership and modes, 2.0 inactive state, all
      legacy units active, legacy media ports owned, port 18080 free, and current service health.
- [x] Update the readiness report and this review with exact evidence, remaining one-window
      handoff work, rollback state, and any blocked gate.

## Acceptance criteria

- [x] The running 1.x server and Viewer-facing endpoint remain continuously available throughout
      staging, with no legacy service restart; the only nginx reload preserves the exact legacy
      routes and passes before/after health checks.
- [x] The staged release is immutable, hash-verified, and not built from an unreviewed dirty tree.
- [x] The imported target DB passes canonical verification against an immutable online snapshot
      and contains no enabled backup destination by default.
- [x] The final handoff is reduced to maintenance include, exact legacy stop, port release, 2.0
      start/health checks, active include switch, and field video verification.

## Review

- Installed immutable release `2.0.0-rc.20260809.5` from clean two-parent replacement commit
  `db09c6c9d142e9c6d1a360b0b4a59ac098fe8283`; the remote `main` branch was not changed.
- Full Go tests/vet, Web 52 tests/lint/build, Viewer 23 tests/build, release hash verification,
  production shell policy, and exact real-DB read-only inspection passed before installation.
- Production inspection found media on the large dedicated filesystem rather than the root
  filesystem. Packaging now keeps state under its protected root and recording/temp together on
  the media filesystem; preflight enforces ownership, free space, and same-filesystem finalization.
- Three legacy sub-stream values were local go2rtc ffmpeg recipes, not camera URLs. The importer
  now maps only that exact loopback/self-key H.264 form to a recording-backed live output and
  rejects other producer expressions. Synthetic regression and actual production inspection pass.
- Online snapshot and target verification passed at canonical fingerprint
  `636af019dce2debb7c30e54b49966be9a1afe2679d3f0a30c0d0fa305bc80874`: cameras `9/8/1`,
  sub entries 9, layout 1/8, settings 30/30/700, blockers 0, backup disabled and protected.
- Nginx preparation verified the original site hash, preserved both original files, reduced two
  wildcard-loaded server blocks to one, and reloaded with the active symlink still targeting the
  legacy routes. Legacy backend/backup/go2rtc PIDs and restart counts remained unchanged.
- Final server preflight returned `PREFLIGHT_READY`; 2.0 remains inactive/disabled, port 18080 is
  free, all 1.x units are active, health reports eight online cameras, NUC traffic continues, and
  the root-only switch approval remains `NO`.
- Boot ownership is guarded: preflight requires legacy active/enabled and 2.0 inactive/disabled;
  switch and both rollback paths transfer systemd enablement with runtime ownership.
- The complete source history is preserved as a verified root-only Git bundle with SHA-256
  `bac75de5224bd55c3128b5cd2326d757274b601d1af8c63a58aef6e146c323db`.

---

# 2026-08-09 isolated home-camera 2.0 canary

> Superseded before implementation: the user stopped the host-port-plumbing approach and asked
> to evaluate containerizing the complete 2.0 runtime. No canary runtime was started and no
> production service was changed by this plan.

## Scope and decisions

- Keep the complete 1.x production generation active and unchanged while running a separate 2.0
  canary generation with only stable keys prefixed `집-` enabled.
- Never point the canary at the fire-station cameras or modify the verified final-cutover DB.
- Give the canary distinct HTTP, go2rtc API, RTSP, WebRTC, state, media, service, and ingress
  boundaries; stop immediately on any port collision, camera-session impact, or legacy health loss.
- Treat canary success as evidence for the home-camera subset only. It does not waive the final
  fire-station, full recording, backup, Viewer, or rollback gates.

## Plan

- [ ] Inventory every hard-coded daemon/go2rtc/recorder port dependency and design explicit
      production defaults plus isolated canary overrides.
- [ ] Implement configuration plumbing and tests proving two independent port sets without
      changing existing defaults or exposing go2rtc API/RTSP listeners publicly.
- [ ] Create a fresh canary DB from the verified target, disable every non-`집-` camera, and prove
      exactly three enabled home cameras with the final-cutover DB unchanged.
- [ ] Package and install an inactive canary unit, environment, media/state roots, and bounded
      LAN test ingress while preserving the active legacy nginx route.
- [ ] Start the canary, prove 1.x continuity, three live home-camera outputs, recorder growth and
      segment playback, then capture logs/status without raw camera URLs or credentials.
- [ ] Stop or retain the canary only according to verified resource/session impact, and document
      the exact pre-cutover cleanup and remaining gates.

## Acceptance criteria

- [ ] All 1.x services remain active and the eight-camera production status stays healthy.
- [ ] Canary APIs and runtime expose exactly three enabled `집-` cameras and no non-home producer.
- [ ] The three canary videos render through the isolated ingress and their recorder files grow.
- [ ] Canary shutdown releases every alternate port and leaves the final-cutover DB/fingerprint
      and legacy boot ownership unchanged.

# 2026-08-09 containerized home-camera 2.0 canary evaluation

## Scope and decisions

- Stop the host-native parallel-runtime implementation before changing source or production.
- Evaluate one self-contained CamStation 2.0 application image containing `camstationd`, go2rtc,
  FFmpeg/ffprobe, and rclone. Container-internal ports may repeat safely under bridge networking;
  only host-published ports must be unique. Keep the existing production nginx and 1.x units
  outside the canary unless an internal nginx is justified independently.
- Preserve the same fail-closed camera boundary: only stable keys prefixed `집-` may be enabled in
  the trial DB, and the verified final-cutover DB must remain immutable. The active 1.x go2rtc YAML
  is the sole authority for camera keys, enabled state, and main/sub producer definitions; do not
  source canary camera data from the 1.x SQLite DB.
- Treat Docker port/volume isolation as runtime isolation only; it does not make duplicate camera
  sessions safe for the fire-station cameras.

## Plan

- [x] Verify Docker/Compose availability, architecture, storage, listener, and firewall constraints
      on the production server without installing or changing anything.
- [x] Audit the daemon and bundled toolchain for container requirements, process supervision,
      signal handling, HTTP/MSE proxying, health checks, persistent volumes, and permissions.
- [x] Compare host-native and containerized cutover/rollback risks and select the simplest safe
      production topology.
- [x] If approved after review, implement and test the image/Compose definition locally before any
      production deployment.
- [x] Build a separate `집-`-only trial DB directly from the active go2rtc YAML and run a bounded
      container canary while proving 1.x continuity and zero non-home producers. Do not import
      1.x DB camera rows, ONVIF fields, layouts, jobs, backup, or alert state into the trial.
- [x] Document image provenance, volume backup, upgrade, rollback, log, health, and cleanup commands.
- [x] After every gate passes, keep the canary running on HTTP `10.0.0.26:18081` for the operator's
      interactive check; stop it automatically if any safety or continuity gate fails.

## Acceptance criteria

- [x] The recommendation is based on the actual server and repository, not only a conceptual Docker
      design.
- [x] No production package, service, firewall, port, DB, or camera session changes during review.
- [x] The proposed topology supports multiple isolated 2.0 instances without shared writable state
      or host-port collisions.
- [x] Only `10.0.0.26:18081/tcp` is published for the retained canary.

## Review

- Image `camstation:2.0.0-rc.20260809.7-canary` is running healthy with restart count 0 and
  manual restart policy. Its final metadata exposes only `18080/tcp`; Compose publishes it only as
  `10.0.0.26:18081/tcp`.
- The YAML-only manifest selected exactly three home main/sub pairs. The generated public/private
  graph contains no fire-station or goat-farm key, and the original 1.0 YAML hash is unchanged.
- Three live H.264 streams, three recording workers, finalized 60-second MP4 playback, browser MSE,
  logs, resource limits, file ownership/modes, and legacy continuity all passed.
- Full procedures are in `docs/2026-08-09_camstation2-docker-canary-operations.md`.
  Its final SHA-256 is `dc409b1ebe8aa3fa244be2d787115ada4abfadcefaeb08f3d44ef41c6c43cb24`.

# 2026-08-09 2.0 dedicated `/viewer` parity

## Scope and decisions

- Treat the operator-designated 1.0 `https://cctv2.nuc.hmini.me/viewer` surface as the
  compatibility reference: a full-viewport, read-only camera layout with no management console,
  navigation, timeline, editing controls, or side panels.
- Add a distinct 2.0 `/viewer` route instead of reusing responsive `/live` or interpreting
  `?viewer=1` as equivalent behavior.
- Reuse 2.0's redacted camera/layout APIs and same-origin HTTP MSE proxy. The current canary must
  still render only `집-마당`, `집-창고1`, and `집-창고2`; fire-station and goat-farm cameras remain
  absent by construction.
- Keep the initial parity boundary narrow: full-screen grid, online indicator/name, layout-derived
  geometry, tile focus/return, and reliable mobile playback. Do not copy 1.0 management or PWA
  features that are not present on the `/viewer` reference surface.

## Plan

- [x] Inspect the real 1.0 `/viewer` at an iPhone-sized viewport and capture DOM, requests,
      playback state, and screenshot evidence.
- [x] Compare the same viewport against the running 2.0 `/live` canary and identify route, layout,
      console-chrome, overflow, and playback differences.
- [x] Implement an isolated `/viewer` route and tests without changing `/` or `/live` behavior.
- [x] Run web tests/lint/build and Go tests/build, then build a new immutable Docker image.
- [x] Replace only the 2.0 canary container and prove `/viewer` renders and plays all three home
      streams on mobile while `/live`, APIs, recorders, port isolation, and 1.0 continuity remain
      healthy.
- [x] Update the canary operations document and retain the validated container for operator access.

## Acceptance criteria

- [x] `GET /viewer` is a directly addressable SPA route and browser reload does not redirect it.
- [x] At a 393x852 mobile viewport, `/viewer` has no console navigation, settings controls,
      timeline, horizontal page overflow, or non-home camera.
- [x] All three `<video>` elements reach a playable state with advancing time through MSE.
- [x] A tile tap opens a single-camera focus view and the explicit close action restores the grid.
- [x] The retained canary remains healthy with only `10.0.0.26:18081/tcp` published, and legacy
      1.0 service PIDs/restart counts plus the source YAML hash remain unchanged.

## Review

- The production 1.0 reference rendered eight simultaneous MSE videos with only the saved camera
  layout. The former 2.0 `/live` mobile rendering retained management chrome, overflowed, and did
  not satisfy that contract.
- The new top-level `/viewer` renders only the read-only saved layout and uses MSE-first playback.
  At 393x852 all three home videos reached readyState 4 with advancing time; focus rendered one
  1280x720 stream and explicit close restored three playing tiles.
- Direct reload stayed on `/viewer`, document dimensions exactly matched the viewport, browser
  errors were empty, and the container/legacy final audit passed.

# 2026-08-09 eight-camera PID capacity correction

## Scope and decisions

- Correct the container task limit using the real final fleet size of eight cameras, not only the
  three-camera canary subset.
- Keep the current positive `집-` canary allowlist unchanged. This correction changes runtime
  capacity only; it does not contact fire-station or goat-farm cameras.
- Raise `pids_limit` from 256 to 1024. Three-camera live startup measured 226 current tasks, a peak
  of 256, and 343 cgroup PID-limit hits; 512 would be too close to the linear eight-camera estimate
  plus focus/reconnect headroom.

## Plan

- [x] Correlate connection retries with per-stream consumers, process/thread counts, and cgroup
      `pids.current`, `pids.peak`, and `pids.events` rather than relying on quiet application logs.
- [x] Update and validate the repository and root-owned production Compose definitions with an
      exact reversible 256-to-1024 change.
- [x] Recreate only `camstation2-canary` and prove health, unchanged image/state/port policy,
      `pids.max=1024`, and zero new cgroup PID-limit hits during reconnect.
- [x] Prove three-page/three-camera Viewer stability, recorder continuity, and unchanged 1.0
      service PID/restart and source-YAML baselines.
- [x] Update the operations report with the measured root cause, new capacity, and validation.

## Acceptance criteria

- [x] The new container reports `pids.max=1024`, `pids.events max 0`, no OOM, and no CPU throttling.
- [x] Three open Viewer pages settle at exactly three consumers per home live stream without a
      growing excess count.
- [x] The container stays healthy with three streaming cameras and three running recorders.
- [x] Only `10.0.0.26:18081/tcp` remains published; every 1.0 continuity baseline remains unchanged.

## Review

- Root cause was cgroup task exhaustion, not an application-level socket leak: before correction,
  the three-camera live workload reached `pids.peak=256` and recorded 343 PID-limit hits while
  memory events and CPU throttling remained zero.
- Repository and production Compose now use `pids_limit: 1024`. Only `camstation2-canary` was
  recreated; the immutable image ID, state/media mounts, HTTP-only binding, and restart policy
  remained unchanged. The previous production Compose was preserved in a root-only backup.
- Three mobile Viewer pages sustained nine MSE videos at readyState 4 with advancing playback for
  about 160 seconds. Each home live stream settled at exactly three viewers; after closing the
  browser, all viewer and consumer counts returned immediately to zero.
- During load, the container stayed healthy with three streaming cameras and three running
  recorders. It measured 224 PIDs with peak 230 against max 1024, zero PID-limit hits, zero OOMs,
  and zero CPU throttling; error/fatal/panic and task-exhaustion log signatures were also zero.
- The five 1.0 service PID/restart baselines, 8/8 enabled-online camera count, and source go2rtc
  YAML hash remained unchanged. Fire-station cameras and `염소장` remain absent from this canary.
- Local and production Compose validation passed with `pids_limit: 1024`. The closing audit passed
  five relative links, balanced code fences, placeholder, sensitive-pattern, raw-runtime-path,
  and `git diff --check` validation. The operations document SHA-256 is
  `27ae3b57c376a9eea4fcc803442280506f9062547a0eca6373f5fde2fee355fd`.

---

# 2026-08-09 NUC Viewer 2.0 installation verification

## Scope and decisions

- Inspect the already installed CamStation Viewer 2.0 on monitor PC `192.168.0.13` through the
  validated `CamStationOps` administrator SSH path.
- Keep CamViewer 1.0 and the interactive monitoring session untouched. This is a read-only
  installation and launch-readiness audit; do not start Viewer 2.0, change its endpoint, restart
  services, run the updater, uninstall either client, or modify scheduled tasks.
- Distinguish MSI registration from a usable client: verify package identity, executable hashes
  and signatures, service/task registration, configured endpoint, process/session state, local
  IPC/listeners, recent secret-safe logs, and reachability to the retained Docker canary.

## Plan

- [x] Capture Windows identity/session, installed-package, product-code, install-root, version,
      publisher, uninstall-command, and Authenticode/hash evidence without dumping registry secrets.
- [x] Verify Viewer service definition, account, automatic start/recovery, executable path,
      scheduled tasks, interactive processes, and coexistence with CamViewer 1.0.
- [x] Inspect only known CamStation configuration and log locations; report endpoint, release
      identity, permissions, IPC/listeners, and bounded recent errors with secrets redacted.
- [x] Test NUC-to-canary HTTP health and `/viewer` reachability without launching or reconfiguring
      the GUI client.
- [x] Classify installation as complete, staged-but-not-ready, or broken; update the maintenance
      report, evidence chain, lessons if a correction is learned, and final validation record.

## Acceptance criteria

- [x] Every MSI-owned Viewer 2.0 payload resolves to an existing expected file at its recorded
      size; key executable versions, hashes, signature state, owners, and ACLs are recorded without
      exposing credentials.
- [x] Service and startup registrations target the same installed release and have a viable automatic
      startup path for the real interactive user.
- [x] The configured server endpoint is identified and tested from NUC; its mismatch with
      `http://10.0.0.26:18081` is reported as a blocker rather than silently changed.
- [x] CamViewer 1.0 remains running and no process, service, task, file, registry value, or network
      listener is changed during verification.

## Review

- Windows Installer reports CamStation Viewer 2.0.20 in installed state 5. Its cached MSI exists;
  all 76 MSI-owned files are present at their expected sizes, with zero missing or mismatched
  payloads. The install root has two additional, inactive files left by the previous bootstrap
  generation; they are not evidence of a failed current MSI installation.
- The direct LocalSystem service is Running/Auto with exit code 0 and SCM restart recovery. The
  current MSI-owned HKLM Run value and common shortcuts point directly to the installed Viewer.
  Zero Viewer scheduled tasks is expected for this package generation, not a missing component.
- Program Files, ProgramData, and the 64-bit Viewer registry key deny ordinary-user writes. The
  cached MSI, setup wrapper, Viewer executable, and service executable have no embedded
  Authenticode signature, so this remains a development/staging package rather than an approved
  signed production release. No server release metadata is currently published for independent
  hash/provenance comparison.
- The installation is complete but cutover is not configured: endpoint `.172:18080` is offline,
  auto-start is false, no interactive 2.0 process exists, and the retained canary has no Viewer
  registration. NUC reaches `http://10.0.0.26:18081` successfully.
- Final non-mutating verification found six CamViewer 1.0 processes in console session 1, zero
  interactive Viewer 2.0 processes, and the same Viewer service PID. No process, service, task,
  file, registry value, listener, endpoint, or auto-start setting was changed.
- Closing validation passed relative-link, Evidence-reference, code-fence, heading-depth,
  placeholder, sensitive-pattern, trailing-whitespace, and `git diff --check` checks. A changing
  health `startedAt` value was investigated rather than accepted as a restart: Docker remained
  healthy with restart count 0, and source confirms that field is request time rather than uptime.
- Updated Korean maintenance report SHA-256:
  `08844411762fa3c3cb9c63d53e745f670c9345f4d9b520c49b6e2d994c3109f4`.

---

# 2026-08-09 NUC Viewer 2.0 latest-version reinstall

## Scope and decisions

- Treat the operator-reported client bug as a functional defect independent of the previously
  validated MSI file placement. Reinstalling the same artifact is not acceptable evidence of a fix.
- Define “latest” by comparing the checked-out Viewer source and commit with the configured upstream,
  then build a new version greater than the installed MSI 2.0.20 with a deterministic hash.
- Keep CamViewer 1.0 running and preserve the current Viewer 2.0 configuration, stable identity,
  auto-start choice, installed MSI cache/hash, and rollback evidence. Do not reboot the NUC, retire
  1.0, or change the Viewer endpoint during this reinstall.
- This NUC is an internal canary workstation. If no production signing identity is available, an
  explicitly marked unsigned development MSI may be installed only after source tests, package
  policy checks, hash verification on both hosts, and a clear final status record.
- The NUC is an installation/maintenance target, not an MSI build host. Build and sign future MSI
  packages on a dedicated Windows VM or CI runner; transfer only the finished, verified package.
- The operator supplied a first-run screenshot and exact defect: while entering the server address,
  the field loses focus and cannot be edited. Treat delayed setup-state hydration and repeated native
  window `show()` calls as separate focus-stealing paths and cover both with regression tests.

## Plan

- [x] Reproduce the reported focus loss in tests: preserve an active/dirty server field across
      delayed setup-state hydration and prohibit a second native focus acquisition after load.
- [x] Implement the smallest renderer/window-lifecycle fix and verify keyboard entry, tab order,
      retry behavior, validation errors, and setup-state preservation.
- [x] Compare local/upstream source, Viewer package metadata, build scripts, prior artifacts, and
      installed 2.0.20 to select a unique higher release version without discarding dirty worktree changes.
- [ ] Run the Viewer/service/MSI tests, build the Electron payload and service, then produce and
      inspect the new MSI/setup artifact with hashes and expected version/signature state.
- [ ] Capture a secret-safe NUC rollback baseline and preserve the cached 2.0.20 package/configuration;
      verify disk space and keep all six CamViewer 1.0 processes running.
- [ ] Transfer to a restricted staging directory, verify the remote hash, execute a quiet no-reboot
      MSI upgrade with a bounded log, and stop on any non-success installer code.
- [ ] Verify new product version/files/service/recovery/ACL/startup/config preservation, confirm 1.0
      continuity, and inspect bounded install/service logs for errors.
- [ ] Update Evidence → Finding → Path, the Korean maintenance report, lessons, and closing checks;
      retain or remove the staging package according to the documented rollback decision.

## Acceptance criteria

- [x] A delayed initial status or retry response never overwrites an active/dirty server address,
      the initial untouched form focuses that field once, and the native window is shown only once.
- [ ] The installed MSI version is greater than 2.0.20 and its key hashes match the locally built,
      remotely transferred artifact; same-version reinstall is not accepted.
- [ ] MSI completes with success or reboot-required-success only, but the workflow never initiates
      a reboot. Product state is installed, service is Running/Auto with exit code 0, and package
      ownership has no missing or size-mismatched file.
- [ ] Configuration schema, endpoint host/port, stable client-identity presence, display-name
      presence, and `autoStart=false` are preserved without printing secret values.
- [ ] CamViewer 1.0 remains active in session 1 throughout; no 2.0 interactive process is launched,
      no endpoint is changed, and the Docker canary remains healthy with no Viewer registration.
- [ ] Rollback material and an operator-ready next step for interactive bug verification are
      documented, and all relevant tests, links, evidence references, and whitespace checks pass.

## Review — stopped before MSI production

- The focus fix passed all 25 Viewer tests and an automated browser scenario in which a delayed
  setup response arrived while the server-address field was active; both the value and focus were
  preserved. No MSI containing this fix was produced or installed.
- WiX 6.0.2 rejected the Linux build host before producing an MSI. A portable Windows .NET SDK was
  then staged on NUC solely to test the Windows build path, but the operator stopped that approach
  before restore/build completed and clarified that NUC must remain an install/maintenance target.
- The exact NUC build stage `C:\ProgramData\CamStation\Maintenance\Viewer-2.0.21-build`, including
  the portable SDK and build inputs, was removed. No scoped build process remained.
- Post-cleanup verification: installed Viewer remains 2.0.20 in Windows Installer state 5;
  `CamStationViewerService` is Running/Auto; six CamViewer 1.0 processes remain active; no
  CamStationViewer 2.0 interactive process is running. The restricted maintenance root was retained
  for receiving verified MSI packages in future.
- Resume only after a dedicated Windows VM/CI runner is designated. That runner builds/signs and
  validates the MSI; NUC receives only the final hash-verified MSI for install/repair/uninstall.

---

# 2026-08-09 local Viewer MSI build path

## Scope and decisions

- Restore the missing repository-owned Windows build entry point for the existing WiX 6.0.2 MSI.
- The build runs only on a dedicated x64 Windows developer machine or VM. The current Linux host
  may run source/policy tests but must fail before pretending to produce an MSI; the NUC remains an
  install/repair/uninstall target and never receives build tools.
- Produce an explicitly unsigned development package only when `-UnsignedDevelopment` is supplied.
  Do not imply production signing when no signing identity or signing script is configured.
- Build in an ignored, version-specific workspace and never rewrite the tracked
  `installer/Files.generated.wxs` or tracked service executable.

## Plan

- [x] Add failing source-policy tests for platform gating, tool/version checks, deterministic build
      order, isolated generated inputs, locked WiX restore, explicit unsigned policy, MSI property
      verification, and SHA-256 build metadata.
- [x] Implement `scripts/build-viewer-msi.ps1` with one public command, actionable prerequisite
      failures, cleanup limited to its exact generated workspace, and no NUC-specific paths.
- [x] Add a Windows-local quick-start and troubleshooting guide under `installer/README.md`, linked
      to the existing installer design and explicitly separating build and install hosts.
- [x] Run focused and full Viewer tests, TypeScript/package builds, PowerShell parser checks, and
      repository whitespace/sensitive-boundary checks on Linux.
- [ ] Record the remaining real-Windows gate: run the documented command on a dedicated Windows
      host and verify the resulting MSI version, file count, hash metadata, and signature state.

## Acceptance criteria

- [ ] One documented Windows command builds Electron, the versioned Go service, a fresh WiX file
      fragment, and `CamStationViewer.msi` without modifying tracked generated/binary inputs.
- [ ] Missing Windows, Node 22+, Go 1.25+, .NET SDK 8.x, or explicit unsigned/signing policy fails
      before an MSI is published, with an actionable error.
- [ ] The output directory contains the MSI, WiX symbols, and `build-metadata.json`; metadata records
      requested/product version, source commit/dirty flag, byte size, lowercase SHA-256, and
      `developmentUnsigned=true` without machine paths or secrets.
- [ ] MSI database inspection proves product name, version, and fixed UpgradeCode before success is
      reported; the artifact remains uninstalled during the build.
- [x] Linux validation passes, and documentation states honestly that actual MSI production still
      requires the dedicated Windows build-host gate.

## Review — build entry point prepared; Windows artifact gate open

- Added `scripts/build-viewer-msi.ps1` and `installer/README.md`. The documented command builds an
  explicitly unsigned development MSI on a dedicated x64 Windows machine or VM, publishes it under
  an ignored version directory, and never installs it or connects to NUC.
- Added four repository contract tests. The initial RED run failed because the entry point and guide
  did not exist; the completed implementation passes all four tests and the full 29-test Viewer suite.
- Linux-side validation passed: Viewer TypeScript build, Windows Electron packaging, Viewer-service
  Go tests, the official PowerShell 7.5 parser, the non-Windows fail-closed gate, a temporary x64 PE
  service cross-build, fresh WiX fragment generation, and tracked-input hash preservation.
- No MSI was produced. This host has Linux Docker only and no QEMU/KVM-backed Windows VM or Windows
  image. WiX's actual Windows Installer build and COM database inspection therefore remain unproved.
- Next gate: designate a dedicated Windows x64 developer machine/VM, run the documented 2.0.21
  command, and validate the MSI, WiX symbols, hash file, and metadata. NUC remains install,
  repair, and uninstall only.
- Recorded the boundary as E-019 → F-012 → P-004 and updated the Korean maintenance report. Final
  relative-link, Evidence-reference, code-fence, secret-boundary, ignored-output, protected-input,
  trailing-whitespace, and `git diff --check` validation passed. Current report SHA-256 is
  `57db8776a510d08aa4549d7d3659871868791135bc084c964cb9c02d29705fc2`.

---

# 2026-08-09 CamStation 2.0 recording cleanup

## Scope and safety boundary

- Target only the retained Docker 2.0 canary `camstation2-canary` on the verified `cctv` server.
  Preserve every legacy 1.0 recording, service, process, database, go2rtc configuration, and path.
- Resolve container mount sources, the 2.0 SQLite recording rows, finalized media files, and active
  temporary segments before deletion. Stop if any target path overlaps the 1.0 runtime.
- Delete only completed 2.0 recording rows/files through an application-supported path when
  available. Never unlink an active temp/open segment or use a broad recursive filesystem command.
- Record exact pre/post counts and bytes, whether the removed files are recoverable, and prove the
  2.0 live/recorder health plus the 1.0 service/PID baseline after cleanup.

## Plan

- [x] Capture the exact 2.0 container identity, mounts, recording settings, DB/file inventory, and
      legacy 1.0 continuity baseline without printing camera URLs or credentials.
- [x] Inspect the 2.0 recording deletion/cleanup implementation and select the narrow supported
      method that keeps DB and filesystem state consistent.
- [x] Delete all finalized 2.0 recording media requested by the operator while excluding active
      temp/open segments and every legacy 1.0 path.
- [x] Verify zero targeted finalized rows/files remain, no orphan metadata is introduced, current
      2.0 recording continues safely, and all 1.0 services/processes remain unchanged.
- [x] Update the operations evidence/report, lessons, and this review with the exact deletion result
      and recoverability statement; run closing document and diff checks.

## Acceptance criteria

- [x] The deletion scope is proven to be 2.0-only by container mount, canonical path, and DB row.
- [x] Every deleted item was finalized and inactive at deletion time; no open/temp segment is removed.
- [x] Post-cleanup DB/file counts agree, and any new post-cleanup segment is healthy and playable.
- [x] Legacy 1.0 service PIDs/restart counts and recording activity remain unchanged.
- [x] The final report states exact removed file/row count and bytes and whether recovery is possible.

## Review

- The exact target was `camstation2-canary`, with state at `/var/lib/camstation2-canary/data` and
  media at `/mnt/hdd/camstation2-canary`. No legacy path overlapped the deletion scope.
- Preflight proved 1,623 `ready` rows exactly matched 1,623 MP4 files and 9,193,448,264 bytes, with
  zero unsafe paths, missing files, size mismatches, or extra files. The latest file from each of
  the three cameras passed ffprobe as an approximately 60-second H.264/AAC MP4.
- Used only `DELETE /api/recordings/segments/{id}` for guarded, exact finalized-ID snapshots. The
  continuously running recorders finalized nine more files during the first sweep; two guarded
  follow-up sweeps removed those too. Total: 1,632 files and 9,245,386,547 bytes.
- Final checkpoint: zero `ready` rows, zero completed recording files, zero `.deleting-*` files,
  three active temp segments, three running recorder workers, three streaming home cameras, and a
  healthy container with restart 0/no OOM. A ten-second sample proved all three active files grew.
- The five 1.0 units retained MainPIDs `248/326/247/396/246`, active/running state, and restart 0;
  legacy health remained `ok`. A closing ten-second sample found all eight legacy recorder MP4
  files on stable inodes and all eight grew. No 1.0 file, database, configuration, service, or
  process changed.
- All deleted rows remain audit tombstones with `backup_state=pending` and no `backed_up_at`. No
  trash/quarantine copy exists, so CamStation cannot restore the deleted recordings. Recording is
  still enabled and will create new one-minute files after the zero-file checkpoint.
- Evidence chain: E-020 → F-013 → P-005.
- Closing validation passed 54 relative links across ten task/report/evidence documents, balanced
  code fences, Evidence references, secret-pattern scan, and `git diff --check`. Final report
  SHA-256 is `38644e8ed25a58c00de2111b861cb816d3667e15db4fda0e371ac711aa159271`;
  canary operations SHA-256 is `b49583060fa4c21ef17e2f87b7c40fcaaf09e098082f8fc4c7253795288e98b3`.

---

# 2026-08-10 WinPC 10.0.0.30 developer access bootstrap

## Scope and decisions

- Provision only the operator-authorized Windows development PC `10.0.0.30`; do not scan the
  subnet or reuse the NUC monitoring-PC account/key.
- Create one dedicated `CamStationBuildOps` local administrator and one host-specific Ed25519 key.
  Keep the private key on this maintenance host and embed only the public key in the operator-run
  elevated PowerShell script.
- Install/enable Windows OpenSSH Server if absent and restrict the new inbound firewall rule to the
  verified maintenance source. Stage one does not edit `sshd_config`; exact-user/key-only hardening
  follows only after a pinned-host-key login succeeds.
- If the dedicated account or administrator key file already exists, stop and return the diagnostic
  instead of overwriting ownership. Do not enable or change RDP, WinRM, SMB, or unattended GUI.

## Plan

- [x] Resolve the exact route/source address and current TCP/22 state for `10.0.0.30` without broad discovery.
- [x] Generate a dedicated local Ed25519 maintenance key and record only its public fingerprint.
- [x] Implement one pasteable elevated PowerShell bootstrap with guarded account creation, the
      administrator-key ACL, an exact-source firewall rule, service start, and host-key output.
- [x] Validate PowerShell syntax and source-policy boundaries locally, then provide the exact
      operator command and expected success output.
- [x] After the operator runs it, pin the returned host key and prove public-key-only administrative
      SSH before using the PC for Viewer/MSI development.

## Acceptance criteria

- [x] No secret/private key is embedded in the script or printed.
- [x] Only `CamStationBuildOps` with the dedicated key can use the newly provisioned SSH path.
- [x] TCP/22 is allowed only from the verified maintenance source/network, not all remote addresses.
- [x] The first-stage script does not edit `sshd_config`, and it stops instead of replacing an
      existing dedicated account or administrator authorized-key file.
- [x] The operator result, independent host-key comparison, pinned-host-key login, administrator
      identity, password denial, and forwarding denial all succeed.

## Review — access established and hardened

- Exact route verification resolved `10.0.0.30` directly over `eth0` with maintenance source
  `10.0.0.16`. TCP/22 was not accepting connections before provisioning; the existing RDP path was
  observed but not modified.
- Generated a target-specific Ed25519 identity outside the repository. The private key is root-only
  (`0600`) and the public fingerprint is
  `SHA256:E1eBfRkf6wvFxi92ov8iD8xfq6XtssO+So2/sFzo5eE`.
- Prepared one block that can be pasted directly into elevated Windows PowerShell without file
  transfer. It verifies the exact target and elevation, installs OpenSSH Server if needed, creates
  `CamStationBuildOps`, installs only the dedicated public key with the Microsoft-required ACL,
  disables the broad default rule, and permits TCP/22 only from `10.0.0.16` to `10.0.0.30`.
- Stage one intentionally left `sshd_config` untouched. The operator returned ED25519 host
  fingerprint `SHA256:nJJI5bVKmwDuWfRqTpN1XUEd5ZkOZZM0cdmyZFIR40Y`; an independent key scan
  produced the same fingerprint before it was written to a dedicated known-hosts file.
- The paste block stops if the target account or administrator key file already exists. It does not
  modify RDP, WinRM, SMB, the NUC, or any CCTV runtime.
- Local validation passed: PowerShell 7.5 parser and two focused source-policy tests. Final paste
  block SHA-256 is `233d37aa70fd3d370c36ad9a9a11c32c86725cfb681c547a50d2cfe9e54e6845`.
- A strict fresh login proved identity `WIN11-DELL\CamStationBuildOps`, membership in
  `S-1-5-32-544`, and High integrity `S-1-16-12288`. The service is Running/Auto and its TCP listener
  is exactly `10.0.0.30:22`.
- Inspected the actual default configuration and then installed a validated managed policy with
  `ListenAddress 10.0.0.30`, `AllowUsers camstationbuildops@10.0.0.16`, public-key-only
  authentication, and forwarding/tunnel denial. The pre-change configuration is retained at
  `C:\ProgramData\ssh\sshd_config.pre-camstation-buildops-20260810-083727.bak`.
- Post-change tests proved: a fresh pinned-key login succeeds; password-only authentication returns
  `Permission denied (publickey)`; direct TCP forwarding returns `administratively prohibited`;
  `sshd -t` passes; and the only enabled inbound TCP/22 rule is `CamStation-BuildOps-SSH-In`, scoped
  from `10.0.0.16` to `10.0.0.30`.
- A read-only development inventory found x64 Windows PowerShell 5.1, 31.3 GB free on `C:`, and no
  Git, Node/npm, Go, .NET SDK, WiX, winget, SignTool, or MSBuild visible to the dedicated account.
  Toolchain installation remains a separate development-host setup action.
- RDP, WinRM, SMB, the interactive RDP PowerShell process, the NUC, and all CCTV runtime state were
  left unchanged. Temporary remote diagnostic PowerShell processes were identified by exact PID and
  removed; the existing RDP-session PowerShell process was preserved.

---

# 2026-08-10 WinPC Viewer development environment and first MSI build

## Scope and decisions

- Use only the authorized `WIN11-DELL` build host through the pinned `CamStationBuildOps` SSH path.
  Keep the monitoring NUC, CCTV server, RDP user's profile, and installed Viewer fleet outside this
  task.
- Install hash-verified portable tools below `C:\CamStationDev\tools` and persist PATH only for the
  dedicated build account. Do not install compilers into the RDP user's profile or machine-wide
  package managers.
- Treat `/workspace/CamStation` as the canonical source. Clone the exact base commit on Windows,
  apply the canonical tracked diff, and transfer only non-ignored untracked files; never transfer
  ignored runtime data, recordings, credentials, caches, `node_modules`, or prebuilt artifacts.
- Use the existing Viewer MSI build specification and locked WiX 6.0.2 restore. Produce version
  `2.0.21` only as an explicitly unsigned development artifact; do not install it on WIN11-DELL or
  the NUC during this task.

## Toolchain selection

- Node.js `22.23.2` Windows x64 ZIP, SHA-256
  `1177b4137ba5adaa56354ae40f1080c7450e8ae09cecb47da459d1c52ac99f97`.
- Go `1.25.12` Windows amd64 ZIP, SHA-256
  `d5dc82da351b00e5eedd04f41356817d674cc4308131f0f638a5b14c5c3af4cb`.
- .NET SDK `8.0.423` Windows x64 ZIP, release-metadata SHA-512
  `063fcc35c136277e6fd767c66579f3b92db22a078a7f0c7177b6af1edb2c9afae1613f6cfdc01acf7421773d9ac77f0ef73a7fd8b37f469e7e3505e5c1361ba0`.
- MinGit `2.55.0.3` x64 ZIP, SHA-256
  `f48e2d2dc74a24454adc6d8fd0ac25bf9c2386f19cfb06202b9465aaad4f9f05`.
- PowerShell `7.6.4` x64 ZIP, SHA-256
  `80832551c52809301e6071c8bac977beb5a2f1ec953eb4db9f94deb953333793`.
- Microsoft Visual C++ Redistributable x64 `14.51.36247.0`, official Microsoft-signed installer
  SHA-256 `843068991daaa1f73ad9f6239bce4d0f6a07a51f18c37ea2a867e9beca71295c`.
  This machine-wide runtime is the narrow exception to the portable-tool policy because Electron's
  native extract module imports `VCRUNTIME140.dll`; installation completed with exit code `0` and
  no restart requirement.

## Plan

- [x] Recheck disk, architecture, outbound HTTPS, exact tool URLs/hashes, and that the dedicated
      development root is absent before making changes.
- [x] Download each official archive to a bounded cache, verify its published digest before
      extraction, install it under the versioned tools root, and persist the dedicated user's PATH.
- [x] Verify tool versions from a fresh pinned SSH session and record a secret-free toolchain
      manifest with URLs, digests, versions, and installation time.
- [x] Clone base commit `1215d0518a8e74866a5d786af865fdb4967bb18d`, apply the canonical tracked
      diff/deletions, transfer non-ignored untracked files, and prove the Windows source status
      represents the current canonical workspace without ignored data.
- [x] Run Viewer dependency installation, all Viewer tests/build/package checks, and targeted Go
      service tests on Windows; fix any source or Windows-only issue in the canonical workspace and
      resynchronize it.
- [x] Run the locked unsigned `2.0.21` MSI build, inspect its database/metadata/hash outputs, and
      prove tracked installer inputs were unchanged and no MSI was installed.
- [x] Record final evidence, remaining signing/lifecycle gates, rollback paths, free space, and the
      next client-development step.

## Acceptance criteria

- [x] Every downloaded executable archive matches an official published digest before extraction.
- [x] A new SSH session resolves x64 PowerShell 7, Node 22, Go 1.25, .NET SDK 8, Git, npm, and the
      locked WiX dependency from only the dedicated development environment.
- [x] The Windows checkout preserves the exact base commit and honestly reports local source changes;
      no secret, runtime DB, recording, log, `node_modules`, or prior artifact is transferred.
- [x] Windows Viewer tests, TypeScript build, Electron x64 package, Go service tests, locked WiX
      restore/build, and MSI COM inspection all pass.
- [x] Published output contains the MSI, wixpdb, SHA-256 file, and secret-free metadata identifying
      version `2.0.21` as unsigned development output.
- [x] No MSI is installed and no NUC, CCTV, RDP-profile, monitoring, or production state changes.

## Review — dedicated Windows environment and unsigned MSI validated

- The isolated development root is `C:\CamStationDev`. A fresh pinned-key SSH session resolves
  PowerShell `7.6.4`, MinGit `2.55.0.3`, Node `22.23.2`/npm `10.9.8`, Go `1.25.12`, .NET SDK
  `8.0.423`, and locked WiX `6.0.2`. Toolchain manifest SHA-256 is
  `18a30cdea8be173c3a5e3fac6d78165b71bd5dcf81668ede139e35f033f39a19`.
- The Windows checkout keeps base commit `1215d0518a8e74866a5d786af865fdb4967bb18d`; its canonical
  status SHA-256 is `3f32c3c4e0be51f520339623bc169b0dac7ad2673a88a282d9d7f8c9f6e26658`
  and tracked binary-diff SHA-256 is
  `d5aef17691c4f62c5ee6dbda0c9632865be98566358945f80ea509a5fd04cd63`.
  `git diff --check` passes and ignored runtime/build data was not transferred.
- Native Windows execution exposed and fixed the real platform boundaries: named-pipe tests,
  portable npm filesystem commands, ASAR path separators, Visual C++ native runtime availability,
  WiX shortcut/directory ICE validation, supported Windows Installer SQL, explicit COM marshalling,
  and deterministic COM handle release.
- `pwsh -NoProfile -File .\scripts\build-viewer-msi.ps1 -Version 2.0.21
  -UnsignedDevelopment` completed with exit code `0`: Viewer tests passed `33/33`, Electron produced
  a win32-x64 package, both targeted Go service packages passed, and WiX reported zero warnings and
  zero errors.
- Published output is `C:\CamStationDev\src\CamStation\artifacts\viewer-msi\2.0.21` with exactly
  the MSI, wixpdb, SHA-256 sidecar, and metadata. `CamStationViewer.msi` is `124350464` bytes with
  SHA-256 `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e`.
  Independent COM inspection confirms version `2.0.21`, ProductCode
  `{094DC194-180B-4FDC-A399-F5DB6E96A86E}`, UpgradeCode
  `{7D4769BB-89EF-4C36-B4F2-52E33BF8BE87}`, and `76` File rows. Build time was
  `2026-08-10 09:32:20 KST`.
- Independent post-build checks confirm the sidecar/metadata digests and size, no secret or absolute
  path in metadata, `NotSigned` signature state, zero installed Viewer uninstall entries, zero
  `CamStationViewerService` services, zero temporary build workspaces, and unchanged protected
  installer inputs. The duplicate pre-fix build and two one-use source-transfer staging files were
  removed after the final MSI hash was rechecked; free space is `28.34 GB`. The monitoring NUC,
  CCTV runtime, and RDP profile were untouched.
- This is an unsigned development artifact, not a production deployment candidate. Remaining gates
  are Authenticode signing and install/upgrade/repair/uninstall lifecycle testing on a disposable
  Windows target. Rollback of this development setup is removal of the bounded `C:\CamStationDev`
  tree; the machine-wide Visual C++ runtime must only be removed after confirming no other software
  uses it.

---

# 2026-08-10 WIN11-DELL clean Viewer MSI installation

## Scope and clean-state definition

- Work only on the authorized `10.0.0.30` development PC. Preserve `C:\CamStationDev`, all build
  tools and source, unrelated software, user documents, the existing RDP session, the monitoring
  NUC, and CCTV services.
- “Clean” means no CamStation Viewer product registration, exact Viewer service/process, install
  directory, machine configuration/installer marker/Run value, product-created public shortcuts,
  ProgramData Viewer state, or exact Electron `CamStationViewer` profile residue.
- Prefer MSI uninstall for a registered product. Remove only an independently resolved exact Viewer
  residue after processes/services are stopped; never use broad CamStation, profile, Program Files,
  or registry deletion.
- Install the already verified unsigned development MSI `2.0.21` and leave it installed for client
  development. Preserve a verbose MSI log and record the exact rollback command. Do not configure a
  server address or start deployment to the NUC in this task.

## Plan

- [x] Inspect the MSI contract and capture a read-only baseline of product registrations, exact
      processes/services, install/state directories, registry values, shortcuts, profile residues,
      active sessions, and MSI integrity.
- [x] Remove only confirmed Viewer-specific registrations and residues, then prove the clean-state
      predicates are all true before installation.
- [x] Install MSI `2.0.21` with `msiexec.exe`, a bounded verbose log, and an explicit timeout; record
      the process exit code and reboot requirement.
- [x] Verify Windows Installer identity/version, installed file manifest, service binary/config/start
      state, Run value, shortcuts, ProgramData/log state, ACL-relevant ownership, and MSI self-repair
      registration without exposing configuration or credentials.
- [x] Perform a bounded basic runtime check without entering a server address, preserve the installed
      state, and document evidence, rollback, remaining UI/configuration tests, and free disk space.

## Acceptance criteria

- [x] The pre-install baseline is clean by the exact Viewer-only definition, with all unrelated host
      state and the development toolchain preserved.
- [x] `msiexec` returns success or success-with-reboot handling is explicitly recorded; the expected
      ProductCode `{094DC194-180B-4FDC-A399-F5DB6E96A86E}` and version `2.0.21` are registered once.
- [x] `CamStationViewerService` runs automatically from the expected Program Files path, produces a
      bounded service log, and has no unexpected restart/failure state.
- [x] Installed files, shortcuts, machine Run value, installer marker, and MSI cache/source identity
      match the authored package, while the Viewer remains unconfigured and no production endpoint
      is contacted.
- [x] The final report identifies the unsigned-development limitation, verbose install log, exact
      uninstall rollback command, and any remaining interactive desktop validation.

## Review — clean install and service lifecycle passed

- At `2026-08-10 09:45:13 KST`, the pre-install baseline found zero Viewer product registrations,
  services, processes, scheduled tasks, owned paths, registry keys/Run values, public shortcuts, and
  exact Electron profile residues. No cleanup deletion was needed. The active RDP session and
  `C:\CamStationDev` remained intact.
- The hash-pinned unsigned MSI was installed silently from `09:45:53` to `09:46:04 KST` with
  `msiexec` exit code `0` and no reboot requirement. The verbose log contains two invariant
  `MainEngineThread is returning 0` markers, no `Return value 3`, and a matching MsiInstaller success
  event. Install-log SHA-256 is
  `2377e524d46a35d4c94f2b41ab841b74fb8b9f88611d0f5a0381eb8dd2c5902e`.
- Windows Installer registers exactly one `CamStation Viewer` `2.0.21` product with ProductCode
  `{094DC194-180B-4FDC-A399-F5DB6E96A86E}`. The install contains `76` files totaling `370771832`
  bytes. Its cached MSI is byte-for-byte hash-identical to the source MSI, whose SHA-256 remains
  `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e`.
- `CamStationViewerService` runs from the exact Program Files path as `LocalSystem` with automatic
  start. A bounded stop/start test replaced its PID and produced the exact service-log sequence
  `running, stopped, running` with zero failure records. Recovery is configured for three 60-second
  restart actions with a one-day reset period.
- The public Desktop and Start Menu shortcuts, HKLM Run value, installer marker, ProgramData log,
  MSI cached source, and authored service path all exist. The machine configuration key is absent,
  no server endpoint was contacted, and no Viewer GUI process was launched.
- The management pipe correctly rejected the SSH network-logon token according to its deny-network,
  allow-interactive ACL. This boundary was not weakened. Opening the Viewer from the existing RDP
  desktop and checking setup-screen input remains the only interactive validation item.
- Final machine verification returned `INSTALLED_AND_SERVICE_VERIFIED` with every non-interactive
  check true at `2026-08-10 09:53:30 KST`. Evidence is under
  `C:\CamStationDev\evidence\viewer-install-2.0.21`; final report SHA-256 is
  `03d12a278ea036f7e72479f53ff0e71672f76249b387ad2c51e9daf3908a9a69`. Free disk is `27.77 GB`.
- The installed state is intentionally preserved. Exact rollback command:
  `msiexec.exe /x {094DC194-180B-4FDC-A399-F5DB6E96A86E} /qn /norestart /L*V
  "C:\CamStationDev\evidence\viewer-install-2.0.21\uninstall.log"`. The artifact remains unsigned
  development output and is not approved for NUC/production deployment.

---

# 2026-08-10 WIN11-DELL interactive GUI observability

## Specification and security boundary

- Provide a closed loop from Linux SSH to the existing `dyllislev` RDP desktop: launch the installed
  Viewer, capture its real window, return PNG/UIA evidence, and later support bounded focus/input
  actions without asking the operator to take screenshots.
- Use Windows Task Scheduler logon type `TASK_LOGON_INTERACTIVE_TOKEN` (`3`) so the one-shot worker
  runs only when the intended user is already logged on. Store no password and add no network
  listener, firewall rule, VNC/RDP service, remote-control account, or weakened Viewer pipe ACL.
- Register a unique least-privileged task for each operation, limit it to two minutes, and delete the
  exact task in `finally`. Restrict the run directory to SYSTEM, Administrators, and the target user.
- Capture only the verified `CamStationViewer` top-level window rectangle. Do not capture the whole
  desktop by default or collect text-field values. Record bounded control names/types, session ID,
  process IDs, dimensions, and image digest for reproducibility.
- Keep the Viewer installed and unconfigured during the first visual proof. Do not type a server
  address or contact CCTV until the rendered setup screen and input controls have been observed.

## Plan

- [x] Inventory Task Scheduler/session prerequisites and confirm no existing GUI bridge, listening
      service, task, or tool owns the proposed namespace.
- [x] Implement the interactive Viewer window capture worker and one-shot Task Scheduler launcher,
      with source-policy tests for session, ACL, timeout, target-window, and cleanup invariants.
- [x] Validate locally, synchronize only the new scripts/tests/docs to WIN11-DELL, and prove source
      hashes before execution.
- [x] Run `LaunchAndCapture` in the existing RDP session, retrieve the PNG and bounded UIA JSON over
      SSH, visually inspect the image, and prove the task was deleted with no new listener/service.
- [x] Record the working command, evidence hashes, limitations, and the next focus/input action needed
      for Viewer development.

## Acceptance criteria

- [x] The worker runs as `WIN11-DELL\dyllislev` in the same nonzero session as the active Explorer
      process without storing or requesting the user's password.
- [x] The installed Viewer produces a nonempty, target-window-only PNG that Codex retrieves and
      inspects directly; metadata identifies the matching Viewer PID/session and exact SHA-256.
- [x] UIA evidence is bounded and secret-safe and is sufficient to locate the setup form or clearly
      records that visual-coordinate fallback is required.
- [x] The unique scheduled task and worker PowerShell process exit, no GUI bridge port/service remains,
      and the Viewer service/RDP session/source MSI stay intact.

## Review

- Local Viewer tests passed `35/35`; the synchronized Windows source matched all recorded SHA-256
  values, parsed under Windows PowerShell 5.1, passed the native Windows Viewer tests `35/35`, and
  passed `git diff --check` before execution.
- Run `20260810T010741737Z-fd33ae4abe3c4a258cbca81d47526b43` launched Viewer PID `10308` as
  `dyllislev` in session `1` and captured a nonempty 1600x1200 window-only PNG.
- Run `20260810T011009032Z-fe024000cf854df78adc548b6b6801f1` repeated capture without relaunch;
  the PNG SHA-256 remained `e06bfb0520eb12ebf6b13c4de298a985155f665a5aa1b658418959b5027511b8`.
- Direct image inspection showed the complete Korean connection form with the server-address field
  focused. Settled UIA evidence independently confirmed `server-url` had keyboard focus and exposed
  `display-name`, both buttons, and the auto-start checkbox without reading any input values.
- Both tasks deleted themselves. Follow-up checks found zero remaining harness tasks/workers/bridge
  services; `CamStationViewerService` remained running/automatic and Explorer stayed in session `1`.

---

# 2026-08-10 repository commit and cleanup

## Specification

- Commit the completed CamStation server/canary, Windows Viewer/MSI, and GUI-observability work in
  reviewable logical commits without mixing generated runtime evidence into source history.
- Preserve all existing source and documentation changes. Do not pull, rebase, reset, or discard
  work while the branch is dirty; report the branch's upstream divergence separately.
- Keep operational evidence available locally but remove it from `git status` through a narrow
  repository-root ignore rule. Do not delete remote installation evidence, the MSI, or the running
  Viewer/service.
- Verify each intended commit's staged scope before committing, then run the relevant Go, web, and
  Viewer checks plus `git diff --check`. Finish with no unintended tracked or untracked source left.

## Plan

- [x] Inventory the complete dirty worktree, generated evidence, current branch, and upstream state.
- [x] Classify changed files into coherent development tooling, server/canary, Viewer surface,
      Windows Viewer/MSI, GUI observability, and maintenance-history commits.
- [x] Add only the narrow generated-evidence ignore needed to make the working tree maintainable.
- [x] Run the full relevant verification suite against the combined final tree.
- [x] Stage and inspect each logical commit, commit it, and verify the resulting commit contents.
- [x] Confirm final worktree status and record commit IDs, verification results, preserved evidence,
      and any remaining upstream action.

## Acceptance criteria

- [x] Every source change from the completed work is committed exactly once in a coherent commit.
- [x] Generated `work/`, build tools, artifacts, runtime data, and secrets are not committed.
- [x] Full verification passes on the exact committed tree.
- [x] The final status is clean apart from explicitly documented upstream divergence.

## Review

- Created eight scoped local commits: `2a2af55` development tooling, `0116944` migration/cutover,
  `b00f2b1` Viewer web surface, `c41b0ff` setup-focus fix, `7499e0e` MSI build, `25863aa` GUI
  capture, `a4d0479` operational documentation, and `1f3ad40` work history.
- Fetched the two upstream `/viewer` commits and reconciled them in merge `c45ec7f`. The verified
  saved-layout Viewer remained canonical, stale duplicate mobile components/plans were omitted, and
  the useful direct `/viewer` embedded-SPA test was retained. The web build reproduced
  `index-BlQ_LcUs.js` and `index-C8YzIVTY.css`.
- `./scripts/check-dev.sh` passed after reconciliation: all Go packages, 55 web tests, 35 Viewer
  tests, web lint/build, Viewer build, and daemon build. `scripts/production/test-policy.sh` and
  `git diff --check` also passed.
- The production policy check was corrected to distinguish the allowed Docker-internal
  `0.0.0.0:18080` listener from forbidden host-production exposure, while asserting the isolated
  host publication mapping explicitly.
- The reusable WinPC bootstrap was promoted from local evidence into `scripts/windows`; its source
  hash matches the operator-run script, so Viewer tests no longer depend on ignored workspace data.
- `.tools/`, `artifacts/`, `work/`, runtime `data/`, generated binaries, MSI output, screenshots,
  known-host files, and operational evidence remain uncommitted. `work/` was preserved locally.
- Final pre-review status was clean and `origin/camstation2-initial...HEAD` was `0 9`; no push was
  performed.

---

# 2026-08-10 project skill for Windows GUI verification

## Specification

- Register a repository-scoped Codex skill under `.agents/skills` so future CamStation sessions can
  discover the proven Windows Viewer GUI evidence workflow without relying on chat history.
- Reuse the reviewed scripts in `scripts/windows` as the only executable implementation. Keep the
  skill itself instructional and do not copy credentials, host keys, camera/server addresses, or
  environment-specific evidence into source control.
- Cover the complete verification loop: confirm an authorized active desktop session, launch or
  recapture the exact Viewer window, retrieve PNG/UIA/completion artifacts through the existing
  SSH boundary, verify hashes, inspect the image directly, and prove one-shot task cleanup.
- Preserve the established safety boundary: no full-desktop capture, text-field value collection,
  stored password, new listener/firewall rule, remote-control software, or Viewer ACL weakening.

## Plan

- [x] Initialize the repository skill with the supported skill-creator scaffold and Codex UI metadata.
- [x] Write concise triggering instructions and a deterministic runbook referencing the canonical
      project scripts, including settled-render recapture and cleanup/failure handling.
- [x] Add a source-policy test that protects discovery metadata, canonical script references, and
      the security/evidence invariants from accidental regression.
- [x] Validate the skill package, run the Viewer tests and repository whitespace checks, then record
      exact results and repository state.

## Acceptance criteria

- [x] The skill passes the official skill validator and lives at
      `.agents/skills/verify-windows-viewer-gui` with valid `SKILL.md` and `agents/openai.yaml`.
- [x] Its description reliably triggers for CamStation Windows GUI capture, focus, rendering, and
      interactive-session verification requests.
- [x] A future agent can follow it without asking the operator to manually capture the screen and
      without learning any secret or weakening remote-access controls.
- [x] Automated checks prove that exact-window capture, bounded UIA, artifact integrity, visual
      inspection, task cleanup, and prohibited access expansions remain documented.

## Review

- Added the repository-scoped `verify-windows-viewer-gui` skill with English and Korean trigger
  phrases plus Codex UI metadata. The runbook references the two reviewed PowerShell scripts instead
  of creating a second executable implementation.
- The workflow requires active-session preflight, local/remote source hash parity, one-shot
  `LaunchAndCapture` or `Capture`, exact three-file retrieval, SHA-256 verification, direct
  `view_image` inspection, bounded UIA correlation, settled-render recapture, and zero-task/worker
  cleanup proof.
- The skill package passed `quick_validate.py`. All Viewer tests passed `37/37`, including two new
  source-policy cases for trigger discovery and the GUI security/evidence contract; `git diff
  --check` also passed.
- A secret/environment scan found no concrete host IP, desktop username, host-key fingerprint, or
  private-key material in the skill. No Windows host, Viewer installation, camera configuration, or
  runtime evidence was changed while registering it.

---

# 2026-08-10 publish Viewer installer and return WIN11-DELL to clean-install state

## Specification

- Treat the acceptance path as one operator journey: `/settings` download -> Windows install ->
  Viewer launch -> server connection -> monitoring confirmation. The downloaded standard package is
  `CamStationViewer.msi`; it must install `CamStationViewer.exe` and the complete MSI-owned runtime.
  Do not publish a bare application EXE or revive the rejected custom Setup EXE.
- Identify the exact current CamStation Viewer 2.0 Windows installer format, version, size, and
  SHA-256 before changing either host. Do not rename an MSI to EXE or publish an unverified build.
- Publish the verified installer through the already running CamStation 2.0 Docker service using its
  existing release/download boundary when available. Keep the artifact in a persistent host mount,
  expose only the intended download file/metadata, and surface the download action on the 2.0
  `/settings` page rather than handing off only a raw API URL.
- Only after the server-side artifact is durable and an independent HTTP download matches its
  source hash, completely uninstall the existing Viewer product from the authorized Windows
  development PC. Remove only Viewer-owned installed state, service, tasks, shortcuts, registration,
  processes, and configuration; preserve SSH access, RDP, Windows build tools, source checkout, and
  unrelated evidence.
- Finish with two independent proofs: a fresh download from the 2.0 service has the expected hash,
  and the Windows PC has no installed Viewer product/runtime remnants while remaining available for
  the operator's manual installation test.

## Plan

- [x] Inventory the existing release endpoint/container mounts, candidate installer artifacts, and
      exact Windows installed product footprint without mutation.
- [x] Select and verify the canonical installer, stage it into persistent 2.0 release storage, and
      make the Docker settings page/download endpoint serve it with matching metadata.
- [x] Download the artifact independently through the `10.x` service URL and prove filename, size,
      content type, and SHA-256.
- [x] Uninstall the exact Windows Viewer product, then remove only confirmed Viewer-owned remnants.
- [x] Audit Windows product/service/process/task/file/registry state plus preserved SSH/build access;
      record the download URL, hash, rollback boundary, and verification results.

## Acceptance criteria

- [x] The published filename and extension match the real installer format and its version/hash are
      fixed in release metadata.
- [x] The installer appears as a download action on the CamStation 2.0 `/settings` page, remains
      downloadable after a container restart, and a downloaded copy matches the published SHA-256.
- [x] WIN11-DELL contains no installed CamStation Viewer product, Viewer service/process/task,
      auto-start entry, application directory, protected Viewer state, or product-created shortcut.
- [x] SSH maintenance access, the interactive Windows account/RDP session, source tree, and MSI build
      toolchain are preserved for subsequent development and the user's manual clean-install test.

## Review

- Commit `ed6c7df` adds exact-name MSI publication, filename-specific download headers, the visible
  settings filename/action, and an explicit guard that keeps MSI out of the legacy EXE Agent update
  path. The full Go suite, publisher contract, 55 web tests/lint/build, 37 Viewer tests/build, daemon
  build, and `git diff --check` pass.
- The dedicated Windows artifact and both server downloads are 124,350,464 bytes with SHA-256
  `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e`.
  The live browser snapshot shows version 2.0.21, `CamStationViewer.msi`, 118.6 MB, hash prefix, and an
  enabled download button; the browser click produced the same hash with no page/console error.
- The canary runs image `camstation:2.0.0-rc.20260810.8-canary` at image ID
  `sha256:719c6eea290f64251fc8858b8d327dc08296bfc52a746cefeec72b4dfbc69220`, healthy with restart 0,
  the same two bind mounts and port, exactly three streaming home cameras, and three running recorder
  workers. All five legacy 1.0 units retain their baseline PIDs and restart count 0.
- WIN11-DELL MSI uninstall returned 0 with no reboot. Product/service/process/task, five exact owned
  paths, Viewer/installer registry, Run value, and every user-profile Viewer residue are absent.
  SSH, Explorer session 1, development source/tools, and the original hash-matching MSI remain.
- Rollback is the previous immutable image `camstation:2.0.0-rc.20260809.7-canary` and root-only
  `.env.pre-msi-download-20260810-111413.bak`; persistent DB, media, and release storage were not
  deleted or replaced.

---

# 2026-08-10 Viewer 시험 레코드 삭제 및 운영 UI 정리

## Specification

- `QA Viewer`/`viewer-qa-01`은 실제 설치 클라이언트가 아니라 `/viewers`의 수동 하트비트
  시험 폼이 생성한 합성 레코드로 취급한다. 다른 Viewer 레코드는 변경하거나 삭제하지 않는다.
- 운영자 화면에서는 Viewer가 자체적으로 보내야 하는 하트비트를 임의 생성할 수 없게 하고,
  빈 목록 안내도 실제 Viewer 설치·연결 절차를 설명하도록 바꾼다.
- 삭제 가능 여부는 서버의 현재 상태 규칙(`offline` 또는 `stale`)과 동일하게 표시한다.
  최근 하트비트가 있는 Viewer에는 삭제 버튼을 비활성화하고 이유를 한국어로 안내한다.
- 서버가 삭제를 거절할 때 내부 오류 문자열 `validation`을 그대로 노출하지 않고, 현재 상태와
  재시도 조건이 포함된 구조화된 한국어 오류를 반환한다.
- 수정된 빌드는 새 불변 canary 이미지로만 배포한다. 기존 1.0 서비스, 카메라 3대,
  녹화·DB·배포 파일은 그대로 유지한다.

## Plan

- [x] 현재 canary에서 `viewer-qa-01`의 상태와 삭제 실패 HTTP 응답을 브라우저/API/로그로 재현한다.
- [x] 운영 페이지의 수동 하트비트 폼을 제거하고 Viewer 목록·삭제 UX를 서버 규칙과 일치시킨다.
- [x] 서버 삭제 충돌 응답을 구조화하고 route 및 웹 회귀 테스트를 추가한다.
- [x] Go·web 테스트, lint/build, daemon build와 diff 검사를 통과시킨다.
- [x] 새 불변 Docker 이미지를 배포하고 2.0/1.0 연속성 및 3대 카메라 상태를 검증한다.
- [x] 이미 TTL 만료 후 삭제된 정확한 `viewer-qa-01`의 부재와 다른 Viewer 보존을 배포 후
      재조회·새로고침·스크린샷으로 다시 증명한다.

## Acceptance criteria

- [x] `/viewers`에 QA용 하트비트 입력 폼과 미리 채워진 `QA Viewer`가 더 이상 노출되지 않는다.
- [x] 온라인 Viewer는 삭제할 수 없고, 오프라인 Viewer는 2단계 확인 후 삭제되며 원시
      `validation` 오류가 UI에 나타나지 않는다.
- [x] `viewer-qa-01`은 canary DB와 목록에서 제거되고 다른 Viewer 레코드 수·ID는 보존된다.
- [x] canary는 새 이미지로 healthy/restart 0이며 홈 카메라 3대가 유지되고, 1.0 다섯 서비스의
      PID와 재시작 횟수는 작업 전 기준과 동일하다.

## Review

- 수정 전 실제 브라우저에서 합성 레코드의 하트비트를 갱신하고 2단계 삭제를 수행해 정확한
  `DELETE /api/viewers/viewer-qa-01` 409와 원시 `validation` 표시를 재현했다. 30초 TTL이
  지난 뒤 같은 정확한 ID만 200으로 삭제됐고, 실제 Viewer ID는 보존됐다.
- 커밋 `00005dd`는 운영 UI의 수동 하트비트 폼과 웹 heartbeat mutation을 제거했다. 실제
  Viewer Agent용 서버 endpoint는 유지했다. UI/서버 모두 `offline`/`stale`만 삭제하며,
  온라인 경합은 `viewer_not_offline`, 현재 상태, 30초 조건이 있는 한국어 409를 반환한다.
- `./scripts/check-dev.sh`는 모든 Go 패키지, 57개 web 테스트·lint/build, 37개 Viewer
  테스트·build와 daemon build를 통과했다. production policy, focused route tests,
  embedded synthetic-string 검사와 `git diff --check`도 통과했다.
- 새 불변 이미지 `camstation:2.0.0-rc.20260810.9-canary`의 로컬/서버 ID는 모두
  `sha256:178b101f02488bf317ea8c447cb619adb4e151a0d943a634f35ea089ee5f28e4`이고 revision은
  `00005dd37760db1ec5c0e6afc06d1c4c60987d03`이다. 직전 `.8` 이미지는 보존했으며 root 전용
  롤백 포인터는 `.env.pre-viewer-registry-20260810-024232.bak`이다.
- 첫 이미지 포인터 치환 시도는 잘못된 `sed` 표현식으로 변경·재생성 전에 중단됐다. 즉시
  재확인한 `.env`, `.8` container, health는 그대로였고, 정확한 키 교체와 Compose 검증 후
  canary 하나만 재생성했다.
- 배포 후 canary는 healthy/restart 0, bind mount 2개와 `10.0.0.26:18081`을 유지한다.
  카메라 3/3, recorder 3/3, MSI 2.0.21/124,350,464 bytes가 유지되고 로그의 error/panic/fatal은
  0이다. 1.0 다섯 unit은 PID `248/326/247/396/246`, NRestarts 0으로 전후 동일하다.
- 실제 `/viewers` 화면은 Viewer 1대만 표시하고 QA 폼·행이 없으며 온라인 삭제 버튼과
  30초 안내가 정확히 렌더링됐다. 최종 화면 SHA-256은
  `ddf77e131e6eb6e8c096918751460470372d8cbb1e12aca9163efd4eaf3d590f`이고 브라우저
  page/console 오류는 0이다.
---

# 2026-08-10 Viewer command feature analysis

## Scope and specification

- Analyze the checked-out CamStation 2.0 implementation only; use legacy/production evidence only
  where it explains compatibility or a runtime dependency.
- Trace the complete command path: operator UI selection and form state, public HTTP API, database
  state transitions, Viewer Agent polling/claiming, local execution, result acknowledgement, and
  operator-visible history/actions.
- For every exposed command, identify its user-facing intent, required/optional inputs, validation,
  actual execution behavior, observable success result, failure/cancel behavior, and implementation
  status. Do not infer capability from a dropdown label alone.
- Distinguish source-level implementation, automated-test coverage, and live Windows/runtime proof.
  A feature is not called operational unless all dependencies required for execution are present.
- Define the target interaction contract: the user selects one currently controllable Viewer,
  chooses a clearly described supported action, supplies only inputs relevant to that action,
  confirms disruptive actions, executes it, and can see delivery, execution, success/failure, and
  retry/cancel outcomes.
- This task is analysis and documentation. Do not change Viewer command product behavior, contact a
  production Viewer, revive stale agents, or enqueue/cancel/delete live commands.

## Plan

- [x] Inventory the Viewer command UI, API modules, route registration, store schema/queries, Agent
      client, local command dispatcher, and related tests/docs.
- [x] Build a command-by-command behavior matrix with evidence for inputs, validation, execution,
      acknowledgement, cancellation, deletion, timeout/staleness, and operator feedback.
- [x] Run the narrow Go/Web/Viewer tests and safe local checks needed to establish what the source
      actually guarantees, recording any environment-dependent gaps separately.
- [x] Identify root causes behind the current ambiguity and any broken, misleading, unsafe, or
      incomplete flows; rank them by their effect on the operator's select-and-run goal.
- [x] Publish a Korean analysis document containing the current data flow, supported capability
      matrix, verified versus unverified status, target UX/API contract, phased implementation plan,
      and independently reproducible evidence paths.
- [x] Validate document links/evidence, `git diff --check`, and final worktree scope, then complete
      this section's review without claiming an implementation change.

## Acceptance criteria

- [x] Every command visible to the user is mapped to concrete server and Viewer code or explicitly
      classified as unsupported/unimplemented.
- [x] The analysis answers whether a selected Viewer can currently receive and execute each action,
      and states the exact prerequisites and proof level behind that answer.
- [x] Operator-visible defects are tied to root causes rather than only screenshot observations.
- [x] The desired select-action-execute-result flow is specified precisely enough to implement and
      test without another discovery pass.
- [x] No command is sent and no external Viewer/server state is changed during analysis.

## Review

- Published `docs/2026-08-10_viewer-command-feature-analysis.md` with the current path, screenshot
  interpretation, command matrix, root causes, target contract, implementation priorities, and
  completion criteria.
- Confirmed that the five UI commands are not operational in the current standard MSI: three are
  ignored by Viewer Service, while the other two are queued but discarded by Electron's
  request-response-only management client. The current Service also has no server result reporter.
- Distinguished the older, implemented Viewer Agent/Host command path from the current packaged
  Service/direct-Electron architecture; old source and stale status prose are not runtime proof for
  the current MSI.
- Verified existing suites: 55 Web tests, 35 Viewer tests, and focused Go tests for `internal/store`,
  `cmd/camstationd`, `internal/vieweragent`, and `internal/viewerservice` all passed. These suites do
  not contain a current-architecture end-to-end command test.
- All relative document links resolve and `git diff --check` passed. No local daemon was running,
  no command was sent, and no external Viewer/server state was changed.

---

# 2026-08-10 Viewer original control architecture reconciliation

## Scope and specification

- Treat the operator's requirement as normative: the server must monitor and remotely control a
  Viewer because routine recovery cannot require direct access to the Viewer PC.
- Search the working tree and complete Git history, including deleted and renamed documentation,
  for the earliest Viewer feature inventory and the monitoring/control layer separation.
- Reconstruct the decision chronology rather than treating the latest MSI packaging plan as the
  entire product contract.
- Reconcile the recovered intent with the current server, Viewer Service, Electron Viewer, UI, and
  the prior analysis document. Do not restore code or choose a new architecture without evidence.
- This remains a read-only product/architecture analysis except for correcting documentation and
  task lessons. Do not send, cancel, delete, or replay a Viewer command.

## Plan

- [x] Inventory all current Viewer specifications, plans, implementation-status notes, and relevant
      commit/file history for monitoring, control, Agent, Service, watchdog, and remote recovery.
- [x] Identify the earliest authoritative feature statement and build a dated decision chronology,
      including any superseding documents and whether they intentionally removed remote control.
- [x] Map the intended monitoring plane and control plane to concrete server and Windows components,
      including lifecycle ownership, command/result flow, and failure recovery boundaries.
- [x] Compare the recovered product contract with the current implementation and classify preserved,
      migrated, broken, or accidentally dropped responsibilities.
- [x] Correct the Viewer command analysis so it preserves server-side remote-control intent while
      accurately describing the current implementation gap and the required target architecture.
- [x] Validate citations, relative links, repository diff scope, and documentation consistency, then
      record the evidence and any remaining ambiguity in this review.

## Acceptance criteria

- [x] The original or earliest recoverable Viewer feature document and its commit are identified.
- [x] Monitoring and control are described as distinct layers with explicit responsibilities.
- [x] The analysis answers whether remote-control capability was superseded intentionally or lost
      during migration, based on documentary evidence rather than inference.
- [x] The corrected target preserves server-driven recovery without requiring normal PC access.
- [x] No production/runtime Viewer state is changed.

## Review

- Identified `docs/superpowers/specs/2026-07-03-viewer-client-redesign.md` at commit `ee6879c` as the
  earliest recoverable Git-tracked Viewer 2.0 feature specification. Its approved original version
  requires server monitoring and control even when the renderer is frozen, crashed, or unresponsive.
- Checked the earlier removed May CCTV wiki and the plan-referenced `.omo/drafts` path. The wiki only
  records a Viewer nginx route, while the draft is absent from both the worktree and Git history.
- Confirmed that the 2026-07-16 Agent design strengthened the monitoring/control separation and that
  the 2026-07-18 standard MSI design retained server status and UI commands while removing general
  process supervision.
- Located the concrete execution regression in `c6ef57c`: the standard MSI conversion deleted the
  prior Electron `onCommand`, `viewer:command`, and `command_result` path without adding equivalent
  unsolicited-command handling to `ManagementConnection`.
- Corrected `docs/2026-08-10_viewer-command-feature-analysis.md` to make remote control a normative
  requirement, document the monitoring/control/lifecycle layers, preserve the full original command
  catalog, and define the missing narrow lifecycle adapter.
- Corrected the Windows Viewer section of `docs/07-implementation-status.md` so the historical
  Agent/Host implementation is not reported as the active standard-MSI runtime and the current
  command gap is explicit.
- Relative links and whitespace checks pass. Only documentation/task files changed; no Viewer
  command was sent and no local or external runtime state changed.

---

# 2026-08-10 Restore Viewer remote control and verify on WinPC

## Scope and specification

- Restore the operator-facing command set currently exposed by `/viewers`: `ping`, `reload_live`,
  `resubscribe_stream`, `restart_viewer`, and the current control-component restart semantic renamed
  from `restart_agent` to `restart_service` at the UI/API compatibility boundary.
- Preserve the recovered product contract: Viewer monitoring and command control are separate
  planes, and server-driven recovery must not require routine direct operation of the Viewer PC.
- Keep the standard MSI and direct Electron launch model. Add only the narrow lifecycle mechanism
  required for explicit, audited Viewer/service restart; do not add arbitrary shell, desktop-control,
  URL-navigation, process-launch, or file-access commands.
- Use durable command identity, bounded deadlines, exact state transitions, duplicate suppression,
  and post-restart reconciliation so delivery is not mistaken for execution.
- Verify source-level behavior locally, build the served Web UI and Windows MSI, deploy through the
  existing approved WinPC maintenance path, and prove each command through server state plus
  process/UI evidence.
- Preserve unrelated existing worktree changes and runtime evidence; do not expose WinPC endpoints,
  credentials, camera URLs, or sensitive screenshots in tracked files or command output.

## Plan

- [x] Capture the current worktree, Viewer command tests/contracts, standard MSI composition, and
      bounded WinPC access/GUI-harness readiness without changing runtime state.
- [x] Write and review a focused design plus implementation plan covering command schemas, state
      transitions, IPC events/results, lifecycle restart ownership, UI behavior, and Windows proof.
- [x] Add failing-first server/store tests for command whitelist and per-type fields, exact lifecycle
      transitions, TTL/cancel semantics, compatibility naming, and safe error handling.
- [x] Implement server/store command validation and current command delivery/result behavior with
      exact operator-visible states and no raw-secret fields.
- [x] Add failing-first Viewer Service tests for direct `ping`, UI-command dispatch/result reporting,
      duplicate suppression, Viewer restart, service restart handoff, and restart reconciliation.
- [x] Implement the Service command engine and narrow lifecycle adapters without weakening named-pipe,
      session, executable-path, or service-ownership boundaries.
- [x] Add failing-first Electron/Web tests and implement unsolicited management commands, renderer
      result return, approved live reload, targeted stream resubscribe, localized capability-aware
      UI controls, confirmations, and active-command status refresh.
- [x] Restore the Viewer Service monitoring adapter so lease/renderer timestamps and bounded
      per-stream telemetry reach the server heartbeat independently of the command engine; prove an
      offline displayed stream remains selectable for targeted resubscribe.
- [x] Run focused and full Go/Web/Viewer tests, lint/build, Windows cross-build/MSI validation, secret
      scan, `git diff --check`, and review the implementation for simpler failure-safe boundaries.
- [x] Deploy the exact verified MSI to WinPC through the approved maintenance workflow and prove all
      five commands from server creation through final result, including process/renderer evidence
      for both restart commands and continued monitoring independence.
- [x] Update implementation status, analysis/design documentation, task review, and lessons with the
      exact verified behavior and any remaining environmental limitation.

## Acceptance criteria

- [x] A user selects a registered Viewer and sees only supported Korean-named actions with relevant
      inputs; arbitrary Viewer IDs, command types, routes, and irrelevant fields are rejected.
- [x] Command UI distinguishes pending, delivered, acknowledged, running, succeeded, failed,
      rejected, expired, and cancelled and updates active commands without manual refresh.
- [x] `ping`, `reload_live`, and `resubscribe_stream` execute exactly once and return a terminal result.
- [x] `restart_viewer` succeeds only after a new Viewer process/lease and renderer-ready proof.
- [x] `restart_service` succeeds only after a new Service boot/control connection reconciles the
      original durable command; the UI does not claim success before reconnection.
- [x] Service heartbeat/control status remains independent from Viewer/renderer/stream status across
      every injected failure and restart.
- [x] Duplicate delivery, cancellation, TTL expiry, missing Viewer lease, missing interactive session,
      and restart timeout produce bounded, safe, operator-visible outcomes.
- [x] The exact locally verified artifact passes real WinPC installation and end-to-end command proof
      without asking the operator to manipulate the Viewer desktop.

## Review

- Restored the five approved commands across server validation/store transitions, Service durable
  execution, Electron/renderer IPC, fixed-target Windows lifecycle adapters, and the Korean operator
  UI. `restart_agent` remains only a compatibility alias for `restart_service`.
- Kept monitoring independent from command execution and fixed a second migration omission found
  during WinPC acceptance: Service had acknowledged then discarded `stream_telemetry`. Viewer lease,
  renderer heartbeat/progress, and bounded streams now reach server heartbeat; the renderer repeats
  current stream state during long recovery cooldowns.
- `./scripts/check-dev.sh` passed all Go packages, 58 Web tests, 36 Viewer tests, Web lint/build,
  Viewer build, embedded Web regeneration, and daemon build. Native Windows Viewer/Service tests,
  Electron packaging, WiX validation, and the standard MSI build also passed without warnings or
  errors.
- Built and installed unsigned development MSI `2.0.24` (`124436480` bytes; SHA-256
  `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160`) on the authorized Windows 11
  PC. All five commands reached `succeeded` through the normal API. Viewer restart replaced the
  entire Viewer process set and recovered lease/renderer state; Service restart changed PID and boot
  generation, recovered control, and kept the Viewer running.
- Exercised the real `/viewers` user path by selecting the registered Viewer and `제어 연결 확인`, then
  submitting with keyboard focus. The API returned HTTP 201 and the UI automatically rendered the
  terminal succeeded row with exact lifecycle timestamps.
- Used an offline disposable camera to prove both monitoring states and targeted resubscription.
  Automated tests cover duplicate, TTL, cancel, unavailable lease/session, timeout, unsafe payload,
  and restart reconciliation boundaries; a long-running fault/soak matrix remains release work.
- Removed the disposable CamStation server/database/camera, Viewer configuration, local command
  journal/history, GUI evidence directories, and temporary automation state. WinPC retains only the
  verified `2.0.24` installation and automatic Service, with the Viewer closed and configuration
  returned to an unconfigured baseline.
- The remaining deliberate boundaries are an unsigned development MSI, no long-duration Windows
  soak, server-side `restart_stream` remaining on the Streams page, and no advertised
  `capture_diagnostics` Viewer command. If Windows accepts a Service stop but cannot start it again,
  the stopped component cannot report a terminal result and external SCM recovery is required.

---

# 2026-08-10 Merge Viewer remote control into 2.0

## Scope and specification

- Commit the verified Viewer monitoring/control restoration on its feature branch without including
  ignored runtime artifacts or external WinPC evidence.
- Merge it into the active local 2.0 branch `camstation2-initial`, preserving the five newer 2.0
  commits for MSI publication, Viewer registry cleanup, and GUI verification.
- Resolve overlapping server, Web, generated-asset, status, and task-document changes by retaining
  both the newer 2.0 behavior and the verified remote-control implementation.
- Rebuild derived Web assets and run the complete repository check from the merged 2.0 worktree
  before declaring the merge complete.
- Do not push, publish a release, reinstall WinPC, or change runtime configuration as part of this
  local branch merge.

## Plan

- [x] Reconfirm both worktrees are clean apart from the reviewed feature changes and identify every
      file changed by the five newer 2.0 commits.
- [x] Commit the verified feature branch with its source, tests, generated Web output, documentation,
      checklist, and lessons.
- [x] Merge the feature commit into `camstation2-initial` with an explicit merge commit and resolve
      each conflict against both parents rather than choosing one side wholesale.
- [x] Rebuild Web/Viewer/daemon derived output as required and run `./scripts/check-dev.sh` from the
      merged 2.0 worktree.
- [x] Inspect merge ancestry, worktree status, diff summary, whitespace, and added-line secret
      patterns; document the exact merge and verification result.

## Acceptance criteria

- [x] `camstation2-initial` contains the verified Viewer command implementation and all five commits
      that preceded the merge.
- [x] The merged branch exposes the five fixed operator commands while retaining current MSI release
      download and Viewer registry behavior.
- [x] Full Go/Web/Viewer tests, lint, builds, embedded Web output, and daemon build pass on the merged
      branch.
- [x] The feature branch and 2.0 parent are both visible in merge ancestry, with no force update or
      history rewrite.
- [x] No remote push, release publication, WinPC reinstall, or runtime-state mutation occurs.

## Review

- Feature commit `1d87081` contains the reviewed Viewer control restoration and no runtime/WinPC
  evidence. Merge commit `326fbab` has exact parents `4504212` (the pre-merge 2.0 tip) and `1d87081`;
  both histories are ancestors and no force update or rewrite was used.
- Resolved the server route by retaining 2.0's offline-only Viewer deletion and MSI
  `SupportsAgentUpdate` gate together with the strict command decoder and operator allowlist.
  Retained the read-only registry and Settings MSI download surface while adding the five functional
  Viewer controls and separate monitoring telemetry.
- Rebuilt embedded Web output from the combined source. The merged hashes are
  `index-BOMrEqyB.css` and `index-fM96zdPZ.js`; no parent-side hashed asset was selected manually.
- `./scripts/check-dev.sh` passed on the merged 2.0 worktree: all Go packages, 60 Web tests, 38 Viewer
  tests, Web lint/build, Viewer build, embedded Web regeneration, and daemon build.
- Final ancestry showed `camstation2-initial` ahead of `origin/camstation2-initial` by seven commits:
  the existing five 2.0 commits, the feature commit, and the merge commit. The worktree was clean
  immediately after the merge commit.
- No push, release publication, installer deployment, WinPC access, server configuration change, or
  runtime mutation was performed during the merge.

---

# 2026-08-10 Deploy merged Viewer controls to Docker canary

## Scope and deployment specification

- Deploy the exact local `camstation2-initial` HEAD containing the merged Viewer monitoring and
  remote-control restoration to the existing isolated CamStation 2.0 Docker canary.
- Build a new immutable image tag and retain the currently running image as the immediate rollback
  target. Never overwrite an existing tag, remove images, prune Docker state, or delete volumes.
- Recreate only the canary `camstation` service. Preserve its state, media, Viewer-release mounts,
  management-network-only HTTP publication, PID limit, restart policy, and three-camera allowlist.
- Keep the legacy 1.0 services, nginx ownership, databases, recordings, camera configuration, and
  client routing untouched. This deployment is not authorization for the 1.0-to-2.0 cutover.
- Publish Viewer MSI `2.0.24` only if the previously verified artifact can be recovered and its exact
  size and SHA-256 match the recorded build. Otherwise retain the current catalog entry and report
  that limitation instead of substituting or rebuilding an unverified installer.
- On any image/config/health acceptance failure, restore the backed-up image pointer and recreate
  the prior canary service before continuing analysis.

## Plan

- [x] Record the exact Git revision, current image/ref/ID, Compose deployment directory, container
      isolation settings, three-camera/recorder state, Viewer release metadata, and legacy service
      PID/restart baseline without mutating runtime state.
- [x] Build and inspect a new immutable Docker image from the exact merged HEAD, including OCI
      revision/version labels, non-root runtime contract, entry point, health check, and embedded UI.
- [x] Transfer the image without deleting the prior image; verify the remote image ID matches the
      locally built image before changing Compose configuration.
- [x] Create a root-only `.env` rollback copy, replace exactly one `CAMSTATION_IMAGE` key atomically,
      validate rendered Compose configuration, and force-recreate only `camstation`.
- [x] Prove health, zero restart count, unchanged port/mount/resource boundaries, three streaming
      home cameras, three running recorders, clean bounded logs, and unchanged legacy services.
- [x] Verify the real `/viewers` operator surface exposes the five supported controls and no
      synthetic Viewer registration form; close the browser automation session afterward.
- [x] If the exact verified MSI is available, publish it through the persistent release catalog and
      verify version/size/hash through API and `/settings`; otherwise leave the catalog unchanged.
- [x] Update the Docker canary runbook, this review, and lessons; run repository whitespace/link/
      secret-scope checks and commit only the deployment record changes.

## Acceptance criteria

- [x] The running canary image label points to the exact merged Git revision and a new immutable tag;
      the previous image and an exact rollback configuration backup remain available.
- [x] The container is healthy with restart count zero and the same host publication, bind mounts,
      PID limit, restart policy, and positive three-camera scope as before deployment.
- [x] Public APIs report exactly three streaming home cameras and exactly three running recorders;
      recent container logs contain no panic/fatal/credential leakage indicators.
- [x] All legacy 1.0 continuity services retain their pre-deployment active state, main PID, and
      restart count.
- [x] `/viewers` renders a selectable Viewer and the five fixed commands (`ping`, `reload_live`,
      `resubscribe_stream`, `restart_viewer`, `restart_service`) with Korean operator guidance.
- [x] The Viewer release catalog either serves the exact verified `2.0.24` MSI or remains unchanged
      with the missing-artifact reason explicitly recorded; no unverified installer is published.
- [x] The deployment record contains enough evidence for deterministic rollback without recording
      credentials, camera URLs, private runtime paths, or sensitive screenshots.

## Review

- Built exact HEAD `f9f43b7bafa6157b8d3fd32562f378f060689c26` as immutable
  `camstation:2.0.0-rc.20260810.10-canary`. Local and server image IDs both equal
  `sha256:19954a0ff6a2ea89a7453ce2af0975d03e7c52f9e26cc3ca4f227e9ce8c1ccc9`; labels, non-root
  user, entry point, health check, and all five embedded control types/labels passed inspection.
- Recovered the previously proven WinPC MSI rather than rebuilding it. Its 124,436,480-byte size
  and SHA-256 `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160`
  matched the acceptance record before atomic 2.0.24 publication. Metadata and a complete HTTP
  download matched again after container recreation; all bounded staging files were removed.
- Changed exactly one `CAMSTATION_IMAGE` line after a validated root-only backup. Compose rendered
  the intended image and recreated only `camstation`; the service became healthy without rollback.
  The immediate rollback pair is retained `.9-canary` image ID
  `sha256:178b101f02488bf317ea8c447cb619adb4e151a0d943a634f35ea089ee5f28e4` and
  `.env.pre-viewer-controls-20260810-074056.bak`.
- The new container is healthy with restart 0. Its management-only port, two bind mounts and mount
  fingerprint, read-only root, UID/GID, dropped capabilities, PID 1024, and restart `no` settings
  exactly match the baseline. APIs report three enabled/streaming home cameras and three running
  recorder workers; startup logs contain zero error/fatal/panic or credential-pattern matches.
- Browser acceptance selected the real Viewer and found exactly five command choices with no
  synthetic registration form. Because that Viewer is currently offline, command execution is
  correctly disabled. `/settings` displays 2.0.24 and the matching artifact identity, while all
  three `/viewer` videos reached MSE `playing`, `readyState=4`. The browser and screenshot were
  closed and removed without issuing a command.
- All five legacy 1.0 services retained their exact baseline PIDs (`248`, `326`, `247`, `246`,
  `396`) and restart count 0 before and after deployment. A final delayed check again returned
  canary healthy/restart 0, cameras 3/3, recorders 3/3, and legacy continuity 5/5.
- One publication assertion initially expected `application/x-msi`; the implemented and tested
  route deliberately serves MSI as `application/octet-stream`. Work stopped before Docker mutation,
  then metadata, length, disposition, complete content hash, and source contract proved publication
  correct. A later `docker top -eo comm` portability assumption was replaced by a read-only in-
  container process-name check; neither diagnostic mismatch changed runtime state.

---

# 2026-08-12 운영 Docker 라이브 초기 연결 상태 진단

## 범위와 진단 사양

- 사용자가 방금 재현한 운영 Docker 2.0 라이브 화면의 최근 로그를 읽기 전용으로 조사한다.
- `연결 중` → `영상 재연결 중` → `대체 스트림 연결 중` → 재생 전이를 만드는 프런트엔드
  조건·타이머·transport fallback과 같은 시각대의 HTTP/WebSocket/go2rtc 로그를 대조한다.
- 표시 문구만 빠르게 바뀌는 정상 초기화인지, 실제 WebSocket/미디어 연결 실패와 재시도가
  있었는지 구분하고 카메라별·transport별 증거를 제시한다.
- 진단 중 운영 컨테이너, 카메라, 설정, DB, 녹화기, Viewer에는 어떤 변경·재시작·명령도
  수행하지 않는다. 재현이 필요하면 읽기 전용 상태 확인 또는 일반 라이브 접속만 사용한다.

## 계획

- [x] 현재 운영 컨테이너 identity/health와 최근 라이브 관련 로그의 UTC·KST 범위를 수집한다.
- [x] 라이브 플레이어 문구별 발생 조건, timeout, primary/fallback transport 전이를 추적한다.
- [x] 브라우저 요청 및 go2rtc 로그를 코드 상태 전이와 맞춰 실제 실패·재시도 횟수를 판정한다.
- [x] 필요한 최소 검증을 수행하고 비밀정보 없는 타임라인, 결론, 권고를 Review에 기록한다.

## 합격 기준

- [x] 각 사용자 문구가 어떤 코드 상태에서 표시되는지 확인된다.
- [x] 최근 시도에서 primary 연결이 성공했는지 실패했는지, fallback이 실제 사용됐는지 로그로
      판정된다.
- [x] 영상이 나오기까지의 지연이 정상 설계 지연인지 결함인지 근거와 함께 설명된다.
- [x] 운영 상태를 변경하지 않았고, 결과에 원시 카메라 URL·자격증명·민감 경로가 노출되지 않는다.

## Review

- 대상은 revision `f9f43b7bafa6157b8d3fd32562f378f060689c26`의
  `camstation:2.0.0-rc.20260810.10-canary`다. 컨테이너는 2026-08-10 기동 뒤
  `healthy`, restart 0을 유지했고 2026-08-12 09:05 KST 사용자 재현 및 09:11 KST 독립
  재현 시각의 컨테이너 로그에는 player/WebSocket 오류가 기록되지 않았다.
- WIN11-DELL Viewer 2.0.24는 09:04:53 KST에 Service가 정상 재시작됐고 오류 code는 없었다.
  서버에는 09:04:03~09:10:56 KST Viewer heartbeat 43건과 09:05:12.742 KST 마지막 영상
  진행이 남았다. Viewer 전용 로그는 비어 있고 화면 종료 후 streams snapshot도 비워지므로,
  원래 재현의 attempt counter 자체는 사후 로그로 복구할 수 없었다.
- 같은 운영 `/live`를 격리 브라우저로 reload해 DOM/video 상태를 50 ms 간격으로 측정했다.
  세 카메라 모두 `연결 중`에서 4.911초에 `영상 입력 재연결 중`, 9.901초에
  `대체 스트림 연결 중`으로 바뀌었고 각각 10.051초, 10.201초, 10.351초에 `playing`과
  `readyState=4`에 도달했다. 이 전이는 코드의 5초 setup timeout 두 번과 정확히 일치한다.
- 복구 순서는 initial WebRTC/live → WebRTC/live retry → MSE/live → 필요할 때만 MSE/focus다.
  재현 중 public stream status는 세 `live` output이 각각 viewer 1로 running이고 모든
  `focus` output이 viewer 0으로 idle이었다. 따라서 이번 성공은 카메라 대체 스트림이 아니라
  동일한 primary live 스트림의 MSE transport 성공이다.
- Docker는 HTTP `18080/tcp`만 host에 publish한다. 생성된 go2rtc WebRTC candidate는 bridge
  내부 주소의 `8555` 한 개이고 외부에 publish되지 않는다. 운영 문서도 same-origin
  `/player/api/ws` MSE를 정상 경로로 지정한다. 따라서 direct WebRTC가 성립하지 않는 배포에서
  UI가 WebRTC를 기본으로 두 번 시도하는 것이 약 10초 지연의 근본 원인이다.
- `fallback` phase를 무조건 `대체 스트림 연결 중`으로 번역해 transport fallback과 stream
  fallback도 혼동한다. 결론은 실제 연결 실패·재시도가 두 번 있고, 마지막 문구도 부정확하다는
  것이다. 카메라 RTSP나 컨테이너 재시작 장애로 인한 지연은 아니다.
- 격리 브라우저를 닫은 뒤 세 live viewer count가 모두 0으로 돌아왔고 컨테이너는 계속
  healthy/restart 0이었다. focused recovery/selection 테스트 17개도 통과했다. 진단 범위에서는
  소스, 컨테이너, 설정, DB, 카메라, 녹화기 및 Viewer에 변경이나 제어 명령을 수행하지 않았다.

---

# 2026-08-12 Docker WebRTC 즉시 연결 및 재생 진단 로그

## 범위와 구현 사양

- 기존 1.0과 병행 중인 Docker 2.0 카나리에 외부 도달 가능한 WebRTC TCP·UDP 경로를 추가한다.
  관리망 주소에만 bind하고, 기존 1.0의 host port 점유를 확인해 충돌 없는 카나리 포트를 쓴다.
- go2rtc가 자동 발견한 Docker bridge candidate 대신 명시적으로 검증된 외부 candidate를
  광고할 수 있게 한다. 비 Docker 실행의 기존 자동 발견 동작은 그대로 보존한다.
- 라이브 재생 attempt에 correlation ID, stream role, transport, attempt, phase, elapsed time,
  failure category, media readiness를 포함한 secret-safe 구조화 로그를 추가한다. 로그 레벨은
  `off/error/warn/info/debug`로 설정하고, 서버가 레벨을 필터링·크기 제한·rate limit한다.
- WebRTC→MSE transport 전환과 `live`→`focus` stream 전환을 서로 다른 상태로 표현한다.
  실제 두 번째 stream candidate를 사용할 때만 `대체 스트림` 문구와 badge를 표시한다.
- 소스 변경은 테스트 후 새 immutable image로 카나리만 교체한다. 기존 이미지와 root 전용
  Compose 포인터 백업을 유지하고, 실패 시 직전 카나리로 되돌린다.
- WIN11-DELL의 기존 Viewer 2.0 설치·Service·RDP 세션·저장 설정을 보존하면서 정상 사용자
  실행 경로로 첫 WebRTC 재생과 구조화 로그를 확인하고, 프로젝트 GUI evidence loop로 실제
  영상 창을 검증한다.

## 계획

- [x] 현재 host TCP/UDP 포트 점유, firewall, Docker 경계, Viewer 세션, rollback 기준선을 확인한다.
- [x] 외부 WebRTC candidate 설정과 유효성 검사를 failing-first Go 테스트로 구현한다.
- [x] playback 구조화 로그 endpoint, 레벨 필터, redaction/bounds/rate limit를 failing-first로 구현한다.
- [x] 프런트엔드 attempt 계측과 transport/stream fallback 표현 분리를 failing-first로 구현한다.
- [x] 전체 Go/Web/Viewer 테스트와 lint/build를 통과하고, 배포 전 secret scan과 diff 검증을 완료한다.
- [x] 새 immutable Docker image를 배포하고 candidate·TCP/UDP publish·health·rollback 경계를 검증한다.
- [x] 브라우저에서 세 카메라가 첫 WebRTC attempt로 즉시 재생되고 info/debug 로그가 맞는지 증명한다.
- [x] WIN11-DELL Viewer에서 같은 결과와 실제 GUI를 증명하고 evidence harness를 정리한다.
- [x] 실제 검증에서 드러난 GUI launcher 기본 worker 경로 초기화 결함을 회귀 테스트로 수정한다.
- [x] 운영 문서, Review와 lessons를 실제 결과에 맞게 갱신한다.

## 합격 기준

- [x] Docker 외부 클라이언트가 private bridge 주소를 받지 않고 관리망에서 도달 가능한 candidate를 받는다.
- [x] 세 카메라 모두 initial WebRTC attempt에서 5초 timeout 없이 재생되며 MSE fallback은 발생하지 않는다.
- [x] 기본 info 로그만으로 attempt 시작·성공/실패·transport·stream·elapsed time을 상관관계로 복원할 수 있다.
- [x] debug 로그는 signaling과 첫 미디어 진행을 보이되 URL, SDP, ICE 원문, 자격증명을 남기지 않는다.
- [x] 실제 stream candidate가 바뀌지 않으면 `대체 스트림` 문구·badge·counter가 나타나지 않는다.
- [x] 카나리 health/restart, 녹화기, 기존 1.0 서비스, Windows Viewer Service와 interactive session이 유지된다.

## Review

- 근본 원인은 Docker가 HTTP만 publish한 상태에서 go2rtc가 bridge 내부 WebRTC candidate를
  광고한 것이다. `/live`와 설치형 Viewer가 도달 불가능한 WebRTC/live를 두 번 5초씩 실제
  timeout한 뒤, 다른 카메라 stream이 아닌 같은 `live` stream의 MSE로 성공했다. 동시에 UI가
  이 transport 전환을 `대체 스트림`으로 잘못 표시했다.
- revision `dd619b5990b4f05a2b6b56a969acdffd39c97f40`은 검증된 명시 WebRTC candidate,
  secret-safe bounded playback diagnostics, transport/stream fallback 표현 분리, Docker
  TCP·UDP publish를 구현한다. native 실행은 명시 candidate가 없을 때 기존 local-interface
  자동 발견을 유지한다.
- 운영에는 immutable `camstation:2.0.0-rc.20260812.11-canary`, image ID
  `sha256:b4e5fe10099bcd167c34925ac178d2951d2ad01c120e0af77858365dcae5259a`를 배포했다.
  host `10.0.0.26:18555/tcp+udp`가 container `8555`에 매핑되고 생성 설정은 candidate
  `10.0.0.26:18555`만 포함한다. API `1984`와 RTSP `8554`는 계속 내부 전용이다.
- 운영 브라우저의 세 session은 WebRTC attempt 1에서 각각 2.917~4.352초에 재생됐고,
  WIN11-DELL Viewer의 세 session은 3.209~4.210초에 재생됐다. 두 실행 모두 retry 0,
  MSE fallback 0, stream fallback 0이며 `대체 스트림` 문구·badge도 나타나지 않았다.
- debug에서 두 실제 클라이언트의 구조화 event 36건이 attempt, socket, signaling answer,
  first track, playback start를 연결했다. URL/SDP/ICE 원문/자격증명 금지 패턴은 0이었다.
  canary는 진단을 위해 debug를 유지하며 Compose 기본값은 info다.
- Windows session 1의 실제 `CamStation 2.0` 창은 PrintWindow PNG와 17개 UIA 요소로
  검증했다. PNG SHA-256은 `7b58feed11f17db87700e70c3c21bd585beba6705bd76dea0823c2c39419b562`,
  UIA SHA-256은 `489ef95db9896e2c07b9418dc275c6c7ed2a4d5fb5df7e880660aa223bc05a6f`다.
  GUI harness의 `$PSScriptRoot` parameter-default 결함은 failing-first 테스트 후 param 이후
  초기화로 고쳤고, WorkerScript를 생략한 실제 Capture도 성공했다. 최종 scheduled task와
  worker는 0, Explorer는 정확히 session 1 하나, Viewer Service는 running이다.
- 카나리는 healthy/restart 0, camera 3/3 streaming, recorder 3/3 running이다. 세 active
  recording 파일은 같은 inode에서 모두 5초간 1,048,576 bytes 증가했다. 기존 1.0 핵심
  서비스 5개의 PID와 restart 0은 기준선 그대로이며 mount fingerprint, 권한, resource limit도
  변하지 않았다.
- 최종 `./scripts/check-dev.sh`는 전체 Go package, Web 64 tests·lint·build, Viewer 38
  tests·build와 daemon build를 통과했다. production policy, 변경 문서 상대 링크 10개,
  changed-diff secret pattern, `git diff --check`도 모두 통과했다.
- 즉시 rollback용 `.10-canary` image ID
  `sha256:19954a0ff6a2ea89a7453ce2af0975d03e7c52f9e26cc3ca4f227e9ce8c1ccc9`와 root 전용
  `.env.pre-webrtc-20260812-113427.bak`, `compose.yaml.pre-webrtc-20260812-113427.bak`을
  보존했다. 첫 재생은 5초 timeout 전 단일 attempt로 복구됐지만 on-demand public output과
  keyframe 준비 때문에 측정상 2.9~4.4초는 남는다.

---

# 2026-08-12 Docker WebRTC 192 대역 경로 교정

## 범위와 계획

- [x] 기존 검증이 10 대역 candidate에 한정됐음을 확인하고 서버의 192 대역 주소·포트 기준선을 수집한다.
- [x] 기존 WIN11-DELL 테스트 PC의 192 대역 주소와 source route를 확인한다.
- [x] Docker Compose가 10/192 HTTP bind와 단일 192 WebRTC candidate를 명시적으로 지원하도록
      failing-first policy test와 최소 변경을 구현한다.
- [x] 카나리만 재생성하고 192 대역 publish, candidate, health, rollback과 1.0 연속성을 검증한다.
- [x] WIN11-DELL의 192 인터페이스와 192 서버 주소로 첫 WebRTC attempt·구조화 로그·실제 GUI를 검증한다.
- [x] 운영 문서와 lessons를 교정하고 전체 회귀 검사·최종 커밋을 완료한다.

## 합격 기준

- [x] 192 대역 전용 클라이언트가 `192.168.0.160:18081`에 접속하고
      `192.168.0.160:18555` WebRTC candidate로 재생한다.
- [x] 테스트 PC의 실제 source address가 192 대역인 상태에서 세 카메라가 첫 WebRTC attempt로 재생된다.
- [x] 10 대역 관리 경로, Docker 보안·저장소 경계, 녹화기와 기존 1.0 서비스가 유지된다.
- [x] Windows Viewer 설정·Service·interactive session을 보존하고 GUI harness task/worker를 모두 정리한다.

## Review

- WIN11-DELL의 실제 주소는 `192.168.0.178/24`이고 source-bound probe로 서버
  `192.168.0.160:18081` health 및 18081/18555 TCP 도달성을 확인했다. 서버 `eth1`에서는
  이 source가 `192.168.0.160:18555/udp`로 만든 실제 흐름 세 개가 관측됐다.
- 카나리의 최종 publish는 HTTP `10.0.0.26:18081`·`192.168.0.160:18081`, WebRTC
  `192.168.0.160:18555/tcp+udp`이며 candidate는 192 주소 한 개다. API `1984`와 RTSP
  `8554`, 10 대역 WebRTC는 공개하지 않는다.
- 첫 배포는 BusyBox `grep` 검증기 사용 오류로 실패했으며 자동 롤백 뒤 이전 port/candidate,
  health, restart 0과 stage 정리를 증명했다. 검증기를 교정한 두 번째 배포는 성공했고 root 전용
  `.env.pre-monitor-192-20260812-125110.bak`과
  `compose.yaml.pre-monitor-192-20260812-125110.bak`을 보존했다.
- Viewer의 저장 주소만 `http://192.168.0.160:18081`로 바꾸고 ID·표시명·AutoStart=false,
  Service와 session 1을 보존했다. 기존 Viewer가 새 주소를 읽도록 `restart_viewer` 한 건을
  실행해 최종 `succeeded`를 확인했다.
- 최종 세 session은 모두 WebRTC attempt 1에서 0.712~1.102초에 재생됐고 retry, MSE,
  stream fallback은 각각 0이었다. 구조화 event 18건의 금지 URL/SDP/ICE/자격증명 패턴과
  error/fatal/panic도 0이었다.
- 실제 GUI 원본에서 영상과 카메라 control 3개, 재연결·대체 스트림 overlay 부재를 확인했다.
  최종 evidence SHA-256은 PNG
  `57918f52dd239bbc8b22905af2b8c3d14ce0aa3e95f086777044a5fe8bfc40fa`, UIA
  `489ef95db9896e2c07b9418dc275c6c7ed2a4d5fb5df7e880660aa223bc05a6f`, complete JSON
  `e8d43baaa9aff09ed1c3f1138608dcafbcad3a40979d0dfd4979eb058e59e94b`이며 capture/config
  task와 worker는 모두 0으로 정리됐다.
- 컨테이너 healthy/restart 0, 카메라 3/3 streaming, recorder 3/3 running, active 녹화 3개
  증가, UID/GID·read-only·PID/memory/CPU·mount 경계와 기존 1.0 핵심 5개 service PID/restart
  기준을 유지했다.
- `./scripts/check-dev.sh`의 Go 전 패키지, Web 64 tests·lint·build, Viewer 38 tests·build,
  production policy, exact Compose JSON render, 변경 문서 link, diff secret-pattern 및
  `git diff --check`를 통과했다. 로컬에 `pwsh`가 없어 PowerShell AST 검사는 생략했지만 같은
  helper의 원격 parser error 0과 실제 `ConfigureOnly` 성공을 확인했다.
- 지연 재점검에서도 운영 container는 healthy/restart 0이고 정확한 dual HTTP·192 WebRTC
  publish/candidate와 보안 한계를 유지했다. 테스트 PC도 source-bound 192 health 성공,
  Viewer Service running, Viewer process 4, Explorer session 1, capture/config task 0이었다.
