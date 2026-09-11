# Indexed playback implementation and verification

Spec: [indexed playback lookups](../specs/2026-09-11-indexed-playback-lookups.md).

1. Add a one-time migration for exact file extrema, transactional maintenance,
   the overlap index, and start/end/active B-tree indexes.
2. Replace fragment aggregation in all four playback span queries with indexed
   file lookups while preserving complete spans and half-open intervals.
   Isolate live/playback reads from the single writer with read-only WAL
   connections and verify committed visibility during an open write.
3. Verify upgraded and fresh DBs, publication/state transitions, overlap
   precision, keyset pagination and index plans.
4. Verify indexed query plans and equivalence/performance on an isolated copy
   of the operating database, including simultaneous polling/registration.
   The user clarified that a synthetic ten-million-row run is unnecessary.
5. Run full Go checks/build, review against the spec and update implementation
   status. Verify live runtime separately when deployed.

## Status

Implementation complete; operating deployment is pending. The operating service
remains revision `49c84f0`.
The diagnosis reproduced 7.4–8.2 seconds to eight videos on fresh browsers;
five external first attempts hit the setup deadline in one sample. Server media
reception and all eight recorder workers remained running.

An initial exploratory fixture run measured fixed eight-camera polls at roughly
7 ms for 500k fragments and 8–10 ms for 10m, including old and middle windows.
Its aggressive eight continuous pollers plus publication workload exceeded the
experiment's initial 100 ms contention threshold (registration p95 132 ms,
publication p95 122 ms). That run was not a passing acceptance test and does not
establish a write/I/O latency guarantee. Synthetic scale generation is not part
of the revised acceptance contract; indexed-plan proof and the operating
snapshot checks below are the final verification targets.

The first operating-snapshot check (472,775 fragments, 11,677 files) passed 88
equivalence checks and preserved canonical recording/media/fragment hashes.
Indexed single-camera window plus bounds queries measured p95 0.56 ms, but the
shared-connection concurrency sample still had a 9.82 s registration outlier
and a 4.80 s publication outlier. This is why the final design also isolates
latency-sensitive reads from the writer; indexing alone did not satisfy startup
responsiveness under concurrent writes.

The final operating-snapshot copy passed the same 88 equivalence checks and
canonical hashes, with R-tree integrity `ok`. Its one-time migration took
716 ms. The 720 window-plus-bounds samples measured p95 0.783 ms (max 53.174 ms);
the 30 eight-camera old/middle/recent sweeps measured p95 16.3 ms. Under eight
concurrent pollers plus publication, 200 registration checks measured p95
2.562 ms (max 9.411 ms), and 160 publications measured p95 13.683 ms (max
44.512 ms). These are local snapshot measurements, not production end-to-end
startup timings; the final sample ran alongside Go tests.

Deterministic tests also hold a writer transaction open until camera, layout,
registration, timeline and published-media reads finish. Readers see the prior
committed state, then committed camera revocation and the extended media prefix
after commit. All four reader connections reject writes after pool replacement
and database reopen, including filenames with URI punctuation. Query-plan tests
cover interval, boundary and archived camera/stream resolution indexes.

The full Go suite and daemon build pass. No frontend source changed. Production
schema migration and fresh-browser first-frame/8-camera startup remain pending
deployment; the local SQL timings do not replace those checks.
