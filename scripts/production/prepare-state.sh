#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

[[ "${1:-}" == "--config" && -n "${2:-}" && "${3:-}" == "--execute" && $# -eq 3 ]] \
  || production_die "usage: $0 --config /absolute/path/cutover.env --execute"
require_root
load_cutover_config "$2"
validate_cutover_config
require_command flock
require_command install

exec 9>/run/lock/camstation-cutover.lock
flock -n 9 || production_die "another cutover operation holds the lock"

[[ -x "$MIGRATOR_BIN" ]] || production_die "migrator is missing or not executable"
sha256_matches "$MIGRATOR_BIN" "$MIGRATOR_SHA256" || production_die "migrator SHA-256 mismatch"
[[ -f "$LEGACY_DB" && ! -L "$LEGACY_DB" ]] || production_die "active legacy DB must be a regular, non-symlink file"
unit_is_active "$V2_UNIT" && production_die "2.x service must be inactive during snapshot/import"
legacy_units_are_active || production_die "legacy units must remain active during online snapshot preparation"

install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0750 \
  "$WORKING_DIR" "$STATE_DIR" "$VIEWER_RELEASES_DIR" \
  "$MEDIA_ROOT" "$RECORDINGS_DIR" "$TEMP_DIR"
install -d -o root -g root -m 0700 "$(dirname "$SOURCE_SNAPSHOT")"

if [[ -e "$SOURCE_SNAPSHOT" ]]; then
  [[ -f "$SOURCE_SNAPSHOT" && ! -L "$SOURCE_SNAPSHOT" && "$(stat -c '%U:%a' -- "$SOURCE_SNAPSHOT")" == "root:600" ]] \
    || production_die "existing source snapshot is not a root-owned immutable 0600 file"
  snapshot_manifest="$(run_migrator_with_expectations inspect "$SOURCE_SNAPSHOT")"
  grep -Eq '"ready"[[:space:]]*:[[:space:]]*true' <<<"$snapshot_manifest" || production_die "existing source snapshot failed inspection"
  production_note "reusing the existing immutable 1.x snapshot"
else
  snapshot_manifest="$(run_migrator_with_expectations snapshot "$LEGACY_DB" "$SOURCE_SNAPSHOT")"
  grep -Eq '"targetStatus"[[:space:]]*:[[:space:]]*"created"' <<<"$snapshot_manifest" || production_die "online source snapshot was not created"
  chown root:root "$SOURCE_SNAPSHOT"
  chmod 0600 "$SOURCE_SNAPSHOT"
  production_note "created and checked an immutable online 1.x SQLite snapshot"
fi

import_manifest="$(run_migrator_with_expectations import "$SOURCE_SNAPSHOT" "$TARGET_DB")"
grep -Eq '"ready"[[:space:]]*:[[:space:]]*true' <<<"$import_manifest" || production_die "2.x target import is not ready"
grep -Eq '"targetStatus"[[:space:]]*:[[:space:]]*"(created|already-current)"' <<<"$import_manifest" \
  || production_die "2.x target import did not reach an accepted state"
chown "$SERVICE_USER:$SERVICE_GROUP" "$TARGET_DB"
chmod 0600 "$TARGET_DB"

verify_manifest="$(run_migrator_with_expectations verify "$SOURCE_SNAPSHOT" "$TARGET_DB")"
grep -Eq '"targetStatus"[[:space:]]*:[[:space:]]*"verified"' <<<"$verify_manifest" || production_die "post-ownership migration verification failed"
production_note "STATE_READY immutable source snapshot and inactive 2.x DB passed canonical verification"
