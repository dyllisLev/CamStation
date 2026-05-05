import type { Camera } from '../../types'
import { WebRTCPlayer } from '../WebRTCPlayer'

interface Props {
  camera: Camera
  onClick: () => void
}

export function MobileCamTile({ camera, onClick }: Props) {
  const camId = camera.has_sub ? `${camera.id}_sub` : camera.id

  return (
    <div
      onClick={onClick}
      style={{
        background: 'linear-gradient(160deg, var(--mob-bg-card), #0a0a18)',
        borderRadius: 8,
        border: '1px solid var(--mob-border)',
        position: 'relative',
        overflow: 'hidden',
        cursor: 'pointer',
      }}
    >
      <WebRTCPlayer
        camId={camId}
        style={{ position: 'absolute', inset: 0, width: '100%', height: '100%' }}
      />
      <div style={{
        position: 'absolute',
        bottom: 0, left: 0, right: 0,
        height: 20,
        background: 'rgba(0,0,0,0.7)',
        display: 'flex',
        alignItems: 'center',
        padding: '0 6px',
        gap: 4,
        zIndex: 1,
      }}>
        <span style={{
          width: 5, height: 5,
          borderRadius: '50%',
          background: camera.online ? 'var(--mob-green)' : 'var(--mob-red)',
          flexShrink: 0,
        }} />
        <span style={{
          fontSize: 10,
          color: 'var(--mob-text-primary)',
          flex: 1,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}>
          {camera.name}
        </span>
      </div>
    </div>
  )
}
