import { useEffect, useState } from 'react'
import { useCameras } from '../hooks/useCameras'
import { MobileGrid } from '../components/mobile/MobileGrid'
import { MobileCamDetail } from '../components/mobile/MobileCamDetail'
import { MobileCamFullscreen } from '../components/mobile/MobileCamFullscreen'
import '../components/mobile/mobile.css'

type MobileView =
  | { screen: 'grid' }
  | { screen: 'detail'; cameraIndex: number }
  | { screen: 'fullscreen'; cameraIndex: number }

function registerServiceWorker() {
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.register('/sw.js').catch(console.error)
  }
}

export function MobilePage() {
  const cameras = useCameras()
  const [view, setView] = useState<MobileView>({ screen: 'grid' })

  useEffect(() => {
    const existing = document.querySelector('link[rel="manifest"]')
    if (!existing) {
      const link = document.createElement('link')
      link.rel = 'manifest'
      link.href = '/mobile.webmanifest'
      document.head.appendChild(link)
    }
    let meta = document.querySelector('meta[name="theme-color"]') as HTMLMetaElement | null
    if (!meta) {
      meta = document.createElement('meta') as HTMLMetaElement
      meta.name = 'theme-color'
      document.head.appendChild(meta)
    }
    meta.content = '#0f0f1a'
    registerServiceWorker()
  }, [])

  const onCameraSelect = (index: number) => setView({ screen: 'detail', cameraIndex: index })
  const onDetailClose = () => setView({ screen: 'grid' })
  const onDetailNavigate = (index: number) => setView({ screen: 'detail', cameraIndex: index })
  const onFullscreen = (index: number) => setView({ screen: 'fullscreen', cameraIndex: index })
  const onFullscreenClose = () => {
    if (view.screen === 'fullscreen') {
      setView({ screen: 'detail', cameraIndex: view.cameraIndex })
    }
  }
  const onFullscreenNavigate = (index: number) => setView({ screen: 'fullscreen', cameraIndex: index })

  const onlineCount = cameras.filter(c => c.online).length

  if (view.screen === 'fullscreen') {
    const camera = cameras[view.cameraIndex]
    if (!camera) return null
    return (
      <MobileCamFullscreen
        camera={camera}
        cameras={cameras}
        cameraIndex={view.cameraIndex}
        onClose={onFullscreenClose}
        onNavigate={onFullscreenNavigate}
      />
    )
  }

  return (
    <div className="mob-root">
      <div className="mob-header">
        <div className="mob-header-logo">◉</div>
        <span className="mob-header-title">CamStation</span>
        {cameras.length > 0 && (
          <span className="mob-pill mob-pill-green">● {onlineCount}/{cameras.length}</span>
        )}
        <span className="mob-pill mob-pill-red">REC</span>
      </div>

      {view.screen === 'grid' && (
        <MobileGrid cameras={cameras} onCameraSelect={onCameraSelect} />
      )}

      {view.screen === 'detail' && cameras[view.cameraIndex] && (
        <MobileCamDetail
          camera={cameras[view.cameraIndex]}
          cameras={cameras}
          cameraIndex={view.cameraIndex}
          onClose={onDetailClose}
          onNavigate={onDetailNavigate}
          onFullscreen={() => onFullscreen(view.cameraIndex)}
        />
      )}
    </div>
  )
}
