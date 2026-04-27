import { useState, useRef, useEffect } from 'react';
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
  height: number;
  layout: Layout[];
  onLayoutChange: (layout: Layout[]) => void;
}

export function CameraGrid({ cameras, motionCams, height, layout, onLayoutChange }: Props) {
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
