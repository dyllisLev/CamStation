# Live PTZ Button Feedback Design

**Date:** 2026-07-11 KST
**Status:** Approved for implementation planning

## Goal

Make every PTZ action read and behave like a finished operational control. Feedback stays on the control itself; the panel does not add instructional or transient status text to explain a button press.

## Scope

The change is limited to the existing `/live` PTZ side panel:

- direction and zoom press-and-hold controls
- center and emergency stop controls
- home-position controls
- preset save, move, and delete controls

Camera APIs, command timing, confirmation rules, capability checks, and the disabled audio, talk, and siren placeholders remain unchanged.

## Interaction States

Every enabled action button has a consistent visual state model:

1. **Rest:** visible surface, border, label, and icon establish a button affordance.
2. **Hover:** the surface and border brighten without changing layout.
3. **Focus:** a keyboard-visible focus ring uses the existing accent color.
4. **Press:** the button translates down by 2 pixels, scales to 95 percent, gains an inset shadow, and strengthens its surface color.
5. **Hold:** direction and zoom buttons keep the pressed treatment for the entire pointer or keyboard hold, not only during the browser's initial `:active` frame.
6. **Pending:** one-shot home and preset mutations keep the pressed surface, disable duplicate activation, and show a compact spinner inside the same button.
7. **Disabled:** unavailable actions remain visibly buttons but lose emphasis and use the existing not-allowed cursor.

Motion uses short CSS transitions and respects `prefers-reduced-motion`. No ripple library, animation dependency, toast, status banner, or separate execution message is added.

## Visual Treatment

The selected direction is a restrained tactical treatment suited to CamStation's dark operational console:

- 2-pixel physical depression plus 0.95 scale
- subtle cyan border and icon glow for normal actions
- inset shadow during press and hold
- immediate release back to the resting surface
- red emphasis reserved for emergency stop and destructive preset deletion

The circular direction pad keeps its current geometry. Each direction becomes a distinct hit surface within the pad so the press treatment is visible. Zoom controls use the same state language.

## Home Controls

`위치 / 홈` becomes a compact command group with two explicit rectangular buttons:

- `홈으로 이동`, with a home icon, is the primary command.
- `현재 위치를 홈으로 설정`, with a location/save icon, is the secondary persistent-state command.

Both controls use consistent height, padding, icon alignment, and press feedback. The existing confirmation before changing the home position remains mandatory.

## Preset Controls

The preset area remains a name input plus command list, but its actions receive explicit button hierarchy:

- `현재 위치 저장` is a full command button paired with the input.
- Each saved preset exposes a clear `이동` button.
- `삭제` is a compact destructive button and keeps its confirmation dialog.

Pending feedback stays inside the button that initiated the mutation. Other unrelated buttons remain usable unless the existing mutation gate already disables them.

## Accessibility and Safety

- Pointer and keyboard press-and-hold behavior remains equivalent.
- `aria-pressed` reflects active direction and zoom holds.
- Pending buttons use `aria-busy` and retain an accessible label.
- Focus rings are visible only for keyboard focus.
- Stop-on-release, pointer cancellation, lost capture, blur, visibility loss, Escape, panel close, and camera change behavior is unchanged.
- Reduced-motion users receive color, border, and inset-shadow feedback without scale or translation animation.

## Implementation Boundary

The existing `HoldButton` owns persistent press state because CSS `:active` alone cannot represent keyboard holds or asynchronous pointer cleanup. One small reusable pending-content pattern may be used inside `PtzControlPanel`; no new component library or global button system is introduced.

The CSS remains scoped to `.new-ptz-*` selectors to avoid changing buttons elsewhere in the console.

## Verification

Development follows targeted checks rather than repeated full-suite runs:

1. Add a focused component or interaction test that fails because hold buttons do not expose persistent pressed state.
2. Implement and run only that test until it passes.
3. Run frontend lint and build once after all PTZ UI changes are complete.
4. Build the daemon and restart it once.
5. In the real `/live` panel, verify one direction hold/release, one zoom hold/release, one home navigation, and one non-destructive preset action. Do not repeat persistent home-setting or destructive preset deletion as automated tests.
6. If a real test reveals an error, test only that error after its fix. Run one final integrated live-panel check after all errors are resolved.
