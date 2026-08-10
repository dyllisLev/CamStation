#!/usr/bin/env bash
set -euo pipefail

camstation_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
camstation_daemon_host=${CAMSTATION_DEV_HOST:-${HOST:-127.0.0.1}}
camstation_daemon_port=${CAMSTATION_DEV_PORT:-${PASEO_PORT:-18080}}

mkdir -p \
  "$camstation_root/data/recordings" \
  "$camstation_root/data/temp" \
  "$camstation_root/data/viewer-releases" \
  "$camstation_root/data/runtime-logs"

export CAMSTATION_ADDR=${CAMSTATION_ADDR:-"$camstation_daemon_host:$camstation_daemon_port"}
export CAMSTATION_DB=${CAMSTATION_DB:-"$camstation_root/data/camstation.db"}
export CAMSTATION_RECORDINGS_DIR=${CAMSTATION_RECORDINGS_DIR:-"$camstation_root/data/recordings"}
export CAMSTATION_TEMP_DIR=${CAMSTATION_TEMP_DIR:-"$camstation_root/data/temp"}
export CAMSTATION_VIEWER_RELEASES_DIR=${CAMSTATION_VIEWER_RELEASES_DIR:-"$camstation_root/data/viewer-releases"}
export CAMSTATION_RECORDING_ENABLED=${CAMSTATION_RECORDING_ENABLED:-false}
export CAMSTATION_MAX_STORAGE_GB=${CAMSTATION_MAX_STORAGE_GB:-0}

cd "$camstation_root"
exec ./scripts/dev-go.sh run ./cmd/camstationd
