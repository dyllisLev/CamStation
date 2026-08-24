import { useEffect, useRef, useState } from "react";
import {
  reportPlaybackDiagnostic,
  type PlaybackDiagnosticEvent,
} from "../../app/playbackDiagnosticsApi";
import { openPlaybackConnection, probePlaybackProgress } from "./playbackConnection";
import {
  PLAYBACK_SETUP_MS,
  PLAYBACK_STALL_MS,
  PlaybackPrimaryPromoter,
  PlaybackProbeScheduler,
  PlaybackRecovery,
  recoveryAttemptPresentation,
  type PlaybackRecoveryStep,
  type PlaybackTransport,
} from "./playbackRecovery";
import { observePresentedVideoFrames } from "./videoFrameProgress";

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
  preferredTransport: PlaybackTransport = "webrtc",
) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const input = typeof streamNames === "string" ? [streamNames] : streamNames;
  const candidateKey = input.filter(Boolean).join("\u001f");
  const [playback, setPlayback] = useState<PlaybackState>(() => initialPlayback(input[0] ?? "", preferredTransport));

  useEffect(() => {
    const candidates = candidateKey ? candidateKey.split("\u001f") : [];
    const videoElement = videoRef.current;
    if (!videoElement || candidates.length === 0) return;
    const video: HTMLVideoElement = videoElement;

    const recovery = new PlaybackRecovery(candidates);
    const recoveryProbeScheduler = new PlaybackProbeScheduler();
    const primaryPromoter = new PlaybackPrimaryPromoter((transport, signal) => (
      probePlaybackProgress(candidates[0], transport, signal)
    ));
    const sessionId = newPlaybackSessionId();
    const sessionStartedAt = Date.now();
    let destroyed = false;
    let generation = 0;
    let closeConnection: (() => void) | null = null;
    let setupTimer: ReturnType<typeof setTimeout> | null = null;
    let stallTimer: ReturnType<typeof setTimeout> | null = null;
    let lastVideoTime = -1;
    let lastPresentedFrameAt = 0;
    let lastBinaryStateAt = 0;
    let lastProgressStateAt = 0;
    let attemptStartedAt = sessionStartedAt;
    let diagnosticPhase: PlaybackPhase = "connecting";
    const attemptEvents = new Set<PlaybackDiagnosticEvent>();
    let activeAttempt: AttemptOptions = {
      transport: preferredTransport,
      streamName: candidates[0],
      attempt: 1,
      phase: "connecting",
    };
    const counts = { reconnect: 0, fallback: 0, resubscribe: 0 };

    function emitDiagnostic(
      event: PlaybackDiagnosticEvent,
      override: {
        readonly phase?: PlaybackPhase;
        readonly errorCategory?: PlaybackErrorCategory;
        readonly streamName?: string;
        readonly transport?: PlaybackTransport;
        readonly attempt?: number;
        readonly usingFallback?: boolean;
      } = {},
    ) {
      const now = Date.now();
      const streamName = override.streamName ?? activeAttempt.streamName;
      reportPlaybackDiagnostic({
        sessionId,
        event,
        streamName,
        transport: override.transport ?? activeAttempt.transport,
        phase: override.phase ?? diagnosticPhase,
        attempt: override.attempt ?? activeAttempt.attempt,
        elapsedMs: now - sessionStartedAt,
        attemptElapsedMs: now - attemptStartedAt,
        errorCategory: override.errorCategory ?? "none",
        readyState: video.readyState,
        usingFallback: override.usingFallback ?? streamName !== candidates[0],
        reconnectCount: counts.reconnect,
        fallbackCount: counts.fallback,
      });
    }

    function emitAttemptDiagnosticOnce(
      event: PlaybackDiagnosticEvent,
      override?: Parameters<typeof emitDiagnostic>[1],
    ) {
      if (attemptEvents.has(event)) return;
      attemptEvents.add(event);
      emitDiagnostic(event, override);
    }

    function clearTimers() {
      if (setupTimer) clearTimeout(setupTimer);
      if (stallTimer) clearTimeout(stallTimer);
      recoveryProbeScheduler.clear();
      primaryPromoter.stop();
      setupTimer = null;
      stallTimer = null;
    }

    function teardownAttempt() {
      clearTimers();
      closeConnection?.();
      closeConnection = null;
      lastVideoTime = video.currentTime;
    }

    function publishAttempt(options: AttemptOptions, errorCategory: PlaybackErrorCategory) {
      lastBinaryStateAt = 0;
      lastProgressStateAt = 0;
      lastPresentedFrameAt = 0;
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

    function enterCooldown(until: number) {
      diagnosticPhase = "cooldown";
      emitDiagnostic("episode_exhausted", {
        phase: "cooldown",
        errorCategory: "episode_exhausted",
      });
      generation++;
      teardownAttempt();
      setPlayback((current) => ({
        ...current,
        phase: "cooldown",
        readyState: video.readyState,
        stalledForMs: recovery.stalledForMs(Date.now()),
        errorCategory: "episode_exhausted",
      }));
      recoveryProbeScheduler.arm(until, () => {
        recovery.restartEpisode(Date.now());
        beginAttempt({
          transport: preferredTransport,
          streamName: candidates[0],
          attempt: 1,
          phase: "retrying",
        }, "episode_exhausted");
      });
    }

    function advance(step: PlaybackRecoveryStep, errorCategory: PlaybackErrorCategory) {
      if ("action" in step) {
        if (step.action === "cooldown") {
          enterCooldown(step.until);
          return;
        }
        counts.resubscribe++;
        beginAttempt({
          transport: preferredTransport,
          streamName: candidates[0],
          attempt: step.attempt,
          phase: "recovering",
        }, errorCategory);
        return;
      }
      const presentation = recoveryAttemptPresentation(
        step,
        candidates[0],
        activeAttempt.transport,
      );
      if (presentation.usingFallback) counts.fallback++;
      else counts.reconnect++;
      beginAttempt({
        ...step,
        phase: presentation.phase,
      }, errorCategory);
    }

    function failAttempt(token: number, errorCategory: PlaybackErrorCategory) {
      if (destroyed || token !== generation) return;
      const now = Date.now();
      const failurePhase = errorCategory === "media_stall" ? "stalled" : diagnosticPhase;
      emitDiagnostic("attempt_failed", { phase: failurePhase, errorCategory });
      if (errorCategory === "unsupported") {
        emitDiagnostic("unsupported", { phase: "unsupported", errorCategory });
      }
      recovery.recordFailure(now);
      generation++;
      teardownAttempt();
      advance(recovery.nextFailure(now), errorCategory);
    }

    function resetStallTimer(token: number) {
      if (stallTimer) clearTimeout(stallTimer);
      stallTimer = setTimeout(() => failAttempt(token, "media_stall"), PLAYBACK_STALL_MS);
    }

    function ensurePrimaryPromotion() {
      if (
        candidates.length < 2
        || activeAttempt.streamName === candidates[0]
        || primaryPromoter.active
      ) return;
      primaryPromoter.start(preferredTransport, {
        onProbeStarted: (transport) => {
          emitDiagnostic("primary_probe_started", {
            streamName: candidates[0],
            transport,
            phase: "fallback",
            attempt: activeAttempt.attempt + 1,
            usingFallback: true,
          });
        },
        onProbeFailed: (transport) => {
          emitDiagnostic("primary_probe_failed", {
            streamName: candidates[0],
            transport,
            phase: "fallback",
            attempt: activeAttempt.attempt + 1,
            usingFallback: true,
          });
        },
        onRecovered: (transport) => {
          if (destroyed || activeAttempt.streamName === candidates[0] || diagnosticPhase !== "playing") return;
          emitDiagnostic("primary_probe_succeeded", {
            streamName: candidates[0],
            transport,
            phase: "recovering",
            attempt: activeAttempt.attempt + 1,
            usingFallback: true,
          });
          counts.reconnect++;
          recovery.resetForPrimaryPromotion();
          beginAttempt({
            transport,
            streamName: candidates[0],
            attempt: activeAttempt.attempt + 1,
            phase: "recovering",
          });
        },
      });
    }

    function markProgress(token: number) {
      if (destroyed || token !== generation) return;
      const now = Date.now();
      if (setupTimer) clearTimeout(setupTimer);
      setupTimer = null;
      resetStallTimer(token);
      diagnosticPhase = "playing";
      emitAttemptDiagnosticOnce("playback_started", {
        phase: "playing",
        errorCategory: "none",
      });
      const budgetReset = recovery.recordProgress(now);
      if (budgetReset) {
        counts.reconnect = 0;
        counts.fallback = 0;
        counts.resubscribe = 0;
      }
      ensurePrimaryPromotion();
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

    const stopPresentedFrameObservation = observePresentedVideoFrames(video, () => {
      const now = Date.now();
      if (!closeConnection || now - lastPresentedFrameAt < 1_000) return;
      lastPresentedFrameAt = now;
      markProgress(generation);
    });

    function beginAttempt(
      options: AttemptOptions,
      previousError: PlaybackErrorCategory = "none",
    ) {
      if (destroyed) return;
      teardownAttempt();
      const now = Date.now();
      const remainingMs = recovery.remainingMs(now);
      if (remainingMs === 0) {
        recovery.recordFailure(now);
        advance(recovery.nextFailure(now), "episode_exhausted");
        return;
      }
      const token = ++generation;
      activeAttempt = options;
      attemptStartedAt = now;
      diagnosticPhase = options.phase;
      attemptEvents.clear();
      publishAttempt(options, previousError);
      emitDiagnostic("attempt_started", {
        phase: options.phase,
        errorCategory: previousError,
      });
      setupTimer = setTimeout(
        () => failAttempt(token, "setup_timeout"),
        recovery.boundedDelayMs(Date.now(), PLAYBACK_SETUP_MS),
      );
      closeConnection = openPlaybackConnection({
        video,
        streamName: options.streamName,
        transport: options.transport,
        onEvent: (event) => {
          if (!destroyed && token === generation) emitAttemptDiagnosticOnce(event);
        },
        onBinary: () => {
          if (destroyed || token !== generation) return;
          const binaryAt = Date.now();
          if (binaryAt - lastBinaryStateAt < 1_000) return;
          lastBinaryStateAt = binaryAt;
          setPlayback((current) => ({ ...current, lastBinaryAt: binaryAt, readyState: video.readyState }));
        },
        onFailure: (errorCategory) => failAttempt(token, errorCategory),
      });
    }

    video.addEventListener("timeupdate", handleTimeUpdate);
    beginAttempt(activeAttempt);
    return () => {
      emitDiagnostic("session_closed");
      destroyed = true;
      generation++;
      stopPresentedFrameObservation();
      video.removeEventListener("timeupdate", handleTimeUpdate);
      teardownAttempt();
    };
  }, [candidateKey, preferredTransport, resubscribeGeneration]);

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

function initialPlayback(streamName: string, transport: PlaybackTransport): PlaybackState {
  return {
    transport,
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

function newPlaybackSessionId() {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return `playback-${crypto.randomUUID()}`;
  }
  return `playback-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 14)}`;
}
