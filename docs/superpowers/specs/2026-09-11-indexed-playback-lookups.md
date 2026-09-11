# Indexed playback lookups

## Problem and requirement

The September 10 change restricted playback aggregation to one camera but still
aggregated that camera's entire fragment history twice per timeline request.
At about 469,000 fragments a fresh external `/live` load again delayed WebSocket
approval until the five-second setup deadline. Ten million published fragments
must not make a fixed-size timeline, seek, latest-bound or page lookup scan the
archive. The current WebRTC-first policy and setup deadline remain unchanged.

## Design

- Keep exact published minimum start and maximum end milliseconds on each
  `recording_segments` row. Maintain them transactionally as completed fragments
  are inserted. An earlier fragment may end later than the last fragment;
  summaries must preserve the true extrema, including incremental publication.
- Legacy ready recordings retain their rounded file timestamps. Fragmented files
  without completed fragments are not playable. Ready, growing and recoverable
  failed published files preserve their existing playback semantics; deleted
  files are excluded, and a failed deletion can restore visibility.
- Use an SQLite R-tree over camera and file time intervals to locate overlap
  candidates, then apply the exact integer millisecond and camera predicates.
  This bounds old and middle-of-history lookups as well as recent lookups.
  R-tree coordinates round outwards, so they are only a candidate index; they
  never replace exact timestamps. See the [SQLite R-tree documentation](https://sqlite.org/rtree.html).
- Use partial B-tree indexes for the latest end, previous end, next start and
  active-recording existence. Resolve camera and stream keys with indexed
  existence/first-owner lookups, preserving historical owner ordering without
  collecting every archived file. Read paths must not aggregate fragment rows.
- Backfill existing published extrema once in a transactional, versioned
  migration. Reopening an upgraded DB must not repeat archive aggregation.
  Preserve recording bytes, statuses, identity, backup state and public DTOs.
- Keep SQLite as the source of truth and retain one serialized writer. Route
  live camera/layout/stream-registration and playback reads through a bounded
  pool of four read-only WAL connections. Configure read-only mode on every
  connection, including replacements. These reads see committed data without
  waiting for the writer connection; publication retains its transaction.
  No external cache, additional service or longer playback timeout is needed.

## Acceptance

1. Existing timeline/seek/pagination/legacy/published-prefix tests still pass.
2. Migration, append/repeat/rejected publication, non-monotonic fragment ends,
   status changes, deletion rollback, interval boundary precision and archived
   camera isolation are verified with the application SQLite driver.
3. Query plans use the interval index or bounded B-tree seeks, including old,
   middle and recent windows; no fragment table is read by these lookups.
4. As clarified by the user, generating ten million rows is not an acceptance
   requirement. Prove that fixed-window lookup work depends on the indexed
   candidates, not the total fragment history. Check old, middle and recent
   windows and the current operating DB snapshot, reporting timings separately
   from the one-time migration.
5. Concurrent timeline polling and publication must leave public stream
   registration responsive without changing its allowlist. Record measured
   contention separately from isolated query latency; do not equate index
   complexity with a latency guarantee under arbitrary write/I/O load.
   A held writer transaction must not block live reads or expose unpublished
   changes; committed camera revocation and publication must become visible.
6. Full Go tests and daemon build pass. Operating deployment and fresh-browser
   video startup are reported separately from local verification.
