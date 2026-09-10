# Live recovery latency implementation

Design: [live recovery contract](../specs/2026-09-07-live-recovery-latency-design.md).

1. Replace the fast/slow cooldown policy with a fixed five-second retry wait and
   reset expired recovery state after five seconds of continuous playback.
2. Pace immediate failures, cancel obsolete scheduled callbacks, and reject progress
   when no playback connection is active.
3. Add deterministic state-machine and actual-hook failure/recovery regressions,
   including prolonged outage and successful playback followed by a new stall.
4. Run web tests/lint/build and Go tests/build; reconcile the current design/status.
5. Separately authorize and verify production rollout, including an actual source
   return and Viewer media progress. No camera outage is injected into production
   during the local implementation.

Steps 1–4 completed: 88 web tests, lint, web/daemon builds pass; all Go packages
passed with the Viewer-agent package using a memory-backed test temp directory
after the original ZFS temp-directory run exceeded two short test deadlines.
Step 5 rollout completed with user approval: commit `45c4789`, Forgejo run 29,
OpenShip ready and matching healthy image, preserved DB/mount identity, eight
normally finalized playable pre-deploy segments and eight growing new recordings.
Official Viewer reload and exact-window capture confirm eight playing feeds.
No intentional camera outage was injected; prolonged real-source return timing
remains unmeasured. Live-resolution settings have not been changed.
