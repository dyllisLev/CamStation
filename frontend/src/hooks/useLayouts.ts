import { useState, useEffect, useCallback } from 'react';
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

  useEffect(() => {
    if (cameras.length === 0 || initialized) return;
    setInitialized(true);

    getLayouts()
      .then(data => {
        setLayouts(data);
        const lastId = localStorage.getItem(LAST_LAYOUT_KEY);
        const last = data.find(l => l.id === lastId);
        if (last) {
          const merged = mergeWithCameras(last.data, cameras);
          setGridLayoutState(merged);
          setSavedSnapshot(merged);
          setCurrentId(last.id);
        } else {
          const def = defaultLayout(cameras);
          setGridLayoutState(def);
          setSavedSnapshot(def);
        }
      })
      .catch(() => {
        const def = defaultLayout(cameras);
        setGridLayoutState(def);
        setSavedSnapshot(def);
      });
  }, [cameras.length, initialized]);

  const setGridLayout = useCallback((layout: Layout[]) => {
    setGridLayoutState(layout);
    setIsDirty(true);
  }, []);

  const loadLayout = useCallback((id: string, cams: Camera[]) => {
    const target = layouts.find(l => l.id === id);
    if (!target) return;
    const merged = mergeWithCameras(target.data, cams);
    setGridLayoutState(merged);
    setSavedSnapshot(merged);
    setCurrentId(id);
    setIsDirty(false);
    localStorage.setItem(LAST_LAYOUT_KEY, id);
  }, [layouts]);

  const saveLayout = useCallback(async () => {
    if (!currentId) return;
    const items = gridLayout.map(toLayoutItem);
    await updateLayout(currentId, { data: items });
    setSavedSnapshot(gridLayout);
    setIsDirty(false);
    setLayouts(prev => prev.map(l =>
      l.id === currentId
        ? { ...l, data: items, updated_at: Math.floor(Date.now() / 1000) }
        : l,
    ));
  }, [currentId, gridLayout]);

  const saveAsLayout = useCallback(async (name: string) => {
    const items = gridLayout.map(toLayoutItem);
    const created = await createLayout({ name, data: items });
    setLayouts(prev => [created, ...prev]);
    setCurrentId(created.id);
    setSavedSnapshot(gridLayout);
    setIsDirty(false);
    localStorage.setItem(LAST_LAYOUT_KEY, created.id);
  }, [gridLayout]);

  const cancelEdit = useCallback(() => {
    setGridLayoutState(savedSnapshot);
    setIsDirty(false);
  }, [savedSnapshot]);

  const deleteLayoutById = useCallback(async (id: string) => {
    await deleteLayoutApi(id);
    setLayouts(prev => prev.filter(l => l.id !== id));
    if (currentId === id) {
      setCurrentId(null);
      setIsDirty(false);
    }
  }, [currentId]);

  return {
    layouts,
    currentId,
    gridLayout,
    isDirty,
    setGridLayout,
    loadLayout,
    saveLayout,
    saveAsLayout,
    cancelEdit,
    deleteLayoutById,
  };
}
