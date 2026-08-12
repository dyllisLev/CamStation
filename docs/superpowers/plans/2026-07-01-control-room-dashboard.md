# Control Room Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `/` as an operator dashboard while keeping `/live` as the current video workspace.

**Architecture:** Add a focused `ControlRoomPage` dashboard that composes existing query hooks. Keep `LiveWorkspace` owned by `LivePage`. Add a small preview modal inside the dashboard that mounts a single MSE player only while open.

**Tech Stack:** React 19, React Router 7, TanStack Query, lucide-react, existing Go static embed and Go tests.

## Global Constraints

- `/live` remains the current multi-camera video workspace.
- `/` does not auto-play camera video.
- The control room can open a temporary single-camera modal preview.
- Use existing APIs for the first pass: cameras, streams status, recorder status, recording storage, events, stream restart.
- Show `-` for connection counts until accurate go2rtc consumer data is exposed.
- Keep the existing left console sidebar.
- Final verification: `npm run build` and `go test ./...`.

---

## File Structure

- `web/src/pages/ControlRoomPage.tsx`: Replace the current `LiveWorkspace` passthrough with the dashboard UI, summary calculations, camera table, operations panel, and preview modal.
- `web/src/pages/LivePage.tsx`: Keep rendering `LiveWorkspace`.
- `web/src/styles/index.css`: Add compact dashboard/table/modal styles that match the existing console visual language.
- `cmd/camstationd/main_test.go`: Extend the existing source-level regression tests so `/` cannot accidentally return `LiveWorkspace` again and `/live` keeps it.
- `cmd/camstationd/web/*`: Regenerated frontend build output after implementation.

---

### Task 1: Lock Route Ownership

**Files:**
- Modify: `cmd/camstationd/main_test.go`
- Modify: `web/src/pages/ControlRoomPage.tsx`

**Interfaces:**
- Consumes: Current page files at `web/src/pages/ControlRoomPage.tsx` and `web/src/pages/LivePage.tsx`.
- Produces: A regression test named `TestConsolePagesKeepSeparateRoles`.

- [ ] **Step 1: Write the failing test**

Add this test to `cmd/camstationd/main_test.go`:

```go
func TestConsolePagesKeepSeparateRoles(t *testing.T) {
	t.Parallel()

	controlRoom, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "pages", "ControlRoomPage.tsx"))
	if err != nil {
		t.Fatalf("read control room page: %v", err)
	}
	live, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "pages", "LivePage.tsx"))
	if err != nil {
		t.Fatalf("read live page: %v", err)
	}

	if strings.Contains(string(controlRoom), "LiveWorkspace") {
		t.Fatalf("ControlRoomPage must not render LiveWorkspace directly")
	}
	if !strings.Contains(string(controlRoom), "ControlRoomDashboard") {
		t.Fatalf("ControlRoomPage must render ControlRoomDashboard")
	}
	if !strings.Contains(string(live), "LiveWorkspace") {
		t.Fatalf("LivePage must keep rendering LiveWorkspace")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
```

Expected: FAIL with `ControlRoomPage must not render LiveWorkspace directly`.

- [ ] **Step 3: Create the minimal component boundary**

Replace `web/src/pages/ControlRoomPage.tsx` with:

```tsx
function ControlRoomDashboard() {
  return (
    <div className="new-control-room">
      <div className="new-panel">
        <div className="new-panel-body">
          <div className="text-sm font-semibold text-slate-100">관제실</div>
          <div className="mt-1 text-xs text-slate-500">운영 상태를 불러오는 중입니다.</div>
        </div>
      </div>
    </div>
  );
}

export function ControlRoomPage() {
  return <ControlRoomDashboard />;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/camstationd/main_test.go web/src/pages/ControlRoomPage.tsx
git commit -m "Separate control room from live workspace"
```

---

### Task 2: Build Dashboard Data Model And Summary Band

**Files:**
- Modify: `web/src/pages/ControlRoomPage.tsx`

**Interfaces:**
- Consumes: `useCameras`, `useStreamStatus`, `useRecorderStatus`, `useRecordingStorage`, `useEvents` from `web/src/app/queries.ts`.
- Produces: Local helper `formatBytes(value: number | undefined): string` and summary cards rendered with class `new-control-summary`.

- [ ] **Step 1: Write the failing source test**

Add these checks inside `TestConsolePagesKeepSeparateRoles` after reading `controlRoom`:

