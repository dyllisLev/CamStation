import type { PlaybackTransport } from "./playbackRecovery";

const URL_LIKE = /^[a-z][a-z0-9+.-]*:/iu;
const TRANSPORTS = new Set<PlaybackTransport>(["webrtc", "mse"]);
const PHASES = new Set(["connecting", "retrying", "fallback", "recovering", "playing", "stalled", "cooldown", "unsupported"]);
const ERRORS = new Set(["none", "setup_timeout", "media_stall", "socket", "signaling", "media", "unsupported", "episode_exhausted"]);
const DIAGNOSTIC_LEVELS = new Set(["debug", "info", "warn", "error"]);
const DIAGNOSTIC_COMPONENTS = new Set(["viewer.main", "viewer.renderer", "viewer.playback", "viewer.control"]);
const DIAGNOSTIC_EVENT = /^[a-z][a-z0-9_]{1,63}$/u;
const DIAGNOSTIC_CODE = /^[a-z][a-z0-9_]{0,63}$/u;
const DIAGNOSTIC_CORRELATION = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/u;
const DIAGNOSTIC_SESSION = /^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$/u;

export type ViewerStreamTelemetry = {
  readonly streamName: string;
  readonly transport: PlaybackTransport;
  readonly phase: string;
  readonly lastBinaryAt?: number;
  readonly lastProgressAt?: number;
  readonly readyState?: number;
  readonly stalledForMs?: number;
  readonly reconnectCount?: number;
  readonly fallbackCount?: number;
  readonly resubscribeCount?: number;
  readonly errorCategory?: string;
};

export type ViewerCommand = { readonly type: "resubscribe_stream"; readonly streamName: string };

export type CamStationViewerBridge = {
  reportStream(telemetry: ViewerStreamTelemetry): void;
  reportDiagnostic?(diagnostic: Record<string, unknown>): void;
  onCommand(handler: (command: unknown) => boolean | Promise<boolean>): void | (() => void);
  setFullscreen?(fullscreen: boolean): Promise<unknown> | void;
  onFullscreenChange?(handler: (fullscreen: boolean) => void): void | (() => void);
};

declare global {
  interface Window {
    camstationViewer?: CamStationViewerBridge;
  }
}

export function reportViewerDiagnostic(input: Record<string, unknown>, bridge = preloadBridge()): void {
  const diagnostic = safeViewerDiagnostic(input);
  if (!diagnostic || typeof bridge?.reportDiagnostic !== "function") return;
  try {
    bridge.reportDiagnostic(diagnostic);
  } catch {
    // Local logging must not affect video playback.
  }
}

export function safeViewerDiagnostic(input: Record<string, unknown>): Record<string, unknown> | null {
  if (
    typeof input.level !== "string" || !DIAGNOSTIC_LEVELS.has(input.level)
    || typeof input.component !== "string" || !DIAGNOSTIC_COMPONENTS.has(input.component)
    || typeof input.event !== "string" || !DIAGNOSTIC_EVENT.test(input.event)
  ) return null;
  const diagnostic: Record<string, unknown> = {
    level: input.level,
    component: input.component,
    event: input.event,
  };
  if (!copyPatternString(diagnostic, input, "correlationId", DIAGNOSTIC_CORRELATION)) return null;
  if (!copyPatternString(diagnostic, input, "sessionId", DIAGNOSTIC_SESSION)) return null;
  if (input.streamName !== undefined) {
    if (!safeStreamName(input.streamName)) return null;
    diagnostic.streamName = input.streamName;
  }
  if (!copyEnumString(diagnostic, input, "transport", TRANSPORTS)) return null;
  if (!copyEnumString(diagnostic, input, "phase", PHASES)) return null;
  if (!copyPatternString(diagnostic, input, "state", DIAGNOSTIC_CODE)) return null;
  if (!copyPatternString(diagnostic, input, "errorCode", DIAGNOSTIC_CODE)) return null;
  copyBoundedDiagnosticInteger(diagnostic, input, "attempt", 0, 32);
  copyBoundedDiagnosticInteger(diagnostic, input, "durationMs", 0, 30 * 60_000);
  copyBoundedDiagnosticInteger(diagnostic, input, "attemptElapsedMs", 0, 30 * 60_000);
  copyBoundedDiagnosticInteger(diagnostic, input, "retryMs", 0, 30 * 60_000);
  copyBoundedDiagnosticInteger(diagnostic, input, "readyState", 0, 4);
  copyBoundedDiagnosticInteger(diagnostic, input, "reconnectCount", 0, 100);
  copyBoundedDiagnosticInteger(diagnostic, input, "fallbackCount", 0, 100);
  if (typeof input.usingFallback === "boolean") diagnostic.usingFallback = input.usingFallback;
  return diagnostic;
}

