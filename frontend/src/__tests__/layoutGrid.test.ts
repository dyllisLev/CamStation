import { describe, expect, it } from 'vitest'
import {
  inferGridRows,
  scaleLayoutGridResolution,
} from '../layoutGrid'

describe('layout grid resolution', () => {
  it('기존 12칸 배치를 48칸 신규 UI 해상도로 비율 유지하며 확장한다', () => {
    const layout = [
      { i: 'yard', x: 0, y: 0, w: 6, h: 4, minW: 2, minH: 2 },
      { i: 'side', x: 6, y: 4, w: 3, h: 2, minW: 2, minH: 2 },
    ]

    expect(scaleLayoutGridResolution(layout, {
      fromCols: 12,
      toCols: 48,
      fromRows: 12,
      toRows: 48,
    })).toEqual([
      { i: 'yard', x: 0, y: 0, w: 24, h: 16, minW: 8, minH: 8 },
      { i: 'side', x: 24, y: 16, w: 12, h: 8, minW: 8, minH: 8 },
    ])
  })

  it('행 해상도 메타데이터가 없는 큰 레거시 배치는 실제 바닥 행을 기준으로 해석한다', () => {
    const legacyLayout = [
      { i: 'yard', x: 0, y: 0, w: 6, h: 28 },
      { i: 'fire', x: 0, y: 28, w: 6, h: 28 },
    ]

    expect(inferGridRows(legacyLayout, 12)).toBe(56)
  })

  it('48칸 신규 UI 배치를 기존 12칸 UI가 읽을 때 가로만 12칸 기준으로 축소할 수 있다', () => {
    const fineLayout = [
      { i: 'yard', x: 0, y: 0, w: 24, h: 16, minW: 8, minH: 8 },
      { i: 'side', x: 24, y: 16, w: 12, h: 8, minW: 8, minH: 8 },
    ]

    expect(scaleLayoutGridResolution(fineLayout, {
      fromCols: 48,
      toCols: 12,
      fromRows: 48,
      toRows: 48,
    })).toEqual([
      { i: 'yard', x: 0, y: 0, w: 6, h: 16, minW: 2, minH: 8 },
      { i: 'side', x: 6, y: 16, w: 3, h: 8, minW: 2, minH: 8 },
    ])
  })
})