```go
for _, required := range []string{
	"useCameras",
	"useStreamStatus",
	"useRecorderStatus",
	"useRecordingStorage",
	"useEvents",
	"new-control-summary",
	"커넥션",
	"저장공간",
} {
	if !strings.Contains(string(controlRoom), required) {
		t.Fatalf("ControlRoomPage missing dashboard requirement %q", required)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
```

Expected: FAIL naming the first missing dashboard requirement.

- [ ] **Step 3: Implement summary data and tiles**

Update `web/src/pages/ControlRoomPage.tsx` to import the query hooks and render summary counters:

```tsx
import { Activity, AlertTriangle, Camera, Database, RadioTower, Users, Video } from "lucide-react";
import { useCameras, useEvents, useRecorderStatus, useRecordingStorage, useStreamStatus } from "../app/queries";

function ControlRoomDashboard() {
  const cameras = useCameras();
  const streams = useStreamStatus();
  const recorders = useRecorderStatus();
  const storage = useRecordingStorage();
  const events = useEvents();

  const cameraRows = cameras.data ?? [];
  const recorderWorkers = recorders.data?.workers ?? [];
  const recentErrors = (events.data ?? []).filter((event) => event.level === "error").length;
  const online = cameraRows.filter((camera) => camera.state === "streaming").length;
  const runningRecorders = recorderWorkers.filter((worker) => worker.state === "running").length;
  const storageBytes = (storage.data?.recordingsBytes ?? 0) + (storage.data?.tempBytes ?? 0);

  const summary = [
    { label: "카메라", value: `${online}/${cameraRows.length}`, detail: "온라인 / 전체", icon: Camera },
    { label: "송출", value: streams.data?.running ? "running" : "offline", detail: streams.data?.apiUrl ?? "-", icon: RadioTower },
    { label: "녹화", value: `${runningRecorders}/${recorderWorkers.length}`, detail: "실행 워커", icon: Video },
    { label: "커넥션", value: "-", detail: "go2rtc 소비자 API 필요", icon: Users },
    { label: "저장공간", value: formatBytes(storageBytes), detail: storage.data?.autoCleanupEnabled ? "자동정리 켜짐" : "자동정리 꺼짐", icon: Database },
    { label: "최근 오류", value: String(recentErrors), detail: "최근 이벤트 100개 기준", icon: AlertTriangle },
  ];

  return (
    <div className="new-control-room">
      <section className="new-control-summary" aria-label="관제실 요약">
        {summary.map((item) => (
          <div className="new-control-stat" key={item.label}>
            <div className="new-feature-icon"><item.icon size={17} /></div>
            <div>
              <div className="new-control-stat-label">{item.label}</div>
              <div className="new-control-stat-value">{item.value}</div>
              <div className="new-control-stat-detail">{item.detail}</div>
            </div>
          </div>
        ))}
      </section>
    </div>
  );
}

function formatBytes(value: number | undefined) {
  if (!value || value <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}
```

