# Live Playback Recovery Design

**Date:** 2026-07-12
**Status:** Approved

## Goal

Prevent live tiles from remaining indefinitely at `연결 중...`, recover automatically through a browser-safe fallback stream, and stop reporting configured-but-inactive go2rtc producers as healthy playback.

## Confirmed Failure Chain

The browser WebSocket and CamStation reverse proxy are working. A browser-equivalent MSE request produced these results:

- `4-live` returned its MSE codec response and two binary fMP4 messages in 227 ms.
- `3-live` and `fire-station-5-live` returned a go2rtc `type:error` control message and no binary media for 20 seconds.
- `3-focus` and `fire-station-5-focus` both returned playable media in about 6.8 seconds.
- A later request to both primary live outputs also recovered, proving that retrying can heal a transient source/relay failure.

The current browser hook ignores every text control message except `type:mse`. Its stall timer starts only after the first binary message. When go2rtc reports an error but leaves the WebSocket open, the hook neither reconnects nor exposes the error and the tile stays at `연결 중...` forever.

go2rtc's stream response also contains URL-only configured producer placeholders. CamStation currently counts `len(producers)`, so a placeholder is reported as one active producer even though it has no producer ID, format, protocol, or media. This creates false `running` status.

The original inputs are independently imperfect: the fire-station 5 live substream timed out under direct probing, and fire-station 3 intermittently returned metadata without delivering media packets. Browser recovery must therefore tolerate both immediate go2rtc errors and initial media silence.

## Considered Approaches

### 1. Browser recovery with focus fallback and truthful runtime status — selected

Cycle through an ordered list of browser-safe public outputs, react to go2rtc control errors, enforce an initial-media deadline, and retry after all candidates fail. Count only active go2rtc producers and derive the tile's playback indicator from the browser playback phase.

This changes no camera credentials or persisted source policy and uses the already configured focus output only when the preferred live output fails.

### 2. Server-only multi-producer fallback

Render multiple go2rtc producers under each live output and rely on go2rtc to select a fallback. This is less observable in the browser, adds generated-config complexity, and couples recovery to go2rtc-specific producer selection behavior.

### 3. Error display and retry without fallback

Close the failed WebSocket and reconnect to the same output. This improves honesty but repeatedly targets a known-dead substream and does not use the focus outputs already proven playable.

## Browser Playback State

`useMseStream` will accept either one stream name or an ordered list of stream names and return:

- `videoRef`
- `connected`
- `phase`: `connecting`, `fallback`, `playing`, `retrying`, or `unsupported`
- `activeStreamName`

The hook will keep its existing MediaSource cleanup and generation guards. Each connection attempt follows these rules:

1. Start a 15-second initial-media watchdog when the WebSocket opens and the MSE request is sent.
2. On a valid `type:mse` message, create the SourceBuffer as today.
3. On `type:error`, malformed control JSON, WebSocket error, SourceBuffer failure, or initial-media timeout, tear down that generation.
4. Try the next distinct candidate after 500 ms.
5. After all candidates fail, wait 3 seconds, return to the preferred candidate, and continue until the component unmounts.
6. After binary media begins, reset a 10-second media-stall watchdog on every chunk.
7. Mark playback connected only after binary media arrives; clear the retry count after media resumes.

Only safe Korean status copy is shown. Raw go2rtc errors, camera URLs, credentials, internal endpoints, and ffmpeg command text never enter the UI.

## Candidate Selection

Normal live tiles use distinct candidates in this order:

1. `liveStreamName`
2. `focusStreamName`
3. `streamName` only when neither role-specific output exists

Focused tiles reverse the first two choices. This preserves the focus output as the preferred enlarged view while allowing a live-output fallback if focus fails.

The hook remains compatible with single-stream callers such as dashboard previews; they retry the same stream after the full-cycle delay.

Once a fallback produces media, it remains active for that mounted tile. A page remount starts from the preferred output again. This avoids interrupting a recovered tile merely to test whether the cheaper primary output has returned.

## Tile Feedback

The camera tile will receive the hook's playback phase:

- `playing`: green tile indicator, no connection overlay
- `connecting`: `연결 중...`
- `fallback`: `대체 스트림 연결 중...`
- `retrying`: red indicator and `영상 입력 재연결 중...`
- `unsupported`: red indicator and `이 브라우저는 라이브 재생을 지원하지 않습니다.`

When a fallback is playing, a small `대체 스트림` badge remains visible. The persisted camera registration state is not used as proof that the browser is receiving media.

## Runtime Status Accuracy

`parseStreamRuntime` will count a producer only when the go2rtc object contains active runtime evidence: a nonzero producer ID, a non-empty format/protocol, or at least one media description. A URL-only placeholder counts as zero producers.

Public output runtime then reports:

- `running` when an active producer exists
- `starting` when consumers exist without an active producer
- `idle` otherwise

Private source aliases remain omitted from public APIs. Camera-wide persisted state is left unchanged because an on-demand public output can correctly be idle while its private source is healthy. Tile playback truth comes from the browser phase instead.

## Recovery Boundaries

This work cannot manufacture frames when every configured camera source is unavailable. If both live and focus candidates fail, the tile keeps retrying and clearly reports that it is reconnecting. It must not claim online playback.

No automatic camera-policy mutation is included. In particular, the system will not rewrite source keys, stream URLs, codecs, dimensions, or activation settings during playback recovery.

## Verification

- Pure frontend tests cover distinct candidate ordering and full-cycle retry selection.
- Browser state helper tests cover `mse`, `error`, malformed control messages, and retry timing.
- Go tests prove URL-only producers are inactive while real producer objects remain active.
- Existing stream-selection, frontend, route, and store tests remain green.
- `npm test`, `npm run lint`, `npm run build`, `go test ./...`, and `go build` pass.
- Runtime verification uses browser-equivalent WebSocket requests without changing camera settings:
  - a healthy live output receives binary media;
  - a failed preferred output advances to its focus fallback;
  - no tile remains indefinitely in the initial connecting phase;
  - `/api/streams/status` no longer reports URL-only placeholders as active producers.

## Out of Scope

- Replacing or repairing camera hardware, firmware, network links, or credentials
- Persistently changing camera output policies during playback
- Exposing raw go2rtc or ffmpeg errors in public APIs or the console
- Adding WebRTC as another browser transport
