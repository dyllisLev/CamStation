import { useEffect, useRef, useState } from 'react';
import { markViewerCameraEvent, registerViewerCamera, unregisterViewerCamera } from '../viewerHealth';

// Codecs go2rtc supports, in priority order
const CODECS = [
  'avc1.640029',  // H.264 high 4.1
  'avc1.64002A',  // H.264 high 4.2
  'avc1.640033',  // H.264 high 5.1
  'mp4a.40.2',    // AAC LC
  'mp4a.40.5',    // AAC HE
  'opus',
];

// If no binary data arrives for this long, force-close the WS to trigger reconnect
const STALL_MS = 10_000;

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
    registerViewerCamera(camId);
    const videoEl = videoRef.current;
    if (!videoEl) {
      unregisterViewerCamera(camId);
      return;
    }
    const video: HTMLVideoElement = videoEl;

    const msClassOrNull = getMediaSourceClass();
    if (!msClassOrNull) {
      unregisterViewerCamera(camId);
      return;
    }
    const MSClass: typeof MediaSource = msClassOrNull;

    const useSrcObject = 'ManagedMediaSource' in window;

    let destroyed = false;
    let gen = 0;
    let activeWs: WebSocket | null = null;
    let activeMs: InstanceType<typeof MediaSource> | null = null;
    let activeSrcUrl = '';
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let stallTimer: ReturnType<typeof setTimeout> | null = null;
    let reconnectCount = 0;
    let lastVideoTime = video.currentTime || 0;
    let lastVideoTimeAt = Date.now();
    let videoProgressTimer: ReturnType<typeof setInterval> | null = null;

    function clearStallTimer() {
      if (stallTimer) { clearTimeout(stallTimer); stallTimer = null; }
    }

    // Properly tears down the current pipeline before each reconnect
    function teardown() {
      clearStallTimer();
      // Null handlers first so no stale callbacks fire after close
      if (activeWs) {
        activeWs.onopen = null;
        activeWs.onmessage = null;
        activeWs.onclose = null;
        activeWs.onerror = null;
        activeWs.close();
        activeWs = null;
      }
      if (activeMs) {
        try { if (activeMs.readyState === 'open') activeMs.endOfStream(); } catch {}
        activeMs = null;
      }
      if (activeSrcUrl) {
        URL.revokeObjectURL(activeSrcUrl);
        activeSrcUrl = '';
      }
      video.src = '';
      video.srcObject = null;
    }

    function scheduleReconnect() {
      reconnectCount += 1;
      markViewerCameraEvent(camId, { connected: false, reconnectCount });
      if (!destroyed) reconnectTimer = setTimeout(connect, 3000);
    }

    function connect() {
      if (destroyed) return;
      teardown();

      const myGen = ++gen;

      // 2 MB scratch buffer for segments arriving while SourceBuffer is busy
      const buf = new Uint8Array(2 * 1024 * 1024);
      let bufLen = 0;
      let sb: SourceBuffer | null = null;
      let hasVideo = false;

      const ms = new MSClass();
      activeMs = ms;

      if (useSrcObject) {
        video.srcObject = ms;
      } else {
        activeSrcUrl = URL.createObjectURL(ms);
        video.src = activeSrcUrl;
        video.srcObject = null;
        console.log(`[MSE] ${camId} → ${activeSrcUrl}`);
      }

      function flush() {
        if (!sb || sb.updating || bufLen === 0) return;
        const data = buf.slice(0, bufLen).buffer;
        bufLen = 0;
        try { sb.appendBuffer(data); } catch {}
      }

      ms.addEventListener('sourceopen', () => {
        if (myGen !== gen || destroyed) return;
        if (activeSrcUrl) { URL.revokeObjectURL(activeSrcUrl); activeSrcUrl = ''; }

        const proto = location.protocol === 'https:' ? 'wss' : 'ws';
        const ws = new WebSocket(
          `${proto}://${location.host}/go2rtc/api/ws?src=${encodeURIComponent(camId)}`
        );
        activeWs = ws;
        ws.binaryType = 'arraybuffer';

        ws.onopen = () => {
          if (myGen !== gen) return;
          markViewerCameraEvent(camId, { connected: true, videoReadyState: video.readyState, reconnectCount, error: null });
          ws.send(JSON.stringify({ type: 'mse', value: buildCodecList(MSClass) }));
        };

        ws.onmessage = (e) => {
          if (myGen !== gen) return;

          if (typeof e.data === 'string') {
            const msg = JSON.parse(e.data) as { type: string; value: string };
            if (msg.type === 'mse') {
              try {
                sb = ms.addSourceBuffer(msg.value);
                sb.mode = 'segments';
                sb.addEventListener('updateend', () => {
                  if (myGen !== gen) return;
                  flush();
                  if (sb && !sb.updating && sb.buffered.length) {
                    const end = sb.buffered.end(sb.buffered.length - 1);
                    const start0 = sb.buffered.start(0);
                    // Only trim when buffer grows large, keep 5s at live edge
                    if (end - start0 > 10) {
                      const trim = end - 5;
                      if (trim > start0) { try { sb.remove(start0, trim); } catch {} }
                    }
                    // Only snap to live edge if we've fallen significantly behind
                    if (end - video.currentTime > 5) {
                      video.currentTime = end - 0.5;
                    }
                  }
                });
              } catch {}
            }
          } else {
            // Binary fMP4 segment: reset stall watchdog
            clearStallTimer();
            stallTimer = setTimeout(() => { if (!destroyed) ws.close(); }, STALL_MS);

            const b = new Uint8Array(e.data as ArrayBuffer);
            markViewerCameraEvent(camId, {
              connected: true,
              videoReadyState: video.readyState,
              binaryBytes: b.byteLength,
              videoTime: video.currentTime,
              stalledMs: 0,
              reconnectCount,
              error: null,
            });
            if (sb && !sb.updating && bufLen === 0) {
              try { sb.appendBuffer(e.data as ArrayBuffer); } catch {}
            } else if (bufLen + b.byteLength <= buf.byteLength) {
              buf.set(b, bufLen);
              bufLen += b.byteLength;
            }
            // else: drop chunk rather than overflow the scratch buffer

            if (!hasVideo) {
              hasVideo = true;
              setConnected(true);
              video.play().catch(() => {});
            }
          }
        };

        ws.onclose = () => {
          if (myGen !== gen) return;
          setConnected(false);
          markViewerCameraEvent(camId, { connected: false, videoReadyState: video.readyState, reconnectCount, error: null });
          scheduleReconnect();
        };

        ws.onerror = () => {
          if (myGen !== gen) return;
          markViewerCameraEvent(camId, { connected: false, videoReadyState: video.readyState, reconnectCount, error: 'websocket_error' });
          ws.close();
        };
      }, { once: true });

      video.play().catch(() => {});
    }

    // video.error is the last resort — browser-level decode failure
    const onVideoError = () => {
      if (!destroyed) {
        markViewerCameraEvent(camId, { connected: false, videoReadyState: video.readyState, reconnectCount, error: 'video_error' });
        setConnected(false);
        scheduleReconnect();
      }
    };
    video.addEventListener('error', onVideoError);

    videoProgressTimer = setInterval(() => {
      const currentVideoTime = video.currentTime || 0;
      const now = Date.now();
      if (currentVideoTime !== lastVideoTime) {
        lastVideoTime = currentVideoTime;
        lastVideoTimeAt = now;
      }
      const stalledMs = now - lastVideoTimeAt;
      markViewerCameraEvent(camId, {
        connected: !!activeWs && activeWs.readyState === WebSocket.OPEN,
        videoReadyState: video.readyState,
        videoTime: currentVideoTime,
        stalledMs,
        reconnectCount,
        error: stalledMs >= STALL_MS ? 'video_stalled' : null,
      });
    }, 5000);

    connect();

    return () => {
      destroyed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      if (videoProgressTimer) clearInterval(videoProgressTimer);
      video.removeEventListener('error', onVideoError);
      teardown();
      unregisterViewerCamera(camId);
      setConnected(false);
    };
  }, [camId]);

  return { videoRef, connected };
}
