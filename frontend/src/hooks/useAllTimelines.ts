import { useState, useEffect } from 'react';
import { getTimeline } from '../api/client';
import type { Camera, TimelineData } from '../types';

export function useAllTimelines(cameras: Camera[], date: string) {
  const [data, setData] = useState<Record<string, TimelineData>>({});

  useEffect(() => {
    if (cameras.length === 0) return;
    const load = () =>
      Promise.all(cameras.map(c => getTimeline(c.id, date).then(d => [c.id, d] as const)))
        .then(entries => setData(Object.fromEntries(entries)))
        .catch(console.error);
    load();
    const id = setInterval(load, 30000);
    return () => clearInterval(id);
  }, [cameras, date]);

  return data;
}
