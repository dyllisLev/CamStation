import { useState, useRef } from 'react'
import type { Camera } from '../../types'
import { MobileCamTile } from './MobileCamTile'
import { getTotalPages, getPageCameras, getSwipeDirection } from './mobileUtils'

interface Props {
  cameras: Camera[]
  onCameraSelect: (globalIndex: number) => void
}

export function MobileGrid({ cameras, onCameraSelect }: Props) {
  const [currentPage, setCurrentPage] = useState(0)
  const touchStartX = useRef<number>(0)
  const totalPages = getTotalPages(cameras)

  const handleTouchStart = (e: React.TouchEvent) => {
    touchStartX.current = e.touches[0].clientX
  }

  const handleTouchEnd = (e: React.TouchEvent) => {
    const dx = e.changedTouches[0].clientX - touchStartX.current
    const direction = getSwipeDirection(dx)
    if (direction === 'left' && currentPage < totalPages - 1) {
      setCurrentPage(p => p + 1)
    } else if (direction === 'right' && currentPage > 0) {
      setCurrentPage(p => p - 1)
    }
  }

  return (
    <>
      <div
        style={{ flex: 1, overflow: 'hidden', position: 'relative' }}
        onTouchStart={handleTouchStart}
        onTouchEnd={handleTouchEnd}
      >
        <div style={{
          display: 'flex',
          width: `${totalPages * 100}%`,
          height: '100%',
          transform: `translateX(-${currentPage * (100 / totalPages)}%)`,
          transition: 'transform 0.28s ease',
          willChange: 'transform',
        }}>
          {Array.from({ length: totalPages }, (_, pageIdx) => {
            const pageCams = getPageCameras(cameras, pageIdx)
            return (
              <div
                key={pageIdx}
                style={{
                  width: `${100 / totalPages}%`,
                  flexShrink: 0,
                  display: 'grid',
                  gridTemplateColumns: '1fr 1fr',
                  gridTemplateRows: '1fr 1fr',
                  gap: 3,
                  padding: 3,
                  background: 'var(--mob-bg-base)',
                }}
              >
                {pageCams.map((camera, localIdx) => (
                  <MobileCamTile
                    key={camera.id}
                    camera={camera}
                    onClick={() => onCameraSelect(pageIdx * 4 + localIdx)}
                  />
                ))}
              </div>
            )
          })}
        </div>
      </div>

      {totalPages > 1 && (
        <div className="mob-page-indicator">
          {Array.from({ length: totalPages }, (_, i) => (
            <div
              key={i}
              className={`mob-page-dot${i === currentPage ? ' mob-page-dot-active' : ''}`}
            />
          ))}
        </div>
      )}
    </>
  )
}
