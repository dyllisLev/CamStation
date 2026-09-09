import { CalendarDays, Clock3, ListVideo, RefreshCw, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { usePlaybackCameras, usePlaybackSegments, usePlaybackTimeline } from "../../app/playbackQueries";
import type { PlaybackCamera, PlaybackSegment } from "../../app/playbackTypes";
import type { Camera } from "../../app/cameraTypes";
import { useCameras } from "../../app/queries";
import { isViewerMode } from "../../app/viewerMode";
import { Button } from "../ui/button";
import {
  hasViewerFullscreenBridge,
  requestViewerFullscreen,
  subscribeViewerFullscreen,
} from "../live/viewerBridge";
import { PlaybackControls } from "./PlaybackControls";
import { PlaybackTimeline, type PlaybackTimelineViewport } from "./PlaybackTimeline";
import {
  RecordedVideoViewport,
  type RecordedVideoViewportState,
} from "./RecordedVideoViewport";
import { usePlaybackWorkspace } from "./usePlaybackWorkspace";

import "./recording-browser.css";

const DAY_MS = 24 * 60 * 60 * 1000;
const NO_CAMERAS: readonly PlaybackCamera[] = [];
const NO_REGISTERED_CAMERAS: readonly Camera[] = [];
const DEFAULT_VIDEO_VIEWPORT: RecordedVideoViewportState = { scale: 1, tx: 0, ty: 0 };

export type RecordingBrowserRequest = {
  readonly requestId: string;
  readonly segmentId: number;
  readonly cameraId: number;
  readonly cameraKeyHint: string;
  readonly atMs: number;
};

type RecordingBrowserWorkspaceProps = {
  readonly active?: boolean;
  readonly request?: RecordingBrowserRequest;
};

export function RecordingBrowserWorkspace({ active = true, request }: RecordingBrowserWorkspaceProps) {
  const [browseDate, setBrowseDate] = useState(() => formatKstDate(Date.now()));
  const [timeDraft, setTimeDraft] = useState(() => formatKstTime(Date.now()));
  const dayRange = useMemo(() => kstDayRange(browseDate), [browseDate]);
  const [viewport, setViewport] = useState<PlaybackTimelineViewport>(dayRange);
  const [selectedCameraKey, setSelectedCameraKey] = useState("");
  const [selectedSegmentId, setSelectedSegmentId] = useState<number | null>(null);
  const [listOpen, setListOpen] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [pendingSeek, setPendingSeek] = useState<{ readonly cameraKey: string; readonly atMs: number }>();
  const [videoViewport, setVideoViewport] = useState<RecordedVideoViewportState>(DEFAULT_VIDEO_VIEWPORT);
  const rootRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const handledRequestRef = useRef<string | undefined>(undefined);
  const preserveRequestedSelectionRef = useRef(false);
  const viewerMode = isViewerMode(window.location.search);
  const nativeFullscreen = viewerMode && hasViewerFullscreenBridge();

  const camerasQuery = usePlaybackCameras();
  const registeredCamerasQuery = useCameras();
  const cameras = camerasQuery.data?.cameras ?? NO_CAMERAS;
  const registeredCameras = registeredCamerasQuery.data ?? NO_REGISTERED_CAMERAS;
  const selectedCamera = cameras.find((camera) => camera.cameraKey === selectedCameraKey);
  const workspace = usePlaybackWorkspace({
    cameraKeys: selectedCameraKey ? [selectedCameraKey] : [],
    initialMode: "playback",
  });
  const { pause, seek, setMuted } = workspace;
  const timelineQuery = usePlaybackTimeline(selectedCameraKey, viewport.fromMs, viewport.toMs, active);
  const segmentsQuery = usePlaybackSegments(selectedCameraKey, dayRange.fromMs, dayRange.toMs, active);
  const segments = useMemo(
    () => segmentsQuery.data?.pages.flatMap((page) => page.segments) ?? [],
    [segmentsQuery.data?.pages],
  );

  useEffect(() => {
    if (active) return;
    pause();
    setMuted(true);
  }, [active, pause, setMuted]);

  useEffect(() => {
    if (!request || handledRequestRef.current === request.requestId || cameras.length === 0) return;
    const requestedCamera = cameraForRequest(cameras, registeredCameras, request);
    if (!requestedCamera) return;
    handledRequestRef.current = request.requestId;
    preserveRequestedSelectionRef.current = true;
    setPendingSeek({ cameraKey: requestedCamera.cameraKey, atMs: request.atMs });
    setSelectedCameraKey(requestedCamera.cameraKey);
    setSelectedSegmentId(request.segmentId);
    setBrowseDate(formatKstDate(request.atMs));
    setTimeDraft(formatKstTime(request.atMs));
    setListOpen(false);
  }, [cameras, registeredCameras, request]);

  useEffect(() => {
    if (!pendingSeek || pendingSeek.cameraKey !== selectedCameraKey) return;
    seek(pendingSeek.atMs, { play: true });
    setPendingSeek(undefined);
  }, [pendingSeek, seek, selectedCameraKey]);

  useEffect(() => {
    if (selectedCameraKey || request || cameras.length === 0) return;
    setSelectedCameraKey(cameras[0].cameraKey);
  }, [cameras, request, selectedCameraKey]);

  useEffect(() => {
    setViewport(dayRange);
    setVideoViewport(DEFAULT_VIDEO_VIEWPORT);
    if (preserveRequestedSelectionRef.current) preserveRequestedSelectionRef.current = false;
    else setSelectedSegmentId(null);
    if (listRef.current) listRef.current.scrollTop = 0;
  }, [dayRange, selectedCameraKey]);

  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || !listOpen) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      setListOpen(false);
    };
    document.addEventListener("keydown", handleEscape, true);
    return () => document.removeEventListener("keydown", handleEscape, true);
  }, [listOpen]);

  useEffect(() => {
    if (nativeFullscreen) return subscribeViewerFullscreen(setFullscreen);
    const handleFullscreen = () => setFullscreen(Boolean(document.fullscreenElement));
    document.addEventListener("fullscreenchange", handleFullscreen);
    return () => document.removeEventListener("fullscreenchange", handleFullscreen);
  }, [nativeFullscreen]);

  const toggleFullscreen = useCallback(async () => {
    if (nativeFullscreen) {
      await requestViewerFullscreen(!fullscreen);
      return;
    }
    if (document.fullscreenElement) {
      await document.exitFullscreen();
      return;
    }
    await rootRef.current?.requestFullscreen();
  }, [fullscreen, nativeFullscreen]);

  const chooseCamera = (cameraKey: string) => {
    setSelectedCameraKey(cameraKey);
    setListOpen(false);
  };

  const chooseDate = (date: string) => {
    if (date) setBrowseDate(date);
  };

  const seekDraft = () => {
    const atMs = parseKstDateTime(browseDate, timeDraft);
    if (atMs !== null && selectedCameraKey) seek(atMs);
  };

  const seekTimeline = (atMs: number) => {
    setTimeDraft(formatKstTime(atMs));
    seek(atMs);
  };

  const chooseSegment = (segment: PlaybackSegment) => {
    setSelectedSegmentId(segment.segmentId);
    setTimeDraft(formatKstTime(segment.startMs));
    seek(segment.startMs, { play: true });
    setListOpen(false);
  };

  const ranges = timelineQuery.data?.coverage.map((range) => ({
    startMs: range.startMs,
    endMs: range.endMs,
  })) ?? [];
  return (
    <div className="recording-browser" ref={rootRef}>
      <header className="recording-browser__toolbar">
        <h1 className="recording-browser__title">녹화 영상</h1>
        <label className="recording-browser__field recording-browser__camera-field">
          <span>카메라</span>
          <select value={selectedCameraKey} onChange={(event) => chooseCamera(event.target.value)} disabled={cameras.length === 0}>
            {cameras.length === 0 && <option value="">카메라 없음</option>}
            {cameras.map((camera) => (
              <option key={camera.cameraKey} value={camera.cameraKey}>
                {camera.name}{camera.registered ? "" : " · 보관"}
              </option>
            ))}
          </select>
        </label>
        <label className="recording-browser__field">
          <CalendarDays aria-hidden="true" />
          <span className="recording-browser__sr-only">조회 날짜</span>
          <input type="date" value={browseDate} onChange={(event) => chooseDate(event.target.value)} />
        </label>
        <form className="recording-browser__field" onSubmit={(event) => { event.preventDefault(); seekDraft(); }}>
          <Clock3 aria-hidden="true" />
          <span className="recording-browser__sr-only">이동할 KST 시각</span>
          <input type="time" step="1" value={timeDraft} onChange={(event) => setTimeDraft(event.target.value)} />
          <Button type="submit" size="sm" variant="secondary" disabled={!selectedCameraKey}>이동</Button>
        </form>
        <Button className="recording-browser__list-toggle" type="button" size="sm" variant="secondary" onClick={() => setListOpen(true)}>
          <ListVideo size={15} />녹화 목록
        </Button>
      </header>

      <div className="recording-browser__body">
        <main className="recording-browser__player">
          <section className="recording-browser__video-frame" aria-label="녹화 영상 플레이어">
            {selectedCameraKey && workspace.hasSelection ? (
              <RecordedVideoViewport
                className="recording-browser__recorded-viewport"
                workspace={workspace}
                cameraKey={selectedCameraKey}
                viewport={videoViewport}
                onViewportChange={setVideoViewport}
              />
            ) : (
              <div className="recording-browser__empty-video" role="status">
                <ListVideo aria-hidden="true" />
                <strong>재생할 시각을 선택하세요</strong>
                <span>오른쪽 목록이나 하단 시간축에서 녹화 구간을 선택할 수 있습니다.</span>
              </div>
            )}
            <div className="recording-browser__video-meta">
              <span>{selectedCamera?.name ?? "카메라 선택 대기"}</span>
              {workspace.hasSelection && <time>{formatKstTimestamp(workspace.playheadMs)}</time>}
            </div>
          </section>

          <PlaybackControls
            className="recording-browser__controls"
            workspace={workspace}
            fullscreen={fullscreen}
            onToggleFullscreen={() => void toggleFullscreen()}
          />

          <div className="recording-browser__timeline-shell">
            <div className="recording-browser__timeline-heading">
              <span>{formatKstDateLabel(browseDate)}</span>
              <span>밝은 구간은 재생 가능한 녹화입니다.</span>
            </div>
            <PlaybackTimeline
              ranges={ranges}
              viewport={viewport}
              playheadMs={workspace.hasSelection ? workspace.playheadMs : null}
              onSeek={seekTimeline}
              onViewportChange={setViewport}
              selectedLabel={selectedCamera?.name ?? "선택 카메라"}
              disabled={!selectedCameraKey || timelineQuery.isLoading}
            />
            {timelineQuery.error && (
              <QueryMessage tone="error" text="시간축을 불러오지 못했습니다." onRetry={() => void timelineQuery.refetch()} />
            )}
          </div>
        </main>

        {listOpen && (
          <button className="recording-browser__drawer-backdrop" type="button" aria-label="녹화 목록 닫기" onClick={() => setListOpen(false)} />
        )}
        <aside className={listOpen ? "recording-browser__list recording-browser__list--open" : "recording-browser__list"} aria-label="녹화 구간 목록">
          <div className="recording-browser__list-header">
            <div>
              <h2>녹화 구간</h2>
              <span>{segments.length}개 불러옴</span>
            </div>
            <div className="recording-browser__list-actions">
              <button type="button" aria-label="목록 새로고침" title="목록 새로고침" onClick={() => void segmentsQuery.refetch()}>
                <RefreshCw aria-hidden="true" />
              </button>
              <button className="recording-browser__drawer-close" type="button" aria-label="녹화 목록 닫기" onClick={() => setListOpen(false)}>
                <X aria-hidden="true" />
              </button>
            </div>
          </div>
          <div className="recording-browser__list-scroll" ref={listRef}>
            {segmentsQuery.isLoading && <QueryMessage text="녹화 구간을 불러오는 중입니다." />}
            {segmentsQuery.error && (
              <QueryMessage tone="error" text="녹화 구간을 불러오지 못했습니다." onRetry={() => void segmentsQuery.refetch()} />
            )}
            {!segmentsQuery.isLoading && !segmentsQuery.error && segments.length === 0 && (
              <QueryMessage text="선택한 날짜에 재생 가능한 녹화가 없습니다." />
            )}
            {segments.map((segment) => (
              <SegmentButton
                key={String(segment.segmentId) + ":" + segment.mediaId}
                segment={segment}
                selected={segment.segmentId === selectedSegmentId}
                playing={segment.segmentId === selectedSegmentId && workspace.playing}
                onClick={() => chooseSegment(segment)}
              />
            ))}
            {segmentsQuery.hasNextPage && (
              <Button
                className="recording-browser__load-more"
                type="button"
                variant="secondary"
                disabled={segmentsQuery.isFetchingNextPage}
                onClick={() => void segmentsQuery.fetchNextPage()}
              >
                {segmentsQuery.isFetchingNextPage ? "불러오는 중" : "더 보기"}
              </Button>
            )}
          </div>
        </aside>
      </div>

      {camerasQuery.isLoading && <div className="recording-browser__notice" role="status">카메라 목록을 불러오는 중입니다.</div>}
      {camerasQuery.error && (
        <QueryMessage tone="error" text="카메라 목록을 불러오지 못했습니다." onRetry={() => void camerasQuery.refetch()} />
      )}
    </div>
  );
}

