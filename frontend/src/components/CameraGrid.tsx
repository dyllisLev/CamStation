import { useState, useRef, useEffect, useMemo } from 'react';
import GridLayout from 'react-grid-layout';
import type { Layout } from 'react-grid-layout';
import 'react-grid-layout/css/styles.css';
import 'react-resizable/css/styles.css';
import { CameraTile } from './CameraTile';
import type { Camera } from '../types';

const COLS = 12;
const ROW_HEIGHT = 60;

interface Props {
  cameras: Camera[];
  motionCams: Set<string>;
  layout: Layout[];
  onLayoutChange: (layout: Layout[]) => void;
}

export function CameraGrid({ cameras, motionCams, layout, onLayoutChange }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(window.innerWidth);
  const [containerHeight, setContainerHeight] = useState(window.innerHeight - 60);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      const rect = entries[0]?.contentRect;
      if (rect?.width) setContainerWidth(rect.width);
      if (rect?.height) setContainerHeight(rect.height);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Max rows that fit in the container — tiles cannot exceed this boundary
  const maxRows = Math.max(1, Math.floor(containerHeight / ROW_HEIGHT));

  // Cap layout items so they never render outside the container
  const boundedLayout = useMemo(() =>
    layout.map(item => {
      const y = Math.min(item.y, maxRows - 1);
      const h = Math.min(item.h, maxRows - y);
      return { ...item, y, h };
    }),
    [layout, maxRows],
  );

  return (
    <div ref={containerRef} style={{ position: 'absolute', inset: 0, overflow: 'hidden' }}>
      <GridLayout
        layout={boundedLayout}
        cols={COLS}
        rowHeight={ROW_HEIGHT}
        maxRows={maxRows}
        width={containerWidth}
        onLayoutChange={onLayoutChange}
        draggableHandle=".cam-drag-handle"
        style={{ background: '#111', height: containerHeight }}
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
