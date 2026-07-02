# FRONTEND GUIDE

## OVERVIEW
`web/` is the React/Vite console package. Source lives under `web/src`; `vite build` emits the embedded daemon assets into `cmd/camstationd/web`.

## STRUCTURE
```text
web/
|-- src/app/          # routes, API client, query hooks, i18n, base path
|-- src/layouts/      # console shell and /live bypass
|-- src/pages/        # control room, live, recordings, cameras, status pages
|-- src/components/   # shared UI and live workspace components
|-- src/styles/       # global operational monitoring styles
`-- vite.config.ts    # dev proxy and embedded build output
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Route ownership | `src/app/App.tsx` | `/` control room, `/live` monitoring. |
| Fetch contracts | `src/app/api.ts` | Keep response types aligned with Go handlers. |
| Cache/invalidation | `src/app/queries.ts` | Mutations invalidate focused query keys. |
| Console shell | `src/layouts/ConsoleLayout.tsx` | Left sidebar and live-page bypass. |
| Live grid | `src/components/live/LiveWorkspace.tsx` | Layouts, focus view, timeline, video zoom. |
| MSE playback | `src/components/live/useMseStream.ts` | WebSocket player path and raw video sink. |
| Visual system | `src/styles/index.css` | Dark monitoring palette and `new-*` classes. |
| Language | `src/app/i18n.tsx` | Korean default, English secondary. |

## CONVENTIONS
- Use `api.ts` for endpoint calls and `queries.ts` for React Query hooks; do not scatter fetches through pages.
- Use `withAppBase(...)` when building app-relative URLs.
- Preserve `/live` as a full-screen monitoring workspace outside the normal console chrome.
- Native browser video controls should stay absent on live tiles.
- Keep `집중 보기` tile enlargement separate from mouse-wheel video zoom/pan.
- Persist live layout state through layout APIs; localStorage is only for selected layout and collapse preferences.
- Keep UI copy operational and concise; default strings should be Korean.

## ANTI-PATTERNS
- Do not turn the control room into the full live grid.
- Do not replace the dark dense monitoring style with a generic SaaS dashboard.
- Do not hardcode direct camera or raw go2rtc URLs in UI code.
- Do not hand-edit generated files under `../cmd/camstationd/web`.

## VERIFY
```bash
cd web && npm run lint
cd web && npm run build
```
