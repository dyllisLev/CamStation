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

production_note "switching nginx to maintenance response"
activate_nginx_include "$NGINX_MAINTENANCE_INCLUDE"
production_note "stopping exact 2.x unit"
systemctl stop "$V2_UNIT"
systemctl disable "$V2_UNIT"
wait_for_port_state 18080 free 20 || production_die "2.x HTTP port did not become free"
for media_port in 1984 8554 8555; do
  wait_for_port_state "$media_port" free 20 || production_die "2.x media port did not become free"
done

production_note "starting preserved legacy units"
legacy_units_enable
legacy_units_start
legacy_units_are_active || production_die "one or more legacy units failed to start"
legacy_units_are_enabled || production_die "one or more legacy units failed to reacquire boot startup ownership"
for media_port in 1984 8554 8555; do
  wait_for_port_state "$media_port" listening 30 || production_die "legacy media listener did not return"
done

activate_nginx_include "$NGINX_LEGACY_INCLUDE"
curl -fsS --max-time 5 "$PUBLIC_BASE_URL/api/system/health" >/dev/null
production_note "SERVER_ROLLED_BACK_1X relaunch CamViewer 1.0 and verify its original /new view"
