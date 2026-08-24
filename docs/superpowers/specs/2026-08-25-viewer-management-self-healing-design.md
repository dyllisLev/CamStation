# Viewer Management Self-Healing Design

**Date:** 2026-08-25
**Status:** Approved for implementation by operator request

## Goal

Keep an already-playing eight-camera Viewer visible while repairing loss of the local Viewer-to-Service
management plane, and make every server/watchdog health claim freshness-aware. A management failure must not
masquerade as healthy, wait forever, or consume playback recovery budgets.

## Confirmed Runtime Evidence

- Three official exact-window captures and one full-desktop capture showed all eight camera images and embedded
  camera clocks advancing. Viewer Service remained `Running` and the interactive session remained `Active`.
- In the same observation window the Service-to-server heartbeat was 2–10 seconds old, while Viewer lease
  heartbeat, renderer heartbeat, stream `updatedAt`, and media `lastProgressAt` were stale by the same
  105–113 seconds and continued aging together.
- The server watcher therefore reported Viewer online/healthy 1/1 but receiving 0/8. Its installed hash and
  aggregation logic match the repository; the contradiction is an input/status-contract defect, not a watcher
  file mismatch.
- The exact Viewer process group was alive in session 1 and the window remained responsive. The maintenance
  identity was correctly denied direct named-pipe status access by the installed ACL, so the investigation did
  not weaken or bypass that boundary.
- Bounded local logs contained playback retry/cooldown records but no management request timeout, pending count,
  lease-expiry, or reconnect-generation evidence. Those failure transitions are currently unobservable.

## Confirmed Defect Points

1. `ManagementConnection.request` has no deadline. A half-open named-pipe request remains in the pending map
   indefinitely and does not force a new connection generation.
2. Viewer heartbeat and renderer/stream reports discard rejected promises. Application errors and timeouts do
   not converge on the existing disconnect/reconnect path.
3. Reconnect is triggered only by socket `error`/`close`. A silent peer, missing response, or expired lease with
   no subsequent response can leave an open but useless connection.
4. The five-second Viewer heartbeat uses a 15-second Service lease, but expiry is lazy and has no owner-expiry
   callback. Cached `running`, `ready`, and stream rows survive expiry until a physical pipe disconnect.
5. The current disconnect path loads the setup document before reconnecting. That destroys a still-playing live
   document for a management-only failure and couples control recovery to media disruption.
6. Service heartbeat payload and watcher `healthy` use cached state enums without requiring recent Viewer and
   renderer heartbeat timestamps. The Service can remain online while the actual Viewer control lease is blind.
7. Media truth used `timeupdate/currentTime` as its only presented-frame signal. The separate
   `requestVideoFrameCallback` and Electron background-throttling hardening remain necessary, but cannot repair
   the management plane.

## Selected Design

### Bounded request generation

Every management request has a five-second deadline. Timeout fails all pending requests, closes that exact
socket, and emits one disconnect notification for the connection generation. Response, close, error, timeout,
and explicit close all clear their own timers; late responses cannot revive a closed generation.

The heartbeat remains the authoritative connection probe. A failed heartbeat converges on socket teardown.
Renderer reports remain best-effort for playback isolation, but their request deadlines also prevent an
unbounded pending map.

### Preserve live while reconnecting

When a live document is already displayed, a management disconnect stops the old heartbeat, invalidates its
lease, and schedules the existing bounded 1/2/5/10/30-second reconnect sequence without navigating the window.
A successful connection acquires a fresh lease and reattaches the existing renderer bridge. It reloads only
when there is no live document or the configured live URL genuinely changed.

Initial setup, explicit shutdown, duplicate direct launch, and background reconnect remain distinct paths. A
temporary `lease_busy` during replacement causes another bounded reconnect; it never quits the active Viewer.

### Service-owned lease expiry

Lease expiry reports the exact owner connection once. The Service marks Viewer and renderer `unresponsive`,
retains bounded last-seen timestamps for diagnosis, and closes only that owner pipe. Physical disconnect then
performs ordinary cleanup. A later valid lease plus renderer reports restores `running`/`ready`.

Expiration state is committed under the LeaseManager mutex, but external notification runs after the mutex is
released. The Server separately tracks the active pipe generation, so a delayed expiry or disconnect from an
old owner cannot overwrite a replacement Viewer's state.

No broad process termination or automatic Viewer restart is added here. If the Electron main process never
resumes after exact pipe closure, the Service reports it truthfully and a separate bounded process-supervision
policy can escalate using its existing restart budget.

### Freshness-derived health

The Service heartbeat payload downgrades cached `running`/`ready` after 15 seconds without an accepted Viewer or
renderer heartbeat. The production watcher independently requires both component timestamps within 30 seconds
before incrementing `healthy`, and emits distinct Viewer-heartbeat and renderer-heartbeat stale alerts. Media
reception keeps its existing 90-second per-stream threshold.

An unexpected active-lease disconnect and lease expiry are `warn` events in the Service log. If the Electron
process survives, it retains only a bounded failure class, start time, and retry count in memory and writes a
`management_recovered` warning after the next lease succeeds. Raw OS errors and pipe paths are never retained.

## Safety and Concurrency

- One connection generation owns its timers, pending requests, disconnect callback, and lease.
- One lease owner expiry closes only its recorded pipe; unrelated read-only/status connections are untouched.
- Lease callbacks never hold the LeaseManager mutex while closing a pipe or taking a Server/Service lock.
- Reconnect scheduling is single-flight and generation-checked. Old callbacks cannot send a heartbeat through a
  new connection or reload a newer document.
- Management recovery does not reset playback recovery, resubscribe streams, restart the Viewer, or mutate
  camera/server settings.
- Existing named-pipe ACL, explicit Windows target profile, no-listener/firewall boundary, and secret filtering
  remain unchanged.

## Acceptance

- A silent request rejects within five seconds, clears every pending request, and notifies disconnect once.
- An expired lease closes only its owner and publishes `unresponsive` without erasing last-seen timestamps.
- A live Viewer survives management disconnect without navigation; it acquires a new lease within 30 seconds
  after the Service becomes responsive.
- Viewer/renderer timestamps older than 30 seconds cannot count as watcher healthy.
- Healthy operation keeps all eight streams progressing and produces no management reconnect or playback
  recovery side effect.

## Out of Scope

- Camera, go2rtc, recorder, network, or stream policy changes
- Broad process killing or unconditional Viewer/Service restart
- Weakening named-pipe ACLs or adding a remote-control listener
- Production rollout before the Windows artifact and server image pass their existing release gates
