import { useCallback, useEffect, useRef, useState } from "react";
import { playbackApi } from "../../app/playbackApi";
import type { PlaybackMedia } from "../../app/playbackTypes";
import { absoluteTimeAt, containsPlaybackTime, continuationSeekAt, mediaTimeAt, resumeBoundaryClock } from "./playbackClock";

export type PlaybackCameraState = {
  status: "idle" | "loading" | "ready" | "playing" | "gap" | "edge_wait" | "error" | "unsupported";
  media?: PlaybackMedia;
  nextAtMs?: number;
  previousAtMs?: number;
  error?: string;
  requestId: number;
  continuationFromMs?: number;
};

type WorkspaceState = {
  mode: "live" | "playback";
  playheadMs: number;
  playing: boolean;
  rate: number;
  volume: number;
  muted: boolean;
  hasSelection: boolean;
  seekGeneration: number;
  cameras: Record<string, PlaybackCameraState>;
};

export type PlaybackWorkspace = WorkspaceState & {
  seek: (atMs: number, options?: { play?: boolean }) => void;
  play: () => void;
  pause: () => void;
  togglePlaying: () => void;
  step: (seconds: number) => void;
  setRate: (rate: number) => void;
  setVolume: (volume: number) => void;
  setMuted: (muted: boolean) => void;
  goLive: () => void;
  retry: (cameraKey?: string) => void;
  nextRecording: (cameraKey?: string) => void;
  // The video adapter owns media loading. These callbacks reject obsolete media events.
  attachVideo: (cameraKey: string, video: HTMLVideoElement) => () => void;
  videoReady: (cameraKey: string, requestId: number) => void;
  videoError: (cameraKey: string, requestId: number, message: string) => void;
  videoEnded: (cameraKey: string, requestId: number) => void;
  currentTime: () => number;
  setVideoMuted: (cameraKey: string, muted: boolean) => void;
};

