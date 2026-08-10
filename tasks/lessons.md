# Lessons

## 2026-08-10 — 시험용 Viewer 등록 기능을 운영 화면과 분리한다

- Viewer 하트비트는 설치된 클라이언트의 생존·상태 증거이지 운영자가 임의로 만드는 등록
  양식이 아니다. 미리 채운 `QA Viewer` 폼을 운영 콘솔에 두면 가상 레코드를 실제 설치로
  오인하게 하고 상태 판단과 유지보수를 방해한다.
- 삭제 버튼은 서버와 같은 삭제 가능 조건을 사용해야 한다. 최근 하트비트로 온라인인 항목에
  “오프라인 Viewer 삭제”를 활성화한 뒤 `validation`만 표시하는 것은 UI 계약 위반이다.
- 충돌 응답은 현재 상태와 필요한 조치를 구조화해 반환하고, UI에서는 한국어로 행동 가능한
  설명을 보여야 한다. QA 데이터 생성은 API 테스트나 별도 개발 도구로만 수행한다.

## 2026-08-10 — 원격 이미지 포인터 변경은 정확한 키 교체로 실패 폐쇄한다

- 셸 변수로 `sed` 치환식을 조립할 때 끝 앵커와 특수 매개변수 확장이 섞이면, 서비스 변경
  전이라도 표현식 오류가 날 수 있다. 이미지 포인터처럼 한 줄만 바꿀 때는 대상 키 개수와
  기존 값을 먼저 검증하고 정확한 키 교체를 사용한다.
- root 전용 백업을 먼저 만들고, Compose 검증 전 오류에서는 컨테이너를 재생성하지 않는다.
  실패 직후 `.env`, 실행 이미지, health를 다시 읽어 무변경을 증명한 뒤 재시도한다.
- 배포 성공 후에는 이미지 ID·mount·port뿐 아니라 기존 세대 서비스 PID/재시작 횟수까지
  전후 비교해 좁은 배포 경계를 입증한다.

## 2026-08-09 — Select the product generation before setup

- When a repository contains separate product generations, do not assume the currently
  checked-out default branch is the requested target.
- Resolve explicit version language such as "2.0" against the branch and architecture
  documentation before installing dependencies or writing environment configuration.
- For CamStation, `main` is the FastAPI/React 1.x line and `camstation2-initial` is the
  Go single-daemon 2.0 line. Development-environment work must state which line it targets.

## 2026-08-09 — Verify Paseo through its registered-project path

- A valid local `paseo.json` and direct wrapper smoke tests do not prove that Paseo has
  loaded project settings.
- Treat placeholder-only lifecycle fields or an empty script list as a failed integration,
  even when the repository config passes schema validation.
- Before declaring Paseo setup complete, verify the registered project or workspace through
  the daemon/UI/CLI path and account for the rule that new worktrees only inherit config
  committed on their selected base branch.

## 2026-08-09 — Separate Viewer UI liveness from control-agent liveness

- Never accept a stored Viewer `state=healthy` value without comparing `last_seen` to the
  current KST time. A stale database row can remain healthy for weeks.
- Correlate at least three surfaces: current reverse-proxy traffic proves the Electron UI is
  alive, heartbeat age proves the control agent is alive, and command history proves remote
  control is being consumed.
- Inspect the route implementation before using a seemingly read-only pending-command GET;
  the legacy endpoint claims and mutates the oldest command. Expire stale restart commands
  before reviving an agent.

## 2026-08-09 — Prove dual-homed and overlay identities cryptographically

- Map management, camera-LAN, and Tailscale addresses with host keys, service certificates,
  and direct-path evidence rather than relying on hostnames or old documentation alone.
- Treat network reachability and authentication as separate gates. An open Windows SSH or
  AnyDesk service does not mean the maintenance environment has an authorized login.
- For recorder liveness, use a per-camera sample long enough to cross write-buffer flushes;
  a three-second sample produced a false negative that a ten-second inode/mtime/size check
  correctly resolved.
- When an operator identifies a stored endpoint as "probably" a known development server,
  retain that attribution as a candidate mapping. Promote it to verified only after hostname,
  interface inventory, and host-key evidence agree through both addresses.

## 2026-08-09 — Bootstrap Windows access in two verified stages

- When an approved operator can run commands on an otherwise unauthenticated Windows target,
  begin with a read-only identity, group, profile, service, and `sshd_config` diagnostic.
