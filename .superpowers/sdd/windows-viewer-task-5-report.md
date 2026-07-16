# Task 5 Report: Windows Agent core and service host

## Scope

- Added a cross-platform Agent core with strict server URL validation, a stable persisted client ID, bounded atomic machine JSON files, durable command intent/result records, update quarantine, restart budgets, SSE-to-long-poll control, independent heartbeat, and restart-generation reconciliation.
- Added a 64 KiB newline-delimited versioned named-pipe protocol. The Windows adapter uses `go-winio`, rejects remote clients through the library's pipe mode, applies a restricted SDDL, verifies the OS-reported client PID/session, requires the active console session, limits the connection to one client, and applies read/write deadlines.
- Added a separate stable Windows SCM host using `golang.org/x/sys/windows/svc`. It reads and validates `current.json`, waits for the initial Agent's durable-engine readiness handshake before reporting `Running`, runs exactly one versioned Agent, requests graceful stop over stdin before bounded kill/reap, reloads a planned Agent restart immediately, and permits only three bounded crash restarts at 5, 30, and 120 seconds.
- Added `camstation-viewer-agent configure --server-url ... --display-name ... --install-dir ...` and `run --config ...`; the stable host additionally uses private `--control-stdin` and `--ready-stdout` lifecycle switches.
- Did not start/restart the CamStation runtime or change camera media settings.

## TDD evidence

Initial RED:

```text
$ go test ./internal/vieweragent -count=1
# camstation/internal/vieweragent [camstation/internal/vieweragent.test]
internal/vieweragent/agent_test.go:13:41: undefined: Command
internal/vieweragent/agent_test.go:15:60: undefined: Command
internal/vieweragent/agent_test.go:21:11: undefined: MachinePaths
internal/vieweragent/agent_test.go:23:11: undefined: Agent
...
FAIL camstation/internal/vieweragent [build failed]
```

Later RED checks caught and drove fixes for:

- terminal duplicate result replay;
- unfinished Viewer restart convergence to the same operation key/generation;
- a crash between receipt and restart-intent persistence;
- unsafe raw executor errors entering state/server reports;
- named-pipe PID/session spoofing and non-console sessions;
- command-report requests without a five-second bound.

Final focused GREEN:

```text
$ go test ./internal/vieweragent -count=1
ok camstation/internal/vieweragent 1.992s

$ go test -race ./internal/vieweragent -count=1
ok camstation/internal/vieweragent 1.605s
```

## Post-review stability hardening

The follow-up review reported no critical findings and identified four important and two minor stability issues. All six were covered and corrected:

- the SSE control path now keeps one connection across keepalives and commands, applies the 25-second inactivity deadline per valid frame, and uses an independent `1s, 2s, 5s, 10s, 30s, 5m` SSE probe budget while long polling;
- command records durably retain `createdAt` and `ttlSeconds`, and startup/duplicate reconciliation expires nonterminal work before any side effect;
- the Host requests graceful Agent stop, waits through a bounded deadline, kills and reaps if required, and treats planned `restart_agent` separately from crash recovery;
- startup reconciliation and a real ledger read/atomic-write health check must succeed before Agent readiness, heartbeat startup, or an `online` control state;
- pipe decoding accepts whitespace after one JSON value but rejects any second JSON value;
- SCM remains `StartPending` until `current.json`, the Agent file, process start, and Agent readiness all succeed.

Additional RED checks caught and drove fixes for persistent SSE reconnects, poll-induced probe resets, immediate-empty poll spinning, expired running commands, stop/kill ordering, repeated planned-restart loops, false-online ledger failures, trailing pipe JSON, missing readiness, and local control-state failures being reported as clean exits.

Fresh focused GREEN after the review fixes:

```text
$ go test ./internal/vieweragent ./cmd/camstation-viewer-agent -count=1
ok camstation/internal/vieweragent 0.845s
ok camstation/cmd/camstation-viewer-agent 0.018s

$ go test -race ./internal/vieweragent ./cmd/camstation-viewer-agent -count=1
ok camstation/internal/vieweragent 2.018s
ok camstation/cmd/camstation-viewer-agent 1.029s
```

