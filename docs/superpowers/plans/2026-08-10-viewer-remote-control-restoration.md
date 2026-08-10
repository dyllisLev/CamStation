# Viewer Remote Control Restoration Implementation Plan

**Status:** Completed and WinPC-verified on 2026-08-10

**Goal:** Restore the five approved server-driven Viewer controls in the standard MSI runtime and
prove them on the authorized WinPC.

**Architecture:** Keep the server queue as the visible state authority. Add strict command schemas,
a durable Service-side command engine, explicit management-pipe events/results, narrow Windows
Viewer/SCM lifecycle adapters, and a capability-focused Korean operator form.

## Task 1: Freeze contracts and server validation

- Add table-driven store tests for the five commands, the `restart_agent` alias, irrelevant fields,
  stream-name safety, TTL bounds, operation-key immutability, cancellation after delivery, and exact
  state timestamps.
- Add route tests for strict JSON and safe validation responses.
- Implement normalization in `internal/store/viewer_commands.go` and strict decoding in Viewer command
  routes. Preserve the internal `update_app` creation path.

## Task 2: Add Service reporting and journal

- Add `ControlClient.Report` tests for the existing PATCH endpoint.
- Add a bounded atomic `CommandJournalStore`, local record model, boot generation, pruning, and
  duplicate/TTL reconciliation tests.
- Implement a serialized `CommandEngine` that persists before reporting or side effects and exposes
  fixed safe failure categories.
- Wire the HTTP control loop to the engine without blocking heartbeat or transport reconnect.

## Task 3: Restore local UI command IPC

- Add protocol tests for unsolicited event envelopes and lease-authorized `command_result` requests.
- Change Service connection writers to emit events, track one result waiter per operation key, and
  reject results from a non-owner connection.
- Add Electron management-pipe decoding/validation tests, command handler registration, and result
  requests.
- Implement `reload_live`, targeted renderer resubscribe, and Electron self-relaunch. Ensure restart
  acknowledgement is flushed before exit.

## Task 4: Add narrow lifecycle adapters

- Add platform-neutral interfaces and fake-adapter tests for Viewer restart success, missing session,
  timeout, changed lease, and renderer-ready proof.
- Implement the Windows active-console launcher with a fixed adjacent Viewer executable and no
  caller-controlled command line.
- Add service boot-generation/reconciliation tests and private restart-helper argument tests.
- Implement the Windows helper against the fixed SCM service name and connect it to graceful Service
  cancellation.

## Task 5: Replace the ambiguous operator form

- Add a pure command-presentation model with Korean names, descriptions, state labels, validation,
  confirmation rules, and tests.
- Replace free-form Viewer ID/command/route/message inputs with registry Viewer/action selectors,
  relevant stream/reason controls, and explicit restart confirmation.
- Auto-refresh only while non-terminal commands exist and show each transport/execution state
  distinctly.

## Task 6: Local verification and review

- Run focused Go, Viewer, and Web tests after each layer.
- Run `go test ./...`, Viewer tests/build, Web tests/lint/build, and `go build` where the toolchain is
  available; otherwise execute the exact build on WinPC and record that boundary.
- Cross-build the Windows Service, validate the MSI policy/manifest, scan changed files for secrets,
  run `git diff --check`, and inspect the final diff for unrelated changes or widened control input.

## Task 7: WinPC end-to-end proof

- Confirm the existing approved SSH and one-shot interactive Viewer-window harness without installing
  a new service or opening a port.
- Build a new unsigned development MSI with a higher version from the reviewed source, verify its
  hash and metadata, install it as a major upgrade, and verify exact installed files/service state.
- Configure the Viewer to a disposable, reachable CamStation test daemon and record its generated
  Viewer identity without exposing endpoint details in tracked output.
- Issue `ping`, `reload_live`, `resubscribe_stream`, `restart_viewer`, and `restart_service` through
  the normal server API. For each, require a terminal server row. For restarts also require changed
  process/boot identity and recovered renderer/control state.
- Remove disposable server state and GUI evidence according to the existing harness cleanup contract;
  leave the verified Viewer installation/configuration only when it is explicitly within the current
  WinPC maintenance scope.

## Task 8: Reconcile documentation

- Update `docs/07-implementation-status.md` and the Viewer command analysis with shipped behavior and
  precise remaining limitations.
- Complete the checklist/review in `tasks/todo.md` and record lessons from any correction or failed
  acceptance attempt in `tasks/lessons.md`.

## Completion record

- Implemented strict server schemas and exact lifecycle transitions for the five approved operator
  commands, including the `restart_agent` compatibility alias and pending-only cancellation.
- Added the Service journal, serialized command engine, safe result reporting, unsolicited IPC,
  targeted renderer recovery, and fixed-target Windows Viewer/SCM lifecycle adapters.
- Replaced the free-form operator form with registry/action selectors, contextual inputs,
  disruptive-action confirmation, exact state labels/timestamps, and active-command polling.
- Restored the independent monitoring adapter after WinPC acceptance exposed that local stream
  telemetry was acknowledged but discarded. The Service now forwards lease/renderer/progress times
  and bounded stream state, with periodic renderer reports across recovery cooldowns.
- `./scripts/check-dev.sh` passed all Go packages, 58 Web tests, 36 Viewer tests, Web lint/build,
  Viewer build, embedded Web regeneration, and daemon build.
- Native Windows build and install of unsigned MSI `2.0.24` passed Viewer and Service tests,
  Electron packaging, WiX validation, and artifact inspection. All five commands succeeded through
  the normal API; restart proofs changed Viewer process identity and Service PID/boot generation.
- Real `/viewers` selection and keyboard submission created a command with HTTP 201 and showed its
  automatic terminal success. Disposable server/config/journal/camera and bounded GUI evidence were
  removed afterward; the verified installation and automatic Service remain, unconfigured.
