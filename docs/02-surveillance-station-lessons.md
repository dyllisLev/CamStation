# Surveillance Station Lessons

## Analysis Boundary

Synology Surveillance Station packages were only used for high-level comparison and public/observable behavior. No sensitive internal implementation, licensing, or proprietary logic was reverse engineered.

## Main Lesson

Surveillance Station treats camera connection as an operational system, not a single RTSP URL.

The useful ideas to bring into CamStation 2.0 are:

- camera profiles
- transport policy
- fallback strategy
- RTSP keepalive
- frame time correction
- connection state machine
- incident and alert dampening
- device/model-specific defaults

## Camera Profiles

CamStation 2.0 should have camera profiles such as:

- generic
- imou
- hikvision
- dahua
- reolink
- vstarcam

Profiles can define:

- default main stream path
- default sub stream path
- preferred transport
- keepalive method
- ONVIF behavior
- known quirks

This does not need to become a huge device pack immediately. A simple editable profile registry is enough.

## Transport Policy

Camera connection settings should support:

- `auto`
- `tcp`
- `udp`
- `http`

For `auto`, the connection engine should remember recent failures and choose a safer fallback.

## Keepalive Policy

RTSP keepalive should be configurable per camera:

- `OPTIONS`
- `GET_PARAMETER`
- `none`

The UI should show:

- last keepalive sent
- last keepalive response
- current keepalive mode
- recent keepalive failures

## State Machine

Camera state should be explicit:

- `disabled`
- `connecting`
- `streaming`
- `degraded`
- `reconnecting`
- `offline`
- `recovering`

This state should drive both UI and alerting.

## Alert Model

Avoid one alert per transient error.

Use incident behavior:

- open after sustained failure
- mark flapping after repeated recovery/failure cycles
- resolve only after stable recovery
- allow acknowledge and snooze
- show incident history in the web console