## Second re-review transport delta

The second review reported no critical findings and left two important and one minor issue. This delta addresses only those new findings:

- SSE response-header acquisition is bounded by the configured 25-second deadline, but that timer is stopped as soon as `Do` returns headers; a healthy persistent stream then uses only the per-frame inactivity timer;
- fallback uses one serialized callback event loop with at most one SSE probe/session and one long poll. Probe scheduling no longer truncates the poll's own full deadline, and proven SSE recovery cancels the poll;
- every ready-session planned Agent exit reloads immediately without consuming crash delays; exit 75 before readiness remains a Windows startup failure before the supervisor sees a child session;
- the SSE parser counts the entire normalized frame before accumulating data and rejects a cumulative frame over 64 KiB.

Second-review RED evidence:

```text
--- FAIL: TestSSEHeaderBlackholeFallsBackWithinDeadline (0.50s)
    result={Transport: Command:<nil> Proven:false} polls=0 elapsed=500.479989ms err=context deadline exceeded
--- FAIL: TestSSEProbesDoNotShortenLongPoll (0.50s)
    context deadline exceeded
--- FAIL: TestLongPollCommandArrivesWhileSSEProbePending (0.50s)
    context deadline exceeded

--- FAIL: TestMultiplePlannedAgentRestartsNeverConsumeCrashBudget
    err=<nil> runs=5 delays=[5s 30s 2m0s]

--- FAIL: TestSSERejectsCumulativeOversizedFrame
    oversized frame err=decode SSE command: invalid character 'x' looking for beginning of value
```

Fresh focused GREEN after the second-review delta:

```text
$ go test ./internal/vieweragent ./cmd/camstation-viewer-agent -count=1
ok camstation/internal/vieweragent 0.957s
ok camstation/cmd/camstation-viewer-agent 0.015s

$ go test -race ./internal/vieweragent ./cmd/camstation-viewer-agent -count=1
ok camstation/internal/vieweragent 7.321s
ok camstation/cmd/camstation-viewer-agent 1.022s
```

## Verification

```text
$ go test ./... -count=1
PASS

$ go vet ./...
PASS

$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-agent.exe ./cmd/camstation-viewer-agent
$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-host.exe ./cmd/camstation-viewer-host
$ file /tmp/camstation-viewer-agent.exe /tmp/camstation-viewer-host.exe
/tmp/camstation-viewer-agent.exe: PE32+ executable (console) x86-64, for MS Windows, 16 sections
/tmp/camstation-viewer-host.exe:  PE32+ executable (console) x86-64, for MS Windows, 16 sections

$ go build -o /tmp/camstationd-task5 ./cmd/camstationd
PASS

$ git diff --check
PASS
```

Fresh second-review full verification:

```text
$ go test ./... -count=1
PASS (camstationd 55.267s, store 34.020s, vieweragent 2.182s)

$ go vet ./...
PASS

$ go build -o /tmp/camstationd-task5-rereview ./cmd/camstationd
PASS

$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-agent-rereview.exe ./cmd/camstation-viewer-agent
$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-host-rereview.exe ./cmd/camstation-viewer-host
$ file /tmp/camstation-viewer-agent-rereview.exe /tmp/camstation-viewer-host-rereview.exe
/tmp/camstation-viewer-agent-rereview.exe: PE32+ executable (console) x86-64, for MS Windows, 16 sections
/tmp/camstation-viewer-host-rereview.exe:  PE32+ executable (console) x86-64, for MS Windows, 16 sections
```

## Deliberate Task 5 boundary

The core has injected executors for Viewer reload/restart, stream actions, diagnostics, and update activation. Task 6 supplies the Electron/bootstrap/Job integration; Task 7 supplies updater/installer activation. `ping` and idempotent `restart_agent` boot-generation handoff work in Task 5 without those later adapters.

## Commit

`6bf2806 feat(viewer-agent): add Windows control service`

Post-review hardening: `fix(viewer-agent): harden control service recovery`

Second re-review transport delta: `fix(viewer-agent): isolate control transport deadlines`
