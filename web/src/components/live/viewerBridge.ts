import type { PlaybackTransport } from "./playbackRecovery";

const STREAM_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const TRANSPORTS = new Set<PlaybackTransport>(["webrtc", "mse"]);
const PHASES = new Set(["connecting", "retrying", "fallback", "recovering", "playing", "stalled", "cooldown", "unsupported"]);
const ERRORS = new Set(["none", "setup_timeout", "media_stall", "socket", "signaling", "media", "unsupported", "episode_exhausted"]);

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
  onCommand(handler: (command: unknown) => void): void | (() => void);
};

declare global {
  interface Window {
    camstationViewer?: CamStationViewerBridge;
  }
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
): () => void {
  if (!bridge) return () => undefined;
  try {
    const unsubscribe = bridge.onCommand((input) => {
      const command = safeCommand(input);
      if (command) handler(command);
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
  return typeof value === "string" && STREAM_NAME.test(value);
}
