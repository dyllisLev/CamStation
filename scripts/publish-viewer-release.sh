#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 --installer <path> --version <version> --release-dir <dir> [--development-unsigned] [--dry-run]" >&2
}

die() {
  echo "publish-viewer-release: $*" >&2
  exit 1
}

json_string() {
  local file=$1
  local key=$2
  sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$file"
}

json_scalar() {
  local file=$1
  local key=$2
  sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\([^,}]*\).*/\1/p" "$file" | tr -d '[:space:]'
}

validate_release() {
  local directory=$1
  local expected_version=$2
  local expected_size=$3
  local expected_sha=$4
  local expected_unsigned=$5
  local artifact="$directory/CamStationViewerSetup.exe"
  local release_manifest="$directory/release.json"

  [[ -d "$directory" && ! -L "$directory" ]] || return 1
  [[ -f "$artifact" && ! -L "$artifact" && -f "$release_manifest" && ! -L "$release_manifest" ]] || return 1
  [[ $(stat -c %s -- "$artifact") == "$expected_size" ]] || return 1
  [[ $(sha256sum -- "$artifact" | awk '{print $1}') == "$expected_sha" ]] || return 1
  [[ $(json_string "$release_manifest" version) == "$expected_version" ]] || return 1
  [[ $(json_string "$release_manifest" filename) == 'CamStationViewerSetup.exe' ]] || return 1
  [[ $(json_scalar "$release_manifest" sizeBytes) == "$expected_size" ]] || return 1
  [[ $(json_string "$release_manifest" sha256) == "$expected_sha" ]] || return 1
  [[ $(json_scalar "$release_manifest" developmentUnsigned) == "$expected_unsigned" ]] || return 1
}

stage_release() {
  local source_installer=$1
  local source_manifest=$2
  local release_id=$3
  local expected_version=$4
  local expected_size=$5
  local expected_sha=$6
  local expected_unsigned=$7
  local destination="$releases_dir/$release_id"

  if [[ -e "$destination" ]]; then
    validate_release "$destination" "$expected_version" "$expected_size" "$expected_sha" "$expected_unsigned" || die "immutable release conflicts with existing content"
    return 0
  fi

  stage=$(mktemp -d "$releases_dir/.stage.XXXXXX")
  cp --reflink=auto -- "$source_installer" "$stage/CamStationViewerSetup.exe"
  if [[ "$source_manifest" == '-' ]]; then
    printf '%s\n' "$manifest" >"$stage/release.json"
  else
    cp --reflink=auto -- "$source_manifest" "$stage/release.json"
  fi
  chmod 0444 "$stage/CamStationViewerSetup.exe" "$stage/release.json"
  validate_release "$stage" "$expected_version" "$expected_size" "$expected_sha" "$expected_unsigned" || die "staged release verification failed"
  chmod 0555 "$stage"
  sync "$stage/CamStationViewerSetup.exe" "$stage/release.json" "$stage"
  mv -T -- "$stage" "$destination"
  stage=''
  sync "$releases_dir"
}

validate_pointer_target() {
  local target=$1
  [[ "$target" == ../releases/* ]] || return 1
  local release_id=${target#../releases/}
  [[ -n "$release_id" && "$release_id" != */* && "$release_id" != '.' && "$release_id" != '..' ]] || return 1
  [[ -d "$releases_dir/$release_id" && ! -L "$releases_dir/$release_id" ]]
}

read_pointer() {
  local directory=$1
  local active="$directory/active"
  if [[ -L "$active" ]]; then
    readlink "$active"
    return
  fi
  [[ ! -e "$active" ]] || return 1
  return 2
}

switch_pointer() {
  local directory=$1
  local target=$2
  pointer_switched=false
  prepared_link=$(mktemp "$directory/.active.new.XXXXXX") || return 1
  rm -- "$prepared_link" || return 1
  ln -s -- "$target" "$prepared_link" || return 1
  sync "$directory" || return 1
  mv -Tf -- "$prepared_link" "$directory/active" || return 1
  prepared_link=''
  pointer_switched=true
  sync "$directory" || return 1
}

