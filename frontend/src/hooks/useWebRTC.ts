import { useEffect, useRef, useState } from 'react';

export function useWebRTC(camId: string) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const pcRef = useRef<RTCPeerConnection | null>(null);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    if (!camId) return;

    async function connect() {
      const pc = new RTCPeerConnection({ iceServers: [] });
      pcRef.current = pc;

      pc.addTransceiver('video', { direction: 'recvonly' });
      pc.addTransceiver('audio', { direction: 'recvonly' });

      pc.ontrack = (e) => {
        if (videoRef.current && e.streams[0]) {
          videoRef.current.srcObject = e.streams[0];
          setConnected(true);
        }
      };

      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);

      const resp = await fetch(`/api/streams/${encodeURIComponent(camId)}/webrtc`, {
        method: 'POST',
        body: offer.sdp,
        headers: { 'Content-Type': 'application/sdp' },
      });
      const answer = await resp.text();
      await pc.setRemoteDescription({ type: 'answer', sdp: answer });
    }

    connect().catch(console.error);

    return () => {
      pcRef.current?.close();
      setConnected(false);
    };
  }, [camId]);

  return { videoRef, connected };
}
