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
for command_name in flock install sha256sum realpath readlink nginx systemctl curl cmp mv basename; do
  require_command "$command_name"
done

exec 9>/run/lock/camstation-cutover.lock
flock -n 9 || production_die "another cutover operation holds the lock"
unit_is_active "$V2_UNIT" && production_die "2.x service must remain inactive during nginx preparation"
legacy_units_are_active || production_die "legacy units must remain active during nginx preparation"
unit_is_active "$NGINX_UNIT" || production_die "nginx must be active during preparation"

candidate="$RELEASE_REAL_DIR/packaging/nginx/camstation-server.conf"
[[ -f "$candidate" && ! -L "$candidate" ]] || production_die "packaged nginx server candidate is missing"
[[ -f "$NGINX_LEGACY_INCLUDE" && ! -L "$NGINX_LEGACY_INCLUDE" ]] || production_die "legacy nginx include is missing"

if [[ -f "$NGINX_SITE_FILE" && ! -L "$NGINX_SITE_FILE" \
  && -L "$NGINX_SITE_LINK" \
  && "$(realpath -e -- "$NGINX_SITE_LINK")" == "$(realpath -e -- "$NGINX_SITE_FILE")" \
  && ! -e "$NGINX_DUPLICATE_SITE" \
  && -L "$NGINX_ACTIVE_INCLUDE" \
  && "$(active_include_target)" == "$(realpath -e -- "$NGINX_LEGACY_INCLUDE")" ]] \
  && cmp -s -- "$candidate" "$NGINX_SITE_FILE"; then
  nginx -t
  curl -fsS --max-time 5 "$PUBLIC_BASE_URL/api/system/health" >/dev/null
  production_note "NGINX_PREPARED existing active include already preserves the 1.x routes"
  exit 0
fi

[[ -f "$NGINX_SITE_FILE" && ! -L "$NGINX_SITE_FILE" ]] || production_die "active nginx site source must be a regular file"
[[ -L "$NGINX_SITE_LINK" ]] || production_die "enabled nginx site must be a symlink"
[[ "$(realpath -e -- "$NGINX_SITE_LINK")" == "$(realpath -e -- "$NGINX_SITE_FILE")" ]] \
  || production_die "enabled nginx site does not resolve to the approved source file"
sha256_matches "$NGINX_SITE_FILE" "$LEGACY_NGINX_SITE_SHA256" \
  || production_die "active legacy nginx site hash does not match the reviewed source"
[[ -f "$NGINX_DUPLICATE_SITE" && ! -L "$NGINX_DUPLICATE_SITE" ]] || production_die "reviewed duplicate nginx site is missing"
sha256_matches "$NGINX_DUPLICATE_SITE" "$LEGACY_NGINX_SITE_SHA256" \
  || production_die "duplicate nginx site differs from the reviewed legacy source"

install -d -o root -g root -m 0700 "$NGINX_BACKUP_DIR"
site_backup="$NGINX_BACKUP_DIR/camstation.$RELEASE_ID.original"
duplicate_backup="$NGINX_BACKUP_DIR/$(basename "$NGINX_DUPLICATE_SITE").disabled"
[[ ! -e "$site_backup" && ! -e "$duplicate_backup" ]] || production_die "nginx preparation backup destination already exists"
install -o root -g root -m 0600 "$NGINX_SITE_FILE" "$site_backup"
mv -- "$NGINX_DUPLICATE_SITE" "$duplicate_backup"

atomic_include_link "$NGINX_LEGACY_INCLUDE"
site_temporary="$(dirname "$NGINX_SITE_FILE")/.camstation-server.$$.tmp"
install -o root -g root -m 0644 "$candidate" "$site_temporary"
mv -Tf -- "$site_temporary" "$NGINX_SITE_FILE"

restore_legacy_site() {
  set +e
  install -o root -g root -m 0644 "$site_backup" "$NGINX_SITE_FILE"
  if [[ -f "$duplicate_backup" && ! -e "$NGINX_DUPLICATE_SITE" ]]; then
    mv -- "$duplicate_backup" "$NGINX_DUPLICATE_SITE"
  fi
  nginx -t && systemctl reload "$NGINX_UNIT"
}

if ! nginx -t; then
  restore_legacy_site
  production_die "nginx preparation candidate failed validation; original site restored"
fi
if ! systemctl reload "$NGINX_UNIT"; then
  restore_legacy_site
  production_die "nginx preparation reload failed; original site restored"
fi

if [[ "$(active_include_target)" != "$(realpath -e -- "$NGINX_LEGACY_INCLUDE")" ]]; then
  restore_legacy_site
  production_die "nginx active include does not resolve to legacy after reload; original site restored"
fi
if ! curl -fsS --max-time 5 "$PUBLIC_BASE_URL/api/system/health" >/dev/null; then
  restore_legacy_site
  production_die "legacy health failed after nginx preparation; original site restored"
fi
production_note "NGINX_PREPARED active symlink preserves 1.x; maintenance and 2.x targets remain inactive"
