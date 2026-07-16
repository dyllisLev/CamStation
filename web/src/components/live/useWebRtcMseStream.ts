import { useEffect, useRef, useState } from "react";
import { withAppBase } from "../../app/basePath";
import { parseMseControlMessage } from "./msePlayback";
import {
  PLAYBACK_COOLDOWN_MS,
  PLAYBACK_SETUP_MS,
  PLAYBACK_STALL_MS,
  PlaybackRecovery,
  type PlaybackRecoveryStep,
  type PlaybackTransport,
} from "./playbackRecovery";
import { inboundVideoReceipt, receiptAdvanced } from "./webrtcReceipt";

const CODECS = ["avc1.640029", "avc1.64002A", "avc1.640033", "mp4a.40.2", "mp4a.40.5", "opus"];

export type PlaybackPhase =
  | "connecting"
  | "retrying"
  | "fallback"
  | "recovering"
  | "playing"
  | "stalled"
  | "cooldown"
  | "unsupported";

export type PlaybackErrorCategory =
  | "none"
  | "setup_timeout"
  | "media_stall"
  | "socket"
  | "signaling"
  | "media"
  | "unsupported"
  | "episode_exhausted";

type PlaybackState = {
  readonly transport: PlaybackTransport;
  readonly phase: PlaybackPhase;
  readonly activeStreamName: string;
  readonly usingFallback: boolean;
  readonly lastBinaryAt: number | null;
  readonly lastProgressAt: number | null;
  readonly readyState: number;
  readonly stalledForMs: number;
  readonly attempt: number;
  readonly reconnectCount: number;
  readonly fallbackCount: number;
  readonly resubscribeCount: number;
  readonly errorCategory: PlaybackErrorCategory;
};

type AttemptOptions = {
  readonly transport: PlaybackTransport;
  readonly streamName: string;
  readonly attempt: number;
  readonly phase: PlaybackPhase;
};

