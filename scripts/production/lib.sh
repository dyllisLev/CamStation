#!/usr/bin/env bash

production_die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

production_note() {
  printf '%s %s\n' "$(TZ=Asia/Seoul date '+%Y-%m-%d %H:%M:%S KST')" "$*"
}

require_root() {
  [[ "$(id -u)" == "0" ]] || production_die "this operation requires root"
}

load_cutover_config() {
  local config_path="${1:-}"
  [[ -n "$config_path" ]] || production_die "--config path is required"
  [[ "$config_path" == /* && "$config_path" != "/" ]] || production_die "config path must be an absolute file path"
  [[ -f "$config_path" && ! -L "$config_path" ]] || production_die "config must be a regular, non-symlink file"

  local owner mode mode_value
  owner="$(stat -c '%u' -- "$config_path")"
  mode="$(stat -c '%a' -- "$config_path")"
  [[ "$owner" == "0" ]] || production_die "config must be owned by root"
  mode_value=$((8#$mode))
  (( (mode_value & 077) == 0 )) || production_die "config must not grant group or other permissions"

  set -a
  # shellcheck disable=SC1090 -- the path is root-owned, non-symlink, and mode checked above.
  source "$config_path"
  set +a
}

require_value() {
  local name="$1"
  local value="${!name:-}"
  [[ -n "$value" && "$value" != *CHANGE_ME* ]] || production_die "$name is missing or unresolved"
  [[ "$value" != *$'\n'* && "$value" != *$'\r'* ]] || production_die "$name contains a newline"
}

require_absolute_path() {
  local name="$1"
  require_value "$name"
  local value="${!name}"
  [[ "$value" == /* && "$value" != "/" && "$value" != */../* && "$value" != */.. && "$value" != *'/./'* ]] \
    || production_die "$name must be a bounded absolute path"
}

require_unit_name() {
  local unit="$1"
  [[ "$unit" =~ ^[A-Za-z0-9_.@-]+\.service$ ]] || production_die "invalid systemd unit name"
}

