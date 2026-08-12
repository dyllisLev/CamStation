# Settings-page MSI download and clean-host implementation plan

1. Extend `internal/viewerrelease` to accept only the exact current MSI and legacy EXE names. Add
   tests for accepted MSI, rejected arbitrary installer names, and legacy compatibility.
2. Make `scripts/publish-viewer-release.sh` preserve the verified installer basename throughout
   immutable staging, validation, legacy migration, pointer switching, and dry-run metadata. Extend
   its contract test with an MSI publication while retaining the EXE path tests.
3. Serve filename-specific content types from `routes_viewer_releases.go` and test MSI metadata,
   headers, and bytes. Prevent `desiredViewerRelease` from producing legacy Agent update commands for
   MSI releases and add a focused route test.
4. Run publisher tests, targeted Go tests, web tests/lint/build, Viewer tests, daemon build, policy
   tests, and `git diff --check`.
5. Copy the already verified MSI from WIN11-DELL to a bounded ignored local staging directory and
   compare size/hash with the Windows source and its sidecar/metadata.
6. Build a new immutable canary image, load it on `cctv`, publish the MSI atomically into the existing
   persistent state mount, and recreate only `camstation2-canary` after backing up its image pointer.
7. Verify API metadata/download headers and hash, inspect `/settings` with a real browser, click the
   download action, and compare the browser-downloaded MSI. Recreate the canary once and repeat the
   download/hash and continuity checks.
8. Uninstall the exact registered Viewer MSI on WIN11-DELL with a verbose bounded log. Remove only
   confirmed Viewer-owned residue and audit the complete clean-state predicate plus preserved
   SSH/Explorer/development environment.
9. Update implementation status, canary operations, task review, and lessons with the exact URL,
   version, hash, KST timestamps, rollback boundary, and unsigned-development limitation.