- Do not assume that `%USERPROFILE%\.ssh\authorized_keys` is effective: administrator accounts
  normally use the shared `%ProgramData%\ssh\administrators_authorized_keys` rule instead.
- Add only the maintenance public key after the effective account/path is known, preserve
  existing keys, apply restrictive ACLs, and prove login from the actual maintenance client.

## 2026-08-09 — Separate installed Viewer version from the operating Viewer version

- An MSI uninstall entry and a running management service prove that a Viewer generation is
  installed, not that its interactive UI owns the current monitoring session.
- Reconcile Windows process/session evidence, local service IPC status, current server traffic,
  and the operator's observed screen before declaring a 1.0-to-2.0 cutover complete.
- Treat a side-by-side 2.0 service with no active Viewer lease as staged until the interactive
  Viewer is launched, connected, visibly rendering, and producing fresh server telemetry.

## 2026-08-09 — Separate branch integration from production replacement

- Merging the 2.0 branch into `main` establishes the future source line; it does not migrate
  the legacy database, service units, camera credentials, recording history, or Viewer state.
- When the operator chooses the existing production `cctv` host as the replacement target,
  design a same-host staged cutover with separate runtime directories, ports, database, and
  service names. Keep the 1.x runtime intact as the immediate rollback until 2.0 acceptance.
- Treat the historical `cctv2` host as optional pre-production evidence, not as the production
  destination, unless the operator explicitly changes the deployment decision.
- Check `merge-base` before promising a normal branch merge. When product generations have
  unrelated histories, preserve both parents while making the release tree exactly equal to
  the approved replacement tree; do not resolve the repositories as if they were one code line.
- A same-host deployment is not runtime blue/green when both generations own the same fixed
  loopback ports. Stage artifacts and data independently, then use a single-active maintenance
  handoff with an intact runtime rollback.
- Production configuration must override development defaults explicitly. Recording disabled,
  a test backup target, a small cleanup threshold, or a development-only health response are
  release blockers even when unit tests pass.

## 2026-08-09 — Separate a legacy WebView shell from its server generation

- A Windows Viewer being WebView/Electron-based means it may render a newer server-owned UI;
  it does not prove that its hard-coded startup path, navigation allowlist, heartbeat protocol,
  or control commands are compatible with the newer server.
- A server-first cutover is a useful risk boundary: keep the old Viewer installation as a
  temporary display shell and rollback asset, but never keep the 1.x backend/recorder/go2rtc
  runtime active alongside 2.0 on the same fixed ports.
- If a legacy shell is retained temporarily, use one exact, testable compatibility route to the
  new live page and label control/health features unsupported. Remove the bridge only after the
  2.0 Viewer passes interactive-session and auto-start acceptance.

## 2026-08-09 — Apply the operator's accepted transitional success criterion

- Once the operator explicitly accepts video-only behavior from a transitional legacy shell,
  do not keep management telemetry or remote-control parity as a server-cutover blocker.
- Move those capabilities to the later native Viewer 2.0 gate while retaining actual live-video
  rendering, camera-count invariants, recording, backup, and rollback as non-negotiable evidence.
- Translate the accepted compromise into an exact compatibility contract and automated negative
  tests; “video only is enough” does not justify a broad legacy route or an untested redirect.

## 2026-08-09 — Make production-dangerous defaults empty and tests explicit

- A disabled feature must not silently carry a development destination into a production DB.
  Use an empty inert default, require the destination when the feature is enabled, and keep
  deletion protection enabled independently.
- Tests for command execution must configure their synthetic remote explicitly. If they rely on
  a dangerous package default, changing that default can turn channel-based tests into hangs
  instead of useful failures.
- When the host lacks the SQLite CLI and the source may use WAL, never copy only the main DB file.
  Use the driver's online-backup API, promote a new immutable snapshot without overwrite, and
  compare the active source, snapshot, and converted target through secret-safe canonical hashes.

## 2026-08-09 — Pre-stage everything that does not require the outage

- When the operator explicitly approves server preparation, distinguish inactive staging from
  the final cutover instead of leaving all production work for the maintenance window.
- While 1.x remains healthy, install a hash-pinned 2.0 release, disabled unit, runtime paths,
  online source snapshot, and verified target DB. Do not start the port-conflicting generation.
- The maintenance window should contain only irreducible active-state changes: maintenance page,
  exact legacy stop, port release, 2.0 start, health/video proof, and nginx handoff.

