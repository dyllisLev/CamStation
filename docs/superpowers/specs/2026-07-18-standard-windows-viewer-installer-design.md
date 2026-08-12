# Standard Windows Viewer Installer Redesign

**Date:** 2026-07-18
**Status:** Approved

## Goal

Replace the custom self-extracting CamStation Viewer installer and supervised
scheduled-task launch chain with a conventional per-machine Windows
installation. The finished product must install, launch, repair, update, and
uninstall like an ordinary Windows desktop application while retaining only
the machine-wide management capabilities CamStation actually needs.

The user-facing result is one standard x64 MSI package, a desktop application
that shortcuts launch directly, and one small management service responsible
only for shared configuration, server status, and signed automatic updates.

## Decisions

- Package the application as a WiX-based, per-machine x64 MSI.
- Install for all Windows users and request elevation through Windows
  Installer.
- Launch `CamStationViewer.exe` directly from the public desktop and Start
  Menu shortcuts.
- Store one Viewer identity and one server configuration per PC.
- Collect the server URL and Viewer display name in the Viewer on first run,
  not in the MSI.
- Preserve entered configuration on connection failure and allow later edits
  from the Viewer settings screen.
- Enable login auto-start by default and allow it to be disabled in settings.
- Apply a server-directed update immediately without asking for approval.
- Remove application data, identity, update files, and logs on full uninstall.
- Require real-Windows acceptance and soak evidence before calling a release
  installable or complete.

## Scope

This design covers the Windows package, desktop application lifecycle,
machine-wide configuration, the minimum management service, update execution,
repair, uninstall, logging, and release acceptance.

The existing live workspace, camera playback behavior, PTZ controls, and
server-side Viewer registry remain product features. Their UI and transport
logic are outside this redesign except where they consume the new settings and
management IPC.

The existing pre-release `2.0.0-dev.*` custom installer does not receive an
in-package migration path. It has not been released as a production package.
Development machines must remove it once before testing the new MSI.

## Removed Architecture

The new implementation removes these installation and launch mechanisms:

- the Go self-extracting `CamStationViewerSetup.exe` installer;
- embedded `release.zip` installation payloads;
- version-and-digest `releases` directories on the Windows client;
- `current.json` release pointers;
- the stable Viewer Bootstrap and service Host indirection;
- the `CamStationViewer` logon scheduled task;
- the `CamStationViewerRecovery` boot scheduled task;
- public shortcuts that execute `schtasks.exe`;
- custom install/update transaction journals and rollback ownership;
- PowerShell-driven shortcut and lifecycle registration;
- direct use of `sc.exe`, `schtasks.exe`, `reg.exe`, and `icacls.exe` as the
  primary installer implementation.

Windows Installer becomes the single owner of installed files, the service,
shortcuts, registry entries, repair, upgrade, rollback, and uninstall.

## Installed Layout

The MSI installs stable application files beneath:

```text
C:\Program Files\CamStation Viewer\
  CamStationViewer.exe
  CamStationViewerService.exe
  CamStationViewerRestart.exe
  Electron runtime and application resources
```

There are no application-managed version directories below Program Files.
Windows Installer upgrades the installed component set.

Machine-owned working data lives beneath:

```text
C:\ProgramData\CamStation\Viewer\
  Logs\
  Updates\
```

The MSI creates this exact root and its ACLs. Full uninstall removes the exact
owned root after the service and Viewer have stopped. No unrelated CamStation
data outside this root is removed.

## Components and Responsibilities

### MSI Package

The WiX package owns:

- Program Files components;
- the automatic Windows service registration;
- service recovery configuration;
- public desktop and all-users Start Menu shortcuts;
- the all-users login auto-start entry;
- Add/Remove Programs registration;
- application and service event-log registration where required;
- ProgramData directories and access rules;
- major-upgrade, repair, rollback, and uninstall behavior.

The package has a stable UpgradeCode. Production builds use versioned
ProductCodes, reject downgrades, and support standard Windows Installer major
upgrades. The package does not ask for a server address or Viewer name.

### CamStation Viewer

`CamStationViewer.exe` is the interactive Electron application. It:

