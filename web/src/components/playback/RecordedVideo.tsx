import Hls from "hls.js";
import { useEffect, useRef, type VideoHTMLAttributes } from "react";
import { withAppBase } from "../../app/basePath";
import { isPlaybackMediaUrl, mediaTimeAt } from "./playbackClock";
import type { PlaybackWorkspace } from "./usePlaybackWorkspace";

export type RecordedVideoProps = Omit<VideoHTMLAttributes<HTMLVideoElement>, "src" | "autoPlay" | "ref"> & {
  workspace: PlaybackWorkspace;
  cameraKey: string;
};

export function RecordedVideo({ workspace, cameraKey, muted = false, ...props }: RecordedVideoProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const { attachVideo, videoReady, videoError, videoEnded, currentTime, setVideoMuted } = workspace;
  const camera = workspace.cameras[cameraKey];
  const media = camera?.media;
  const requestId = camera?.requestId;

  useEffect(() => {
    const video = videoRef.current;
    if (video) return attachVideo(cameraKey, video);
  }, [attachVideo, cameraKey]);

  useEffect(() => { setVideoMuted(cameraKey, muted); }, [cameraKey, muted, setVideoMuted]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !media || requestId === undefined) return;
    if (!isPlaybackMediaUrl(media.url)) {
      videoError(cameraKey, requestId, "녹화 주소를 확인할 수 없습니다.");
      return;
    }
    let disposed = false;
    let prepared = false;
    let target = mediaTimeAt(media, currentTime());
    let hls: Hls | undefined;
    const fail = () => { if (!disposed) videoError(cameraKey, requestId, "녹화 영상을 재생하지 못했습니다."); };
    const prepare = () => {
      if (disposed || prepared || video.readyState < 1) return;
      // A remounted focus tile joins the workspace at its current absolute position.
      target = mediaTimeAt(media, currentTime());
      if (Number.isFinite(video.duration)) target = Math.min(target, Math.max(0, video.duration - 0.001));
      if (Math.abs(video.currentTime - target) > 0.05) {
        try { video.currentTime = target; } catch { return; }
      }
      if (!video.seeking && video.readyState >= 2 && Math.abs(video.currentTime - target) < 0.25) {
        prepared = true;
        videoReady(cameraKey, requestId);
      }
    };
    const ended = () => { if (!disposed) videoEnded(cameraKey, requestId); };
    video.pause();
    video.addEventListener("loadedmetadata", prepare);
    video.addEventListener("loadeddata", prepare);
    video.addEventListener("canplay", prepare);
    video.addEventListener("seeked", prepare);
    video.addEventListener("error", fail);
    video.addEventListener("ended", ended);
    const url = withAppBase(media.url);
    if (media.kind === "hls" && Hls.isSupported()) {
      hls = new Hls({ startPosition: target, lowLatencyMode: false, maxLiveSyncPlaybackRate: 1 });
      hls.on(Hls.Events.ERROR, (_event, data) => { if (data.fatal) fail(); });
      hls.loadSource(url);
      hls.attachMedia(video);
    } else if (media.kind === "file" || video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url;
      video.load();
    } else {
      videoError(cameraKey, requestId, "이 환경에서는 녹화 형식을 재생할 수 없습니다.");
    }
    // A failed/never-ready input must not hold every other tile at the initial seek barrier.
    const timeout = window.setTimeout(() => { if (!prepared) fail(); }, 15_000);
    return () => {
      disposed = true;
      window.clearTimeout(timeout);
      video.removeEventListener("loadedmetadata", prepare);
      video.removeEventListener("loadeddata", prepare);
      video.removeEventListener("canplay", prepare);
      video.removeEventListener("seeked", prepare);
      video.removeEventListener("error", fail);
      video.removeEventListener("ended", ended);
      video.pause();
      hls?.destroy();
      video.removeAttribute("src");
      video.load();
    };
  }, [cameraKey, currentTime, media, requestId, videoEnded, videoError, videoReady]);

  return <video {...props} ref={videoRef} playsInline preload="auto" muted={workspace.muted || muted} data-playback-camera={cameraKey} />;
}
