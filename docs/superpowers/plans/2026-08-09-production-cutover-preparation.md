# CamStation 1.x production cutover preparation plan

**Goal:** Produce a tested CamStation 2.0 release candidate that can import the current 1.x
camera/layout/settings snapshot and support the approved server-first, display-only transitional
Viewer sequence without touching production during preparation.

**Architecture:** Add the narrow legacy Viewer entry route, preserve bounded Unicode camera
keys, build an offline snapshot importer with redacted manifests, package the daemon for a
single-active systemd/nginx cutover, and verify everything on synthetic data before any approved
production rehearsal.

**Tech stack:** Go 1.25, modernc SQLite, Go `net/http`, React/Vite embedded assets, systemd,
nginx, go2rtc, ffmpeg, rclone, Bash.

## Task 1: Add the transitional Viewer compatibility route

**Files:**

- Modify `cmd/camstationd/routes.go`
- Create `cmd/camstationd/routes_legacy_viewer.go`
- Create `cmd/camstationd/routes_legacy_viewer_test.go`

- [x] Add a non-cacheable exact `GET /new?viewer=1` redirect to `/live?viewer=1`.
- [x] Redirect broader `/new` queries to `/`, reject non-GET methods, and leave subpaths to SPA fallback.
- [x] Run focused route, root, and core API tests.

## Task 2: Preserve safe Hangul camera keys

**Files:**

- Create `internal/streamkey/streamkey.go`
- Create `internal/streamkey/streamkey_test.go`
- Modify `cmd/camstationd/camera_profile_helpers.go`
- Modify `internal/store/camera_policies.go`
- Modify `internal/store/cameras.go`

- [x] Write tests accepting the nine observed key shapes and rejecting path, URL, whitespace,
      control-character, encoding, boundary-hyphen, invalid UTF-8, and oversized inputs.
- [x] Replace ASCII round-trip validation with the shared stable-key validator.
- [x] Enforce the same invariant at the store boundary so offline import cannot bypass it.
- [x] Run focused store and daemon tests.

## Task 3: Build the offline 1.x snapshot importer

**Files:**

- Create `internal/legacyimport/` package and tests
- Create `cmd/camstation-migrate/main.go` and tests
- Create synthetic legacy SQLite fixtures only under test temporary directories

- [x] Implement online snapshot, read-only schema inspection, and SQLite quick-check.
- [x] Implement deterministic conversion and a secret-free manifest for cameras, layouts,
      recording settings, optional-field presence, and blockers.
- [x] Implement `snapshot`, `inspect`, `dry-run`, `import`, and `verify`; forbid symlink,
      active, overwrite, and different-nonempty targets.
- [x] Prove `9/8/1`, disabled-state preservation, sub-stream selection, three outputs, layout
      key integrity, 48-column conversion, settings mapping, idempotent verification, and
      failure without promoted partial state.
- [x] Prove stdout/stderr and manifests never contain synthetic raw credentials.

## Task 4: Add production packaging and offline preflight

**Files:**

- Create `packaging/systemd/` unit and environment schema
- Create `packaging/nginx/` inactive upstream includes
- Create bounded `scripts/production/` install, preflight, switch, and rollback helpers
- Create shell policy tests

- [x] Inventory target filesystem/mount/service names read-only and record only non-secret facts.
- [x] Validate required absolute paths, ownership, free space, binaries, ports, release hashes,
      DB quick-check, importer manifest, and inactive service state.
- [x] Keep install separate from switch; make switch single-active and rollback exact.
- [x] Reject development defaults, active 1.x/2.0 collision, broad process matching, unresolved
      variables, enabled backup without a target, or unknown nginx include state. Keep the
      disabled/empty backup as a visible post-start acceptance gate.

## Task 5: Rehearse and publish the execution packet

**Files:**

- Update `docs/2026-08-09_cctv-1x-to-2x-production-cutover-strategy.md`
- Create a dated readiness report under `docs/`
- Update `docs/07-implementation-status.md` only for code actually shipped and verified
- Update `tasks/todo.md` and `tasks/lessons.md`

- [x] Run Go, Web, Viewer, shell, build, link, redaction, and diff validation.
- [x] Run importer twice against a synthetic snapshot and prove idempotent verification.
- [x] Run snapshot/dry-run/import/verify against the final production source during the
      approved preparation window.
- [ ] Rehearse exact server and client-only rollback paths on `cctv2` if it becomes available;
      otherwise preserve the production No-Go until an approved maintenance window has local
      rollback personnel and all offline gates are green.
- [x] Publish the Go/No-Go table, KST-oriented helpers, and the exact next authorized action.
      Record the isolated clean replacement commit, release hashes, and root-only source bundle.

## Review

- Source and inactive production preparation are implemented. The exact Viewer bridge, bounded Hangul keys, online
  snapshot, secret-free importer, production-safe backup default, systemd/nginx packaging,
  and guarded staging/preflight/switch/rollback helpers all have automated coverage.
- Synthetic import proves nine cameras, eight enabled, `소방서2` disabled, nine sub streams,
  one layout/eight items, three outputs per camera, 30/30/700 settings, pending policy state,
  atomic no-overwrite promotion, repeat-safe verification, and no raw synthetic credentials.
- `go test ./...`, `go vet ./...`, 52 Web tests, Web lint/build, 23 Viewer tests, Viewer build,
  daemon build, migrator build, Bash syntax/policy, and whitespace checks passed.
- Production staging installed and hash-verified release `2.0.0-rc.20260809.5`, created an
  immutable online source snapshot, verified the inactive target DB, placed recording/temp on
  the dedicated media filesystem, and prepared nginx with the active link still on legacy.
- Real source inspection exposed three loopback go2rtc ffmpeg recipes in the legacy sub field.
  The importer now maps those exact derived forms to recording-backed H.264 live outputs; both
  regression tests and the actual `9/8/1` manifest pass with zero blockers.
- Full server preflight passes. Production remains No-Go until the replacement commit is integrated
  into remote `main` and real 2.0 camera/recording/backup/GUI/rollback gates are proven.
- Switch and rollback now transfer systemd boot enablement together with runtime ownership, so a
  reboot cannot silently reactivate both generations after a successful handoff.
