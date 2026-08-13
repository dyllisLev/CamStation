import { withAppBase } from "../../app/basePath.ts";
import { parseMseControlMessage } from "./msePlayback.ts";
import type { PlaybackTransport } from "./playbackRecovery.ts";
import { inboundVideoReceipt, receiptAdvanced } from "./webrtcReceipt.ts";

const CODECS = ["avc1.640029", "avc1.64002A", "avc1.640033", "mp4a.40.2", "mp4a.40.5", "opus"];

export const PLAYBACK_PRIMARY_PROBE_TIMEOUT_MS = 8_000;
export const PLAYBACK_PRIMARY_PROBE_PROGRESS_SECONDS = 1;

export class PlaybackProgressVerifier {
  private baseline: number | null = null;

  observe(currentTime: number): boolean {
    if (!Number.isFinite(currentTime) || currentTime <= 0) return false;
    if (this.baseline === null || currentTime < this.baseline) {
      this.baseline = currentTime;
      return false;
    }
    return currentTime - this.baseline >= PLAYBACK_PRIMARY_PROBE_PROGRESS_SECONDS;
  }
}

export type PlaybackConnectionEvent =
  | "socket_open"
  | "signaling_answer"
  | "first_track"
  | "media_source_open"
  | "mse_ready"
  | "first_media";

export type PlaybackConnectionError =
  | "socket"
  | "signaling"
  | "media"
  | "unsupported";

type PlaybackConnectionOptions = {
  readonly video: HTMLVideoElement;
  readonly streamName: string;
  readonly transport: PlaybackTransport;
  readonly onEvent: (event: PlaybackConnectionEvent) => void;
  readonly onBinary: () => void;
  readonly onFailure: (error: PlaybackConnectionError) => void;
};

