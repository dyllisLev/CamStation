#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 --installer <path> --version <version> --release-dir <dir> [--development-unsigned] [--dry-run]" >&2
}

die() {
  echo "publish-viewer-release: $*" >&2
  exit 1
}

installer=''
version=''
release_dir=''
development_unsigned=false
dry_run=false

while (($#)); do
  case "$1" in
    --installer|--version|--release-dir)
      (($# >= 2)) || { usage; die "$1 requires a value"; }
      case "$1" in
        --installer) installer=$2 ;;
        --version) version=$2 ;;
        --release-dir) release_dir=$2 ;;
      esac
      shift 2
      ;;
    --development-unsigned)
      development_unsigned=true
      shift
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$installer" && -n "$version" && -n "$release_dir" ]] || { usage; die "installer, version, and release directory are required"; }
[[ -f "$installer" && ! -L "$installer" ]] || die "installer must be a regular file"
[[ $(basename -- "$installer") == 'CamStationViewerSetup.exe' ]] || die "installer filename must be CamStationViewerSetup.exe"
[[ "/$installer/" != *'/../'* ]] || die "installer path must not contain parent traversal"
[[ "$version" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] || die "version contains unsupported characters"
[[ "$release_dir" != '/' && "$release_dir" != '.' && "$release_dir" != '..' && "/$release_dir/" != *'/../'* ]] || die "unsafe release directory"

size_bytes=$(stat -c %s -- "$installer")
((size_bytes > 0)) || die "installer must not be empty"
sha256=$(sha256sum -- "$installer" | awk '{print $1}')
published_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
manifest=$(printf '{"version":"%s","filename":"CamStationViewerSetup.exe","sizeBytes":%s,"sha256":"%s","publishedAt":"%s","developmentUnsigned":%s}\n' \
  "$version" "$size_bytes" "$sha256" "$published_at" "$development_unsigned")

if [[ "$dry_run" == true ]]; then
  printf '%s\n' "$manifest"
  exit 0
fi

mkdir -p -- "$release_dir"
stage=$(mktemp -d "$release_dir/.current.new.XXXXXX")
old_previous=''
current_rotated=false
published=false

cleanup() {
  local status=$?
  trap - EXIT
  if [[ "$published" != true ]]; then
    if [[ "$current_rotated" == true && ! -e "$release_dir/current" && -e "$release_dir/previous" ]]; then
      mv -- "$release_dir/previous" "$release_dir/current" || true
    fi
    if [[ -n "$old_previous" && ! -e "$release_dir/previous" && -e "$old_previous" ]]; then
      mv -- "$old_previous" "$release_dir/previous" || true
    fi
  fi
  [[ -z "${stage:-}" || ! -e "$stage" ]] || rm -rf -- "$stage"
  exit "$status"
}
trap cleanup EXIT

cp -- "$installer" "$stage/CamStationViewerSetup.exe"
chmod 0644 "$stage/CamStationViewerSetup.exe"
printf '%s\n' "$manifest" >"$stage/release.json"
chmod 0644 "$stage/release.json"

[[ $(stat -c %s -- "$stage/CamStationViewerSetup.exe") == "$size_bytes" ]] || die "staged installer size mismatch"
[[ $(sha256sum -- "$stage/CamStationViewerSetup.exe" | awk '{print $1}') == "$sha256" ]] || die "staged installer hash mismatch"

if command -v sync >/dev/null 2>&1; then
  sync "$stage/CamStationViewerSetup.exe" "$stage/release.json" "$stage"
fi

if [[ -e "$release_dir/previous" ]]; then
  old_previous=$(mktemp -d "$release_dir/.previous.old.XXXXXX")
  rmdir -- "$old_previous"
  mv -- "$release_dir/previous" "$old_previous"
fi
if [[ -e "$release_dir/current" ]]; then
  mv -- "$release_dir/current" "$release_dir/previous"
  current_rotated=true
fi
mv -- "$stage" "$release_dir/current"
stage=''
published=true

[[ -z "$old_previous" || ! -e "$old_previous" ]] || rm -rf -- "$old_previous" || true
if command -v sync >/dev/null 2>&1; then
  sync "$release_dir" || true
fi

printf 'published %s (%s bytes, %s)\n' "$version" "$size_bytes" "$sha256"
