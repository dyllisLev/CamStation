# Common Browser Live Output Profile Design

## Goal

Make every browser live tile start predictably without depending on each camera's native GOP, while keeping server load bounded on the four CPU cores available to CamStation.

## Current Evidence

- All eight live tiles open independent WebSocket/MSE connections concurrently.
- `fire-station-1-live` copies the camera H.264 stream and delivered keyframes 6.36 and 8.14 seconds apart in wall-clock measurements.
- The other seven live outputs currently resolve to server-side H.264 transcoding.
- With eight browser consumers connected, go2rtc had 16 FFmpeg children using about 241% aggregate CPU and 779 MiB RSS.
- Two uncapped 1080p live encoders each used close to one CPU core.

## Chosen Approach

Define one browser-live output contract and use the existing per-camera policy machinery to realize it. Do not add a global-settings subsystem or a new dependency.

The common live output settings are:

| Field | Value |
|---|---|
| Purpose | `live` |
| Source | Preserve each camera's selected live source |
| Video mode | `h264` |
| Maximum dimensions | `640x360` |
| Maximum frame rate | `15` |
| Audio | `none` |
| Activation | `always` |
| Encoder GOP | Existing server default, 20 frames |

Recording and focus output policies remain unchanged.

## Product Behavior

The frontend registration form and server-side fallback defaults must produce the same common live profile for newly registered cameras. Existing cameras are updated once through the normal policy update path so revision checks, validation, probing, applied snapshots, and last-good rollback remain intact.

The live output stays warm even when no browser is connected. A browser therefore joins an already-running short-GOP server stream instead of waiting for a camera-native keyframe or a cold transcoder.

## Application and Rollback

Before changing existing cameras, capture their complete desired output policies. Apply the live changes while preserving recording and focus settings and source selection.

After apply:

1. Confirm all eight live outputs are `running`, have one producer, and are verified as transcoding H.264 at no more than 640x360 and 15 fps.
2. Observe aggregate FFmpeg CPU once per second for 60 seconds.
3. Keep the common profile only if the 60-second average is at most 280% CPU, representing 70% of the four available cores.
4. If the threshold is exceeded or runtime verification fails, restore every captured policy through the same policy path and verify the restored runtime.

## Verification

- Store tests cover the server fallback defaults.
- Frontend model tests cover registration defaults.
- Stream-policy tests confirm forced H.264, size/FPS caps, audio removal, and always-on preload generation.
- `go test ./...`, frontend tests, frontend lint/build, and the Go build must pass.
- Runtime evidence must include public API state, sanitized generated configuration behavior, FFmpeg process/CPU measurements, and a fresh live-page reconnect observation.

## Non-Goals

- No camera firmware or camera-side GOP changes.
- No new global settings screen.
- No changes to recording or focus output behavior.
- No generic adaptive transcoder pool or idle grace-period feature.
