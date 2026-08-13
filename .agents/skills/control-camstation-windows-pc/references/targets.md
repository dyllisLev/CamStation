# Authorized Windows target profiles

Both PCs use the same control capabilities and the same canonical scripts. The alias exists only to
select the right pinned transport and prove the remote Windows identities before an action.

| Alias | Computer | SSH maintenance identity | Interactive identity | Current expected session | Display handling |
|---|---|---|---|---:|---|
| `test-pc` | `WIN11-DELL` | `WIN11-DELL\CamStationBuildOps` | `WIN11-DELL\dyllislev` | 1 | RDP geometry is dynamic; query it every run |
| `monitoring-pc` | `NUC` | `NUC\CamStationOps` | `NUC\dyllislev` | 1 | Last observed desktop was 2560×1440; query it every run |

Machine names and identities are non-secret selection facts. SSH address, port, private-key path,
and known-hosts path live only in ignored `work/windows-control-targets.json`. The tracked schema is
`scripts/windows/windows-control-targets.example.json`. Never copy local connection values into the
skill, a plan, source control, logs, or a chat command.

## Exact selection algorithm

1. Map the user's wording to exactly `test-pc` or `monitoring-pc`. Never default to the first profile.
2. Let `Invoke-CamStationWindowsTarget.mjs` load the fixed local profile. It rejects unknown fields,
   missing aliases, duplicate endpoints/computers, a loose private-key mode, noncanonical remote
   project roots, and arbitrary host overrides.
3. The wrapper connects with `IdentitiesOnly`, `BatchMode`, public-key authentication only,
   strict host-key checking, and the profile's dedicated known-hosts file.
4. Before any write it requires, case-insensitively, the exact computer name and maintenance
   `WindowsIdentity`. It then requires exactly one Explorer owned by the profile's interactive user
   in the expected nonzero session.
5. It reads the session's numeric Windows Terminal Services state. GUI Plan/capture requires
   `Active`; a retained Explorer in `Disconnected` state is not treated as a usable desktop.
6. `Status` independently resolves the same interactive identity and checks the scheduler, driver,
   task, telemetry, TCP, firewall, and script state. A preflight/Status disagreement is a hard stop.

The expected session is deliberately fail-closed. If the user logs off and Windows assigns another
session, first observe the new session, confirm that it belongs to the intended interactive identity,
then update the ignored local profile deliberately. Do not weaken the comparison to “any session.”

## Per-run facts, not profile identity

Screen dimensions, DPI, foreground window, running applications, Viewer version, service state,
and CPU use can change. Query them for the current request. Do not decide which PC was selected from
one of those values and do not embed a desired application state in the target profile.

The aliases identify machines, not deployment roles or desired Viewer versions. A result on one PC
does not establish the other PC's display, network, or application state. Select the PC named by the
request, observe its application state afresh, and obtain explicit application-change authority before
installing, upgrading, removing, or reconfiguring software.

On the NUC, a previous simultaneous remote-display encoder drove total CPU to saturation and made a
normal Status call take roughly an order of magnitude longer. If control becomes slow, use the same
CPU counters before and after changing a workload; do not mislabel an intentionally detached Cua
daemon as a Windows “zombie.”

## Fast commands

```text
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target test-pc --mode status
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target monitoring-pc --mode status
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode desktop-capture
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode viewer-capture --viewer-operation Capture
node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target <alias> --mode viewer-configure --configuration <viewer-config.json>
```

Every result repeats the selected alias, actual computer, interactive identity/session, and cleanup
state without printing the SSH endpoint or private-key path.
