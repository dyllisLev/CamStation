import { useState, useCallback, useRef, useEffect } from 'react';
import GridLayout from 'react-grid-layout';
import type { Layout } from 'react-grid-layout';
import 'react-grid-layout/css/styles.css';
import 'react-resizable/css/styles.css';
import { CameraTile } from './CameraTile';
import type { Camera } from '../types';

const STORAGE_KEY = 'camstation-layout';
const COLS = 12;
const ROW_HEIGHT = 60;

function defaultLayout(cameras: Camera[]): Layout[] {
  return cameras.map((c, i) => ({
    i: c.id,
    x: i === 0 ? 0 : 6 + ((i - 1) % 2) * 3,
    y: i === 0 ? 0 : Math.floor((i - 1) / 2) * 2,
    w: i === 0 ? 6 : 3,
    h: i === 0 ? 4 : 2,
    minW: 2, minH: 2,
  }));
}

interface Props {
  cameras: Camera[];
  motionCams: Set<string>;
  height: number;
}

export function CameraGrid({ cameras, motionCams, height }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(window.innerWidth);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      const w = entries[0]?.contentRect.width;
      if (w) setContainerWidth(w);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const [layout, setLayout] = useState<Layout[]>(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (!saved) return defaultLayout(cameras);
    const parsed: Layout[] = JSON.parse(saved);
    const savedIds = new Set(parsed.map(l => l.i));
    const allPresent = cameras.every(c => savedIds.has(c.id));
    return allPresent ? parsed : defaultLayout(cameras);
  });

  useEffect(() => {
    if (cameras.length === 0) return;
    const savedIds = new Set(layout.map(l => l.i));
    const allPresent = cameras.every(c => savedIds.has(c.id));
    if (!allPresent) {
      const fresh = defaultLayout(cameras);
      setLayout(fresh);
      localStorage.setItem(STORAGE_KEY, JSON.stringify(fresh));
    }
  }, [cameras]);

  const onLayoutChange = useCallback((newLayout: Layout[]) => {
    setLayout(newLayout);
    localStorage.setItem(STORAGE_KEY, JSON.stringify(newLayout));
  }, []);

  return (
    <div ref={containerRef} style={{ width: '100%', height }}>
      <GridLayout
        layout={layout}
        cols={COLS}
        rowHeight={ROW_HEIGHT}
        width={containerWidth}
        onLayoutChange={onLayoutChange}
        draggableHandle=".cam-drag-handle"
        style={{ background: '#111', minHeight: height }}
      >
        {cameras.map(cam => (
          <div key={cam.id} style={{ position: 'relative', overflow: 'hidden', border: '1px solid #2a2a2a' }}>
            <CameraTile camera={cam} hasMotion={motionCams.has(cam.id)} />
          </div>
        ))}
      </GridLayout>
    </div>
  );
}
