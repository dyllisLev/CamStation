# Live PTZ Button Feedback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every PTZ action a consistent tactile button surface with visible press, hold, pending, disabled, and destructive states while converting the home and preset areas into finished command groups.

**Architecture:** Keep PTZ command behavior and APIs unchanged. Add a tiny framework-free press-source state helper used by the existing `HoldButton`, then scope all visual treatment to `PtzControlPanel` markup and `.new-ptz-*` CSS selectors. One-shot mutations expose their existing TanStack Query pending state inside the initiating button.

**Tech Stack:** React 19, TypeScript 6, TanStack Query, Lucide React, native CSS, Node test runner, Vite.

## Global Constraints

- Do not add a toast, status banner, instructional message, ripple library, GSAP dependency, or global button abstraction.
- Preserve current ONVIF commands, hold timing, stop safety, confirmation dialogs, capability gates, and disabled audio/talk/siren behavior.
- Use cyan for normal action feedback and red only for emergency stop and preset deletion.
- Keep all new styling under `.new-ptz-*` selectors.
- Respect `prefers-reduced-motion` while preserving non-motion feedback.
- Run targeted tests during fixes. Run frontend lint/build and the final real-device verification once after all UI errors are resolved.

---

## File Map

- Create `web/src/components/live/ptzPressState.ts`: pure press-source state transition used by hold controls.
- Create `web/tests/ptzPressState.test.ts`: framework-free Node regression test for simultaneous pointer and keyboard sources.
- Modify `web/src/components/live/PtzControlPanel.tsx`: persistent hold state, explicit command-button markup, icons, and in-button pending feedback.
- Modify `web/src/styles/index.css`: tactile PTZ button surfaces, press/hold/pending/destructive states, spinner, and reduced-motion override.
- Regenerate `cmd/camstationd/web/` through the existing frontend build; do not hand-edit generated assets.

### Task 1: Persistent Hold Button State

**Files:**
- Create: `web/tests/ptzPressState.test.ts`
- Create: `web/src/components/live/ptzPressState.ts`
- Modify: `web/src/components/live/PtzControlPanel.tsx`

**Interfaces:**
- Produces: `type PtzPressSource = "pointer" | "keyboard"`
- Produces: `updatePtzPressSources(current, source, active): ReadonlySet<PtzPressSource>`
- `HoldButton` consumes the helper and exposes `data-pressed` plus `aria-pressed`.

- [ ] **Step 1: Write the failing press-source test**

```ts
import assert from "node:assert/strict";
import test from "node:test";
import { updatePtzPressSources } from "../src/components/live/ptzPressState.ts";

test("keeps a hold pressed until every active input source releases", () => {
  let active = updatePtzPressSources(new Set(), "pointer", true);
  active = updatePtzPressSources(active, "keyboard", true);
  active = updatePtzPressSources(active, "pointer", false);
  assert.deepEqual([...active], ["keyboard"]);
  active = updatePtzPressSources(active, "keyboard", false);
  assert.equal(active.size, 0);
});
```

- [ ] **Step 2: Run the focused test and confirm RED**

Run:

```bash
cd web && node --experimental-strip-types --test tests/ptzPressState.test.ts
```

Expected: FAIL because `ptzPressState.ts` does not exist.

- [ ] **Step 3: Add the minimal state transition**

```ts
export type PtzPressSource = "pointer" | "keyboard";

export function updatePtzPressSources(
  current: ReadonlySet<PtzPressSource>,
  source: PtzPressSource,
  active: boolean,
): ReadonlySet<PtzPressSource> {
  const next = new Set(current);
  if (active) next.add(source);
  else next.delete(source);
  return next;
}
```

- [ ] **Step 4: Wire persistent pressed state into `HoldButton`**

Import `useState`, `PtzPressSource`, and `updatePtzPressSources`. Add a `pressedSources` ref, a `pressed` state, and this transition inside `HoldButton`:

