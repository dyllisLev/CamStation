# Live startup latency — 2026-09-10

The reported failure is a fresh browser or Windows Viewer connection that waits
for video and repeatedly shows reconnecting. An already playing monitoring PC
does not establish that new connections work. Completion requires actual public
domain access from a separate external host, in addition to LAN and Viewer checks.

## Reproduction and cause

- At 09:40 KST, a fresh Chrome on asura loaded the public live document in 179 ms,
  but none of the eight videos presented a frame within the 40-second observation.
  Setup attempts failed at the five-second deadline before `socket_open`.
- OCI independently completed TLS in 42 ms, while the valid-origin WebSocket
  upgrade took 5,953 ms. Developer-host public and LAN browser sessions also
  failed, so this is not only an external TLS/proxy issue.
- Playback timeline queries joined and grouped every camera's published recording
  fragments before filtering the requested camera. Each eight-camera workspace
  polls sixteen such queries every five seconds. On a representative archive with
  11,157 segments and 120,000 fragments, the actual modernc SQLite driver took
  5.21 seconds per poll. The daemon uses one DB connection; new WebSocket approval
  also performed full camera/policy hydration through that connection.
- Existing streams bypass those repeated registration queries after their initial
  upgrade, explaining why the monitoring PC could continue showing all eight
  streams while fresh connections waited. The server was not at its total CPU
  allocation, although the daemon was using roughly one full core.
- Explicit WebRTC candidates are private, but actual external signaling also
  advertises public server-reflexive candidates. An asura probe after the DB fix
  connected in 261 ms and presented video in 792 ms. The same external
  eight-camera workspace with the original WebRTC-first frontend presented all
  videos within 2.20 seconds without retries. Explicit private configuration alone
  therefore does not establish an external transport failure.

## Changes and validation

- Restrict the common playback CTE to its camera before joining fragments. The
  requested window still filters complete file spans afterward, preserving seek,
  pagination, fragment extrema and uncommitted-data exclusion. The same modernc
  eight-camera workload now takes 0.63 seconds, an 8.3-fold improvement.
- Check enabled public stream registration with one SQL statement, preserving
  effective output names, legacy fallback names and preview/origin restrictions.
  The DB remains canonical; no cache, pool or timeout expansion is involved.
- The original WebRTC-first/MSE-recovery policy and five-second setup limit are
  retained for both browser and Viewer. Actual external transport comparison
  confirmed the DB fix is sufficient for successful first connections.
- Full Go tests, Web tests (101/101), lint and production builds pass. Focused
  tests preserve camera registration/security and all four playback query
  contracts. Final revision `49c84f05f602b367e106ede4dc6138467a1a5d98` started at
  09:57:19 KST. Exact CI run 49 succeeded, OpenShip deployment
  `dep_fXRAfWOlYn0LYbVX` is ready, and runtime revision/image/health match with
  restart count zero. The final frontend source and embedded assets match the
  original pre-fix frontend.

## Final operating observations

| Check | Observed result |
| --- | --- |
| asura, unmodified public `/live`, fresh Chrome at 09:57:48 KST | First presented frame 0.896 s; all eight 2.666 s from navigation start; eight WebRTC attempts, eight successes, zero retries/errors; actual screenshot inspected |
| OCI public WebSocket, valid Origin | TLS plus upgrade 5,953 ms before the DB fix, 91 ms afterward; HTTP 101. This is a handshake measurement, not an OCI video decode test |
| Monitoring PC, fresh native page load at 09:58:29 KST | All eight started on first WebRTC attempt, 589–1,527 ms from each connection attempt; zero setup failures; actual Viewer screenshot inspected |
| Archive and server state | Eight old active files finalized and passed ffprobe; eight new files and indexes advanced. DB quick check passed, zero foreign-key violations, unchanged persistent mounts, live 8/8, NVENC 2 |
| Camera list | 59 ms in the final operating sample, versus the earlier 12-second timeout |

The final external browser loaded the normal served assets without request or
transport overrides. Earlier controlled WebRTC/MSE comparisons are diagnostic
evidence and are distinct from this final acceptance run. CPU samples dropped
from 93.5% to 16.5% of one core, but those two-second samples did not control for
identical load and are not a capacity benchmark.

The native check used the built-in `reload_live` command (28, succeeded) for the
uniquely identified online `NUC` Viewer. Target `monitoring-pc`, interactive
`NUC\\dyllislev`, session 1 Active, Viewer process 9476 and the maximized layout
were preserved. The exact-window capture ran 09:59:04–09:59:07 KST via PrintWindow,
2576×1408, and was directly inspected. Its local evidence directory is
`work/windows-control-evidence/monitoring-pc/viewer-20260910T005902431Z-012545973a0149bdba849353a06f4aac`;
`viewer-window.png` SHA-256 is
`6376331f8cf925f85f7693f4b6731b96e495b24be7933cbb04fe5d0348a309eb` and the UIA hash is
`144d8caa6b322c23035b4d9cc05efa579824e27f16adf0897dfef624b44f5092`.
Task deletion and exact remote-run removal passed. Final Status took 2,582 ms;
service/session/driver identity were unchanged, all four task counts and driver
TCP/firewall counts were zero, and all canonical script hashes matched.
No Windows package, service configuration, saved server address or camera setting
was changed. Native full-screen and keyboard-focus changes were not exercised.

The measured reproduction and bounded query experiments are recorded in the
task's local evidence folder. No camera settings, original recording bytes or
running recording files were changed during diagnosis.
