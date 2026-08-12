import { useCallback, useEffect, useRef } from "react";
import { cameraControlApi } from "../../app/cameraControlApi";
import type { PTZMoveVector } from "../../app/cameraTypes";

export function usePtzHold(streamName: string, onError: (message: string) => void) {
  const generationRef = useRef(0);
  const intentRef = useRef(0);
  const timerRef = useRef<number | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const inFlightRef = useRef<Promise<unknown> | null>(null);
  const activeRef = useRef(false);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    timerRef.current = null;
  }, []);

  const stopCurrent = useCallback(
    async (invalidateIntent: boolean): Promise<boolean> => {
      if (invalidateIntent) {
        intentRef.current += 1;
        activeRef.current = false;
      }
      generationRef.current += 1;
      clearTimer();
      abortRef.current?.abort();
      await inFlightRef.current?.catch(() => undefined);
      abortRef.current = null;
      inFlightRef.current = null;
      if (!streamName) return true;
      try {
        await cameraControlApi.stopCamera(streamName);
        return true;
      } catch (error: unknown) {
        onError(error instanceof Error ? error.message : "카메라 정지 요청에 실패했습니다.");
        return false;
      }
    },
    [clearTimer, onError, streamName],
  );

  const stop = useCallback(async () => {
    await stopCurrent(true);
  }, [stopCurrent]);

  const start = useCallback(
    (move: PTZMoveVector) => {
      if (!streamName) return;
      const hadMovement = activeRef.current || inFlightRef.current !== null || timerRef.current !== null;
      const intent = ++intentRef.current;
      activeRef.current = true;
      const ready = hadMovement ? stopCurrent(false) : Promise.resolve(true);
      void ready.then((stopped) => {
        if (intentRef.current !== intent) return;
        if (!stopped) {
          activeRef.current = false;
          return;
        }
        const generation = ++generationRef.current;
        const dispatch = () => {
          if (generationRef.current !== generation) return;
          const startedAt = performance.now();
          const abort = new AbortController();
          abortRef.current = abort;
          const request = cameraControlApi.moveCamera(streamName, move, abort.signal);
          inFlightRef.current = request;
          void request
            .then(() => {
              if (generationRef.current !== generation) return;
              const delay = Math.max(0, 1000 - (performance.now() - startedAt));
              timerRef.current = window.setTimeout(dispatch, delay);
            })
            .catch((error: unknown) => {
              if (abort.signal.aborted || generationRef.current !== generation) return;
              onError(error instanceof Error ? error.message : "카메라 이동 요청에 실패했습니다.");
              void stop();
            })
            .finally(() => {
              if (inFlightRef.current === request) inFlightRef.current = null;
              if (abortRef.current === abort) abortRef.current = null;
            });
        };
        dispatch();
      });
    },
    [onError, stop, stopCurrent, streamName],
  );

  useEffect(() => {
    const stopIfActive = () => {
      if (activeRef.current) void stop();
    };
    const stopOnVisibility = () => {
      if (document.hidden) stopIfActive();
    };
    const stopOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") stopIfActive();
    };
    window.addEventListener("blur", stopIfActive);
    document.addEventListener("visibilitychange", stopOnVisibility);
    window.addEventListener("keydown", stopOnEscape);
    return () => {
      window.removeEventListener("blur", stopIfActive);
      document.removeEventListener("visibilitychange", stopOnVisibility);
      window.removeEventListener("keydown", stopOnEscape);
      stopIfActive();
    };
  }, [stop]);

  return { start, stop };
}
