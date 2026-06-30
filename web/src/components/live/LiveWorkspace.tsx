import { Expand } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import GridLayout from "react-grid-layout/legacy";
import "react-grid-layout/css/styles.css";
import "react-resizable/css/styles.css";
import type { Camera, TimelineData } from "../../app/api";
import {
  useCameras,
  useCreateLayout,
  useLayouts,
  useTimeline,
  useUpdateLayout,
} from "../../app/queries";
import { cn, liveUrl } from "../../lib/utils";
import { useMseStream } from "./useMseStream";

const GRID_COLS = 48;
const GRID_ROWS = 48;
const GRID_MARGIN: [number, number] = [4, 4];
const LAST_LAYOUT_KEY = "camstation-live-layout-id";
const TIMELINE_KEY = "camstation-live-timeline-collapsed";

type TimelineRange = { ts_start: number; ts_end: number };
type MonitorLayoutItem = {
  i: string;
  x: number;
  y: number;
  w: number;
  h: number;
  minW?: number;
  minH?: number;
};

export function LiveWorkspace() {
  const cameras = useCameras();
  const layoutsQuery = useLayouts();
  const createLayout = useCreateLayout();
  const updateLayout = useUpdateLayout();
  const rows = useMemo(() => cameras.data ?? [], [cameras.data]);
  const layouts = useMemo(() => layoutsQuery.data ?? [], [layoutsQuery.data]);
  const [layout, setLayout] = useState<MonitorLayoutItem[]>([]);
  const [currentId, setCurrentId] = useState<string>("");
  const [selectedStream, setSelectedStream] = useState("");
  const [dirty, setDirty] = useState(false);
  const [savedFlash, setSavedFlash] = useState(false);
  const [sideHidden, setSideHidden] = useState(false);
  const [fullscreen, setFullscreen] = useState(false);
  const [timelineCollapsed, setTimelineCollapsed] = useState(() => localStorage.getItem(TIMELINE_KEY) === "true");
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Seoul" }).format(new Date());
  const selectedCamera = rows.find((camera) => camera.streamName === selectedStream) ?? rows[0];
  const selectedTimeline = useTimeline(selectedCamera?.streamName ?? "", today);

  useEffect(() => {
    if (selectedStream || rows.length === 0) return;
    setSelectedStream(rows[0].streamName);
  }, [rows, selectedStream]);

  useEffect(() => {
    const handler = () => setFullscreen(Boolean(document.fullscreenElement));
    document.addEventListener("fullscreenchange", handler);
    return () => document.removeEventListener("fullscreenchange", handler);
  }, []);

  useEffect(() => {
    if (rows.length === 0 || layout.length > 0) return;
    const savedId = localStorage.getItem(LAST_LAYOUT_KEY);
    const saved = layouts.find((item) => item.id === savedId) ?? layouts[0];
    if (saved) {
      setCurrentId(saved.id);
      setLayout(mergeWithCameras(saved.data, rows));
      setTimelineCollapsed(saved.timeline_collapsed);
      return;
    }
    setLayout(defaultLayout(rows));
  }, [layout.length, layouts, rows]);

  const onlineCount = rows.filter((camera) => camera.state === "streaming").length;

  const handleLayoutChange = useCallback((next: MonitorLayoutItem[]) => {
    setLayout(clampLayout(next));
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
    if (!document.fullscreenElement) {
      await document.documentElement.requestFullscreen();
      return;
    }
    await document.exitFullscreen();
  }

  return (
    <div className="new-app new-live-app">
      <MonitorHeader>
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
          {savedFlash ? "저장됨" : "배치 저장"}
        </button>
        <button className="new-ghost" type="button" onClick={saveAsLayout}>
          새 이름 저장
        </button>
        <button className="new-ghost" type="button" onClick={() => setSideHidden((value) => !value)}>
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
        <div className="new-live-pill">
          <span className="new-pulse" />
          LIVE
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
              layout={layout}
              selectedStream={selectedCamera?.streamName ?? ""}
              onLayoutChange={handleLayoutChange}
              onSelectCamera={(camera) => setSelectedStream(camera.streamName)}
            />
          ) : (
            <div className="new-empty">카메라와 배치 정보를 불러오는 중입니다.</div>
          )}
        </main>
        {!sideHidden && (
          <aside className="new-side-panel" aria-label="운영 패널">
            <section className="new-panel-card">
              <div className="new-section-title">
                <span>
                  저장된 배치 <em>{Math.max(layouts.length, 1)} profiles</em>
                </span>
                <button className="new-icon-button" type="button" onClick={() => setSideHidden(true)} aria-label="우측 패널 숨기기">
                  -
                </button>
              </div>
              <div className="new-layout-list">
                {layouts.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    className={cn("new-layout-row", item.id === currentId && "new-active-row")}
                    onClick={() => loadLayout(item.id)}
                  >
                    <span>{item.name}</span>
                    <em>{item.id === currentId && dirty ? "편집됨" : formatShortTime(item.updated_at)}</em>
                  </button>
                ))}
                {layouts.length === 0 && (
                  <button type="button" className="new-layout-row new-active-row">
                    <span>기본</span>
                    <em>미저장</em>
                  </button>
                )}
              </div>
            </section>
            <section className="new-panel-card">
              <div className="new-section-title">
                카메라 상태 <em>{onlineCount} / {rows.length} online</em>
              </div>
              <div className="new-camera-list">
                {rows.map((camera) => (
                  <button
                    key={camera.id}
                    className={cn("new-camera-row", camera.streamName === selectedCamera?.streamName && "new-active-row")}
                    type="button"
                    onClick={() => setSelectedStream(camera.streamName)}
                  >
                    <span className={cn("new-state", camera.state !== "streaming" && "new-danger")} />
                    <span>{camera.name}</span>
                    <em>{camera.state === "streaming" ? "live" : camera.state}</em>
                  </button>
                ))}
              </div>
            </section>
          </aside>
        )}
      </section>

      <TwoRowTimeline
        cameras={rows}
        selectedCamera={selectedCamera}
        date={today}
        data={selectedTimeline.data}
        collapsed={timelineCollapsed}
        onToggle={toggleTimeline}
      />
    </div>
  );
}

