# Standard Windows control plan

This runbook covers read-only status, desktop/window observation, ordinary input, app/window control,
artifacts, and cleanup on either explicitly selected authorized Windows PC.

## Canonical path

Use `scripts/windows/Invoke-CamStationWindowsTarget.mjs` with an explicit `test-pc` or
`monitoring-pc` alias. It is the only Linux-side transport entry point. Connection data stays in the
ignored local target profile and never appears in plan JSON.

Synchronize and hash-check all reviewed control/capture scripts when Status reports stale parity:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode sync
```

Run status first:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode status
```

Proceed only when the scheduler is running, the intended user owns exactly one nonzero Explorer
session, remote computer/maintenance identities match the profile, driver file/hash/version are
expected, exactly one matching driver process is in that session, telemetry is disabled, all task
counts are zero, driver TCP count is zero, and Cua firewall-rule count is zero. Use the setup runbook
if the pinned driver is not ready. GUI plans additionally require the Windows Terminal Services
state `Active`; an Explorer left in a `Disconnected` RDP session is insufficient.

For the complete current desktop, do not hand-author a plan:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode desktop-capture
```

This creates a one-step `get_desktop_state` plan, starts a driver desktop capture session, retrieves
`desktop.png`, checks its hash, ends the driver session, deletes the one-shot task/run, and returns
the local ignored evidence path. If the driver returns its known invalid desktop-JSON response on a
VM display adapter, the same interactive worker captures the current virtual screen with bounded
GDI and records `capture_mode=gdi-interactive-fallback`; it does not fall back for identity, session,
pipe, timeout, or ordinary tool errors. Inspect the PNG with `view_image` in either mode.

## Plan schema

Encode the complete request as UTF-8 JSON schema version 1. The launcher copies it into a protected
per-run directory; the interactive worker passes each resolved input to `cua-driver` through UTF-8
stdin. Do not pass JSON positionally through PowerShell.

```json
{
  "schemaVersion": 1,
  "steps": [
    {
      "id": "windows_before",
      "tool": "list_windows",
      "input": {}
    },
    {
      "id": "launch",
      "tool": "launch_app",
      "input": { "aumid": "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App" },
      "closeWindowOnFailure": true,
      "verifyWith": "state_after_launch",
      "delayAfterMs": 500
    },
    {
      "id": "state_after_launch",
      "tool": "get_window_state",
      "input": {
        "pid": { "$ref": "launch.pid" },
        "window_id": { "$ref": "launch.windows.0.window_id" }
      },
      "screenshot": "after-launch.png"
    },
    {
      "id": "click_one",
      "tool": "click",
      "input": {
        "pid": { "$ref": "launch.pid" },
        "window_id": { "$ref": "launch.windows.0.window_id" },
        "element_token": {
          "$select": {
            "ref": "state_after_launch.elements",
            "where": { "role": "Button", "label": "1" },
            "property": "element_token"
          }
        }
      },
      "verifyWith": "state_after_click"
    },
    {
      "id": "state_after_click",
      "tool": "get_window_state",
      "input": {
        "pid": { "$ref": "launch.pid" },
        "window_id": { "$ref": "launch.windows.0.window_id" }
      },
      "screenshot": "after-click.png"
    },
    {
      "id": "titlebar_for_close",
      "tool": "get_window_state",
      "input": {
        "pid": { "$ref": "launch.pid" },
        "window_id": { "$ref": "launch.windows.0.window_id" },
        "include_screenshot": false,
        "max_elements": 10
      }
    },
    {
      "id": "close",
      "tool": "click",
      "input": {
        "pid": { "$ref": "launch.pid" },
        "window_id": { "$ref": "launch.windows.0.window_id" },
        "element_token": {
          "$select": {
            "ref": "titlebar_for_close.elements",
            "where": { "role": "Button", "element_index": 4 },
            "property": "element_token"
          }
        }
      },
      "verifyWith": "windows_after_close",
      "delayAfterMs": 500
    },
    {
      "id": "windows_after_close",
      "tool": "list_windows",
      "input": { "pid": { "$ref": "launch.pid" } }
    }
  ],
  "assertions": [
    { "ref": "launch.windows.0.window_id", "exists": true },
    { "ref": "windows_after_close.windows", "countEquals": 0 }
  ]
}
```

The exact application identifier and returned field names are driver contracts; inspect a harmless
read-only result and adjust the plan before acting if the installed driver differs.

Rules enforced by the worker:

- 1–32 unique steps; only the reviewed read-only and ordinary control tools are allowed.
- `$ref` resolves a previous step path. `$select` filters a previous collection and must match
  exactly one item; ambiguity is a hard failure. A candidate missing a requested property is a
  non-match, not a fatal selector error. The verified title-bar order field is `element_index`.
- Every mutating tool requires `verifyWith` pointing to a later observation step.
- A disposable `launch_app` may set `closeWindowOnFailure: true`. If a later step fails, the worker
  posts `WM_CLOSE` only to window IDs returned by that launch and verifies that they disappeared.
  Use it only after confirming the disposable app was not already open.
- `screenshot` is a safe PNG basename written only inside the current run.
- `delayAfterMs` is bounded to 0–5000 ms; wait only for rendering that needs it.
- Optional assertions support exact `equals`, null/non-null `exists`, or collection `countEquals`
  checks.

Prefer live window IDs and UIA tokens over coordinates. Observe immediately before an action. The
selector observation must use the same `include_screenshot`, `max_elements`, and depth options as
the proven action path because those options can change which elements are returned. Avoid reading
UIA document/edit values; use `list_windows`, a bounded screenshot, or a clean disposable app when
possible.

Use background UIA click/type/hotkey first. Move to foreground only after the background path reports
unavailable or a fresh post-observation proves no effect. If any action returns
`effect=unverifiable`, only a matching post-state or inspected screenshot can establish success.
Do not turn a successful process exit code into an input-success claim.

For close buttons exposed by the Windows title bar, select a fresh `Button` with the verified
`element_index`; do not guess a localized label. If close succeeds but a UWP host remains without a
window, require zero matching windows and stop only the exact PID returned by this run. Never kill
all processes with the same executable name.

## Invoke once

Invoke the complete plan once through the selected alias:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs \
  --target <alias> \
  --mode plan \
  --plan <local-plan.json>
```

