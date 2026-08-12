#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

[[ "${1:-}" == "--config" && -n "${2:-}" && "${3:-}" == "--bundle" && -n "${4:-}" && "${5:-}" == "--execute" && $# -eq 5 ]] \
  || production_die "usage: $0 --config /absolute/path/cutover.env --bundle /absolute/path/bundle --execute"
require_root
load_cutover_config "$2"
validate_cutover_config
bundle="$4"
[[ "$bundle" == /* && "$bundle" != "/" && -d "$bundle" && ! -L "$bundle" ]] || production_die "bundle must be a bounded absolute, non-symlink directory"
require_command sha256sum
require_command install
require_command getent

unit_is_active "$V2_UNIT" && production_die "2.x service must be inactive while staging a release"
legacy_units_are_active || production_die "legacy units must remain active while staging a release"
getent passwd "$SERVICE_USER" >/dev/null || production_die "service user does not exist"
getent group "$SERVICE_GROUP" >/dev/null || production_die "service group does not exist"
[[ "$(<"$bundle/RELEASE_ID")" == "$RELEASE_ID" ]] || production_die "bundle release id does not match cutover config"
(
  cd "$bundle"
  sha256sum -c SHA256SUMS
)
[[ ! -e "$RELEASE_REAL_DIR" ]] || production_die "immutable release destination already exists"

install -d -o root -g root -m 0755 "$RELEASES_ROOT"
install -d -o root -g root -m 0755 "$RELEASE_REAL_DIR"
cp -a "$bundle/." "$RELEASE_REAL_DIR/"
chown -R root:root "$RELEASE_REAL_DIR"

install_if_absent_or_identical() {
  local source="$1" destination="$2" mode="$3"
  install -d -o root -g root -m 0755 "$(dirname "$destination")"
  if [[ -e "$destination" ]]; then
    cmp -s -- "$source" "$destination" || production_die "refusing to overwrite a different installed file: $destination"
    return 0
  fi
  install -o root -g root -m "$mode" "$source" "$destination"
}

install_if_absent_or_identical "$RELEASE_REAL_DIR/packaging/systemd/camstationd-2x.service" "$V2_UNIT_FILE" 0644
install_if_absent_or_identical "$RELEASE_REAL_DIR/packaging/nginx/legacy-location.inc" "$NGINX_LEGACY_INCLUDE" 0644
install_if_absent_or_identical "$RELEASE_REAL_DIR/packaging/nginx/camstation2-location.inc" "$NGINX_V2_INCLUDE" 0644
install_if_absent_or_identical "$RELEASE_REAL_DIR/packaging/nginx/maintenance-location.inc" "$NGINX_MAINTENANCE_INCLUDE" 0644
if [[ ! -e "$DAEMON_ENV_FILE" ]]; then
  install -d -o root -g root -m 0755 "$(dirname "$DAEMON_ENV_FILE")"
  install -o root -g root -m 0600 "$RELEASE_REAL_DIR/packaging/systemd/camstationd-2x.env.example" "$DAEMON_ENV_FILE"
fi

temporary_link="$RELEASES_ROOT/.current.$$.tmp"
ln -s -- "$RELEASE_REAL_DIR" "$temporary_link"
mv -Tf -- "$temporary_link" "$RELEASE_DIR"
systemctl daemon-reload

[[ "$(realpath -e -- "$RELEASE_DIR")" == "$(realpath -e -- "$RELEASE_REAL_DIR")" ]] || production_die "current release link verification failed"
sha256_matches "$CAMSTATIOND_BIN" "$CAMSTATIOND_SHA256" || production_die "installed camstationd SHA-256 mismatch"
sha256_matches "$MIGRATOR_BIN" "$MIGRATOR_SHA256" || production_die "installed migrator SHA-256 mismatch"
production_note "RELEASE_STAGED immutable 2.x release installed; no service or nginx active include was switched"