- is the direct shortcut target;
- presents first-run connection setup;
- renders the live workspace after configuration;
- exposes connection and auto-start settings;
- connects to the management service through local IPC;
- obtains a single-machine Viewer lease before opening the live window;
- reports Viewer and renderer state to the service;
- displays update, connection, service, and recovery status;
- starts the user-session restart helper only during an immediate update.

The Viewer does not write machine configuration files, invoke MSI, install a
service, supervise itself, or terminate solely because one IPC request failed.

### Minimum Management Service

`CamStationViewerService.exe` runs automatically as LocalSystem because it must
perform machine-wide MSI updates. Its responsibilities are limited to:

- owning and validating the PC-wide Viewer configuration;
- owning the stable `clientId`;
- granting at most one active Viewer lease per PC;
- maintaining the server heartbeat and management channel after configuration;
- reporting service, Viewer, renderer, connection, and update status;
- receiving server-directed update commands;
- downloading and verifying update packages;
- launching the short-lived update broker flow;
- keeping bounded rotating operational logs.

It does not start the Viewer during normal operation, inject a process into a
user session, create scheduled tasks, own an Electron process tree, or perform
general process supervision. A manually closed Viewer stays closed until a
user launches it or the next enabled login auto-start.

### Restart Helper

`CamStationViewerRestart.exe` is a short-lived user-session helper used only
during an update while the Viewer is open. Before the Viewer exits, the helper
is copied to a unique user temporary directory and launched with a random
update-session identifier. It waits for the MSI transaction and management
service to become ready, then launches the stable
`C:\Program Files\CamStation Viewer\CamStationViewer.exe` path.

The helper launches either the upgraded Viewer or the Windows-Installer-rolled-
back Viewer. It has a bounded timeout, displays a recoverable error if neither
version becomes ready, and never loops indefinitely.

## Machine Configuration

The management service is the only configuration writer. It stores one
schema-versioned configuration value under an MSI-created HKLM application
key. The MSI owns the key's ACL and removal rule but does not author or repair
the mutable configuration value.
The value contains:

- normalized server URL;
- Viewer display name;
- stable random `clientId`;
- auto-start enabled state;
- configuration schema version.

The complete configuration is committed as one registry value only after all
new values have passed validation. This avoids partial multi-value changes and
removes the prior multi-process JSON replacement problem. The Viewer accesses
configuration only through service IPC. The MSI owns removal of the exact HKLM
application key on full uninstall.

Changing the server URL preserves the PC's `clientId`. The old server may show
the client as offline under its normal heartbeat TTL. A successful full
uninstall deletes the identity; a later reinstall creates a new one.

## Local IPC and Single-Viewer Lease

The service exposes one local-only named pipe. Its ACL permits LocalSystem,
Administrators, and local interactive users; remote named-pipe clients are
rejected. The service verifies the client process ID and session for requests
that change configuration or request the active Viewer lease.

The protocol is versioned and length bounded. Its operations are limited to:

- read configuration and connection state;
- validate and commit configuration;
- acquire, refresh, and release the Viewer lease;
- report Viewer, renderer, and stream state;
- query or acknowledge update state;
- submit redacted local diagnostic events.

The first eligible interactive Viewer instance to acquire the lease owns it.
The service releases it when the pipe closes or its bounded heartbeat expires.
Viewer processes started in other user sessions exit quietly when another
session owns the lease. A malformed or unauthorized message closes only that
client connection. A storage or server error returns a structured request
error and does not tear down an otherwise valid Viewer connection.

## Shortcuts and Login Auto-Start

The MSI creates:

- `CamStation Viewer` on the public desktop;
- `CamStation Viewer` in the all-users Start Menu.

Both shortcuts target `CamStationViewer.exe` directly. They do not call a
scheduled task, PowerShell, a shell script, or a version-specific path.

The MSI also owns one all-users Windows login auto-start entry that launches:

```text
"C:\Program Files\CamStation Viewer\CamStationViewer.exe" --autostart
```

The entry always remains MSI-owned. In `--autostart` mode, the Viewer asks the
service whether auto-start is enabled and exits quietly when it is disabled.
This allows settings to toggle behavior without deleting an MSI-owned registry
entry and lets MSI repair remain deterministic. Auto-start is enabled by
default.

## Install and First-Run Flow

1. The user launches the signed x64 MSI.
2. Windows Installer displays its normal UI and UAC confirmation.
3. The MSI installs application files, registers and starts the service, and
   creates the standard shortcuts and auto-start entry.
