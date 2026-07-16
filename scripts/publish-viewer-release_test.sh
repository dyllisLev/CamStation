#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
publisher="$root_dir/scripts/publish-viewer-release.sh"
real_sync=$(command -v sync)
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

pointer_dir() {
  local release_dir=$1
  local slot=$2
  local target
  target=$(readlink "$release_dir/$slot/active") || return 1
  (cd "$release_dir/$slot" && cd "$target" && pwd -P)
}

pointer_version() {
  local directory
  directory=$(pointer_dir "$1" "$2") || return 1
  sed -n 's/.*"version":"\([^"]*\)".*/\1/p' "$directory/release.json"
}

write_installer() {
  local directory=$1
  local contents=$2
  mkdir -p "$directory"
  printf '%s\n' "$contents" >"$directory/CamStationViewerSetup.exe"
}

release_dir="$tmp_dir/releases-root"
installer_dir="$tmp_dir/input-v1"
installer="$installer_dir/CamStationViewerSetup.exe"
write_installer "$installer_dir" 'viewer-installer-v1'

expect_failure "$publisher"
expect_failure "$publisher" --installer "$installer" --version '../2.0.0' --release-dir "$release_dir"
write_installer "$tmp_dir/escape" 'wrong name'
mv "$tmp_dir/escape/CamStationViewerSetup.exe" "$tmp_dir/escape.exe"
expect_failure "$publisher" --installer "$tmp_dir/escape.exe" --version '2.0.0-dev.1' --release-dir "$release_dir"
(
  cd "$tmp_dir"
  expect_failure "$publisher" --installer "$installer" --version '2.0.0-dev.1' --release-dir '-unsafe'
  [[ ! -e '-unsafe' ]] || fail "leading-dash release directory was created"
)

"$publisher" --installer "$installer" --version '2.0.0-dev.1' --release-dir "$release_dir" --development-unsigned --dry-run >/dev/null
[[ ! -e "$release_dir" ]] || fail "dry-run created the release directory"

legacy_dir="$release_dir/current"
mkdir -p "$legacy_dir"
printf 'legacy-installer\n' >"$legacy_dir/CamStationViewerSetup.exe"
legacy_size=$(stat -c %s "$legacy_dir/CamStationViewerSetup.exe")
legacy_hash=$(sha256sum "$legacy_dir/CamStationViewerSetup.exe" | awk '{print $1}')
printf '{"version":"1.9.0","filename":"CamStationViewerSetup.exe","sizeBytes":%s,"sha256":"%s","publishedAt":"2026-07-15T00:00:00Z","developmentUnsigned":true}\n' \
  "$legacy_size" "$legacy_hash" >"$legacy_dir/release.json"

"$publisher" --installer "$installer" --version '2.0.0-dev.1' --release-dir "$release_dir" --development-unsigned >/dev/null

[[ -L "$release_dir/current/active" ]] || fail "current active pointer is missing"
[[ -L "$release_dir/previous/active" ]] || fail "previous active pointer is missing after legacy migration"
[[ -f "$legacy_dir/release.json" && -f "$legacy_dir/CamStationViewerSetup.exe" ]] || fail "legacy current files were removed during migration"
[[ $(pointer_version "$release_dir" current) == '2.0.0-dev.1' ]] || fail "current pointer did not select v1"
[[ $(pointer_version "$release_dir" previous) == '1.9.0' ]] || fail "previous pointer did not retain the legacy release"
cmp -s "$legacy_dir/CamStationViewerSetup.exe" "$(pointer_dir "$release_dir" previous)/CamStationViewerSetup.exe" || fail "legacy snapshot differs"
cmp -s "$installer" "$(pointer_dir "$release_dir" current)/CamStationViewerSetup.exe" || fail "active installer differs from input"

