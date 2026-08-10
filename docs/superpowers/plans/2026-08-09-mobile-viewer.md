# CamStation 2.0 Mobile Viewer Implementation Plan

## Baseline

The implementation must preserve the current React/Vite architecture, embedded Go SPA, public camera DTOs, and `/player` proxy. Generated files under `cmd/camstationd/web` are produced only by the frontend build.

## Task 1: Add deterministic mobile-viewer state helpers

Files:

- Add `web/src/pages/viewer/mobileViewerModel.ts`.
- Add `web/tests/mobileViewerModel.test.ts`.

Behavior:

- Four cameras per page.
- Strict 50-pixel swipe threshold matching the legacy mobile page.
- Safe page clamping as camera counts change.
- Generic page slicing that does not depend on private camera fields.

Verification:

- `cd web && npm test -- --test-name-pattern='mobile viewer'`

## Task 2: Build the mobile viewer page

Files:

- Add `web/src/pages/ViewerPage.tsx`.
- Add `web/src/pages/viewer/MobileViewerVideo.tsx`.
- Add `web/src/pages/viewer/mobileViewer.css`.

Behavior:

- Query and filter enabled cameras.
- Render the four-camera grid, detail, and fullscreen states.
- Use `streamName` as selected identity.
- Reuse `playbackStreamCandidates` and `useWebRtcMseStream`.
- Mount playback only for the current grid page.
- Keep controls accessible and safe-area aware.
- Handle fullscreen/orientation as best-effort browser capabilities.

Verification:

- TypeScript build proves component/type integration.
- Source inspection confirms no raw stream URLs or credentials are introduced.

## Task 3: Register `/viewer` outside the console shell

Files:

- Update `web/src/app/App.tsx`.
- Extend `web/tests/mobileViewerModel.test.ts` with route-source verification.

Behavior:

- Render `ViewerPage` for `/viewer` before the `ConsoleLayout` branch.
- Leave `/`, `/live`, and all console routes unchanged.

Verification:

- Unit test proves the top-level route registration.
- Built daemon receives `/viewer` through the SPA fallback.

## Task 4: Reconcile documentation and complete verification

Files:

- Update `docs/07-implementation-status.md`.
- Update `tasks/todo.md` review and `tasks/lessons.md`.

Commands:

```bash
cd web && npm test
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o /tmp/camstationd-mobile-viewer ./cmd/camstationd
```

Surface check:

- Start only an isolated built daemon fixture if existing tests cannot exercise the embedded SPA.
- Request `/viewer` and confirm a successful HTML response containing the built app entry.
- Do not restart or disturb a real `cctv` or `cctv2` runtime for this feature verification.