function SegmentButton({ segment, selected, playing, onClick }: {
  readonly segment: PlaybackSegment;
  readonly selected: boolean;
  readonly playing: boolean;
  readonly onClick: () => void;
}) {
  return (
    <button
      className={selected ? "recording-browser__segment recording-browser__segment--selected" : "recording-browser__segment"}
      type="button"
      aria-pressed={selected}
      onClick={onClick}
    >
      <span className="recording-browser__segment-marker" aria-hidden="true" />
      <span className="recording-browser__segment-copy">
        <strong>{formatKstRange(segment.startMs, segment.endMs)}</strong>
        <span>{formatDuration(segment.startMs, segment.endMs)} · {playbackStateLabel(segment.state)}</span>
      </span>
      {selected && <span className="recording-browser__segment-state">{playing ? "재생 중" : "선택됨"}</span>}
    </button>
  );
}

function QueryMessage({ text, tone = "muted", onRetry }: {
  readonly text: string;
  readonly tone?: "muted" | "error";
  readonly onRetry?: () => void;
}) {
  return (
    <div
      className={"recording-browser__query-message recording-browser__query-message--" + tone}
      role={tone === "error" ? "alert" : "status"}
    >
      <span>{text}</span>
      {onRetry && <button type="button" onClick={onRetry}>다시 시도</button>}
    </div>
  );
}

