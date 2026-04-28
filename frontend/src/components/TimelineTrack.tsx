import type { RecordingSegment, MotionEvent } from '../types';

interface Props {
  camId: string;
  segments: RecordingSegment[];
  motionEvents: MotionEvent[];
  dayStart: number;
  dayEnd: number;
  cursorTs: number;
  onSeek: (ts: number) => void;
}

export function TimelineTrack({ camId, segments, motionEvents, dayStart, dayEnd, cursorTs, onSeek }: Props) {
  const range = dayEnd - dayStart;
  const pct = (ts: number) => ((ts - dayStart) / range) * 100;

  const handleClick = (e: React.MouseEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const ratio = (e.clientX - rect.left) / rect.width;
    onSeek(dayStart + ratio * range);
  };

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6, height: 18, marginBottom: 2 }}>
      <div style={{ fontSize: 9, color: '#555', width: 52, textAlign: 'right', flexShrink: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>
        {camId}
      </div>
      <div
        onClick={handleClick}
        style={{ flex: 1, height: 12, background: '#111', borderRadius: 2, position: 'relative', cursor: 'pointer', overflow: 'hidden' }}
      >
        {segments.map(s => {
          const end = s.ts_end ?? cursorTs;
          return (
            <div key={s.filename} style={{
              position: 'absolute', top: 0, bottom: 0,
              left: `${pct(s.ts_start)}%`,
              width: `${Math.max(pct(end) - pct(s.ts_start), 0.1)}%`,
              background: '#1565c0', opacity: 0.7, borderRadius: 2,
            }} />
          );
        })}
        {motionEvents.map((m) => (
          <div key={`${m.ts_start}`} style={{
            position: 'absolute', top: 0, bottom: 0,
            left: `${pct(m.ts_start)}%`,
            width: `${Math.max(pct((m.ts_end ?? m.ts_start + 5)) - pct(m.ts_start), 0.3)}%`,
            background: '#e65100', opacity: 0.9, borderRadius: 1,
          }} />
        ))}
        <div style={{
          position: 'absolute', top: 0, bottom: 0, width: 1,
          left: `${Math.min(Math.max(pct(cursorTs), 0), 100)}%`,
          background: '#3dd2f0', zIndex: 5,
        }} />
      </div>
    </div>
  );
}