4. The completion page offers to launch CamStation Viewer and selects that
   option by default. The launch runs with the installing interactive user's
   normal token, not the elevated Windows Installer token.
5. The Viewer starts directly in the installing user's session.
6. When no valid configuration exists, the Viewer shows the first-run screen.
7. The user enters a CamStation server URL and Viewer display name.
8. `Connect and save` validates URL syntax, server reachability, API
   compatibility, and Viewer registration without overwriting existing valid
   settings.
9. Only after validation succeeds does the service commit the new
   configuration and identity.
10. The Viewer opens `/live?viewer=1` through the normal hardened window.

No post-install command window, PowerShell output, or scheduled-task state is
visible to the user.

## Connection and Settings UX

The first-run and disconnected views use the same connection form. It contains
the server URL, Viewer display name, `Connect and save`, and `Retry` controls.

On connection failure:

- entered and previously saved values remain visible;
- the Viewer does not delete configuration or create a new identity;
- the screen distinguishes malformed URL, unreachable server, incompatible
  API, rejected registration, and management-service failure;
- automatic retries use a bounded backoff with a maximum 30-second interval;
- the user may retry immediately or edit settings at any time.

The normal settings screen exposes the same server URL and display name plus
the login auto-start toggle. A server change is validated before it replaces
the current working configuration. If validation fails, the previous server
configuration remains active.

## Server Status and Control

After configuration, the service maintains the server heartbeat and management
channel whether or not the Viewer is open. It reports these states separately:

- service online/offline;
- server connection state;
- Viewer running/closed;
- renderer ready/unresponsive;
- installed MSI version;
- update idle/downloading/applying/failed;
- last Viewer and server heartbeat timestamps.

Server commands that affect the visible UI are delivered to the Viewer through
IPC only when it holds the lease. Closing the Viewer is not considered an Agent
or installation failure. The service never claims the renderer is healthy
based only on service liveness.

## Immediate Automatic Update

The server may direct an immediate update. The command identifies an exact MSI
version, size, SHA-256 digest, download URL, and command generation.

The update flow is:

1. The service downloads the MSI to a unique transaction directory below
   ProgramData.
2. The service checks the exact size and SHA-256 digest.
3. The service verifies Authenticode trust and the configured CamStation
   publisher identity. Production policy rejects unsigned packages.
4. If a Viewer holds the lease, the service sends an `update_applying` event.
5. The Viewer displays a short noninteractive update screen, copies and starts
   the restart helper, and exits normally.
6. A short-lived, signed update broker is copied to a unique SYSTEM- and
   Administrators-only temporary directory outside Program Files. Running as
   LocalSystem, it re-verifies the package and starts `msiexec` with quiet,
   no-restart options. The broker survives the service stop performed by MSI.
7. Windows Installer stops the old service, performs the major upgrade, and
   starts the new service.
8. The broker records the Windows Installer result and exits. The restarted
   service verifies its installed version and reports the command result.
9. When a Viewer was open before the update, the user-session restart helper
   waits for the new or rolled-back service and launches the direct stable
   Viewer path.

If no Viewer is open, the service applies the update without starting a new
Viewer afterward.

One failed package is not immediately retried in a loop. The service reports
the exact failure phase and waits for a new server command generation or an
explicit operator retry.

## Update Failure and Rollback

Windows Installer, not CamStation application code, owns file and service
rollback. A failed upgrade must produce one of two observable outcomes:

- the new MSI is fully installed, the service reports the exact new version,
  and the Viewer can relaunch; or
- Windows Installer restores the prior registered product and service, and the
  prior Viewer can relaunch.

Hash or signature failures occur before the Viewer closes. MSI failures after
the Viewer closes cause the restart helper to launch the rolled-back version.
The Viewer then displays the saved update error returned by the service.

An update interruption caused by machine reboot is reconciled by Windows
Installer. There is no CamStation boot recovery scheduled task. On the next
service start, the service reads the installed MSI version and any bounded
update result marker, reports the result, and removes stale transaction files.

## Repair

Standard Windows Installer repair restores:

- missing or corrupted installed files;
- service registration and startup configuration;
- public desktop and Start Menu shortcuts;
- the all-users auto-start entry;
- required ProgramData directories and ACLs.

