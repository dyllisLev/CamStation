import { describe, it, expect } from 'vitest'
import { getPageCameras, getTotalPages, getSwipeDirection } from '../components/mobile/mobileUtils'
import type { Camera } from '../types'

const makeCam = (id: string): Camera => ({ id, name: id, online: true, has_sub: false })
const cams = Array.from({ length: 7 }, (_, i) => makeCam(`cam${i}`))

describe('getTotalPages', () => {
  it('returns 1 for 4 or fewer cameras', () => {
    expect(getTotalPages([])).toBe(1)
    expect(getTotalPages(cams.slice(0, 4))).toBe(1)
  })
  it('returns 2 for 5–8 cameras', () => {
    expect(getTotalPages(cams.slice(0, 5))).toBe(2)
    expect(getTotalPages(cams)).toBe(2)
  })
})

describe('getPageCameras', () => {
  it('returns first 4 cameras for page 0', () => {
    expect(getPageCameras(cams, 0).map(c => c.id)).toEqual(['cam0','cam1','cam2','cam3'])
  })
  it('returns remaining cameras for page 1', () => {
    expect(getPageCameras(cams, 1).map(c => c.id)).toEqual(['cam4','cam5','cam6'])
  })
  it('returns empty array for out-of-range page', () => {
    expect(getPageCameras(cams, 5)).toEqual([])
  })
})

describe('getSwipeDirection', () => {
  it('returns "left" when dx < -50', () => {
    expect(getSwipeDirection(-51)).toBe('left')
    expect(getSwipeDirection(-100)).toBe('left')
  })
  it('returns "right" when dx > 50', () => {
    expect(getSwipeDirection(51)).toBe('right')
  })
  it('returns null for small movements', () => {
    expect(getSwipeDirection(0)).toBeNull()
    expect(getSwipeDirection(49)).toBeNull()
    expect(getSwipeDirection(-50)).toBeNull()
  })
})
