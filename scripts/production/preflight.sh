#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

[[ "${1:-}" == "--config" && -n "${2:-}" && $# -eq 2 ]] || production_die "usage: $0 --config /absolute/path/cutover.env"
require_root
load_cutover_config "$2"
validate_cutover_config

for command_name in systemctl nginx curl sha256sum stat readlink realpath df awk grep ss go2rtc ffmpeg ffprobe rclone; do
  require_command "$command_name"
done

[[ -x "$CAMSTATIOND_BIN" && -x "$MIGRATOR_BIN" ]] || production_die "release binaries are missing or not executable"
[[ -L "$RELEASE_DIR" && "$(realpath -e -- "$RELEASE_DIR")" == "$(realpath -e -- "$RELEASE_REAL_DIR")" ]] \
  || production_die "current release link does not resolve to the approved immutable release"
sha256_matches "$CAMSTATIOND_BIN" "$CAMSTATIOND_SHA256" || production_die "camstationd SHA-256 mismatch"
sha256_matches "$MIGRATOR_BIN" "$MIGRATOR_SHA256" || production_die "migrator SHA-256 mismatch"
[[ -s "$SOURCE_SNAPSHOT" && -s "$TARGET_DB" ]] || production_die "source snapshot and imported target DB must both be nonempty"
[[ "$(stat -c '%a' -- "$TARGET_DB")" == "600" ]] || production_die "target DB mode must be 0600"
[[ "$(stat -c '%U:%G' -- "$TARGET_DB")" == "$SERVICE_USER:$SERVICE_GROUP" ]] || production_die "target DB owner must match the 2.x service account"

[[ -f "$DAEMON_ENV_FILE" && ! -L "$DAEMON_ENV_FILE" && "$(stat -c '%U:%a' -- "$DAEMON_ENV_FILE")" == "root:600" ]] \
  || production_die "daemon environment file must be a root-owned 0600 regular file"
grep -Fqx "CAMSTATION_ADDR=127.0.0.1:18080" "$DAEMON_ENV_FILE" || production_die "daemon address is not the production loopback value"
grep -Fqx "CAMSTATION_DB=$TARGET_DB" "$DAEMON_ENV_FILE" || production_die "daemon DB path does not match the imported target"
grep -Fqx "CAMSTATION_RECORDINGS_DIR=$RECORDINGS_DIR" "$DAEMON_ENV_FILE" || production_die "daemon recordings path mismatch"
grep -Fqx "CAMSTATION_TEMP_DIR=$TEMP_DIR" "$DAEMON_ENV_FILE" || production_die "daemon temp path mismatch"
grep -Fqx "CAMSTATION_VIEWER_RELEASES_DIR=$VIEWER_RELEASES_DIR" "$DAEMON_ENV_FILE" || production_die "daemon Viewer releases path mismatch"
grep -Fqx "CAMSTATION_RECORDING_ENABLED=true" "$DAEMON_ENV_FILE" || production_die "production recording is not enabled"
grep -Fqx "CAMSTATION_SEGMENT_MINUTES=30" "$DAEMON_ENV_FILE" || production_die "production segment duration is not 30 minutes"
grep -Fqx "CAMSTATION_MAX_STORAGE_GB=700" "$DAEMON_ENV_FILE" || production_die "production storage limit is not 700 GB"

[[ "$(realpath -e -- "$(systemctl show "$V2_UNIT" -p FragmentPath --value)")" == "$(realpath -e -- "$V2_UNIT_FILE")" ]] \
  || production_die "installed 2.x unit file mismatch"
[[ "$(systemctl show "$V2_UNIT" -p User --value)" == "$SERVICE_USER" ]] || production_die "2.x unit user mismatch"
[[ "$(systemctl show "$V2_UNIT" -p Group --value)" == "$SERVICE_GROUP" ]] || production_die "2.x unit group mismatch"
[[ "$(systemctl show "$V2_UNIT" -p WorkingDirectory --value)" == "$WORKING_DIR" ]] || production_die "2.x unit working directory mismatch"
[[ "$(systemctl show "$V2_UNIT" -p ExecStart --value)" == *"$CAMSTATIOND_BIN"* ]] || production_die "2.x unit binary mismatch"

for directory in "$WORKING_DIR" "$STATE_DIR" "$VIEWER_RELEASES_DIR" "$MEDIA_ROOT" "$RECORDINGS_DIR" "$TEMP_DIR"; do
  [[ -d "$directory" && ! -L "$directory" ]] || production_die "runtime directory is missing or is a symlink"
  [[ "$(stat -c '%U:%G' -- "$directory")" == "$SERVICE_USER:$SERVICE_GROUP" ]] || production_die "runtime directory owner mismatch"
done
[[ "$(stat -c '%d' -- "$RECORDINGS_DIR")" == "$(stat -c '%d' -- "$TEMP_DIR")" ]] \
  || production_die "recording and temp directories must share a filesystem for atomic finalization"

state_free="$(df -B1 --output=avail "$STATE_DIR" | awk 'NR==2 {print $1}')"
recording_free="$(df -B1 --output=avail "$RECORDINGS_DIR" | awk 'NR==2 {print $1}')"
(( state_free >= MIN_STATE_FREE_BYTES )) || production_die "state filesystem free space is below threshold"
(( recording_free >= MIN_RECORDING_FREE_BYTES )) || production_die "recording filesystem free space is below threshold"

unit_is_active "$V2_UNIT" && production_die "2.x service must be inactive during offline preflight"
unit_is_enabled "$V2_UNIT" && production_die "2.x service must remain disabled before the handoff"
legacy_units_are_active || production_die "all configured legacy units must be active before handoff"
legacy_units_are_enabled || production_die "all configured legacy units must own boot startup before handoff"
unit_is_active "$NGINX_UNIT" || production_die "nginx must be active"
[[ "$(active_include_target)" == "$(realpath -e -- "$NGINX_LEGACY_INCLUDE")" ]] || production_die "nginx active include is not the preserved legacy include"
nginx -t

port_is_listening 18080 && production_die "2.x loopback port 18080 is already occupied"
for legacy_port in 1984 8554 8555; do
  port_is_listening "$legacy_port" || production_die "expected legacy media listener is absent"
done

active_manifest="$(run_migrator_with_expectations inspect "$LEGACY_DB")"
snapshot_manifest="$(run_migrator_with_expectations inspect "$SOURCE_SNAPSHOT")"
active_fingerprint="$(manifest_fingerprint <<<"$active_manifest")"
snapshot_fingerprint="$(manifest_fingerprint <<<"$snapshot_manifest")"
[[ -n "$active_fingerprint" && "$active_fingerprint" == "$snapshot_fingerprint" ]] \
  || production_die "active 1.x camera/layout/settings state drifted after the immutable snapshot"

manifest="$(run_migrator_with_expectations verify "$SOURCE_SNAPSHOT" "$TARGET_DB")"
grep -Eq '"ready"[[:space:]]*:[[:space:]]*true' <<<"$manifest" || production_die "migration manifest is not ready"
grep -Eq '"targetStatus"[[:space:]]*:[[:space:]]*"verified"' <<<"$manifest" || production_die "migration target is not verified"

production_note "PREFLIGHT_READY active/snapshot parity, target DB, services, nginx, ports, release, and storage passed"
production_note "BACKUP_GATE_PENDING imported backup remains disabled with protectUnbacked=true until the production remote is configured and proven"
