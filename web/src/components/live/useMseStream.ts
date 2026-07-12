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
