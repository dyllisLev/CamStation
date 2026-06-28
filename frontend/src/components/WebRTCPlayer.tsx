import { useEffect, useRef, useState, useCallback } from 'react';
import { useMSE } from '../hooks/useMSE';

interface Props {
  camId: string;
  style?: React.CSSProperties;
}

const debugMode = new URLSearchParams(location.search).has('debug');

interface DebugInfo {
  readyState: number;
  latency: string;
  buffered: string;
}

// --- Zoom/Pan ---
const ZOOM_STORAGE_KEY = 'camviewer-zoom';
const MIN_SCALE = 1;
const MAX_SCALE = 8;
const ZOOM_STEP = 1.15;

type ZoomState = { scale: number; tx: number; ty: number };

function loadZoom(camId: string): ZoomState {
  try {
    const raw = localStorage.getItem(ZOOM_STORAGE_KEY);
    if (!raw) return { scale: 1, tx: 0, ty: 0 };
    const map = JSON.parse(raw);
    return map[camId] ?? { scale: 1, tx: 0, ty: 0 };
  } catch { return { scale: 1, tx: 0, ty: 0 }; }
}

function saveZoom(camId: string, s: ZoomState) {
  try {
    const raw = localStorage.getItem(ZOOM_STORAGE_KEY);
    const map = raw ? JSON.parse(raw) : {};
    map[camId] = s;
    localStorage.setItem(ZOOM_STORAGE_KEY, JSON.stringify(map));
  } catch { /* */ }
}

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v));
}

function clampTx(tx: number, ty: number, scale: number, w: number, h: number) {
  if (scale <= 1) return { tx: 0, ty: 0 };
  const maxTx = ((scale - 1) * w) / 2;
  const maxTy = ((scale - 1) * h) / 2;
  return { tx: clamp(tx, -maxTx, maxTx), ty: clamp(ty, -maxTy, maxTy) };
}

