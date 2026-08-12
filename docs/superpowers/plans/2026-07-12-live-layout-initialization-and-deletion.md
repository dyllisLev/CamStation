# Live Layout Initialization and Deletion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/live` restore saved tile placement deterministically and allow operators to permanently delete saved layouts.

**Architecture:** Gate initial layout resolution on successful camera and layout queries, with pure state helpers keeping the asynchronous and post-delete rules independently testable. Add a conventional SQLite delete method and `DELETE /api/layouts/{id}`, then wire it through the existing camera API, TanStack Query, and the saved-layout panel.

**Tech Stack:** Go 1.24+, `net/http`, SQLite, React 19, TypeScript 6, TanStack Query 5, Node test runner, existing CSS.

## Global Constraints

- Do not add dependencies.
- Keep SQLite layout rows canonical; local storage contains only the last selected layout ID.
- Do not expose camera URLs, credentials, runtime paths, or generated configuration.
- Deleting a non-current layout must not change the current view.
- Deleting the current layout selects the newest remaining layout; deleting the final layout returns to the unsaved default.
- Do not overwrite unsaved operator edits during background query refreshes.
- Frontend source changes must be followed by `cd web && npm run build`, then `go build`.

---

## File Map

- `internal/store/layouts.go`: delete one canonical layout row and report a missing ID.
- `internal/store/layouts_test.go`: store-level create/delete/not-found coverage.
- `cmd/camstationd/routes_core.go`: expose the layout DELETE endpoint.
- `cmd/camstationd/routes_characterization_test.go`: characterize deletion over HTTP.
- `web/src/components/live/liveLayoutState.ts`: pure initialization and post-delete layout selection rules.
- `web/tests/liveLayoutState.test.ts`: asynchronous readiness and deletion-transition regression tests.
- `web/src/components/live/LiveWorkspace.tsx`: query gating, error state, delete interaction, and valid row markup.
- `web/src/app/cameraApi.ts`: typed DELETE request.
- `web/src/app/queries.ts`: deletion mutation and layout query invalidation.
- `web/src/styles/index.css`: saved-layout row/delete control styling.
- `docs/07-implementation-status.md`: record the shipped endpoint and live-page behavior.

---

### Task 1: SQLite Layout Deletion

**Files:**
- Modify: `internal/store/layouts.go`
- Create: `internal/store/layouts_test.go`

**Interfaces:**
- Consumes: existing `DB`, `LayoutProfile`, `CreateLayout`, `GetLayout`, and `ErrNotFound`.
- Produces: `func (d *DB) DeleteLayout(ctx context.Context, id string) error`.

- [ ] **Step 1: Write the failing store tests**

Create `internal/store/layouts_test.go`:

