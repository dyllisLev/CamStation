#!/usr/bin/env bash
set -euo pipefail

camstation_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
camstation_go="$camstation_root/.tools/go1.25.12/bin/go"

if [[ -x "$camstation_go" ]]; then
  exec "$camstation_go" "$@"
fi

if command -v go >/dev/null 2>&1; then
  exec go "$@"
fi

echo "Go is not available. Run ./scripts/setup-dev.sh first." >&2
exit 1
