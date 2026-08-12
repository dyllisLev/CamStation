#!/usr/bin/env bash
set -euo pipefail

camstation_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
camstation_web_host=${CAMSTATION_WEB_HOST:-${HOST:-127.0.0.1}}
camstation_web_port=${CAMSTATION_WEB_PORT:-${PASEO_PORT:-5173}}

if [[ ! -d "$camstation_root/web/node_modules" ]]; then
  echo "Web dependencies are missing. Run ./scripts/setup-dev.sh first." >&2
  exit 1
fi

exec npm --prefix "$camstation_root/web" run dev -- \
  --host "$camstation_web_host" \
  --port "$camstation_web_port" \
  --strictPort
