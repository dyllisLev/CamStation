import type { Camera } from "../../app/api";

type CameraPlaybackStreams = Pick<Camera, "streamName" | "liveStreamName" | "recordingStreamName" | "focusStreamName">;

export function playbackStreamCandidates(camera: CameraPlaybackStreams, focused = false): string[] {
  const roleCandidates = focused
    ? [camera.focusStreamName, camera.liveStreamName]
    : [camera.liveStreamName, camera.focusStreamName];
  const distinct = roleCandidates.filter(
    (streamName, index): streamName is string => Boolean(streamName) && roleCandidates.indexOf(streamName) === index,
  );
  return distinct.length > 0 ? distinct : [camera.streamName];
}

export function playbackStreamName(camera: CameraPlaybackStreams, focused = false) {
  return playbackStreamCandidates(camera, focused)[0];
}

export function shouldRenderLiveTile(cameraKey: string, focusedCameraKey: string | null) {
  return cameraKey !== focusedCameraKey;
}
