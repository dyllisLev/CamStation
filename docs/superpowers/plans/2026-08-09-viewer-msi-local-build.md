# Viewer MSI local build implementation plan

**Goal:** restore a safe, one-command Windows-local MSI build without using the monitoring NUC as a
build machine.

**Design:** a PowerShell entry point validates its Windows toolchain, runs existing Viewer policy
tests, packages Electron, cross-builds the Go service, copies only WiX authoring into an ignored
workspace, generates the release-specific file fragment there, restores locked WiX packages, builds
and inspects the MSI, then writes secret-free hash metadata. Linux tests statically enforce this
contract; a dedicated Windows execution remains the final artifact gate.

## Implementation checklist

- [x] Write RED tests for the public command and failure boundaries.
- [x] Implement the isolated Windows build script and MSI database inspection.
- [x] Document prerequisites, quick start, outputs, failures, and the NUC prohibition.
- [x] Pass focused and full Viewer tests plus PowerShell syntax validation.
- [x] Record what is locally proven and what still requires a Windows build run.