function MonitorHeader({ children }: { children: ReactNode }) {
  return (
    <header className="new-command">
      <a className="new-brand" href="/" aria-label="CamStation 모니터링">
        <div className="new-brand-mark">CS</div>
        <div>
          <div className="new-brand-title">CamStation</div>
          <div className="new-mini">HOME MONITOR</div>
        </div>
      </a>
      <nav className="new-nav" aria-label="주요 화면">
        <a className="new-active" href="/live">라이브</a>
        <a href="/recordings">녹화</a>
        <a href="/settings">설정</a>
      </nav>
      {children}
    </header>
  );
}

function CameraGrid({
  cameras,
  layout,
  selectedStream,
  onLayoutChange,
  onSelectCamera,
}: {
  cameras: Camera[];
  layout: MonitorLayoutItem[];
  selectedStream: string;
  onLayoutChange: (layout: MonitorLayoutItem[]) => void;
  onSelectCamera: (camera: Camera) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerSize, setContainerSize] = useState({ width: 0, height: 0 });
  const cameraByStream = useMemo(() => new Map(cameras.map((camera) => [camera.streamName, camera])), [cameras]);

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
    <div ref={containerRef} className="new-grid-stage">
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
          isBounded
          autoSize={false}
          style={{ height: "100%" }}
        >
          {layout.map((item) => {
            const camera = cameraByStream.get(item.i);
            if (!camera) return <div key={item.i} className="new-grid-item" />;
            return (
              <div key={item.i} className="new-grid-item">
                <CameraTile
                  camera={camera}
                  selected={camera.streamName === selectedStream}
                  onSelect={() => onSelectCamera(camera)}
                />
              </div>
            );
          })}
        </GridLayout>
      )}
    </div>
  );
}

function CameraTile({ camera, selected, onSelect }: { camera: Camera; selected: boolean; onSelect: () => void }) {
  const { videoRef, connected } = useMseStream(camera.state === "streaming" ? camera.streamName : "");

  return (
    <article
      className={cn("new-camera-tile", selected && "new-selected", camera.state !== "streaming" && "new-offline")}
      onClick={onSelect}
    >
      {camera.state === "streaming" ? (
        <video
          ref={videoRef}
          className="new-live-video"
          autoPlay
          muted
          playsInline
          disablePictureInPicture
          controls={false}
        />
      ) : (
        <div className="new-offline-layer">연결 없음</div>
      )}
      {camera.state === "streaming" && !connected && <div className="new-offline-layer">연결 중...</div>}
      <div className="new-tile-head cam-drag-handle">
        <span className={cn("new-state", camera.state !== "streaming" && "new-danger")} />
        <strong>{camera.name}</strong>
        <span className="new-cam-id">{camera.name}</span>
      </div>
      <button
        className="new-focus-btn"
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          window.open(liveUrl(camera.streamName, "mse"), "_blank");
        }}
      >
        집중 보기
      </button>
    </article>
  );
}

