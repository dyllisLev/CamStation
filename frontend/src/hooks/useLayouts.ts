import { useState, useEffect, useCallback, useRef } from 'react';
import type { Layout } from 'react-grid-layout';
import type { Camera, LayoutItem, LayoutProfile } from '../types';
import {
  getLayouts,
  createLayout,
  updateLayout,
  deleteLayout as deleteLayoutApi,
} from '../api/client';
import { inferGridRows, scaleLayoutGridResolution } from '../layoutGrid';

const LAST_LAYOUT_KEY = 'camstation-last-layout-id';
const LEGACY_GRID_COLS = 12;
const LEGACY_GRID_ROWS = 12;

interface LayoutGridOptions {
  gridCols?: number;
  gridRows?: number;
}

function legacyDefaultItem(c: Camera, i: number): Layout {
  return {
    i: c.id,
    x: i === 0 ? 0 : 6 + ((i - 1) % 2) * 3,
    y: i === 0 ? 0 : Math.floor((i - 1) / 2) * 2,
    w: i === 0 ? 6 : 3,
    h: i === 0 ? 4 : 2,
    minW: 2,
    minH: 2,
  };
}

function scaleFromLegacy(layout: Layout[], targetCols: number, targetRows: number): Layout[] {
  return scaleLayoutGridResolution(layout, {
    fromCols: LEGACY_GRID_COLS,
    toCols: targetCols,
    fromRows: LEGACY_GRID_ROWS,
    toRows: targetRows,
  });
}

function defaultLayout(cameras: Camera[], targetCols: number, targetRows: number): Layout[] {
  return scaleFromLegacy(cameras.map(legacyDefaultItem), targetCols, targetRows);
}

function missingCameraLayout(camera: Camera, index: number, targetCols: number, targetRows: number): Layout {
  return scaleFromLegacy([{
    i: camera.id,
    x: (index % 4) * 3,
    y: Math.floor(index / 4) * 2,
    w: 3,
    h: 2,
    minW: 2,
    minH: 2,
  }], targetCols, targetRows)[0];
}

function sourceRowsFor(profile: Pick<LayoutProfile, 'data' | 'grid_rows'>): number {
  return profile.grid_rows ?? inferGridRows(profile.data, LEGACY_GRID_ROWS);
}

function normalizeProfileLayout(
  profile: LayoutProfile,
  targetCols: number,
  targetRowsOverride: number | undefined,
): Layout[] {
  const sourceCols = profile.grid_cols ?? LEGACY_GRID_COLS;
  const sourceRows = sourceRowsFor(profile);
  const targetRows = targetRowsOverride ?? sourceRows;
  return scaleLayoutGridResolution(profile.data, {
    fromCols: sourceCols,
    toCols: targetCols,
    fromRows: sourceRows,
    toRows: targetRows,
  });
}

function mergeWithCameras(saved: LayoutItem[], cameras: Camera[], targetCols: number, targetRows: number): Layout[] {
  const savedMap = new Map(saved.map(l => [l.i, l]));
  return cameras.map((c, i) => savedMap.get(c.id) ?? missingCameraLayout(c, i, targetCols, targetRows));
}

function toLayoutItem(l: Layout): LayoutItem {
  return { i: l.i, x: l.x, y: l.y, w: l.w, h: l.h, minW: l.minW, minH: l.minH };
}