export function usePlaybackWorkspace({ cameraKeys, initialMode = "playback", initialAtMs }: {
  cameraKeys: readonly string[];
  initialMode?: "live" | "playback";
  initialAtMs?: number;
}): PlaybackWorkspace {
  const [state, setState] = useState<WorkspaceState>(() => ({
    mode: initialMode, playheadMs: initialAtMs ?? Date.now(), playing: false,
    rate: 1, volume: 1, muted: true, hasSelection: initialAtMs !== undefined,
    seekGeneration: 0, cameras: {},
  }));
  const current = useRef(state);
  const keys = useRef<readonly string[]>([]);
  const videos = useRef(new Map<string, HTMLVideoElement>());
  const mutedVideos = useRef(new Map<string, boolean>());
  const requests = useRef(new Map<string, AbortController>());
  const requestSequence = useRef(0);
  const preparation = useRef(false);
  const lastPoll = useRef(new Map<string, number>());
  const playPending = useRef(new WeakSet<HTMLVideoElement>());

  const update = useCallback((patch: Partial<WorkspaceState>) => {
    current.current = { ...current.current, ...patch };
    setState(current.current);
  }, []);
  const cameraUpdate = useCallback((key: string, camera: PlaybackCameraState) => {
    update({ cameras: { ...current.current.cameras, [key]: camera } });
  }, [update]);
  const pauseVideos = useCallback(() => { videos.current.forEach((video) => video.pause()); }, []);

  const resolveCamera = useCallback(async (key: string, atMs: number, continuationFromMs?: number) => {
    requests.current.get(key)?.abort();
    const controller = new AbortController();
    requests.current.set(key, controller);
    const generation = current.current.seekGeneration;
    const requestId = ++requestSequence.current;
    cameraUpdate(key, { status: "loading", requestId });
    const timeout = window.setTimeout(() => {
      if (controller.signal.aborted || current.current.cameras[key]?.requestId !== requestId) return;
      controller.abort();
      cameraUpdate(key, { status: "error", requestId, error: "녹화 조회가 지연되고 있습니다. 다시 시도해 주세요." });
    }, 15_000);
    try {
      let result = await playbackApi.resolve(key, atMs, controller.signal);
      if (controller.signal.aborted || generation !== current.current.seekGeneration || !keys.current.includes(key)) return;
      const continuationAt = continuationSeekAt(result, continuationFromMs);
      if (continuationAt !== undefined) {
        result = await playbackApi.resolve(key, continuationAt, controller.signal);
        if (controller.signal.aborted || generation !== current.current.seekGeneration || !keys.current.includes(key)) return;
      }
      cameraUpdate(key, {
        status: result.status === "found" && result.media ? "loading" : result.status === "found" ? "error" : result.status,
        requestId, media: result.media, nextAtMs: result.nextAtMs, previousAtMs: result.previousAtMs, continuationFromMs,
        error: result.status === "found" && !result.media ? "녹화 정보를 읽을 수 없습니다." : undefined,
      });
    } catch {
      if (controller.signal.aborted || generation !== current.current.seekGeneration) return;
      cameraUpdate(key, { status: "error", requestId, error: "녹화를 불러오지 못했습니다." });
    } finally {
      window.clearTimeout(timeout);
      if (requests.current.get(key) === controller) requests.current.delete(key);
    }
  }, [cameraUpdate]);

  const seek = useCallback((atMs: number, options?: { play?: boolean }) => {
    if (!Number.isFinite(atMs)) return;
    const previous = current.current;
    pauseVideos();
    requests.current.forEach((request) => request.abort());
    preparation.current = true;
    update({ mode: "playback", playheadMs: Math.round(atMs), hasSelection: true,
      playing: options?.play ?? (previous.mode === "live" ? true : previous.playing),
      seekGeneration: previous.seekGeneration + 1, cameras: {},
    });
    for (const key of keys.current) void resolveCamera(key, atMs);
  }, [pauseVideos, resolveCamera, update]);

  const keySignature = JSON.stringify([...new Set(cameraKeys)].sort());
  useEffect(() => {
    const nextKeys = JSON.parse(keySignature) as string[];
    keys.current = nextKeys;
    const retained = { ...current.current.cameras };
    for (const key of Object.keys(retained)) {
      if (nextKeys.includes(key)) continue;
      requests.current.get(key)?.abort();
      requests.current.delete(key);
      videos.current.get(key)?.pause();
      videos.current.delete(key);
      delete retained[key];
    }
    update({ cameras: retained });
    if (current.current.mode !== "playback" || !current.current.hasSelection) return;
    for (const key of nextKeys) {
      if (!retained[key] || requests.current.get(key)?.signal.aborted) void resolveCamera(key, current.current.playheadMs);
    }
  }, [keySignature, resolveCamera, update]);

  const attachVideo = useCallback((key: string, video: HTMLVideoElement) => {
    videos.current.set(key, video);
    const camera = current.current.cameras[key];
    if (camera?.media && ["ready", "playing"].includes(camera.status)) cameraUpdate(key, { ...camera, status: "loading" });
    return () => {
      video.pause();
      if (videos.current.get(key) === video) videos.current.delete(key);
    };
  }, [cameraUpdate]);
  const videoReady = useCallback((key: string, requestId: number) => {
    const camera = current.current.cameras[key];
    if (camera?.requestId === requestId && camera.status === "loading") cameraUpdate(key, { ...camera, status: "ready" });
  }, [cameraUpdate]);
  const videoError = useCallback((key: string, requestId: number, message: string) => {
    const camera = current.current.cameras[key];
    if (camera?.requestId !== requestId) return;
    videos.current.get(key)?.pause();
    cameraUpdate(key, { ...camera, status: "error", error: message });
  }, [cameraUpdate]);
  const videoEnded = useCallback((key: string, requestId: number) => {
    const camera = current.current.cameras[key];
    if (camera?.requestId !== requestId || !camera.media || !current.current.playing) return;
    const atMs = Math.max(current.current.playheadMs, camera.media.endMs);
    const anotherPlayable = keys.current.some((otherKey) => {
      const other = current.current.cameras[otherKey];
      return otherKey !== key && videos.current.has(otherKey) && other?.media
        && ["ready", "playing"].includes(other.status) && containsPlaybackTime(other.media, current.current.playheadMs);
    });
    if (!anotherPlayable) update({ playheadMs: atMs });
    void resolveCamera(key, atMs, camera.media.endMs);
  }, [resolveCamera, update]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      const now = current.current;
      if (now.mode === "live") { update({ playheadMs: Date.now() }); return; }
      if (!now.hasSelection) return;
      if (preparation.current) {
        const waiting = keys.current.some((key) => {
          const camera = now.cameras[key];
          return !camera || (camera.status === "loading" && (!camera.media || videos.current.has(key)));
        });
        if (waiting) { pauseVideos(); return; }
        preparation.current = false;
      }
      let atMs = now.playheadMs;
      if (now.playing) {
        // The shared cursor follows decoded media, so buffering/all-gap states freeze it.
        for (const [key, video] of videos.current) {
          const camera = now.cameras[key];
          if (!camera?.media || camera.media.startMs > now.playheadMs || !["ready", "playing"].includes(camera.status) || video.paused || video.seeking || video.readyState < 2) continue;
          const presentedAtMs = Math.min(camera.media.endMs, absoluteTimeAt(camera.media, video.currentTime));
          atMs = Math.max(atMs, presentedAtMs);
        }
        const readyMedia = [...videos.current.keys()].flatMap((key) => {
          const camera = now.cameras[key];
          return camera?.media && ["ready", "playing"].includes(camera.status) ? [camera.media] : [];
        });
        atMs = resumeBoundaryClock(atMs, readyMedia);
        if (atMs !== now.playheadMs) update({ playheadMs: atMs });
      }
      const pollTime = Date.now();
      for (const key of keys.current) {
        const camera = current.current.cameras[key];
        if (!camera) continue;
        const video = videos.current.get(key);
        if (camera.media && ["ready", "playing"].includes(camera.status)) {
          if (now.playing && atMs >= camera.media.endMs) {
            video?.pause();
            void resolveCamera(key, atMs, camera.media.endMs);
            continue;
          }
          if (!video) continue;
          video.playbackRate = now.rate;
          video.volume = now.volume;
          video.muted = now.muted || !!mutedVideos.current.get(key);
          if (!now.playing) { video.pause(); continue; }
          // A camera whose next file starts slightly later waits for the other tiles' clock.
          if (atMs < camera.media.startMs) { video.pause(); continue; }
          if (containsPlaybackTime(camera.media, atMs) && !video.seeking && Math.abs(absoluteTimeAt(camera.media, video.currentTime) - atMs) > 800) {
            video.currentTime = mediaTimeAt(camera.media, atMs);
          }
          if (video.paused && !playPending.current.has(video)) {
            playPending.current.add(video);
            void video.play().then(() => {
              if (!current.current.playing || current.current.mode !== "playback") video.pause();
            }).catch((error: unknown) => {
              if (error instanceof DOMException && error.name === "AbortError") return;
              videoError(key, camera.requestId, "재생을 시작하지 못했습니다. 다시 시도해 주세요.");
            }).finally(() => playPending.current.delete(video));
          }
        } else if (now.playing && !requests.current.has(key)) {
          const nextReached = camera.status === "gap" && camera.nextAtMs !== undefined && atMs >= camera.nextAtMs;
          const edgePoll = camera.status === "edge_wait" && pollTime - (lastPoll.current.get(key) ?? 0) >= 2000;
          if (nextReached || edgePoll) {
            lastPoll.current.set(key, pollTime);
            void resolveCamera(key, atMs, camera.continuationFromMs);
          }
        }
      }
    }, 100);
    return () => window.clearInterval(timer);
  }, [pauseVideos, resolveCamera, update, videoError]);

  useEffect(() => () => {
    requests.current.forEach((request) => request.abort());
    videos.current.forEach((video) => video.pause());
  }, []);

  const pause = useCallback(() => { pauseVideos(); update({ playing: false }); }, [pauseVideos, update]);
  const play = useCallback(() => {
    if (current.current.mode === "playback" && current.current.hasSelection) update({ playing: true });
  }, [update]);
  const togglePlaying = useCallback(() => { if (current.current.playing) pause(); else play(); }, [pause, play]);
  const step = useCallback((seconds: number) => {
    if (current.current.mode === "playback" && current.current.hasSelection) seek(current.current.playheadMs + seconds * 1000);
  }, [seek]);
  const setRate = useCallback((rate: number) => { if (Number.isFinite(rate)) update({ rate: Math.max(0.25, Math.min(4, rate)) }); }, [update]);
  const setVolume = useCallback((volume: number) => {
    if (!Number.isFinite(volume)) return;
    const value = Math.max(0, Math.min(1, volume));
    videos.current.forEach((video, key) => { video.volume = value; video.muted = value === 0 || !!mutedVideos.current.get(key); });
    update({ volume: value, muted: value === 0 });
  }, [update]);
  const setMuted = useCallback((muted: boolean) => {
    videos.current.forEach((video, key) => { video.muted = muted || !!mutedVideos.current.get(key); });
    update({ muted });
  }, [update]);
  const goLive = useCallback(() => {
    pauseVideos();
    requests.current.forEach((request) => request.abort());
    preparation.current = false;
    update({ mode: "live", playing: false, hasSelection: false, playheadMs: Date.now(), cameras: {}, seekGeneration: current.current.seekGeneration + 1 });
  }, [pauseVideos, update]);
  const retry = useCallback((key?: string) => {
    if (current.current.mode !== "playback" || !current.current.hasSelection) return;
    for (const target of key ? [key] : keys.current) void resolveCamera(target, current.current.playheadMs);
  }, [resolveCamera]);
  const nextRecording = useCallback((key?: string) => {
    const candidates = (key ? [key] : keys.current).map((target) => current.current.cameras[target]?.nextAtMs)
      .filter((at): at is number => at !== undefined && at > current.current.playheadMs);
    if (candidates.length) seek(Math.min(...candidates));
  }, [seek]);
  const currentTime = useCallback(() => current.current.playheadMs, []);
  const setVideoMuted = useCallback((key: string, muted: boolean) => {
    mutedVideos.current.set(key, muted);
    const video = videos.current.get(key);
    if (video) video.muted = current.current.muted || muted;
  }, []);

  return { ...state, seek, play, pause, togglePlaying, step, setRate, setVolume, setMuted, goLive, retry, nextRecording,
    attachVideo, videoReady, videoError, videoEnded, currentTime, setVideoMuted };
}
