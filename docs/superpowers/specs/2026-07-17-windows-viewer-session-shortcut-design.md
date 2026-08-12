# Windows Viewer Session Detection and Desktop Shortcut Design

**Date:** 2026-07-17
**Status:** Approved for planning

## Goal

Make the CamStation Viewer installer work from an interactive local or RDP
desktop session without binding the Viewer to the account used for UAC
elevation. Add a durable shortcut to the Windows public desktop.

## Scope

This change covers only initial installation and repair registration behavior:

- identify the user who owns the interactive Windows shell in the installer's
  current session;
- use that user's SID for the Viewer logon task and persisted Viewer config;
- create `CamStation Viewer.lnk` on the public desktop;
- remove or restore the shortcut during uninstall, failed-install rollback,
  and repair recovery.

It does not add an installer wizard, arbitrary user selection, per-user
shortcuts, Start Menu entries, or a custom shortcut helper executable.

## Interactive User Selection

The installer must stop using `Win32_ComputerSystem.UserName`. That value may
be empty for an RDP-only machine and represents the physical console rather
than necessarily the session running the installer.

On Windows, the installer will:

1. obtain the shell window for its current interactive session;
2. obtain the shell process ID;
3. open the shell process token for query access only;
4. read and validate the token user SID;
5. use that SID as `MonitoringUserSID`.

This selects the actual desktop user even when UAC elevation uses different
administrator credentials. It also works for an installer launched from an
RDP desktop because the shell belongs to that RDP session.

If the current session has no shell window, the installer must fail before
machine mutation with an error that says an interactive desktop session is
required. It must not fall back to the elevated process token, another active
session, or an arbitrary logged-on account.

## Public Desktop Shortcut

Registration creates this shortcut:

- location: Windows `CommonDesktopDirectory`;
- name: `CamStation Viewer.lnk`;
- target: `%SystemRoot%\System32\schtasks.exe`;
- arguments: `/Run /TN "CamStationViewer"`;
- icon: `%ProgramFiles%\CamStation Viewer\CamStationViewerBootstrap.exe`;
- working directory: `%ProgramFiles%\CamStation Viewer`.

The shortcut launches the stable scheduled task instead of a release-specific
Electron executable. This keeps it valid across Viewer updates and reuses the
task's configured user identity and `IgnoreNew` multiple-instance policy.

The installer will use the Windows Script Host shortcut COM interface through
the existing bounded PowerShell execution path. It will not add a dependency
or ship another binary solely to write a `.lnk` file.

## Transaction and Removal Behavior

Shortcut creation is part of runtime registration. Registration fails if the
shortcut cannot be written or validated.

- A clean-install rollback unregisters runtime entries and deletes the
  shortcut.
- A repair rollback restores the previous runtime registration and recreates
  the shortcut from the stable paths.
- Uninstall deletes the shortcut; a missing shortcut is already the desired
  state and is not an error.
- Reinstall or repair overwrites the owned shortcut through the shortcut COM
  save operation.

No user-created file other than the exact public-desktop
`CamStation Viewer.lnk` path may be removed.

## Error Reporting

The installer keeps its existing console output contract. User-selection
errors will distinguish these cases:

- no interactive shell exists in the current session;
- the shell process cannot be queried;
- the shell token does not yield a valid SID.

Shortcut creation errors include the failed registration operation in the
installer's returned error. No persistent installer log is added in this
change.

## Testing and Verification

Automated coverage will prove:

- interactive-user selection accepts a validated SID obtained from the
  current shell process rather than a CIM computer-wide username;
- a missing shell or invalid SID fails before installation mutation;
- the generated shortcut registration targets the `CamStationViewer`
  scheduled task, uses the stable bootstrap icon, and selects the public
  desktop;
- uninstall registration removes the exact owned shortcut and tolerates it
  already being absent;
- existing installer transaction and registration tests remain green;
- all Windows-only code cross-compiles for `windows/amd64`.

The target Windows verification is:

1. launch the installer from an RDP desktop in an elevated PowerShell;
2. confirm installation succeeds and the scheduled task SID matches the RDP
   desktop user, not alternate UAC credentials;
3. confirm `CamStation Viewer.lnk` appears on the public desktop;
4. launch the shortcut twice and confirm only the supervised Viewer task runs;
5. uninstall and confirm the shortcut, task, service, and owned installation
   files are removed.
