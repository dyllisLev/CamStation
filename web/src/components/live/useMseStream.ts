import type { PlaybackSurface } from "../../app/playbackDiagnosticsApi";
import { useWebRtcMseStream, type PlaybackPhase } from "./useWebRtcMseStream";

export type MsePlaybackPhase = PlaybackPhase;

export function useMseStream(streamNames: string | readonly string[], surface: PlaybackSurface = "unknown") {
  return useWebRtcMseStream(streamNames, 0, "mse", surface);
}
