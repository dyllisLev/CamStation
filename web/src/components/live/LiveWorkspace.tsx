import { Camera as CameraIcon, ChevronLeft, Expand, Eye, LayoutDashboard, PanelRightClose, PanelRightOpen, RefreshCw, Save, SaveAll, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent, ReactNode, WheelEvent } from "react";
import GridLayout from "react-grid-layout/legacy";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";
import type { Camera } from "../../app/api";
import { withAppBase } from "../../app/basePath";
import { isViewerMode, livePlaybackSurface } from "../../app/viewerMode";
import {
  useCameras,
  useCreateLayout,
  useDeleteLayout,
  useLayouts,
  useRefreshCameraControls,
  useUpdateLayout,
} from "../../app/queries";
import { cn } from "../../lib/utils";
import {
  clampLayout,
  GRID_COLS,
  GRID_ROWS,
  mergeWithCameras,
  resolveInitialLayout,
  resolveLayoutAfterDelete,
  type MonitorLayoutItem,
  type VideoViewport,
} from "./liveLayoutState";
import { PtzControlPanel } from "./PtzControlPanel";
import { playbackStatusCopy } from "./playbackPresentation";
import { playbackStreamCandidates, tileFocusPresentation } from "./streamSelection";
import { hasViewerFullscreenBridge, reportViewerStream, requestViewerFullscreen, subscribeViewerCommands, subscribeViewerFullscreen } from "./viewerBridge";
import { useWebRtcMseStream, type PlaybackPhase } from "./useWebRtcMseStream";
import { usePlaybackTimelines } from "../../app/playbackQueries";
import { PlaybackControls } from "../playback/PlaybackControls";
import { RecordedVideoViewport } from "../playback/RecordedVideoViewport";
import { PlaybackTimeline, type PlaybackTimelineRange } from "../playback/PlaybackTimeline";
import { usePlaybackWorkspace, type PlaybackWorkspace } from "../playback/usePlaybackWorkspace";
import "./livePlayback.css";

const GRID_MARGIN: [number, number] = [4, 4];
const LAST_LAYOUT_KEY = "camstation-live-layout-id";
const TIMELINE_KEY = "camstation-live-timeline-collapsed";
const DEFAULT_VIDEO_VIEWPORT: VideoViewport = { scale: 1, tx: 0, ty: 0 };

