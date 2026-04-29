import React, { useState, useRef, useEffect } from 'react';
import GridLayout from 'react-grid-layout';
import type { Layout } from 'react-grid-layout';
import 'react-grid-layout/css/styles.css';
import 'react-resizable/css/styles.css';
import { CameraTile } from './CameraTile';
import type { Camera } from '../types';

const COLS = 12;
const BASE_ROW_HEIGHT = 20;

interface Props {
  cameras: Camera[];
  motionCams: Set<string>;
  layout: Layout[];
  onLayoutChange: (layout: Layout[]) => void;
  readOnly?: boolean;
}

const H = 8;  // edge handle thickness px
const C = 14; // corner handle size px

function handleStyle(axis: string): React.CSSProperties {
  const base: React.CSSProperties = { position: 'absolute', zIndex: 10 };
  switch (axis) {
    case 'n':  return { ...base, top: 0, left: C, right: C, height: H, cursor: 'ns-resize' };
    case 's':  return { ...base, bottom: 0, left: C, right: C, height: H, cursor: 'ns-resize' };
    case 'e':  return { ...base, right: 0, top: C, bottom: C, width: H, cursor: 'ew-resize' };
    case 'w':  return { ...base, left: 0, top: C, bottom: C, width: H, cursor: 'ew-resize' };
    case 'ne': return { ...base, top: 0, right: 0, width: C, height: C, cursor: 'ne-resize' };
    case 'nw': return { ...base, top: 0, left: 0, width: C, height: C, cursor: 'nw-resize' };
    case 'se': return { ...base, bottom: 0, right: 0, width: C, height: C, cursor: 'se-resize' };
    case 'sw': return { ...base, bottom: 0, left: 0, width: C, height: C, cursor: 'sw-resize' };
    default:   return base;
  }
}

const ResizeHandle = React.forwardRef<HTMLDivElement, { handleAxis?: string }>(
  ({ handleAxis = 'se', ...rest }, ref) => (
    <div
      ref={ref}
      {...rest}
      style={handleStyle(handleAxis)}
      className={`cam-resize-handle cam-resize-handle-${handleAxis}`}
    />
  )
);

export function CameraGrid({ cameras, motionCams, layout, onLayoutChange, readOnly }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [containerWidth, setContainerWidth] = useState(window.innerWidth);
  const [containerHeight, setContainerHeight] = useState(0);
  const [focusedCamId, setFocusedCamId] = useState<string | null>(null);

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

  useEffect(() => {
    if (!focusedCamId) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFocusedCamId(null);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [focusedCamId]);

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
          isDraggable={!readOnly}
          isResizable={!readOnly}
          draggableHandle=".cam-drag-handle"
          resizeHandles={['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']}
          resizeHandle={<ResizeHandle />}
          margin={[2, 2]}
          containerPadding={[0, 0]}
          style={{ background: '#111' }}
        >
          {cameras.map(cam => {
            const focused = cam.id === focusedCamId;
            return (
              <div key={cam.id} style={{ position: 'relative', overflow: 'hidden', border: '1px solid #2a2a2a' }}>
                <div
                  style={focused ? {
                    position: 'fixed', inset: 0, zIndex: 1000,
                    background: '#000', display: 'flex', flexDirection: 'column',
                  } : { width: '100%', height: '100%' }}
                  onDoubleClick={() => setFocusedCamId(focused ? null : cam.id)}
                >
                  <CameraTile camera={cam} hasMotion={motionCams.has(cam.id)} />
                </div>
              </div>
            );
          })}
        </GridLayout>
      )}
    </div>
  );
}
