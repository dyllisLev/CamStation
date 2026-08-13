# Windows management-plane control

Use this runbook for elevated process, service, file, package, registry, and performance work. It is
separate from interactive desktop input: SSH runs as the maintenance identity in its own session;
the Cua/UIA Plan bridge runs as the logged-on user in the Explorer session.

## Standard invocation

Write one bounded local PowerShell file, review it, then run it through the selected profile:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs \
  --target <test-pc|monitoring-pc> \
  --mode system \
  --intent <read-only|change> \
  --script <operation.ps1>
```

The intent flag is mandatory and is recorded in the result. It does not grant extra Windows
privileges; it prevents a state-changing script from being mistaken for a diagnostic. The wrapper:

1. proves computer, maintenance identity, interactive identity, and session;
2. accepts only a regular `.ps1` file no larger than 1 MiB;
3. uploads it to one unique directory, verifies its SHA-256, and runs the Windows PowerShell parser;
4. executes it in a child PowerShell process with stdout/stderr bounded to 64 KiB;
5. deletes the exact uploaded script/directory in `finally`; and
6. repeats target/session preflight and reports task residue.

Do not call `ssh`, `scp`, or remote PowerShell directly. Do not put a host, username, key, password,
or credential in the operation file.

## Operation-file contract

Start with strict failure behavior and resolve exact targets before mutation:

```powershell
$ErrorActionPreference = "Stop"
$target = Get-Process -Id $expectedPid -ErrorAction Stop
if ($target.SessionId -ne $expectedSessionId) { throw "PID belongs to another session" }

# Perform one requested operation, then query its post-state.
[ordered]@{
  Result = "OPERATION_COMPLETE"
  ProcessId = $expectedPid
  Verified = $true
} | ConvertTo-Json -Compress
```

Embed request-specific constants in the short untracked operation file only after obtaining them
from the same run's observation. Prefer invariant fields and numeric states over localized display
text. Use `throw` on failure; do not hide errors with `SilentlyContinue` except when absence is the
explicit state being tested.

For a change, capture the smallest useful before/after set: exact PID and executable path for a
process; service name, status, start mode, and PID for a service; full resolved path plus hash for a
file; product code/version for a package; key/value/type for a registry value. Report partial changes
as failures even when the child process exits zero.

## Process and service rules

- First resolve PID, session, parent PID, executable path, product/version, and top-level window.
- Close an interactive application through its verified window/UIA path first. Use `Stop-Process
  -Id <exact>` only when force-close is explicitly requested or graceful close is verified to fail.
- Never use `taskkill /IM`, a wildcard, `Get-Process name | Stop-Process`, `kill`, `pkill`, or a broad
  PID match. Re-resolve the PID immediately before stopping it to prevent PID-reuse mistakes.
- Address services by invariant service name, not localized display name. Record status and start
  mode separately; stopping a service must not silently change its startup configuration.
- A windowless `ApplicationFrameHost` is not proof that the requested app is still visible. Match
  the exact launch PID and require zero remaining target windows before cleaning that PID.

## Performance diagnosis

Windows has no Unix zombie state. Separate four questions:

1. Is a one-shot CamStation task, worker, run directory, or staged script actually left behind?
2. Does a process have a missing parent, a top-level window, or an expected detached-daemon role?
3. Is the host resource-bound? Sample total CPU repeatedly, normalized process CPU by logical CPU
   count, available/committed memory and paging, plus disk busy time and queue.
4. Does the same standard `Status` call become faster after one exact workload is stopped?

Use the same counters before and after. Free disk capacity is not disk latency, installed RAM is not
memory pressure, and cumulative `Get-Process CPU` is not current utilization.

## Interactive operations belong in Plan

Do not use the system plane for click, typing, hotkeys, window movement, screenshots, or focus. SSH
does not own the logged-on desktop. Use a Plan so the one-shot task receives the interactive token,
and verify the visible result with a fresh state or screenshot.
