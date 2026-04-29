import { useCameras } from '../hooks/useCameras';
import { useLayouts } from '../hooks/useLayouts';
import { useAllTimelines } from '../hooks/useAllTimelines';
import { CameraGrid } from '../components/CameraGrid';
import { format } from 'date-fns';

export function ViewerPage() {
  const cameras = useCameras();
  const { gridLayout, setGridLayout } = useLayouts(cameras);
  const today = format(new Date(), 'yyyy-MM-dd');
  const timelineData = useAllTimelines(cameras, today);

  const now = Date.now() / 1000;
  const motionCams = new Set(
    cameras
      .map(c => c.id)
      .filter(id => (timelineData[id]?.motion_events ?? []).some(e => now - e.ts_start < 10)),
  );

  if (gridLayout.length === 0) return null;

  return (
    <div style={{ width: '100vw', height: '100vh', background: '#111', overflow: 'hidden' }}>
      <CameraGrid
        cameras={cameras}
        motionCams={motionCams}
        layout={gridLayout}
        onLayoutChange={setGridLayout}
      />
    </div>
  );
}