export function openPlaybackConnection(options: PlaybackConnectionOptions): () => void {
  const { video } = options;
  let closed = false;
  let ws: WebSocket | null = null;
  let peer: RTCPeerConnection | null = null;
  let mediaSource: MediaSource | null = null;
  let sourceBuffer: SourceBuffer | null = null;
  let objectURL = "";
  let statsTimer: ReturnType<typeof setInterval> | null = null;

  const fail = (error: PlaybackConnectionError) => {
    if (!closed) options.onFailure(error);
  };

  const close = () => {
    if (closed) return;
    closed = true;
    if (statsTimer) clearInterval(statsTimer);
    statsTimer = null;
    if (sourceBuffer) {
      sourceBuffer.removeEventListener("error", handleSourceBufferError);
      sourceBuffer.removeEventListener("updateend", handleUpdateEnd);
      sourceBuffer = null;
    }
    if (ws) {
      ws.onopen = null;
      ws.onmessage = null;
      ws.onclose = null;
      ws.onerror = null;
      ws.close();
      ws = null;
    }
    if (peer) {
      peer.ontrack = null;
      peer.onicecandidate = null;
      peer.onconnectionstatechange = null;
      peer.close();
      peer = null;
    }
    if (mediaSource) {
      try {
        if (mediaSource.readyState === "open") mediaSource.endOfStream();
      } catch {
        // Best-effort browser cleanup.
      }
      mediaSource = null;
    }
    if (objectURL) {
      URL.revokeObjectURL(objectURL);
      objectURL = "";
    }
    video.pause();
    video.removeAttribute("src");
    video.srcObject = null;
    video.load();
  };

  let pending: Uint8Array | null = null;
  let pendingLength = 0;

  function flush() {
    if (!sourceBuffer || sourceBuffer.updating || !pending || pendingLength === 0) return;
    const data = pending.slice(0, pendingLength).buffer;
    pendingLength = 0;
    try {
      sourceBuffer.appendBuffer(data);
    } catch {
      fail("media");
    }
  }

  function handleSourceBufferError() {
    fail("media");
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
        // The browser can reject trimming while the buffer changes state.
      }
    }
    if (end - video.currentTime > 5) video.currentTime = end - 0.5;
  }

  if (options.transport === "webrtc") {
    if (!("RTCPeerConnection" in window)) {
      queueMicrotask(() => fail("unsupported"));
      return close;
    }
    try {
      const connection = new RTCPeerConnection({ bundlePolicy: "max-bundle" });
      peer = connection;
      connection.addTransceiver("video", { direction: "recvonly" });
      connection.addTransceiver("audio", { direction: "recvonly" });
      connection.ontrack = (event) => {
        if (closed) return;
        options.onEvent("first_track");
        const media = video.srcObject instanceof MediaStream ? video.srcObject : new MediaStream();
        if (!media.getTracks().some((track) => track.id === event.track.id)) media.addTrack(event.track);
        video.srcObject = media;
        void video.play().catch(() => undefined);
      };
      connection.onicecandidate = (event) => {
        if (closed || !ws || ws.readyState !== WebSocket.OPEN) return;
        ws.send(JSON.stringify({ type: "webrtc/candidate", value: event.candidate?.candidate ?? "" }));
      };
      connection.onconnectionstatechange = () => {
        if (connection.connectionState === "failed" || connection.connectionState === "disconnected") {
          fail("media");
        }
      };

      let statsPending = false;
      let previousReceipt = { bytesReceived: 0, packetsReceived: 0 };
      statsTimer = setInterval(async () => {
        if (statsPending || closed) return;
        statsPending = true;
        try {
          const receipt = inboundVideoReceipt(await connection.getStats());
          if (closed) return;
          if (receiptAdvanced(previousReceipt, receipt)) {
            options.onEvent("first_media");
            options.onBinary();
          }
          previousReceipt = receipt;
        } catch {
          // The setup and media-progress deadlines remain authoritative.
        } finally {
          statsPending = false;
        }
      }, 1_000);

      ws = openPlayerSocket(options.streamName);
      ws.onopen = async () => {
        if (closed) return;
        options.onEvent("socket_open");
        try {
          const offer = await connection.createOffer();
          await connection.setLocalDescription(offer);
          if (closed || !ws || ws.readyState !== WebSocket.OPEN) return;
          ws.send(JSON.stringify({ type: "webrtc/offer", value: offer.sdp ?? "" }));
        } catch {
          fail("signaling");
        }
      };
      ws.onmessage = (event) => {
        if (closed || typeof event.data !== "string") return;
        let message: { type?: unknown; value?: unknown };
        try {
          message = JSON.parse(event.data) as { type?: unknown; value?: unknown };
        } catch {
          fail("signaling");
          return;
        }
        if (message.type === "webrtc/answer" && typeof message.value === "string") {
          options.onEvent("signaling_answer");
          void connection.setRemoteDescription({ type: "answer", sdp: message.value }).catch(() => fail("signaling"));
        } else if (message.type === "webrtc/candidate" && typeof message.value === "string") {
          void connection.addIceCandidate({ candidate: message.value, sdpMid: "0" }).catch(() => fail("signaling"));
        } else if (message.type === "error") {
          fail("signaling");
        }
      };
      ws.onclose = () => fail("socket");
      ws.onerror = () => fail("socket");
    } catch {
      queueMicrotask(() => fail("media"));
    }
    return close;
  }

  const MediaSourceClass = mediaSourceClass();
  if (!MediaSourceClass) {
    queueMicrotask(() => fail("unsupported"));
    return close;
  }
  try {
    const source = new MediaSourceClass();
    mediaSource = source;
    pending = new Uint8Array(2 * 1024 * 1024);
    objectURL = URL.createObjectURL(source);
    video.src = objectURL;
    source.addEventListener("error", () => fail("media"), { once: true });
    source.addEventListener("sourceopen", () => {
      if (closed) return;
      options.onEvent("media_source_open");
      ws = openPlayerSocket(options.streamName);
      ws.binaryType = "arraybuffer";
      ws.onopen = () => {
        if (closed) return;
        options.onEvent("socket_open");
        ws?.send(JSON.stringify({ type: "mse", value: supportedCodecs(MediaSourceClass) }));
      };
      ws.onmessage = (event) => {
        if (closed) return;
        if (typeof event.data === "string") {
          const message = parseMseControlMessage(event.data);
          if (message.type !== "mse") {
            fail("signaling");
            return;
          }
          try {
            sourceBuffer = source.addSourceBuffer(message.value);
            sourceBuffer.mode = "segments";
            sourceBuffer.addEventListener("error", handleSourceBufferError);
            sourceBuffer.addEventListener("updateend", handleUpdateEnd);
            options.onEvent("mse_ready");
          } catch {
            fail("media");
          }
          return;
        }

        options.onEvent("first_media");
        options.onBinary();
        const chunk = new Uint8Array(event.data as ArrayBuffer);
        if (sourceBuffer && !sourceBuffer.updating && pendingLength === 0) {
          try {
            sourceBuffer.appendBuffer(event.data as ArrayBuffer);
          } catch {
            fail("media");
          }
        } else if (pending && pendingLength + chunk.byteLength <= pending.byteLength) {
          pending.set(chunk, pendingLength);
          pendingLength += chunk.byteLength;
        } else {
          fail("media");
        }
        void video.play().catch(() => undefined);
      };
      ws.onclose = () => fail("socket");
      ws.onerror = () => fail("socket");
    }, { once: true });
    void video.play().catch(() => undefined);
  } catch {
    queueMicrotask(() => fail("media"));
  }
  return close;
}