load_legacy_units() {
  require_value LEGACY_UNITS
  read -r -a LEGACY_UNIT_ARRAY <<<"$LEGACY_UNITS"
  (( ${#LEGACY_UNIT_ARRAY[@]} > 0 )) || production_die "LEGACY_UNITS is empty"
  local unit
  for unit in "${LEGACY_UNIT_ARRAY[@]}"; do
    require_unit_name "$unit"
  done
}

validate_cutover_config() {
  local variable
  for variable in SERVICE_USER SERVICE_GROUP CAMSTATIOND_SHA256 MIGRATOR_SHA256 INTERNAL_BASE_URL PUBLIC_BASE_URL \
    RELEASE_ID EXPECTED_CAMERAS EXPECTED_ENABLED EXPECTED_SUBSTREAMS EXPECTED_DISABLED_CAMERA EXPECTED_LAYOUTS EXPECTED_LAYOUT_ITEMS \
    EXPECTED_SEGMENT_MINUTES EXPECTED_RETENTION_DAYS EXPECTED_MAX_STORAGE_GB LEGACY_NGINX_SITE_SHA256; do
    require_value "$variable"
  done
  for variable in WORKING_DIR MEDIA_ROOT DAEMON_ENV_FILE V2_UNIT_FILE RELEASES_ROOT RELEASE_REAL_DIR RELEASE_DIR CAMSTATIOND_BIN MIGRATOR_BIN LEGACY_DB SOURCE_SNAPSHOT TARGET_DB \
    STATE_DIR RECORDINGS_DIR TEMP_DIR VIEWER_RELEASES_DIR NGINX_ACTIVE_INCLUDE NGINX_LEGACY_INCLUDE NGINX_V2_INCLUDE NGINX_MAINTENANCE_INCLUDE; do
    require_absolute_path "$variable"
  done
  for variable in NGINX_SITE_FILE NGINX_SITE_LINK NGINX_DUPLICATE_SITE NGINX_BACKUP_DIR; do
    require_absolute_path "$variable"
  done
  require_value V2_UNIT
  require_value NGINX_UNIT
  require_unit_name "$V2_UNIT"
  require_unit_name "$NGINX_UNIT"
  load_legacy_units

  [[ "$INTERNAL_BASE_URL" == "http://127.0.0.1:18080" ]] || production_die "INTERNAL_BASE_URL must remain loopback port 18080"
  [[ "$PUBLIC_BASE_URL" =~ ^https?://([A-Za-z0-9.-]+|\[[0-9A-Fa-f:]+\])(:[0-9]+)?$ ]] || production_die "PUBLIC_BASE_URL must be an origin without credentials or path"
  [[ "$CAMSTATIOND_SHA256" =~ ^[0-9a-f]{64}$ ]] || production_die "CAMSTATIOND_SHA256 must be lowercase SHA-256"
  [[ "$MIGRATOR_SHA256" =~ ^[0-9a-f]{64}$ ]] || production_die "MIGRATOR_SHA256 must be lowercase SHA-256"
  [[ "$LEGACY_NGINX_SITE_SHA256" =~ ^[0-9a-f]{64}$ ]] || production_die "LEGACY_NGINX_SITE_SHA256 must be lowercase SHA-256"
  [[ "$RELEASE_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] || production_die "RELEASE_ID is unsafe"
  [[ "$EXPECTED_CAMERAS" =~ ^[1-9][0-9]*$ && "$EXPECTED_ENABLED" =~ ^[1-9][0-9]*$ && "$EXPECTED_SUBSTREAMS" =~ ^[1-9][0-9]*$ \
    && "$EXPECTED_LAYOUTS" =~ ^[1-9][0-9]*$ && "$EXPECTED_LAYOUT_ITEMS" =~ ^[1-9][0-9]*$ \
    && "$EXPECTED_SEGMENT_MINUTES" =~ ^[1-9][0-9]*$ && "$EXPECTED_RETENTION_DAYS" =~ ^[1-9][0-9]*$ \
    && "$EXPECTED_MAX_STORAGE_GB" =~ ^[1-9][0-9]*([.][0-9]+)?$ ]] \
    || production_die "expected counts must be positive integers"
  [[ "$MIN_STATE_FREE_BYTES" =~ ^[1-9][0-9]*$ && "$MIN_RECORDING_FREE_BYTES" =~ ^[1-9][0-9]*$ ]] \
    || production_die "free-space thresholds must be positive integers"
  [[ "$SOURCE_SNAPSHOT" != "$TARGET_DB" ]] || production_die "source and target databases must differ"
  [[ "$LEGACY_DB" != "$SOURCE_SNAPSHOT" && "$LEGACY_DB" != "$TARGET_DB" ]] || production_die "active, snapshot, and target databases must differ"
  [[ "$STATE_DIR" == "$WORKING_DIR/data" ]] || production_die "STATE_DIR must be WORKING_DIR/data for generated go2rtc state"
  [[ "$TARGET_DB" == "$STATE_DIR"/* ]] || production_die "TARGET_DB must be inside STATE_DIR"
  [[ "$RELEASE_REAL_DIR" == "$RELEASES_ROOT/$RELEASE_ID" ]] || production_die "RELEASE_REAL_DIR must match RELEASES_ROOT/RELEASE_ID"
  [[ "$CAMSTATIOND_BIN" == "$RELEASE_DIR"/* && "$MIGRATOR_BIN" == "$RELEASE_DIR"/* ]] || production_die "release binaries must be inside the current release link"
  [[ "$SERVICE_USER" == "camstation" && "$SERVICE_GROUP" == "camstation" && "$WORKING_DIR" == "/var/lib/camstation2" \
    && "$MEDIA_ROOT" == "/mnt/hdd/camstation2" ]] \
    || production_die "packaged systemd hardening requires the camstation account and approved state/media roots"
  [[ "$STATE_DIR" == "$WORKING_DIR"/* && "$VIEWER_RELEASES_DIR" == "$WORKING_DIR"/* ]] \
    || production_die "state and Viewer release paths must remain inside WORKING_DIR"
  [[ "$RECORDINGS_DIR" == "$MEDIA_ROOT"/* && "$TEMP_DIR" == "$MEDIA_ROOT"/* ]] \
    || production_die "recording and temp paths must remain inside MEDIA_ROOT"
  [[ "$NGINX_ACTIVE_INCLUDE" != "$NGINX_LEGACY_INCLUDE" && "$NGINX_ACTIVE_INCLUDE" != "$NGINX_V2_INCLUDE" && "$NGINX_ACTIVE_INCLUDE" != "$NGINX_MAINTENANCE_INCLUDE" ]] \
    || production_die "active nginx include must be a separate symlink"
  [[ "$NGINX_SITE_FILE" != "$NGINX_SITE_LINK" && "$NGINX_DUPLICATE_SITE" != "$NGINX_SITE_LINK" \
    && "$NGINX_DUPLICATE_SITE" != "$NGINX_SITE_FILE" ]] \
    || production_die "nginx site, link, and duplicate paths must be distinct"
}

run_migrator_with_expectations() {
  local operation="$1" source="$2" target="${3:-}"
  local arguments=("$operation" -source "$source"
    -expect-cameras "$EXPECTED_CAMERAS"
    -expect-enabled "$EXPECTED_ENABLED"
    -expect-substreams "$EXPECTED_SUBSTREAMS"
    -expect-disabled "$EXPECTED_DISABLED_CAMERA"
    -expect-layouts "$EXPECTED_LAYOUTS"
    -expect-layout-items "$EXPECTED_LAYOUT_ITEMS"
    -expect-segment-minutes "$EXPECTED_SEGMENT_MINUTES"
    -expect-retention-days "$EXPECTED_RETENTION_DAYS"
    -expect-max-storage-gb "$EXPECTED_MAX_STORAGE_GB")
  if [[ -n "$target" ]]; then
    arguments+=(-target "$target")
  fi
  "$MIGRATOR_BIN" "${arguments[@]}"
}

manifest_fingerprint() {
  sed -nE 's/^[[:space:]]*"canonicalFingerprint"[[:space:]]*:[[:space:]]*"([0-9a-f]{64})",?[[:space:]]*$/\1/p' \
    | head -n 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || production_die "required command is missing: $1"
}

sha256_matches() {
  local path="$1" expected="$2"
  [[ "$(sha256sum -- "$path" | awk '{print $1}')" == "$expected" ]]
}

unit_is_active() {
  systemctl is-active --quiet "$1"
}

unit_is_enabled() {
  systemctl is-enabled --quiet "$1"
}

port_is_listening() {
  local port="$1"
  ss -ltnH | awk -v wanted=":$port" '$4 ~ wanted "$" {found=1} END {exit !found}'
}

wait_for_port_state() {
  local port="$1" wanted="$2" attempts="${3:-20}"
  local index
  for ((index=0; index<attempts; index++)); do
    if [[ "$wanted" == "free" ]] && ! port_is_listening "$port"; then
      return 0
    fi
    if [[ "$wanted" == "listening" ]] && port_is_listening "$port"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

active_include_target() {
  [[ -L "$NGINX_ACTIVE_INCLUDE" ]] || return 1
  readlink -f -- "$NGINX_ACTIVE_INCLUDE"
}

atomic_include_link() {
  local target="$1"
  local directory temporary
  directory="$(dirname "$NGINX_ACTIVE_INCLUDE")"
  temporary="$directory/.active-backend.$$.tmp"
  [[ -f "$target" && ! -L "$target" ]] || production_die "nginx include target must be a regular, non-symlink file"
  ln -s -- "$target" "$temporary"
  mv -Tf -- "$temporary" "$NGINX_ACTIVE_INCLUDE"
}

activate_nginx_include() {
  local target="$1"
  local previous
  previous="$(active_include_target)" || production_die "active nginx include is not a resolvable symlink"
  atomic_include_link "$target"
  if ! nginx -t; then
    atomic_include_link "$previous"
    nginx -t || production_die "nginx restore validation failed"
    production_die "nginx candidate validation failed; previous include restored"
  fi
  if ! systemctl reload "$NGINX_UNIT"; then
    atomic_include_link "$previous"
    nginx -t || production_die "nginx restore validation failed after reload error"
    systemctl reload "$NGINX_UNIT" || production_die "nginx reload and restore both failed"
    production_die "nginx reload failed; previous include restored"
  fi
}

legacy_units_start() {
  systemctl start "${LEGACY_UNIT_ARRAY[@]}"
}

legacy_units_stop() {
  systemctl stop "${LEGACY_UNIT_ARRAY[@]}"
}

legacy_units_are_active() {
  local unit
  for unit in "${LEGACY_UNIT_ARRAY[@]}"; do
    unit_is_active "$unit" || return 1
  done
}

legacy_units_are_inactive() {
  local unit
  for unit in "${LEGACY_UNIT_ARRAY[@]}"; do
    unit_is_active "$unit" && return 1
  done
}

legacy_units_enable() {
  systemctl enable "${LEGACY_UNIT_ARRAY[@]}"
}

legacy_units_disable() {
  systemctl disable "${LEGACY_UNIT_ARRAY[@]}"
}

legacy_units_are_enabled() {
  local unit
  for unit in "${LEGACY_UNIT_ARRAY[@]}"; do
    unit_is_enabled "$unit" || return 1
  done
}

legacy_units_are_disabled() {
  local unit
  for unit in "${LEGACY_UNIT_ARRAY[@]}"; do
    unit_is_enabled "$unit" && return 1
  done
}

verify_legacy_viewer_route() {
  local headers
  headers="$(curl -fsS -o /dev/null -D - --max-time 5 "$INTERNAL_BASE_URL/new?viewer=1")" || return 1
  grep -Eq '^HTTP/[^ ]+ 302([[:space:]]|$)' <<<"$headers" \
    && grep -Eiq '^Location:[[:space:]]*/live\?viewer=1[[:space:]]*$' <<<"$headers" \
    && grep -Eiq '^Cache-Control:[[:space:]]*no-store[[:space:]]*$' <<<"$headers"
}
