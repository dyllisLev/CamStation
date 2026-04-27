import { WebRTCPlayer } from './WebRTCPlayer';
import type { Camera } from '../types';

interface Props {
  camera: Camera;
  hasMotion: boolean;
}

export function CameraTile({ camera, hasMotion }: Props) {
  return (
    <div style={{ width: '100%', height: '100%', position: 'relative' }}>
      <div style={{
        background: 'rgba(0,0,0,0.7)', padding: '2px 8px',
        fontSize: 10, color: '#ddd', display: 'flex', alignItems: 'center', gap: 6,
        position: 'absolute', top: 0, left: 0, right: 0, zIndex: 2,
      }}>
        <span style={{
          width: 6, height: 6, borderRadius: '50%',
          background: camera.online ? '#4caf50' : '#f44336', flexShrink: 0,
        }} />
        {camera.name}
        {hasMotion && (
          <span style={{
            marginLeft: 'auto', background: '#e65100', color: '#fff',
            fontSize: 8, padding: '1px 4px', borderRadius: 2,
          }}>
            모션
          </span>
        )}
      </div>
      <WebRTCPlayer camId={camera.id} />
    </div>
  );
}
