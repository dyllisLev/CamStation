# CamStation 2.0 Mobile Viewer Design

## Goal

CamStation 2.0 exposes a dedicated mobile monitoring surface at `/viewer`. It is a full-viewport, read-only camera viewer and does not inherit the desktop console sidebar or the `/live` operator controls.

## Legacy evidence

The local `main` branch at commit `21e1e24` has two separate viewer entries:

- `/viewer` renders `ViewerPage`, a read-only saved-layout grid.
- `/mobile` renders `MobilePage`, the purpose-built mobile flow with a four-camera paged grid, camera detail, and fullscreen view.

The requested product contract is explicit that the CamStation 2.0 mobile viewer must use `/viewer`. Therefore 2.0 uses the legacy mobile interaction model at the requested `/viewer` address; it does not preserve the old `/viewer` versus `/mobile` split.

## User experience

### Route and shell

- `/viewer` is a top-level React route outside `ConsoleLayout`.
- Direct navigation and refresh are served by the existing embedded SPA fallback.
- The page fills the dynamic viewport and respects mobile safe-area insets.

### Grid

- Only enabled cameras are included.
- Cameras are presented four per page in a fixed 2×2 grid.
- A horizontal gesture beyond 50 CSS pixels moves one page left or right.
- Page indicators show the current page when multiple pages exist.
- Only the visible page owns live player connections so off-screen pages do not consume mobile bandwidth.
- Each tile shows the camera label and a streaming/degraded/offline state marker.

### Detail

- Selecting a tile opens a single-camera detail view.
- The detail view provides close, previous, next, and fullscreen controls.
- A KST clock, camera label, playback state, and recording indicator remain compact overlays.
- Camera identity is tracked by stable `streamName`, not array index, so a query refresh cannot silently switch the selected camera.

### Fullscreen

- Non-iOS browsers request fullscreen directly from the initiating tap and then request landscape orientation when supported.
- iOS uses the video element's native fullscreen API when available.
- If browser fullscreen is denied, the app still provides a fixed full-viewport viewer with an explicit close control.
- Exiting browser fullscreen returns to the same camera detail view.

### Playback

- Grid tiles prefer the camera's applied `live` output.
- Detail and fullscreen prefer the applied `focus` output.
- Both reuse the existing bounded WebRTC-primary/MSE-fallback recovery hook and its public `/player/api/ws` proxy.
- No camera URL, credential, raw go2rtc endpoint, or runtime path is exposed.

### States

- Loading, camera-query failure with retry, and no-active-camera states are explicit.
- If the selected camera becomes unavailable, the page returns to the grid.
- If camera count shrinks, the current grid page is clamped to the last valid page.

## Non-goals

- PTZ, saved-layout editing, timeline, recordings playback, and desktop viewer-agent registration are not part of this route.
- This change does not add PWA installation, a service worker, or a `/mobile` compatibility route.
- This change does not alter stream-output policy or recorder behavior.

## Acceptance criteria

1. A direct GET of `/viewer` returns the embedded SPA, and React renders the mobile viewer without console navigation.
2. Five or more enabled cameras produce multiple four-camera pages navigable by swipe and page controls.
3. A tile opens detail; previous/next preserve camera order; close returns to the grid.
4. Fullscreen is requested from the user gesture, and exiting it returns to detail.
5. Grid playback selects live-first candidates; detail/fullscreen select focus-first candidates.
6. Web tests, lint, frontend build, Go tests, and daemon build pass.
