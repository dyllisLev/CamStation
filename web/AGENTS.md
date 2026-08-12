# FRONTEND GUIDE

## OVERVIEW
`web/` is the React/Vite console package. Source lives under `web/src`; `npm run build` emits the embedded daemon assets into `../cmd/camstationd/web`.

## STRUCTURE
```text
web/
|-- src/app/          # route tree, domain API modules, query hooks, i18n/base path
|-- src/layouts/      # console shell and /live bypass
|-- src/pages/        # control room, cameras, recordings, backup, incidents, logs, system, viewers
|-- src/components/   # shared UI and live workspace components
|-- src/styles/       # global dense monitoring console styles
`-- vite.config.ts    # dev proxy and embedded build output
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Client routes | `src/app/App.tsx` | `/` control room, `/live` workspace, console pages. |
| API composition | `src/app/api.ts`, `src/app/*Api.ts` | Keep response types aligned with Go public DTOs. |
| Query hooks | `src/app/*Queries.ts` | Mutations invalidate focused query keys. |
| Console shell | `src/layouts/ConsoleLayout.tsx` | Left sidebar and `/live` shell bypass. |
| Live grid | `src/components/live/LiveWorkspace.tsx` | Layouts, focus view, timeline, video zoom/pan. |
| Recordings UI | `src/pages/recordings/` | Storage, workers, segment playback, backup state. |
| Backup UI | `src/pages/backup/` | Target, schedule, prefix, job history. |
| Settings UI | `src/pages/settings/` | Recording/backup/alert settings and test alert. |
| Visual system | `src/styles/index.css`, `DESIGN.md` | Dark operational console; avoid generic dashboard drift. |

## CONVENTIONS
- Use domain API modules and query hooks; do not scatter raw `fetch` calls through pages.
- Use `withAppBase(...)` for app-relative URLs and safe helper functions for API-provided media URLs.
- Keep UI copy Korean-first, concise, and operational.
- Preserve `/live` as full-screen monitoring outside normal console chrome.
- Native browser video controls stay absent on live tiles; recordings may use controls.
- Keep `집중 보기` tile enlargement separate from mouse-wheel video zoom/pan.
- `npm run build` is required before daemon rebuild when UI source changes.
- TypeScript config is strict: unused locals/params and non-erasable syntax fail builds.

## ANTI-PATTERNS
- Do not turn the control room into the full live grid or a marketing-style dashboard.
- Do not hardcode direct camera URLs, raw go2rtc URLs, local runtime paths, or secrets in UI code.
- Do not hand-edit generated files under `../cmd/camstationd/web`.
- Do not add visible instructional text for normal controls when icons/labels already communicate the action.
- Do not let compact tables reflow unpredictably; fixed min widths/scroll are preferred for dense operations.

## VERIFY
```bash
cd web && npm run lint
cd web && npm run build
```