function cameraForRequest(
  cameras: readonly PlaybackCamera[],
  registeredCameras: readonly Camera[],
  request: RecordingBrowserRequest,
) {
  const registered = registeredCameras.find((camera) => (
    camera.streamName === request.cameraKeyHint
    || camera.recordingStreamName === request.cameraKeyHint
    || camera.streamOutputs.some((output) => output.purpose === "recording" && output.streamName === request.cameraKeyHint)
  ));
  return cameras.find((camera) => camera.cameraKey === registered?.streamName)
    ?? cameras.find((camera) => camera.cameraKey === request.cameraKeyHint)
    ?? cameras.find((camera) => camera.cameraKey === "camera:" + String(request.cameraId));
}

function kstDayRange(date: string): PlaybackTimelineViewport {
  const fromMs = Date.parse(date + "T00:00:00+09:00");
  const safeFromMs = Number.isFinite(fromMs) ? fromMs : Date.now();
  return { fromMs: safeFromMs, toMs: safeFromMs + DAY_MS };
}

function parseKstDateTime(date: string, time: string) {
  const atMs = Date.parse(date + "T" + (time || "00:00:00") + "+09:00");
  return Number.isFinite(atMs) ? atMs : null;
}

function formatKstParts(atMs: number) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Seoul",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).formatToParts(new Date(atMs));
  return Object.fromEntries(parts.map((part) => [part.type, part.value]));
}