function TwoRowTimeline({
  cameras,
  selectedCamera,
  date,
  data,
  collapsed,
  onToggle,
}: {
  cameras: Camera[];
  selectedCamera?: Camera;
  date: string;
  data?: TimelineData;
  collapsed: boolean;
  onToggle: () => void;
}) {
  const [now, setNow] = useState(new Date());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const dayStart = new Date(`${date}T00:00:00+09:00`).getTime() / 1000;
  const dayEnd = dayStart + 86400;
  const cursorTs = now.getTime() / 1000;
  const selectedRanges = (data?.segments ?? []).map((segment) => ({
    ts_start: segment.ts_start,
    ts_end: segment.ts_end ?? cursorTs,
  }));
  const motionEvents = data?.motion_events ?? [];

  return (
    <footer className={cn("new-timeline", collapsed && "new-collapsed")} aria-label="선택 카메라와 전체 카메라 2줄 타임라인">
      <div className="new-timeline-top">
        <div className="new-clock">{formatClock(now)}</div>
        <div className="new-mini">{date} · 1줄 선택 카메라 · 2줄 전체 집계</div>
        <div className="new-spacer" />
        <div className="new-live-pill">
          <span className="new-pulse" />
          LIVE
        </div>
        <button className="new-ghost" type="button" onClick={onToggle}>
          {collapsed ? "타임라인 보기" : "타임라인 숨기기"}
        </button>
      </div>
      {!collapsed && (
        <div className="new-timeline-body">
          <div className="new-track">
            <div className="new-track-name">
              <strong>{selectedCamera?.name ?? "카메라 없음"}</strong>선택 카메라
            </div>
            <TimelineBar ranges={selectedRanges} motionEvents={motionEvents} dayStart={dayStart} dayEnd={dayEnd} cursorTs={cursorTs} />
          </div>
          <div className="new-track">
            <div className="new-track-name">
              <strong>전체 카메라</strong>{cameras.length > 0 ? "녹화 있음" : "녹화 없음"}
            </div>
            <TimelineBar ranges={selectedRanges} motionEvents={motionEvents} dayStart={dayStart} dayEnd={dayEnd} cursorTs={cursorTs} aggregate />
          </div>
          <div className="new-ticks">
            <span />
            {["00", "04", "08", "12", "16", "20", "24"].map((tick) => (
              <span key={tick}>{tick}</span>
            ))}
          </div>
        </div>
      )}
    </footer>
  );
}

function TimelineBar({
  ranges,
  motionEvents,
  dayStart,
  dayEnd,
  cursorTs,
  aggregate,
}: {
  ranges: TimelineRange[];
  motionEvents: Array<{ ts_start: number; ts_end: number | null }>;
  dayStart: number;
  dayEnd: number;
  cursorTs: number;
  aggregate?: boolean;
}) {
  const handleClick = (event: MouseEvent<HTMLDivElement>) => {
    event.currentTarget.blur();
  };
  return (
    <div className={cn("new-daybar", aggregate && "new-aggregate")} onClick={handleClick}>
      {ranges.map((range, index) => {
        const left = pctInDay(range.ts_start, dayStart, dayEnd);
        const right = pctInDay(range.ts_end, dayStart, dayEnd);
        return <span key={`${range.ts_start}-${index}`} className="new-chunk" style={{ left: `${left}%`, width: `${Math.max(right - left, 0.18)}%` }} />;
      })}
      {motionEvents.map((event, index) => {
        const left = pctInDay(event.ts_start, dayStart, dayEnd);
        const right = pctInDay(event.ts_end ?? event.ts_start + 5, dayStart, dayEnd);
        return <span key={`${event.ts_start}-${index}`} className="new-motion" style={{ left: `${left}%`, width: `${Math.max(right - left, 0.24)}%` }} />;
      })}
      <span className="new-cursor" style={{ left: `${pctInDay(cursorTs, dayStart, dayEnd)}%` }} />
    </div>
  );
}

function defaultLayout(cameras: Camera[]): MonitorLayoutItem[] {
  return cameras.map((camera, index) => ({
    i: camera.streamName,
    x: index === 0 ? 0 : 24 + ((index - 1) % 2) * 12,
    y: index === 0 ? 0 : Math.floor((index - 1) / 2) * 12,
    w: index === 0 ? 24 : 12,
    h: index === 0 ? 24 : 12,
    minW: 8,
    minH: 8,
  }));
}

function mergeWithCameras(saved: Array<{ i: string; x: number; y: number; w: number; h: number; minW?: number; minH?: number }>, cameras: Camera[]): MonitorLayoutItem[] {
  const savedMap = new Map(saved.map((item) => [item.i, item]));
  return cameras.map((camera, index) => savedMap.get(camera.streamName) ?? defaultLayout([camera]).map((item) => ({
    ...item,
    x: 24 + (index % 2) * 12,
    y: Math.floor(index / 2) * 12,
  }))[0]);
}

function clampLayout(layout: MonitorLayoutItem[]): MonitorLayoutItem[] {
  return layout.map((item) => {
    const minW = item.minW ?? 1;
    const minH = item.minH ?? 1;
    const w = Math.min(Math.max(item.w, minW), GRID_COLS);
    const h = Math.min(Math.max(item.h, minH), GRID_ROWS);
    return {
      ...item,
      w,
      h,
      x: Math.min(Math.max(item.x, 0), GRID_COLS - w),
      y: Math.min(Math.max(item.y, 0), GRID_ROWS - h),
    };
  });
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
  };
}

function pctInDay(ts: number, dayStart: number, dayEnd: number): number {
  const value = ((ts - dayStart) / (dayEnd - dayStart)) * 100;
  return Math.min(100, Math.max(0, value));
}

function formatClock(date: Date): string {
  return new Intl.DateTimeFormat("ko-KR", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

function formatShortTime(value: number): string {
  return new Intl.DateTimeFormat("ko-KR", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(new Date(value * 1000));
}