## 2026-08-09 — Validate production topology and semantic stream types before packaging

- A configured recording size is not evidence that the proposed runtime path uses the recording
  filesystem. Resolve mounts and free space first, then keep recording and temp on the same media
  filesystem so finalization can remain atomic.
- A legacy field named `sub_stream_url` may contain a go2rtc producer recipe rather than a camera
  endpoint. Detect the exact loopback/self-key form and translate its intent into a 2.0 output;
  never wrap it as another input and create a recursive producer.
- Files named as backups can still be active when they remain under an nginx wildcard include.
  Compare their hashes, move exact duplicates to a root-only recovery location, and prove the
  single active server block continues serving legacy health before declaring nginx ready.
- Runtime ownership and boot ownership are separate state. A start/stop-only cutover can appear
  healthy and still revert into a port collision after reboot; switch enablement inside the same
  automatic rollback boundary as service and nginx ownership.

## 2026-08-09 — Use camera-capability-aware canaries instead of all-or-nothing rehearsal

- If only part of a camera fleet permits duplicate consumers, an isolated canary can validate that
  subset while the legacy generation remains active. Never infer that success applies to devices
  known to reject concurrent sessions.
- Isolation must cover the whole runtime, not only HTTP: go2rtc API/RTSP/WebRTC, recorder inputs,
  state DB, temp/recording roots, service identity, ingress, and shutdown verification all need
  separate boundaries.
- Build the canary DB from a verified snapshot, disable out-of-scope cameras fail-closed, and keep
  the final-cutover DB immutable so trial state cannot silently become production state.

## 2026-08-09 — Re-evaluate the deployment boundary before adding host-runtime configuration

- When parallel generations are the goal, check whether container isolation removes more lifecycle
  and dependency coupling than adding host-level port flags. Stop before implementation when the
  user changes this architectural boundary.
- Container isolation solves ports, files, dependencies, and rollback packaging; it does not solve
  upstream camera session limits, so camera allowlisting remains a separate fail-closed control.
- Keep ingress separate from the application image when an existing production reverse proxy owns
  the stable client address; this avoids bundling two process managers and reduces cutover scope.

## 2026-08-09 — Distinguish host-port collisions from container-internal ports

- Do not describe a host-native collision as if it also applies to Docker bridge networking. Each
  container has its own network namespace, so repeated internal ports are safe; only duplicate host
  port publications or `network_mode: host` collide.
- If nginx is inside a bridge-networked container, its internal port 80 does not conflict with host
  nginx when it is published to a distinct host port. The reason to omit it is architectural
  simplicity, not an unavoidable port collision.
- Separately name upstream camera-session contention: bridge NAT can still make both generations
  reach a camera from the same host IP, so Docker network isolation does not remove a camera's
  single-client restriction.

## 2026-08-09 — Verify hybrid legacy configuration authority before migration

- Do not call a legacy DB the sole source of truth until startup and runtime code prove it. This
  1.x installation stores camera registry fields and URLs in SQLite, imports missing rows from
  go2rtc YAML, and treats YAML-enabled streams as authoritative for startup recording.
- Cross-check stable keys, enabled state, and secret-safe URL fingerprints between SQLite and
  go2rtc before selecting canary cameras. A fleet-wide mismatch means a DB-only conversion is not
  acceptable even when the intended subset matches.
- Describe a snapshot precisely as an offline data input, not as the old program or a directly
  reusable database. For a hybrid source, snapshot every required authority or build the new DB
  from a reconciled subset.

## 2026-08-09 — Follow the operator-designated runtime authority

- When the operator identifies the live go2rtc configuration as authoritative because it is the
  configuration currently producing video, use it directly for camera keys, enabled state, and
  producer definitions. Do not reintroduce the legacy DB as a camera source through convenience.
- For a video-only canary, generate a minimal new 2.0 DB containing only the explicitly selected
  active YAML streams. Omit legacy ONVIF metadata, layouts, jobs, backup, and alert state rather
  than guessing or merging them.
- Keep the YAML capture read-only and secret-safe: record file hashes and selected stream-name/
  URL fingerprints, never raw producer URLs in logs, manifests, or documentation.

## 2026-08-09 — Separate container-internal media ports from required ingress

- A bridge-networked all-in-one container may keep camstationd, go2rtc API, RTSP, and WebRTC on
  their normal internal ports without publishing each one on the host.
