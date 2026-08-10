#!/usr/bin/env bash
set -euo pipefail

camstation_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)

cd "$camstation_root"
"$camstation_root/scripts/test-dev.sh"
npm --prefix "$camstation_root/web" run lint
npm --prefix "$camstation_root/web" run build
npm --prefix "$camstation_root/viewer-app" run build
"$camstation_root/scripts/dev-go.sh" build -o "$camstation_root/camstationd" ./cmd/camstationd
