# Live Playback Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make live tiles leave indefinite `연결 중...` states, recover through a proven browser-safe fallback output, and report only active go2rtc producers as running.

**Architecture:** Pure TypeScript helpers define ordered playback candidates, go2rtc control-message parsing, and retry selection. `useMseStream` owns MediaSource/WebSocket cleanup and uses those helpers to cycle preferred and fallback public outputs, while `LiveWorkspace` renders browser-phase truth. Go runtime parsing independently filters URL-only producer placeholders.

**Tech Stack:** React 19, TypeScript 6, browser MediaSource/WebSocket APIs, Node test runner, Go 1.24+, go2rtc status JSON, existing CSS.

## Global Constraints

- Add no dependencies.
- Never expose camera URLs, credentials, raw go2rtc errors, internal endpoints, or ffmpeg commands.
- Do not mutate persisted camera source keys, output policies, codecs, dimensions, activation settings, or credentials during playback recovery.
- Normal tiles prefer `liveStreamName` and fall back to `focusStreamName`; focused tiles use the reverse order.
- Use `streamName` only when neither role-specific output exists.
- Initial media deadline is 15 seconds, fallback delay is 500 ms, full-cycle retry delay is 3 seconds, and media stall deadline is 10 seconds.
- Once fallback media plays, keep it for the mounted tile; remounting starts from the preferred output.
- Private source aliases remain absent from public APIs.
- Frontend source changes require `npm run build` before `go build`.

---

## File Map

- `web/src/components/live/streamSelection.ts`: ordered, distinct preferred/fallback stream candidates.
- `web/tests/streamSelection.test.ts`: candidate order and legacy fallback tests.
- `web/src/components/live/msePlayback.ts`: pure go2rtc control parsing and next-attempt timing.
- `web/tests/msePlayback.test.ts`: control-message and retry-cycle tests.
- `web/src/components/live/useMseStream.ts`: browser MediaSource/WebSocket recovery state machine.
- `web/src/components/live/LiveWorkspace.tsx`: tile phase feedback and fallback badge.
- `web/src/styles/index.css`: fallback/retry feedback styling.
- `internal/stream/go2rtc.go`: active producer recognition.
- `internal/stream/go2rtc_test.go`: URL-only placeholder regression coverage.
- `docs/07-implementation-status.md`: shipped recovery behavior.

---

### Task 1: Pure Playback Selection and Retry Rules

**Files:**
- Modify: `web/src/components/live/streamSelection.ts`
- Modify: `web/tests/streamSelection.test.ts`
- Create: `web/src/components/live/msePlayback.ts`
- Create: `web/tests/msePlayback.test.ts`

**Interfaces:**
- Produces: `playbackStreamCandidates(camera, focused): string[]`.
- Produces: `parseMseControlMessage(raw): MseControlMessage`.
- Produces: `nextPlaybackAttempt(currentIndex, candidateCount): { candidateIndex: number; delayMs: number }`.

- [ ] **Step 1: Write failing candidate and retry tests**

Extend `web/tests/streamSelection.test.ts` imports and add:

```ts
import { playbackStreamCandidates, playbackStreamName, shouldRenderLiveTile } from "../src/components/live/streamSelection.ts";

test("normal playback falls back from live to focus without duplicates", () => {
  assert.deepEqual(playbackStreamCandidates(dualStreamCamera), ["yard-live", "yard-focus"]);
  assert.deepEqual(
    playbackStreamCandidates({ ...dualStreamCamera, focusStreamName: "yard-live" }),
    ["yard-live"],
  );
});

test("focused playback falls back from focus to live", () => {
  assert.deepEqual(playbackStreamCandidates(dualStreamCamera, true), ["yard-focus", "yard-live"]);
});

test("stable stream is used only without role outputs", () => {
  assert.deepEqual(playbackStreamCandidates({ streamName: "legacy" }), ["legacy"]);
});
```

Create `web/tests/msePlayback.test.ts`:

```ts
import assert from "node:assert/strict";
import test from "node:test";
import { nextPlaybackAttempt, parseMseControlMessage } from "../src/components/live/msePlayback.ts";

test("parses mse and error control messages without exposing unknown payloads", () => {
  assert.deepEqual(parseMseControlMessage('{"type":"mse","value":"video/mp4"}'), {
    type: "mse",
    value: "video/mp4",
  });
  assert.deepEqual(parseMseControlMessage('{"type":"error","value":"secret upstream detail"}'), {
    type: "error",
    value: "",
  });
  assert.deepEqual(parseMseControlMessage("not-json"), { type: "invalid", value: "" });
});

test("advances quickly to fallback and waits before a new preferred cycle", () => {
  assert.deepEqual(nextPlaybackAttempt(0, 2), { candidateIndex: 1, delayMs: 500 });
  assert.deepEqual(nextPlaybackAttempt(1, 2), { candidateIndex: 0, delayMs: 3000 });
  assert.deepEqual(nextPlaybackAttempt(0, 1), { candidateIndex: 0, delayMs: 3000 });
});
```

- [ ] **Step 2: Run focused tests and verify RED**

Run:

```bash
cd web
node --experimental-strip-types --test tests/streamSelection.test.ts tests/msePlayback.test.ts
```

Expected: FAIL because `playbackStreamCandidates` and `msePlayback.ts` do not exist.

- [ ] **Step 3: Implement distinct candidate selection**

Replace the playback selection portion of `streamSelection.ts` with:

```ts
export function playbackStreamCandidates(camera: CameraPlaybackStreams, focused = false): string[] {
  const roleCandidates = focused
    ? [camera.focusStreamName, camera.liveStreamName]
    : [camera.liveStreamName, camera.focusStreamName];
  const distinct = roleCandidates.filter(
    (streamName, index): streamName is string => Boolean(streamName) && roleCandidates.indexOf(streamName) === index,
  );
  return distinct.length > 0 ? distinct : [camera.streamName];
}

export function playbackStreamName(camera: CameraPlaybackStreams, focused = false) {
  return playbackStreamCandidates(camera, focused)[0];
}
```

- [ ] **Step 4: Implement safe control parsing and retry selection**

Create `web/src/components/live/msePlayback.ts`:

```ts
export type MseControlMessage =
  | { readonly type: "mse"; readonly value: string }
  | { readonly type: "error" | "invalid"; readonly value: "" };

export function parseMseControlMessage(raw: string): MseControlMessage {
  try {
    const message: unknown = JSON.parse(raw);
    if (!message || typeof message !== "object" || !("type" in message)) return { type: "invalid", value: "" };
    if (message.type === "mse" && "value" in message && typeof message.value === "string") {
      return { type: "mse", value: message.value };
    }
    if (message.type === "error") return { type: "error", value: "" };
    return { type: "invalid", value: "" };
  } catch {
    return { type: "invalid", value: "" };
  }
}

export function nextPlaybackAttempt(currentIndex: number, candidateCount: number) {
  const count = Math.max(1, candidateCount);
  const candidateIndex = (currentIndex + 1) % count;
  return { candidateIndex, delayMs: candidateIndex === 0 ? 3000 : 500 };
}
```

- [ ] **Step 5: Run focused tests and verify GREEN**

Run:

```bash
cd web
node --experimental-strip-types --test tests/streamSelection.test.ts tests/msePlayback.test.ts
```

Expected: all candidate and retry tests PASS.

- [ ] **Step 6: Commit pure playback rules**

```bash
git add web/src/components/live/streamSelection.ts web/tests/streamSelection.test.ts web/src/components/live/msePlayback.ts web/tests/msePlayback.test.ts
git commit -m "test: define live playback recovery rules"
```

---

### Task 2: Recovering Browser MSE Hook

**Files:**
- Modify: `web/src/components/live/useMseStream.ts`

**Interfaces:**
- Consumes: `parseMseControlMessage` and `nextPlaybackAttempt` from Task 1.
- Produces: `MsePlaybackPhase` and `useMseStream(streamNames: string | readonly string[])` returning `{ videoRef, connected, phase, activeStreamName, usingFallback }`.

- [ ] **Step 1: Replace the hook with candidate-aware recovery**

Replace `web/src/components/live/useMseStream.ts` with:

