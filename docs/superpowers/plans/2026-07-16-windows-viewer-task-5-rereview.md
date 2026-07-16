# Windows Viewer Task 5 Re-review Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the remaining Task 5 stability findings without changing the established Agent/Host public behavior.

**Architecture:** Keep `RunControl` as the single callback dispatcher while one SSE session and one full-deadline long poll may run concurrently during fallback. Bound only SSE response-header acquisition before `Do` returns, then use the existing per-frame idle timer. Keep planned-restart classification at the already-ready child boundary and enforce the SSE limit while accumulating a frame.

**Tech Stack:** Go standard library HTTP/context/timer primitives, existing `internal/vieweragent` tests, Windows cross-compilation.

## Global Constraints

- Preserve one SSE session and at most one long poll.
- Every long poll retains its full configured deadline and `wait=24` contract.
- Only a proven SSE frame cancels fallback polling; callbacks remain serialized.
- Planned exit code 75 is immediate only after the Windows adapter completed Agent readiness.
- An SSE frame is at most 64 KiB cumulatively.
- Do not touch runtime services, cameras, media settings, or unrelated untracked files.

---

### Task 1: Independent SSE header, stream, probe, and poll lifetimes

**Files:**
- Modify: `internal/vieweragent/control_test.go`
- Modify: `internal/vieweragent/control.go`

**Interfaces:**
- Consumes: `ControlClient.StreamSSE`, `ControlClient.longPoll`, `ReconnectState.NextDelay`.
- Produces: `ControlClient.RunControl` with serialized events from one SSE worker and one poll worker.

- [x] **Step 1: Write failing HTTP tests**

Add tests whose handlers (a) blackhole response headers until request cancellation, (b) keep a valid SSE stream alive beyond the header deadline with regular frames, (c) keep a long poll blocked across early probes, and (d) return a poll command while an SSE probe is still waiting for headers.

- [x] **Step 2: Verify RED**

Run: `go test ./internal/vieweragent -run 'TestSSEHeaderBlackholeFallsBackWithinDeadline|TestSSEHeaderDeadlineDoesNotEndHealthyStream|TestSSEProbesDoNotShortenLongPoll|TestLongPollCommandArrivesWhileSSEProbePending' -count=1`

Expected: blackhole hangs until the outer test deadline and current probe-bound poll loses/cancels the blocked poll command.

- [x] **Step 3: Implement the minimum event loop**

Use `time.AfterFunc(client.deadline(), cancel)` around only `HTTPClient.Do`, stop it immediately when `Do` returns headers, and retain the existing frame idle timer afterward. In `RunControl`, funnel worker results through one event channel, start no more than one poll and one SSE session, let probe timers start SSE without canceling poll, and cancel poll only after a proven SSE frame.

- [x] **Step 4: Verify GREEN**

Run the Task 1 test expression again and then `go test ./internal/vieweragent -count=1`.

Expected: PASS.

### Task 2: Per-ready-session planned restart

**Files:**
- Modify: `internal/vieweragent/host_test.go`
- Modify: `internal/vieweragent/host.go`

**Interfaces:**
- Consumes: `RunChildSupervisor`, `ChildPlannedRestart`, `ChildCrashed`.
- Produces: unlimited distinct ready-session planned reloads that do not increment the bounded crash counter.

- [x] **Step 1: Replace the old host-lifetime guard test**

Drive three planned exits, then one crash, then a clean stop. Assert five runs total and exactly one delay of five seconds.

- [x] **Step 2: Verify RED**

Run: `go test ./internal/vieweragent -run '^TestMultiplePlannedAgentRestartsNeverConsumeCrashBudget$' -count=1`

Expected: FAIL because the second planned exit is currently converted to a crash.

- [x] **Step 3: Remove host-wide planned state**

Make every `ChildPlannedRestart` continue immediately; retain crash counting and the Windows startup/readiness adapter boundary.

- [x] **Step 4: Verify GREEN**

Run the Task 2 test and all `internal/vieweragent` tests.

Expected: PASS.

### Task 3: Cumulative SSE frame limit

**Files:**
- Modify: `internal/vieweragent/control_test.go`
- Modify: `internal/vieweragent/control.go`

**Interfaces:**
- Consumes: `scanSSE`.
- Produces: frame-size rejection before JSON join/decode.

- [x] **Step 1: Write a failing many-small-lines test**

Send individually valid short `data:` lines whose cumulative normalized frame exceeds 64 KiB, then assert the stream returns a frame-size error. Keep a whitespace-padded normal JSON frame test green.

- [x] **Step 2: Verify RED**

Run: `go test ./internal/vieweragent -run 'TestSSERejectsCumulativeOversizedFrame|TestSSEAcceptsWhitespacePaddedFrame' -count=1`

Expected: the oversized case fails with a JSON error instead of a size error.

- [x] **Step 3: Count the normalized frame before accumulation**

Track each scanned line plus its newline, reject once the total exceeds `maxControlMessageBytes`, and reset only after the blank frame terminator.

- [x] **Step 4: Verify GREEN**

Run the Task 3 tests and all focused tests with `-race`.

Expected: PASS.

### Task 4: Report, full verification, and commit

**Files:**
- Modify: `.superpowers/sdd/windows-viewer-task-5-report.md`

- [x] **Step 1: Record second-review RED/GREEN evidence**

Document header-only timeout, independent concurrent fallback, repeated planned reloads, and cumulative frame enforcement.

- [x] **Step 2: Run fresh verification**

Run focused tests and race, `go test ./... -count=1`, `go vet ./...`, daemon build, Windows Agent/Host CGO-disabled builds with `file`, and `git diff --check`.

Expected: every command exits zero; Windows binaries are PE32+ x86-64.

- [x] **Step 3: Commit only the second-review delta**

Commit message: `fix(viewer-agent): isolate control transport deadlines`.