```go
package store

import (
	"errors"
	"testing"
)

func TestDeleteLayout(t *testing.T) {
	db := openMigratedStore(t)
	created, err := db.CreateLayout(t.Context(), LayoutProfile{
		ID:   "ops",
		Name: "Ops",
		Data: []LayoutItem{{I: "yard", X: 0, Y: 0, W: 24, H: 24}},
	})
	if err != nil {
		t.Fatalf("create layout: %v", err)
	}

	if err := db.DeleteLayout(t.Context(), created.ID); err != nil {
		t.Fatalf("delete layout: %v", err)
	}
	if _, err := db.GetLayout(t.Context(), created.ID); err == nil {
		t.Fatal("get deleted layout returned nil error")
	}
}

func TestDeleteLayoutReturnsNotFound(t *testing.T) {
	db := openMigratedStore(t)
	if err := db.DeleteLayout(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteLayout error = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `go test ./internal/store -run 'TestDeleteLayout' -count=1`

Expected: FAIL because `DeleteLayout` is undefined.

- [ ] **Step 3: Add the minimal store method**

Append to `internal/store/layouts.go`:

```go
func (d *DB) DeleteLayout(ctx context.Context, id string) error {
	result, err := d.db.ExecContext(ctx, `DELETE FROM layouts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete layout: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete layout rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("layout %q: %w", id, ErrNotFound)
	}
	return nil
}
```

- [ ] **Step 4: Run the store tests and verify GREEN**

Run: `go test ./internal/store -run 'TestDeleteLayout' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the store slice**

```bash
git add internal/store/layouts.go internal/store/layouts_test.go
git commit -m "feat: delete saved layouts from store"
```

---

### Task 2: Layout DELETE HTTP Endpoint

**Files:**
- Modify: `cmd/camstationd/routes_core.go`
- Modify: `cmd/camstationd/routes_characterization_test.go`

**Interfaces:**
- Consumes: `(*store.DB).DeleteLayout(context.Context, string) error` from Task 1.
- Produces: `DELETE /api/layouts/{id}` with `204` on success and `404` for an unknown ID.

- [ ] **Step 1: Extend route characterization with failing DELETE assertions**

Immediately after the existing layout creation assertions in `TestRoutesPreserveCoreAPISurface`, add:

```go
	layoutID, ok := layout["id"].(string)
	if !ok || layoutID == "" {
		t.Fatalf("created layout id = %#v, want non-empty string", layout["id"])
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/layouts/"+layoutID, nil)
	deleteRec := httptest.NewRecorder()
	server.handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/layouts/{id} status = %d, want %d; body=%s", deleteRec.Code, http.StatusNoContent, deleteRec.Body.String())
	}
	if layouts := getJSONArray(t, server.handler, "/api/layouts"); len(layouts) != 0 {
		t.Fatalf("layouts after delete = %#v, want empty list", layouts)
	}

	missingReq := httptest.NewRequest(http.MethodDelete, "/api/layouts/"+layoutID, nil)
	missingRec := httptest.NewRecorder()
	server.handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("second DELETE /api/layouts/{id} status = %d, want %d", missingRec.Code, http.StatusNotFound)
	}
```

- [ ] **Step 2: Run the route test and verify RED**

Run: `go test ./cmd/camstationd -run TestRoutesPreserveCoreAPISurface -count=1`

Expected: FAIL because the unmatched DELETE falls through to the SPA response instead of returning `204 No Content`.

- [ ] **Step 3: Register the DELETE handler**

Add `errors` to the imports in `cmd/camstationd/routes_core.go`, then add after the PUT handler:

```go
	mux.HandleFunc("DELETE /api/layouts/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := d.db.DeleteLayout(r.Context(), r.PathValue("id")); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, err)
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
```

- [ ] **Step 4: Run route and store tests and verify GREEN**

Run: `go test ./cmd/camstationd ./internal/store -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the HTTP slice**

```bash
git add cmd/camstationd/routes_core.go cmd/camstationd/routes_characterization_test.go
git commit -m "feat: expose saved layout deletion"
```

---

### Task 3: Deterministic Frontend Layout State

**Files:**
- Create: `web/src/components/live/liveLayoutState.ts`
- Create: `web/tests/liveLayoutState.test.ts`
- Modify: `web/src/components/live/LiveWorkspace.tsx`

**Interfaces:**
- Consumes: `Camera`, `LayoutItem`, and `LayoutProfile` from `web/src/app/api.ts`.
- Produces: `MonitorLayoutItem`, `resolveInitialLayout`, `resolveLayoutAfterDelete`, `mergeWithCameras`, `defaultLayout`, and `clampLayout`.

- [ ] **Step 1: Write failing readiness and deletion-state tests**

Create `web/tests/liveLayoutState.test.ts`:

```ts
import assert from "node:assert/strict";
import test from "node:test";
import { resolveInitialLayout, resolveLayoutAfterDelete } from "../src/components/live/liveLayoutState.ts";

const cameras = [{ streamName: "yard" }, { streamName: "gate" }];
const saved = {
  id: "saved",
  name: "저장 배치",
  data: [
    { i: "yard", x: 12, y: 4, w: 20, h: 16 },
    { i: "gate", x: 32, y: 4, w: 16, h: 16 },
  ],
  timeline_collapsed: true,
};

test("waits for layouts when cameras arrive first", () => {
  assert.equal(resolveInitialLayout(cameras, [], "saved", false), null);
  const result = resolveInitialLayout(cameras, [saved], "saved", true);
  assert.equal(result?.currentId, "saved");
  assert.deepEqual(result?.layout.map(({ i, x, y, w, h }) => ({ i, x, y, w, h })), saved.data);
  assert.equal(result?.timelineCollapsed, true);
});

test("falls back to the newest saved layout when the remembered id is absent", () => {
  const newest = { ...saved, id: "newest", name: "최신" };
  assert.equal(resolveInitialLayout(cameras, [newest, saved], "missing", true)?.currentId, "newest");
});

test("deleting a non-current layout leaves selection unchanged", () => {
  assert.equal(resolveLayoutAfterDelete("saved", "other", [saved], cameras), null);
});

test("deleting the current layout selects the newest remaining layout", () => {
  const next = { ...saved, id: "next", name: "다음" };
  assert.equal(resolveLayoutAfterDelete("saved", "saved", [saved, next], cameras)?.currentId, "next");
});

test("deleting the final layout returns an unsaved default", () => {
  const result = resolveLayoutAfterDelete("saved", "saved", [saved], cameras);
  assert.equal(result?.currentId, "");
  assert.equal(result?.timelineCollapsed, undefined);
  assert.deepEqual(result?.layout.map((item) => item.i), ["yard", "gate"]);
});
```

- [ ] **Step 2: Run the frontend test and verify RED**

Run: `cd web && node --experimental-strip-types --test tests/liveLayoutState.test.ts`

Expected: FAIL because `liveLayoutState.ts` does not exist.

- [ ] **Step 3: Add the pure state module**

Create `web/src/components/live/liveLayoutState.ts`:

```ts
import type { LayoutItem, LayoutProfile } from "../../app/api";

export const GRID_COLS = 48;
export const GRID_ROWS = 48;

type CameraKey = { readonly streamName: string };
type SavedLayout = Pick<LayoutProfile, "id" | "data" | "timeline_collapsed">;

export type VideoViewport = { scale: number; tx: number; ty: number };
export type MonitorLayoutItem = LayoutItem & { videoZoom?: VideoViewport };
export type ResolvedLayout = {
  currentId: string;
  layout: MonitorLayoutItem[];
  timelineCollapsed: boolean | undefined;
};

export function resolveInitialLayout(
  cameras: readonly CameraKey[],
  layouts: readonly SavedLayout[],
  savedId: string | null,
  layoutsReady: boolean,
): ResolvedLayout | null {
  if (!layoutsReady || cameras.length === 0) return null;
  const saved = layouts.find((item) => item.id === savedId) ?? layouts[0];
  return saved
    ? { currentId: saved.id, layout: mergeWithCameras(saved.data, cameras), timelineCollapsed: saved.timeline_collapsed }
    : { currentId: "", layout: defaultLayout(cameras), timelineCollapsed: undefined };
}

export function resolveLayoutAfterDelete(
  deletedId: string,
  currentId: string,
  layouts: readonly SavedLayout[],
  cameras: readonly CameraKey[],
): ResolvedLayout | null {
  if (deletedId !== currentId) return null;
  const remaining = layouts.filter((item) => item.id !== deletedId);
  return resolveInitialLayout(cameras, remaining, remaining[0]?.id ?? null, true);
}

export function defaultLayout(cameras: readonly CameraKey[]): MonitorLayoutItem[] {
  return cameras.map((camera, index) => ({
    i: camera.streamName,
    x: index === 0 ? 0 : 24 + ((index - 1) % 2) * 12,
    y: index === 0 ? 0 : Math.floor((index - 1) / 2) * 12,
    w: index === 0 ? 24 : 12,
    h: index === 0 ? 24 : 12,
    minW: 8,
    minH: 8,
  }));
}

export function mergeWithCameras(saved: readonly LayoutItem[], cameras: readonly CameraKey[]): MonitorLayoutItem[] {
  const savedMap = new Map(saved.map((item) => [item.i, item]));
  return cameras.map((camera, index) => savedMap.get(camera.streamName) ?? {
    ...defaultLayout([camera])[0],
    x: 24 + (index % 2) * 12,
    y: Math.floor(index / 2) * 12,
  });
}

export function clampLayout(layout: readonly MonitorLayoutItem[]): MonitorLayoutItem[] {
  return layout.map((item) => {
    const minW = item.minW ?? 1;
    const minH = item.minH ?? 1;
    const w = Math.min(Math.max(item.w, minW), GRID_COLS);
    const h = Math.min(Math.max(item.h, minH), GRID_ROWS);
    return {
      ...item,
      w,
      h,
      x: Math.min(Math.max(item.x, 0), GRID_COLS - w),
      y: Math.min(Math.max(item.y, 0), GRID_ROWS - h),
    };
  });
}
```

- [ ] **Step 4: Integrate one-time query gating into `LiveWorkspace`**

In `LiveWorkspace.tsx`:

1. Remove local `GRID_COLS`, `GRID_ROWS`, `VideoViewport`, `MonitorLayoutItem`, `defaultLayout`, `mergeWithCameras`, and `clampLayout` declarations.
2. Import them from `./liveLayoutState` together with `resolveInitialLayout` and `resolveLayoutAfterDelete`.
3. Add `const layoutInitializedRef = useRef(false);` beside the existing refs.
4. Replace the current initialization effect with:

```ts
  useEffect(() => {
    if (layoutInitializedRef.current || !cameras.isSuccess || !layoutsQuery.isSuccess) return;
    const resolved = resolveInitialLayout(rows, layouts, localStorage.getItem(LAST_LAYOUT_KEY), true);
    if (!resolved) return;
    layoutInitializedRef.current = true;
    setCurrentId(resolved.currentId);
    setLayout(resolved.layout);
    if (resolved.timelineCollapsed !== undefined) setTimelineCollapsed(resolved.timelineCollapsed);
  }, [cameras.isSuccess, layouts, layoutsQuery.isSuccess, rows]);
```

5. Replace the empty-grid copy with an error-aware message:

```tsx
<div className="new-empty">
  {cameras.isError || layoutsQuery.isError
    ? "라이브 배치 정보를 불러오지 못했습니다."
    : "카메라와 배치 정보를 불러오는 중입니다."}
</div>
```

- [ ] **Step 5: Run frontend tests and type-check**

Run: `cd web && npm test && npm run build`

Expected: all tests PASS and the TypeScript/Vite build succeeds.

- [ ] **Step 6: Commit deterministic initialization**

```bash
git add web/src/components/live/liveLayoutState.ts web/tests/liveLayoutState.test.ts web/src/components/live/LiveWorkspace.tsx
git commit -m "fix: restore saved live layout deterministically"
```

---

### Task 4: Frontend Delete Mutation and Saved-Layout UI

**Files:**
- Modify: `web/src/app/cameraApi.ts`
- Modify: `web/src/app/queries.ts`
- Modify: `web/src/components/live/LiveWorkspace.tsx`
- Modify: `web/src/styles/index.css`

**Interfaces:**
- Consumes: `DELETE /api/layouts/{id}` and `resolveLayoutAfterDelete` from prior tasks.
- Produces: `cameraApi.deleteLayout(id: string): Promise<void>` and `useDeleteLayout()`.

- [ ] **Step 1: Add the typed API and mutation**

In `web/src/app/cameraApi.ts`, add beside `updateLayout`:

```ts
  deleteLayout: (id: string) =>
    request<void>(`/api/layouts/${encodeURIComponent(id)}`, { method: "DELETE" }),
```

In `web/src/app/queries.ts`, add after `useUpdateLayout`:

```ts
export function useDeleteLayout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteLayout(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["layouts"] });
    },
  });
}
```

- [ ] **Step 2: Add deletion behavior to `LiveWorkspace`**

Import `Trash2` from `lucide-react` and `useDeleteLayout` from queries. Instantiate `const deleteLayout = useDeleteLayout();` beside the create/update mutations.

Add this handler after `loadLayout`:

```ts
  async function deleteSavedLayout(id: string, name: string) {
    if (!window.confirm(`‘${name}’ 배치를 삭제할까요?`)) return;
    try {
      await deleteLayout.mutateAsync(id);
      const resolved = resolveLayoutAfterDelete(id, currentId, layouts, rows);
      if (!resolved) return;
      setCurrentId(resolved.currentId);
      setLayout(resolved.layout);
      if (resolved.timelineCollapsed !== undefined) setTimelineCollapsed(resolved.timelineCollapsed);
      setDirty(false);
      if (resolved.currentId) localStorage.setItem(LAST_LAYOUT_KEY, resolved.currentId);
      else localStorage.removeItem(LAST_LAYOUT_KEY);
    } catch {
      window.alert("저장된 배치를 삭제하지 못했습니다.");
    }
  }
```

- [ ] **Step 3: Render separate load and delete buttons**

Replace the saved-layout mapping in the right panel with valid sibling buttons:

```tsx
{layouts.map((item) => (
  <div
    key={item.id}
    className={cn("new-layout-row", item.id === currentId && "new-active-row")}
  >
    <button type="button" className="new-layout-load" onClick={() => loadLayout(item.id)}>
      <span>{item.name}</span>
      <em>{item.id === currentId && dirty ? "편집됨" : formatShortTime(item.updated_at)}</em>
    </button>
    <button
      type="button"
      className="new-layout-delete"
      aria-label={`${item.name} 배치 삭제`}
      title="배치 삭제"
      disabled={deleteLayout.isPending}
      onClick={() => void deleteSavedLayout(item.id, item.name)}
    >
      <Trash2 size={14} />
    </button>
  </div>
))}
```

Replace the no-layout button with non-interactive markup:

```tsx
<div className="new-layout-row new-layout-row-empty new-active-row">
  <span>기본</span>
  <em>미저장</em>
</div>
```

- [ ] **Step 4: Style the two-action row without layout animation**

In `web/src/styles/index.css`, replace the existing block from `.new-layout-row, .new-camera-row` through `.new-camera-row { ... }` with:

```css
.new-layout-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: stretch;
  min-height: 40px;
  border: 1px solid #1d303d;
  border-radius: 7px;
  color: var(--new-muted);
  background: #08131b;
  overflow: hidden;
  transition: border-color .12s, background-color .12s, color .12s;
}

.new-layout-load {
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 0;
  color: inherit;
  background: transparent;
  text-align: left;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}

.new-layout-delete {
  width: 36px;
  display: grid;
  place-items: center;
  border: 0;
  border-left: 1px solid #1d303d;
  color: var(--new-muted);
  background: transparent;
  cursor: pointer;
}

.new-layout-delete:hover {
  color: var(--new-danger);
  background: color-mix(in oklch, var(--new-danger), transparent 90%);
}

.new-layout-delete:disabled {
  cursor: wait;
  opacity: .45;
}

.new-layout-row-empty {
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
}

.new-layout-row:hover,
.new-camera-row:hover {
  color: var(--new-fg);
  border-color: color-mix(in oklch, var(--new-accent), var(--new-border) 62%);
  background: color-mix(in oklch, var(--new-surface), black 4%);
}

.new-camera-row {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 10px;
  min-height: 40px;
  padding: 8px 10px;
  border: 1px solid #1d303d;
  border-radius: 7px;
  color: var(--new-muted);
  background: #08131b;
  text-align: left;
  font-size: 12px;
  cursor: pointer;
  transition: border-color .12s, background-color .12s, color .12s;
}
```

- [ ] **Step 5: Run frontend verification**

Run: `cd web && npm test && npm run lint && npm run build`

Expected: tests, lint, TypeScript, and Vite build all PASS.

- [ ] **Step 6: Commit frontend deletion**

```bash
git add web/src/app/cameraApi.ts web/src/app/queries.ts web/src/components/live/LiveWorkspace.tsx web/src/styles/index.css
git commit -m "feat: delete saved layouts from live workspace"
```

---

### Task 5: Documentation and Full Verification

**Files:**
- Modify: `docs/07-implementation-status.md`

**Interfaces:**
- Consumes: all completed behavior from Tasks 1–4.
- Produces: updated shipped-status record and final verification evidence.

- [ ] **Step 1: Update implementation status**

Under the live-page feature list, add:

```markdown
  - saved layout deletion with confirmation and deterministic fallback selection
  - saved layout initialization waits for both camera and layout queries, preventing camera-first navigation races
```

Under the layout persistence API list, add:

```markdown
  - `DELETE /api/layouts/{id}`
```

- [ ] **Step 2: Run complete verification**

Run:

```bash
cd web && npm test
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
```

Expected: every command exits `0`. The web build refreshes `cmd/camstationd/web` before the Go build embeds it.

- [ ] **Step 3: Perform bounded surface verification**

Use `scripts/camstationctl.sh restart` and `scripts/camstationctl.sh verify`, then verify in `/live`:

1. Navigate from `/` to `/live` without refreshing and confirm the remembered saved layout is used.
2. Delete a non-current saved layout and confirm the current tiles do not move.
3. Delete the current layout and confirm the newest remaining layout loads.
4. With a temporary final layout, delete it and confirm the selector returns to `기본` and local storage no longer contains `camstation-live-layout-id`.

Expected: daemon verification passes and all four visible behaviors match the design. Do not delete an operator-owned layout solely for testing; create temporary layouts for destructive checks.

- [ ] **Step 4: Commit documentation and generated web assets**

```bash
git add docs/07-implementation-status.md cmd/camstationd/web
git commit -m "docs: record live layout recovery and deletion"
```

---

## Final Review Checklist

- [ ] `git diff --check` reports no whitespace errors.
- [ ] `git status --short` contains no unintended runtime or user-owned files.
- [ ] The initial layout is never chosen from a still-loading layout query.
- [ ] Query refetches do not overwrite dirty layout edits.
- [ ] DELETE returns `204` once and `404` after the row is gone.
- [ ] Deleting the current/final/non-current layouts follows the approved transition rules.
- [ ] All automated and surface verification commands pass.
