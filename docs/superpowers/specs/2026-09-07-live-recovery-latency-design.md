# Live recovery after short and prolonged source loss

Status: source implementation; production rollout and real-camera recovery verification pending.

## Required behavior

A camera may stop sending media for seconds or minutes. Once usable media returns,
the affected tile must continue recovering automatically without a page/Viewer reload,
without disturbing other cameras, and without a minutes-long retry sleep. Live output
resolution may be reduced independently of recording quality, but that is not evidence
that the recovery state machine works.

## Observed defect

On 2026-09-06 KST, official Viewer playback for 소방서5-live entered cooldown at
07:29:38 and 12:30:10. The server live warmer and recorder continued reporting about
1,200 frames per minute. The next attempts began exactly five minutes later and played
within approximately one second. These episodes skipped the MSE/fallback steps.

After a cooldown retry, the 30-second recovery deadline stayed anchored to the retry
start until five minutes of continuous media progress. Playback for 183 or 225 seconds
therefore exhausted the old deadline even though the connection had recovered.
The next stall went directly to cooldown. This is independent of the initial stall's
cause, which is not established by the higher-resolution live output.

## Recovery contract

- Preserve a finite 30-second active recovery episode and five-second setup deadline.
- Preserve primary WebRTC retry, primary MSE, optional fallback MSE and isolated
  resubscription. There is at most one visible connection per tile.
- After every exhausted episode, wait five seconds and start a fresh episode.
  Outage age and prior failures never increase that wait to minutes.
- Space immediately failing attempts at least three seconds apart. Time already spent
  waiting for setup or a media stall counts toward that interval.
- Five seconds of continuous real playback progress resets the episode deadline and
  attempt sequence. A single buffered frame, binary receipt, or socket open does not.
- Preserve the ten-second no-progress detector. Recovery latency includes detection,
  attempt setup, actual source/keyframe arrival and browser scheduling; the five-second
  cooldown is not an unconditional five-second end-to-end recovery guarantee.
- Queued callbacks from cleared retry timers and closed connections cannot restart
  playback or mark a cooldown tile healthy. Unmount cancels pending retries.
- A working fallback remains visible while existing bounded primary probes run.
- Do not change recording outputs, restart cameras, or automatically restart the
  whole Viewer in response to an individual stream failure.

## Verification

Use the actual recovery class and playback hook with virtual timers and controlled
connections. Cover a brief loss, a prolonged outage, repeated refusal, a recovered
183/225-second session followed by another stall, stale events and cleanup. With the
test source immediately usable, recovery should occur within 15 seconds of return.
Real camera and Windows decode/network timings remain a separate rollout check.

Run web tests, lint, the production web build, Go tests and the daemon build. Review
the generated embedded assets. No production setting change is implied by these local
checks; lowering a live resolution is an independent operational change.
