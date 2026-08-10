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
[[ "${CUTOVER_APPROVED:-NO}" == "YES" ]] || production_die "CUTOVER_APPROVED must be YES for the approved window"
require_command flock

exec 9>/run/lock/camstation-cutover.lock
flock -n 9 || production_die "another cutover operation holds the lock"
"$SCRIPT_DIR/preflight.sh" --config "$2"

handoff_started=0
rollback_on_error() {
  local exit_code=$?
  trap - ERR
  if (( handoff_started == 1 )); then
    set +e
    production_note "automatic server rollback started"
    activate_nginx_include "$NGINX_MAINTENANCE_INCLUDE"
    systemctl stop "$V2_UNIT"
    systemctl disable "$V2_UNIT"
    legacy_units_enable
    legacy_units_start
    activate_nginx_include "$NGINX_LEGACY_INCLUDE"
    production_note "automatic server rollback attempted; inspect all unit and nginx states before retry"
  fi
  exit "$exit_code"
}
trap rollback_on_error ERR

production_note "switching nginx to maintenance response"
activate_nginx_include "$NGINX_MAINTENANCE_INCLUDE"
handoff_started=1

production_note "stopping exact legacy units: ${LEGACY_UNIT_ARRAY[*]}"
legacy_units_stop
legacy_units_are_inactive || production_die "one or more legacy units remained active"
legacy_units_disable
for media_port in 1984 8554 8555; do
  wait_for_port_state "$media_port" free 20 || production_die "legacy media port did not become free"
done

production_note "starting $V2_UNIT"
systemctl enable "$V2_UNIT"
systemctl start "$V2_UNIT"
wait_for_port_state 18080 listening 30 || production_die "2.x HTTP listener did not start"
curl -fsS --max-time 5 "$INTERNAL_BASE_URL/api/health" >/dev/null
curl -fsS --max-time 5 "$INTERNAL_BASE_URL/api/cameras/config" >/dev/null
curl -fsS --max-time 5 "$INTERNAL_BASE_URL/api/recorders/status" >/dev/null
verify_legacy_viewer_route || production_die "legacy Viewer compatibility route failed"

production_note "switching nginx to 2.x"
activate_nginx_include "$NGINX_V2_INCLUDE"
curl -fsS --max-time 5 "$PUBLIC_BASE_URL/api/health" >/dev/null
unit_is_enabled "$V2_UNIT" || production_die "2.x service did not acquire boot startup ownership"
legacy_units_are_disabled || production_die "one or more legacy units still own boot startup"

handoff_started=0
trap - ERR
production_note "SERVER_SWITCHED_2X restart CamViewer 1.0 normally and prove eight visible videos"
production_note "SERVER_ACCEPTANCE_PENDING prove eight growing recorders and configure/test the production backup remote"