function formatKstDate(atMs: number) {
  const parts = formatKstParts(atMs);
  return String(parts.year) + "-" + String(parts.month) + "-" + String(parts.day);
}

function formatKstTime(atMs: number) {
  const parts = formatKstParts(atMs);
  return String(parts.hour) + ":" + String(parts.minute) + ":" + String(parts.second);
}

function formatKstTimestamp(atMs: number) {
  return formatKstDate(atMs) + " " + formatKstTime(atMs) + " KST";
}

function formatKstDateLabel(date: string) {
  const atMs = Date.parse(date + "T12:00:00+09:00");
  if (!Number.isFinite(atMs)) return date;
  return new Intl.DateTimeFormat("ko-KR", {
    timeZone: "Asia/Seoul",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    weekday: "short",
  }).format(new Date(atMs));
}

function formatKstRange(startMs: number, endMs: number) {
  return formatKstTime(startMs) + " – " + formatKstTime(endMs);
}

function formatDuration(startMs: number, endMs: number) {
  const seconds = Math.max(0, Math.round((endMs - startMs) / 1000));
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return minutes > 0 ? String(minutes) + "분 " + String(rest) + "초" : String(rest) + "초";
}

function playbackStateLabel(state: string) {
  if (state === "recording") return "녹화 중";
  if (state === "finalizing") return "마무리";
  return "완료";
}