current_manifest="$(pointer_dir "$release_dir" current)/release.json"
expected_size=$(stat -c %s "$installer")
expected_hash=$(sha256sum "$installer" | awk '{print $1}')
assert_file_contains "$current_manifest" '"filename":"CamStationViewerSetup.exe"'
assert_file_contains "$current_manifest" "\"sizeBytes\":$expected_size"
assert_file_contains "$current_manifest" "\"sha256\":\"$expected_hash\""
assert_file_contains "$current_manifest" '"developmentUnsigned":true'
grep -Eq '"publishedAt":"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z"' "$current_manifest" || fail "publishedAt is not UTC RFC3339"

current_before_failure=$(readlink "$release_dir/current/active")
previous_before_failure=$(readlink "$release_dir/previous/active")
expect_failure "$publisher" --installer "$tmp_dir/missing/CamStationViewerSetup.exe" --version '2.0.0-dev.2' --release-dir "$release_dir"
[[ $(readlink "$release_dir/current/active") == "$current_before_failure" ]] || fail "input failure changed current pointer"
[[ $(readlink "$release_dir/previous/active") == "$previous_before_failure" ]] || fail "input failure changed previous pointer"

installer_v2_dir="$tmp_dir/input-v2"
installer_v2="$installer_v2_dir/CamStationViewerSetup.exe"
write_installer "$installer_v2_dir" 'viewer-installer-v2'
fake_bin="$tmp_dir/fake-bin"
mkdir -p "$fake_bin"
cat >"$fake_bin/sync" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ $# == 1 && $1 == "$FAIL_SYNC_DIR" ]]; then
  count=0
  [[ ! -f "$SYNC_COUNT_FILE" ]] || read -r count <"$SYNC_COUNT_FILE"
  count=$((count + 1))
  printf '%s\n' "$count" >"$SYNC_COUNT_FILE"
  if [[ ${SYNC_FAIL_MODE:-once} == persistent && $count -ge 2 ]] || [[ ${SYNC_FAIL_MODE:-once} == once && $count -eq 2 ]]; then
    exit 74
  fi
fi
exec "$REAL_SYNC" "$@"
EOF
chmod +x "$fake_bin/sync"
expect_failure env PATH="$fake_bin:$PATH" REAL_SYNC="$real_sync" FAIL_SYNC_DIR="$release_dir/current" SYNC_COUNT_FILE="$tmp_dir/sync-count" \
  "$publisher" --installer "$installer_v2" --version '2.0.0-dev.2' --release-dir "$release_dir"
[[ $(readlink "$release_dir/current/active") == "$current_before_failure" ]] || fail "post-switch sync failure did not restore current"
[[ $(readlink "$release_dir/previous/active") == "$previous_before_failure" ]] || fail "post-switch sync failure changed previous"

"$publisher" --installer "$installer_v2" --version '2.0.0-dev.2' --release-dir "$release_dir" >/dev/null
[[ $(pointer_version "$release_dir" current) == '2.0.0-dev.2' ]] || fail "current pointer did not select v2"
[[ $(pointer_version "$release_dir" previous) == '2.0.0-dev.1' ]] || fail "previous pointer did not retain v1"

current_before_repeat=$(readlink "$release_dir/current/active")
previous_before_repeat=$(readlink "$release_dir/previous/active")
"$publisher" --installer "$installer_v2" --version '2.0.0-dev.2' --release-dir "$release_dir" >/dev/null
[[ $(readlink "$release_dir/current/active") == "$current_before_repeat" ]] || fail "same release publish changed current"
[[ $(readlink "$release_dir/previous/active") == "$previous_before_repeat" ]] || fail "same release publish replaced the last previous release"

rollback_dir="$tmp_dir/input-rollback"
write_installer "$rollback_dir" 'viewer-installer-rollback-failure'
set +e
env PATH="$fake_bin:$PATH" REAL_SYNC="$real_sync" FAIL_SYNC_DIR="$release_dir/current" SYNC_COUNT_FILE="$tmp_dir/persistent-sync-count" SYNC_FAIL_MODE=persistent \
  "$publisher" --installer "$rollback_dir/CamStationViewerSetup.exe" --version '2.0.0-dev.rollback' --release-dir "$release_dir" >"$tmp_dir/persistent-sync.out" 2>"$tmp_dir/persistent-sync.err"