```ts
import { useEffect, useRef, useState } from "react";
import { withAppBase } from "../../app/basePath";
import { nextPlaybackAttempt, parseMseControlMessage } from "./msePlayback";

const CODECS = ["avc1.640029", "avc1.64002A", "avc1.640033", "mp4a.40.2", "mp4a.40.5", "opus"];
const INITIAL_MEDIA_MS = 15_000;
const MEDIA_STALL_MS = 10_000;

export type MsePlaybackPhase = "connecting" | "fallback" | "playing" | "retrying" | "unsupported";

type PlaybackState = {
  readonly phase: MsePlaybackPhase;
  readonly activeStreamName: string;
  readonly usingFallback: boolean;
};

function mediaSourceClass(): typeof MediaSource | null {
  if ("ManagedMediaSource" in window) {
    return (window as unknown as { ManagedMediaSource: typeof MediaSource }).ManagedMediaSource;
  }
  if ("MediaSource" in window) return MediaSource;
  return null;
}

function supportedCodecs(MediaSourceClass: typeof MediaSource) {
  return CODECS.filter((codec) => MediaSourceClass.isTypeSupported(`video/mp4; codecs="${codec}"`)).join(",");
}

export function useMseStream(streamNames: string | readonly string[]) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const input = typeof streamNames === "string" ? [streamNames] : streamNames;
  const candidateKey = input.filter(Boolean).join("\u001f");
  const [playback, setPlayback] = useState<PlaybackState>({
    phase: "connecting",
    activeStreamName: input[0] ?? "",
    usingFallback: false,
  });

  useEffect(() => {
    const candidates = candidateKey ? candidateKey.split("\u001f") : [];
    const videoElement = videoRef.current;
    const SourceClass = mediaSourceClass();
    if (candidates.length === 0 || !videoElement) return;
    if (!SourceClass) {
      setPlayback({ phase: "unsupported", activeStreamName: candidates[0], usingFallback: false });
      return;
    }
    const video: HTMLVideoElement = videoElement;
    const MediaSourceClass: typeof MediaSource = SourceClass;

    let destroyed = false;
    let generation = 0;
    let ws: WebSocket | null = null;
    let mediaSource: MediaSource | null = null;
    let objectUrl = "";
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let initialTimer: ReturnType<typeof setTimeout> | null = null;
    let stallTimer: ReturnType<typeof setTimeout> | null = null;

    function clearMediaTimers() {
      if (initialTimer) clearTimeout(initialTimer);
      if (stallTimer) clearTimeout(stallTimer);
      initialTimer = null;
      stallTimer = null;
    }

    function teardownMedia() {
      clearMediaTimers();
      if (ws) {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onclose = null;
        ws.onerror = null;
        ws.close();
        ws = null;
      }
      if (mediaSource) {
        try {
          if (mediaSource.readyState === "open") mediaSource.endOfStream();
        } catch {
          // best-effort cleanup
        }
        mediaSource = null;
      }
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl);
        objectUrl = "";
      }
      video.removeAttribute("src");
      video.srcObject = null;
      video.load();
    }

    function connect(candidateIndex: number, retrying = false) {
      if (destroyed) return;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      reconnectTimer = null;
      teardownMedia();

      const currentGeneration = ++generation;
      const source = new MediaSourceClass();
      const activeStreamName = candidates[candidateIndex];
      mediaSource = source;
      let sourceBuffer: SourceBuffer | null = null;
      const pending = new Uint8Array(2 * 1024 * 1024);
      let pendingLength = 0;
      let attemptFailed = false;

      setPlayback({
        phase: retrying ? "retrying" : candidateIndex > 0 ? "fallback" : "connecting",
        activeStreamName,
        usingFallback: candidateIndex > 0,
      });

      function failAttempt() {
        if (attemptFailed || destroyed || currentGeneration !== generation) return;
        attemptFailed = true;
        generation++;
        teardownMedia();
        const next = nextPlaybackAttempt(candidateIndex, candidates.length);
        setPlayback({
          phase: next.candidateIndex === 0 ? "retrying" : "fallback",
          activeStreamName: candidates[next.candidateIndex],
          usingFallback: next.candidateIndex > 0,
        });
        reconnectTimer = setTimeout(
          () => connect(next.candidateIndex, next.candidateIndex === 0),
          next.delayMs,
        );
      }

      function resetStallTimer() {
        if (stallTimer) clearTimeout(stallTimer);
        stallTimer = setTimeout(failAttempt, MEDIA_STALL_MS);
      }

      function flush() {
        if (!sourceBuffer || sourceBuffer.updating || pendingLength === 0) return;
        const data = pending.slice(0, pendingLength).buffer;
        pendingLength = 0;
        try {
          sourceBuffer.appendBuffer(data);
        } catch {
          failAttempt();
        }
      }

      function handleUpdateEnd() {
        flush();
        if (!sourceBuffer || sourceBuffer.updating || sourceBuffer.buffered.length === 0) return;
        const end = sourceBuffer.buffered.end(sourceBuffer.buffered.length - 1);
        const start = sourceBuffer.buffered.start(0);
        if (end - start > 10) {
          try {
            sourceBuffer.remove(start, end - 5);
          } catch {
            // browser can reject trim while busy
          }
        }
        if (end - video.currentTime > 5) video.currentTime = end - 0.5;
      }

      objectUrl = URL.createObjectURL(source);
      video.src = objectUrl;
      source.addEventListener("error", failAttempt, { once: true });
      source.addEventListener(
        "sourceopen",
        () => {
          if (destroyed || currentGeneration !== generation) return;
          const protocol = location.protocol === "https:" ? "wss" : "ws";
          ws = new WebSocket(
            `${protocol}://${location.host}${withAppBase(`/player/api/ws?src=${encodeURIComponent(activeStreamName)}`)}`,
          );
          ws.binaryType = "arraybuffer";
          ws.onopen = () => {
            if (currentGeneration !== generation) return;
            ws?.send(JSON.stringify({ type: "mse", value: supportedCodecs(MediaSourceClass) }));
            initialTimer = setTimeout(failAttempt, INITIAL_MEDIA_MS);
          };
          ws.onmessage = (event) => {
            if (currentGeneration !== generation) return;
            if (typeof event.data === "string") {
              const message = parseMseControlMessage(event.data);
              if (message.type !== "mse") {
                failAttempt();
                return;
              }
              try {
                sourceBuffer = source.addSourceBuffer(message.value);
                sourceBuffer.mode = "segments";
                sourceBuffer.addEventListener("error", failAttempt);
                sourceBuffer.addEventListener("updateend", handleUpdateEnd);
              } catch {
                failAttempt();
              }
              return;
            }

            if (initialTimer) clearTimeout(initialTimer);
            initialTimer = null;
            resetStallTimer();
            const chunk = new Uint8Array(event.data as ArrayBuffer);
            if (sourceBuffer && !sourceBuffer.updating && pendingLength === 0) {
              try {
                sourceBuffer.appendBuffer(event.data as ArrayBuffer);
              } catch {
                failAttempt();
                return;
              }
            } else if (pendingLength + chunk.byteLength <= pending.byteLength) {
              pending.set(chunk, pendingLength);
              pendingLength += chunk.byteLength;
            } else {
              failAttempt();
              return;
            }
            setPlayback({ phase: "playing", activeStreamName, usingFallback: candidateIndex > 0 });
            video.play().catch(() => {});
          };
          ws.onclose = failAttempt;
          ws.onerror = failAttempt;
        },
        { once: true },
      );
      video.play().catch(() => {});
    }

    connect(0);
    return () => {
      destroyed = true;
      generation++;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      teardownMedia();
    };
  }, [candidateKey]);

  return {
    videoRef,
    connected: playback.phase === "playing",
    phase: playback.phase,
    activeStreamName: playback.activeStreamName,
    usingFallback: playback.usingFallback,
  };
}
```

- [ ] **Step 2: Run frontend tests and build**

Run:

```bash
cd web
npm test
npm run build
```

Expected: all tests PASS and TypeScript accepts existing single-stream callers.

- [ ] **Step 3: Commit the MSE recovery state machine**

```bash
git add web/src/components/live/useMseStream.ts
git commit -m "fix: recover stalled browser MSE playback"
```

---

### Task 3: Tile Fallback and Honest Browser Feedback

**Files:**
- Modify: `web/src/components/live/LiveWorkspace.tsx`
- Modify: `web/src/styles/index.css`

**Interfaces:**
- Consumes: `playbackStreamCandidates`, `MsePlaybackPhase`, and the extended `useMseStream` result.
- Produces: playback-driven tile indicator, Korean retry copy, and fallback badge.

- [ ] **Step 1: Wire ordered candidates and playback phase into camera tiles**

Replace the `playbackStreamName` import with `playbackStreamCandidates` and import `MsePlaybackPhase` from `useMseStream`.

Inside `CameraTile`, initialize:

```ts
const [playback, setPlayback] = useState<{ phase: MsePlaybackPhase; usingFallback: boolean }>({
  phase: "connecting",
  usingFallback: false,
});
const browserPlaying = playback.phase === "playing";
const playbackUnavailable = !suspended && !browserPlaying;
```

Render `LiveVideo` whenever the tile is not suspended, regardless of the persisted camera state:

```tsx
{!suspended ? (
  <LiveVideo
    streamNames={playbackStreamCandidates(camera, zoomed)}
    viewport={videoViewport}
    onViewportChange={onVideoViewportChange}
    onPlaybackChange={setPlayback}
  />
) : (
  <div className="new-offline-layer">집중보기 중 라이브 연결 중지</div>
)}
```

Use `playbackUnavailable` for `new-offline` and the header dot's `new-danger` class. Do not use `camera.state === "streaming"` as proof of browser playback.

- [ ] **Step 2: Render safe Korean phase feedback**

Change `LiveVideo` to accept `streamNames` and `onPlaybackChange`. After calling the hook, report state through an effect:

```ts
const { videoRef, connected, phase, usingFallback } = useMseStream(streamNames);