export function probePlaybackProgress(
  streamName: string,
  transport: PlaybackTransport,
  signal: AbortSignal,
): Promise<boolean> {
  if (signal.aborted) return Promise.resolve(false);
  const video = document.createElement("video");
  video.autoplay = true;
  video.muted = true;
  video.playsInline = true;
  video.tabIndex = -1;
  video.setAttribute("aria-hidden", "true");
  video.style.cssText = "position:fixed;left:-2px;bottom:0;width:1px;height:1px;opacity:0;pointer-events:none";
  document.body.appendChild(video);

  return new Promise<boolean>((resolve) => {
    let settled = false;
    const progressVerifier = new PlaybackProgressVerifier();
    let closeConnection: () => void = () => undefined;
    const checkProgress = () => {
      if (progressVerifier.observe(video.currentTime)) settle(true);
    };
    const progressTimer = window.setInterval(checkProgress, 250);
    const deadlineTimer = window.setTimeout(() => settle(false), PLAYBACK_PRIMARY_PROBE_TIMEOUT_MS);
    const onAbort = () => settle(false);
    const settle = (recovered: boolean) => {
      if (settled) return;
      settled = true;
      signal.removeEventListener("abort", onAbort);
      window.clearInterval(progressTimer);
      window.clearTimeout(deadlineTimer);
      video.removeEventListener("timeupdate", checkProgress);
      closeConnection();
      video.remove();
      resolve(recovered);
    };

    signal.addEventListener("abort", onAbort, { once: true });
    video.addEventListener("timeupdate", checkProgress);
    const openedConnection = openPlaybackConnection({
      video,
      streamName,
      transport,
      onEvent: () => undefined,
      onBinary: () => undefined,
      onFailure: () => settle(false),
    });
    closeConnection = openedConnection;
    if (settled) openedConnection();
  });
}

function mediaSourceClass(): typeof MediaSource | null {
  if ("MediaSource" in window) return MediaSource;
  if ("ManagedMediaSource" in window) {
    return (window as unknown as { ManagedMediaSource: typeof MediaSource }).ManagedMediaSource;
  }
  return null;
}

function supportedCodecs(MediaSourceClass: typeof MediaSource) {
  return CODECS.filter((codec) => MediaSourceClass.isTypeSupported(`video/mp4; codecs="${codec}"`)).join(",");
}

function openPlayerSocket(streamName: string) {
  const protocol = location.protocol === "https:" ? "wss" : "ws";
  return new WebSocket(
    `${protocol}://${location.host}${withAppBase(`/player/api/ws?src=${encodeURIComponent(streamName)}`)}`,
  );
}
