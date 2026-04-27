import { useState, useEffect } from 'react';
import { getTimeline } from '../api/client';
import type { TimelineData } from '../types';

export function useTimeline(camId: string, date: string) {
  const [data, setData] = useState<TimelineData>({ segments: [], motion_events: [] });

  useEffect(() => {
    if (!camId || !date) return;
    getTimeline(camId, date).then(setData).catch(console.error);
    const id = setInterval(() => getTimeline(camId, date).then(setData).catch(console.error), 30000);
    return () => clearInterval(id);
  }, [camId, date]);

  return data;
}
