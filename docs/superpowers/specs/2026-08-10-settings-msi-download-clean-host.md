# Settings-page MSI download and clean-host specification

## Operator journey

Completion is one end-to-end path: the operator opens `/settings`, downloads
`CamStationViewer.msi`, installs it, launches the MSI-installed `CamStationViewer.exe`, connects it to
CamStation 2.0, and confirms monitoring. A bare application EXE and the rejected legacy
self-extracting Setup EXE are not release artifacts for this workflow.

## Goal

Publish the verified CamStation Viewer `2.0.21` Windows Installer package from the retained 2.0
Docker canary and expose it as the visible download action on `/settings`. After a server-side
download has been independently verified, remove the currently installed Viewer from the authorized
Windows development PC so the operator can perform a manual clean-install test.

## Observed starting state

- The live `/settings` page already renders the `Windows 모니터링 클라이언트` card.
- `GET /api/viewers/app/version` returns HTTP 503 because the canary's persistent state mount has no
  `viewer-releases` directory, so the card displays `설치 파일이 아직 게시되지 않았습니다.`.
- The old release catalog and publisher accept only `CamStationViewerSetup.exe`, which belongs to the
  superseded embedded-installer architecture.
- The current canonical artifact is the independently verified unsigned development MSI
  `CamStationViewer.msi` version `2.0.21`, size `124350464` bytes, built on WIN11-DELL.
- WIN11-DELL currently has that product installed and running. The development source, MSI artifact,
  toolchain, SSH access, and active desktop session must remain available.

## Design

### Release formats

The release catalog accepts only two exact installer filenames:

- `CamStationViewer.msi` for the current manual standard installer;
- `CamStationViewerSetup.exe` for verified legacy release compatibility.

No arbitrary `.msi` or `.exe` filename is accepted. The publisher preserves the source basename in
the immutable release directory and manifest. The HTTP route emits the established PE content type
for the legacy EXE and `application/octet-stream` for MSI, always with attachment disposition, exact
length, and `X-Content-Type-Options: nosniff`.

The settings card remains on the fixed same-origin download route and uses metadata for the visible
version, size, hash prefix, and downloaded filename. The full hash remains available from the
metadata API and operational record.

### Automatic-update boundary

The current MSI is a manual installation artifact. It must not be sent to the superseded legacy
Agent update flow, which launches `CamStationViewerSetup.exe` with an EXE-specific transaction
contract. Only the exact legacy EXE filename is eligible for automatic update commands. Publishing
an MSI therefore enables the settings-page download but creates no automatic Viewer update command.

### Persistence and deployment

The canary reads `/var/lib/camstation/data/viewer-releases`, already inside the persistent host bind
mount `/var/lib/camstation2-canary/data`. Publish through the tested atomic pointer script and set
artifact/release files read-only while retaining directory traversal for container UID/GID 10001.
Build and load a new immutable Docker image before changing the canary's image pointer. Preserve the
current image and root-only `.env` backup as rollback.

### Windows clean-state boundary

Run the registered MSI uninstall for the exact product code resolved from the uninstall registry.
After successful MSI removal, delete only independently confirmed Viewer-owned remnants:

- exact Viewer process and service;
- exact product-created install, ProgramData, Start Menu, and public Desktop paths;
- exact machine Viewer/installer keys and `CamStationViewer` Run value;
- exact Electron profile directory for the interactive user;
- exact CamStation GUI capture task namespace if a failed historical task exists.

Preserve `C:\CamStationDev`, the MSI artifact, source checkout, build tools, Visual C++ runtime, SSH
configuration/account/firewall rule, user profile, Explorer/RDP session, and unrelated CamStation
projects or evidence.

## Verification

1. Publisher, catalog, route, automatic-update-boundary, web, and full repository checks pass.
2. A new immutable image runs healthy with the same DB/media mounts, port, camera allowlist, recorder
   state, PID limit, and no legacy 1.0 service change.
3. `/settings` visibly shows version `2.0.21`, size, hash prefix, unsigned badge, and enabled download
   action.
4. Browser download through the card and an independent HTTP download both produce
   `CamStationViewer.msi` with the source size and SHA-256.
5. The same URL works after one controlled canary recreation, proving persistent publication.
6. Windows has zero exact Viewer product, service, process, task, owned path, registry/autostart, and
   profile residue while SSH, Explorer, and `C:\CamStationDev` remain healthy.

## Rollback

- Server: restore the prior image tag from the root-only `.env` backup and recreate only the canary.
  The new release directory may remain inert in the isolated state mount; do not delete it during
  runtime rollback.
- Windows: the requested end state is uninstalled. Reinstall only by explicit operator action using
  the hash-verified settings-page download; do not silently reinstall as rollback.