- [ ] **Step 4: Run tests and type build**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
cd web && npm run build
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/camstationd/main_test.go web/src/pages/ControlRoomPage.tsx cmd/camstationd/web
git commit -m "Add control room summary dashboard"
```

---

### Task 3: Add Camera Status Table And Operations Panel

**Files:**
- Modify: `cmd/camstationd/main_test.go`
- Modify: `web/src/pages/ControlRoomPage.tsx`
- Modify: `web/src/styles/index.css`

**Interfaces:**
- Consumes: Summary dashboard from Task 2 and query data.
- Produces: Camera status table with class `new-control-table`, operations panel with class `new-control-ops`, and fallback degraded states.

- [ ] **Step 1: Write the failing source test**

Add these checks inside `TestConsolePagesKeepSeparateRoles`:

```go
for _, required := range []string{
	"new-control-table",
	"연결",
	"송출",
	"녹화",
	"최근 오류",
	"new-control-ops",
	"Recorder workers",
	"Recent events",
} {
	if !strings.Contains(string(controlRoom), required) {
		t.Fatalf("ControlRoomPage missing table or operations requirement %q", required)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
```

Expected: FAIL naming a missing table or operations requirement.

- [ ] **Step 3: Implement table and operations panel**

Extend `ControlRoomDashboard` with a two-column body:

```tsx
<section className="new-control-grid">
  <div className="new-panel">
    <div className="new-panel-header">
      <h2 className="text-sm font-semibold">카메라 상태</h2>
    </div>
    <div className="new-panel-body">
      <div className="new-table-wrap">
        <table className="new-table new-control-table">
          <thead>
            <tr>
              <th className="px-3 py-2 font-medium">카메라</th>
              <th className="px-3 py-2 font-medium">연결</th>
              <th className="px-3 py-2 font-medium">송출</th>
              <th className="px-3 py-2 font-medium">커넥션</th>
              <th className="px-3 py-2 font-medium">녹화</th>
              <th className="px-3 py-2 font-medium">최근 오류</th>
              <th className="px-3 py-2 font-medium">작업</th>
            </tr>
          </thead>
          <tbody>
            {cameraRows.map((camera) => {
              const worker = recorderWorkers.find((item) => item.streamName === camera.streamName);
              return (
                <tr key={camera.id}>
                  <td className="px-3 py-3 font-semibold text-slate-100">{camera.name}</td>
                  <td className="px-3 py-3">{camera.state}</td>
                  <td className="px-3 py-3">{streams.data?.running ? "running" : "offline"}</td>
                  <td className="px-3 py-3">-</td>
                  <td className="px-3 py-3">{worker?.state ?? "stopped"}</td>
                  <td className="px-3 py-3">{camera.lastProbe?.reachable === false ? "probe failed" : "-"}</td>
                  <td className="px-3 py-3">
                    <div className="new-control-actions">
                      <button className="new-ghost" type="button">보기</button>
                      <a className="new-ghost" href="/live">라이브</a>
                    </div>
                  </td>
                </tr>
              );
            })}
            {cameraRows.length === 0 && (
              <tr>
                <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={7}>등록된 카메라가 없습니다.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  </div>

  <aside className="new-control-ops">
    <section className="new-panel">
      <div className="new-panel-header"><h2 className="text-sm font-semibold">Recorder workers</h2></div>
      <div className="new-panel-body space-y-2">
        {recorderWorkers.map((worker) => (
          <div className="new-control-row" key={worker.streamName}>
            <span>{worker.streamName}</span>
            <em>{worker.state}</em>
          </div>
        ))}
      </div>
    </section>
    <section className="new-panel">
      <div className="new-panel-header"><h2 className="text-sm font-semibold">Recent events</h2></div>
      <div className="new-panel-body space-y-2">
        {(events.data ?? []).slice(0, 8).map((event) => (
          <div className="new-control-event" key={event.id}>
            <span>{event.message}</span>
            <em>{event.level}</em>
          </div>
        ))}
      </div>
    </section>
  </aside>
</section>
```

Add CSS:

```css
.new-control-room { display: grid; gap: 14px; }
.new-control-summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; }
.new-control-stat { min-height: 92px; display: flex; align-items: center; gap: 12px; border: 1px solid var(--new-border); border-radius: 8px; background: color-mix(in oklch, var(--new-panel), black 10%); padding: 12px; }
.new-control-stat-label, .new-control-stat-detail { color: var(--new-muted); font-size: 11px; }
.new-control-stat-value { margin-top: 2px; color: var(--new-fg); font-size: 18px; font-weight: 760; }
.new-control-grid { display: grid; grid-template-columns: minmax(0, 1fr) 22rem; gap: 14px; align-items: start; }
.new-control-actions { display: flex; gap: 6px; flex-wrap: wrap; }
.new-control-ops { display: grid; gap: 14px; }
.new-control-row, .new-control-event { display: flex; justify-content: space-between; gap: 10px; color: var(--new-muted); font-size: 12px; }
.new-control-row em, .new-control-event em { color: var(--new-dim); font-style: normal; }
@media (max-width: 1100px) { .new-control-grid { grid-template-columns: 1fr; } }
```

- [ ] **Step 4: Run tests and build**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
cd web && npm run build
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/camstationd/main_test.go web/src/pages/ControlRoomPage.tsx web/src/styles/index.css cmd/camstationd/web
git commit -m "Add control room camera status table"
```

---

### Task 4: Add On-Demand Preview Modal

**Files:**
- Modify: `cmd/camstationd/main_test.go`
- Modify: `web/src/pages/ControlRoomPage.tsx`
- Modify: `web/src/styles/index.css`

**Interfaces:**
- Consumes: Camera rows from Task 3.
- Produces: Modal state `previewCamera`, component `CameraPreviewModal`, and CSS class `new-preview-modal`.

- [ ] **Step 1: Write the failing source test**

Add these checks inside `TestConsolePagesKeepSeparateRoles`:

```go
for _, required := range []string{
	"CameraPreviewModal",
	"previewCamera",
	"new-preview-modal",
	"useMseStream",
} {
	if !strings.Contains(string(controlRoom), required) {
		t.Fatalf("ControlRoomPage missing preview modal requirement %q", required)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
```

Expected: FAIL naming a missing preview modal requirement.

- [ ] **Step 3: Implement modal**

Add imports:

```tsx
import { X } from "lucide-react";
import { useState } from "react";
import type { Camera as CameraModel } from "../app/api";
import { useMseStream } from "../components/live/useMseStream";
```

In `ControlRoomDashboard`, add state:

```tsx
const [previewCamera, setPreviewCamera] = useState<CameraModel | null>(null);
```

Change the `보기` button:

```tsx
<button className="new-ghost" type="button" onClick={() => setPreviewCamera(camera)}>보기</button>
```

Render the modal at the end of the dashboard:

```tsx
{previewCamera && (
  <CameraPreviewModal camera={previewCamera} onClose={() => setPreviewCamera(null)} />
)}
```

Add the modal component:

```tsx
function CameraPreviewModal({ camera, onClose }: { camera: CameraModel; onClose: () => void }) {
  const videoRef = useMseStream(camera.streamName);

  return (
    <div className="new-preview-backdrop" role="dialog" aria-modal="true" aria-label={`${camera.name} 미리보기`}>
      <div className="new-preview-modal">
        <div className="new-preview-head">
          <div>
            <div className="text-sm font-semibold text-slate-100">{camera.name}</div>
            <div className="text-xs text-slate-500">{camera.state}</div>
          </div>
          <button className="new-icon-button" type="button" onClick={onClose} aria-label="미리보기 닫기">
            <X size={16} />
          </button>
        </div>
        <video ref={videoRef} className="new-preview-video" muted playsInline autoPlay />
      </div>
    </div>
  );
}
```

Add CSS:

```css
.new-preview-backdrop { position: fixed; inset: 0; z-index: 60; display: grid; place-items: center; padding: 20px; background: rgb(0 0 0 / 72%); }
.new-preview-modal { width: min(920px, 100%); border: 1px solid var(--new-border); border-radius: 8px; background: #050a0e; overflow: hidden; box-shadow: 0 18px 60px rgb(0 0 0 / 45%); }
.new-preview-head { min-height: 54px; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 12px; border-bottom: 1px solid var(--new-border); background: color-mix(in oklch, var(--new-surface), black 18%); }
.new-preview-video { width: 100%; aspect-ratio: 16 / 9; background: #000; object-fit: contain; }
```

- [ ] **Step 4: Run tests and build**

Run:

```bash
go test ./cmd/camstationd -run TestConsolePagesKeepSeparateRoles -count=1
cd web && npm run build
```

Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/camstationd/main_test.go web/src/pages/ControlRoomPage.tsx web/src/styles/index.css cmd/camstationd/web
git commit -m "Add control room camera preview modal"
```

---

### Task 5: Final Verification And Runtime Refresh

**Files:**
- Modify: `cmd/camstationd/web/*`
- No source code changes unless verification finds a defect.

**Interfaces:**
- Consumes: Completed Tasks 1-4.
- Produces: Running server with `/` dashboard and `/live` live workspace.

- [ ] **Step 1: Run full verification**

Run:

```bash
cd /root/camstation/web && npm run build
cd /root/camstation && go test ./...
make build
```

Expected: all commands exit 0.

- [ ] **Step 2: Restart server**

Run:

```bash
cd /root/camstation
old=$(pgrep -x camstationd || true)
if [ -n "$old" ]; then kill $old; sleep 1; fi
pkill -x go2rtc || true
sleep 1
setsid -f ./camstationd -addr 0.0.0.0:18080 -db ./data/camstation.db -recording-enabled -segment-minutes 5 -max-storage-gb 0.30 > ./data/runtime-logs/camstationd.out 2>&1 < /dev/null
sleep 2
pgrep -a camstationd
```

Expected: one `camstationd` process is listed.

- [ ] **Step 3: Smoke test endpoints**

Run:

```bash
curl -sS --max-time 5 http://10.0.0.29:18080/ | rg 'assets/'
curl -sS --max-time 5 http://10.0.0.29:18080/api/health
curl -sS --max-time 5 http://10.0.0.29:18080/live | rg 'assets/'
```

Expected: HTML asset lines for `/` and `/live`, health JSON with `"ok":true`.

- [ ] **Step 4: Commit final build output if not already committed**

```bash
git add cmd/camstationd/web
git commit -m "Refresh embedded control room build"
```

Skip commit if `git diff --cached --quiet` shows no staged changes.
