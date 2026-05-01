import type { Camera } from '../../types'

const CAMERAS_PER_PAGE = 4

export function getTotalPages(cameras: Camera[]): number {
  return Math.max(1, Math.ceil(cameras.length / CAMERAS_PER_PAGE))
}

export function getPageCameras(cameras: Camera[], page: number): Camera[] {
  return cameras.slice(page * CAMERAS_PER_PAGE, (page + 1) * CAMERAS_PER_PAGE)
}

export function getSwipeDirection(dx: number): 'left' | 'right' | null {
  if (dx < -50) return 'left'
  if (dx > 50) return 'right'
  return null
}