```tsx
const pressedSources = useRef<ReadonlySet<PtzPressSource>>(new Set());
const [pressed, setPressed] = useState(false);
const setSourcePressed = (source: PtzPressSource, active: boolean) => {
  pressedSources.current = updatePtzPressSources(pressedSources.current, source, active);
  setPressed(pressedSources.current.size > 0);
};
```

Set `data-pressed={pressed}` and `aria-pressed={pressed}` on the button. Call `setSourcePressed("pointer", true)` after accepted pointer down and clear it on pointer up, cancel, leave, and lost capture. Call `setSourcePressed("keyboard", true)` on the accepted Space/Enter keydown and clear it on keyup and blur. Keep every existing `begin()` and `end()` call in the same event branches.

- [ ] **Step 5: Run the focused test and confirm GREEN**

Run:

```bash
cd web && node --experimental-strip-types --test tests/ptzPressState.test.ts
```

Expected: 1 test, 1 pass.

- [ ] **Step 6: Commit the independent interaction state**

```bash
git add web/tests/ptzPressState.test.ts web/src/components/live/ptzPressState.ts web/src/components/live/PtzControlPanel.tsx
git commit -m "feat: expose persistent PTZ button press state"
```

### Task 2: Finished Home and Preset Command Buttons

**Files:**
- Modify: `web/src/components/live/PtzControlPanel.tsx`
- Modify: `web/src/styles/index.css`

**Interfaces:**
- Consumes existing mutations: `gotoHome`, `setHome`, `createPreset`, `gotoPreset`, and `deletePreset`.
- Produces no API or shared component changes.

- [ ] **Step 1: Add in-button content and icons**

Import `House`, `LoaderCircle`, `MapPin`, `Navigation`, `Save`, and `Trash2` from `lucide-react`. Add a local presentation helper:

```tsx
function ActionContent({ pending, icon, children }: { pending: boolean; icon: ReactNode; children: ReactNode }) {
  return (
    <>
      <span className="new-ptz-action-icon" aria-hidden="true">
        {pending ? <LoaderCircle className="new-ptz-spinner" /> : icon}
      </span>
      <span>{children}</span>
    </>
  );
}
```

- [ ] **Step 2: Convert home actions to explicit command buttons**

Keep the existing click handlers and confirmation. Apply `new-ptz-action new-ptz-home-action`, `aria-busy`, and `data-pending` to both buttons. Their content is:

```tsx
<ActionContent pending={gotoHome.isPending} icon={<House />}>홈으로 이동</ActionContent>
<ActionContent pending={setHome.isPending} icon={<MapPin />}>현재 위치를 홈으로 설정</ActionContent>
```

Use a full-width section title above the two-button grid. Do not add status copy.

- [ ] **Step 3: Convert preset actions to the same hierarchy**

Apply `new-ptz-action new-ptz-save-action` to `현재 위치 저장`. For preset rows, calculate initiating-token pending state:

```tsx
const moving = gotoPreset.isPending && gotoPreset.variables?.token === preset.token;
const deleting = deletePreset.isPending && deletePreset.variables?.token === preset.token;
```

Render `Navigation` for `이동`, `Trash2` for `삭제`, and `LoaderCircle` only on the matching pending row. Apply `new-ptz-danger-action` only to delete. Preserve both existing confirmation dialogs and mutation handlers.

- [ ] **Step 4: Add the shared tactile CSS**

Add scoped rules equivalent to:

