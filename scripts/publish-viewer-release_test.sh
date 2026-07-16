#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
publisher="$root_dir/scripts/publish-viewer-release.sh"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

expect_failure() {
  if "$@" >/dev/null 2>&1; then
    fail "command unexpectedly succeeded: $*"
  fi
}

assert_file_contains() {
  local file=$1
  local text=$2
  grep -Fq "$text" "$file" || fail "$file does not contain: $text"
}

release_dir="$tmp_dir/releases"
installer_dir="$tmp_dir/input"
installer="$installer_dir/CamStationViewerSetup.exe"
mkdir -p "$installer_dir"
printf 'viewer-installer-v1\n' >"$installer"

expect_failure "$publisher"
expect_failure "$publisher" --installer "$installer" --version '../2.0.0' --release-dir "$release_dir"
printf 'wrong name\n' >"$installer_dir/../escape.exe"
expect_failure "$publisher" --installer "$installer_dir/../escape.exe" --version '2.0.0-dev.1' --release-dir "$release_dir"

"$publisher" --installer "$installer" --version '2.0.0-dev.1' --release-dir "$release_dir" --development-unsigned --dry-run >/dev/null
[[ ! -e "$release_dir" ]] || fail "dry-run created the release directory"

"$publisher" --installer "$installer" --version '2.0.0-dev.1' --release-dir "$release_dir" --development-unsigned >/dev/null

current_installer="$release_dir/current/CamStationViewerSetup.exe"
current_manifest="$release_dir/current/release.json"
[[ -f "$current_installer" ]] || fail "current installer is missing"
[[ -f "$current_manifest" ]] || fail "current manifest is missing"
cmp -s "$installer" "$current_installer" || fail "published installer differs from input"

expected_size=$(stat -c %s "$installer")
expected_hash=$(sha256sum "$installer" | awk '{print $1}')
assert_file_contains "$current_manifest" '"version":"2.0.0-dev.1"'
assert_file_contains "$current_manifest" '"filename":"CamStationViewerSetup.exe"'
assert_file_contains "$current_manifest" "\"sizeBytes\":$expected_size"
assert_file_contains "$current_manifest" "\"sha256\":\"$expected_hash\""
assert_file_contains "$current_manifest" '"developmentUnsigned":true'
grep -Eq '"publishedAt":"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z"' "$current_manifest" || fail "publishedAt is not UTC RFC3339"

cp "$current_manifest" "$tmp_dir/current-before-failure.json"
cp "$current_installer" "$tmp_dir/current-before-failure.exe"
expect_failure "$publisher" --installer "$tmp_dir/missing/CamStationViewerSetup.exe" --version '2.0.0-dev.2' --release-dir "$release_dir"
cmp -s "$tmp_dir/current-before-failure.json" "$current_manifest" || fail "failed publish changed current manifest"
cmp -s "$tmp_dir/current-before-failure.exe" "$current_installer" || fail "failed publish changed current installer"

printf 'viewer-installer-v2\n' >"$installer"
"$publisher" --installer "$installer" --version '2.0.0-dev.2' --release-dir "$release_dir" >/dev/null

cmp -s "$tmp_dir/current-before-failure.exe" "$release_dir/previous/CamStationViewerSetup.exe" || fail "previous installer was not retained"
assert_file_contains "$release_dir/previous/release.json" '"version":"2.0.0-dev.1"'
assert_file_contains "$release_dir/current/release.json" '"version":"2.0.0-dev.2"'
assert_file_contains "$release_dir/current/release.json" '"developmentUnsigned":false'

find "$release_dir" -type f -printf '%P\n' | sort >"$tmp_dir/files-before-dry-run"
sha256sum "$release_dir/current/release.json" "$release_dir/current/CamStationViewerSetup.exe" "$release_dir/previous/release.json" "$release_dir/previous/CamStationViewerSetup.exe" >"$tmp_dir/hashes-before-dry-run"
"$publisher" --installer "$installer" --version '2.0.0-dev.3' --release-dir "$release_dir" --development-unsigned --dry-run >/dev/null
find "$release_dir" -type f -printf '%P\n' | sort >"$tmp_dir/files-after-dry-run"
sha256sum "$release_dir/current/release.json" "$release_dir/current/CamStationViewerSetup.exe" "$release_dir/previous/release.json" "$release_dir/previous/CamStationViewerSetup.exe" >"$tmp_dir/hashes-after-dry-run"
cmp -s "$tmp_dir/files-before-dry-run" "$tmp_dir/files-after-dry-run" || fail "dry-run changed release files"
cmp -s "$tmp_dir/hashes-before-dry-run" "$tmp_dir/hashes-after-dry-run" || fail "dry-run changed release contents"
compgen -G "$release_dir/.current.new.*" >/dev/null && fail "staging directory was left behind"

echo "PASS: viewer release publisher"