export function useLayouts(cameras: Camera[], options: LayoutGridOptions = {}) {
  const targetCols = options.gridCols ?? LEGACY_GRID_COLS;
  const fixedTargetRows = options.gridRows;
  const [layouts, setLayouts] = useState<LayoutProfile[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [gridLayout, setGridLayoutState] = useState<Layout[]>([]);
  const [savedSnapshot, setSavedSnapshot] = useState<Layout[]>([]);
  const [isDirty, setIsDirty] = useState(false);
  const [timelineCollapsed, setTimelineCollapsed] = useState(false);
  // react-grid-layout fires onLayoutChange once on mount to normalize the layout.
  // This ref skips that first call so we don't incorrectly mark the layout as dirty.
  const skipNextChangeRef = useRef(false);
  // Track camera IDs to detect set changes after initialization.
  const initializedRef = useRef(false);
  const prevCameraIdsRef = useRef<string>('');

  const rowsForSave = useCallback((layout: Layout[]) => (
    fixedTargetRows ?? inferGridRows(layout, LEGACY_GRID_ROWS)
  ), [fixedTargetRows]);

  useEffect(() => {
    if (cameras.length === 0 || initializedRef.current) return;
    initializedRef.current = true;
    prevCameraIdsRef.current = cameras.map(c => c.id).join(',');

    getLayouts()
      .then(data => {
        setLayouts(data);
        const lastId = localStorage.getItem(LAST_LAYOUT_KEY);
        const last = data.find(l => l.id === lastId) ?? (data.length > 0 ? data[0] : null);
        if (last) {
          const targetRows = fixedTargetRows ?? sourceRowsFor(last);
          const normalized = normalizeProfileLayout(last, targetCols, fixedTargetRows);
          const merged = mergeWithCameras(normalized.map(toLayoutItem), cameras, targetCols, targetRows);
          skipNextChangeRef.current = true;
          setGridLayoutState(merged);
          setSavedSnapshot(merged);
          setCurrentId(last.id);
          setTimelineCollapsed(last.timeline_collapsed ?? false);
          localStorage.setItem(LAST_LAYOUT_KEY, last.id);
        } else {
          const def = defaultLayout(cameras, targetCols, fixedTargetRows ?? LEGACY_GRID_ROWS);
          skipNextChangeRef.current = true;
          setGridLayoutState(def);
          setSavedSnapshot(def);
        }
      })
      .catch(() => {
        const def = defaultLayout(cameras, targetCols, fixedTargetRows ?? LEGACY_GRID_ROWS);
        skipNextChangeRef.current = true;
        setGridLayoutState(def);
        setSavedSnapshot(def);
      });
  }, [cameras, cameras.length, fixedTargetRows, targetCols]);

  // Re-merge when camera set changes after initialization (handles add/remove/replace)
  useEffect(() => {
    if (!initializedRef.current || cameras.length === 0) return;
    const currentIds = cameras.map(c => c.id).join(',');
    if (currentIds === prevCameraIdsRef.current) return;
    prevCameraIdsRef.current = currentIds;
    const targetRows = rowsForSave(gridLayout);
    setGridLayoutState(prev => mergeWithCameras(prev.map(toLayoutItem), cameras, targetCols, targetRows));
    setSavedSnapshot(prev => mergeWithCameras(prev.map(toLayoutItem), cameras, targetCols, targetRows));
  }, [cameras, gridLayout, rowsForSave, targetCols]);

  const setGridLayout = useCallback((layout: Layout[]) => {
    if (skipNextChangeRef.current) {
      skipNextChangeRef.current = false;
      setGridLayoutState(layout);
      return; // savedSnapshot은 init/load 시 이미 설정됨 — 여기서 업데이트하면 연쇄 렌더 발생
    }
    setGridLayoutState(layout);
    setIsDirty(true);
  }, []);

  const loadLayout = useCallback((id: string, cams: Camera[]) => {
    const target = layouts.find(l => l.id === id);
    if (!target) return;
    const targetRows = fixedTargetRows ?? sourceRowsFor(target);
    const normalized = normalizeProfileLayout(target, targetCols, fixedTargetRows);
    const merged = mergeWithCameras(normalized.map(toLayoutItem), cams, targetCols, targetRows);
    skipNextChangeRef.current = true;
    setGridLayoutState(merged);
    setSavedSnapshot(merged);
    setCurrentId(id);
    setTimelineCollapsed(target.timeline_collapsed ?? false);
    setIsDirty(false);
    localStorage.setItem(LAST_LAYOUT_KEY, id);
  }, [layouts, fixedTargetRows, targetCols]);

  const saveLayout = useCallback(async () => {
    if (!currentId) return;
    const items = gridLayout.map(toLayoutItem);
    const gridRows = rowsForSave(gridLayout);
    try {
      await updateLayout(currentId, {
        data: items,
        timeline_collapsed: timelineCollapsed,
        grid_cols: targetCols,
        grid_rows: gridRows,
      });
    } catch (e) {
      console.error('Failed to save layout:', e);
      return;
    }
    setSavedSnapshot(gridLayout);
    setIsDirty(false);
    setLayouts(prev => prev.map(l =>
      l.id === currentId
        ? {
          ...l,
          data: items,
          timeline_collapsed: timelineCollapsed,
          grid_cols: targetCols,
          grid_rows: gridRows,
          updated_at: Math.floor(Date.now() / 1000),
        }
        : l,
    ));
  }, [currentId, gridLayout, rowsForSave, targetCols, timelineCollapsed]);

  const saveAsLayout = useCallback(async (name: string) => {
    const items = gridLayout.map(toLayoutItem);
    const gridRows = rowsForSave(gridLayout);
    let created: LayoutProfile;
    try {
      created = await createLayout({
        name,
        data: items,
        timeline_collapsed: timelineCollapsed,
        grid_cols: targetCols,
        grid_rows: gridRows,
      });
    } catch (e) {
      console.error('Failed to create layout:', e);
      return;
    }
    setLayouts(prev => [created, ...prev]);
    setCurrentId(created.id);
    setSavedSnapshot(gridLayout);
    setIsDirty(false);
    localStorage.setItem(LAST_LAYOUT_KEY, created.id);
  }, [gridLayout, rowsForSave, targetCols, timelineCollapsed]);

  const cancelEdit = useCallback(() => {
    skipNextChangeRef.current = true;
    setGridLayoutState(savedSnapshot);
    setIsDirty(false);
  }, [savedSnapshot]);

  const toggleTimelineCollapsed = useCallback(() => {
    setTimelineCollapsed(p => !p);
    setIsDirty(true);
  }, []);

  const deleteLayoutById = useCallback(async (id: string) => {
    try {
      await deleteLayoutApi(id);
    } catch (e) {
      console.error('Failed to delete layout:', e);
      return;
    }
    setLayouts(prev => prev.filter(l => l.id !== id));
    if (currentId === id) {
      setCurrentId(null);
      setIsDirty(false);
      localStorage.removeItem(LAST_LAYOUT_KEY);
    }
  }, [currentId]);

  return {
    layouts,
    currentId,
    gridLayout,
    isDirty,
    timelineCollapsed,
    setGridLayout,
    loadLayout,
    saveLayout,
    saveAsLayout,
    cancelEdit,
    deleteLayoutById,
    toggleTimelineCollapsed,
  };
}
