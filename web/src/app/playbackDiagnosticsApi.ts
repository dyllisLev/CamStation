import { withAppBase } from "./basePath.ts";
import { reportViewerDiagnostic } from "../components/live/viewerBridge.ts";

export const playbackDiagnosticEvents = {
  socket_open: "debug",
  signaling_answer: "debug",
  first_track: "debug",
  media_source_open: "debug",
  mse_ready: "debug",
  first_media: "debug",
  primary_probe_started: "debug",
  primary_probe_failed: "debug",
  session_closed: "debug",
  attempt_started: "info",
  playback_started: "info",
  primary_probe_succeeded: "info",
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
const playbackSurfaces = new Set([
  "official_viewer",
  "viewer_browser",
  "operator_live",
  "viewer_page",
  "control_room_preview",
  "camera_profile_preview",
  "unknown",
]);
const candidateRoles = new Set(["primary", "fallback", "primary_probe"]);
const terminalReasons = new Set([
  "setup_timeout",
  "media_stall",
  "socket",
  "signaling",
  "media",
  "unsupported",
  "retry_budget_exhausted",
  "primary_restored",
  "resubscribe_requested",
  "candidates_changed",
  "transport_changed",
  "surface_changed",
  "component_unmounted",
]);
const sessionPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{7,63}$/u;

export type PlaybackDiagnosticEvent = keyof typeof playbackDiagnosticEvents;
export type PlaybackDiagnosticLevel = (typeof playbackDiagnosticEvents)[PlaybackDiagnosticEvent];
export type PlaybackSurface =
  | "official_viewer"
  | "viewer_browser"
  | "operator_live"
  | "viewer_page"
  | "control_room_preview"
  | "camera_profile_preview"
  | "unknown";
export type PlaybackCandidateRole = "primary" | "fallback" | "primary_probe";
export type PlaybackTerminalReason =
  | "setup_timeout"
  | "media_stall"
  | "socket"
  | "signaling"
  | "media"
  | "unsupported"
  | "retry_budget_exhausted"
  | "primary_restored"
  | "resubscribe_requested"
  | "candidates_changed"
  | "transport_changed"
  | "surface_changed"
  | "component_unmounted";

export type SafePlaybackDiagnostic = {
  readonly sessionId: string;
  readonly documentId?: string;
  readonly surface?: PlaybackSurface;
  readonly event: PlaybackDiagnosticEvent;
  readonly streamName: string;
  readonly candidateRole?: PlaybackCandidateRole;
  readonly transport: "webrtc" | "mse";
  readonly phase: string;
  readonly attempt: number;
  readonly attemptGeneration?: number;
  readonly resubscribeGeneration?: number;
  readonly elapsedMs: number;
  readonly attemptElapsedMs: number;
  readonly errorCategory: string;
  readonly terminalReason?: PlaybackTerminalReason;
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
  const documentId = boundedString(input.documentId, 64);
  const event = boundedString(input.event, 32);
  const streamName = boundedString(input.streamName, 128);
  const transport = boundedString(input.transport, 8);
  const phase = boundedString(input.phase, 16);
  const errorCategory = boundedString(input.errorCategory ?? "none", 24);
  const surface = boundedString(input.surface, 32);
  const candidateRole = boundedString(input.candidateRole, 16);
  const terminalReason = boundedString(input.terminalReason, 32);
  const attemptGeneration = optionalBoundedInteger(input.attemptGeneration, 1, 1_000_000);
  const resubscribeGeneration = optionalBoundedInteger(input.resubscribeGeneration, 0, 1_000_000);

  if (
    !sessionId
    || !sessionPattern.test(sessionId)
    || (input.documentId !== undefined && (!documentId || !sessionPattern.test(documentId)))
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
    || (input.surface !== undefined && (!surface || !playbackSurfaces.has(surface)))
    || (input.candidateRole !== undefined && (!candidateRole || !candidateRoles.has(candidateRole)))
    || (input.terminalReason !== undefined && (!terminalReason || !terminalReasons.has(terminalReason)))
  ) {
    return null;
  }

  return {
    sessionId,
    ...(documentId ? { documentId } : {}),
    ...(surface ? { surface: surface as PlaybackSurface } : {}),
    event: event as PlaybackDiagnosticEvent,
    streamName,
    ...(candidateRole ? { candidateRole: candidateRole as PlaybackCandidateRole } : {}),
    transport: transport as "webrtc" | "mse",
    phase,
    attempt: boundedInteger(input.attempt, 1, 32, 1),
    ...(attemptGeneration !== undefined ? { attemptGeneration } : {}),
    ...(resubscribeGeneration !== undefined ? { resubscribeGeneration } : {}),
    elapsedMs: boundedInteger(input.elapsedMs, 0, 30 * 60_000, 0),
    attemptElapsedMs: boundedInteger(input.attemptElapsedMs, 0, 30 * 60_000, 0),
    errorCategory,
    ...(terminalReason ? { terminalReason: terminalReason as PlaybackTerminalReason } : {}),
    readyState: boundedInteger(input.readyState, 0, 4, 0),
    usingFallback: input.usingFallback === true,
    reconnectCount: boundedInteger(input.reconnectCount, 0, 100, 0),
    fallbackCount: boundedInteger(input.fallbackCount, 0, 100, 0),
  };
}

export function reportPlaybackDiagnostic(input: Record<string, unknown>): void {
  const event = safePlaybackDiagnostic(input);
  if (!event) return;
  reportViewerDiagnostic({
    level: playbackDiagnosticLevel(event.event),
    component: "viewer.playback",
    event: event.event,
    sessionId: event.sessionId,
    ...(event.documentId ? { documentId: event.documentId } : {}),
    ...(event.surface ? { surface: event.surface } : {}),
    streamName: event.streamName,
    ...(event.candidateRole ? { candidateRole: event.candidateRole } : {}),
    transport: event.transport,
    phase: event.phase,
    attempt: event.attempt,
    ...(event.attemptGeneration !== undefined ? { attemptGeneration: event.attemptGeneration } : {}),
    ...(event.resubscribeGeneration !== undefined ? { resubscribeGeneration: event.resubscribeGeneration } : {}),
    durationMs: event.elapsedMs,
    attemptElapsedMs: event.attemptElapsedMs,
    readyState: event.readyState,
    usingFallback: event.usingFallback,
    reconnectCount: event.reconnectCount,
    fallbackCount: event.fallbackCount,
    ...(event.terminalReason ? { terminalReason: event.terminalReason } : {}),
    ...(event.errorCategory === "none" ? {} : { errorCode: event.errorCategory }),
  });
  if (typeof fetch !== "function") return;
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

function optionalBoundedInteger(value: unknown, minimum: number, maximum: number): number | undefined {
  if (typeof value !== "number" || !Number.isFinite(value)) return undefined;
  return Math.min(maximum, Math.max(minimum, Math.trunc(value)));
}
