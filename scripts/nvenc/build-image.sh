#!/usr/bin/env bash
set -euo pipefail
repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
image=${1:-local/camstation:nvenc-dev}
revision=$(git -C "$repo" rev-parse HEAD)
docker build --network "${BUILD_NETWORK:-default}" --file "$repo/Dockerfile.nvenc" \
  --build-arg "BUILD_JOBS=${BUILD_JOBS:-4}" \
  --build-arg "VERSION=${VERSION:-nvenc-dev}" \
  --build-arg "VCS_REF=$revision" \
  --build-arg "BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --tag "$image" "$repo"
