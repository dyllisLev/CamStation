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
  private episodeStartedAt: number;
  private step = 0;
  private stableSince: number | null = null;
  private lastProgressAt: number | null = null;
  private progressedSinceFailure = false;

  constructor(streamNames: readonly string[], startedAt: number) {
    this.streamNames = streamNames.filter((name, index) => Boolean(name) && streamNames.indexOf(name) === index);
    this.episodeStartedAt = startedAt;
  }

  nextFailure(now: number): PlaybackRecoveryStep {
    if (this.progressedSinceFailure) {
      this.episodeStartedAt = now;
      this.progressedSinceFailure = false;
    }
    if (now - this.episodeStartedAt >= PLAYBACK_EPISODE_MS) return this.cooldown(now);

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
    if (this.lastProgressAt === null || now - this.lastProgressAt > PLAYBACK_STALL_MS) this.stableSince = now;
    this.lastProgressAt = now;
    this.progressedSinceFailure = true;
    if (this.stableSince === null || now - this.stableSince < PLAYBACK_STABLE_RESET_MS) return false;
    this.episodeStartedAt = now;
    this.step = 0;
    this.stableSince = now;
    return true;
  }

  private cooldown(now: number): PlaybackRecoveryStep {
    return { action: "cooldown", until: now + PLAYBACK_COOLDOWN_MS };
  }
}
