import { useEffect, useRef, useState } from 'react';

// Codecs go2rtc supports, in priority order
const CODECS = [
  'avc1.640029',  // H.264 high 4.1
  'avc1.64002A',  // H.264 high 4.2
  'avc1.640033',  // H.264 high 5.1
  'mp4a.40.2',    // AAC LC
  'mp4a.40.5',    // AAC HE
  'opus',
];

// ManagedMediaSource is Safari 17+ only (standard MediaSource doesn't work on iOS)
function getMediaSourceClass(): typeof MediaSource | null {
  if ('ManagedMediaSource' in window) {
    return (window as unknown as { ManagedMediaSource: typeof MediaSource }).ManagedMediaSource;
  }
  if ('MediaSource' in window) return MediaSource;
  return null;
}

function buildCodecList(MSClass: typeof MediaSource): string {
  return CODECS.filter(c => MSClass.isTypeSupported(`video/mp4; codecs="${c}"`)).join(',');
}

export function useMSE(camId: string) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!camId) return;
    const videoEl = videoRef.current;
    if (!videoEl) return;
    const video: HTMLVideoElement = videoEl;

    const msClassOrNull = getMediaSourceClass();
    if (!msClassOrNull) return; // No MSE support on this browser
    const MSClass: typeof MediaSource = msClassOrNull; // non-nullable for closures

    const useSrcObject = 'ManagedMediaSource' in window; // Safari 17+

    let destroyed = false;
    let ws: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

    function connect() {
      if (destroyed) return;

      let sb: SourceBuffer | null = null;
      const buf = new Uint8Array(2 * 1024 * 1024); // 2 MB pre-allocated buffer
      let bufLen = 0;
      let hasVideo = false;
      let srcUrl = '';

      const ms = new MSClass();

      if (useSrcObject) {
        // ManagedMediaSource (Safari 17+): use srcObject
        video.srcObject = ms;
      } else {
        srcUrl = URL.createObjectURL(ms);
        video.src = srcUrl;
        video.srcObject = null;
      }

      function flush() {
        if (!sb || sb.updating || bufLen === 0) return;
        try { sb.appendBuffer(buf.slice(0, bufLen)); bufLen = 0; } catch {}
      }

      ms.addEventListener('sourceopen', () => {
        if (srcUrl) { URL.revokeObjectURL(srcUrl); srcUrl = ''; }

        const proto = location.protocol === 'https:' ? 'wss' : 'ws';
        ws = new WebSocket(
          `${proto}://${location.host}/go2rtc/api/ws?src=${encodeURIComponent(camId)}`
        );
        ws.binaryType = 'arraybuffer';

        const codecs = buildCodecList(MSClass);

        ws.onopen = () => {
          // go2rtc protocol: client announces supported codecs first
          ws!.send(JSON.stringify({ type: 'mse', value: codecs }));
        };

        ws.onmessage = (e) => {
          if (typeof e.data === 'string') {
            const msg = JSON.parse(e.data) as { type: string; value: string };
            if (msg.type === 'mse') {
              // go2rtc responds with the MIME type to use for SourceBuffer
              try {
                sb = ms.addSourceBuffer(msg.value);
                sb.mode = 'segments';
                sb.addEventListener('updateend', () => {
                  flush();
                  // Keep at live edge
                  if (sb && !sb.updating && sb.buffered.length) {
                    const end = sb.buffered.end(sb.buffered.length - 1);
                    const trim = end - 5;
                    const start0 = sb.buffered.start(0);
                    if (trim > start0) { try { sb.remove(start0, trim); } catch {} }
                    if (video.currentTime < trim) video.currentTime = trim;
                  }
                });
              } catch {}
            }
          } else {
            // Binary: fMP4 segment
            const b = new Uint8Array(e.data as ArrayBuffer);
            if (sb && !sb.updating && bufLen === 0) {
              try { sb.appendBuffer(e.data as ArrayBuffer); } catch {}
            } else {
              buf.set(b, bufLen);
              bufLen += b.byteLength;
            }
            if (!hasVideo) {
              hasVideo = true;
              setConnected(true);
              video.play().catch(() => {});
            }
          }
        };

        ws.onclose = () => {
          setConnected(false);
          if (!destroyed) reconnectTimer = setTimeout(connect, 3000);
        };

        ws.onerror = () => ws?.close();
      }, { once: true });

      // Start buffering/playback early
      video.play().catch(() => {});
    }

    connect();

    return () => {
      destroyed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      ws?.close();
      setConnected(false);
    };
  }, [camId]);

  return { videoRef, connected };
}
