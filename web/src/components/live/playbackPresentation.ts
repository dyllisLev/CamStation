import type { PlaybackTransport } from "./playbackRecovery";

type PlaybackPresentationPhase =
  | "connecting"
  | "retrying"
  | "fallback"
  | "recovering"
  | "playing"
  | "stalled"
  | "cooldown"
  | "unsupported";

export function playbackStatusCopy(
  phase: PlaybackPresentationPhase,
  transport: PlaybackTransport,
) {
  switch (phase) {
    case "fallback":
      return "대체 스트림 연결 중...";
    case "retrying":
      return transport === "mse" ? "영상 연결 방식 전환 중..." : "영상 입력 재연결 중...";
    case "recovering":
      return "영상 입력을 다시 구독하는 중...";
    case "stalled":
      return "멈춘 영상 입력을 복구하는 중...";
    case "cooldown":
      return "자동 복구 한도에 도달했습니다.";
    case "unsupported":
      return "이 브라우저는 라이브 재생을 지원하지 않습니다.";
    default:
      return "연결 중...";
  }
}
