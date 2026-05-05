import { useEffect, useState } from 'react'
import type { Camera } from '../../types'
import { useMSE } from '../../hooks/useMSE'

interface Props {
  camera: Camera
  cameras: Camera[]
  cameraIndex: number
  onClose: () => void
  onNavigate: (newIndex: number) => void
  onFullscreen: () => void
}

function isIOS(): boolean {
  return /iPhone|iPad|iPod/.test(navigator.userAgent) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

function formatTime(d: Date): string {
  return d.toTimeString().slice(0, 8)
}

export function MobileCamDetail({ camera, cameras, cameraIndex, onClose, onNavigate, onFullscreen }: Props) {
  const camId = camera.has_sub ? `${camera.id}_sub` : camera.id
  const { videoRef, connected } = useMSE(camId)
  const [time, setTime] = useState(() => new Date())

  useEffect(() => {
    const id = setInterval(() => setTime(new Date()), 1000)
    return () => clearInterval(id)
  }, [])

  const handleFullscreen = () => {
    if (isIOS()) {
      const v = videoRef.current as HTMLVideoElement & { webkitEnterFullscreen?: () => void }
      v?.webkitEnterFullscreen?.()
    } else {
      onFullscreen()
    }
  }

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
      <div style={{ flex: 1, position: 'relative', background: 'linear-gradient(160deg, var(--mob-bg-card), #080818)' }}>
        <video
          ref={videoRef}
          autoPlay
          muted
          playsInline
          style={{ width: '100%', height: '100%', objectFit: 'contain', display: 'block' }}
        />
        {!connected && (
          <div style={{
            position: 'absolute', inset: 0,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            color: '#444', fontSize: 13,
          }}>
            연결 중...
          </div>
        )}
        <div style={{ position: 'absolute', top: 10, left: 12, fontSize: 12, color: 'rgba(255,255,255,0.6)' }}>
          📷 {camera.name}
        </div>
        <div style={{
          position: 'absolute', top: 10, right: 12,
          fontSize: 11, color: 'var(--mob-red)',
          display: 'flex', alignItems: 'center', gap: 3,
        }}>
          <span style={{ width: 6, height: 6, borderRadius: '50%', background: 'var(--mob-red)', display: 'inline-block' }} />
          REC
        </div>
        <div style={{ position: 'absolute', bottom: 10, right: 12, fontSize: 11, color: 'rgba(255,255,255,0.4)' }}>
          {formatTime(time)}
        </div>
      </div>

      <div className="mob-ctrl-bar">
        <button className="mob-ctrl-btn" onClick={onClose} title="닫기">✕</button>
        <button
          className="mob-ctrl-btn"
          onClick={() => onNavigate(cameraIndex - 1)}
          disabled={cameraIndex === 0}
          title="이전"
        >
          ‹
        </button>
        <span className="mob-ctrl-name">{camera.name}</span>
        <button
          className="mob-ctrl-btn"
          onClick={() => onNavigate(cameraIndex + 1)}
          disabled={cameraIndex === cameras.length - 1}
          title="다음"
        >
          ›
        </button>
        <button className="mob-ctrl-btn mob-ctrl-btn-accent" onClick={handleFullscreen} title="전체화면">
          ⛶
        </button>
      </div>
    </div>
  )
}
