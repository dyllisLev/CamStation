import { useCallback, useRef } from "react";
import type { MouseEvent, WheelEvent } from "react";
import { RecordedVideo } from "./RecordedVideo";
import type { PlaybackWorkspace } from "./usePlaybackWorkspace";
import "./recorded-video-viewport.css";

export type RecordedVideoViewportState = {
  scale: number;
  tx: number;
  ty: number;
};

const DEFAULT_RECORDED_VIDEO_VIEWPORT: RecordedVideoViewportState = { scale: 1, tx: 0, ty: 0 };

export function RecordedVideoViewport({
  workspace,
  cameraKey,
  selected = true,
  viewport,
  onViewportChange,
  showOverlay = true,
  className,
}: {
  workspace: PlaybackWorkspace;
  cameraKey: string;
  selected?: boolean;
  viewport?: RecordedVideoViewportState;
  onViewportChange: (viewport: RecordedVideoViewportState) => void;
  showOverlay?: boolean;
  className?: string;
}) {
  const frameRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ x: number; y: number; tx: number; ty: number } | null>(null);
  const currentViewport = viewport ?? DEFAULT_RECORDED_VIDEO_VIEWPORT;
  const zoomed = currentViewport.scale > 1.001;
  const state = workspace.cameras[cameraKey];

  const applyViewport = useCallback((next: RecordedVideoViewportState) => {
    const frame = frameRef.current;
    if (!frame || next.scale <= 1) {
      onViewportChange(DEFAULT_RECORDED_VIDEO_VIEWPORT);
      return;
    }
    const rect = frame.getBoundingClientRect();
    onViewportChange(clampViewport(next, rect.width, rect.height));
  }, [onViewportChange]);

  const handleWheel = useCallback((event: WheelEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.stopPropagation();
    const frame = frameRef.current;
    if (!frame) return;
    const rect = frame.getBoundingClientRect();
    const nextScale = clamp(currentViewport.scale * (event.deltaY < 0 ? 1.15 : 1 / 1.15), 1, 4);
    if (nextScale === 1) {
      applyViewport(DEFAULT_RECORDED_VIDEO_VIEWPORT);
      return;
    }
    const offsetX = event.clientX - rect.left - rect.width / 2;
    const offsetY = event.clientY - rect.top - rect.height / 2;
    const scaleRatio = nextScale / currentViewport.scale;
    applyViewport({
      scale: nextScale,
      tx: currentViewport.tx * scaleRatio - offsetX * (scaleRatio - 1),
      ty: currentViewport.ty * scaleRatio - offsetY * (scaleRatio - 1),
    });
  }, [applyViewport, currentViewport]);

  const handleMouseDown = useCallback((event: MouseEvent<HTMLDivElement>) => {
    if (!zoomed || event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    dragRef.current = { x: event.clientX, y: event.clientY, tx: currentViewport.tx, ty: currentViewport.ty };

    const handleMove = (moveEvent: globalThis.MouseEvent) => {
      const drag = dragRef.current;
      const frame = frameRef.current;
      if (!drag || !frame) return;
      const rect = frame.getBoundingClientRect();
      onViewportChange(clampViewport({
        scale: currentViewport.scale,
        tx: drag.tx + moveEvent.clientX - drag.x,
        ty: drag.ty + moveEvent.clientY - drag.y,
      }, rect.width, rect.height));
    };

    const handleUp = () => {
      dragRef.current = null;
      window.removeEventListener("mousemove", handleMove);
      window.removeEventListener("mouseup", handleUp);
    };

    window.addEventListener("mousemove", handleMove);
    window.addEventListener("mouseup", handleUp);
  }, [currentViewport, onViewportChange, zoomed]);

  return (
    <div
      ref={frameRef}
      className={["new-live-video-frame", "playback-recorded-video-frame", className].filter(Boolean).join(" ")}
      onWheel={handleWheel}
      onMouseDown={handleMouseDown}
      onDoubleClick={(event) => {
        event.stopPropagation();
        applyViewport(DEFAULT_RECORDED_VIDEO_VIEWPORT);
      }}
    >
      <RecordedVideo
        workspace={workspace}
        cameraKey={cameraKey}
        className="new-live-video"
        muted={!selected || workspace.muted}
        style={{
          transform: `scale(${currentViewport.scale}) translate(${currentViewport.tx / currentViewport.scale}px, ${currentViewport.ty / currentViewport.scale}px)`,
        }}
      />
      {showOverlay && state && !["ready", "playing"].includes(state.status) && (
        <div
          className="new-offline-layer playback-recorded-video-state"
          onDoubleClick={(event) => event.stopPropagation()}
        >
          <span>{playbackStateCopy(state.status, state.error)}</span>
          {state.status === "gap" && state.nextAtMs !== undefined && (
            <button
              type="button"
              className="new-ghost"
              onClick={(event) => {
                event.stopPropagation();
                workspace.nextRecording();
              }}
            >
              다음 녹화
            </button>
          )}
          {state.status === "error" && (
            <button
              type="button"
              className="new-ghost"
              onClick={(event) => {
                event.stopPropagation();
                workspace.retry(cameraKey);
              }}
            >
              다시 시도
            </button>
          )}
        </div>
      )}
      {showOverlay && !state && <div className="new-offline-layer">녹화를 준비하는 중입니다.</div>}
      {zoomed && <div className="new-zoom-badge">{currentViewport.scale.toFixed(1)}x</div>}
    </div>
  );
}

function clampViewport(viewport: RecordedVideoViewportState, width: number, height: number): RecordedVideoViewportState {
  const scale = clamp(viewport.scale, 1, 4);
  if (scale <= 1) return DEFAULT_RECORDED_VIDEO_VIEWPORT;
  return {
    scale,
    tx: clamp(viewport.tx, -((scale - 1) * width) / 2, ((scale - 1) * width) / 2),
    ty: clamp(viewport.ty, -((scale - 1) * height) / 2, ((scale - 1) * height) / 2),
  };
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function playbackStateCopy(status: PlaybackWorkspace["cameras"][string]["status"], error?: string): string {
  if (error) return error;
  switch (status) {
    case "idle":
    case "loading":
      return "녹화를 불러오는 중입니다.";
    case "gap":
      return "이 시각에는 녹화가 없습니다.";
    case "edge_wait":
      return "최근 녹화 조각을 기다리는 중입니다.";
    case "unsupported":
      return "지원하지 않는 녹화 형식입니다.";
    case "error":
      return "녹화를 재생하지 못했습니다.";
    case "ready":
    case "playing":
      return "녹화 재생 중";
  }
}