- CamStation's same-origin MSE/WebSocket player is carried through the public HTTP `/player`
  reverse proxy, so an HTTP-only canary does not need a host RTSP, go2rtc API, or ICE mapping.
- Direct WebRTC media is the explicit exception: it needs a reachable ICE listener. When the
  operator accepts video-only MSE validation, do not publish an unused WebRTC port or mistake an
  automatic five-second WebRTC-to-MSE fallback for direct WebRTC success.

## 2026-08-09 — Report fleet counts by site before canary work

- A fleet-wide phrase such as “eight active cameras” is ambiguous when the operator is explicitly
  separating home cameras from fire-station cameras. Always break the baseline down by site and
  state which generation reported it.
- Keep the existing 1.0 operating state distinct from the 2.0 canary selection: observing an
  enabled legacy camera does not mean the canary enabled or contacted it.
- Express canary selection as a positive `집-` allowlist, not as a fire-station-only denylist;
  this also excludes the goat-farm camera and any future non-home entry by default.
- Before any canary start, re-prove that the canary container and DB are absent and state exactly
  what has merely been staged versus what is running.

## 2026-08-09 — Bind canary ingress to the operator's access network

- A technically reachable CCTV-side address is not automatically the address the operator will
  use. Confirm the requested interface before starting the container and bind only that address.
- For this retained canary, publish only `10.0.0.26:18081/tcp`; keep all internal go2rtc ports
  unpublished and report the exact URL only after runtime and continuity gates pass.

## 2026-08-09 — Treat the operator's Viewer route as a separate product surface

- Do not infer that a responsive management route such as `/live` is the mobile Viewer merely
  because it contains video tiles or accepts a `viewer=1` query. Route semantics must be verified
  against the production surface the operator actually uses.
- For this system, the operator-designated 1.0 `/viewer` contract is a chrome-free, full-viewport,
  read-only camera layout that starts all visible MSE streams immediately. It is materially
  different from the 2.0 live operations workspace with navigation, layout editing, side panels,
  PTZ controls, and timeline.
- Validate Viewer parity using the exact route at a mobile viewport: inspect visible UI, overflow,
  video count, ready state/current-time advancement, transport, tile interaction, and direct-page
  reload. A successful desktop `/live` check does not satisfy this gate.

## 2026-08-09 — Normalize Viewer counts by open pages before diagnosing a leak

- A three-camera Viewer creates three downstream media consumers per open page. Ask for or observe
  the number of simultaneously open pages before treating an aggregate `viewerCount` above three
  as a reconnect storm.
- Compare the expected baseline `open pages × visible cameras` with per-stream counts, then watch
  whether excess consumers drain after reloads. Transient stale sockets and continuously growing
  consumers are different failure modes.
- Correlate excess count with CPU, PID count, browser playback state, and connection age before
  changing retry behavior or container limits.

## 2026-08-09 — Size task limits for the final fleet, not the canary subset

- A three-camera canary proves behavior but does not define production capacity when the real fleet
  has eight cameras. Extrapolate recorder and live-transcoder threads to the final camera count and
  include focus and reconnect headroom.
- Container PID counters include threads. Inspect cgroup `pids.peak` and `pids.events`; quiet app
  logs do not disprove PID exhaustion when the kernel has rejected task creation.
- Preserve camera-safety scope while correcting capacity: raising a cgroup limit does not authorize
  enabling excluded cameras or contacting upstreams outside the positive allowlist.

## 2026-08-09 — Separate installer-owned payload from upgrade remnants

- A directory file count cannot by itself classify an MSI installation as corrupt. Resolve the
  cached MSI Directory, Component, and File tables, then compare every owned path and expected size
  before treating extra files as missing or damaged installation content.
- Record key-binary hashes, signature state, root ACLs, product state, service registration, and
  package provenance separately. Complete payload placement does not make an unsigned development
  package production-approved.
- Installed and service-running do not mean cutover-ready. Verify the active endpoint, auto-start
  setting, interactive process, server registration/heartbeat, renderer state, and visible playback
  while preserving the currently operating client as rollback.

## 2026-08-09 — Treat functional client defects separately from install integrity

- A complete MSI payload and healthy service do not disprove an operator-observed application bug.
  Installation integrity answers whether the package landed correctly; only a newer, identified
  build plus reproduction-focused interactive testing can answer whether the bug is fixed.
