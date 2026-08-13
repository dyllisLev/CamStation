#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd -P)"

for script in "$SCRIPT_DIR"/*.sh; do
  bash -n "$script"
done

python3 -B "$SCRIPT_DIR/test_camstation_log_watch.py"

if grep -ERn '(^|[[:space:]])(pkill|killall|kill[[:space:]]+-9|rm[[:space:]]+-rf)([[:space:]]|$)' "$SCRIPT_DIR"; then
  printf 'production scripts contain a forbidden broad/destructive process command\n' >&2
  exit 1
fi
if grep -ERn 'gdrive:/cctvTest|CAMSTATION_MAX_STORAGE_GB=0\.30' \
  "$ROOT_DIR/packaging" \
  "$SCRIPT_DIR/lib.sh" "$SCRIPT_DIR/preflight.sh" "$SCRIPT_DIR/prepare-state.sh" "$SCRIPT_DIR/stage-release.sh" \
  "$SCRIPT_DIR/switch-to-2x.sh" "$SCRIPT_DIR/rollback-to-1x.sh"; then
  printf 'production artifacts contain a development default\n' >&2
  exit 1
fi
if grep -ERn '0\.0\.0\.0:18080' \
  "$ROOT_DIR/packaging/nginx" "$ROOT_DIR/packaging/production" "$ROOT_DIR/packaging/systemd" \
  "$SCRIPT_DIR/lib.sh" "$SCRIPT_DIR/preflight.sh" "$SCRIPT_DIR/prepare-state.sh" "$SCRIPT_DIR/stage-release.sh" \
  "$SCRIPT_DIR/switch-to-2x.sh" "$SCRIPT_DIR/rollback-to-1x.sh"; then
  printf 'host production artifacts expose the daemon development bind\n' >&2
  exit 1
fi

grep -Fq 'legacy_units_stop' "$SCRIPT_DIR/switch-to-2x.sh"
grep -Fq 'legacy_units_disable' "$SCRIPT_DIR/switch-to-2x.sh"
grep -Fq 'systemctl enable "$V2_UNIT"' "$SCRIPT_DIR/switch-to-2x.sh"
grep -Fq 'systemctl disable "$V2_UNIT"' "$SCRIPT_DIR/rollback-to-1x.sh"
grep -Fq 'legacy_units_enable' "$SCRIPT_DIR/rollback-to-1x.sh"
grep -Fq 'run_migrator_with_expectations snapshot' "$SCRIPT_DIR/prepare-state.sh"
grep -Fq 'active_fingerprint' "$SCRIPT_DIR/preflight.sh"
grep -Fq 'no service or nginx active include was switched' "$SCRIPT_DIR/stage-release.sh"
grep -Fq 'NGINX_PREPARED' "$SCRIPT_DIR/prepare-nginx.sh"
grep -Fq 'sha256_matches "$NGINX_SITE_FILE" "$LEGACY_NGINX_SITE_SHA256"' "$SCRIPT_DIR/prepare-nginx.sh"
grep -Fq 'wait_for_port_state "$media_port" free' "$SCRIPT_DIR/switch-to-2x.sh"
grep -Fq 'verify_legacy_viewer_route' "$SCRIPT_DIR/switch-to-2x.sh"
grep -Fq 'activate_nginx_include "$NGINX_LEGACY_INCLUDE"' "$SCRIPT_DIR/rollback-to-1x.sh"
grep -Fq 'KillMode=control-group' "$ROOT_DIR/packaging/systemd/camstationd-2x.service"
grep -Fq 'ReadWritePaths=/var/lib/camstation2 /mnt/hdd/camstation2' "$ROOT_DIR/packaging/systemd/camstationd-2x.service"
grep -Fq 'CAMSTATION_ADDR=127.0.0.1:18080' "$ROOT_DIR/packaging/systemd/camstationd-2x.env.example"
grep -Fq 'CAMSTATION_RECORDINGS_DIR=/mnt/hdd/camstation2/recordings' "$ROOT_DIR/packaging/systemd/camstationd-2x.env.example"
grep -Fq 'include /etc/nginx/camstation/active-backend.inc;' "$ROOT_DIR/packaging/nginx/camstation-server.conf"
grep -Fq 'CAMSTATION_ADDR: 0.0.0.0:18080' "$ROOT_DIR/packaging/docker/compose.canary.yaml"
test "$(grep -Fc 'target: 18080' "$ROOT_DIR/packaging/docker/compose.canary.yaml")" -eq 2
test "$(grep -Fc 'published: "${CANARY_HTTP_PORT:-18081}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml")" -eq 2
test "$(grep -Fc 'host_ip: "${CANARY_BIND_IP:?CANARY_BIND_IP is required}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml")" -eq 1
test "$(grep -Fc 'host_ip: "${CANARY_MONITOR_BIND_IP:?CANARY_MONITOR_BIND_IP is required}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml")" -eq 3
grep -Fq 'CAMSTATION_WEBRTC_CANDIDATES: "${CANARY_MONITOR_BIND_IP:?CANARY_MONITOR_BIND_IP is required}:${CANARY_WEBRTC_PORT:-18555}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml"
grep -Fq 'CAMSTATION_LOG_LEVEL: "${CAMSTATION_LOG_LEVEL:-info}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml"
grep -Fq 'CAMSTATION_LOG_LEVELS: "${CAMSTATION_LOG_LEVELS:-}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml"
grep -Fq 'CAMSTATION_LOG_DIR: /var/lib/camstation/data/logs' "$ROOT_DIR/packaging/docker/compose.canary.yaml"
grep -Fq 'CAMSTATION_LOG_MAX_MB: "${CAMSTATION_LOG_MAX_MB:-25}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml"
grep -Fq 'CAMSTATION_LOG_FILES: "${CAMSTATION_LOG_FILES:-8}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml"
grep -Fq 'CAMSTATION_LOG_LEVELS=playback=debug,stream.go2rtc=debug,stream.live_warm=debug,recorder.ffmpeg=debug' "$ROOT_DIR/packaging/docker/canary.env.example"
grep -Fq 'CAMSTATION_LOG_DIR=/var/lib/camstation2/data/logs' "$ROOT_DIR/packaging/systemd/camstationd-2x.env.example"
test -x "$SCRIPT_DIR/camstation_log_watch.py"
grep -Fq 'EnvironmentFile=/etc/camstation/camstation-log-watch.env' "$ROOT_DIR/packaging/systemd/camstation-log-watch.service"
grep -Fq 'ExecStart=/usr/local/libexec/camstation-log-watch' "$ROOT_DIR/packaging/systemd/camstation-log-watch.service"
grep -Fq 'OnUnitActiveSec=1min' "$ROOT_DIR/packaging/systemd/camstation-log-watch.timer"
grep -Fq 'Persistent=true' "$ROOT_DIR/packaging/systemd/camstation-log-watch.timer"
grep -Fq 'CAMSTATION_WATCH_OUTPUT_MAX_MB=10' "$ROOT_DIR/packaging/systemd/camstation-log-watch.env.example"
grep -Fq 'CAMSTATION_WATCH_OUTPUT_FILES=4' "$ROOT_DIR/packaging/systemd/camstation-log-watch.env.example"
test "$(grep -Fc 'target: 8555' "$ROOT_DIR/packaging/docker/compose.canary.yaml")" -eq 2
test "$(grep -Fc 'published: "${CANARY_WEBRTC_PORT:-18555}"' "$ROOT_DIR/packaging/docker/compose.canary.yaml")" -eq 2
grep -A5 -F 'name: webrtc-tcp' "$ROOT_DIR/packaging/docker/compose.canary.yaml" | grep -Fq 'protocol: tcp'
grep -A5 -F 'name: webrtc-udp' "$ROOT_DIR/packaging/docker/compose.canary.yaml" | grep -Fq 'protocol: udp'
grep -A5 -F 'name: http-management' "$ROOT_DIR/packaging/docker/compose.canary.yaml" | grep -Fq 'host_ip: "${CANARY_BIND_IP:?CANARY_BIND_IP is required}"'
grep -A5 -F 'name: http-monitor' "$ROOT_DIR/packaging/docker/compose.canary.yaml" | grep -Fq 'host_ip: "${CANARY_MONITOR_BIND_IP:?CANARY_MONITOR_BIND_IP is required}"'
grep -Fq 'CANARY_MONITOR_BIND_IP=192.168.0.160' "$ROOT_DIR/packaging/docker/canary.env.example"

printf 'production policy checks passed\n'
