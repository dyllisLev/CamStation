import { queryString, request } from "./http";
import type { PlaybackCamera, PlaybackResolveResponse, PlaybackSegmentsResponse, PlaybackTimelineResponse } from "./playbackTypes";

export const playbackApi = {
  cameras: (signal?: AbortSignal) => request<{ cameras: PlaybackCamera[] }>("/api/playback/cameras", { signal }),
  segments: (filter: { cameraKey: string; fromMs: number; toMs: number; limit?: number; cursor?: string }, signal?: AbortSignal) =>
    request<PlaybackSegmentsResponse>(`/api/playback/segments${queryString(filter)}`, { signal }),
  timeline: (cameraKey: string, fromMs: number, toMs: number, signal?: AbortSignal) =>
    request<PlaybackTimelineResponse>(`/api/playback/timeline${queryString({ cameraKey, fromMs: Math.round(fromMs), toMs: Math.round(toMs) })}`, { signal }),
  resolve: (cameraKey: string, atMs: number, signal?: AbortSignal) =>
    request<PlaybackResolveResponse>(`/api/playback/resolve${queryString({ cameraKey, atMs: Math.round(atMs) })}`, { signal }),
};