useEffect(() => {
  onPlaybackChange({ phase, usingFallback });
}, [onPlaybackChange, phase, usingFallback]);
```

Use this phase copy:

```ts
const statusCopy =
  phase === "fallback"
    ? "대체 스트림 연결 중..."
    : phase === "retrying"
      ? "영상 입력 재연결 중..."
      : phase === "unsupported"
        ? "이 브라우저는 라이브 재생을 지원하지 않습니다."
        : "연결 중...";
```

Render:

```tsx
{!connected && <div className="new-offline-layer">{statusCopy}</div>}
{connected && usingFallback && <div className="new-fallback-badge">대체 스트림</div>}
```

- [ ] **Step 3: Add fallback badge styling**

Add to `web/src/styles/index.css` after `.new-zoom-badge`:

```css
.new-fallback-badge {
  position: absolute;
  right: 8px;
  top: 36px;
  z-index: 3;
  padding: 3px 7px;
  border: 1px solid color-mix(in oklch, var(--new-accent), transparent 55%);
  border-radius: 4px;
  background: rgb(0 0 0 / 72%);
  color: var(--new-accent);
  font: 10px/1.4 var(--new-font-mono);
  pointer-events: none;
}
```

- [ ] **Step 4: Run frontend verification**

Run:

```bash
cd web
npm test
npm run lint
npm run build
```

Expected: all frontend tests, lint, and production build PASS.

- [ ] **Step 5: Commit tile feedback**

```bash
git add web/src/components/live/LiveWorkspace.tsx web/src/styles/index.css
git commit -m "feat: fall back live tiles to browser-safe streams"
```

---

### Task 4: Active go2rtc Producer Accounting

**Files:**
- Modify: `internal/stream/go2rtc.go`
- Modify: `internal/stream/go2rtc_test.go`

**Interfaces:**
- Produces: runtime `ProducerCount` that excludes URL-only configured placeholders.

- [ ] **Step 1: Write the failing placeholder test**

Add to `internal/stream/go2rtc_test.go`:

```go
func TestParseStreamRuntimeDoesNotCountConfiguredProducerPlaceholder(t *testing.T) {
	raw := `{
		"failed-live": {"producers": [{"url": "ffmpeg:private-source"}], "consumers": []},
		"active-live": {"producers": [{"id": 7, "format_name": "rtsp", "protocol": "rtsp+tcp", "medias": ["video, recvonly, H264"]}], "consumers": []}
	}`
	runtime, err := parseStreamRuntime(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got := runtime["failed-live"]; got.ProducerCount != 0 || got.State != "idle" {
		t.Fatalf("failed-live runtime = %#v, want zero producers and idle", got)
	}
	if got := runtime["active-live"]; got.ProducerCount != 1 || got.State != "running" {
		t.Fatalf("active-live runtime = %#v, want one producer and running", got)
	}
}
```

- [ ] **Step 2: Run the Go test and verify RED**

Run: `go test ./internal/stream -run TestParseStreamRuntimeDoesNotCountConfiguredProducerPlaceholder -count=1`

Expected: FAIL because `failed-live` reports one producer and `running`.

- [ ] **Step 3: Count active producer evidence only**

Add this package type above `parseStreamRuntime` and use it in the payload decoder:

```go
type go2RTCProducer struct {
	ID         int      `json:"id"`
	FormatName string   `json:"format_name"`
	Protocol   string   `json:"protocol"`
	Medias     []string `json:"medias"`
}

// Inside the anonymous stream payload struct:
Producers []go2RTCProducer `json:"producers"`
```

Add:

```go
func activeProducerCount(producers []go2RTCProducer) int {
	count := 0
	for _, producer := range producers {
		if producer.ID != 0 || producer.FormatName != "" || producer.Protocol != "" || len(producer.Medias) > 0 {
			count++
		}
	}
	return count
}
```

Replace `ProducerCount: len(stream.Producers)` with `ProducerCount: activeProducerCount(stream.Producers)`.

- [ ] **Step 4: Run stream tests and verify GREEN**

Run: `go test ./internal/stream -count=1`

Expected: all stream tests PASS.

- [ ] **Step 5: Commit truthful producer status**

```bash
git add internal/stream/go2rtc.go internal/stream/go2rtc_test.go
git commit -m "fix: report only active go2rtc producers"
```

---

### Task 5: Documentation, Full Build, and Runtime Verification

**Files:**
- Modify: `docs/07-implementation-status.md`
- Regenerate: `cmd/camstationd/web/**`

**Interfaces:**
- Consumes: all prior tasks.
- Produces: embedded console assets and runtime evidence.

- [ ] **Step 1: Document shipped recovery behavior**

Add under the live workspace status:

```markdown
  - browser MSE errors, initial-media silence, and media stalls trigger bounded reconnects
  - normal tiles fall back from the live output to the browser-safe focus output without mutating camera policy
  - tile status reflects browser media receipt and identifies fallback playback
  - go2rtc URL-only producer placeholders are reported idle instead of running
```

- [ ] **Step 2: Run complete automated verification**

Run:

```bash
cd web && npm test
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
```

Expected: every command exits `0`; the only allowed Vite output is the existing large-chunk warning.

- [ ] **Step 3: Restart and verify the managed daemon**

Run build and lifecycle commands separately so PID matching cannot include the build shell:

```bash
go build -o camstationd ./cmd/camstationd
scripts/camstationctl.sh restart
scripts/camstationctl.sh verify
```

Expected: health succeeds, camstationd owns go2rtc, and ports `18080`, `1984`, `8554`, and `8555` are listening as documented.

- [ ] **Step 4: Verify browser-equivalent recovery without camera mutation**

Use a Node WebSocket probe with the same MSE codec request as `useMseStream`:

1. Confirm a healthy live output receives an `mse` message and at least two binary messages.
2. Request `3-live` and `fire-station-5-live`; when either errors or times out, request its corresponding focus candidate.
3. Confirm `3-focus` and `fire-station-5-focus` receive binary media.
4. Query `/api/streams/status` and confirm streams with URL-only producer objects report `producerCount: 0` and do not report `running`.

Do not call camera policy mutation, stream deletion, camera deletion, or raw go2rtc configuration endpoints.

- [ ] **Step 5: Commit docs and embedded assets**

```bash
git add docs/07-implementation-status.md cmd/camstationd/web
git commit -m "docs: record live playback recovery"
```

---

## Final Review Checklist

- [ ] A go2rtc `type:error` cannot leave the hook indefinitely connecting.
- [ ] Initial media silence triggers fallback after 15 seconds.
- [ ] Media silence after playback triggers recovery after 10 seconds.
- [ ] Failed preferred live output advances to focus after 500 ms.
- [ ] Exhausted candidates restart from the preferred output after 3 seconds.
- [ ] Single-stream preview callers still compile and retry.
- [ ] Tile green state means the browser received media, not merely that the camera row says streaming.
- [ ] No raw upstream error or secret reaches UI/API output.
- [ ] URL-only producer placeholders count as zero.
- [ ] Frontend, Go, build, daemon, and real WebSocket verification all pass.
