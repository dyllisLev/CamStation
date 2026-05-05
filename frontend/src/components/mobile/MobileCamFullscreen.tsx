import { useEffect, useRef, useState } from 'react'
import type { Camera } from '../../types'
import { WebRTCPlayer } from '../WebRTCPlayer'

interface Props {
  camera: Camera
  cameras: Camera[]
  cameraIndex: number
  onClose: () => void
  onNavigate: (newIndex: number) => void
}

export function MobileCamFullscreen({ camera, cameras, cameraIndex, onClose, onNavigate }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [overlayVisible, setOverlayVisible] = useState(false)
  const overlayTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const camId = camera.has_sub ? `${camera.id}_sub` : camera.id

  useEffect(() => {
    const enter = async () => {
      try { await containerRef.current?.requestFullscreen() } catch {}
      try { await screen.orientation.lock('landscape') } catch {}
    }
    enter()

    const handleFullscreenChange = () => {
      if (!document.fullscreenElement) {
        try { screen.orientation.unlock() } catch {}
        onClose()
      }
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange)

    return () => {
      document.removeEventListener('fullscreenchange', handleFullscreenChange)
      if (document.fullscreenElement) document.exitFullscreen().catch(() => {})
      try { screen.orientation.unlock() } catch {}
      clearTimeout(overlayTimer.current)
    }
  }, [onClose])

  const handleTap = () => {
    clearTimeout(overlayTimer.current)
    setOverlayVisible(true)
    overlayTimer.current = setTimeout(() => setOverlayVisible(false), 2000)
  }

  const handleClose = (e: React.MouseEvent) => {
    e.stopPropagation()
    document.exitFullscreen().catch(() => {})
  }

  return (
    <div
      ref={containerRef}
      onClick={handleTap}
      style={{
        position: 'fixed',
        inset: 0,
        background: '#000',
        zIndex: 9999,
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div style={{ flex: 1, position: 'relative', overflow: 'hidden' }}>
        <WebRTCPlayer camId={camId} />

        {overlayVisible && (
          <div style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.45)', zIndex: 10 }}>
            <button
              onClick={handleClose}
              style={{
                position: 'absolute',
                top: 10, right: 14,
                width: 32, height: 32,
                borderRadius: '50%',
                border: '1.5px solid rgba(255,255,255,0.6)',
                background: 'transparent',
                color: 'rgba(255,255,255,0.85)',
                fontSize: 16,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                cursor: 'pointer',
                zIndex: 11,
              }}
            >
              ✕
            </button>
          </div>
        )}
      </div>

      <div
        style={{
          height: 36,
          background: 'rgba(15,15,26,0.9)',
          borderTop: '1px solid #1e1e33',
          display: 'flex',
          alignItems: 'center',
          padding: '0 16px',
          flexShrink: 0,
          zIndex: 10,
        }}
        onClick={e => e.stopPropagation()}
      >
        <button
          onClick={() => onNavigate(cameraIndex - 1)}
          disabled={cameraIndex === 0}
          style={{
            background: 'none', border: 'none',
            color: cameraIndex === 0 ? '#444' : '#aaa',
            fontSize: 18, cursor: 'pointer', padding: '0 8px',
          }}
        >
          ‹
        </button>
        <span style={{ flex: 1, textAlign: 'center', fontSize: 13, color: '#ccc' }}>
          {camera.name}
        </span>
        <button
          onClick={() => onNavigate(cameraIndex + 1)}
          disabled={cameraIndex === cameras.length - 1}
          style={{
            background: 'none', border: 'none',
            color: cameraIndex === cameras.length - 1 ? '#444' : '#aaa',
            fontSize: 18, cursor: 'pointer', padding: '0 8px',
          }}
        >
          ›
        </button>
      </div>
    </div>
  )
}
