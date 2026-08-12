#!/usr/bin/env bash
set -euo pipefail

camstation_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
camstation_go_version=1.25.12
camstation_go_sha256=234828b7a89e0e303d2556310ee549fbcf253d28de937bac3da13d6294262ac1
camstation_go_dir="$camstation_root/.tools/go$camstation_go_version"

require_command() {
  local command_name=$1
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required command not found: $command_name" >&2
    exit 1
  fi
}

install_go() {
  local system_name machine_name archive_name download_url staging_dir archive_path
  system_name=$(uname -s)
  machine_name=$(uname -m)
  if [[ "$system_name" != "Linux" || "$machine_name" != "x86_64" ]]; then
    echo "Automatic Go installation supports Linux x86_64 only." >&2
    echo "Install Go 1.25 or newer and ensure 'go' is on PATH." >&2
    exit 1
  fi

  require_command curl
  require_command sha256sum
  require_command tar

  mkdir -p "$camstation_root/.tools"
  staging_dir=$(mktemp -d "$camstation_root/.tools/.go-install.XXXXXX")
  archive_name="go${camstation_go_version}.linux-amd64.tar.gz"
  archive_path="$staging_dir/$archive_name"
  download_url="https://go.dev/dl/$archive_name"

  cleanup_go_staging() {
    case "$staging_dir" in
      "$camstation_root"/.tools/.go-install.*) rm -rf -- "$staging_dir" ;;
      *) echo "Refusing to clean unexpected staging path: $staging_dir" >&2 ;;
    esac
  }
  trap cleanup_go_staging EXIT INT TERM

  echo "Downloading Go $camstation_go_version from go.dev..."
  curl --fail --location --retry 3 --output "$archive_path" "$download_url"
  printf '%s  %s\n' "$camstation_go_sha256" "$archive_path" | sha256sum --check --status
  tar -C "$staging_dir" -xzf "$archive_path"

  if [[ ! -x "$staging_dir/go/bin/go" ]]; then
    echo "Downloaded Go archive did not contain bin/go." >&2
    exit 1
  fi
  if [[ -e "$camstation_go_dir" ]]; then
    echo "Refusing to replace unexpected Go directory: $camstation_go_dir" >&2
    exit 1
  fi
  mv -- "$staging_dir/go" "$camstation_go_dir"
  cleanup_go_staging
  trap - EXIT INT TERM
}

require_command npm

if [[ ! -x "$camstation_go_dir/bin/go" ]]; then
  install_go
fi

if [[ $("$camstation_go_dir/bin/go" env GOVERSION) != "go$camstation_go_version" ]]; then
  echo "Unexpected Go version in $camstation_go_dir" >&2
  exit 1
fi

cd "$camstation_root"
"$camstation_go_dir/bin/go" mod download
npm --prefix "$camstation_root/web" ci
npm --prefix "$camstation_root/viewer-app" ci

mkdir -p \
  "$camstation_root/data/recordings" \
  "$camstation_root/data/temp" \
  "$camstation_root/data/viewer-releases" \
  "$camstation_root/data/runtime-logs"

echo "CamStation 2.0 development environment is ready."
echo "Go: $("$camstation_go_dir/bin/go" version)"
echo "Node: $(node --version)"
echo "Runtime data: $camstation_root/data"
