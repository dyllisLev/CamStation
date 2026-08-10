import {
  Camera as CameraIcon,
  ChevronLeft,
  ChevronRight,
  Cctv,
  Maximize2,
  Radio,
  RefreshCw,
  X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { TouchEvent } from "react";
import type { Camera } from "../app/api";
import { useCameras } from "../app/queries";
import { MobileViewerVideo } from "./viewer/MobileViewerVideo";
import {
  clampMobileViewerPage,
  mobileViewerPageAfterSwipe,
  mobileViewerPageCount,
  mobileViewerPageItems,
  mobileViewerSwipeDirection,
} from "./viewer/mobileViewerModel";
import "./viewer/mobileViewer.css";

type MobileViewerView =
  | { readonly screen: "grid" }
  | { readonly screen: "detail" | "fullscreen"; readonly streamName: string };

type IOSVideoElement = HTMLVideoElement & {
  webkitEnterFullscreen?: () => void;
};

type LockableScreenOrientation = ScreenOrientation & {
  lock?: (orientation: "landscape") => Promise<void>;
  unlock?: () => void;
};

const kstClock = new Intl.DateTimeFormat("ko-KR", {
  timeZone: "Asia/Seoul",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hourCycle: "h23",
});

export function ViewerPage() {
  const cameraQuery = useCameras();
  const cameras = useMemo(() => cameraQuery.data?.filter((camera) => camera.enabled) ?? [], [cameraQuery.data]);
  const [view, setView] = useState<MobileViewerView>({ screen: "grid" });
  const rootRef = useRef<HTMLDivElement>(null);
  const selectedIndex = view.screen === "grid"
    ? -1
    : cameras.findIndex((camera) => camera.streamName === view.streamName);
  const selectedCamera = selectedIndex >= 0 ? cameras[selectedIndex] : undefined;
  const onlineCount = cameras.filter((camera) => camera.state === "streaming").length;

  useMobileViewerTheme();

  useEffect(() => {
    if (view.screen === "grid" || !cameraQuery.isSuccess || selectedCamera) return;
    if (view.screen === "fullscreen") {
      unlockOrientation();
      if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
    }
    setView({ screen: "grid" });
  }, [cameraQuery.isSuccess, selectedCamera, view.screen]);

  useEffect(() => {
    const root = rootRef.current;
    return () => {
      unlockOrientation();
      if (document.fullscreenElement === root) void document.exitFullscreen().catch(() => undefined);
    };
  }, []);

  useEffect(() => {
    if (view.screen !== "fullscreen") return;
    const handleFullscreenChange = () => {
      if (document.fullscreenElement) return;
      unlockOrientation();
      setView((current) => current.screen === "fullscreen"
        ? { screen: "detail", streamName: current.streamName }
        : current);
    };
    document.addEventListener("fullscreenchange", handleFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", handleFullscreenChange);
  }, [view.screen]);

  const openFullscreen = useCallback((streamName: string, video: HTMLVideoElement | null) => {
    const iosVideo = video as IOSVideoElement | null;
    if (isIOSDevice() && iosVideo?.webkitEnterFullscreen) {
      iosVideo.webkitEnterFullscreen();
      return;
    }

    let fullscreenRequest: Promise<void> | undefined;
    try {
      fullscreenRequest = rootRef.current?.requestFullscreen?.();
    } catch {
      fullscreenRequest = undefined;
    }
    setView({ screen: "fullscreen", streamName });
    if (fullscreenRequest) {
      void fullscreenRequest.then(lockLandscape, () => undefined);
    }
  }, []);

  const closeFullscreen = useCallback((streamName: string) => {
    setView({ screen: "detail", streamName });
    unlockOrientation();
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined);
  }, []);

  const navigate = useCallback((screen: "detail" | "fullscreen", index: number) => {
    const camera = cameras[index];
    if (camera) setView({ screen, streamName: camera.streamName });
  }, [cameras]);

  return (
    <div ref={rootRef} className="mobile-viewer-root" data-screen={view.screen}>
      {view.screen !== "fullscreen" && (
        <MobileViewerHeader
          cameraCount={cameras.length}
          onlineCount={onlineCount}
          showCounts={Boolean(cameraQuery.data)}
        />
      )}

      {cameraQuery.isPending && !cameraQuery.data && (
        <MobileViewerState title="카메라를 불러오는 중입니다." />
      )}

      {cameraQuery.isError && !cameraQuery.data && (
        <MobileViewerState
          title="카메라 정보를 불러오지 못했습니다."
          detail="네트워크 연결을 확인한 뒤 다시 시도하세요."
          retry={() => void cameraQuery.refetch()}
        />
      )}

      {!cameraQuery.isPending && cameraQuery.data !== undefined && cameras.length === 0 && (
        <MobileViewerState title="표시할 카메라가 없습니다." detail="활성화된 카메라가 등록되면 여기에 표시됩니다." />
      )}

      {cameras.length > 0 && view.screen === "grid" && (
        <MobileViewerGrid cameras={cameras} onCameraSelect={(camera) => {
          setView({ screen: "detail", streamName: camera.streamName });
        }} />
      )}

      {selectedCamera && view.screen === "detail" && (
        <MobileViewerDetail
          camera={selectedCamera}
          cameraIndex={selectedIndex}
          cameraCount={cameras.length}
          onClose={() => setView({ screen: "grid" })}
          onNavigate={(index) => navigate("detail", index)}
          onFullscreen={(video) => openFullscreen(selectedCamera.streamName, video)}
        />
      )}

      {selectedCamera && view.screen === "fullscreen" && (
        <MobileViewerFullscreen
          camera={selectedCamera}
          cameraIndex={selectedIndex}
          cameraCount={cameras.length}
          onClose={() => closeFullscreen(selectedCamera.streamName)}
          onNavigate={(index) => navigate("fullscreen", index)}
        />
      )}
    </div>
  );
}

function MobileViewerHeader({
  cameraCount,
  onlineCount,
  showCounts,
}: {
  readonly cameraCount: number;
  readonly onlineCount: number;
  readonly showCounts: boolean;
}) {
  return (
    <header className="mobile-viewer-header">
      <span className="mobile-viewer-brand-mark" aria-hidden="true"><Cctv size={16} /></span>
      <strong className="mobile-viewer-title">CamStation</strong>
      {showCounts && cameraCount > 0 && (
        <span className="mobile-viewer-status-pill mobile-viewer-status-online" aria-label={`${cameraCount}대 중 ${onlineCount}대 온라인`}>
          <span aria-hidden="true" />
          {onlineCount}/{cameraCount}
        </span>
      )}
      <span className="mobile-viewer-status-pill mobile-viewer-status-recording" aria-label="녹화 모니터링">
        <Radio size={11} aria-hidden="true" />
        REC
      </span>
    </header>
  );
}

function MobileViewerGrid({
  cameras,
  onCameraSelect,
}: {
  readonly cameras: readonly Camera[];
  readonly onCameraSelect: (camera: Camera) => void;
}) {
  const totalPages = mobileViewerPageCount(cameras.length);
  const [currentPage, setCurrentPage] = useState(0);
  const touchStartX = useRef<number | null>(null);
  const suppressClickUntil = useRef(0);

  useEffect(() => {
    setCurrentPage((page) => clampMobileViewerPage(page, totalPages));
  }, [totalPages]);

  const handleTouchStart = (event: TouchEvent<HTMLDivElement>) => {
    touchStartX.current = event.touches[0].clientX;
  };

  const handleTouchEnd = (event: TouchEvent<HTMLDivElement>) => {
    if (touchStartX.current === null) return;
    const deltaX = event.changedTouches[0].clientX - touchStartX.current;
    touchStartX.current = null;
    if (mobileViewerSwipeDirection(deltaX)) suppressClickUntil.current = Date.now() + 400;
    setCurrentPage((page) => mobileViewerPageAfterSwipe(page, totalPages, deltaX));
  };

  const selectCamera = (camera: Camera) => {
    if (Date.now() < suppressClickUntil.current) return;
    onCameraSelect(camera);
  };

  return (
    <main className="mobile-viewer-grid-shell" aria-label="카메라 목록">
      <div
        className="mobile-viewer-grid-viewport"
        onTouchStart={handleTouchStart}
        onTouchEnd={handleTouchEnd}
        onTouchCancel={() => {
          touchStartX.current = null;
        }}
      >
        <div
          className="mobile-viewer-grid-track"
          style={{
            width: `${totalPages * 100}%`,
            transform: `translate3d(-${currentPage * (100 / totalPages)}%, 0, 0)`,
          }}
        >
          {Array.from({ length: totalPages }, (_, page) => {
            const active = page === currentPage;
            return (
              <section
                key={page}
                className="mobile-viewer-grid-page"
                aria-label={`${page + 1}/${totalPages} 페이지`}
                aria-hidden={!active}
                style={{ width: `${100 / totalPages}%` }}
              >
                {mobileViewerPageItems(cameras, page).map((camera) => (
                  <MobileViewerTile
                    key={camera.streamName}
                    camera={camera}
                    active={active}
                    onClick={() => selectCamera(camera)}
                  />
                ))}
              </section>
            );
          })}
        </div>
      </div>

      {totalPages > 1 && (
        <nav className="mobile-viewer-page-indicator" aria-label="카메라 페이지 선택">
          {Array.from({ length: totalPages }, (_, page) => (
            <button
              key={page}
              type="button"
              className={page === currentPage ? "mobile-viewer-page-dot mobile-viewer-page-dot-active" : "mobile-viewer-page-dot"}
              aria-label={`${page + 1} 페이지`}
              aria-current={page === currentPage ? "page" : undefined}
              onClick={() => setCurrentPage(page)}
            />
          ))}
        </nav>
      )}
    </main>
  );
}

function MobileViewerTile({
  camera,
  active,
  onClick,
}: {
  readonly camera: Camera;
  readonly active: boolean;
  readonly onClick: () => void;
}) {
  const status = mobileCameraStatus(camera.state);
  return (
    <button
      type="button"
      className="mobile-viewer-tile"
      data-camera-state={status.kind}
      tabIndex={active ? 0 : -1}
      aria-label={`${camera.name} 자세히 보기, ${status.label}`}
      onClick={onClick}
    >
      {active && <MobileViewerVideo camera={camera} />}
      <span className="mobile-viewer-tile-footer">
        <span className="mobile-viewer-camera-state" aria-hidden="true" />
        <span>{camera.name}</span>
      </span>
    </button>
  );
}

function MobileViewerDetail({
  camera,
  cameraIndex,
  cameraCount,
  onClose,
  onNavigate,
  onFullscreen,
}: {
  readonly camera: Camera;
  readonly cameraIndex: number;
  readonly cameraCount: number;
  readonly onClose: () => void;
  readonly onNavigate: (index: number) => void;
  readonly onFullscreen: (video: HTMLVideoElement | null) => void;
}) {
  const detailRef = useRef<HTMLElement>(null);
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <main ref={detailRef} className="mobile-viewer-detail">
      <div className="mobile-viewer-detail-stage">
        <MobileViewerVideo camera={camera} focused announceStatus />
        <div className="mobile-viewer-camera-label"><CameraIcon size={13} aria-hidden="true" />{camera.name}</div>
        <div className="mobile-viewer-recording-label"><span aria-hidden="true" />REC</div>
        <time className="mobile-viewer-clock" dateTime={now.toISOString()}>{kstClock.format(now)} KST</time>
      </div>
      <MobileViewerControls
        cameraName={camera.name}
        previousDisabled={cameraIndex <= 0}
        nextDisabled={cameraIndex >= cameraCount - 1}
        onClose={onClose}
        onPrevious={() => onNavigate(cameraIndex - 1)}
        onNext={() => onNavigate(cameraIndex + 1)}
        onFullscreen={() => onFullscreen(detailRef.current?.querySelector("video") ?? null)}
      />
    </main>
  );
}