export function reportViewerStream(input: Record<string, unknown>, bridge = preloadBridge()): void {
  const telemetry = safeTelemetry(input);
  if (!telemetry || !bridge) return;
  try {
    bridge.reportStream(telemetry);
  } catch {
    // Agent IPC health must not affect video playback.
  }
}

export function subscribeViewerCommands(
  handler: (command: ViewerCommand) => void,
  bridge = preloadBridge(),
  registeredStreamNames?: readonly string[],
): () => void {
  if (!bridge) return () => undefined;
  const registered = registeredStreamNames ? new Set(registeredStreamNames) : null;
  try {
    const unsubscribe = bridge.onCommand((input) => {
      const command = safeCommand(input);
      if (!command || (registered && !registered.has(command.streamName))) return false;
      handler(command);
      return true;
    });
    return typeof unsubscribe === "function"
      ? () => {
          try {
            unsubscribe();
          } catch {
            // Agent IPC health must not affect React cleanup.
          }
        }
      : () => undefined;
  } catch {
    return () => undefined;
  }
}

export function hasViewerFullscreenBridge(bridge = preloadBridge()): boolean {
  return typeof bridge?.setFullscreen === "function" && typeof bridge.onFullscreenChange === "function";
}

export async function requestViewerFullscreen(fullscreen: boolean, bridge = preloadBridge()): Promise<boolean> {
  if (typeof bridge?.setFullscreen !== "function") return false;
  try {
    await bridge.setFullscreen(fullscreen);
    return true;
  } catch {
    return false;
  }
}

export function subscribeViewerFullscreen(handler: (fullscreen: boolean) => void, bridge = preloadBridge()): () => void {
  if (typeof bridge?.onFullscreenChange !== "function") return () => undefined;
  try {
    const unsubscribe = bridge.onFullscreenChange(handler);
    return typeof unsubscribe === "function"
      ? () => {
          try {
            unsubscribe();
          } catch {
            // Native fullscreen IPC cleanup must not affect the live workspace.
          }
        }
      : () => undefined;
  } catch {
    return () => undefined;
  }
}

function preloadBridge(): CamStationViewerBridge | undefined {
  return typeof window === "undefined" ? undefined : window.camstationViewer;
}

function safeTelemetry(input: Record<string, unknown>): ViewerStreamTelemetry | null {
  if (!safeStreamName(input.streamName) || !TRANSPORTS.has(input.transport as PlaybackTransport) || !PHASES.has(input.phase as string)) {
    return null;
  }
  const telemetry: Record<string, string | number> = {
    streamName: input.streamName,
    transport: input.transport as string,
    phase: input.phase as string,
  };
  for (const key of ["lastBinaryAt", "lastProgressAt", "readyState", "stalledForMs", "reconnectCount", "fallbackCount", "resubscribeCount"] as const) {
    const value = input[key];
    if (typeof value === "number" && Number.isFinite(value) && value >= 0) telemetry[key] = Math.floor(value);
  }
  if (typeof input.errorCategory === "string" && ERRORS.has(input.errorCategory)) telemetry.errorCategory = input.errorCategory;
  return telemetry as ViewerStreamTelemetry;
}

function safeCommand(input: unknown): ViewerCommand | null {
  if (!input || typeof input !== "object") return null;
  const command = input as Record<string, unknown>;
  if (command.type !== "resubscribe_stream" || !safeStreamName(command.streamName)) return null;
  return { type: "resubscribe_stream", streamName: command.streamName };
}

function safeStreamName(value: unknown): value is string {
  if (typeof value !== "string" || value.length === 0 || value.length > 128) return false;
  const trimmed = value.trim();
  const containsControlCharacter = Array.from(value).some((character) => {
    const code = character.charCodeAt(0);
    return code <= 31 || (code >= 127 && code <= 159);
  });
  return value === trimmed
    && trimmed.length > 0
    && !containsControlCharacter
    && !URL_LIKE.test(trimmed)
    && !trimmed.startsWith("//");
}

function copyPatternString(
  output: Record<string, unknown>,
  input: Record<string, unknown>,
  key: string,
  pattern: RegExp,
): boolean {
  const value = input[key];
  if (value === undefined) return true;
  if (typeof value !== "string" || !pattern.test(value)) return false;
  output[key] = value;
  return true;
}

function copyEnumString(
  output: Record<string, unknown>,
  input: Record<string, unknown>,
  key: string,
  allowed: ReadonlySet<unknown>,
): boolean {
  const value = input[key];
  if (value === undefined) return true;
  if (typeof value !== "string" || !allowed.has(value)) return false;
  output[key] = value;
  return true;
}

function copyBoundedDiagnosticInteger(
  output: Record<string, unknown>,
  input: Record<string, unknown>,
  key: string,
  minimum: number,
  maximum: number,
): void {
  const value = input[key];
  if (typeof value === "number" && Number.isFinite(value)) {
    output[key] = Math.min(maximum, Math.max(minimum, Math.trunc(value)));
  }
}
