# Recording playback startup, 2026-09-11

## Observation

The operator reported a recording that would not play immediately after the
indexed-playback deployment. The access log identifies Safari 26.6.1 requests
at 17:56:34 and 17:56:52 KST for the 17:30 recording. Resolve, manifest, init and
the requested fragments returned HTTP 200. The file was present and readable;
the lookup and file transfer did not reproduce the preceding database stall.
The same recording played in Chrome.

The adapter selected Hls.js whenever MSE was supported, including Safari.
In the Linux WebKit 26.6 reproduction it remained paused at time zero with
`HAVE_METADATA`, although buffered media started later. Preparation waited for
`loadeddata`/`canplay`, which could not arrive while it stayed in that initial
gap. The existing timeout then displayed the generic playback error.

## Change

- Use browser-native HLS for completed recordings when supported. Growing
  EVENT playlists keep explicit Hls.js positioning: a native player can join
  their live edge instead of the requested historical position.
- Prepare MSE playback again after a complete fragment has buffered, moving
  an unbuffered start into available media before waiting for a decoded frame.
  Preparation runs after the buffer callback and is cancelled on teardown.
- Shared-clock correction also respects the available buffered position, so
  it does not immediately seek the newly prepared video back into its gap.
- Archive bytes, recording timestamps, API responses and DB schema are unchanged.

## Verification

- Web tests: 106 passed, including actual adapter effects for native selection,
  growing-playlist selection, metadata-only startup and obsolete-event cleanup.
- Lint, embedded web build and daemon build passed.
- Chrome: the reported recording played for 20 seconds; pause held its exact
  position, a 10-second seek completed, and resume advanced normally.
  A growing recording also passed startup, pause, seek and resume through MSE.
- WebKit 26.6 with native HLS enabled: the actual recordings page displayed
  decoded video, advanced its cursor, paused at 7.744 seconds, sought to 17.744,
  and resumed past 20.244 seconds. The 1,264.458-second duration was preserved.
  The rendered screenshot was inspected.
- Linux WebKit's MSE path also exhibited a GStreamer seek deadlock during
  investigation. Instrumenting early media events with full range/state reads
  additionally disturbed native playback in this test environment. Final UI
  validation used normal controls, later state samples and a decoded-frame
  callback. No macOS Safari session was available; Linux WebKit is supporting
  evidence, not a claim of verification on the operator's Mac.

Deployment verification is recorded after rollout.
