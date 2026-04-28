import { useEffect, useState } from 'react';
import { format } from 'date-fns';
import { TimelineTrack } from './TimelineTrack';
import type { Camera, TimelineData } from '../types';

interface Props {
  cameras: Camera[];
  timelineData: Record<string, TimelineData>;
  date: string;
  isLive: boolean;
  onSeek: (camId: string, ts: number) => void;
  collapsed: boolean;
  onToggleCollapsed: () => void;
}

export function Timeline({ cameras, timelineData, date, isLive, onSeek, collapsed, onToggleCollapsed }: Props) {
  const [currentTime, setCurrentTime] = useState(() => new Date());

  useEffect(() => {
    if (!isLive) return;
    const id = setInterval(() => setCurrentTime(new Date()), 1000);
    return () => clearInterval(id);
  }, [isLive]);

  const dayStart = new Date(date + 'T00:00:00').getTime() / 1000;
  const dayEnd = dayStart + 86400;
  const cursorTs = isLive ? Date.now() / 1000 : currentTime.getTime() / 1000;

  return (
    <div style={{ borderTop: '2px solid #3a3a3a', background: '#181818', flexShrink: 0 }}>
      <button
        onClick={onToggleCollapsed}
        style={{
          width: '100%', background: '#242424', border: 'none', borderBottom: '1px solid #333',
          color: '#888', fontSize: 10, padding: '4px 0', cursor: 'pointer', display: 'flex',
          alignItems: 'center', justifyContent: 'center', gap: 4,
        }}
      >
        {collapsed ? '▲ 타임라인 펼치기' : '▼ 타임라인 접기'}
      </button>

      {!collapsed && (
        <>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 10px', borderBottom: '1px solid #222', height: 28 }}>
            <span style={{ fontSize: 16, fontWeight: 'bold', color: '#3dd2f0', fontFamily: 'monospace' }}>
              {format(currentTime, 'HH:mm:ss')}
            </span>
            <span style={{ fontSize: 10, color: '#555' }}>{format(currentTime, 'yyyy-MM-dd EEE')}</span>
            <div style={{ marginLeft: 'auto' }}>
              {isLive && (
                <span style={{ background: '#c62828', color: '#fff', fontSize: 10, fontWeight: 'bold', padding: '2px 7px', borderRadius: 10 }}>
                  ● LIVE
                </span>
              )}
            </div>
          </div>
          <div style={{ padding: '3px 10px 4px' }}>
            {cameras.map(cam => (
              <TimelineTrack
                key={cam.id}
                camId={cam.id}
                segments={timelineData[cam.id]?.segments ?? []}
                motionEvents={timelineData[cam.id]?.motion_events ?? []}
                dayStart={dayStart}
                dayEnd={dayEnd}
                cursorTs={cursorTs}
                onSeek={(ts) => onSeek(cam.id, ts)}
              />
            ))}
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', padding: '1px 10px 2px calc(10px + 52px)', fontSize: 9, color: '#3a3a3a' }}>
            {['00:00','04:00','08:00','12:00','16:00','20:00','24:00'].map(t => <span key={t}>{t}</span>)}
          </div>
        </>
      )}
    </div>
  );
}