export function LiveWorkspace() {
  const cameras = useCameras();
  const layoutsQuery = useLayouts();
  const createLayout = useCreateLayout();
  const deleteLayout = useDeleteLayout();
  const updateLayout = useUpdateLayout();
  const refreshCameraControls = useRefreshCameraControls();
  const rows = useMemo(() => cameras.data?.filter((camera) => camera.enabled) ?? [], [cameras.data]);
  const layouts = useMemo(() => layoutsQuery.data ?? [], [layoutsQuery.data]);
  const [layout, setLayout] = useState<MonitorLayoutItem[]>([]);
  const [currentId, setCurrentId] = useState<string>("");
  const [selectedStream, setSelectedStream] = useState("");
  const [reconnectGenerations, setReconnectGenerations] = useState<Record<string, number>>({});
  const [dirty, setDirty] = useState(false);
  const [savedFlash, setSavedFlash] = useState(false);
  const [sideHidden, setSideHidden] = useState(false);
  const [ptzPanelOpen, setPtzPanelOpen] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [zoomedStream, setZoomedStream] = useState<string | null>(null);
  const [recordedViewports, setRecordedViewports] = useState<Record<string, VideoViewport>>({});
  const [timelineCollapsed, setTimelineCollapsed] = useState(() => localStorage.getItem(TIMELINE_KEY) === "true");
  const viewerMode = isViewerMode(window.location.search);
  const nativeFullscreen = hasViewerFullscreenBridge();
  const today = formatKstDate(Date.now());
  const [timelineViewport, setTimelineViewport] = useState(() => kstDayViewport(today));
  const [browseDate, setBrowseDate] = useState(today);
  const [timeDraft, setTimeDraft] = useState(() => formatKstTime(Date.now()));
  const selectedCamera = rows.find((camera) => camera.streamName === selectedStream);
  const cameraByStream = useMemo(() => new Map(rows.map((camera) => [camera.streamName, camera])), [rows]);
  const displayedCameraKeys = useMemo(
    () => layout.map((item) => item.i).filter((streamName) => cameraByStream.has(streamName)),
    [cameraByStream, layout],
  );
  const workspace = usePlaybackWorkspace({ cameraKeys: displayedCameraKeys, initialMode: "live" });
  const timelineQueries = usePlaybackTimelines(
    displayedCameraKeys,
    timelineViewport.fromMs,
    timelineViewport.toMs,
  );
  const selectedTimelineIndex = displayedCameraKeys.indexOf(selectedCamera?.streamName ?? "");
  const selectedCoverage = selectedTimelineIndex >= 0
    ? timelineQueries[selectedTimelineIndex]?.data?.coverage ?? []
    : [];
  const aggregateCoverage = useMemo(
    () => mergeTimelineRanges(timelineQueries.flatMap((query) => query.data?.coverage ?? [])),
    [timelineQueries],
  );
  const failedTimelineQueries = timelineQueries.filter((query) => query.isError);
  const layoutInitializedRef = useRef(false);
  const refreshAttemptedRef = useRef(new Set<string>());
  const ptzStopRef = useRef<() => Promise<void>>(async () => undefined);
  const playbackIntentRef = useRef(0);
  const selectedControls = selectedCamera?.controlCapabilities;
  const ptzEnabled = Boolean(
    workspace.mode === "live" &&
    selectedCamera?.state === "streaming" &&
      selectedControls?.ptz.support === "supported" &&
      selectedControls.ptz.available,
  );
  const ptzDisabledReason = !selectedCamera
    ? "카메라를 선택하세요."
    : workspace.mode === "playback"
      ? "녹화 재생 중에는 PTZ를 사용할 수 없습니다."
      : selectedCamera.state !== "streaming"
      ? "카메라가 온라인 상태가 아닙니다."
      : selectedControls?.ptz.support === "unknown" || !selectedControls
        ? "PTZ 지원 여부를 확인하지 못했습니다."
        : selectedControls.ptz.support === "unsupported"
          ? "이 카메라는 PTZ를 지원하지 않습니다."
          : "PTZ 제어를 사용할 수 없습니다.";

  const registerPtzStop = useCallback((stop: () => Promise<void>) => {
    ptzStopRef.current = stop;
  }, []);

  const closePtzPanel = useCallback(async () => {
    await ptzStopRef.current();
    setPtzPanelOpen(false);
  }, []);

  const toggleSidePanel = useCallback(async () => {
    if (!sideHidden && ptzPanelOpen) await closePtzPanel();
    setSideHidden((value) => !value);
  }, [closePtzPanel, ptzPanelOpen, sideHidden]);

  const hideSidePanel = useCallback(async () => {
    if (ptzPanelOpen) await closePtzPanel();
    setSideHidden(true);
  }, [closePtzPanel, ptzPanelOpen]);

  const seekPlayback = useCallback(async (atMs: number) => {
    const intent = ++playbackIntentRef.current;
    if (workspace.mode === "live") {
      await ptzStopRef.current();
      if (intent !== playbackIntentRef.current) return;
      setPtzPanelOpen(false);
    }
    setBrowseDate(formatKstDate(atMs));
    setTimeDraft(formatKstTime(atMs));
    workspace.seek(atMs, { play: workspace.mode === "live" ? true : workspace.playing });
  }, [workspace]);

  const returnToLive = useCallback(() => {
    playbackIntentRef.current += 1;
    setRecordedViewports({});
    workspace.goLive();
  }, [workspace]);

  const retryCamera = useCallback((streamName: string) => {
    if (workspace.mode === "playback") {
      workspace.retry(streamName);
      return;
    }
    setReconnectGenerations((current) => ({
      ...current,
      [streamName]: (current[streamName] ?? 0) + 1,
    }));
  }, [workspace]);

  const handleRecordedViewportChange = useCallback((streamName: string, viewport: VideoViewport) => {
    setRecordedViewports((current) => ({ ...current, [streamName]: viewport }));
  }, []);

  const handleBrowseDateChange = useCallback((nextDate: string) => {
    if (!nextDate) return;
    setBrowseDate(nextDate);
    setTimelineViewport(kstDayViewport(nextDate));
  }, []);

  const commitTimeDraft = useCallback(() => {
    const atMs = parseKstDateTime(browseDate, timeDraft);
    if (atMs !== null) void seekPlayback(atMs);
  }, [browseDate, seekPlayback, timeDraft]);

  useEffect(() => {
    if (selectedStream || rows.length === 0) return;
    setSelectedStream(rows[0].streamName);
  }, [rows, selectedStream]);

  useEffect(() => {
    if (!selectedStream || rows.some((camera) => camera.streamName === selectedStream)) return;
    let active = true;
    setZoomedStream(null);
    void ptzStopRef.current().finally(() => {
      if (!active) return;
      setPtzPanelOpen(false);
      setSelectedStream(rows[0]?.streamName ?? "");
    });
    return () => {
      active = false;
    };
  }, [rows, selectedStream]);

  useEffect(() => {
    const streamName = selectedCamera?.streamName;
    if (!streamName || selectedCamera.controlCapabilities?.discoveredAt) return;
    if (refreshAttemptedRef.current.has(streamName)) return;
    refreshAttemptedRef.current.add(streamName);
    refreshCameraControls.mutate({ streamName });
  }, [refreshCameraControls, selectedCamera]);

  const previousSelectedStreamRef = useRef(selectedStream);
  useEffect(() => {
    if (previousSelectedStreamRef.current !== selectedStream) {
      previousSelectedStreamRef.current = selectedStream;
      if (ptzPanelOpen) void closePtzPanel();
    }
  }, [closePtzPanel, ptzPanelOpen, selectedStream]);

  useEffect(() => {
    if (ptzPanelOpen && (!ptzEnabled || sideHidden)) void closePtzPanel();
  }, [closePtzPanel, ptzEnabled, ptzPanelOpen, sideHidden]);

  useEffect(
    () => () => {
      void ptzStopRef.current();
    },
    [],
  );

  useEffect(() => {
    if (nativeFullscreen) return subscribeViewerFullscreen(setFullscreen);
    const handler = () => setFullscreen(Boolean(document.fullscreenElement));
    document.addEventListener("fullscreenchange", handler);
    return () => document.removeEventListener("fullscreenchange", handler);
  }, [nativeFullscreen]);

  useEffect(() => {
    if (!zoomedStream) return;
    const handler = (event: KeyboardEvent) => {
      if (event.key === "Escape") setZoomedStream(null);
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [zoomedStream]);

  useEffect(() => {
    if (layoutInitializedRef.current || !cameras.isSuccess || !layoutsQuery.isSuccess) return;
    const resolved = resolveInitialLayout(rows, layouts, localStorage.getItem(LAST_LAYOUT_KEY), true);
    if (!resolved) return;
    layoutInitializedRef.current = true;
    setCurrentId(resolved.currentId);
    setLayout(resolved.layout);
    if (resolved.timelineCollapsed !== undefined) setTimelineCollapsed(resolved.timelineCollapsed);
  }, [cameras.isSuccess, layouts, layoutsQuery.isSuccess, rows]);

  const onlineCount = rows.filter((camera) => camera.state === "streaming").length;

  const handleLayoutChange = useCallback((next: MonitorLayoutItem[]) => {
    setLayout((current) => {
      const zoomByStream = new Map(current.map((item) => [item.i, item.videoZoom]));
      return clampLayout(
        next.map((item) => ({
          ...item,
          videoZoom: zoomByStream.get(item.i),
        })),
      );
    });
    setDirty(true);
  }, []);

  const handleVideoViewportChange = useCallback((streamName: string, viewport: VideoViewport) => {
    setLayout((current) =>
      current.map((item) =>
        item.i === streamName
          ? {
              ...item,
              videoZoom: viewport.scale > 1 ? viewport : undefined,
            }
          : item,
      ),
    );
    setDirty(true);
  }, []);

  async function saveCurrentLayout() {
    const payload = {
      name: layouts.find((item) => item.id === currentId)?.name ?? "기본",
      data: layout.map(toLayoutItem),
      timeline_collapsed: timelineCollapsed,
      grid_cols: GRID_COLS,
      grid_rows: GRID_ROWS,
    };
    const result = currentId
      ? await updateLayout.mutateAsync({ id: currentId, layout: payload })
      : await createLayout.mutateAsync(payload);
    setCurrentId(result.id);
    localStorage.setItem(LAST_LAYOUT_KEY, result.id);
    setDirty(false);
    flashSaved();
  }

  async function saveAsLayout() {
    const name = window.prompt("새 배치 이름", "신규 관제 배치");
    if (!name) return;
    const result = await createLayout.mutateAsync({
      name,
      data: layout.map(toLayoutItem),
      timeline_collapsed: timelineCollapsed,
      grid_cols: GRID_COLS,
      grid_rows: GRID_ROWS,
    });
    setCurrentId(result.id);
    localStorage.setItem(LAST_LAYOUT_KEY, result.id);
    setDirty(false);
    flashSaved();
  }

  function loadLayout(id: string) {
    const saved = layouts.find((item) => item.id === id);
    if (!saved) return;
    setCurrentId(saved.id);
    setLayout(mergeWithCameras(saved.data, rows));
    setTimelineCollapsed(saved.timeline_collapsed);
    setDirty(false);
    localStorage.setItem(LAST_LAYOUT_KEY, saved.id);
  }

  async function deleteSavedLayout(id: string, name: string) {
    if (!window.confirm(`‘${name}’ 배치를 삭제할까요?`)) return;
    try {
      await deleteLayout.mutateAsync(id);
      const resolved = resolveLayoutAfterDelete(id, currentId, layouts, rows);
      if (!resolved) return;
      setCurrentId(resolved.currentId);
      setLayout(resolved.layout);
      if (resolved.timelineCollapsed !== undefined) setTimelineCollapsed(resolved.timelineCollapsed);
      setDirty(false);
      if (resolved.currentId) localStorage.setItem(LAST_LAYOUT_KEY, resolved.currentId);
      else localStorage.removeItem(LAST_LAYOUT_KEY);
    } catch {
      window.alert("저장된 배치를 삭제하지 못했습니다.");
    }
  }

  function toggleTimeline() {
    const next = !timelineCollapsed;
    setTimelineCollapsed(next);
    localStorage.setItem(TIMELINE_KEY, String(next));
    setDirty(true);
  }

  function flashSaved() {
    setSavedFlash(true);
    window.setTimeout(() => setSavedFlash(false), 1400);
  }

  async function toggleFullscreen() {
    if (nativeFullscreen) {
      await requestViewerFullscreen(!fullscreen);
      return;
    }
    if (!document.fullscreenElement) {
      await document.documentElement.requestFullscreen();
      return;
    }
    await document.exitFullscreen();
  }

  return (
    <div className="new-app new-live-app">
      <MonitorHeader viewerMode={viewerMode}>
        <select
          className="new-select"
          value={currentId}
          onChange={(event) => loadLayout(event.target.value)}
          aria-label="저장된 배치 선택"
        >
          {layouts.length === 0 && <option value="">기본</option>}
          {layouts.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name}
              {item.id === currentId && dirty ? " *" : ""}
            </option>
          ))}
        </select>
        <button className="new-primary" type="button" onClick={saveCurrentLayout}>
          <Save size={14} />
          {savedFlash ? "저장됨" : "배치 저장"}
        </button>
        <button className="new-ghost" type="button" onClick={saveAsLayout}>
          <SaveAll size={14} />
          새 이름 저장
        </button>
        <button
          className={ptzEnabled ? "new-primary" : "new-ghost"}
          type="button"
          disabled={!ptzEnabled}
          title={ptzEnabled ? "선택 카메라 PTZ 제어" : ptzDisabledReason}
          aria-describedby={ptzEnabled ? undefined : "ptz-disabled-reason"}
          onClick={() => {
            setSideHidden(false);
            setPtzPanelOpen(true);
          }}
        >
          PTZ 제어
        </button>
        {!ptzEnabled && (
          <span id="ptz-disabled-reason" className="new-sr-only">
            {ptzDisabledReason}
          </span>
        )}
        <button className="new-ghost" type="button" onClick={() => void toggleSidePanel()}>
          {sideHidden ? <PanelRightOpen size={14} /> : <PanelRightClose size={14} />}
          {sideHidden ? "우측 패널 보기" : "우측 패널 숨기기"}
        </button>
        <button
          className={cn("new-timeline-command", timelineCollapsed ? "new-primary" : "new-ghost")}
          type="button"
          onClick={toggleTimeline}
        >
          {timelineCollapsed ? "타임라인 보기" : "타임라인 숨기기"}
        </button>
        <div className="new-spacer" />
        <div className={cn("new-mode-pill", workspace.mode === "live" ? "new-mode-live" : "new-mode-playback") }>
          <span className={workspace.mode === "live" ? "new-pulse" : "new-mode-dot"} />
          {workspace.mode === "live" ? "LIVE" : `녹화 재생 · ${formatKstDateTime(workspace.playheadMs)}`}
        </div>
        <button className="new-ghost" type="button" onClick={toggleFullscreen}>
          <Expand size={14} />
          {fullscreen ? "전체화면 종료" : "전체화면"}
        </button>
      </MonitorHeader>

      <section className={cn("new-live-workspace", sideHidden && "new-panel-hidden")}>
        <main className="new-grid-wrap" aria-label="라이브 카메라 그리드">
          {layout.length > 0 ? (
            <CameraGrid
              cameras={rows}
              reconnectGenerations={reconnectGenerations}
              layout={layout}
              workspace={workspace}
              selectedStream={selectedCamera?.streamName ?? ""}
              onLayoutChange={handleLayoutChange}
              onSelectCamera={(camera) => setSelectedStream(camera.streamName)}
              zoomedStream={zoomedStream}
              onToggleZoom={(camera) => {
                setSelectedStream(camera.streamName);
                setZoomedStream((current) => (current === camera.streamName ? null : camera.streamName));
              }}
              onVideoViewportChange={handleVideoViewportChange}
              recordedViewports={recordedViewports}
              onRecordedViewportChange={handleRecordedViewportChange}
            />
          ) : (
            <div className="new-empty">
              {cameras.isError || layoutsQuery.isError
                ? "라이브 배치 정보를 불러오지 못했습니다."
                : "카메라와 배치 정보를 불러오는 중입니다."}
            </div>
          )}
        </main>
        {!sideHidden && (
          <aside className="new-side-panel" aria-label="운영 패널">
            {ptzPanelOpen && ptzEnabled && selectedCamera ? (
              <PtzControlPanel
                camera={selectedCamera}
                onBack={() => setPtzPanelOpen(false)}
                onStopReady={registerPtzStop}
              />
            ) : (
              <>
                <section className="new-panel-card">
                  <div className="new-section-title">
                    <span>
                      저장된 배치 <em>{Math.max(layouts.length, 1)} profiles</em>
                    </span>
                    <button
                      className="new-icon-button"
                      type="button"
                      onClick={() => void hideSidePanel()}
                      aria-label="우측 패널 숨기기"
                    >
                      <ChevronLeft size={14} />
                    </button>
                  </div>
                  <div className="new-layout-list">
                    {layouts.map((item) => (
                      <div
                        key={item.id}
                        className={cn("new-layout-row", item.id === currentId && "new-active-row")}
                      >
                        <button type="button" className="new-layout-load" onClick={() => loadLayout(item.id)}>
                          <span>{item.name}</span>
                          <em>{item.id === currentId && dirty ? "편집됨" : formatShortTime(item.updated_at)}</em>
                        </button>
                        <button
                          type="button"
                          className="new-layout-delete"
                          aria-label={`${item.name} 배치 삭제`}
                          title="배치 삭제"
                          disabled={deleteLayout.isPending}
                          onClick={() => void deleteSavedLayout(item.id, item.name)}
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    ))}
                    {layouts.length === 0 && (
                      <div className="new-layout-row new-layout-row-empty new-active-row">
                        <span>기본</span>
                        <em>미저장</em>
                      </div>
                    )}
                  </div>
                </section>
                <section className="new-panel-card">
                  <div className="new-section-title">
                    <span className="new-title-with-icon">
                      <CameraIcon size={14} /> 카메라 상태
                    </span>
                    <em>
                      {onlineCount} / {rows.length} online
                    </em>
                  </div>
                  <div className="new-camera-list">
                    {rows.map((camera) => (
                      <div
                        key={camera.streamName}
                        className={cn(
                          "new-camera-row",
                          camera.streamName === selectedCamera?.streamName && "new-active-row",
                        )}
                      >
                        <button
                          className="new-camera-select"
                          type="button"
                          onClick={() => setSelectedStream(camera.streamName)}
                        >
                          <span className={cn("new-state", camera.state !== "streaming" && "new-danger")} />
                          <span>{camera.name}</span>
                          <em>{workspace.mode === "playback" ? "playback" : camera.state === "streaming" ? "live" : camera.state}</em>
                        </button>
                        <button
                          className="new-camera-reconnect"
                          type="button"
                          aria-label={`${camera.name} ${workspace.mode === "live" ? "다시 연결" : "녹화 다시 시도"}`}
                          title={workspace.mode === "live" ? "다시 연결" : "녹화 다시 시도"}
                          onClick={() => retryCamera(camera.streamName)}
                        >
                          <RefreshCw size={14} />
                        </button>
                      </div>
                    ))}
                  </div>
                </section>
              </>
            )}
          </aside>
        )}
      </section>

      <footer className={cn("new-timeline new-playback-timeline-shell", timelineCollapsed && "new-collapsed")} aria-label="선택 카메라와 현재 배치 녹화 타임라인">
        <div className="new-timeline-top new-playback-timeline-top">
          <form
            className="new-playback-datetime"
            onSubmit={(event) => {
              event.preventDefault();
              commitTimeDraft();
            }}
          >
            <input
              type="date"
              value={browseDate}
              onChange={(event) => handleBrowseDateChange(event.target.value)}
              aria-label="탐색 날짜"
            />
            <input
              type="time"
              step="1"
              value={timeDraft}
              onChange={(event) => setTimeDraft(event.target.value)}
              aria-label="탐색 시각"
            />
            <button type="submit" className="new-ghost">이동</button>
          </form>
          <PlaybackControls
            workspace={{ ...workspace, goLive: returnToLive }}
            liveEnabled
            className="new-live-playback-controls"
          />
          <button className="new-ghost new-playback-collapse" type="button" onClick={toggleTimeline}>
            {timelineCollapsed ? "타임라인 보기" : "타임라인 숨기기"}
          </button>
        </div>
        {!timelineCollapsed && (
          <div className="new-live-playback-timeline-body">
            {failedTimelineQueries.length > 0 && (
              <div className="new-playback-timeline-error" role="alert">
                <span>일부 카메라의 녹화 범위를 불러오지 못했습니다.</span>
                <button
                  type="button"
                  className="new-ghost"
                  onClick={() => failedTimelineQueries.forEach((query) => void query.refetch())}
                >
                  다시 시도
                </button>
              </div>
            )}
            <PlaybackTimeline
              ranges={toTimelineRanges(selectedCoverage)}
              aggregateRanges={aggregateCoverage}
              viewport={timelineViewport}
              playheadMs={workspace.playheadMs}
              onSeek={(atMs) => void seekPlayback(atMs)}
              onViewportChange={setTimelineViewport}
              selectedLabel={selectedCamera?.name ?? "카메라 없음"}
              aggregateLabel="현재 배치 · 하나 이상 녹화"
              disabled={displayedCameraKeys.length === 0}
              className="new-live-playback-timeline"
              ariaLabel="선택 카메라와 현재 배치 녹화 범위"
            />
          </div>
        )}
      </footer>
    </div>
  );
}