- Before a “latest version” reinstall, compare upstream and local source, assign a version greater
  than the installed product, and verify artifact hashes on both sides. Never spend the maintenance
  window reinstalling the exact same package under an ambiguous label.

## 2026-08-09 — Keep MSI production off the monitoring workstation

- A monitoring workstation is an installation and maintenance target, not a convenient build host.
  Do not stage compilers, SDKs, package restores, or installer source there even when administrative
  access and disk capacity make it technically possible.
- For this WiX 6 package, build and sign on a dedicated Windows VM or CI runner. Transfer only the
  completed MSI plus its hash/signature evidence to the restricted NUC maintenance staging area.
- When the operator corrects the boundary, stop before installation, remove only the exact temporary
  build stage, and prove the installed client, service, and legacy monitoring session are unchanged.

## 2026-08-09 — Separate build-path readiness from artifact readiness

- A Linux host can validate Viewer tests, Windows Electron packaging, a cross-compiled service,
  source policy, and PowerShell syntax, but those checks do not prove that WiX produced a valid MSI.
- Report the repository entry point as prepared while keeping the real-Windows build, MSI database
  inspection, signature state, and lifecycle tests as an explicit open gate.
- Do not substitute Wine or a monitoring workstation for the missing Windows build environment.
  Designate a dedicated Windows VM or CI runner, then retain its version, hash, source commit, dirty
  state, and tool versions as artifact provenance.

## 2026-08-10 — Clean live recording stores through finalized-ID snapshots

- A recording cleanup against active one-minute workers is a moving target. Capture an exact
  finalized-ID cutoff, delete only that snapshot through the application's checked delete path,
  and expect newly finalized rows while the sweep runs.
- Prove the database/file set is one-to-one before deletion: canonical managed-root containment,
  row/file count, byte size, missing/extra files, and representative ffprobe results. Do not use a
  recursive filesystem deletion when the application maintains recording tombstones.
- After the first sweep, repeat only bounded guarded snapshots until a zero-ready checkpoint is
  observed; never include `recording`/`finalizing` rows or active temp files. State clearly that new
  files will recur unless recording is separately disabled.
- Recovery status comes from backup evidence, not assumption. An operator-deleted file with
  `backup_state=pending`, no backed-up timestamp, and no trash/quarantine copy is not recoverable
  through CamStation even though its audit row remains.

## 2026-08-10 — Separate development-host access from monitoring-host maintenance

- Give a dedicated Windows build PC its own local maintenance principal and host-specific key;
  never reuse the monitoring NUC account, key, or broader access policy for development work.
- Minimize the access surface, not merely the number of script lines: bind sshd to the authorized
  target address, restrict TCP/22 and `AllowUsers` to the independently resolved maintenance source,
  require public-key authentication, and disable forwarding.
- An operator-run bootstrap is only staged access. Record the returned server host-key fingerprint,
  pin it independently, and prove the intended administrative identity before reporting that the PC
  is controllable or installing build tools.
- Existing SSH services, authorized keys, authentication policy, or competing port-22 firewall rules
  are ownership boundaries. Stop for review instead of merging them into an automated bootstrap.
- When the operator says file transfer is unavailable, deliver the first-stage bootstrap directly as
  one pasteable PowerShell block. Establish only a source-restricted key path first; inspect and
  harden `sshd_config` from the verified session instead of making the operator transport a full
  maintenance package before access exists.
- On Windows PowerShell over SSH, avoid per-rule firewall joins across the full rule set: they can
  leave expensive orphaned diagnostic processes if the transport times out. Query protocol filters
  first, narrow by local port, then resolve the associated rule; if cleanup is needed, inspect
  session, parent PID, and command line and terminate only the exact diagnostics created by the task.

## 2026-08-10 — Keep remote Windows bootstrap output separate from control values

- In Windows PowerShell, every success-stream message emitted inside a function becomes part of its
  return value. A helper that both prints download progress and returns an archive path can therefore
  pass a malformed array to `tar`; use `Write-Host`/the information stream for progress, or make the
  function return only one typed control value.
- Windows PowerShell 5.1 recursively unwraps nested arrays in the success pipeline. Do not encode a
  table of validation tuples as nested arrays and rely on row boundaries; use objects with named
  properties or perform explicit scalar checks before any extraction.
- After a remote setup script stops, inspect the bounded destination before retrying. Resume only
  after proving that no partially extracted tool directory was promoted into the versioned tools
  root.
