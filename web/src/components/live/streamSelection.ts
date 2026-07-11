import type { Camera } from "../../app/api";

type CameraPlaybackStreams = Pick<Camera, "streamName" | "liveStreamName" | "recordingStreamName">;

export function playbackStreamName(camera: CameraPlaybackStreams, focused = false) {
  return (focused ? camera.recordingStreamName : "") || camera.liveStreamName || camera.streamName;
}
