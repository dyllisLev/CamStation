import { useMemo, useRef, useState } from "react";
import type { KeyboardEvent, PointerEvent, WheelEvent } from "react";

import "./playback-controls.css";

export type PlaybackTimelineRange = {
  readonly startMs: number;
  readonly endMs: number;
};

export type PlaybackTimelineViewport = {
  readonly fromMs: number;
  readonly toMs: number;
};

export type PlaybackTimelineProps = {
  readonly ranges: readonly PlaybackTimelineRange[];
  readonly aggregateRanges?: readonly PlaybackTimelineRange[];
  readonly viewport: PlaybackTimelineViewport;
  readonly playheadMs?: number | null;
  readonly onSeek: (atMs: number) => void;
  readonly onViewportChange: (viewport: PlaybackTimelineViewport) => void;
  readonly selectedLabel?: string;
  readonly aggregateLabel?: string;
  readonly disabled?: boolean;
  readonly className?: string;
  readonly ariaLabel?: string;
};

type PointerGesture =
  | {
      readonly kind: "cursor";
      readonly pointerId: number;
      readonly track: HTMLDivElement;
      candidateMs: number;
    }
  | {
      readonly kind: "pan";
      readonly pointerId: number;
      readonly track: HTMLDivElement;
      readonly startClientX: number;
      readonly startViewport: PlaybackTimelineViewport;
      moved: boolean;
    };

