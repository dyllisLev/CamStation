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
- WebRTC is configured with private candidates. External transport reachability
  needs separate validation once the DB delay is removed; a fast upgrade alone
  does not prove that WebRTC video can reach an external browser.

## Changes and validation

- Restrict the common playback CTE to its camera before joining fragments. The
  requested window still filters complete file spans afterward, preserving seek,
  pagination, fragment extrema and uncommitted-data exclusion. The same modernc
  eight-camera workload now takes 0.63 seconds, an 8.3-fold improvement.
- Check enabled public stream registration with one SQL statement, preserving
  effective output names, legacy fallback names and preview/origin restrictions.
  The DB remains canonical; no cache, pool or timeout expansion is involved.
- Hostname and public-IP access starts with MSE over the existing HTTP media
  proxy. Direct private, loopback and link-local IP access retains WebRTC first.
  This is an explicit default policy, not an ICE reachability detector: LAN DNS
  names also use MSE. Browser and Viewer use the same selection; recovery paths
  and five-second setup limits are unchanged.
- Full Go tests, Web tests (103/103), lint and production builds pass. Focused
  tests preserve camera registration/security and all four playback query
  contracts. Production rollout and final external/browser/Viewer observations
  are pending.

The measured reproduction and bounded query experiments are recorded in the
task's local evidence folder. No camera settings, original recording bytes or
running recording files were changed during diagnosis.