```css
.new-ptz-action,
.new-ptz-zoom-button,
.new-ptz-direction,
.new-ptz-stop-center {
  border: 1px solid color-mix(in oklch, var(--new-border), white 4%);
  background: linear-gradient(180deg, #10212c, #091720);
  color: var(--new-fg);
  box-shadow: 0 3px 0 #02080d, 0 7px 18px rgb(0 0 0 / .2);
  cursor: pointer;
  transition: transform 90ms ease, border-color 120ms ease, background 120ms ease, box-shadow 90ms ease, color 120ms ease;
}

.new-ptz-action:hover:not(:disabled),
.new-ptz-zoom-button:hover:not(:disabled),
.new-ptz-direction:hover:not(:disabled),
.new-ptz-stop-center:hover:not(:disabled) {
  border-color: color-mix(in oklch, var(--new-accent), var(--new-border) 38%);
  background: linear-gradient(180deg, #14303c, #0b202a);
}

.new-ptz-action:active:not(:disabled),
.new-ptz-action[data-pending="true"],
.new-ptz-zoom-button[data-pressed="true"],
.new-ptz-direction[data-pressed="true"],
.new-ptz-stop-center:active:not(:disabled) {
  transform: translateY(2px) scale(.95);
  border-color: var(--new-accent);
  background: color-mix(in oklch, var(--new-accent), #071017 82%);
  box-shadow: inset 0 2px 8px rgb(0 0 0 / .55), 0 0 18px color-mix(in oklch, var(--new-accent), transparent 72%);
  color: color-mix(in oklch, var(--new-accent), white 28%);
}
```

Also add visible `:focus-visible`, disabled opacity/cursor, a 14px action-icon slot, `@keyframes new-ptz-spin`, red overrides for `.new-ptz-danger-action` and `.new-ptz-emergency`, and a `prefers-reduced-motion: reduce` block that removes transform/animation while retaining border, surface, and inset-shadow feedback.

- [ ] **Step 5: Check the changed UI surface once**

Run:

```bash
cd web && npm run lint
cd web && npm run build
```

Expected: both commands exit 0. Inspect the generated asset names but do not hand-edit them.

- [ ] **Step 6: Commit the visual hierarchy**

```bash
git add web/src/components/live/PtzControlPanel.tsx web/src/styles/index.css cmd/camstationd/web/index.html cmd/camstationd/web/assets
git commit -m "feat: finish PTZ control button interactions"
```

### Task 3: Deploy and Run One Final Real-Device Check

**Files:**
- Generated by build: `cmd/camstationd/web/`
- No source edits unless a specific failure is found.

**Interfaces:**
- Consumes the finished embedded frontend and existing `camstationctl.sh` lifecycle.
- Produces a deployed `/live` panel on port `18080`.

- [ ] **Step 1: Build the daemon once**

Run:

```bash
go build -o camstationd ./cmd/camstationd
```

Expected: exit 0.

- [ ] **Step 2: Restart through the managed lifecycle once**

Run:

```bash
scripts/camstationctl.sh restart
```

Expected: health reports `ok: true`, with one managed daemon and one managed go2rtc process.

- [ ] **Step 3: Perform the real-camera interaction check**

Open `http://10.0.0.29:18080/live`, select `염소장`, and open `PTZ 제어`.

Verify exactly once per action:

- hold and release one direction; the button remains depressed during the hold and the camera stops on release
- hold and release zoom; the button remains depressed during the hold and zoom stops on release
- press `홈으로 이동`; the button depresses immediately, shows its in-button spinner while pending, and returns to rest
- inspect home and preset command groups for explicit button boundaries at the deployed panel width
- if a preset already exists, invoke `이동` once and confirm only that row shows pending feedback; if none exists, do not create persistent device state solely for testing

- [ ] **Step 4: Handle failures narrowly**

If an action fails, change only the responsible PTZ component/helper/CSS rule and rerun only its focused test plus that real action. Do not repeat lint, full build, restart, or the other camera actions while any specific failure remains.

- [ ] **Step 5: Run the final integrated operational verification**

After every specific failure is resolved, run once:

```bash
scripts/camstationctl.sh verify
```

Expected: daemon health is OK, go2rtc is managed, port `18080` is listening, and `/live` returns HTTP 200. Confirm the live video still renders and PTZ opens for `염소장`.
