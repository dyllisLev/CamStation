import { describe, expect, it } from 'vitest'
import type { MotionEvent, RecordingSegment } from '../types'
import {
  NEW_LIVE_TIMELINE_COLLAPSED_KEY,
  calculateGridRowsPixelHeight,
  calculateLiveGridRowHeight,
  clampLayoutToGridBounds,
  filterRecordingSegments,
  formatDuration,
  formatStorageSize,
  getBoundedLayoutOrFallback,
  getTimelineToggleLabel,
  isNewViewerMode,
  layoutFitsWithinGridRows,
  mergeTimelineRanges,
  readNewLiveTimelineCollapsedPreference,
  shouldRestrictNewViewerPage,
  segmentOverlapsMotion,
} from '../pages/new-ui/newUiUtils'

const seg = (name: string, start: number, end: number | null = start + 600, size = 1024 * 1024): RecordingSegment => ({
  camera_id: 'cam1',
  filename: name,
  ts_start: start,
  ts_end: end,
  file_size: size,
})

const motion = (start: number, end: number | null = start + 10): MotionEvent => ({
  camera_id: 'cam1',
  ts_start: start,
  ts_end: end,
})

describe('mergeTimelineRanges', () => {
  it('카메라별 녹화 구간을 전체 카메라 집계용 비중복 구간으로 병합한다', () => {
    const ranges = mergeTimelineRanges([
      seg('a.mp4', 0, 100),
      seg('b.mp4', 90, 180),
      seg('c.mp4', 250, 300),
      seg('d.mp4', 180, null),
    ], 240)

    expect(ranges).toEqual([
      { ts_start: 0, ts_end: 240 },
      { ts_start: 250, ts_end: 300 },
    ])
  })
})

describe('segmentOverlapsMotion', () => {
  it('모션 이벤트가 세그먼트 시간 범위와 겹치면 true를 반환한다', () => {
    expect(segmentOverlapsMotion(seg('a.mp4', 100, 200), [motion(190, 210)])).toBe(true)
    expect(segmentOverlapsMotion(seg('b.mp4', 100, 200), [motion(201, 230)])).toBe(false)
  })
})

describe('filterRecordingSegments', () => {
  it('motion 필터는 모션과 겹치는 세그먼트만 남긴다', () => {
    const segments = [seg('motion.mp4', 100, 200), seg('plain.mp4', 300, 400)]

    expect(filterRecordingSegments(segments, [motion(150, 160)], 'motion').map(item => item.filename))
      .toEqual(['motion.mp4'])
  })

  it('offline 필터는 긴 공백 바로 전후 세그먼트를 남긴다', () => {
    const segments = [seg('before-gap.mp4', 0, 600), seg('after-gap.mp4', 7200, 7800), seg('normal.mp4', 8400, 9000)]

    expect(filterRecordingSegments(segments, [], 'offline').map(item => item.filename))
      .toEqual(['before-gap.mp4', 'after-gap.mp4'])
  })
})

describe('표시 포맷', () => {
  it('관제 UI에서 쓰는 시간과 용량을 짧은 한국어 표기로 만든다', () => {
    expect(formatDuration(610)).toBe('10분 10초')
    expect(formatStorageSize(1024 * 1024 * 1536)).toBe('1.5 GB')
  })
})

describe('신규 라이브 타임라인 표시 상태', () => {
  it('저장된 신규 UI 설정이 없으면 기존 배치의 접힘 상태와 무관하게 기본으로 타임라인을 보여준다', () => {
    const storage = new Map<string, string>()

    expect(readNewLiveTimelineCollapsedPreference((key) => storage.get(key) ?? null)).toBe(false)
  })

  it('신규 UI 전용 설정이 접힘이면 보기 버튼 문구를 제공한다', () => {
    const storage = new Map([[NEW_LIVE_TIMELINE_COLLAPSED_KEY, 'true']])

    expect(readNewLiveTimelineCollapsedPreference((key) => storage.get(key) ?? null)).toBe(true)
    expect(getTimelineToggleLabel(true)).toBe('타임라인 보기')
    expect(getTimelineToggleLabel(false)).toBe('타임라인 숨기기')
  })
})

