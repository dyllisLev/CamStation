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
- Full Go tests, Web tests, lint and production builds pass. Focused
  tests preserve camera registration/security and all four playback query
  contracts. The DB change is deployed; final rollout with the original frontend
  policy and final external/browser/Viewer observations are pending.

The measured reproduction and bounded query experiments are recorded in the
task's local evidence folder. No camera settings, original recording bytes or
running recording files were changed during diagnosis.