function MonitorHeader({ children, viewerMode }: { readonly children: ReactNode; readonly viewerMode: boolean }) {
  return (
    <header className="new-command">
      {!viewerMode && (
        <>
          <a className="new-brand" href={withAppBase("/")} aria-label="CamStation 모니터링">
            <div className="new-brand-mark"><LayoutDashboard size={16} /></div>
            <div>
              <div className="new-brand-title">CamStation</div>
              <div className="new-mini">HOME MONITOR</div>
            </div>
          </a>
          <nav className="new-nav" aria-label="주요 화면">
            <a className="new-active" href={withAppBase("/live")}>라이브</a>
            <a href={withAppBase("/recordings")}>녹화</a>
            <a href={withAppBase("/settings")}>설정</a>
          </nav>
        </>
      )}
      {children}
    </header>
  );
}

function CameraGrid({
  cameras,
  reconnectGenerations,
  layout,
  workspace,
  selectedStream,
  onLayoutChange,
  onSelectCamera,
  zoomedStream,
  onToggleZoom,
  onVideoViewportChange,
  recordedViewports,
  onRecordedViewportChange,
}: {
  cameras: Camera[];
  reconnectGenerations: Record<string, number>;
  layout: MonitorLayoutItem[];
  workspace: PlaybackWorkspace;
  selectedStream: string;
  onLayoutChange: (layout: MonitorLayoutItem[]) => void;
  onSelectCamera: (camera: Camera) => void;
  zoomedStream: string | null;
  onToggleZoom: (camera: Camera) => void;
  onVideoViewportChange: (streamName: string, viewport: VideoViewport) => void;
  recordedViewports: Record<string, VideoViewport>;
  onRecordedViewportChange: (streamName: string, viewport: VideoViewport) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerSize, setContainerSize] = useState({ width: 0, height: 0 });
  const cameraByStream = useMemo(() => new Map(cameras.map((camera) => [camera.streamName, camera])), [cameras]);
  const zoomedCamera = zoomedStream ? cameraByStream.get(zoomedStream) : undefined;

  useEffect(() => {
    const element = containerRef.current;
    if (!element) return;
    const measure = () => setContainerSize({ width: element.offsetWidth, height: element.offsetHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const rowHeight =
    containerSize.height > 0
      ? Math.max(1, Math.floor((containerSize.height - (GRID_ROWS - 1) * GRID_MARGIN[1]) / GRID_ROWS))
      : 1;

  return (
    <div ref={containerRef} className={cn("new-grid-stage", zoomedCamera && "new-zoom-active")}>
      {containerSize.width > 0 && containerSize.height > 0 && (
        <GridLayout
          layout={clampLayout(layout)}
          cols={GRID_COLS}
          rowHeight={rowHeight}
          width={containerSize.width}
          onLayoutChange={(nextLayout) => onLayoutChange(nextLayout.map((item) => ({ ...item })))}
          draggableHandle=".cam-drag-handle"
          resizeHandles={["n", "s", "e", "w", "ne", "nw", "se", "sw"]}
          margin={GRID_MARGIN}
          containerPadding={[0, 0]}
          maxRows={GRID_ROWS}
          isDraggable={!zoomedCamera}
          isResizable={!zoomedCamera}
          isBounded
          autoSize={false}
          style={{ height: "100%" }}
        >
          {layout.map((item) => {
            const camera = cameraByStream.get(item.i);
            if (!camera) return <div key={item.i} className="new-grid-item" />;
            const presentation = tileFocusPresentation(camera.streamName, zoomedCamera?.streamName ?? null);
            return (
              <div key={item.i} className={cn("new-grid-item", `new-grid-item-${presentation}`)}>
                <CameraTile
                  camera={camera}
                  workspace={workspace}
                  reconnectGeneration={reconnectGenerations[camera.streamName] ?? 0}
                  selected={camera.streamName === selectedStream}
                  zoomed={presentation === "focused"}
                  onSelect={() => onSelectCamera(camera)}
                  onToggleZoom={() => onToggleZoom(camera)}
                  videoViewport={item.videoZoom}
                  onVideoViewportChange={(viewport) => onVideoViewportChange(camera.streamName, viewport)}
                  recordedViewport={recordedViewports[camera.streamName]}
                  onRecordedViewportChange={(viewport) => onRecordedViewportChange(camera.streamName, viewport)}
                />
              </div>
            );
          })}
        </GridLayout>
      )}
    </div>
  );
}

function CameraTile({
  camera,
  workspace,
  reconnectGeneration,
  selected,
  zoomed = false,
  onSelect,
  onToggleZoom,
  videoViewport,
  onVideoViewportChange,
  recordedViewport,
  onRecordedViewportChange,
}: {
  camera: Camera;
  workspace: PlaybackWorkspace;
  reconnectGeneration: number;
  selected: boolean;
  zoomed?: boolean;
  onSelect: () => void;
  onToggleZoom: () => void;
  videoViewport?: VideoViewport;
  onVideoViewportChange: (viewport: VideoViewport) => void;
  recordedViewport?: VideoViewport;
  onRecordedViewportChange: (viewport: VideoViewport) => void;
}) {
  const [playback, setPlayback] = useState<{ phase: PlaybackPhase; usingFallback: boolean }>({
    phase: "connecting",
    usingFallback: false,
  });
  const browserPlaying = playback.phase === "playing";
  const recordedState = workspace.cameras[camera.streamName];
  const playbackUnavailable = workspace.mode === "live"
    ? !browserPlaying
    : !recordedState || !["ready", "playing"].includes(recordedState.status);

  return (
    <article
      className={cn("new-camera-tile", selected && "new-selected", zoomed && "new-zoomed", playbackUnavailable && "new-offline")}
      onClick={onSelect}
      onDoubleClick={(event) => {
        event.stopPropagation();
        onToggleZoom();
      }}
    >
      {workspace.mode === "live" ? (
        <LiveVideo
          reconnectGeneration={reconnectGeneration}
          streamNames={playbackStreamCandidates(camera)}
          viewport={videoViewport}
          onViewportChange={onVideoViewportChange}
          onPlaybackChange={setPlayback}
        />
      ) : (
        <RecordedVideoViewport
          workspace={workspace}
          cameraKey={camera.streamName}
          selected={selected}
          viewport={recordedViewport}
          onViewportChange={onRecordedViewportChange}
        />
      )}
      <div className="new-tile-head cam-drag-handle">
        <span className={cn("new-state", playbackUnavailable && "new-danger")} />
        <strong>{camera.name}</strong>
        <span className="new-cam-id">{workspace.mode === "live" ? camera.name : "녹화 재생"}</span>
      </div>
      <button
        className="new-focus-btn"
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          onToggleZoom();
        }}
      >
        <Eye size={13} />
        {zoomed ? "집중 보기 종료" : "집중 보기"}
      </button>
    </article>
  );
}

function LiveVideo({
  streamNames,
  reconnectGeneration,
  viewport,
  onViewportChange,
  onPlaybackChange,
}: {
  streamNames: readonly string[];
  reconnectGeneration: number;
  viewport?: VideoViewport;
  onViewportChange: (viewport: VideoViewport) => void;
  onPlaybackChange: (playback: { phase: PlaybackPhase; usingFallback: boolean }) => void;
}) {
  const streamKey = streamNames.join("\u001f");
  const [resubscribeGeneration, setResubscribeGeneration] = useState(0);
  const surface = livePlaybackSurface(window.location.search, Boolean(window.camstationViewer));
  const {
    videoRef,
    connected,
    transport,
    phase,
    activeStreamName,
    usingFallback,
    lastBinaryAt,
    lastProgressAt,
    readyState,
    stalledForMs,
    reconnectCount,
    fallbackCount,
    resubscribeCount,
    errorCategory,
  } = useWebRtcMseStream(streamNames, resubscribeGeneration + reconnectGeneration, "webrtc", surface);
  const frameRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ x: number; y: number; tx: number; ty: number } | null>(null);
  const currentViewport = viewport ?? DEFAULT_VIDEO_VIEWPORT;
  const zoomed = currentViewport.scale > 1.001;
  const statusCopy = playbackStatusCopy(phase, transport);

  useEffect(() => {
    const candidates = streamKey.split("\u001f").filter(Boolean);
    return subscribeViewerCommands(
      () => setResubscribeGeneration((value) => value + 1),
      undefined,
      candidates,
    );
  }, [streamKey]);

  useEffect(() => {
    if (!activeStreamName) return;
    const report = () => {
      reportViewerStream({
        streamName: activeStreamName,
        transport,
        phase,
        lastBinaryAt: lastBinaryAt ?? undefined,
        lastProgressAt: lastProgressAt ?? undefined,
        readyState,
        stalledForMs,
        reconnectCount,
        fallbackCount,
        resubscribeCount,
        errorCategory,
      });
    };
    report();
    const timer = window.setInterval(report, 5_000);
    return () => window.clearInterval(timer);
  }, [activeStreamName, errorCategory, fallbackCount, lastBinaryAt, lastProgressAt, phase, readyState, reconnectCount, resubscribeCount, stalledForMs, transport]);

  useEffect(() => {
    onPlaybackChange({ phase, usingFallback });
  }, [onPlaybackChange, phase, usingFallback]);

  const applyViewport = useCallback((next: VideoViewport) => {
    const frame = frameRef.current;
    if (!frame || next.scale <= 1) {
      onViewportChange(DEFAULT_VIDEO_VIEWPORT);
      return;
    }
    const rect = frame.getBoundingClientRect();
    onViewportChange(clampVideoViewport(next, rect.width, rect.height));
  }, [onViewportChange]);

  const handleWheel = useCallback(
    (event: WheelEvent<HTMLDivElement>) => {
      event.preventDefault();
      event.stopPropagation();
      const frame = frameRef.current;
      if (!frame) return;
      const rect = frame.getBoundingClientRect();
      const nextScale = clampNumber(currentViewport.scale * (event.deltaY < 0 ? 1.15 : 1 / 1.15), 1, 4);
      if (nextScale === 1) {
        applyViewport({ scale: 1, tx: 0, ty: 0 });
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
    },
    [applyViewport, currentViewport],
  );

  const handleMouseDown = useCallback(
    (event: MouseEvent<HTMLDivElement>) => {
      if (!zoomed || event.button !== 0) return;
      event.preventDefault();
      event.stopPropagation();
      dragRef.current = { x: event.clientX, y: event.clientY, tx: currentViewport.tx, ty: currentViewport.ty };

      const handleMove = (moveEvent: globalThis.MouseEvent) => {
        const drag = dragRef.current;
        const frame = frameRef.current;
        if (!drag || !frame) return;
        const rect = frame.getBoundingClientRect();
        onViewportChange(
          clampVideoViewport(
            {
              scale: currentViewport.scale,
              tx: drag.tx + moveEvent.clientX - drag.x,
              ty: drag.ty + moveEvent.clientY - drag.y,
            },
            rect.width,
            rect.height,
          ),
        );
      };

      const handleUp = () => {
        dragRef.current = null;
        window.removeEventListener("mousemove", handleMove);
        window.removeEventListener("mouseup", handleUp);
      };

      window.addEventListener("mousemove", handleMove);
      window.addEventListener("mouseup", handleUp);
    },
    [currentViewport, onViewportChange, zoomed],
  );

  return (
    <div
      ref={frameRef}
      className="new-live-video-frame"
      onWheel={handleWheel}
      onMouseDown={handleMouseDown}
      onDoubleClick={(event) => {
        event.stopPropagation();
        applyViewport({ scale: 1, tx: 0, ty: 0 });
      }}
    >
      <video
        ref={videoRef}
        className="new-live-video"
        autoPlay
        muted
        playsInline
        disablePictureInPicture
        controls={false}
        style={{
          transform: `scale(${currentViewport.scale}) translate(${currentViewport.tx / currentViewport.scale}px, ${currentViewport.ty / currentViewport.scale}px)`,
        }}
      />
      {!connected && <div className="new-offline-layer">{statusCopy}</div>}
      {connected && usingFallback && <div className="new-fallback-badge">대체 스트림</div>}
      {zoomed && <div className="new-zoom-badge">{currentViewport.scale.toFixed(1)}x</div>}
    </div>
  );
}

function clampVideoViewport(viewport: { scale: number; tx: number; ty: number }, width: number, height: number) {
  const scale = clampNumber(viewport.scale, 1, 4);
  if (scale <= 1) return { scale: 1, tx: 0, ty: 0 };
  return {
    scale,
    tx: clampNumber(viewport.tx, -((scale - 1) * width) / 2, ((scale - 1) * width) / 2),
    ty: clampNumber(viewport.ty, -((scale - 1) * height) / 2, ((scale - 1) * height) / 2),
  };
}

function clampNumber(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}

function toLayoutItem(item: MonitorLayoutItem) {
  return {
    i: item.i,
    x: item.x,
    y: item.y,
    w: item.w,
    h: item.h,
    minW: item.minW,
    minH: item.minH,
    videoZoom: item.videoZoom,
  };
}

function toTimelineRanges(ranges: readonly { startMs: number; endMs: number }[]): PlaybackTimelineRange[] {
  return ranges.map(({ startMs, endMs }) => ({ startMs, endMs }));
}

function mergeTimelineRanges(ranges: readonly { startMs: number; endMs: number }[]): PlaybackTimelineRange[] {
  const sorted = toTimelineRanges(ranges)
    .filter((range) => Number.isFinite(range.startMs) && Number.isFinite(range.endMs) && range.endMs > range.startMs)
    .sort((left, right) => left.startMs - right.startMs || left.endMs - right.endMs);
  const merged: PlaybackTimelineRange[] = [];
  for (const range of sorted) {
    const previous = merged.at(-1);
    if (!previous || range.startMs > previous.endMs) {
      merged.push(range);
      continue;
    }
    if (range.endMs > previous.endMs) {
      merged[merged.length - 1] = { startMs: previous.startMs, endMs: range.endMs };
    }
  }
  return merged;
}

function formatKstDate(atMs: number): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Seoul",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date(atMs));
}

function formatKstTime(atMs: number): string {
  return new Intl.DateTimeFormat("en-GB", {
    timeZone: "Asia/Seoul",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).format(new Date(atMs));
}

function formatKstDateTime(atMs: number): string {
  return `${formatKstDate(atMs)} ${formatKstTime(atMs)} KST`;
}

function kstDayViewport(date: string) {
  const fromMs = new Date(`${date}T00:00:00+09:00`).getTime();
  return { fromMs, toMs: fromMs + 24 * 60 * 60 * 1000 };
}

function parseKstDateTime(date: string, time: string): number | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !/^\d{2}:\d{2}(?::\d{2})?$/.test(time)) return null;
  const atMs = new Date(`${date}T${time.length === 5 ? `${time}:00` : time}+09:00`).getTime();
  return Number.isFinite(atMs) ? atMs : null;
}

function formatShortTime(value: number): string {
  return new Intl.DateTimeFormat("ko-KR", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(value * 1000));
}
