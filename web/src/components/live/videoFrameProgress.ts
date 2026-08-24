export function observePresentedVideoFrames(
  video: HTMLVideoElement,
  onFrame: () => void,
): () => void {
  if (typeof video.requestVideoFrameCallback !== "function") return () => undefined;

  let stopped = false;
  let handle: number | null = null;

  const schedule = () => {
    try {
      handle = video.requestVideoFrameCallback(() => {
        handle = null;
        if (stopped) return;
        schedule();
        onFrame();
      });
    } catch {
      stopped = true;
      handle = null;
    }
  };

  schedule();
  return () => {
    if (stopped) return;
    stopped = true;
    if (handle !== null && typeof video.cancelVideoFrameCallback === "function") {
      video.cancelVideoFrameCallback(handle);
    }
    handle = null;
  };
}