restore_pointer() {
  local directory=$1
  local existed=$2
  local target=$3
  local durability_failed=false
  if [[ "$existed" == true ]]; then
    prepared_link=$(mktemp "$directory/.active.rollback.XXXXXX") || return 1
    rm -- "$prepared_link" || return 1
    ln -s -- "$target" "$prepared_link" || return 1
    sync "$directory" || durability_failed=true
    mv -Tf -- "$prepared_link" "$directory/active" || return 1
    prepared_link=''
  else
    rm -f -- "$directory/active" || return 1
  fi
  sync "$directory" || durability_failed=true
  [[ "$durability_failed" == false ]]
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
[[ "$release_dir" != -* ]] || die "relative release directory must not begin with a dash"
[[ "$release_dir" != '/' && "$release_dir" != '.' && "$release_dir" != '..' && "/$release_dir/" != *'/../'* ]] || die "unsafe release directory"
command -v flock >/dev/null 2>&1 || die "flock is required"
command -v sync >/dev/null 2>&1 || die "sync is required"

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
release_dir=$(cd -- "$release_dir" && pwd -P)
exec {lock_fd}>"$release_dir/.publish.lock"
chmod 0600 "$release_dir/.publish.lock"
flock -x "$lock_fd"

current_dir="$release_dir/current"
previous_dir="$release_dir/previous"
releases_dir="$release_dir/releases"
for directory in "$current_dir" "$previous_dir" "$releases_dir"; do
  [[ ! -L "$directory" ]] || die "release layout directory must not be a symlink"
  mkdir -p -- "$directory"
  [[ -d "$directory" ]] || die "release layout path is not a directory"
done
sync "$release_dir" "$current_dir" "$previous_dir" "$releases_dir"

stage=''
prepared_link=''
current_switched=false
previous_switched=false
old_current_pointer_exists=false
old_current_pointer=''
old_previous_pointer_exists=false
old_previous_pointer=''

cleanup() {
  local status=$?
  local rollback_failed=false
  trap - EXIT
  set +e
  if ((status != 0)); then
    if [[ "$previous_switched" == true ]]; then
      if ! restore_pointer "$previous_dir" "$old_previous_pointer_exists" "$old_previous_pointer"; then
        echo "publish-viewer-release: previous pointer rollback durability failed" >&2
        rollback_failed=true
      fi
    fi
    if [[ "$current_switched" == true ]]; then
      if ! restore_pointer "$current_dir" "$old_current_pointer_exists" "$old_current_pointer"; then
        echo "publish-viewer-release: current pointer rollback durability failed" >&2
        rollback_failed=true
      fi
    fi
  fi
  [[ -z "$prepared_link" || ! -e "$prepared_link" && ! -L "$prepared_link" ]] || rm -f -- "$prepared_link"
  [[ -z "$stage" || ! -e "$stage" ]] || { chmod -R u+w -- "$stage"; rm -rf -- "$stage"; }
  if [[ "$rollback_failed" == true ]]; then
    echo "publish-viewer-release: publication failed with status $status and pointer rollback could not be durably confirmed" >&2
    status=75
  fi
  exit "$status"
}
trap cleanup EXIT

if old_current_pointer=$(read_pointer "$current_dir"); then
  old_current_pointer_exists=true
  validate_pointer_target "$old_current_pointer" || die "current pointer is invalid"
else
  pointer_status=$?
  ((pointer_status == 2)) || die "current pointer is invalid"
  if [[ -f "$current_dir/release.json" && ! -L "$current_dir/release.json" && -f "$current_dir/CamStationViewerSetup.exe" && ! -L "$current_dir/CamStationViewerSetup.exe" ]]; then
    legacy_version=$(json_string "$current_dir/release.json" version)
    legacy_size=$(json_scalar "$current_dir/release.json" sizeBytes)
    legacy_sha=$(json_string "$current_dir/release.json" sha256)
    legacy_unsigned=$(json_scalar "$current_dir/release.json" developmentUnsigned)
    [[ "$legacy_version" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ && "$legacy_size" =~ ^[0-9]+$ && "$legacy_sha" =~ ^[a-f0-9]{64}$ && "$legacy_unsigned" =~ ^(true|false)$ ]] || die "legacy release manifest is invalid"
    legacy_id="$legacy_version-$legacy_sha"
    stage_release "$current_dir/CamStationViewerSetup.exe" "$current_dir/release.json" "$legacy_id" "$legacy_version" "$legacy_size" "$legacy_sha" "$legacy_unsigned"
    old_current_pointer="../releases/$legacy_id"
  fi
fi

if old_previous_pointer=$(read_pointer "$previous_dir"); then
  old_previous_pointer_exists=true
  validate_pointer_target "$old_previous_pointer" || die "previous pointer is invalid"
else
  pointer_status=$?
  ((pointer_status == 2)) || die "previous pointer is invalid"
fi

release_id="$version-$sha256"
stage_release "$installer" '-' "$release_id" "$version" "$size_bytes" "$sha256" "$development_unsigned"
new_current_pointer="../releases/$release_id"

if [[ "$old_current_pointer_exists" != true || "$old_current_pointer" != "$new_current_pointer" ]]; then
  if ! switch_pointer "$current_dir" "$new_current_pointer"; then
    [[ "$pointer_switched" == true ]] && current_switched=true
    die "failed to switch current release"
  fi
  current_switched=true
fi

if [[ "$current_switched" == true && -n "$old_current_pointer" && ( "$old_previous_pointer_exists" != true || "$old_previous_pointer" != "$old_current_pointer" ) ]]; then
  if ! switch_pointer "$previous_dir" "$old_current_pointer"; then
    [[ "$pointer_switched" == true ]] && previous_switched=true
    die "failed to switch previous release"
  fi
  previous_switched=true
fi

printf 'published %s (%s bytes, %s)\n' "$version" "$size_bytes" "$sha256"
