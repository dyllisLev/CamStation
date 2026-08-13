export const PLAYBACK_SETUP_MS = 5_000;
export const PLAYBACK_STALL_MS = 10_000;
export const PLAYBACK_EPISODE_MS = 30_000;
export const PLAYBACK_STABLE_RESET_MS = 5 * 60_000;
export const PLAYBACK_COOLDOWN_MS = 5 * 60_000;
export const PLAYBACK_PRIMARY_PROBE_INTERVAL_MS = 60_000;

export type PlaybackProbeClock = {
  readonly now: () => number;
  readonly set: (callback: () => void, delayMs: number) => number;
  readonly clear: (timerId: number) => void;
};

export class PlaybackProbeScheduler {
  private timerId: number | null = null;
  private readonly clock: PlaybackProbeClock;

  constructor(clock: PlaybackProbeClock = browserPlaybackProbeClock()) {
    this.clock = clock;
  }

  arm(until: number, probe: () => void): void {
    this.clear();
    this.timerId = this.clock.set(() => {
      this.timerId = null;
      probe();
    }, Math.max(0, until - this.clock.now()));
  }

  clear(): void {
    if (this.timerId === null) return;
    this.clock.clear(this.timerId);
    this.timerId = null;
  }
}

export type PlaybackTransport = "webrtc" | "mse";

export type PlaybackPrimaryProbe = (
  transport: PlaybackTransport,
  signal: AbortSignal,
) => Promise<boolean>;

export type PlaybackPrimaryPromotionCallbacks = {
  readonly onProbeStarted: (transport: PlaybackTransport) => void;
  readonly onProbeFailed: (transport: PlaybackTransport) => void;
  readonly onRecovered: (transport: PlaybackTransport) => void;
};

export class PlaybackPrimaryPromoter {
  private readonly probe: PlaybackPrimaryProbe;
  private readonly clock: PlaybackProbeClock;
  private readonly scheduler: PlaybackProbeScheduler;
  private running = false;
  private generation = 0;
  private controller: AbortController | null = null;
  private preferredTransport: PlaybackTransport = "webrtc";
  private callbacks: PlaybackPrimaryPromotionCallbacks | null = null;

  constructor(
    probe: PlaybackPrimaryProbe,
    clock: PlaybackProbeClock = browserPlaybackProbeClock(),
  ) {
    this.probe = probe;
    this.clock = clock;
    this.scheduler = new PlaybackProbeScheduler(clock);
  }

  get active(): boolean {
    return this.running;
  }

  start(
    preferredTransport: PlaybackTransport,
    callbacks: PlaybackPrimaryPromotionCallbacks,
  ): void {
    if (this.running) return;
    this.running = true;
    this.preferredTransport = preferredTransport;
    this.callbacks = callbacks;
    const token = ++this.generation;
    this.schedule(token);
  }

  stop(): void {
    this.running = false;
    this.generation++;
    this.scheduler.clear();
    this.controller?.abort();
    this.controller = null;
    this.callbacks = null;
  }

  private schedule(token: number): void {
    if (!this.running || token !== this.generation) return;
    this.scheduler.arm(this.clock.now() + PLAYBACK_PRIMARY_PROBE_INTERVAL_MS, () => {
      void this.runCycle(token);
    });
  }

  private async runCycle(token: number): Promise<void> {
    const transports = primaryProbeTransports(this.preferredTransport);
    for (const transport of transports) {
      if (!this.running || token !== this.generation || !this.callbacks) return;
      this.callbacks.onProbeStarted(transport);
      const controller = new AbortController();
      this.controller = controller;
      let recovered = false;
      try {
        recovered = await this.probe(transport, controller.signal);
      } catch {
        recovered = false;
      }
      if (!this.running || token !== this.generation || controller.signal.aborted || !this.callbacks) return;
      this.controller = null;
      if (recovered) {
        const callbacks = this.callbacks;
        this.running = false;
        this.callbacks = null;
        callbacks.onRecovered(transport);
        return;
      }
      this.callbacks.onProbeFailed(transport);
    }
    this.schedule(token);
  }
}

