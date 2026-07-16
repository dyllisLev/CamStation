import { useWebRtcMseStream, type PlaybackPhase } from "./useWebRtcMseStream";

export type MsePlaybackPhase = PlaybackPhase;

export function useMseStream(streamNames: string | readonly string[]) {
  return useWebRtcMseStream(streamNames);
}
