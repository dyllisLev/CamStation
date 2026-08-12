import { withAppBase } from "./basePath.ts";

export const playbackDiagnosticEvents = {
  socket_open: "debug",
  signaling_answer: "debug",
  first_track: "debug",
  media_source_open: "debug",
  mse_ready: "debug",
  first_media: "debug",
  session_closed: "debug",
  attempt_started: "info",
  playback_started: "info",
  attempt_failed: "warn",
  episode_exhausted: "error",
  unsupported: "error",
} as const;

const transports = new Set(["webrtc", "mse"]);
const phases = new Set([
  "connecting",
  "retrying",
  "fallback",
  "recovering",
  "playing",
  "stalled",
  "cooldown",
  "unsupported",
]);
const errorCategories = new Set([
  "none",
  "setup_timeout",
  "media_stall",
  "socket",
  "signaling",
  "media",
  "unsupported",
  "episode_exhausted",
]);
const sessionPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{7,63}$/u;

export type PlaybackDiagnosticEvent = keyof typeof playbackDiagnosticEvents;
export type PlaybackDiagnosticLevel = (typeof playbackDiagnosticEvents)[PlaybackDiagnosticEvent];

export type SafePlaybackDiagnostic = {
  readonly sessionId: string;
  readonly event: PlaybackDiagnosticEvent;
  readonly streamName: string;
  readonly transport: "webrtc" | "mse";
  readonly phase: string;
  readonly attempt: number;
  readonly elapsedMs: number;
  readonly attemptElapsedMs: number;
  readonly errorCategory: string;
  readonly readyState: number;
  readonly usingFallback: boolean;
  readonly reconnectCount: number;
  readonly fallbackCount: number;
};

export function playbackDiagnosticLevel(event: string): PlaybackDiagnosticLevel | null {
  return Object.prototype.hasOwnProperty.call(playbackDiagnosticEvents, event)
    ? playbackDiagnosticEvents[event as PlaybackDiagnosticEvent]
    : null;
}

export function safePlaybackDiagnostic(input: Record<string, unknown>): SafePlaybackDiagnostic | null {
  const sessionId = boundedString(input.sessionId, 64);
  const event = boundedString(input.event, 32);
  const streamName = boundedString(input.streamName, 128);
  const transport = boundedString(input.transport, 8);
  const phase = boundedString(input.phase, 16);
  const errorCategory = boundedString(input.errorCategory ?? "none", 24);

  if (
    !sessionId
    || !sessionPattern.test(sessionId)
    || !event
    || playbackDiagnosticLevel(event) === null
    || !streamName
    || streamName !== streamName.trim()
    || /[\r\n\t]/u.test(streamName)
    || !transport
    || !transports.has(transport)
    || !phase
    || !phases.has(phase)
    || !errorCategory
    || !errorCategories.has(errorCategory)
  ) {
    return null;
  }

  return {
    sessionId,
    event: event as PlaybackDiagnosticEvent,
    streamName,
    transport: transport as "webrtc" | "mse",
    phase,
    attempt: boundedInteger(input.attempt, 1, 32, 1),
    elapsedMs: boundedInteger(input.elapsedMs, 0, 30 * 60_000, 0),
    attemptElapsedMs: boundedInteger(input.attemptElapsedMs, 0, 30 * 60_000, 0),
    errorCategory,
    readyState: boundedInteger(input.readyState, 0, 4, 0),
    usingFallback: input.usingFallback === true,
    reconnectCount: boundedInteger(input.reconnectCount, 0, 100, 0),
    fallbackCount: boundedInteger(input.fallbackCount, 0, 100, 0),
  };
}

export function reportPlaybackDiagnostic(input: Record<string, unknown>): void {
  const event = safePlaybackDiagnostic(input);
  if (!event || typeof fetch !== "function") return;
  void fetch(withAppBase("/api/playback/events"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(event),
    credentials: "same-origin",
    keepalive: true,
  }).catch(() => undefined);
}

function boundedString(value: unknown, maximum: number): string | null {
  return typeof value === "string" && value.length > 0 && value.length <= maximum ? value : null;
}

function boundedInteger(value: unknown, minimum: number, maximum: number, fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) return fallback;
  return Math.min(maximum, Math.max(minimum, Math.trunc(value)));
}
