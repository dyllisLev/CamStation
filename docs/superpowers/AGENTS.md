# SPECS AND PLANS GUIDE

## OVERVIEW
`docs/superpowers/` is the active design-and-plan workspace for larger CamStation changes.

## STRUCTURE
```text
docs/superpowers/
|-- specs/   # product behavior, constraints, UX/API decisions
`-- plans/   # implementation steps, file ownership, verification criteria
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Product intent for a feature | `specs/` | Read before changing broad behavior. |
| Implementation sequence | `plans/` | Follow file-level tasks and verification notes. |
| Current shipped state | `../07-implementation-status.md` | Status doc wins over stale draft assumptions. |

## CONVENTIONS
- Specs describe behavior and constraints; plans describe executable steps.
- Update or add these docs for larger features, architecture decisions, or user corrections likely to recur.
- Keep documents tied to concrete files, API surfaces, and runtime verification.
- Distinguish source files from generated embedded assets and runtime `data/` output.
- If a plan was written before later implementation, reconcile it against `docs/07-implementation-status.md` before acting.

## ANTI-PATTERNS
- Do not treat old plans as proof that code is implemented.
- Do not write vague checklist items that cannot be verified through an API, UI, file, process, DB row, or log.
- Do not store secrets, camera URLs, runtime logs, or recording paths containing sensitive details in these docs.
