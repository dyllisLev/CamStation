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
  layout: Layout[];
  onLayoutChange: (layout: Layout[]) => void;
}

export function CameraGrid({ cameras, motionCams, layout, onLayoutChange }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(window.innerWidth);
  const [containerHeight, setContainerHeight] = useState(0);

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

  return (
    <div ref={containerRef} style={{ width: '100%', flex: 1, minHeight: 0, overflow: 'hidden' }}>
      <GridLayout
        layout={layout}
        cols={COLS}
        rowHeight={ROW_HEIGHT}
        width={containerWidth}
        onLayoutChange={onLayoutChange}
        draggableHandle=".cam-drag-handle"
        style={{ background: '#111', minHeight: containerHeight }}
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