const KST = new Intl.DateTimeFormat("ko-KR", {
  timeZone: "Asia/Seoul",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

const KST_FULL = new Intl.DateTimeFormat("ko-KR", {
  timeZone: "Asia/Seoul",
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

const MIN_VIEWPORT_MS = 10_000;
const MAX_VIEWPORT_MS = 31 * 24 * 60 * 60 * 1_000;

export function PlaybackTimeline({
  ranges,
  aggregateRanges,
  viewport,
  playheadMs,
  onSeek,
  onViewportChange,
  selectedLabel = "선택 카메라",
  aggregateLabel = "전체 카메라 합집합",
  disabled = false,
  className,
  ariaLabel = "녹화 재생 시간축",
}: PlaybackTimelineProps) {
  const gestureRef = useRef<PointerGesture | null>(null);
  const [candidateMs, setCandidateMs] = useState<number | null>(null);
  const durationMs = validDuration(viewport);
  const ticks = useMemo(() => makeTicks(viewport, 7), [viewport]);
  const visiblePlayhead = candidateMs ?? playheadMs;

  function beginPan(event: PointerEvent<HTMLDivElement>) {
    if (disabled || event.button !== 0 || durationMs <= 0) return;
    event.currentTarget.focus();
    event.currentTarget.setPointerCapture(event.pointerId);
    gestureRef.current = {
      kind: "pan",
      pointerId: event.pointerId,
      track: event.currentTarget,
      startClientX: event.clientX,
      startViewport: viewport,
      moved: false,
    };
  }

  function beginCursorDrag(event: PointerEvent<HTMLButtonElement>) {
    if (disabled || event.button !== 0 || durationMs <= 0) return;
    event.preventDefault();
    event.stopPropagation();
    const track = event.currentTarget.parentElement;
    if (!(track instanceof HTMLDivElement)) return;
    track.focus();
    track.setPointerCapture(event.pointerId);
    const atMs = timeAtClientX(event.clientX, track, viewport);
    gestureRef.current = { kind: "cursor", pointerId: event.pointerId, track, candidateMs: atMs };
    setCandidateMs(atMs);
  }

  function updatePointer(event: PointerEvent<HTMLDivElement>) {
    const gesture = gestureRef.current;
    if (!gesture || gesture.pointerId !== event.pointerId) return;

    if (gesture.kind === "cursor") {
      const next = timeAtClientX(event.clientX, gesture.track, viewport);
      gesture.candidateMs = next;
      setCandidateMs(next);
      return;
    }

    const width = gesture.track.getBoundingClientRect().width;
    if (width <= 0) return;
    const deltaX = event.clientX - gesture.startClientX;
    if (Math.abs(deltaX) >= 3) gesture.moved = true;
    if (!gesture.moved) return;
    const span = validDuration(gesture.startViewport);
    const shiftMs = -(deltaX / width) * span;
    onViewportChange({
      fromMs: gesture.startViewport.fromMs + shiftMs,
      toMs: gesture.startViewport.toMs + shiftMs,
    });
  }

  function finishPointer(event: PointerEvent<HTMLDivElement>) {
    const gesture = gestureRef.current;
    if (!gesture || gesture.pointerId !== event.pointerId) return;
    gestureRef.current = null;
    if (gesture.track.hasPointerCapture(event.pointerId)) gesture.track.releasePointerCapture(event.pointerId);

    if (gesture.kind === "cursor") {
      setCandidateMs(null);
      onSeek(gesture.candidateMs);
      return;
    }

    if (!gesture.moved) onSeek(timeAtClientX(event.clientX, gesture.track, gesture.startViewport));
  }

  function cancelPointer(event: PointerEvent<HTMLDivElement>) {
    const gesture = gestureRef.current;
    if (!gesture || gesture.pointerId !== event.pointerId) return;
    gestureRef.current = null;
    setCandidateMs(null);
  }

  function zoomViewport(event: WheelEvent<HTMLDivElement>) {
    if (disabled || durationMs <= 0) return;
    event.preventDefault();
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const anchorRatio = clamp((event.clientX - rect.left) / rect.width, 0, 1);
    const nextDuration = clamp(durationMs * Math.exp(event.deltaY * 0.0015), MIN_VIEWPORT_MS, MAX_VIEWPORT_MS);
    const anchorMs = viewport.fromMs + durationMs * anchorRatio;
    onViewportChange({
      fromMs: anchorMs - nextDuration * anchorRatio,
      toMs: anchorMs + nextDuration * (1 - anchorRatio),
    });
  }

  function handleKeyboard(event: KeyboardEvent<HTMLDivElement>) {
    if (disabled || durationMs <= 0) return;
    if (event.key === "Escape" && candidateMs !== null) {
      gestureRef.current = null;
      setCandidateMs(null);
      event.preventDefault();
      return;
    }

    const current = clamp(playheadMs ?? midpoint(viewport), viewport.fromMs, viewport.toMs);
    let next: number | undefined;
    if (event.key === "ArrowLeft") next = current - 10_000;
    if (event.key === "ArrowRight") next = current + 10_000;
    if (event.key === "PageUp") next = current - durationMs / 10;
    if (event.key === "PageDown") next = current + durationMs / 10;
    if (event.key === "Home") next = viewport.fromMs;
    if (event.key === "End") next = viewport.toMs;
    if (next === undefined) return;
    event.preventDefault();
    onSeek(clamp(next, viewport.fromMs, viewport.toMs));
  }

  const rootClassName = ["playback-timeline", className].filter(Boolean).join(" ");
  const cursorPercent = visiblePlayhead == null ? null : percentAt(visiblePlayhead, viewport);

  return (
    <section className={rootClassName} aria-label={ariaLabel}>
      <TimelineTrack
        label={selectedLabel}
        ranges={ranges}
        viewport={viewport}
        cursorPercent={cursorPercent}
        cursorLabel={visiblePlayhead == null ? undefined : formatKst(visiblePlayhead)}
        disabled={disabled}
        onPointerDown={beginPan}
        onPointerMove={updatePointer}
        onPointerUp={finishPointer}
        onPointerCancel={cancelPointer}
        onCursorPointerDown={beginCursorDrag}
        onWheel={zoomViewport}
        onKeyDown={handleKeyboard}
        playheadMs={playheadMs}
      />
      {aggregateRanges !== undefined && (
        <TimelineTrack
          label={aggregateLabel}
          ranges={aggregateRanges}
          viewport={viewport}
          cursorPercent={cursorPercent}
          cursorLabel={visiblePlayhead == null ? undefined : formatKst(visiblePlayhead)}
          disabled={disabled}
          aggregate
          onPointerDown={beginPan}
          onPointerMove={updatePointer}
          onPointerUp={finishPointer}
          onPointerCancel={cancelPointer}
          onCursorPointerDown={beginCursorDrag}
          onWheel={zoomViewport}
          onKeyDown={handleKeyboard}
          playheadMs={playheadMs}
        />
      )}
      <div className="playback-timeline__ticks" aria-hidden="true">
        <span className="playback-timeline__ticks-label">KST</span>
        <div className="playback-timeline__ticks-scale">
          {ticks.map((tick) => (
            <span key={tick.atMs} style={{ left: `${tick.percent}%` }} title={formatKst(tick.atMs)}>
              {tick.label}
            </span>
          ))}
        </div>
      </div>
    </section>
  );
}

type TimelineTrackProps = {
  readonly label: string;
  readonly ranges: readonly PlaybackTimelineRange[];
  readonly viewport: PlaybackTimelineViewport;
  readonly cursorPercent: number | null;
  readonly cursorLabel?: string;
  readonly disabled: boolean;
  readonly aggregate?: boolean;
  readonly playheadMs?: number | null;
  readonly onPointerDown: (event: PointerEvent<HTMLDivElement>) => void;
  readonly onPointerMove: (event: PointerEvent<HTMLDivElement>) => void;
  readonly onPointerUp: (event: PointerEvent<HTMLDivElement>) => void;
  readonly onPointerCancel: (event: PointerEvent<HTMLDivElement>) => void;
  readonly onCursorPointerDown: (event: PointerEvent<HTMLButtonElement>) => void;
  readonly onWheel: (event: WheelEvent<HTMLDivElement>) => void;
  readonly onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => void;
};

function TimelineTrack({
  label,
  ranges,
  viewport,
  cursorPercent,
  cursorLabel,
  disabled,
  aggregate = false,
  playheadMs,
  onPointerDown,
  onPointerMove,
  onPointerUp,
  onPointerCancel,
  onCursorPointerDown,
  onWheel,
  onKeyDown,
}: TimelineTrackProps) {
  return (
    <div className="playback-timeline__row">
      <div className="playback-timeline__label" title={label}>{label}</div>
      <div
        className={["playback-timeline__track", aggregate && "playback-timeline__track--aggregate"].filter(Boolean).join(" ")}
        role="slider"
        tabIndex={disabled ? -1 : 0}
        aria-label={`${label} 재생 위치`}
        aria-disabled={disabled}
        aria-valuemin={Math.round(viewport.fromMs)}
        aria-valuemax={Math.round(viewport.toMs)}
        aria-valuenow={playheadMs == null ? undefined : Math.round(clamp(playheadMs, viewport.fromMs, viewport.toMs))}
        aria-valuetext={playheadMs == null ? "재생 위치 없음" : formatKst(playheadMs)}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerCancel}
        onLostPointerCapture={onPointerCancel}
        onWheel={onWheel}
        onKeyDown={onKeyDown}
      >
        {ranges.map((range, index) => {
          const clipped = clipRange(range, viewport);
          if (!clipped) return null;
          return (
            <span
              className="playback-timeline__range"
              key={`${range.startMs}-${range.endMs}-${index}`}
              style={{ left: `${clipped.left}%`, width: `${clipped.width}%` }}
            />
          );
        })}
        {cursorPercent !== null && cursorPercent >= 0 && cursorPercent <= 100 && (
          <button
            type="button"
            className="playback-timeline__cursor-hit"
            style={{ left: `${cursorPercent}%` }}
            aria-label={cursorLabel ? `재생 커서 ${cursorLabel}` : "재생 커서"}
            tabIndex={-1}
            disabled={disabled}
            onPointerDown={onCursorPointerDown}
          >
            <span className="playback-timeline__cursor" />
          </button>
        )}
      </div>
    </div>
  );
}

function validDuration(viewport: PlaybackTimelineViewport) {
  const duration = viewport.toMs - viewport.fromMs;
  return Number.isFinite(duration) && duration > 0 ? duration : 0;
}

function midpoint(viewport: PlaybackTimelineViewport) {
  return viewport.fromMs + validDuration(viewport) / 2;
}

function timeAtClientX(clientX: number, track: HTMLElement, viewport: PlaybackTimelineViewport) {
  const rect = track.getBoundingClientRect();
  const ratio = rect.width > 0 ? clamp((clientX - rect.left) / rect.width, 0, 1) : 0;
  return viewport.fromMs + validDuration(viewport) * ratio;
}

function percentAt(atMs: number, viewport: PlaybackTimelineViewport) {
  const duration = validDuration(viewport);
  return duration > 0 ? ((atMs - viewport.fromMs) / duration) * 100 : -1;
}

function clipRange(range: PlaybackTimelineRange, viewport: PlaybackTimelineViewport) {
  if (!Number.isFinite(range.startMs) || !Number.isFinite(range.endMs) || range.endMs <= range.startMs) return null;
  const startMs = Math.max(range.startMs, viewport.fromMs);
  const endMs = Math.min(range.endMs, viewport.toMs);
  if (endMs <= startMs) return null;
  return {
    left: percentAt(startMs, viewport),
    width: Math.max(percentAt(endMs, viewport) - percentAt(startMs, viewport), 0.14),
  };
}

function makeTicks(viewport: PlaybackTimelineViewport, count: number) {
  const duration = validDuration(viewport);
  if (duration <= 0) return [];
  return Array.from({ length: count }, (_, index) => {
    const ratio = index / (count - 1);
    const atMs = viewport.fromMs + duration * ratio;
    return { atMs, percent: ratio * 100, label: KST.format(new Date(atMs)) };
  });
}

function formatKst(atMs: number) {
  return `${KST_FULL.format(new Date(atMs))} KST`;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}
