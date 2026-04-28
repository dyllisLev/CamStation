import { useEffect, useRef, useState } from 'react';

export function useMSE(camId: string) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!camId) return;
    const videoEl = videoRef.current;
    if (!videoEl) return;

    // Explicitly typed as non-nullable so nested closures see HTMLVideoElement
    const video: HTMLVideoElement = videoEl;

    let destroyed = false;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let currentWs: WebSocket | null = null;
    let currentUrl = '';

    function connect() {
      if (destroyed) return;

      let sb: SourceBuffer | null = null;
      const queue: ArrayBuffer[] = [];
      let hasVideo = false;

      const ms = new MediaSource();
      currentUrl = URL.createObjectURL(ms);
      video.src = currentUrl;

      function flush() {
        if (!sb || sb.updating || queue.length === 0) return;
        try { sb.appendBuffer(queue.shift()!); } catch { queue.length = 0; }
      }

      function trimBuffer() {
        if (!sb || sb.updating || !video.buffered.length) return;
        const behind = video.currentTime - video.buffered.start(0);
        if (behind > 30) {
          try { sb.remove(video.buffered.start(0), video.currentTime - 10); } catch {}
        }
      }

      ms.addEventListener('sourceopen', () => {
        const proto = location.protocol === 'https:' ? 'wss' : 'ws';
        const ws = new WebSocket(
          `${proto}://${location.host}/go2rtc/api/ws?src=${encodeURIComponent(camId)}`
        );
        currentWs = ws;
        ws.binaryType = 'arraybuffer';

        ws.onmessage = (e) => {
          if (typeof e.data === 'string') {
            try {
              sb = ms.addSourceBuffer(e.data);
              sb.addEventListener('updateend', () => { trimBuffer(); flush(); });
            } catch {}
          } else {
            queue.push(e.data as ArrayBuffer);
            flush();
            if (!hasVideo) {
              hasVideo = true;
              setConnected(true);
              video.play().catch(() => {});
            }
          }
        };

        ws.onclose = () => {
          URL.revokeObjectURL(currentUrl);
          setConnected(false);
          if (!destroyed) {
            reconnectTimer = setTimeout(connect, 2000);
          }
        };

        ws.onerror = () => ws.close();
      }, { once: true });
    }

    connect();

    return () => {
      destroyed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      currentWs?.close();
      URL.revokeObjectURL(currentUrl);
      setConnected(false);
    };
  }, [camId]);

  return { videoRef, connected };
}
