import { useEffect, useRef, useState } from 'react';
import { useMSE } from '../hooks/useMSE';

interface Props {
  camId: string;
  style?: React.CSSProperties;
}

const debugMode = new URLSearchParams(location.search).has('debug');

interface DebugInfo {
  readyState: number;
  latency: string;
  buffered: string;
}

export function WebRTCPlayer({ camId, style }: Props) {
  const { videoRef, connected } = useMSE(camId);
  const [debug, setDebug] = useState<DebugInfo | null>(null);
  const rafRef = useRef<number>(0);

  useEffect(() => {
    if (!debugMode) return;
    const update = () => {
      const v = videoRef.current;
      if (v) {
        const bufferedEnd = v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0;
        const latency = (bufferedEnd - v.currentTime).toFixed(2);
        const ranges = Array.from({ length: v.buffered.length }, (_, i) =>
          `${v.buffered.start(i).toFixed(1)}-${v.buffered.end(i).toFixed(1)}`
        ).join(' ');
        setDebug({ readyState: v.readyState, latency, buffered: ranges || '—' });
      }
      rafRef.current = requestAnimationFrame(update);
    };
    rafRef.current = requestAnimationFrame(update);
    return () => cancelAnimationFrame(rafRef.current);
  }, [videoRef]);

  return (
    <div style={{ position: 'relative', width: '100%', height: '100%', background: '#0d0d0d', ...style }}>
      <video
        ref={videoRef}
        autoPlay
        muted
        playsInline
        style={{ width: '100%', height: '100%', objectFit: 'contain' }}
      />
      {!connected && (
        <div style={{
          position: 'absolute', inset: 0,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: '#444', fontSize: 12,
        }}>
          연결 중...
        </div>
      )}
      {debugMode && debug && (
        <div style={{
          position: 'absolute', bottom: 4, left: 4,
          background: 'rgba(0,0,0,0.7)', color: '#0f0', fontSize: 10,
          fontFamily: 'monospace', padding: '2px 5px', borderRadius: 3,
          pointerEvents: 'none', lineHeight: 1.5,
        }}>
          <div>rs:{debug.readyState} lat:{debug.latency}s</div>
          <div>buf:{debug.buffered}</div>
        </div>
      )}
    </div>
  );
}
