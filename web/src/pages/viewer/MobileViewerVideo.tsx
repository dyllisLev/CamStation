import type { Camera } from "../../app/api";
import { playbackStreamCandidates } from "../../components/live/streamSelection";
import { useWebRtcMseStream, type PlaybackPhase } from "../../components/live/useWebRtcMseStream";

type MobileViewerVideoProps = {
  readonly camera: Camera;
  readonly focused?: boolean;
  readonly announceStatus?: boolean;
};

export function MobileViewerVideo({ camera, focused = false, announceStatus = false }: MobileViewerVideoProps) {
  const { videoRef, connected, phase, usingFallback } = useWebRtcMseStream(
    playbackStreamCandidates(camera, focused),
  );

  return (
    <div className="mobile-viewer-video" data-phase={phase}>
      <video
        ref={videoRef}
        autoPlay
        muted
        playsInline
        disablePictureInPicture
        controls={false}
        aria-label={`${camera.name} 실시간 영상`}
      />
      {!connected && (
        <div
          className="mobile-viewer-video-status"
          aria-live={announceStatus ? "polite" : "off"}
          aria-hidden={announceStatus ? undefined : true}
        >
          {mobilePlaybackStatus(phase)}
        </div>
      )}
      {connected && usingFallback && <div className="mobile-viewer-fallback-badge">대체 스트림</div>}
    </div>
  );
}

function mobilePlaybackStatus(phase: PlaybackPhase) {
  switch (phase) {
    case "fallback":
      return "대체 영상 연결 중...";
    case "retrying":
      return "영상 재연결 중...";
    case "recovering":
      return "영상 복구 중...";
    case "cooldown":
      return "잠시 후 다시 연결합니다.";
    case "stalled":
      return "영상 수신 대기 중...";
    case "unsupported":
      return "이 브라우저에서는 재생할 수 없습니다.";
    default:
      return "연결 중...";
  }
}
