import { useEffect, useRef, useState } from "react";
import { withAppBase } from "../../app/basePath";

const CODECS = ["avc1.640029", "avc1.64002A", "avc1.640033", "mp4a.40.2", "mp4a.40.5", "opus"];
const STALL_MS = 10_000;

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

export function useMseStream(streamName: string) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!streamName) return;
    const videoElement = videoRef.current;
    const SourceClass = mediaSourceClass();
    if (!videoElement || !SourceClass) return;
    const video: HTMLVideoElement = videoElement;
    const MediaSourceClass: typeof MediaSource = SourceClass;

    let destroyed = false;
    let generation = 0;
    let ws: WebSocket | null = null;
    let mediaSource: MediaSource | null = null;
    let objectUrl = "";
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let stallTimer: ReturnType<typeof setTimeout> | null = null;

    function clearStallTimer() {
      if (stallTimer) {
        clearTimeout(stallTimer);
        stallTimer = null;
      }
    }

    function teardown() {
      clearStallTimer();
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

    function scheduleReconnect() {
      setConnected(false);
      if (!destroyed) reconnectTimer = setTimeout(connect, 3000);
    }

    function connect() {
      if (destroyed) return;
      teardown();

      const currentGeneration = ++generation;
      const source = new MediaSourceClass();
      mediaSource = source;
      let sourceBuffer: SourceBuffer | null = null;
      const pending = new Uint8Array(2 * 1024 * 1024);
      let pendingLength = 0;

      objectUrl = URL.createObjectURL(source);
      video.src = objectUrl;

      const flush = () => {
        if (!sourceBuffer || sourceBuffer.updating || pendingLength === 0) return;
        const data = pending.slice(0, pendingLength).buffer;
        pendingLength = 0;
        try {
          sourceBuffer.appendBuffer(data);
        } catch {
          scheduleReconnect();
        }
      };

      source.addEventListener(
        "sourceopen",
        () => {
          if (destroyed || currentGeneration !== generation) return;
          const protocol = location.protocol === "https:" ? "wss" : "ws";
          ws = new WebSocket(
            `${protocol}://${location.host}${withAppBase(`/player/api/ws?src=${encodeURIComponent(streamName)}`)}`,
          );
          ws.binaryType = "arraybuffer";

          ws.onopen = () => {
            if (currentGeneration !== generation) return;
            ws?.send(JSON.stringify({ type: "mse", value: supportedCodecs(MediaSourceClass) }));
          };

          ws.onmessage = (event) => {
            if (currentGeneration !== generation) return;

            if (typeof event.data === "string") {
              const message = JSON.parse(event.data) as { type: string; value: string };
              if (message.type !== "mse") return;
              try {
                sourceBuffer = source.addSourceBuffer(message.value);
                sourceBuffer.mode = "segments";
                sourceBuffer.addEventListener("updateend", () => {
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
                });
              } catch {
                scheduleReconnect();
              }
              return;
            }

            clearStallTimer();
            stallTimer = setTimeout(() => ws?.close(), STALL_MS);

            const chunk = new Uint8Array(event.data as ArrayBuffer);
            if (sourceBuffer && !sourceBuffer.updating && pendingLength === 0) {
              try {
                sourceBuffer.appendBuffer(event.data as ArrayBuffer);
              } catch {
                scheduleReconnect();
              }
            } else if (pendingLength + chunk.byteLength <= pending.byteLength) {
              pending.set(chunk, pendingLength);
              pendingLength += chunk.byteLength;
            }
            setConnected(true);
            video.play().catch(() => {});
          };

          ws.onclose = scheduleReconnect;
          ws.onerror = () => ws?.close();
        },
        { once: true },
      );

      video.play().catch(() => {});
    }

    connect();

    return () => {
      destroyed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      teardown();
      setConnected(false);
    };
  }, [streamName]);

  return { videoRef, connected };
}
