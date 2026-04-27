import { useState, useEffect } from 'react';
import { getCameras } from '../api/client';
import type { Camera } from '../types';

export function useCameras(pollMs = 10000) {
  const [cameras, setCameras] = useState<Camera[]>([]);

  useEffect(() => {
    getCameras().then(setCameras).catch(console.error);
    const id = setInterval(() => getCameras().then(setCameras).catch(console.error), pollMs);
    return () => clearInterval(id);
  }, [pollMs]);

  return cameras;
}