Repair preserves the PC-wide server configuration, Viewer name, identity, and
auto-start preference. Repair does not create a new server registration.

## Uninstall

Full uninstall uses Windows Restart Manager to request a normal Viewer exit and
stops the management service. It removes:

- all MSI-owned Program Files components;
- the service;
- desktop and Start Menu shortcuts;
- the auto-start entry;
- the exact HKLM CamStation Viewer configuration key;
- the exact `C:\ProgramData\CamStation\Viewer` root, including configuration,
  identity, update artifacts, and logs.

When the server is reachable, the service makes a best-effort unregister
request before removal. Server unavailability never blocks uninstall. The
server's normal TTL marks an unreported client offline so an operator can
remove it later.

The normal success path requires no reboot and leaves no CamStation Viewer
service, scheduled task, process, shortcut, Program Files directory, ProgramData
directory, or application registry key. Removal is restricted to exact
installer-owned paths and keys.

## Logging and Diagnostics

Service and Viewer operational logs are written as separate bounded files
under `C:\ProgramData\CamStation\Viewer\Logs`. The directory ACL permits the
service to manage logs and interactive Viewer processes to append only to
their assigned log files. Logs rotate by size and retained-file count.

Logs may contain normalized server origin, version, Windows error codes,
installer result codes, and state transitions. They must not contain camera
URLs, credentials, authorization headers, update tokens, named-pipe nonces, or
raw server response bodies. UI errors show a short Korean explanation and a
correlation ID that maps to local logs.

## Build and Signing

The build produces one x64 `CamStationViewer.msi`. The MSI and every installed
EXE are Authenticode signed for production. The server publishes immutable MSI
metadata and serves the exact verified artifact.

Development builds may use an explicitly configured development certificate
or an isolated unsigned-test policy. Unsigned policy is never inferred from a
version string and is not enabled in production packages.

## Acceptance Gates

No release is described as installed, fixed, or complete until all required
real-Windows gates pass. Linux unit tests and Windows cross-compilation are
supporting evidence only.

### Environment Matrix

- Windows 10 x64 and Windows 11 x64;
- Korean and English Windows installations;
- standard interactive user with UAC administrator approval;
- local desktop and RDP session;
- server available, unavailable, and restarted.

### Installation and Configuration

- clean MSI install and normal UAC flow;
- direct desktop and Start Menu launch;
- first-run server configuration;
- malformed and unreachable server handling with values preserved;
- settings-based server and display-name change;
- default auto-start and disabled auto-start behavior;
- one Viewer lease across multiple signed-in Windows accounts;
- no command window, PowerShell window, or scheduled task;
- expected Program Files, ProgramData, service, shortcut, and registry state.

### Lifecycle

- service restart while the Viewer remains open and reconnects;
- server outage and recovery without configuration loss;
- Viewer close and manual relaunch without service recovery intervention;
- Windows Installer repair with configuration preserved;
- major upgrade with identity preserved;
- full uninstall with exact owned resources absent;
- clean reinstall starting at first-run configuration with a new identity.

### Update Failure Injection

- immediate healthy signed update and Viewer relaunch;
- incorrect size or SHA-256 rejected before shutdown;
- unsigned or wrong-publisher MSI rejected before shutdown;
- network interruption during download;
- MSI install failure followed by standard rollback and old Viewer relaunch;
- reboot during upgrade followed by Windows Installer reconciliation;
- duplicate command delivery without duplicate update execution;
- failed generation not retried indefinitely.

### Stability

- at least two continuous hours on a target Windows PC;
- repeated Viewer/service IPC reconnects;
- repeated server outage and recovery;
- service restart without Viewer termination;
- ten install/repair/update/uninstall cycles;
- logs and UI errors checked against every injected failure.

Each gate records MSI logs, service and Viewer logs, process/session evidence,
installed-product information, shortcut targets, registry state, and server
heartbeat evidence. A source review, mock, or cross-build cannot substitute for
this evidence.

## Release Transition

The current custom dev installer remains a rejected development artifact. On
the existing Windows test PC it is removed completely before the first MSI
test. The new MSI is then tested as a clean install. No production migration
code is added for unreleased dev artifacts.

The server delivery endpoint changes from an EXE payload to immutable MSI
metadata and download. The server does not publish the new package as current
until the full acceptance gates pass.
