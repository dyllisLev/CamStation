# CamStation Design System

## 1. Atmosphere & Identity

CamStation is a dense dark monitoring console. It should feel like a control room: quiet, compact, operational, and immediately scannable. The signature is cyan status light over layered near-black panels, with Korean-first labels and minimal decorative motion.

## 2. Color

### Palette

| Role | Token | Dark | Usage |
| --- | --- | --- | --- |
| Surface/app | `--new-bg` | `#03070c` | Console background |
| Surface/panel | `--new-panel` | `#0b1620` | Panels, cards, table shells |
| Surface/subtle | `--new-panel-soft` | `#071017` | Panel headers, form interiors |
| Text/primary | `--new-fg` | `#e5edf4` | Primary labels and values |
| Text/muted | `--new-muted` | `#8ba0b2` | Captions, metadata |
| Text/dim | `--new-dim` | `#637486` | Secondary metadata |
| Border/default | `--new-border` | `#1d303d` | Panels, inputs, table rows |
| Accent/primary | `--new-accent` | `#06b6d4` | Active navigation, primary actions |
| Status/success | `--new-success` | `#10b981` | Online/running states |
| Status/warning | `--new-warning` | `#f59e0b` | Degraded states |
| Status/error | `--new-danger` | `#ef4444` | Errors and destructive actions |

### Rules

- Use cyan only for active controls, scanning/preview actions, and live system emphasis.
- Destructive actions use the error token family and require confirmation.
- Do not expose raw camera credentials in UI text, logs, screenshots, or docs.

## 3. Typography

### Scale

| Level | Size | Weight | Line Height | Tracking | Usage |
| --- | --- | --- | --- | --- | --- |
| Page title | 18px | 650 | 1.3 | 0 | Console page heading |
| Section title | 14px | 650 | 1.35 | 0 | Panel headings |
| Body | 13px | 400 | 1.45 | 0 | Tables, form values |
| Caption | 11px | 650 | 1.35 | 0 | Field labels, table headers |
| Mono | 12px | 400 | 1.4 | 0 | Stream names, tokens, paths |

### Font Stack

- Primary: system UI stack.
- Mono: `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, Courier New, monospace`.

### Rules

- Korean text must not rely on negative letter spacing.
- Compact panels use section-title scale, never hero-scale type.

## 4. Spacing & Layout

### Base Unit

All spacing uses 4px multiples.

| Token | Value | Usage |
| --- | --- | --- |
| `--space-1` | 4px | Tight inline gaps |
| `--space-2` | 8px | Button icon gaps, compact stacks |
| `--space-3` | 12px | Panel headers, cards |
| `--space-4` | 16px | Page bands, larger gaps |

### Grid

- Console content is constrained by the shell and prefers dense grids.
- Camera administration uses two-column form/profile layout on desktop and one-column stacks on mobile.
- Tables collapse to labeled mobile cards below 720px.

## 5. Components

### Panel

- **Structure**: `section.new-panel` with `new-panel-header` and `new-panel-body`.
- **States**: default, empty, error content in body.
- **Accessibility**: headings identify each panel.
- **Motion**: none.

### Form Control

- **Structure**: label plus `.new-form-control`.
- **States**: default, focus, disabled.
- **Accessibility**: visible Korean label or explicit `aria-label`.
- **Motion**: focus ring only.

### Camera Table

- **Structure**: `.new-table-wrap > table.new-camera-table`.
- **States**: default, selected row, empty state.
- **Accessibility**: mobile cells use `data-label` for readable card labels.
- **Motion**: none.

### Profile Role Selector

- **Structure**: channel selector, recording profile selector, live profile selector, preview actions, stream candidate list.
- **States**: empty, scanning/loading, preview open, error.
- **Accessibility**: selectors are native controls; preview close has an accessible label.
- **Motion**: loader spin only.

### Confirmed Destructive Action

- **Structure**: first click arms confirmation; second click performs delete; cancel returns to default.
- **States**: default, confirming, pending, error.
- **Accessibility**: destructive buttons are real buttons with explicit text.
- **Motion**: none.

## 6. Motion & Interaction

| Type | Duration | Easing | Usage |
| --- | --- | --- | --- |
| Micro | 120ms | ease-out | Hover/focus color changes |
| Loading | continuous | linear | Spinner only while network mutation is pending |

### Rules

- Do not animate layout properties.
- Do not add decorative motion to monitoring surfaces.
- Every destructive action requires a confirmation state.

## 7. Depth & Surface

### Strategy

Mixed borders and tonal-shift. Panels and repeated rows use 1px borders and subtle near-black tonal differences. Avoid large shadows, gradient orbs, and decorative backgrounds in operational screens.