export function useWebRtcMseStream(
  streamNames: string | readonly string[],
  resubscribeGeneration = 0,
) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const input = typeof streamNames === "string" ? [streamNames] : streamNames;
  const candidateKey = input.filter(Boolean).join("\u001f");
  const [playback, setPlayback] = useState<PlaybackState>(() => initialPlayback(input[0] ?? ""));

  useEffect(() => {
    const candidates = candidateKey ? candidateKey.split("\u001f") : [];
    const videoElement = videoRef.current;
    if (!videoElement || candidates.length === 0) return;
    const video: HTMLVideoElement = videoElement;

    const recovery = new PlaybackRecovery(candidates, Date.now());
    let destroyed = false;
    let generation = 0;
    let ws: WebSocket | null = null;
    let peer: RTCPeerConnection | null = null;
    let mediaSource: MediaSource | null = null;
    let objectURL = "";
    let setupTimer: ReturnType<typeof setTimeout> | null = null;
    let stallTimer: ReturnType<typeof setTimeout> | null = null;
    let cooldownTimer: ReturnType<typeof setTimeout> | null = null;
    let statsTimer: ReturnType<typeof setInterval> | null = null;
    let terminalAttempt = false;
    let lowFrequencyProbeUsed = false;
    let lastVideoTime = -1;
    let lastBinaryStateAt = 0;
    let lastProgressStateAt = 0;
    let activeAttempt: AttemptOptions = {
      transport: "webrtc",
      streamName: candidates[0],
      attempt: 1,
      phase: "connecting",
    };
    const counts = { reconnect: 0, fallback: 0, resubscribe: 0 };

    function clearTimers() {
      if (setupTimer) clearTimeout(setupTimer);
      if (stallTimer) clearTimeout(stallTimer);
      if (cooldownTimer) clearTimeout(cooldownTimer);
      if (statsTimer) clearInterval(statsTimer);
      setupTimer = null;
      stallTimer = null;
      cooldownTimer = null;
      statsTimer = null;
    }

    function teardownAttempt() {
      clearTimers();
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
          // best-effort browser cleanup
        }
        mediaSource = null;
      }
      if (objectURL) {
        URL.revokeObjectURL(objectURL);
        objectURL = "";
      }
      video.removeAttribute("src");
      video.srcObject = null;
      video.load();
      lastVideoTime = video.currentTime;
    }

    function publishAttempt(options: AttemptOptions, errorCategory: PlaybackErrorCategory) {
      lastBinaryStateAt = 0;
      lastProgressStateAt = 0;
      setPlayback((current) => ({
        ...current,
        transport: options.transport,
        phase: options.phase,
        activeStreamName: options.streamName,
        usingFallback: options.streamName !== candidates[0],
        lastBinaryAt: null,
        lastProgressAt: null,
        readyState: video.readyState,
        stalledForMs: recovery.stalledForMs(Date.now()),
        attempt: options.attempt,
        reconnectCount: counts.reconnect,
        fallbackCount: counts.fallback,
        resubscribeCount: counts.resubscribe,
        errorCategory,
      }));
    }

    function enterCooldown(until: number, scheduleProbe = true) {
      generation++;
      teardownAttempt();
      setPlayback((current) => ({
        ...current,
        phase: "cooldown",
        readyState: video.readyState,
        stalledForMs: recovery.stalledForMs(Date.now()),
        errorCategory: "episode_exhausted",
      }));
      if (scheduleProbe && !lowFrequencyProbeUsed) {
        lowFrequencyProbeUsed = true;
        cooldownTimer = setTimeout(() => {
          cooldownTimer = null;
          beginAttempt({
            transport: "webrtc",
            streamName: candidates[0],
            attempt: activeAttempt.attempt + 1,
            phase: "retrying",
          }, "episode_exhausted", true);
        }, Math.max(0, until - Date.now()));
      }
    }

    function advance(step: PlaybackRecoveryStep, errorCategory: PlaybackErrorCategory) {
      if ("action" in step) {
        if (step.action === "cooldown") {
          enterCooldown(step.until);
          return;
        }
        counts.resubscribe++;
        beginAttempt({
          transport: "webrtc",
          streamName: candidates[0],
          attempt: step.attempt,
          phase: "recovering",
        }, errorCategory);
        return;
      }
      if (step.transport === "webrtc") counts.reconnect++;
      else counts.fallback++;
      beginAttempt({
        ...step,
        phase: step.transport === "webrtc" ? "retrying" : "fallback",
      }, errorCategory);
    }

    function failAttempt(token: number, errorCategory: PlaybackErrorCategory) {
      if (destroyed || token !== generation) return;
      const now = Date.now();
      recovery.recordFailure(now);
      generation++;
      teardownAttempt();
      if (terminalAttempt) {
        enterCooldown(now + PLAYBACK_COOLDOWN_MS, false);
        return;
      }
      advance(recovery.nextFailure(now), errorCategory);
    }

    function resetStallTimer(token: number) {
      if (stallTimer) clearTimeout(stallTimer);
      stallTimer = setTimeout(() => failAttempt(token, "media_stall"), PLAYBACK_STALL_MS);
    }

    function markProgress(token: number) {
      if (destroyed || token !== generation) return;
      const now = Date.now();
      if (setupTimer) clearTimeout(setupTimer);
      setupTimer = null;
      resetStallTimer(token);
      const budgetReset = recovery.recordProgress(now);
      if (budgetReset) {
        counts.reconnect = 0;
        counts.fallback = 0;
        counts.resubscribe = 0;
        terminalAttempt = false;
        lowFrequencyProbeUsed = false;
      }
      if (now - lastProgressStateAt < 1_000) return;
      lastProgressStateAt = now;
      setPlayback((current) => ({
        ...current,
        phase: "playing",
        lastProgressAt: now,
        readyState: video.readyState,
        stalledForMs: 0,
        attempt: budgetReset ? 1 : current.attempt,
        reconnectCount: counts.reconnect,
        fallbackCount: counts.fallback,
        resubscribeCount: counts.resubscribe,
        errorCategory: "none",
      }));
    }

    function handleTimeUpdate() {
      if (video.currentTime <= lastVideoTime + 0.001) return;
      lastVideoTime = video.currentTime;
      markProgress(generation);
    }

    function beginAttempt(
      options: AttemptOptions,
      previousError: PlaybackErrorCategory = "none",
      terminal = false,
    ) {
      if (destroyed) return;
      teardownAttempt();
      const now = Date.now();
      const remainingMs = recovery.remainingMs(now);
      if (!terminal && remainingMs === 0) {
        recovery.recordFailure(now);
        advance(recovery.nextFailure(now), "episode_exhausted");
        return;
      }
      const token = ++generation;
      activeAttempt = options;
      terminalAttempt = terminal;
      publishAttempt(options, previousError);
      setupTimer = setTimeout(
        () => failAttempt(token, "setup_timeout"),
        terminal ? PLAYBACK_SETUP_MS : recovery.boundedDelayMs(Date.now(), PLAYBACK_SETUP_MS),
      );
      if (options.transport === "webrtc") connectWebRTC(token, options.streamName);
      else connectMSE(token, options.streamName);
    }

    function connectWebRTC(token: number, streamName: string) {
      if (!("RTCPeerConnection" in window)) {
        queueMicrotask(() => failAttempt(token, "unsupported"));
        return;
      }
      const connection = new RTCPeerConnection({ bundlePolicy: "max-bundle" });
      peer = connection;
      connection.addTransceiver("video", { direction: "recvonly" });
      connection.addTransceiver("audio", { direction: "recvonly" });
      connection.ontrack = (event) => {
        if (destroyed || token !== generation) return;
        const media = video.srcObject instanceof MediaStream ? video.srcObject : new MediaStream();
        if (!media.getTracks().some((track) => track.id === event.track.id)) media.addTrack(event.track);
        video.srcObject = media;
        void video.play().catch(() => undefined);
      };
      connection.onicecandidate = (event) => {
        if (token !== generation || !ws || ws.readyState !== WebSocket.OPEN) return;
        ws.send(JSON.stringify({ type: "webrtc/candidate", value: event.candidate?.candidate ?? "" }));
      };
      connection.onconnectionstatechange = () => {
        if (connection.connectionState === "failed" || connection.connectionState === "disconnected") {
          failAttempt(token, "media");
        }
      };

      let statsPending = false;
      let previousReceipt = { bytesReceived: 0, packetsReceived: 0 };
      statsTimer = setInterval(async () => {
        if (statsPending || destroyed || token !== generation) return;
        statsPending = true;
        try {
          const receipt = inboundVideoReceipt(await connection.getStats());
          if (token !== generation) return;
          if (receiptAdvanced(previousReceipt, receipt)) {
            const now = Date.now();
            setPlayback((current) => ({ ...current, lastBinaryAt: now, readyState: video.readyState }));
          }
          previousReceipt = receipt;
        } catch {
          // Setup and media-progress deadlines remain authoritative.
        } finally {
          statsPending = false;
        }
      }, 1_000);

      ws = openPlayerSocket(streamName);
      ws.onopen = async () => {
        try {
          const offer = await connection.createOffer();
          await connection.setLocalDescription(offer);
          if (token !== generation || !ws || ws.readyState !== WebSocket.OPEN) return;
          ws.send(JSON.stringify({ type: "webrtc/offer", value: offer.sdp ?? "" }));
        } catch {
          failAttempt(token, "signaling");
        }
      };
      ws.onmessage = (event) => {
        if (token !== generation || typeof event.data !== "string") return;
        let message: { type?: unknown; value?: unknown };
        try {
          message = JSON.parse(event.data) as { type?: unknown; value?: unknown };
        } catch {
          failAttempt(token, "signaling");
          return;
        }
        if (message.type === "webrtc/answer" && typeof message.value === "string") {
          void connection.setRemoteDescription({ type: "answer", sdp: message.value }).catch(() => failAttempt(token, "signaling"));
        } else if (message.type === "webrtc/candidate" && typeof message.value === "string") {
          void connection.addIceCandidate({ candidate: message.value, sdpMid: "0" }).catch(() => failAttempt(token, "signaling"));
        } else if (message.type === "error") {
          failAttempt(token, "signaling");
        }
      };
      ws.onclose = () => failAttempt(token, "socket");
      ws.onerror = () => failAttempt(token, "socket");
    }

    function connectMSE(token: number, streamName: string) {
      const MediaSourceClass = mediaSourceClass();
      if (!MediaSourceClass) {
        queueMicrotask(() => failAttempt(token, "unsupported"));
        return;
      }
      const source = new MediaSourceClass();
      mediaSource = source;
      let sourceBuffer: SourceBuffer | null = null;
      const pending = new Uint8Array(2 * 1024 * 1024);
      let pendingLength = 0;

      function flush() {
        if (!sourceBuffer || sourceBuffer.updating || pendingLength === 0) return;
        const data = pending.slice(0, pendingLength).buffer;
        pendingLength = 0;
        try {
          sourceBuffer.appendBuffer(data);
        } catch {
          failAttempt(token, "media");
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
            // browser can reject trimming while busy
          }
        }
        if (end - video.currentTime > 5) video.currentTime = end - 0.5;
      }

      objectURL = URL.createObjectURL(source);
      video.src = objectURL;
      source.addEventListener("error", () => failAttempt(token, "media"), { once: true });
      source.addEventListener("sourceopen", () => {
        if (destroyed || token !== generation) return;
        ws = openPlayerSocket(streamName);
        ws.binaryType = "arraybuffer";
        ws.onopen = () => {
          ws?.send(JSON.stringify({ type: "mse", value: supportedCodecs(MediaSourceClass) }));
        };
        ws.onmessage = (event) => {
          if (token !== generation) return;
          if (typeof event.data === "string") {
            const message = parseMseControlMessage(event.data);
            if (message.type !== "mse") {
              failAttempt(token, "signaling");
              return;
            }
            try {
              sourceBuffer = source.addSourceBuffer(message.value);
              sourceBuffer.mode = "segments";
              sourceBuffer.addEventListener("error", () => failAttempt(token, "media"));
              sourceBuffer.addEventListener("updateend", handleUpdateEnd);
            } catch {
              failAttempt(token, "media");
            }
            return;
          }

          const now = Date.now();
          if (now - lastBinaryStateAt >= 1_000) {
            lastBinaryStateAt = now;
            setPlayback((current) => ({ ...current, lastBinaryAt: now, readyState: video.readyState }));
          }
          const chunk = new Uint8Array(event.data as ArrayBuffer);
          if (sourceBuffer && !sourceBuffer.updating && pendingLength === 0) {
            try {
              sourceBuffer.appendBuffer(event.data as ArrayBuffer);
            } catch {
              failAttempt(token, "media");
            }
          } else if (pendingLength + chunk.byteLength <= pending.byteLength) {
            pending.set(chunk, pendingLength);
            pendingLength += chunk.byteLength;
          } else {
            failAttempt(token, "media");
          }
          void video.play().catch(() => undefined);
        };
        ws.onclose = () => failAttempt(token, "socket");
        ws.onerror = () => failAttempt(token, "socket");
      }, { once: true });
      void video.play().catch(() => undefined);
    }

    video.addEventListener("timeupdate", handleTimeUpdate);
    beginAttempt(activeAttempt);
    return () => {
      destroyed = true;
      generation++;
      video.removeEventListener("timeupdate", handleTimeUpdate);
      teardownAttempt();
    };
  }, [candidateKey, resubscribeGeneration]);

  return {
    videoRef,
    connected: playback.phase === "playing",
    ...playback,
    episodeCounts: {
      attempt: playback.attempt,
      reconnects: playback.reconnectCount,
      fallbacks: playback.fallbackCount,
      resubscribes: playback.resubscribeCount,
    },
  };
}

function initialPlayback(streamName: string): PlaybackState {
  return {
    transport: "webrtc",
    phase: "connecting",
    activeStreamName: streamName,
    usingFallback: false,
    lastBinaryAt: null,
    lastProgressAt: null,
    readyState: 0,
    stalledForMs: 0,
    attempt: 1,
    reconnectCount: 0,
    fallbackCount: 0,
    resubscribeCount: 0,
    errorCategory: "none",
  };
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
