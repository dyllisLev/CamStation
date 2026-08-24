import assert from "node:assert/strict";
import test from "node:test";
import { observePresentedVideoFrames } from "../src/components/live/videoFrameProgress.ts";

type FrameCallback = (now: number, metadata: VideoFrameCallbackMetadata) => void;

class FakeVideoFrameSource {
  nextHandle = 1;
  callbacks = new Map<number, FrameCallback>();
  cancelled: number[] = [];

  requestVideoFrameCallback(callback: FrameCallback): number {
    const handle = this.nextHandle++;
    this.callbacks.set(handle, callback);
    return handle;
  }

  cancelVideoFrameCallback(handle: number): void {
    this.cancelled.push(handle);
    this.callbacks.delete(handle);
  }

  present(handle: number): void {
    const callback = this.callbacks.get(handle);
    assert.ok(callback);
    this.callbacks.delete(handle);
    callback(1_000, {} as VideoFrameCallbackMetadata);
  }
}

test("presented-frame observation rearms after every real frame and cancels exactly once", () => {
  const video = new FakeVideoFrameSource();
  let frames = 0;

  const stop = observePresentedVideoFrames(video as unknown as HTMLVideoElement, () => frames++);
  assert.deepEqual([...video.callbacks.keys()], [1]);

  video.present(1);
  assert.equal(frames, 1);
  assert.deepEqual([...video.callbacks.keys()], [2]);

  video.present(2);
  assert.equal(frames, 2);
  assert.deepEqual([...video.callbacks.keys()], [3]);

  stop();
  stop();
  assert.deepEqual(video.cancelled, [3]);
  assert.equal(video.callbacks.size, 0);
});

test("presented-frame observation is a safe no-op when Chromium lacks the API", () => {
  let frames = 0;
  const stop = observePresentedVideoFrames({} as HTMLVideoElement, () => frames++);

  assert.doesNotThrow(stop);
  assert.equal(frames, 0);
});
