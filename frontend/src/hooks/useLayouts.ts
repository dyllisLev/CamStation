import { useState, useEffect, useCallback, useRef } from 'react';
import type { Layout } from 'react-grid-layout';
import type { Camera, LayoutItem, LayoutProfile } from '../types';
import {
  getLayouts,
  createLayout,
  updateLayout,
  deleteLayout as deleteLayoutApi,
} from '../api/client';

const LAST_LAYOUT_KEY = 'camstation-last-layout-id';

function defaultLayout(cameras: Camera[]): Layout[] {
  return cameras.map((c, i) => ({
    i: c.id,
    x: i === 0 ? 0 : 6 + ((i - 1) % 2) * 3,
    y: i === 0 ? 0 : Math.floor((i - 1) / 2) * 2,
    w: i === 0 ? 6 : 3,
    h: i === 0 ? 4 : 2,
    minW: 2,
    minH: 2,
  }));
}

function mergeWithCameras(saved: LayoutItem[], cameras: Camera[]): Layout[] {
  const savedMap = new Map(saved.map(l => [l.i, l]));
  return cameras.map((c, i) => {
    const s = savedMap.get(c.id);
    return s ?? {
      i: c.id,
      x: (i % 4) * 3,
      y: Math.floor(i / 4) * 2,
      w: 3, h: 2, minW: 2, minH: 2,
    };
  });
}

function toLayoutItem(l: Layout): LayoutItem {
  return { i: l.i, x: l.x, y: l.y, w: l.w, h: l.h, minW: l.minW, minH: l.minH };
}

export function useLayouts(cameras: Camera[]) {
  const [layouts, setLayouts] = useState<LayoutProfile[]>([]);
  const [currentId, setCurrentId] = useState<string | null>(null);
  const [gridLayout, setGridLayoutState] = useState<Layout[]>([]);
  const [savedSnapshot, setSavedSnapshot] = useState<Layout[]>([]);
  const [isDirty, setIsDirty] = useState(false);
  const [initialized, setInitialized] = useState(false);
  const [timelineCollapsed, setTimelineCollapsed] = useState(false);
  // react-grid-layout fires onLayoutChange once on mount to normalize the layout.
  // This ref skips that first call so we don't incorrectly mark the layout as dirty.
  const skipNextChangeRef = useRef(false);
  // Track camera IDs to detect set changes after initialization.
  const prevCameraIdsRef = useRef<string>('');

  useEffect(() => {
    if (cameras.length === 0 || initialized) return;
    setInitialized(true);
    prevCameraIdsRef.current = cameras.map(c => c.id).join(',');

    getLayouts()
      .then(data => {
        setLayouts(data);
        const lastId = localStorage.getItem(LAST_LAYOUT_KEY);
        const last = data.find(l => l.id === lastId);
        if (last) {
          const merged = mergeWithCameras(last.data, cameras);
          skipNextChangeRef.current = true;
          setGridLayoutState(merged);
          setSavedSnapshot(merged);
          setCurrentId(last.id);
          setTimelineCollapsed(last.timeline_collapsed ?? false);
        } else {
          const def = defaultLayout(cameras);
          skipNextChangeRef.current = true;
          setGridLayoutState(def);
          setSavedSnapshot(def);
        }
      })
      .catch(() => {
        const def = defaultLayout(cameras);
        skipNextChangeRef.current = true;
        setGridLayoutState(def);
        setSavedSnapshot(def);
      });
  }, [cameras.length, initialized]);

  // Re-merge when camera set changes after initialization (handles add/remove/replace)
  useEffect(() => {
    if (!initialized || cameras.length === 0) return;
    const currentIds = cameras.map(c => c.id).join(',');
    if (currentIds === prevCameraIdsRef.current) return;
    prevCameraIdsRef.current = currentIds;
    setGridLayoutState(prev => mergeWithCameras(prev.map(toLayoutItem), cameras));
    setSavedSnapshot(prev => mergeWithCameras(prev.map(toLayoutItem), cameras));
  }, [cameras, initialized]);

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
    const merged = mergeWithCameras(target.data, cams);
    skipNextChangeRef.current = true;
    setGridLayoutState(merged);
    setSavedSnapshot(merged);
    setCurrentId(id);
    setTimelineCollapsed(target.timeline_collapsed ?? false);
    setIsDirty(false);
    localStorage.setItem(LAST_LAYOUT_KEY, id);
  }, [layouts]);

  const saveLayout = useCallback(async () => {
    if (!currentId) return;
    const items = gridLayout.map(toLayoutItem);
    try {
      await updateLayout(currentId, { data: items, timeline_collapsed: timelineCollapsed });
    } catch (e) {
      console.error('Failed to save layout:', e);
      return;
    }
    setSavedSnapshot(gridLayout);
    setIsDirty(false);
    setLayouts(prev => prev.map(l =>
      l.id === currentId
        ? { ...l, data: items, timeline_collapsed: timelineCollapsed, updated_at: Math.floor(Date.now() / 1000) }
        : l,
    ));
  }, [currentId, gridLayout, timelineCollapsed]);

  const saveAsLayout = useCallback(async (name: string) => {
    const items = gridLayout.map(toLayoutItem);
    let created: LayoutProfile;
    try {
      created = await createLayout({ name, data: items, timeline_collapsed: timelineCollapsed });
    } catch (e) {
      console.error('Failed to create layout:', e);
      return;
    }
    setLayouts(prev => [created, ...prev]);
    setCurrentId(created.id);
    setSavedSnapshot(gridLayout);
    setIsDirty(false);
    localStorage.setItem(LAST_LAYOUT_KEY, created.id);
  }, [gridLayout, timelineCollapsed]);

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
