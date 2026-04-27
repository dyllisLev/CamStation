import { useState, useCallback } from 'react';
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
    x: i === 0 ? 0 : ((i - 1) % 3) * 3 + 6,
    y: i === 0 ? 0 : Math.floor((i - 1) / 3) * 2,
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
  const saved = localStorage.getItem(STORAGE_KEY);
  const [layout, setLayout] = useState<Layout[]>(
    saved ? JSON.parse(saved) : defaultLayout(cameras)
  );

  const onLayoutChange = useCallback((newLayout: Layout[]) => {
    setLayout(newLayout);
    localStorage.setItem(STORAGE_KEY, JSON.stringify(newLayout));
  }, []);

  return (
    <GridLayout
      layout={layout}
      cols={COLS}
      rowHeight={ROW_HEIGHT}
      width={window.innerWidth}
      onLayoutChange={onLayoutChange}
      draggableHandle=".cam-drag-handle"
      style={{ background: '#111', minHeight: height }}
    >
      {cameras.map(cam => (
        <div key={cam.id} style={{ position: 'relative', overflow: 'hidden', border: '1px solid #2a2a2a' }}>
          <div className="cam-drag-handle" style={{ position: 'absolute', inset: 0, zIndex: 1, cursor: 'move' }} />
          <CameraTile camera={cam} hasMotion={motionCams.has(cam.id)} />
        </div>
      ))}
    </GridLayout>
  );
}
