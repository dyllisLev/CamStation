import { describe, expect, it } from 'vitest'
import { getAppEntryForPath } from '../routes'

describe('getAppEntryForPath', () => {
  it('/new와 하위 경로를 기존 UI와 분리된 신규 UI 엔트리로 보낸다', () => {
    expect(getAppEntryForPath('/new')).toBe('new')
    expect(getAppEntryForPath('/new/recordings')).toBe('new')
  })

  it('기존 공개 경로는 현재 엔트리를 유지한다', () => {
    expect(getAppEntryForPath('/')).toBe('classic')
    expect(getAppEntryForPath('/viewer')).toBe('viewer')
    expect(getAppEntryForPath('/mobile')).toBe('mobile')
  })
})
