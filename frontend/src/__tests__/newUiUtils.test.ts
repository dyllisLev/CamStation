import { describe, expect, it } from 'vitest'
import type { MotionEvent, RecordingSegment } from '../types'
import {
  filterRecordingSegments,
  formatDuration,
  formatStorageSize,
  mergeTimelineRanges,
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
