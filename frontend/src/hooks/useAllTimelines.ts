import { useState, useEffect, useMemo } from 'react';
import { getTimeline } from '../api/client';
import type { Camera, TimelineData } from '../types';

export function useAllTimelines(cameras: Camera[], date: string) {
  const [data, setData] = useState<Record<string, TimelineData>>({});
  const camIds = useMemo(() => cameras.map(c => c.id).join(','), [cameras]);

  useEffect(() => {
    if (!camIds) return;
    const ids = camIds.split(',');
    const load = () =>
      Promise.all(ids.map(id => getTimeline(id, date).then(d => [id, d] as const)))
        .then(entries => setData(Object.fromEntries(entries)))
        .catch(console.error);
    load();
    const id = setInterval(load, 30000);
    return () => clearInterval(id);
  }, [camIds, date]);

  return data;
}