- Avoid nested Bash → SSH → PowerShell `-Command` quoting for scripts that contain PowerShell
  strings or variables. Send a literal script block to `pwsh -File -` over standard input so error
  policy, paths, and comparison values retain their exact meaning.
- Normalize `CRLF` only at the text-comparison boundary when comparing Windows-generated manifests
  with Linux output. A raw manifest hash can differ solely because PowerShell emits `CRLF`, even
  when every recorded file digest and path is identical; never rewrite the transferred source to
  make the diagnostic match.
- Parenthesize both operands when combining PowerShell's `-join` operator with comparisons such as
  `-ne`. Without explicit grouping, operator precedence can compare the wrong expression and make
  an exact artifact-name set appear invalid.
- Treat a missing JSON property as a verification failure, not as an empty report field. Inspect the
  artifact schema first and assert every required field is present and non-null before publishing a
  summary; PowerShell otherwise returns `$null` for a misspelled or wrongly nested property without
  stopping the script.

## 2026-08-10 — Exercise native Windows paths before declaring Viewer packaging ready

- A Unix-domain-socket test path such as `service.sock` is not a valid substitute for a Windows
  named pipe on native Windows. Integration tests for the Viewer management channel must select a
  unique `\\.\pipe\...` endpoint on Windows and retain a temporary Unix socket only elsewhere.
- npm scripts intended for native Windows must not depend on POSIX `rm` or `mv`. Put filesystem
  preparation/finalization in a small Node script so the same locked command runs under `cmd.exe`
  and Unix shells.
- `@electron/asar` reports archive entries with the host separator (`\\` on Windows). Normalize its
  returned entries to one leading slash and `/` separators before required/leaked-file checks;
  otherwise a valid Windows package is falsely reported as missing its runtime files.
- A per-machine MSI must not mix non-advertised profile shortcuts with a file-keyed machine
  component. Keep the executable as the file KeyPath, make its all-users shortcuts advertised, and
  author an uninstall `RemoveFolder` row for the product-created Start Menu directory; keep ICE43,
  ICE57, and ICE64 enabled so this boundary is proved by real Windows Installer validation.
- Windows Installer database SQL is deliberately restricted and does not support a normal
  `SELECT COUNT(*)` aggregate. For an inspected table count, select its primary-key column and count
  successive COM `Fetch()` records; an unsupported query can surface as an unhelpful COM type or
  dispatch error after an otherwise valid MSI build.
- Windows Installer automation accepts `OpenDatabase(path, 0)` when the mode is marshalled as a
  signed `Int32`, while a `UInt32` variant reproduces `DISP_E_TYPEMISMATCH`. Cast COM paths, open
  modes, SQL strings, and record indices explicitly instead of relying on PowerShell's implicit
  automation marshalling.
- Closing query views is not enough to release an MSI file handle: the Windows Installer COM
  `Database` remains open until its RCW is released. Initialize the COM references before the build
  try block and call `FinalReleaseComObject` for Database then Installer in `finally` before deleting
  the exact temporary workspace, so a successful artifact never returns a cleanup failure.
- A native npm addon may be present and hash-correct yet fail with `ERR_DLOPEN_FAILED` when its PE
  imports are unavailable. Inspect the binary imports and system DLL evidence before blaming npm's
  optional-dependency warning. On a dedicated x64 Windows build host, install Microsoft's current
  signed x64 Visual C++ Redistributable, record its installer hash/version/exit code, suppress
  restart, and prove the exact native import before retrying the build.
- Every operational named-pipe probe must bound both connect and response reads. Use an asynchronous
  read with a timeout and dispose the pipe on timeout; a plain `ReadLine()` can strand the SSH child
  process even after the local transport is interrupted.
- Do not use an SSH session to prove a desktop-only Viewer management pipe. The service pipe
  intentionally denies the Windows Network SID and permits interactive users, administrators, and
  SYSTEM under its reviewed ACL; preserve that boundary and run UI/pipe acceptance from an actual
  interactive desktop token.
- Windows service and MSI verification must not depend on localized display text. Prefer process
  exit codes, event IDs, registered values, invariant engine markers, and numeric recovery settings;
  Korean `sc.exe` text can also be mojibake when captured through a differently encoded SSH stream.

## 2026-08-10 — Remote GUI development requires an interactive evidence loop

