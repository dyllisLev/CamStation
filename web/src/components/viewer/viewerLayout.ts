import type { LayoutItem, LayoutProfile } from "../../app/api";

const GRID_COLS = 48;
const GRID_ROWS = 48;

type CameraKey = { readonly streamName: string };

export type ViewerLayout = {
  readonly cols: number;
  readonly rows: number;
  readonly items: readonly LayoutItem[];
};

export type ViewerRect = {
  readonly left: number;
  readonly top: number;
  readonly width: number;
  readonly height: number;
};

export function resolveViewerLayout(
  cameras: readonly CameraKey[],
  layouts: readonly LayoutProfile[],
): ViewerLayout {
  const saved = layouts[0];
  const defaults = defaultViewerLayout(cameras);
  const savedByStream = new Map(saved?.data.map((item) => [item.i, item]) ?? []);
  const items = defaults.map((item) => savedByStream.get(item.i) ?? item);
  const cols = positiveInteger(saved?.grid_cols) ?? GRID_COLS;
  const bottom = Math.max(1, ...items.map((item) => item.y + item.h));
  const rows = Math.max(positiveInteger(saved?.grid_rows) ?? GRID_ROWS, bottom);
  return { cols, rows, items };
}

function defaultViewerLayout(cameras: readonly CameraKey[]): LayoutItem[] {
  return cameras.map((camera, index) => ({
    i: camera.streamName,
    x: index === 0 ? 0 : 24 + ((index - 1) % 2) * 12,
    y: index === 0 ? 0 : Math.floor((index - 1) / 2) * 12,
    w: index === 0 ? 24 : 12,
    h: index === 0 ? 24 : 12,
    minW: 8,
    minH: 8,
  }));
}

export function viewerRect(item: LayoutItem, cols: number, rows: number): ViewerRect {
  const safeCols = positiveInteger(cols) ?? GRID_COLS;
  const safeRows = positiveInteger(rows) ?? GRID_ROWS;
  const widthUnits = clamp(item.w, 1, safeCols);
  const heightUnits = clamp(item.h, 1, safeRows);
  const x = clamp(item.x, 0, safeCols - widthUnits);
  const y = clamp(item.y, 0, safeRows - heightUnits);
  return {
    left: (x / safeCols) * 100,
    top: (y / safeRows) * 100,
    width: (widthUnits / safeCols) * 100,
    height: (heightUnits / safeRows) * 100,
  };
}

function positiveInteger(value: number | null | undefined): number | null {
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : null;
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}