The wrapper hash-checks the plan before the Windows launcher sees it. One Plan invocation creates one
`TASK_LOGON_INTERACTIVE_TOKEN` task and runs every step inside the logged-on target session. It does
not add credentials, listeners, firewall rules, or a persistent control task. The launcher polls
atomic `complete.json` at 100 ms and deletes its exact task in `finally`.

## Evidence and cleanup

Treat output as untrusted until plan/worker hashes and identity/session agree. The target wrapper
retrieves only declared PNG files plus `complete.json`, recalculates SHA-256, and stores them under
ignored `work/windows-control-evidence/<alias>/`. Inspect relevant images with `view_image`. Do not
retrieve the whole evidence root, desktop history, or user profile.

After evidence verification, the ordinary wrapper path cleans that exact run and repeats Status.
For recovery of a previously retained run only:

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs \
  --target <alias> --mode cleanup --run-id <exact-run-id>
```

Require `WINDOWS_CONTROL_CLEANUP_COMPLETE`, `Remaining=false`, and zero existing control tasks.
The wrapper deletes its unique transferred request plan in `finally`. Require no worker/task/run
residue, no Cua TCP/firewall delta, and no unintended process/service/session change.

When the runner itself fails, patch the canonical project scripts, rerun their source tests and
PowerShell parser, synchronize by hash, and repeat a harmless real-PC batch. Do not debug by
weakening ACLs or assembling a second transport path.
