import { useCameras } from '../hooks/useCameras';
import { useLayouts } from '../hooks/useLayouts';
import { useAllTimelines } from '../hooks/useAllTimelines';
import { CameraGrid } from '../components/CameraGrid';
import { format } from 'date-fns';

export function ViewerPage() {
  const cameras = useCameras();
  const { layouts, currentId, gridLayout, setGridLayout, loadLayout } = useLayouts(cameras);
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
      {layouts.length > 1 && (
        <div style={{
          position: 'absolute', top: 8, left: 8, zIndex: 10,
        }}>
          <select
            value={currentId ?? ''}
            onChange={e => loadLayout(e.target.value, cameras)}
            style={{
              background: 'rgba(0,0,0,0.6)', color: '#ccc', border: '1px solid #444',
              borderRadius: 4, fontSize: 11, padding: '3px 6px', cursor: 'pointer',
              outline: 'none',
            }}
          >
            {layouts.map(l => (
              <option key={l.id} value={l.id}>{l.name}</option>
            ))}
          </select>
        </div>
      )}
      <CameraGrid
        cameras={cameras}
        motionCams={motionCams}
        layout={gridLayout}
        onLayoutChange={setGridLayout}
        readOnly
      />
    </div>
  );
}