- Administrative SSH proves installation and service state, but it does not prove what an Electron
  window renders or whether real keyboard focus works in another user's RDP session. Never present
  command-line health as desktop acceptance.
- Do not make the operator act as the agent's camera by repeatedly supplying screenshots. Establish
  a session-aware loop that can launch the target, capture only its window, collect bounded UIA
  metadata, apply an intentional input action, and return fresh evidence over the existing secured
  transport.
- Prefer a passwordless, one-shot `TASK_LOGON_INTERACTIVE_TOKEN` task in the already logged-on test
  user's session over a new VNC server, listening port, stored RDP credential, or weakened named-pipe
  ACL. Use a unique task name, least-privileged interactive token, bounded execution, exact target
  process, restricted evidence directory, and guaranteed task deletion.
- A full-desktop screenshot can expose unrelated windows. Default GUI evidence to the verified
  CamStation Viewer window rectangle and record the user/session/process identity with every image.
- Never carry patch-marker `+` prefixes into a literal remote here-string. Require an explicit
  success sentinel before treating a remote validation payload as executed.
- Electron's UIA tree can lag behind the first rendered frame. If a first bounded scan misses edit
  controls, repeat a capture after the renderer settles before declaring UIA unavailable or falling
  back to coordinates.

## 2026-08-10 — Keep committed tests independent of local evidence

- Before cleanup, trace every test fixture back to its source. A test that reads an untracked
  `work/` artifact can pass in the active workspace and fail in a clean clone.
- Promote reusable maintenance scripts into a reviewed source directory, point tests there, and
  keep raw screenshots, runtime evidence, known-host files, and operator records outside Git.

## 2026-08-10 — Reconcile long-running work before handing off commits

- For a long dirty session, inventory tracked, untracked, ignored, and upstream state before staging.
  Split completed work by responsibility and inspect every staged name/stat/check result.
- If upstream gained overlapping work, first secure the local work in logical commits, then fetch and
  reconcile explicitly. Keep the implementation proven against the real runtime, retain useful
  upstream tests, and remove duplicate or superseded source and plans.
- Embedded frontend assets are derived from resolved source. Rebuild them after conflict resolution
  and confirm the expected content hashes before the final full-suite verification.

## 2026-08-10 — Preserve remote GUI knowledge as a repository skill

- A proven remote GUI evidence path should not remain only in chat history or an operator's memory.
  Register a repository-scoped skill under `.agents/skills` so later project sessions discover the
  same procedure from the repository.
- Keep operational code canonical in the reviewed project scripts and make the skill a narrow
  decision/runbook layer. Copying PowerShell into skill resources creates two implementations that
  can silently diverge.
- GUI verification instructions must require direct image inspection plus bounded UIA evidence;
  installation, service state, process existence, and nonempty screenshot files are not substitutes
  for seeing the rendered window.
- Put Korean and English task phrases in the skill description when the project's operator language
  is Korean. Protect the trigger metadata, exact-window boundary, artifact integrity, and cleanup
  rules with a source-policy test that also rejects embedded environment IPs, key fingerprints, and
  private-key material.

## 2026-08-10 — Publish operator downloads through the settings surface

- When the operator asks to download a client from the 2.0 server, a working API endpoint alone is
  incomplete. The canonical operator entry point is `/settings`; verify the real rendered page and
  require a visible download action backed by the same release metadata and artifact hash.
- Inspect the live page before adding UI. The source may already contain the correct card while the
  deployed release catalog is empty; in that case publish and verify the artifact instead of
  duplicating the component or inventing a second download route.
- Define completion by the operator journey: settings-page download, Windows installation, Viewer
  launch, server connection, and live monitoring. Do not confuse the installed application EXE with
  the distributable installer. The current standard package is the MSI and must install the Viewer
  EXE, service, shortcuts, and uninstall registration as one lifecycle-owned product; reviving the
  rejected custom Setup EXE or publishing a bare application EXE would not satisfy that journey.
- For an optional Windows registry value, read the parent key and inspect
  `PSObject.Properties[name]`. `Get-ItemPropertyValue -ErrorAction SilentlyContinue` can still emit a
  localized missing-property diagnostic even when absence is the expected success state, making a
  successful cleanup appear failed.
- In nested SSH shell validation, avoid `$1`-based `awk` snippets under a remote `set -u` unless the
  quoting boundary is proven. Prefer shell parameter trimming or a local parser so validation does
  not fail after the underlying publication already succeeded.