persistent_status=$?
set -e
[[ $persistent_status == 75 ]] || fail "persistent rollback sync failure exit = $persistent_status, want 75"
grep -Fq 'current pointer rollback durability failed' "$tmp_dir/persistent-sync.err" || fail "persistent rollback failure was not reported"
[[ $(readlink "$release_dir/current/active") == "$current_before_repeat" ]] || fail "persistent sync failure did not atomically restore current"
[[ $(readlink "$release_dir/previous/active") == "$previous_before_repeat" ]] || fail "persistent sync failure changed previous"

installer_v3_dir="$tmp_dir/input-v3"
installer_v4_dir="$tmp_dir/input-v4"
write_installer "$installer_v3_dir" 'viewer-installer-v3'
write_installer "$installer_v4_dir" 'viewer-installer-v4'
"$publisher" --installer "$installer_v3_dir/CamStationViewerSetup.exe" --version '2.0.0-dev.3' --release-dir "$release_dir" >"$tmp_dir/publish-v3.out" &
pid_v3=$!
"$publisher" --installer "$installer_v4_dir/CamStationViewerSetup.exe" --version '2.0.0-dev.4' --release-dir "$release_dir" >"$tmp_dir/publish-v4.out" &
pid_v4=$!
wait "$pid_v3" || fail "concurrent v3 publish failed"
wait "$pid_v4" || fail "concurrent v4 publish failed"
current_version=$(pointer_version "$release_dir" current)
previous_version=$(pointer_version "$release_dir" previous)
[[ "$current_version" != "$previous_version" ]] || fail "concurrent publishes collapsed current and previous"
case "$current_version:$previous_version" in
  '2.0.0-dev.3:2.0.0-dev.4'|'2.0.0-dev.4:2.0.0-dev.3') ;;
  *) fail "concurrent publish order is invalid: $current_version:$previous_version" ;;
esac

availability_failure="$tmp_dir/availability-failure"
(
  for number in $(seq 1 12); do
    "$publisher" --installer "$installer_v4_dir/CamStationViewerSetup.exe" --version "2.1.0-dev.$number" --release-dir "$release_dir" >/dev/null
  done
) &
writer_pid=$!
while kill -0 "$writer_pid" 2>/dev/null; do
  target=$(readlink "$release_dir/current/active") || { : >"$availability_failure"; break; }
  [[ -f "$release_dir/current/$target/release.json" ]] || { : >"$availability_failure"; break; }
done
wait "$writer_pid" || fail "repeated writer failed"
[[ ! -e "$availability_failure" ]] || fail "current active pointer was unavailable during publish"

files_before=$(find "$release_dir" -type f -printf '%P\n' | sort | sha256sum | awk '{print $1}')
current_before=$(readlink "$release_dir/current/active")
previous_before=$(readlink "$release_dir/previous/active")
"$publisher" --installer "$installer" --version '9.9.9-dry-run' --release-dir "$release_dir" --development-unsigned --dry-run >/dev/null
files_after=$(find "$release_dir" -type f -printf '%P\n' | sort | sha256sum | awk '{print $1}')
[[ "$files_before" == "$files_after" && $(readlink "$release_dir/current/active") == "$current_before" && $(readlink "$release_dir/previous/active") == "$previous_before" ]] || fail "dry-run changed release state"

find "$release_dir/releases" -maxdepth 1 -name '.stage.*' -print -quit | grep -q . && fail "staging directory was left behind"
[[ -f "$release_dir/.publish.lock" ]] || fail "publisher lock file is missing"

echo "PASS: viewer release publisher"