export function WebRTCPlayer({ camId, style }: Props) {
  const { videoRef, connected } = useMSE(camId);
  const [debug, setDebug] = useState<DebugInfo | null>(null);
  const rafRef = useRef<number>(0);

  const containerRef = useRef<HTMLDivElement>(null);
  const stateRef = useRef<ZoomState>(loadZoom(camId));
  const [, forceRender] = useState(0);
  const dragging = useRef(false);
  const dragStart = useRef({ x: 0, y: 0, tx: 0, ty: 0 });

  // Sync state → DOM & localStorage via ref + render trigger
  const updateState = useCallback((patch: Partial<ZoomState>) => {
    const prev = stateRef.current;
    const next = { ...prev, ...patch };
    next.scale = clamp(next.scale, MIN_SCALE, MAX_SCALE);
    if (next.scale <= 1) { next.tx = 0; next.ty = 0; }
    stateRef.current = next;
    saveZoom(camId, next);
    forceRender(n => n + 1);
  }, [camId]);

  // Reset on camId change
  useEffect(() => {
    stateRef.current = loadZoom(camId);
    forceRender(n => n + 1);
  }, [camId]);

  // Debug overlay
  useEffect(() => {
    if (!debugMode) return;
    const update = () => {
      const v = videoRef.current;
      if (v) {
        const bufferedEnd = v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0;
        const latency = (bufferedEnd - v.currentTime).toFixed(2);
        const ranges = Array.from({ length: v.buffered.length }, (_, i) =>
          `${v.buffered.start(i).toFixed(1)}-${v.buffered.end(i).toFixed(1)}`
        ).join(' ');
        setDebug({ readyState: v.readyState, latency, buffered: ranges || '—' });
      }
      rafRef.current = requestAnimationFrame(update);
    };
    rafRef.current = requestAnimationFrame(update);
    return () => cancelAnimationFrame(rafRef.current);
  }, [videoRef]);

  // Wheel zoom centered on cursor
  const handleWheel = useCallback((e: React.WheelEvent) => {
    e.preventDefault();
    const container = containerRef.current;
    if (!container) return;
    const rect = container.getBoundingClientRect();
    const w = rect.width, h = rect.height;
    const mx = e.clientX - rect.left - w / 2;
    const my = e.clientY - rect.top - h / 2;

    const { scale: prevScale, tx: prevTx, ty: prevTy } = stateRef.current;
    const factor = e.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP;
    const nextScale = clamp(prevScale * factor, MIN_SCALE, MAX_SCALE);

    if (nextScale <= 1) {
      updateState({ scale: 1, tx: 0, ty: 0 });
      return;
    }

    // Zoom centered on cursor position
    const ratio = nextScale / prevScale;
    const ntx = mx - ratio * (mx - prevTx);
    const nty = my - ratio * (my - prevTy);
    const c = clampTx(ntx, nty, nextScale, w, h);
    updateState({ scale: nextScale, tx: c.tx, ty: c.ty });
  }, [updateState]);

  // Drag pan
  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    if (stateRef.current.scale <= 1) return;
    e.preventDefault();
    dragging.current = true;
    dragStart.current = { x: e.clientX, y: e.clientY, tx: stateRef.current.tx, ty: stateRef.current.ty };

    const onMove = (ev: MouseEvent) => {
      if (!dragging.current) return;
      const container = containerRef.current;
      if (!container) return;
      const rect = container.getBoundingClientRect();
      const dx = ev.clientX - dragStart.current.x;
      const dy = ev.clientY - dragStart.current.y;
      const c = clampTx(
        dragStart.current.tx + dx,
        dragStart.current.ty + dy,
        stateRef.current.scale,
        rect.width, rect.height,
      );
      stateRef.current = { ...stateRef.current, ...c };
      saveZoom(camId, stateRef.current);
      forceRender(n => n + 1);
    };

    const onUp = () => {
      dragging.current = false;
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };

    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, [camId]);

  // Double-click reset
  const handleDoubleClick = useCallback(() => {
    updateState({ scale: 1, tx: 0, ty: 0 });
  }, [updateState]);

  const { scale, tx, ty } = stateRef.current;
  const isZoomed = scale > 1;
  const cursor = isZoomed ? 'grab' : 'default';

  return (
    <div
      ref={containerRef}
      style={{
        position: 'relative',
        width: '100%',
        height: '100%',
        background: '#05070a',
        overflow: 'hidden',
        cursor,
        ...style,
      }}
      onWheel={handleWheel}
      onMouseDown={handleMouseDown}
      onDoubleClick={handleDoubleClick}
    >
      <video
        ref={videoRef}
        autoPlay
        muted
        playsInline
        style={{
          width: '100%',
          height: '100%',
          objectFit: 'contain',
          transform: `scale(${scale}) translate(${tx / scale}px, ${ty / scale}px)`,
          transformOrigin: 'center center',
          willChange: 'transform',
          pointerEvents: 'none',
        }}
      />
      {!connected && (
        <div style={{
          position: 'absolute', inset: 0,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: '#444', fontSize: 12,
        }}>
          연결 중...
        </div>
      )}
      {isZoomed && (
        <div style={{
          position: 'absolute', bottom: 6, right: 8,
          background: 'rgba(0,0,0,0.65)', color: '#fff', fontSize: 11,
          fontFamily: 'var(--new-font-mono, monospace)',
          padding: '2px 7px', borderRadius: 4,
          pointerEvents: 'none', zIndex: 3,
        }}>
          {scale.toFixed(1)}×
        </div>
      )}
      {debugMode && debug && (
        <div style={{
          position: 'absolute', bottom: 4, left: 4,
          background: 'rgba(0,0,0,0.7)', color: '#0f0', fontSize: 10,
          fontFamily: 'monospace', padding: '2px 5px', borderRadius: 3,
          pointerEvents: 'none', lineHeight: 1.5,
        }}>
          <div>rs:{debug.readyState} lat:{debug.latency}s</div>
          <div>buf:{debug.buffered}</div>
        </div>
      )}
    </div>
  );
}
