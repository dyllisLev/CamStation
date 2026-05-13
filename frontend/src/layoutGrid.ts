export interface GridResolution {
  fromCols: number
  toCols: number
  fromRows: number
  toRows: number
}

export interface GridLayoutItem {
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

export function layoutBottom(layout: GridLayoutItem[]): number {
  return Math.max(0, ...layout.map(item => item.y + item.h))
}

export function inferGridRows(layout: GridLayoutItem[], fallbackRows: number): number {
  return Math.max(fallbackRows, layoutBottom(layout))
}

function scaleGridUnit(value: number, fromSize: number, toSize: number): number {
  if (fromSize <= 0 || toSize <= 0) return value
  return Math.round(value * (toSize / fromSize))
}

export function scaleLayoutGridResolution<T extends GridLayoutItem>(layout: T[], resolution: GridResolution): T[] {
  const { fromCols, toCols, fromRows, toRows } = resolution
  if (fromCols === toCols && fromRows === toRows) {
    return layout.map(item => ({ ...item }))
  }

  return layout.map(item => {
    const minW = item.minW == null
      ? item.minW
      : clampNumber(scaleGridUnit(item.minW, fromCols, toCols), 1, toCols)
    const minH = item.minH == null
      ? item.minH
      : clampNumber(scaleGridUnit(item.minH, fromRows, toRows), 1, toRows)
    const w = clampNumber(scaleGridUnit(item.w, fromCols, toCols), minW ?? 1, toCols)
    const h = clampNumber(scaleGridUnit(item.h, fromRows, toRows), minH ?? 1, toRows)
    const x = clampNumber(scaleGridUnit(item.x, fromCols, toCols), 0, toCols - w)
    const y = clampNumber(scaleGridUnit(item.y, fromRows, toRows), 0, toRows - h)

    return { ...item, x, y, w, h, minW, minH } as T
  })
}
