import { describe, expect, it } from 'vitest'
import type { MotionEvent, RecordingSegment } from '../types'
import {
  NEW_LIVE_TIMELINE_COLLAPSED_KEY,
  filterRecordingSegments,
  formatDuration,
  formatStorageSize,
  getTimelineToggleLabel,
  mergeTimelineRanges,
  readNewLiveTimelineCollapsedPreference,
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
