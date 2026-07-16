export const PLAYBACK_SETUP_MS = 5_000;
export const PLAYBACK_STALL_MS = 10_000;
export const PLAYBACK_EPISODE_MS = 30_000;
export const PLAYBACK_STABLE_RESET_MS = 5 * 60_000;
export const PLAYBACK_COOLDOWN_MS = 5 * 60_000;

export type PlaybackTransport = "webrtc" | "mse";

export type PlaybackRecoveryStep =
  | { readonly transport: PlaybackTransport; readonly streamName: string; readonly attempt: number }
  | { readonly action: "resubscribe"; readonly attempt: number }
  | { readonly action: "cooldown"; readonly until: number };

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
    if (this.lastProgressAt === null || now - this.lastProgressAt > PLAYBACK_STALL_MS) this.stableSince = now;
    this.lastProgressAt = now;
    if (this.stableSince === null || now - this.stableSince < PLAYBACK_STABLE_RESET_MS) return false;
    this.episodeStartedAt = null;
    this.step = 0;
    this.stableSince = now;
    return true;
  }

  recordFailure(now: number): void {
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

  private cooldown(now: number): PlaybackRecoveryStep {
    return { action: "cooldown", until: now + PLAYBACK_COOLDOWN_MS };
  }
}
