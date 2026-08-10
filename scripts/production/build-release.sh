#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

[[ "${1:-}" == "--output" && -n "${2:-}" && "${3:-}" == "--release-id" && -n "${4:-}" && $# -eq 4 ]] \
  || die "usage: $0 --output /absolute/path/bundle --release-id RELEASE_ID"
output="$2"
release_id="$4"
[[ "$output" == /* && "$output" != "/" ]] || die "output must be a bounded absolute path"
[[ "$release_id" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] || die "release id is unsafe"
[[ ! -e "$output" ]] || die "output already exists"
[[ "$(git -C "$ROOT_DIR" branch --show-current)" == "main" ]] || die "production bundles must be built from main"
[[ -z "$(git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all)" ]] || die "production bundles require a clean worktree"

parent="$(dirname "$output")"
mkdir -p "$parent"
stage="$(mktemp -d "$parent/.camstation-release.XXXXXX")"
cleanup_stage() {
  if [[ -d "$stage" && "$stage" == "$parent"/.camstation-release.* ]]; then
    rm -r -- "$stage"
  fi
}
trap cleanup_stage EXIT

(
  cd "$ROOT_DIR/web"
  npm run build
)
[[ -z "$(git -C "$ROOT_DIR" status --porcelain=v1 --untracked-files=all)" ]] || die "web build changed tracked or untracked source output; commit and re-run verification first"

install -d -m 0755 "$stage/bin" "$stage/packaging/systemd" "$stage/packaging/nginx" "$stage/packaging/production" "$stage/docs"
(
  cd "$ROOT_DIR"
  "$ROOT_DIR/scripts/dev-go.sh" build -trimpath -o "$stage/bin/camstationd" ./cmd/camstationd
  "$ROOT_DIR/scripts/dev-go.sh" build -trimpath -o "$stage/bin/camstation-migrate" ./cmd/camstation-migrate
)
install -m 0644 "$ROOT_DIR/packaging/systemd/camstationd-2x.service" "$stage/packaging/systemd/"
install -m 0644 "$ROOT_DIR/packaging/systemd/camstationd-2x.env.example" "$stage/packaging/systemd/"
install -m 0644 "$ROOT_DIR/packaging/nginx/camstation-server.conf" "$stage/packaging/nginx/"
install -m 0644 "$ROOT_DIR/packaging/nginx/legacy-location.inc" "$stage/packaging/nginx/"
install -m 0644 "$ROOT_DIR/packaging/nginx/camstation2-location.inc" "$stage/packaging/nginx/"
install -m 0644 "$ROOT_DIR/packaging/nginx/maintenance-location.inc" "$stage/packaging/nginx/"
install -m 0644 "$ROOT_DIR/packaging/production/cutover.env.example" "$stage/packaging/production/"
install -m 0644 "$ROOT_DIR/docs/2026-08-09_cctv-1x-to-2x-production-cutover-strategy.md" "$stage/docs/production-cutover.md"
install -m 0644 "$ROOT_DIR/docs/superpowers/specs/2026-08-09-production-cutover-preparation-design.md" "$stage/docs/preparation-design.md"
cp -a "$ROOT_DIR/scripts/production" "$stage/scripts"

printf '%s\n' "$release_id" >"$stage/RELEASE_ID"
printf '%s\n' "$(git -C "$ROOT_DIR" rev-parse HEAD)" >"$stage/GIT_COMMIT"
printf '%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" >"$stage/BUILT_AT_UTC"
chmod 0755 "$stage/bin/camstationd" "$stage/bin/camstation-migrate" "$stage/scripts"/*.sh
(
  cd "$stage"
  find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum >SHA256SUMS
)
mv -- "$stage" "$output"
trap - EXIT
printf 'release bundle created: %s\n' "$output"