export function primaryProbeTransports(preferred: PlaybackTransport): readonly PlaybackTransport[] {
  return preferred === "webrtc" ? ["webrtc", "mse"] : ["mse", "webrtc"];
}

export type PlaybackRecoveryStep =
  | { readonly transport: PlaybackTransport; readonly streamName: string; readonly attempt: number }
  | { readonly action: "resubscribe"; readonly attempt: number }
  | { readonly action: "cooldown"; readonly until: number };

type PlaybackTransportStep = Extract<PlaybackRecoveryStep, { readonly transport: PlaybackTransport }>;

export function recoveryAttemptPresentation(
  step: PlaybackTransportStep,
  primaryStreamName: string,
  previousTransport: PlaybackTransport,
) {
  const usingFallback = step.streamName !== primaryStreamName;
  return {
    phase: usingFallback ? "fallback" as const : "retrying" as const,
    usingFallback,
    transportChanged: step.transport !== previousTransport,
  };
}

export class PlaybackRecovery {
  readonly streamNames: readonly string[];
  private episodeStartedAt: number | null = null;
  private step = 0;
  private stableSince: number | null = null;
  private lastProgressAt: number | null = null;
  private stallStartedAt: number | null = null;

  constructor(streamNames: readonly string[]) {
    this.streamNames = streamNames.filter((name, index) => Boolean(name) && streamNames.indexOf(name) === index);
  }

  nextFailure(now: number): PlaybackRecoveryStep {
    if (this.episodeStartedAt === null) this.episodeStartedAt = this.stallStartedAt ?? now;
    if (this.remainingMs(now) === 0) return this.cooldown(now);

    const primary = this.streamNames[0];
    if (!primary) return this.cooldown(now);
    const steps: Omit<PlaybackRecoveryStep, "attempt">[] = [
      { transport: "webrtc", streamName: primary },
      { transport: "mse", streamName: primary },
    ];
    if (this.streamNames[1]) steps.push({ transport: "mse", streamName: this.streamNames[1] });
    steps.push({ action: "resubscribe" });

    const next = steps[this.step++];
    if (!next) return this.cooldown(now);
    return { ...next, attempt: this.step + 1 } as PlaybackRecoveryStep;
  }

  recordProgress(now: number): boolean {
    this.stallStartedAt = null;
    if (this.stableSince === null || this.lastProgressAt === null || now - this.lastProgressAt > PLAYBACK_STALL_MS) {
      this.stableSince = now;
    }
    this.lastProgressAt = now;
    if (this.stableSince === null || now - this.stableSince < PLAYBACK_STABLE_RESET_MS) return false;
    this.episodeStartedAt = null;
    this.step = 0;
    this.stableSince = now;
    return true;
  }

  recordFailure(now: number): void {
    this.stableSince = null;
    if (this.stallStartedAt === null) {
      this.stallStartedAt = now;
      if (this.episodeStartedAt === null) this.episodeStartedAt = now;
    }
  }

  stalledForMs(now: number): number {
    return this.stallStartedAt === null ? 0 : Math.max(0, now - this.stallStartedAt);
  }

  remainingMs(now: number): number {
    if (this.episodeStartedAt === null) return PLAYBACK_EPISODE_MS;
    return Math.max(0, this.episodeStartedAt + PLAYBACK_EPISODE_MS - now);
  }

  boundedDelayMs(now: number, maximumMs: number): number {
    return Math.max(0, Math.min(maximumMs, this.remainingMs(now)));
  }

  restartEpisode(now: number): void {
    this.episodeStartedAt = now;
    this.step = 0;
    this.stableSince = null;
  }

  resetForPrimaryPromotion(): void {
    this.episodeStartedAt = null;
    this.step = 0;
    this.stableSince = null;
    this.lastProgressAt = null;
    this.stallStartedAt = null;
  }

  private cooldown(now: number): PlaybackRecoveryStep {
    return { action: "cooldown", until: now + PLAYBACK_COOLDOWN_MS };
  }
}

function browserPlaybackProbeClock(): PlaybackProbeClock {
  return {
    now: () => Date.now(),
    set: (callback, delayMs) => window.setTimeout(callback, delayMs),
    clear: (timerId) => window.clearTimeout(timerId),
  };
}