describe('신규 UI 뷰어 모드 제한', () => {
  it('viewer=1 검색 파라미터를 EXE 전용 신규 UI 모드로 판정한다', () => {
    expect(isNewViewerMode('?viewer=1')).toBe(true)
    expect(isNewViewerMode('?viewer=0')).toBe(false)
    expect(isNewViewerMode('')).toBe(false)
  })

  it('EXE 전용 신규 UI 모드에서는 녹화와 설정 화면 접근을 제한한다', () => {
    expect(shouldRestrictNewViewerPage('live', true)).toBe(false)
    expect(shouldRestrictNewViewerPage('recordings', true)).toBe(true)
    expect(shouldRestrictNewViewerPage('settings', true)).toBe(true)
    expect(shouldRestrictNewViewerPage('settings', false)).toBe(false)
  })
})

describe('신규 라이브 그리드 영역 제한', () => {
  it('고정 행 수가 라이브뷰 실제 세로 픽셀을 넘지 않도록 행 높이를 계산한다', () => {
    const rowHeight = calculateLiveGridRowHeight(1000, 12, 6)

    expect(calculateGridRowsPixelHeight(12, rowHeight, 6)).toBeLessThanOrEqual(1000)
    expect(calculateGridRowsPixelHeight(12, rowHeight + 1, 6)).toBeGreaterThan(1000)
  })

  it('카메라 타일들의 y+h 합산 바닥이 라이브뷰 최대 행을 넘으면 영역 밖 배치로 판정한다', () => {
    const within = [
      { i: 'yard', x: 0, y: 0, w: 8, h: 7 },
      { i: 'fire', x: 0, y: 7, w: 8, h: 5 },
    ]
    const overflow = [
      { i: 'yard', x: 0, y: 0, w: 8, h: 8 },
      { i: 'fire', x: 0, y: 8, w: 8, h: 5 },
    ]

    expect(layoutFitsWithinGridRows(within, 12, 12)).toBe(true)
    expect(layoutFitsWithinGridRows(overflow, 12, 12)).toBe(false)
  })

  it('리사이즈가 다른 카메라를 라이브뷰 하단 밖으로 밀면 마지막 정상 배치로 되돌린다', () => {
    const lastValid = [
      { i: 'yard', x: 0, y: 0, w: 8, h: 7, minH: 2 },
      { i: 'fire', x: 0, y: 7, w: 8, h: 5, minH: 2 },
    ]
    const pushedOutside = [
      { i: 'yard', x: 0, y: 0, w: 8, h: 8, minH: 2 },
      { i: 'fire', x: 0, y: 8, w: 8, h: 5, minH: 2 },
    ]

    expect(getBoundedLayoutOrFallback(pushedOutside, lastValid, 12, 12)).toEqual(lastValid)
  })

  it('그리드가 일시적으로 빈 배치를 보고해도 기존 카메라 배치를 지우지 않는다', () => {
    const lastValid = [
      { i: 'yard', x: 0, y: 0, w: 8, h: 7, minH: 2 },
      { i: 'fire', x: 0, y: 7, w: 8, h: 5, minH: 2 },
    ]

    expect(getBoundedLayoutOrFallback([], lastValid, 12, 12)).toEqual(lastValid)
  })

  it('기존 동적 행 높이로 저장된 큰 배치는 비율을 유지한 채 라이브뷰 12행 안으로 축소한다', () => {
    const legacyLayout = [
      { i: 'yard', x: 0, y: 0, w: 6, h: 28, minH: 2 },
      { i: 'fire', x: 0, y: 28, w: 6, h: 28, minH: 2 },
      { i: 'side-top', x: 6, y: 0, w: 3, h: 17, minH: 2 },
      { i: 'side-middle', x: 6, y: 17, w: 3, h: 17, minH: 2 },
      { i: 'side-bottom', x: 6, y: 34, w: 3, h: 17, minH: 2 },
    ]

    const bounded = clampLayoutToGridBounds(legacyLayout, 12, 12)

    expect(bounded.find(item => item.i === 'yard')).toMatchObject({ y: 0, h: 6 })
    expect(bounded.find(item => item.i === 'fire')).toMatchObject({ y: 6, h: 6 })
    expect(Math.max(...bounded.map(item => item.y + item.h))).toBeLessThanOrEqual(12)
  })
})
