import type { Camera } from "../../app/api";

type CameraPlaybackStreams = Pick<Camera, "streamName" | "liveStreamName" | "recordingStreamName" | "focusStreamName">;

export function playbackStreamName(camera: CameraPlaybackStreams, focused = false) {
  return (focused ? camera.focusStreamName || camera.recordingStreamName : "") || camera.liveStreamName || camera.streamName;
}

export function shouldRenderLiveTile(cameraKey: string, focusedCameraKey: string | null) {
  return cameraKey !== focusedCameraKey;
}
