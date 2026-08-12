# Viewer recordings access and native fullscreen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the installed Windows Viewer open read-only recorded-video playback and use true native window fullscreen.

**Architecture:** Keep the Electron main process as the navigation and window-state authority. The preload bridge exposes only a narrow fullscreen request/state interface to the server-hosted React UI. The React UI preserves normal browser behavior but recognizes Viewer mode for the recordings route and exposes playback without administrative recording controls.

**Tech Stack:** Electron 43, TypeScript, React 19, React Router, Node test runner, Vite, Windows MSI.

## Global Constraints

- Permit only the configured server origin and the Viewer live/recordings routes; settings remains unavailable.
- Do not expose camera URLs, credentials, go2rtc addresses, runtime paths, or secrets.
- Viewer fullscreen must be native Electron fullscreen: no title bar, app menu, or Windows taskbar.
- Preserve ordinary browser-console fullscreen behavior and existing service reconnect behavior.
- Do not edit Vite-generated `cmd/camstationd/web` files directly; build them from `web/`.

---

### Task 1: Restrict navigation to live and recordings

**Files:**
- Modify: `viewer-app/src/navigation.ts`
- Modify: `viewer-app/tests/navigation.test.ts`

**Interfaces:**
- Produces: `isNavigationAllowed(candidate: string, liveURL: string): boolean`, returning true only for the exact live Viewer URL or the same-origin `/recordings?viewer=1` route.
- Consumed by: `viewer-app/src/main.ts` `will-navigate` handler.

- [ ] **Step 1: Write the failing navigation test**

```ts
test("Viewer navigation permits only live and recordings", () => {
  const live = "http://viewer.example:18080/live?viewer=1";
  assert.equal(isNavigationAllowed("http://viewer.example:18080/recordings?viewer=1", live), true);
  assert.equal(isNavigationAllowed("http://viewer.example:18080/settings", live), false);
  assert.equal(isNavigationAllowed("http://viewer.example:18080/recordings", live), false);
});
```

- [ ] **Step 2: Run the focused test and verify failure**

Run: `cd viewer-app && npm test -- tests/navigation.test.ts`

Expected: FAIL because recordings is currently rejected.

- [ ] **Step 3: Implement the minimal allow-list**

```ts
export function isNavigationAllowed(candidate: string, liveURL: string): boolean {
  try {
    const live = new URL(liveURL);
    const next = new URL(candidate);
    return next.origin === live.origin && (
      next.href === live.href ||
      (next.pathname === "/recordings" && next.search === "?viewer=1")
    );
  } catch {
    return false;
  }
}
```

- [ ] **Step 4: Run the focused test and verify success**

Run: `cd viewer-app && npm test -- tests/navigation.test.ts`

Expected: PASS.

### Task 2: Add native fullscreen IPC

**Files:**
- Modify: `viewer-app/src/preload.ts`
- Modify: `viewer-app/src/main.ts`
- Modify: `viewer-app/tests/preload.test.ts`
- Modify: `viewer-app/tests/navigation.test.ts`

**Interfaces:**
- Produces: preload methods `setFullscreen(fullscreen: boolean): Promise<unknown>` and `onFullscreenChange(handler: (fullscreen: boolean) => void): () => void`.
- Consumes: `ipcMain.handle("viewer:set-fullscreen", ...)` and the BrowserWindow `enter-full-screen`/`leave-full-screen` events.
- Consumed by: `web/src/components/live/viewerBridge.ts`.

- [ ] **Step 1: Write failing preload and main-process contract tests**

```ts
assert.deepEqual(Object.keys(bridge).sort(), [
  "getSetupState", "onCommand", "onFullscreenChange", "reportRenderer",
  "reportStream", "retryConnection", "saveConfiguration", "setFullscreen",
]);
assert.match(source, /ipcMain\.handle\("viewer:set-fullscreen"/u);
assert.match(source, /window\.on\("enter-full-screen"/u);
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `cd viewer-app && npm test -- tests/preload.test.ts tests/navigation.test.ts`

Expected: FAIL because the bridge and native handlers do not exist.

- [ ] **Step 3: Implement the narrow native fullscreen bridge**

```ts
// preload
setFullscreen(fullscreen: boolean) {
  return ipc.invoke?.("viewer:set-fullscreen", Boolean(fullscreen));
},
onFullscreenChange(handler: (fullscreen: boolean) => void) {
  const listener = (_event: unknown, fullscreen: unknown) => {
    if (typeof fullscreen === "boolean") handler(fullscreen);
  };
  ipc.on("viewer:fullscreen-changed", listener);
  return () => ipc.removeListener("viewer:fullscreen-changed", listener);
},

