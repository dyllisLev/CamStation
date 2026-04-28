import { useState, useEffect } from 'react';
import { useCameras } from '../hooks/useCameras';
import { useAllTimelines } from '../hooks/useAllTimelines';
import { useLayouts } from '../hooks/useLayouts';
import { CameraGrid } from '../components/CameraGrid';
import { LayoutDropdown } from '../components/LayoutDropdown';
import { Timeline } from '../components/Timeline';
import { format } from 'date-fns';

interface Props {
  onNavigate: (page: 'recordings' | 'settings') => void;
}

export function LiveView({ onNavigate }: Props) {
  const cameras = useCameras();
  const today = format(new Date(), 'yyyy-MM-dd');
  const timelineData = useAllTimelines(cameras, today);
  const {
    layouts, currentId, gridLayout, isDirty, timelineCollapsed,
    setGridLayout, loadLayout, saveLayout, saveAsLayout, cancelEdit, deleteLayoutById,
    toggleTimelineCollapsed,
  } = useLayouts(cameras);

  const [isFullscreen, setIsFullscreen] = useState(false);

  useEffect(() => {
    const handler = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', handler);
    return () => document.removeEventListener('fullscreenchange', handler);
  }, []);

  const toggleFullscreen = () => {
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen();
    } else {
      document.exitFullscreen();
    }
  };

  const now = Date.now() / 1000;
  const motionCams = new Set(
    cameras
      .map(c => c.id)
      .filter(id => (timelineData[id]?.motion_events ?? []).some(e => now - e.ts_start < 10)),
  );

  const handleSeek = (camId: string, ts: number) => {
    const segs = timelineData[camId]?.segments ?? [];
    const seg = [...segs].reverse().find(s => s.ts_start <= ts);
    if (!seg) return;
    const url = `/api/recordings/${encodeURIComponent(camId)}/${seg.date}/${seg.filename}`;
    window.open(url, '_blank');
  };

  return (
    <div style={{ display: 'grid', gridTemplateRows: 'auto minmax(0, 1fr) auto', height: '100vh', background: '#111', color: '#eee' }}>
      <div style={{
        background: '#242424', borderBottom: '1px solid #333',
        padding: '6px 12px', display: 'flex', alignItems: 'center', gap: 10,
      }}>
        <span style={{ fontSize: 13, fontWeight: 'bold', color: '#64b5f6' }}>📷 CamStation</span>
        <LayoutDropdown
          layouts={layouts}
          currentId={currentId}
          isDirty={isDirty}
          onLoad={id => loadLayout(id, cameras)}
          onSave={saveLayout}
          onSaveAs={saveAsLayout}
          onCancel={cancelEdit}
          onDelete={deleteLayoutById}
        />
        <button
          onClick={() => onNavigate('recordings')}
          style={{ background: 'none', border: 'none', color: '#777', fontSize: 11, padding: '3px 8px', borderRadius: 4, cursor: 'pointer' }}
        >녹화목록</button>
        <button
          onClick={() => onNavigate('settings')}
          style={{ background: 'none', border: 'none', color: '#777', fontSize: 11, padding: '3px 8px', borderRadius: 4, cursor: 'pointer' }}
        >설정</button>
        <span style={{ marginLeft: 'auto', background: '#c62828', color: '#fff', fontSize: 10, fontWeight: 'bold', padding: '2px 7px', borderRadius: 10 }}>● LIVE</span>
        <button
          onClick={toggleFullscreen}
          title={isFullscreen ? '전체화면 종료' : '전체화면'}
          style={{ background: 'none', border: '1px solid #444', color: '#aaa', fontSize: 11, padding: '2px 7px', borderRadius: 4, cursor: 'pointer', lineHeight: 1 }}
>{isFullscreen ? '⊠ 종료' : '⛶ 전체화면'}</button>
      </div>
      <main style={{ position: 'relative', overflow: 'hidden', borderBottom: '2px solid #444' }}>
        {gridLayout.length > 0 && (
          <CameraGrid
            cameras={cameras}
            motionCams={motionCams}
            layout={gridLayout}
            onLayoutChange={setGridLayout}
          />
        )}
      </main>
      <Timeline
        cameras={cameras}
        timelineData={timelineData}
        date={today}
        isLive={true}
        onSeek={handleSeek}
        collapsed={timelineCollapsed}
        onToggleCollapsed={toggleTimelineCollapsed}
      />
    </div>
  );
}
