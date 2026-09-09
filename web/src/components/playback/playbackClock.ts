import type { PlaybackMedia, PlaybackResolveResponse } from "../../app/playbackTypes.ts";

export function mediaTimeAt(media: PlaybackMedia, atMs: number): number {
  return media.mediaStartSeconds + Math.max(0, atMs - media.startMs) / 1000;
}

export function absoluteTimeAt(media: PlaybackMedia, seconds: number): number {
  return media.startMs + (seconds - media.mediaStartSeconds) * 1000;
}

export function containsPlaybackTime(media: PlaybackMedia, atMs: number): boolean {
  return atMs >= media.startMs && atMs < media.endMs;
}

// Normal recorder rotations may leave a few hundred milliseconds between files.
// Only an exhausted media boundary may bridge that gap; explicit seeks stay exact.
export function continuationSeekAt(result: PlaybackResolveResponse, endedAtMs?: number): number | undefined {
  const next = result.nextAtMs;
  if (endedAtMs === undefined || result.status !== "gap" || next === undefined) return undefined;
  return next > endedAtMs && next - endedAtMs <= 1000 ? next : undefined;
}

export function resumeBoundaryClock(atMs: number, readyMedia: readonly PlaybackMedia[]): number {
  if (readyMedia.some((media) => containsPlaybackTime(media, atMs))) return atMs;
  const starts = readyMedia.map((media) => media.startMs).filter((start) => start > atMs && start - atMs <= 1000);
  return starts.length ? Math.min(...starts) : atMs;
}

// Accept only public, same-application media routes, including when hosted below a base path.
export function isPlaybackMediaUrl(url: string): boolean {
  return /^\/api\/recordings\/segments\/\d+\/play$/.test(url)
    || /^\/api\/playback\/media\/[a-zA-Z0-9_-]+\/manifest\.m3u8$/.test(url);
}