// main
ipcMain.handle("viewer:set-fullscreen", (_event, fullscreen: unknown) => {
  const next = Boolean(fullscreen);
  window?.setFullScreen(next);
  return window?.isFullScreen() ?? false;
});
window.on("enter-full-screen", () => window?.webContents.send("viewer:fullscreen-changed", true));
window.on("leave-full-screen", () => window?.webContents.send("viewer:fullscreen-changed", false));
```

- [ ] **Step 4: Run focused tests and verify success**

Run: `cd viewer-app && npm test -- tests/preload.test.ts tests/navigation.test.ts`

Expected: PASS.

### Task 3: Make Viewer recordings playback-only and connect fullscreen UI

**Files:**
- Create: `web/src/app/viewerMode.ts`
- Create: `web/tests/viewerMode.test.ts`
- Modify: `web/src/components/live/viewerBridge.ts`
- Modify: `web/src/components/live/LiveWorkspace.tsx`
- Modify: `web/src/pages/RecordingsPage.tsx`
- Modify: `web/src/pages/recordings/RecordingSegmentsPanel.tsx`

**Interfaces:**
- Produces: `isViewerMode(search: string): boolean` and `viewerRoute(path: "/live" | "/recordings"): string`.
- Produces: `requestViewerFullscreen(fullscreen: boolean)` and `subscribeViewerFullscreen(handler)` that safely no-op outside Electron.
- Consumes: preload methods from Task 2.

- [ ] **Step 1: Write failing Viewer-mode tests**

```ts
test("Viewer mode requires the exact viewer query", () => {
  assert.equal(isViewerMode("?viewer=1"), true);
  assert.equal(isViewerMode("?viewer=0"), false);
  assert.equal(viewerRoute("/recordings"), "/recordings?viewer=1");
});
```

- [ ] **Step 2: Run the focused test and verify failure**

Run: `cd web && npm test -- tests/viewerMode.test.ts`

Expected: FAIL because the Viewer-mode helpers do not exist.

- [ ] **Step 3: Implement the minimal UI behavior**

```ts
// viewerMode.ts
export function isViewerMode(search: string): boolean {
  return new URLSearchParams(search).get("viewer") === "1";
}
export function viewerRoute(path: "/live" | "/recordings"): string {
  return `${path}?viewer=1`;
}
```

- In `LiveWorkspace`, route 녹화 through `viewerRoute("/recordings")` when Viewer mode is active, omit the settings link in Viewer mode, and request/subcribe native fullscreen through `viewerBridge`; retain DOM fullscreen only when no Viewer bridge exists.
- In `RecordingsPage`, when Viewer mode is active, render only the segment list/detail playback surface, not storage cleanup or recorder worker controls.
- In `RecordingSegmentsPanel`, add `readOnly` and make the Viewer row's 재생 action select its segment for inline video playback; omit delete, download, and new-window actions in read-only mode.

- [ ] **Step 4: Run focused test and frontend checks**

Run: `cd web && npm test -- tests/viewerMode.test.ts && npm run lint && npm run build`

Expected: PASS, then Vite emits updated assets to `cmd/camstationd/web`.

### Task 4: Package and validate on the Windows VM

**Files:**
- Modify: no source files

**Interfaces:**
- Consumes: Tasks 1-3 artifacts, `scripts/camstationctl.sh`, and the existing Windows MSI deployment path.

- [ ] **Step 1: Run complete local verification**

Run: `cd viewer-app && npm test && npm run build && npm run package:win`; then `git diff --check`.

Expected: all tests, TypeScript builds, MSI packaging, and whitespace verification pass.

- [ ] **Step 2: Rebuild and restart only the CamStation test server**

Run: `go build -o camstationd ./cmd/camstationd && scripts/camstationctl.sh restart && scripts/camstationctl.sh verify`

Expected: health check succeeds. Do not affect legacy CCTV services.

- [ ] **Step 3: Install the newly packaged MSI on the approved Windows VM**

Run the existing remote Windows installer workflow using the approved SSH key, then confirm the service and Viewer process state.

Expected: installer exits successfully and `CamStationViewerAgent` is Running.

- [ ] **Step 4: Perform VM acceptance checks**

1. Open the Viewer live screen and select 녹화.
2. Verify the recording list loads; select a segment and verify inline video playback controls appear.
3. Verify no settings navigation is available from the Viewer and a direct settings navigation is rejected.
4. Select 전체화면; verify the app title, application menu, and taskbar are absent.
5. Press Escape; verify the normal framed window returns.

Expected: all four user-visible requirements pass.

### Task 5: Keep Viewer route switching available on recordings

**Files:**
- Modify: `web/src/layouts/ConsoleLayout.tsx`
- Modify: `web/tests/viewerMode.test.ts`

**Interfaces:**
- Consumes: `isViewerMode(search)` and `viewerRoute(path)` from
  `web/src/app/viewerMode.ts`.
- Produces: a Viewer-only shared header with links to `/live?viewer=1` and
  `/recordings?viewer=1`, with no administrative links.

- [ ] **Step 1: Write the failing regression test**

```ts
test("Viewer layout retains live and recordings navigation only", async () => {
  const source = await readFile(path.resolve(import.meta.dirname, "../src/layouts/ConsoleLayout.tsx"), "utf8");
  assert.match(source, /viewerRoute\("\/live"\)/u);
  assert.match(source, /viewerRoute\("\/recordings"\)/u);
  assert.doesNotMatch(source, /viewerMode[\s\S]*viewerRoute\("\/settings"\)/u);
});
```

- [ ] **Step 2: Run the focused test and verify failure**

Run: `cd web && node --experimental-strip-types --test tests/viewerMode.test.ts`

Expected: FAIL because the Viewer layout currently contains no shared route tabs.

- [ ] **Step 3: Add the compact shared Viewer header**

```tsx
if (isViewerMode(location.search)) {
  return (
    <div className="new-console-app">
      <nav aria-label="Viewer 화면">
        <a href={withAppBase(viewerRoute("/live"))}>라이브</a>
        <a href={withAppBase(viewerRoute("/recordings"))}>녹화</a>
      </nav>
      <main className="new-console-main px-4 py-5 lg:px-6"><Outlet /></main>
    </div>
  );
}
```

- [ ] **Step 4: Run focused and complete web verification**

Run: `cd web && node --experimental-strip-types --test tests/viewerMode.test.ts && npm test && npm run lint && npm run build`

Expected: all tests pass; Vite emits the updated server assets.

- [ ] **Step 5: Rebuild and validate on the Windows VM**

Run: rebuild only the CamStation test server with `scripts/camstationctl.sh`, then navigate the installed Viewer between `/recordings?viewer=1` and `/live?viewer=1`.

Expected: the 녹화 screen visibly provides `라이브`; both links preserve `viewer=1`; settings remains unavailable.
