import { useEffect, useMemo, useState } from "react";
import type { Camera } from "../app/api";
import { useCameras, useLayouts } from "../app/queries";
import { playbackStreamCandidates } from "../components/live/streamSelection";
import { useMseStream } from "../components/live/useMseStream";
import { resolveViewerLayout, viewerRect } from "../components/viewer/viewerLayout";

export function ViewerPage() {
  const camerasQuery = useCameras();
  const layoutsQuery = useLayouts();
  const cameras = useMemo(
    () => camerasQuery.data?.filter((camera) => camera.enabled) ?? [],
    [camerasQuery.data],
  );
  const layout = useMemo(
    () => resolveViewerLayout(cameras, layoutsQuery.data ?? []),
    [cameras, layoutsQuery.data],
  );
  const cameraByStream = useMemo(
    () => new Map(cameras.map((camera) => [camera.streamName, camera])),
    [cameras],
  );
  const [focusedStream, setFocusedStream] = useState<string | null>(null);
  const focusedCamera = focusedStream ? cameraByStream.get(focusedStream) : undefined;

  useEffect(() => {
    if (!focusedStream) return;
    if (!focusedCamera) setFocusedStream(null);
  }, [focusedCamera, focusedStream]);

  useEffect(() => {
    if (!focusedStream) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setFocusedStream(null);
    };
    window.addEventListener("keydown", closeOnEscape);
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [focusedStream]);

  if (camerasQuery.isError || layoutsQuery.isError) {
    return <main className="viewer-app viewer-message" role="alert">카메라 배치를 불러오지 못했습니다.</main>;
  }

  if (!camerasQuery.isSuccess || !layoutsQuery.isSuccess) {
    return <main className="viewer-app viewer-message" role="status">카메라 연결 준비 중...</main>;
  }

  if (cameras.length === 0) {
    return <main className="viewer-app viewer-message" role="status">표시할 카메라가 없습니다.</main>;
  }

  return (
    <main className="viewer-app" aria-label="CCTV 전용 뷰어">
      {focusedCamera ? (
        <section className="viewer-focus" aria-label={`${focusedCamera.name} 집중 보기`}>
          <ViewerVideo camera={focusedCamera} focused />
          <button
            className="viewer-focus-close"
            type="button"
            aria-label="집중 보기 닫기"
            onClick={() => setFocusedStream(null)}
          >
            ×
          </button>
        </section>
      ) : (
        <section className="viewer-grid" aria-label="카메라 전체 배치">
          {layout.items.map((item) => {
            const camera = cameraByStream.get(item.i);
            if (!camera) return null;
            const rect = viewerRect(item, layout.cols, layout.rows);
            return (
              <article
                key={camera.streamName}
                className="viewer-tile"
                style={{
                  left: `${rect.left}%`,
                  top: `${rect.top}%`,
                  width: `${rect.width}%`,
                  height: `${rect.height}%`,
                }}
                aria-label={`${camera.name} 라이브`}
                onClick={() => setFocusedStream(camera.streamName)}
              >
                <ViewerVideo camera={camera} />
              </article>
            );
          })}
        </section>
      )}
    </main>
  );
}

function ViewerVideo({ camera, focused = false }: { camera: Camera; focused?: boolean }) {
  const playback = useMseStream(playbackStreamCandidates(camera, focused));
  const status = playback.phase === "playing" ? "재생 중" : "연결 중";

  return (
    <div
      className="viewer-video-frame"
      data-phase={playback.phase}
      data-transport={playback.transport}
    >
      <video
        ref={playback.videoRef}
        className="viewer-video"
        autoPlay
        muted
        playsInline
        disablePictureInPicture
      />
      {!playback.connected && <div className="viewer-connecting">영상 연결 중...</div>}
      <div className="viewer-tile-label">
        <span className={playback.connected ? "viewer-status viewer-status-online" : "viewer-status"} />
        <strong>{camera.name}</strong>
        <span className="viewer-sr-only">{status}</span>
      </div>
    </div>
  );
}
