# Live Source Lifecycle Recovery Design

## Goal

Restore reliable and fast `/live` startup after the camera stream-policy rollout without removing recording/live/focus policies, browser codec conversion, private source identities, or atomic apply/rollback.

## Root Cause

The policy runtime introduced public recording/live/focus outputs backed by private input aliases. Normal live output now commonly starts an on-demand FFmpeg process, which then opens the private source through local RTSP. Repeated on-demand startup can fail before the private camera producer is available; go2rtc returns a route-level 404 after its input timeout and the browser remains in its reconnect loop.

This was observed during rollout as an unresolved on-demand 404 and is now reproduced through the ordinary `/live` UI. It is independent of the later short-GOP encoder flags.

## Selected Design

Keep the existing policy control plane and change only media-source lifecycle and producer selection:

1. Private camera inputs used by applied live outputs are preloaded by go2rtc. The camera producer is established once at daemon/go2rtc startup and remains shared across public outputs.
2. Public output activation remains independent. `on_demand` still controls expensive transform FFmpeg processes; `always` still preloads the public output itself.
3. Existing public-output producer selection remains unchanged in this recovery. Outputs that currently require H.264 conversion, resize, FPS limiting, audio conversion, or audio removal keep their bounded server-generated FFmpeg pipeline.
4. Recording, live, and focus keep their server-owned public names and continue to share a single private producer for the same source row.

The official go2rtc documentation specifically identifies preload as appropriate for cameras that take a long time to start. Preloading the private input, rather than every transformed output, keeps camera sessions stable without forcing all software encoders to run continuously.

## Preserved Behavior

- SQLite desired/applied revisions and exactly three output policies remain unchanged.
- Input URLs and credentials remain confined to the mode-0600 generated config.
- Public APIs expose only server-owned output names and redacted state.
- `auto`, `copy`, and `h264` video policy semantics remain unchanged.
- Recording continues to consume only the applied recording output.
- The serialized apply coordinator, last-good configuration, rollback, and recorder handoff remain unchanged.
- Focus view continues to suspend the matching normal live consumer.
- Short fixed-GOP settings remain limited to software H.264 outputs.

## Configuration Rendering

The renderer will calculate the set of private sources referenced by applied live outputs and add those private source names to `preload` with `video&audio`. Preloading both available tracks lets a single-source camera share the same producer with later recording/focus consumers without reopening the camera solely to add audio. Existing public-output preload entries for `activation=always` remain.

Preload entries must be deduplicated. They must never contain raw camera URLs or public output aliases in place of the canonical private source identity.

Eliminating FFmpeg from video-only copy outputs is deliberately deferred. It is not required to fix the producer lifecycle failure and would change a second runtime variable in the same recovery.

## Failure Handling

- A temporarily unavailable camera does not roll back the global config; go2rtc keeps the preloaded source registered and retries according to its native source lifecycle.
- The browser retains the existing reconnect behavior, but a healthy preloaded source should already have media available before the MSE WebSocket is opened.
- Runtime status must not report a source as healthy solely from stale policy verification. The live output remains starting/degraded until a producer exists.

## Verification

Automated regression coverage must prove:

- applied live inputs render one deduplicated private-source preload entry;
- public `activation=always` preload behavior remains intact;
- multiple outputs sharing one source do not create multiple private preload entries;
- raw camera URLs and credentials do not enter public names or test diagnostics;
- transform outputs retain their existing FFmpeg policy and short-GOP flags;
- existing direct-copy and FFmpeg output selection is unchanged;
- full Go tests, web build, and daemon build pass.

Runtime verification on `cctv2` must use the managed lifecycle script and record KST evidence for all eight live outputs. Acceptance requires each healthy camera to acquire a producer and render in `/live`, no ordinary reconnect 404, no extra camera connection per recording/live/focus output sharing the same source, and no disruption to the legacy `cctv` server.

## Non-Goals

- Removing the stream-policy editor or database model
- Changing camera credentials, URLs, profiles, or the legacy `cctv` server
- Adding a new streaming dependency or custom retry daemon
- Keeping all software H.264 encoders running when nobody is viewing
- Optimizing video-only copy outputs to remove their existing FFmpeg wrapper
