import type { Camera } from '../../types'

const CAMERAS_PER_PAGE = 4

export function getTotalPages(cameras: Camera[]): number {
  return Math.max(1, Math.ceil(cameras.length / CAMERAS_PER_PAGE))
}

export function getPageCameras(cameras: Camera[], page: number): Camera[] {
  return cameras.slice(page * CAMERAS_PER_PAGE, (page + 1) * CAMERAS_PER_PAGE)
}

const SWIPE_THRESHOLD_PX = 50

export function getSwipeDirection(dx: number): 'left' | 'right' | null {
  if (dx < -SWIPE_THRESHOLD_PX) return 'left'
  if (dx > SWIPE_THRESHOLD_PX) return 'right'
  return null
}
