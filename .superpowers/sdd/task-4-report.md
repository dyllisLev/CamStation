# Task 4 report: camera policy UI and focus consumer behavior

## Implemented

- Added exact frontend camera policy DTOs for stable source keys, the three output purposes, desired/applied/effective/verification/runtime data, revisions, and mutation payloads.
- Added camera policy API methods and React Query mutations for save/apply, camera probe, reapply with required `expectedDesiredRevision`, and bulk probe.
- Centralized the five required invalidation surfaces: cameras, legacy stream status, current stream status, recorder status, and events.
- Added shared `StreamOutputPolicyForm` and one defaults/validation model used by registration and editing.
- Registration sends three source-key policies with the camera transaction and preserves 202/pending warnings after save.
- Editing restores its full draft from `GET /api/cameras`, preserves dirty drafts across 10-second refetches, detects server revision changes, handles 409 with an explicit reload action, and shows 202/pending/apply-failed states.
- Policy cards expose source, video/audio/activation, size/FPS limits, advertised and detected input, desired and applied settings, effective output/transcoding, verification, and runtime counts.
- Missing live sources are not offered. A legacy desired live selection remains visible as disabled and validation prevents saving it until corrected.
- Replaced public-camera numeric-ID assumptions with stable `streamName` selection/keys.
- Live tiles use applied `liveStreamName`; focus uses applied `focusStreamName`. The focused camera's normal `LiveVideo` unmounts, triggering MSE WebSocket cleanup, while other tiles stay connected.
- Preserved the existing user changes that keep stored/default usernames blank and only send credentials when both username and password are present.

## Tests and verification

- TDD red run confirmed missing policy model and focus-suspension exports before implementation.
- `cd web && npm test`: 12/12 pass.
- `cd web && npm run lint`: pass.
- `cd web && npm run build`: pass; only the existing Vite large-chunk warning remains.
- `git diff --check`: pass.
- No Playwright binary is installed in this workspace, so no editor screenshot was captured in Task 4. Runtime/UI screenshot verification remains for the integrated real-camera phase.

## Notes

- The API contract now uses `maxFPS` exactly; no `maxFps` compatibility shim remains in the frontend.
- Generated embedded assets were rebuilt for verification but intentionally excluded from this source-only Task 4 commit because they were already dirty and are handled by the final integrated build.

## Review follow-up

- Unified camera create/update/delete response typing with the coordinator mutation contract (`saved`, `applied`, `camera`, optional `warning`) and removed the legacy required `ok/go2rtc` shape.
- A 409 now invalidates the cameras query; the reload action explicitly fetches `GET /api/cameras` and rebuilds the draft from that fresh response instead of copying a stale prop.
- Connection/profile rescans show and send the same policy draft. Existing manual modes and limits remain intact, while a live source deduplicated by the backend is visibly normalized to recording before validation/save.
- Live-input availability now compares public producer identity, not only differing profile tokens, matching backend same-URL deduplication.
- Follow-up verification: 15/15 tests, lint without warnings, and production build pass.
