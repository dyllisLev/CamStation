#!/usr/bin/env bash
set -euo pipefail

camstation_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)

cd "$camstation_root"
"$camstation_root/scripts/dev-go.sh" test ./...
npm --prefix "$camstation_root/web" test
npm --prefix "$camstation_root/viewer-app" test
