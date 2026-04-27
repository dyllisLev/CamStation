import { useWebRTC } from '../hooks/useWebRTC';

interface Props {
  camId: string;
  style?: React.CSSProperties;
}

export function WebRTCPlayer({ camId, style }: Props) {
  const { videoRef, connected } = useWebRTC(camId);
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
    </div>
  );
}
