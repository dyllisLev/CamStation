import type { MotionEvent, RecordingSegment } from '../../types'

export const NEW_LIVE_TIMELINE_COLLAPSED_KEY = 'camstation-new-live-timeline-collapsed'

type StorageReader = (key: string) => string | null

type GridBoundsItem = {
  i: string
  x: number
  y: number
  w: number
  h: number
  minW?: number | null
  minH?: number | null
}

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max)
}

function layoutBottom(layout: GridBoundsItem[]): number {
  return Math.max(0, ...layout.map(item => item.y + item.h))
}

export function calculateGridRowsPixelHeight(rows: number, rowHeight: number, marginY: number): number {
  if (rows <= 0) return 0
  return rows * rowHeight + (rows - 1) * marginY
}

export function calculateLiveGridRowHeight(containerHeight: number, maxRows: number, marginY: number): number {
  const availableHeight = containerHeight - Math.max(0, maxRows - 1) * marginY
  return Math.max(1, Math.floor(availableHeight / Math.max(1, maxRows)))
}

export function layoutFitsWithinGridRows(layout: GridBoundsItem[], maxRows: number, cols: number): boolean {
  return layout.every(item => (
    item.x >= 0
    && item.y >= 0
    && item.w > 0
    && item.h > 0
    && item.x + item.w <= cols
    && item.y + item.h <= maxRows
  ))
}

export function clampLayoutToGridBounds<T extends GridBoundsItem>(layout: T[], maxRows: number, cols: number): T[] {
  const bottom = layoutBottom(layout)
  const scaleY = bottom > maxRows ? maxRows / bottom : 1

  return layout.map(item => {
    const minW = clampNumber(item.minW ?? 1, 1, cols)
    const minH = clampNumber(item.minH ?? 1, 1, maxRows)
    const scaledY = scaleY < 1 ? Math.round(item.y * scaleY) : item.y
    const scaledH = scaleY < 1 ? Math.round(item.h * scaleY) : item.h
    const w = clampNumber(item.w, minW, cols)
    const h = clampNumber(scaledH, minH, maxRows)
    const x = clampNumber(item.x, 0, cols - w)
    const y = clampNumber(scaledY, 0, maxRows - h)
    return { ...item, x, y, w, h }
  })
}

export function getBoundedLayoutOrFallback<T extends GridBoundsItem>(
  nextLayout: T[],
  fallbackLayout: T[],
  maxRows: number,
  cols: number,
): T[] {
  if (layoutFitsWithinGridRows(nextLayout, maxRows, cols)) {
    return clampLayoutToGridBounds(nextLayout, maxRows, cols)
  }
  if (fallbackLayout.length > 0) {
    return clampLayoutToGridBounds(fallbackLayout, maxRows, cols)
  }
  return clampLayoutToGridBounds(nextLayout, maxRows, cols)
}

export function readNewLiveTimelineCollapsedPreference(readStorage: StorageReader): boolean {
  return readStorage(NEW_LIVE_TIMELINE_COLLAPSED_KEY) === 'true'
}

export function getTimelineToggleLabel(collapsed: boolean): string {
  return collapsed ? '타임라인 보기' : '타임라인 숨기기'
}

export type RecordingFilter = 'all' | 'motion' | 'offline'

export interface TimelineRange {
  ts_start: number
  ts_end: number
}

export function mergeTimelineRanges(segments: RecordingSegment[], fallbackEndTs: number): TimelineRange[] {
  const ranges = segments
    .map(segment => ({
      ts_start: segment.ts_start,
      ts_end: segment.ts_end ?? fallbackEndTs,
    }))
    .filter(range => range.ts_end > range.ts_start)
    .sort((a, b) => a.ts_start - b.ts_start)

  const merged: TimelineRange[] = []
  for (const range of ranges) {
    const last = merged[merged.length - 1]
    if (!last || range.ts_start > last.ts_end) {
      merged.push({ ...range })
      continue
    }
    last.ts_end = Math.max(last.ts_end, range.ts_end)
  }
  return merged
}

export function segmentOverlapsMotion(segment: RecordingSegment, motionEvents: MotionEvent[]): boolean {
  const segmentEnd = segment.ts_end ?? segment.ts_start
  return motionEvents.some(event => {
    const eventEnd = event.ts_end ?? event.ts_start + 5
    return event.ts_start < segmentEnd && eventEnd > segment.ts_start
  })
}

export function filterRecordingSegments(
  segments: RecordingSegment[],
  motionEvents: MotionEvent[],
  filter: RecordingFilter,
  offlineGapSeconds = 30 * 60,
): RecordingSegment[] {
  if (filter === 'all') return segments
  if (filter === 'motion') return segments.filter(segment => segmentOverlapsMotion(segment, motionEvents))

  const sorted = [...segments].sort((a, b) => a.ts_start - b.ts_start)
  const adjacent = new Set<string>()
  for (let i = 0; i < sorted.length - 1; i += 1) {
    const current = sorted[i]
    const next = sorted[i + 1]
    const currentEnd = current.ts_end ?? current.ts_start
    if (next.ts_start - currentEnd >= offlineGapSeconds) {
      adjacent.add(current.filename)
      adjacent.add(next.filename)
    }
  }
  return segments.filter(segment => adjacent.has(segment.filename))
}

export function formatDuration(seconds: number): string {
  const minutes = Math.floor(seconds / 60)
  const rest = Math.max(0, Math.floor(seconds % 60))
  return `${minutes}분 ${rest.toString().padStart(2, '0')}초`
}

export function formatStorageSize(bytes: number | null | undefined): string {
  if (!bytes) return '-'
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  if (bytes >= 1024 ** 2) return `${Math.round(bytes / 1024 ** 2)} MB`
  return `${Math.round(bytes / 1024)} KB`
}

export function formatClock(ts: number): string {
  const date = new Date(ts * 1000)
  return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`
}

export function recordingUrl(cameraId: string, date: string, filename: string): string {
  return `/api/recordings/${encodeURIComponent(cameraId)}/${date}/${filename}`
}