function MobileViewerControls({
  cameraName,
  previousDisabled,
  nextDisabled,
  onClose,
  onPrevious,
  onNext,
  onFullscreen,
}: {
  readonly cameraName: string;
  readonly previousDisabled: boolean;
  readonly nextDisabled: boolean;
  readonly onClose: () => void;
  readonly onPrevious: () => void;
  readonly onNext: () => void;
  readonly onFullscreen: () => void;
}) {
  return (
    <nav className="mobile-viewer-controls" aria-label="카메라 상세 제어">
      <button type="button" onClick={onClose} aria-label="카메라 목록으로 돌아가기"><X size={19} /></button>
      <button type="button" onClick={onPrevious} disabled={previousDisabled} aria-label="이전 카메라"><ChevronLeft size={21} /></button>
      <strong title={cameraName}>{cameraName}</strong>
      <button type="button" onClick={onNext} disabled={nextDisabled} aria-label="다음 카메라"><ChevronRight size={21} /></button>
      <button type="button" className="mobile-viewer-control-accent" onClick={onFullscreen} aria-label="전체화면">
        <Maximize2 size={18} />
      </button>
    </nav>
  );
}

function MobileViewerFullscreen({
  camera,
  cameraIndex,
  cameraCount,
  onClose,
  onNavigate,
}: {
  readonly camera: Camera;
  readonly cameraIndex: number;
  readonly cameraCount: number;
  readonly onClose: () => void;
  readonly onNavigate: (index: number) => void;
}) {
  const [closeVisible, setCloseVisible] = useState(true);
  const closeTimer = useRef<number | undefined>(undefined);

  const revealClose = () => {
    window.clearTimeout(closeTimer.current);
    setCloseVisible(true);
    closeTimer.current = window.setTimeout(() => setCloseVisible(false), 2000);
  };

  useEffect(() => {
    revealClose();
    return () => window.clearTimeout(closeTimer.current);
  }, [camera.streamName]);

  return (
    <main className="mobile-viewer-fullscreen" onClick={revealClose}>
      <div className="mobile-viewer-fullscreen-stage">
        <MobileViewerVideo camera={camera} focused announceStatus />
        {closeVisible && (
          <button
            type="button"
            className="mobile-viewer-fullscreen-close"
            aria-label="전체화면 닫기"
            onClick={(event) => {
              event.stopPropagation();
              onClose();
            }}
          >
            <X size={20} />
          </button>
        )}
      </div>
      <nav className="mobile-viewer-fullscreen-nav" aria-label="전체화면 카메라 선택" onClick={(event) => event.stopPropagation()}>
        <button type="button" disabled={cameraIndex <= 0} onClick={() => onNavigate(cameraIndex - 1)} aria-label="이전 카메라">
          <ChevronLeft size={22} />
        </button>
        <strong>{camera.name}</strong>
        <button type="button" disabled={cameraIndex >= cameraCount - 1} onClick={() => onNavigate(cameraIndex + 1)} aria-label="다음 카메라">
          <ChevronRight size={22} />
        </button>
      </nav>
    </main>
  );
}

