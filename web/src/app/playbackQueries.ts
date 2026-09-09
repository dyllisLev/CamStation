import { useInfiniteQuery, useQueries, useQuery } from "@tanstack/react-query";
import { playbackApi } from "./playbackApi";

export const playbackKeys = {
  cameras: ["playback", "cameras"] as const,
  timeline: (cameraKey: string, fromMs: number, toMs: number) => ["playback", "timeline", cameraKey, fromMs, toMs] as const,
};

export function usePlaybackCameras() {
  return useQuery({ queryKey: playbackKeys.cameras, queryFn: ({ signal }) => playbackApi.cameras(signal), refetchInterval: 15_000 });
}

export function usePlaybackSegments(cameraKey: string, fromMs: number, toMs: number, enabled = true) {
  return useInfiniteQuery({
    queryKey: ["playback", "segments", cameraKey, fromMs, toMs],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => playbackApi.segments({ cameraKey, fromMs, toMs, limit: 50, cursor: pageParam }, signal),
    getNextPageParam: (page) => page.nextCursor ?? undefined,
    enabled: enabled && !!cameraKey && Number.isFinite(fromMs) && toMs > fromMs,
  });
}

function timelineOptions(cameraKey: string, fromMs: number, toMs: number, enabled: boolean) {
  return {
    queryKey: playbackKeys.timeline(cameraKey, fromMs, toMs),
    queryFn: ({ signal }: { signal: AbortSignal }) => playbackApi.timeline(cameraKey, fromMs, toMs, signal),
    enabled: enabled && !!cameraKey && Number.isFinite(fromMs) && toMs > fromMs,
    refetchInterval: 5_000,
  };
}

export function usePlaybackTimeline(cameraKey: string, fromMs: number, toMs: number, enabled = true) {
  return useQuery(timelineOptions(cameraKey, fromMs, toMs, enabled));
}

export function usePlaybackTimelines(cameraKeys: readonly string[], fromMs: number, toMs: number, enabled = true) {
  return useQueries({ queries: cameraKeys.map((key) => timelineOptions(key, fromMs, toMs, enabled)) });
}
