# Viewer Management Self-Healing Implementation Plan

## 1. Failure-first tests

- Add Viewer management-pipe tests for a silent response deadline, all-pending rejection, late response
  rejection, application-level heartbeat failure, and exactly-once disconnect notification.
- Add lifecycle tests proving a visible live document is preserved across management loss and same-URL lease
  reacquisition, while initial setup and explicit shutdown retain their existing behavior.
- Add LeaseManager/Server tests for exact-owner expiration, one expiry notification, state downgrade, timestamp
  preservation, and subsequent lease recovery.
- Add Service heartbeat and Python watcher tests for stale Viewer/renderer timestamps and independent media
  freshness.

## 2. Viewer connection boundary

- Give `ManagementConnection` a bounded connect/request deadline and timer-owning pending entries.
- Centralize terminal failure so timeout/error/close reject all pending calls and notify once.
- Make heartbeat failure fatal to its connection generation; keep renderer telemetry best-effort and bounded.

## 3. Non-disruptive reconnect

- Stop the old heartbeat and invalidate globals before reconnect.
- Preserve a verified same-URL live document during reconnect attempts.
- Acquire a fresh lease, restart the heartbeat, report Viewer/renderer state, and let the existing renderer
  telemetry bridge resume without page reload.
- Keep the current bounded reconnect cadence and prevent duplicate timers or stale callbacks.

## 4. Service and watcher truth

- Add exact-owner lease-expiry notification and close only that Service connection.
- Publish stale cached Viewer/renderer states as `unresponsive` while retaining last-seen evidence.
- Require fresh Viewer and renderer heartbeat timestamps for watcher healthy; emit separate bounded alert codes.

## 5. Verification and rollout gate

- Run focused RED/GREEN tests, full Viewer/Web/Python/Go suites, lint, production builds, and `git diff --check`.
- Do not claim Go verification when the current environment lacks a Go toolchain; use only an existing approved
  reproducible build path, without installing tools or weakening dependency verification.
- Before any rollout, verify immutable artifact identity and rollback. After an authorized rollout, require an
  official Viewer capture, fresh lease/renderer/stream timestamps, three one-minute watcher samples at 8/8,
  Service Running, and zero Windows task/listener/firewall residue.
