import type { LayoutItem, LayoutProfile } from "../../app/api";

export const GRID_COLS = 48;
export const GRID_ROWS = 48;

type CameraKey = { readonly streamName: string };
type SavedLayout = Pick<LayoutProfile, "id" | "data" | "timeline_collapsed">;

export type VideoViewport = { scale: number; tx: number; ty: number };
export type MonitorLayoutItem = LayoutItem & { videoZoom?: VideoViewport };
export type ResolvedLayout = {
  currentId: string;
  layout: MonitorLayoutItem[];
  timelineCollapsed: boolean | undefined;
};

export function resolveInitialLayout(
  cameras: readonly CameraKey[],
  layouts: readonly SavedLayout[],
  savedId: string | null,
  layoutsReady: boolean,
): ResolvedLayout | null {
  if (!layoutsReady || cameras.length === 0) return null;
  const saved = layouts.find((item) => item.id === savedId) ?? layouts[0];
  return saved
    ? { currentId: saved.id, layout: mergeWithCameras(saved.data, cameras), timelineCollapsed: saved.timeline_collapsed }
    : { currentId: "", layout: defaultLayout(cameras), timelineCollapsed: undefined };
}

export function resolveLayoutAfterDelete(
  deletedId: string,
  currentId: string,
  layouts: readonly SavedLayout[],
  cameras: readonly CameraKey[],
): ResolvedLayout | null {
  if (deletedId !== currentId) return null;
  const remaining = layouts.filter((item) => item.id !== deletedId);
  return resolveInitialLayout(cameras, remaining, remaining[0]?.id ?? null, true);
}

export function defaultLayout(cameras: readonly CameraKey[]): MonitorLayoutItem[] {
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

export function mergeWithCameras(
  saved: readonly LayoutItem[],
  cameras: readonly CameraKey[],
): MonitorLayoutItem[] {
  const savedMap = new Map(saved.map((item) => [item.i, item]));
  return cameras.map(
    (camera, index) =>
      savedMap.get(camera.streamName) ?? {
        ...defaultLayout([camera])[0],
        x: 24 + (index % 2) * 12,
        y: Math.floor(index / 2) * 12,
      },
  );
}

export function clampLayout(layout: readonly MonitorLayoutItem[]): MonitorLayoutItem[] {
  return layout.map((item) => {
    const minW = item.minW ?? 1;
    const minH = item.minH ?? 1;
    const w = Math.min(Math.max(item.w, minW), GRID_COLS);
    const h = Math.min(Math.max(item.h, minH), GRID_ROWS);
    return {
      ...item,
      w,
      h,
      x: Math.min(Math.max(item.x, 0), GRID_COLS - w),
      y: Math.min(Math.max(item.y, 0), GRID_ROWS - h),
    };
  });
}
