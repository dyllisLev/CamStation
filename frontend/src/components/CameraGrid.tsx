import { useState, useRef, useEffect } from 'react';
import GridLayout from 'react-grid-layout';
import type { Layout } from 'react-grid-layout';
import 'react-grid-layout/css/styles.css';
import 'react-resizable/css/styles.css';
import { CameraTile } from './CameraTile';
import type { Camera } from '../types';

const COLS = 12;
const BASE_ROW_HEIGHT = 60;

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
    setContainerWidth(el.offsetWidth);
    setContainerHeight(el.offsetHeight);
    const ro = new ResizeObserver(() => {
      setContainerWidth(el.offsetWidth);
      setContainerHeight(el.offsetHeight);
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Shrink rowHeight only when tiles would overflow — never cap tile positions
  const maxItemRows = Math.max(1, ...layout.map(item => item.y + item.h));
  const rowHeight = containerHeight > 0
    ? Math.min(BASE_ROW_HEIGHT, Math.floor(containerHeight / maxItemRows))
    : BASE_ROW_HEIGHT;

  return (
    <div ref={containerRef} style={{ position: 'absolute', inset: 0, overflow: 'hidden' }}>
      {containerHeight > 0 && (
        <GridLayout
          layout={layout}
          cols={COLS}
          rowHeight={rowHeight}
          width={containerWidth}
          onLayoutChange={onLayoutChange}
          draggableHandle=".cam-drag-handle"
          resizeHandles={['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']}
          margin={[2, 2]}
          containerPadding={[0, 0]}
          style={{ background: '#111' }}
        >
          {cameras.map(cam => (
            <div key={cam.id} style={{ position: 'relative', overflow: 'hidden', border: '1px solid #2a2a2a' }}>
              <CameraTile camera={cam} hasMotion={motionCams.has(cam.id)} />
            </div>
          ))}
        </GridLayout>
      )}
    </div>
  );
}
