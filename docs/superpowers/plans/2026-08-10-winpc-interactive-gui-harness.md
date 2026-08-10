# WIN11-DELL interactive GUI harness plan

1. Confirm one active `dyllislev` Explorer session, Task Scheduler availability, Viewer installation,
   UI Automation/System.Drawing support, and absence of an existing GUI bridge namespace.
2. Add a window-only capture worker, one-shot interactive-task launcher, and source-policy tests.
3. Run Viewer tests and PowerShell parser checks; synchronize exact file hashes to WIN11-DELL.
4. Invoke `LaunchAndCapture`, wait for atomic completion, and confirm task deletion.
5. Retrieve and inspect `viewer-window.png` and `uia.json`; record identity, session, PID, dimensions,
   hashes, and any visual defect without configuring a server.
6. Recheck Viewer service, RDP session, task/process cleanup, and document the next interactive input
   operation.

## Result

Completed on 2026-08-10. Two independent runs proved launch-and-capture and repeat-capture behavior
in the existing RDP session. The stable target-window image was inspected directly, and the settled
UIA tree exposes `server-url` and `display-name`; a future bounded input operation can therefore use
those exact automation IDs instead of screen coordinates. No Viewer configuration was changed.
