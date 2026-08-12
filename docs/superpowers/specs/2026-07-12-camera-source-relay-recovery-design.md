# Camera Source Relay Recovery Design

## Goal

Make every preloaded camera input recover automatically after source silence or disconnect, while preserving fast normal startup and the existing recording/live/focus policy model.

## Root Cause

The current private inputs use go2rtc's native protocol clients. On `cctv2`, 소방서1 and 소방서5 initially connect and stream, then their RTSP reads time out. The preloaded producer remains in a stale connection state, later attempts report `start from CONN state`, and dependent FFmpeg outputs terminate with EOF. Other inputs can encounter the same lifecycle failure even though it has only been reproduced on these two cameras.

## Selected Design

Render every private camera input through an FFmpeg copy relay. RTSP inputs use:

```text
ffmpeg:<private-camera-url>#video=copy#audio=copy#timeout=5
```

Non-RTSP inputs, including Reolink HTTP-FLV, use the same copy relay without an RTSP-only timeout parameter:

```text
ffmpeg:<private-camera-url>#video=copy#audio=copy
```

The relay copies encoded tracks without video re-encoding. Existing private-source preload keeps live relays active. If a source disconnects or reaches the RTSP five-second input timeout, FFmpeg exits; go2rtc can then start a fresh relay process for the still-preloaded source instead of reusing a stale native connection object.

The five-second value is a failure-detection timeout, not a startup delay. A healthy stream starts normally. A failed RTSP stream should be detected in at most approximately five seconds, followed by the camera's reconnect time and go2rtc retry interval.

## Scope

- Apply the relay uniformly to every persisted camera input source.
- Add `timeout=5` only when the stored private URL scheme is RTSP or RTSPS.
- Preserve other protocols and query parameters unchanged inside the private FFmpeg source.
- Preserve the source URL and credentials only inside the mode-0600 generated go2rtc config.
- Preserve public output names, desired/applied policy snapshots, activation modes, short-GOP H.264 output settings, recorder handoff, and last-good rollback.
- Keep public transform FFmpeg outputs on demand.
- Do not add a CamStation watchdog or restart all of go2rtc when one camera stalls.

## Error Handling

- RTSP copy relays use a fixed five-second input timeout.
- HTTP-FLV and other copy relays restart on EOF or process failure without receiving an invalid RTSP-only timeout option.
- Relay termination is handled by go2rtc's existing producer retry lifecycle while preload remains active.
- A repeated reconnect failure remains visible as a degraded/no-producer runtime state and redacted warning log.
- No raw URL, credential, internal alias, or FFmpeg command is returned through public APIs or committed runtime evidence.

## Verification

Automated tests must prove:

- RTSP and RTSPS private sources render one FFmpeg copy relay with video copy, audio copy, and a five-second timeout;
- HTTP-FLV sources render one FFmpeg copy relay without the RTSP-only timeout parameter;
- generated private credentials remain confined to the private config;
- public output producer selection and short-GOP template remain unchanged.

Runtime verification on `cctv2` must use `scripts/camstationctl.sh`, confirm all eight preloaded private inputs and browser live outputs recover after restart, and observe byte counters increasing for every source. A bounded fault test must terminate one managed source-relay child without stopping camstationd/go2rtc and confirm go2rtc creates a replacement relay and resumes bytes within the recovery window. The legacy `cctv` server and camera configuration must not be changed.

## Non-Goals

- Re-encoding the private source relay
- Adding hardware acceleration
- Changing camera profiles, credentials, ports, or source URLs
- Restarting go2rtc or all cameras when one relay exits
- Adding a user-facing timeout setting before a measured need exists
