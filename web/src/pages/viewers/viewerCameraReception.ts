import type { Camera, Viewer, ViewerStreamHealth } from "../../app/api";
import { playbackStreamCandidates } from "../../components/live/streamSelection.ts";

const CURRENT_TELEMETRY_WINDOW_MS = 15_000;
const FUTURE_CLOCK_TOLERANCE_MS = 5_000;

export type ViewerCameraReception = {
  readonly cameraName: string;
  readonly cameraStreamName: string;
  readonly resubscribeStreamName: string;
  readonly receiving: boolean;
  readonly activeStreamName?: string;
  readonly transport?: string;
  readonly lastProgressAt?: string;
};

export type ViewerCameraReceptionSummary = {
  readonly receptions: readonly ViewerCameraReception[];
  readonly receiving: readonly ViewerCameraReception[];
  readonly missing: readonly ViewerCameraReception[];
  readonly receivingCount: number;
  readonly totalCount: number;
};

export function viewerCameraReceptionSummary(
  viewer: Viewer | undefined,
  cameras: readonly Camera[],
): ViewerCameraReceptionSummary {
  const activeCameras = cameras.filter((camera) => camera.enabled);
  const streams = viewer?.streams ?? [];
  const referenceAt = latestTelemetryReference(viewer, streams);
  const rendering = viewerIsRendering(viewer);
  const receptions = activeCameras.map((camera) => {
    const candidates = playbackStreamCandidates(camera);
    const candidateStreams = streams
      .filter((stream) => candidates.includes(stream.streamName))
      .sort(compareMostRecentStream);
    const currentStream = candidateStreams[0];
    const receiving = Boolean(rendering && currentStream && streamIsReceiving(currentStream, referenceAt));

    return {
      cameraName: camera.name,
      cameraStreamName: camera.streamName,
      resubscribeStreamName: candidates[0] ?? camera.streamName,
      receiving,
      ...(currentStream?.streamName ? { activeStreamName: currentStream.streamName } : {}),
      ...(currentStream?.transport ? { transport: currentStream.transport } : {}),
      ...(currentStream?.lastProgressAt ? { lastProgressAt: currentStream.lastProgressAt } : {}),
    } satisfies ViewerCameraReception;
  });
  const receiving = receptions.filter((reception) => reception.receiving);
  const missing = receptions.filter((reception) => !reception.receiving);

  return {
    receptions,
    receiving,
    missing,
    receivingCount: receiving.length,
    totalCount: receptions.length,
  };
}

function viewerIsRendering(viewer: Viewer | undefined): boolean {
  return viewer !== undefined
    && viewer.status !== "offline"
    && viewer.status !== "stale"
    && viewer.viewer?.state === "running"
    && viewer.renderer?.state === "ready";
}

function latestTelemetryReference(viewer: Viewer | undefined, streams: readonly ViewerStreamHealth[]): number | undefined {
  const timestamps = [
    parseTimestamp(viewer?.renderer?.lastHeartbeatAt),
    ...streams.map((stream) => parseTimestamp(stream.updatedAt)),
  ].filter((value): value is number => value !== undefined);
  return timestamps.length > 0 ? Math.max(...timestamps) : undefined;
}

function streamIsReceiving(stream: ViewerStreamHealth, referenceAt: number | undefined): boolean {
  const progressAt = parseTimestamp(stream.lastProgressAt);
  const updatedAt = parseTimestamp(stream.updatedAt);
  if (stream.state !== "playing" || referenceAt === undefined || progressAt === undefined || updatedAt === undefined) {
    return false;
  }
  return timestampIsCurrent(progressAt, referenceAt) && timestampIsCurrent(updatedAt, referenceAt);
}

function timestampIsCurrent(timestamp: number, referenceAt: number): boolean {
  return timestamp >= referenceAt - CURRENT_TELEMETRY_WINDOW_MS
    && timestamp <= referenceAt + FUTURE_CLOCK_TOLERANCE_MS;
}

function compareMostRecentStream(left: ViewerStreamHealth, right: ViewerStreamHealth): number {
  return (parseTimestamp(right.updatedAt) ?? 0) - (parseTimestamp(left.updatedAt) ?? 0)
    || (parseTimestamp(right.lastProgressAt) ?? 0) - (parseTimestamp(left.lastProgressAt) ?? 0);
}

function parseTimestamp(value: string | undefined): number | undefined {
  if (!value) return undefined;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? timestamp : undefined;
}