function MobileViewerState({
  title,
  detail,
  retry,
}: {
  readonly title: string;
  readonly detail?: string;
  readonly retry?: () => void;
}) {
  return (
    <main className="mobile-viewer-empty" role="status">
      <Cctv size={30} aria-hidden="true" />
      <strong>{title}</strong>
      {detail && <span>{detail}</span>}
      {retry && (
        <button type="button" onClick={retry}><RefreshCw size={15} />다시 시도</button>
      )}
    </main>
  );
}

function mobileCameraStatus(state: string) {
  if (state === "streaming") return { kind: "online", label: "온라인" } as const;
  if (state === "degraded") return { kind: "degraded", label: "점검 필요" } as const;
  return { kind: "offline", label: "오프라인" } as const;
}

function isIOSDevice() {
  return /iPhone|iPad|iPod/u.test(navigator.userAgent)
    || (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
}

async function lockLandscape() {
  try {
    const orientation = screen.orientation as LockableScreenOrientation | undefined;
    await orientation?.lock?.("landscape");
  } catch {
    // Orientation locking is optional and commonly denied outside installed apps.
  }
}

function unlockOrientation() {
  try {
    const orientation = screen.orientation as LockableScreenOrientation | undefined;
    orientation?.unlock?.();
  } catch {
    // Best-effort cleanup for browsers without the orientation API.
  }
}

function useMobileViewerTheme() {
  useEffect(() => {
    let theme = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    const created = !theme;
    const previous = theme?.content;
    if (!theme) {
      theme = document.createElement("meta");
      theme.name = "theme-color";
      document.head.appendChild(theme);
    }
    theme.content = "#071017";

    return () => {
      if (created) theme?.remove();
      else if (theme && previous !== undefined) theme.content = previous;
    };
  }, []);
}
