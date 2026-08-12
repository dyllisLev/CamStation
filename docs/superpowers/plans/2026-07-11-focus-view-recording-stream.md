# Focus View Recording Stream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make only the enlarged `/live` focus tile play the camera's recording-quality stream while the normal grid and recorder workers keep their existing streams.

**Architecture:** Extract the existing playback-name choice into a small pure TypeScript helper so its normal and focused fallback rules can be tested without rendering React. `CameraTile` passes its existing `zoomed` state to that helper; no Go API, go2rtc configuration, or recorder code changes.

**Tech Stack:** React 19, TypeScript 6, Node 22 built-in test runner with type stripping, Vite 8, Go 1.x daemon.

## Global Constraints

- Normal tiles select `liveStreamName` then `streamName`.
- Focused tiles select `recordingStreamName`, then `liveStreamName`, then `streamName`.
- Do not change recorder workers, ffmpeg commands, go2rtc configuration generation, public APIs, or camera credentials.
- Do not preconnect the recording-quality stream.
- Keep existing MSE connection and `연결 중...` behavior.
- Do not hand-edit generated files under `cmd/camstationd/web`; regenerate them with `cd web && npm run build`.
- Preserve all pre-existing worktree changes and stage only files owned by this feature.

---

### Task 1: Select the recording stream only for focus view

**Files:**
- Create: `web/src/components/live/streamSelection.ts`
- Create: `web/tests/streamSelection.test.ts`
- Modify: `web/src/components/live/LiveWorkspace.tsx:1-18,537-565,703-705`

**Interfaces:**
- Consumes: `Camera` fields `streamName`, optional `liveStreamName`, and optional `recordingStreamName` from `web/src/app/cameraTypes.ts`.
- Produces: `playbackStreamName(camera: CameraPlaybackStreams, focused?: boolean): string` for `CameraTile`.

- [ ] **Step 1: Write the failing stream-selection test**

Create `web/tests/streamSelection.test.ts`:

```ts
import assert from "node:assert/strict";
import test from "node:test";
import { playbackStreamName } from "../src/components/live/streamSelection.ts";

const dualStreamCamera = {
  streamName: "yard",
  liveStreamName: "yard-live",
  recordingStreamName: "yard-recording",
};

test("normal view uses the live stream", () => {
  assert.equal(playbackStreamName(dualStreamCamera), "yard-live");
});

test("focus view uses the recording stream", () => {
  assert.equal(playbackStreamName(dualStreamCamera, true), "yard-recording");
});

test("focus view falls back through live to the stable stream name", () => {
  assert.equal(playbackStreamName({ streamName: "single", liveStreamName: "single-live" }, true), "single-live");
  assert.equal(playbackStreamName({ streamName: "legacy" }, true), "legacy");
});
```

- [ ] **Step 2: Run the test and verify the RED state**

Run:

```bash
cd web && node --experimental-strip-types --test tests/streamSelection.test.ts
```

Expected: FAIL with `ERR_MODULE_NOT_FOUND` for `src/components/live/streamSelection.ts`.

- [ ] **Step 3: Implement the minimal pure helper**

Create `web/src/components/live/streamSelection.ts`:

```ts
import type { Camera } from "../../app/api";

type CameraPlaybackStreams = Pick<Camera, "streamName" | "liveStreamName" | "recordingStreamName">;

export function playbackStreamName(camera: CameraPlaybackStreams, focused = false) {
  return (focused ? camera.recordingStreamName : "") || camera.liveStreamName || camera.streamName;
}
```

- [ ] **Step 4: Wire the existing focus state into the helper**

In `web/src/components/live/LiveWorkspace.tsx`, add:

```ts
import { playbackStreamName } from "./streamSelection";
```

Change the online tile playback to:

```tsx
<LiveVideo streamName={playbackStreamName(camera, zoomed)} viewport={videoViewport} onViewportChange={onVideoViewportChange} />
```

Delete the old local helper:

```ts
function playbackStreamName(camera: Camera) {
  return camera.liveStreamName || camera.streamName;
}
```

- [ ] **Step 5: Run the focused test and verify the GREEN state**

Run:

```bash
cd web && node --experimental-strip-types --test tests/streamSelection.test.ts
```

Expected: three tests pass, zero fail.

- [ ] **Step 6: Run frontend static verification and regenerate embedded assets**

Run:

```bash
cd web && npm run lint
cd web && npm run build
```

Expected: lint exits 0; TypeScript and Vite build exit 0. Generated `cmd/camstationd/web` changes remain unstaged because that directory already contains unrelated user build output.

- [ ] **Step 7: Commit only the feature source and regression test**

```bash
git add web/src/components/live/LiveWorkspace.tsx web/src/components/live/streamSelection.ts web/tests/streamSelection.test.ts
git diff --cached --check
git commit -m "feat: use recording stream in focus view"
```

Expected: the commit contains only the three listed files.

### Task 2: Verify recorder isolation and record shipped behavior

**Files:**
- Modify: `docs/07-implementation-status.md:77-82,157-170`

**Interfaces:**
- Consumes: the stream-selection behavior from Task 1 and read-only recorder/go2rtc status APIs.
- Produces: verified build evidence and implementation-status documentation; no runtime configuration changes.

- [ ] **Step 1: Run full backend tests and build the daemon**

Run:

```bash
go test ./...
go build -o camstationd ./cmd/camstationd
```

Expected: all Go packages pass and the daemon build exits 0.

- [ ] **Step 2: Confirm the code diff cannot mutate recorder state**

Run:

```bash
git diff HEAD^ -- web/src/components/live/LiveWorkspace.tsx web/src/components/live/streamSelection.ts web/tests/streamSelection.test.ts
git diff HEAD^ -- cmd/camstationd internal/recorder internal/stream
```

Expected: the first diff only changes browser stream-name selection; the second command prints no feature-owned Go source diff.

- [ ] **Step 3: Capture current runtime stream and recorder status without restarting the daemon**

Run:

```bash
curl -fsS http://127.0.0.1:18080/api/cameras | jq '[.[] | {name, liveStreamName, recordingStreamName}]'
curl -fsS http://127.0.0.1:18080/api/recorders/status | jq '{enabled, workers: [.workers[]? | {streamName, state, input}]}'
scripts/camstationctl.sh status
```

Expected: cameras retain distinct role names where configured, and recorder workers—if any—retain their existing `recordingStreamName` local RTSP inputs. Do not restart the daemon because that would itself interrupt active recording.

- [ ] **Step 4: Update implementation status**

Add these statements to the focus-view shipped/verified sections of `docs/07-implementation-status.md`:

```markdown
- normal `/live` tiles use the camera's live role stream
- `집중 보기` uses the recording role stream, falling back to the live/stable stream name when unavailable
- changing focus view does not reconfigure or restart recorder workers
```

- [ ] **Step 5: Run final verification**

Run:

```bash
cd web && node --experimental-strip-types --test tests/streamSelection.test.ts
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
git diff --check
```

Expected: three Node tests pass; lint, web build, all Go tests, daemon build, and diff check exit 0.

- [ ] **Step 6: Commit the implementation-status update**

```bash
git add docs/07-implementation-status.md
git diff --cached --check
git commit -m "docs: record focus stream behavior"
```

Expected: the commit contains only `docs/07-implementation-status.md`.
