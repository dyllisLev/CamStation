import { useCameras } from '../hooks/useCameras';
import { useAllTimelines } from '../hooks/useAllTimelines';
import { CameraGrid } from '../components/CameraGrid';
import { Timeline } from '../components/Timeline';
import { format } from 'date-fns';

export function LiveView() {
  const cameras = useCameras();
  const today = format(new Date(), 'yyyy-MM-dd');
  const timelineData = useAllTimelines(cameras, today);

  const now = Date.now() / 1000;
  const motionCams = new Set(
    cameras
      .map(c => c.id)
      .filter(id => (timelineData[id]?.motion_events ?? []).some(e => now - e.ts_start < 10))
  );

  const handleSeek = (camId: string, ts: number) => {
    const segs = timelineData[camId]?.segments ?? [];
    const seg = [...segs].reverse().find(s => s.ts_start <= ts);
    if (!seg) return;
    const url = `/api/recordings/${encodeURIComponent(camId)}/${seg.date}/${seg.filename}`;
    window.open(url, '_blank');
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: '#111', color: '#eee' }}>
      <div style={{ background: '#242424', borderBottom: '1px solid #333', padding: '6px 12px', display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
        <span style={{ fontSize: 13, fontWeight: 'bold', color: '#64b5f6' }}>📷 CamStation</span>
        <span style={{ marginLeft: 'auto', background: '#c62828', color: '#fff', fontSize: 10, fontWeight: 'bold', padding: '2px 7px', borderRadius: 10 }}>● LIVE</span>
      </div>
      <div style={{ flex: 1, overflow: 'hidden' }}>
        <CameraGrid cameras={cameras} motionCams={motionCams} height={window.innerHeight - 150} />
      </div>
      <Timeline
        cameras={cameras}
        timelineData={timelineData}
        date={today}
        isLive={true}
        onSeek={handleSeek}
      />
    </div>
  );
}
