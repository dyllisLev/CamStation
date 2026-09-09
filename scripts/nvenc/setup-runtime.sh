#!/usr/bin/env bash
# Run on the Docker host (inside the GPU-enabled LXC), never on Proxmox itself.
# Default: register an opt-in runtime, preserving Docker's current default.
# --set-default is intended only for the dedicated CamStation production LXC.
set -euo pipefail
set_default=false
case "${1:-}" in
  --set-default) set_default=true; shift ;;
  --help) echo "Usage: $0 [--set-default] [verified NVIDIA .run archive]"; exit 0 ;;
esac
[[ $# -le 1 && $EUID -eq 0 ]] || { echo 'Run as root; see --help' >&2; exit 1; }
[[ $(uname -m) == x86_64 ]] || { echo 'Only amd64 is supported' >&2; exit 1; }
for tool in curl sha256sum python3 dpkg ldconfig docker systemctl mount mountpoint; do
  command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool" >&2; exit 1; }
done
# This helper never changes LXC passthrough or the Proxmox kernel driver.
for node in nvidia0 nvidiactl nvidia-uvm nvidia-uvm-tools; do
  [[ -c /dev/$node ]] || { echo "GPU passthrough missing: /dev/$node" >&2; exit 1; }
done
grep -q '470.256.02' /proc/driver/nvidia/version || { echo 'Host driver must be 470.256.02' >&2; exit 1; }
runtime_root=/opt/camstation-nvidia-driver470
runtime_config=/etc/camstation-nvidia/config.toml
cache=/var/cache/camstation-nvenc-runtime
backup=/var/backups/camstation-nvenc-runtime/$(date -u +%Y%m%dT%H%M%SZ)-$$
install -d -m 0700 "$cache" "$backup"
install -d /etc/camstation-nvidia /usr/local/libexec
for file in /etc/docker/daemon.json "$runtime_config" /etc/systemd/system/camstation-nvidia-driver.service; do
  if [[ -f $file ]]; then cp --parents -a "$file" "$backup/"; fi
done
printf '%s\n' "Configuration backup: $backup"
driver_archive=${1:-$cache/NVIDIA-Linux-x86_64-470.256.02.run}
if [[ ! -f $driver_archive ]]; then
  curl --fail --silent --show-error --location --retry 3 https://us.download.nvidia.com/XFree86/Linux-x86_64/470.256.02/NVIDIA-Linux-x86_64-470.256.02.run -o "$driver_archive"
fi
printf '%s  %s\n' d6451862deb695bb0447f3b7cd6268f73e81168c10e2c10597ff3fa01349b1de "$driver_archive" | sha256sum --check --status
# Direct pinned packages avoid Ubuntu's transitional 470 packages, which now
# install an incompatible 580 userspace driver. Toolkit installs no GPU driver.
cat >"$cache/toolkit.sha256" <<'SUMS'
326da26f762a24f93c251b4c4932dec187426260284d2ae16351cb7178a1e87f  libnvidia-container1_1.20.0-1_amd64.deb
6535704295d041b2d51f0364e4a65b8631325bb93921b7fe08266cd4aed66e59  libnvidia-container-tools_1.20.0-1_amd64.deb
28a6f2d41913897effe923b46234998f878fc21b32941ea9a2b878efa62b2267  nvidia-container-toolkit-base_1.20.0-1_amd64.deb
da7bb4acbd6027349fb5936dfed7e6394592c40a5392ca43172e2356d5b539b5  nvidia-container-toolkit_1.20.0-1_amd64.deb
SUMS
packages=()
while read -r checksum package; do
  packages+=("$cache/$package")
  if ! printf '%s  %s\n' "$checksum" "$cache/$package" | sha256sum --check --status 2>/dev/null; then
    curl --fail --silent --show-error --location --retry 3 "https://nvidia.github.io/libnvidia-container/stable/deb/amd64/$package" -o "$cache/$package"
  fi
done <"$cache/toolkit.sha256"
(cd "$cache" && sha256sum --check toolkit.sha256)
dpkg -i "${packages[@]}"
source_parent=$(mktemp -d "$cache/driver-source.XXXXXX")
source_dir=$source_parent/extracted
trap 'rm -rf -- "$source_parent"' EXIT
sh "$driver_archive" --extract-only --target "$source_dir" >/dev/null
mkdir -p "$runtime_root"/{usr/lib/x86_64-linux-gnu,usr/bin,etc,dev,proc}
for lib in libcuda libnvidia-encode libnvcuvid libnvidia-ml libnvidia-ptxjitcompiler libnvidia-compiler libnvidia-allocator; do
  install -m 0644 "$source_dir/$lib.so.470.256.02" "$runtime_root/usr/lib/x86_64-linux-gnu/"
  ln -sfn "$lib.so.470.256.02" "$runtime_root/usr/lib/x86_64-linux-gnu/$lib.so.1"
  ln -sfn "$lib.so.470.256.02" "$runtime_root/usr/lib/x86_64-linux-gnu/$lib.so"
done
install -m 0755 "$source_dir/nvidia-smi" "$runtime_root/usr/bin/nvidia-smi"
printf '/usr/lib/x86_64-linux-gnu\n' >"$runtime_root/etc/ld.so.conf"
ldconfig -r "$runtime_root"
# libnvidia-container chroots its NVML helper into driver-root. A private cache
# and GPU/proc views are required there; system driver libraries stay untouched.
cat >/usr/local/libexec/camstation-nvidia-mounts <<'MOUNTS'
#!/usr/bin/env bash
set -euo pipefail
root=/opt/camstation-nvidia-driver470
for node in nvidia0 nvidiactl nvidia-uvm nvidia-uvm-tools; do
  [[ -c /dev/$node ]] || { echo "Missing GPU device $node" >&2; exit 1; }
  if ! mountpoint -q "$root/dev/$node"; then
    touch "$root/dev/$node"
    mount --bind "/dev/$node" "$root/dev/$node"
  fi
done
if ! mountpoint -q "$root/proc"; then mount --bind /proc "$root/proc"; fi
mount -o remount,bind,ro "$root/proc"
MOUNTS
chmod 0755 /usr/local/libexec/camstation-nvidia-mounts
cat >/etc/systemd/system/camstation-nvidia-driver.service <<'UNIT'
[Unit]
Description=CamStation isolated NVIDIA 470 driver view
After=local-fs.target
Before=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/libexec/camstation-nvidia-mounts
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable camstation-nvidia-driver.service
systemctl start camstation-nvidia-driver.service
cat >"$runtime_config" <<'CONFIG'
[nvidia-container-cli]
root = "/opt/camstation-nvidia-driver470"
ldcache = "/etc/ld.so.cache"
load-kmods = false
no-cgroups = true

[nvidia-container-runtime]
mode = "legacy"
runtimes = ["runc"]

[nvidia-container-runtime-hook]
path = "/usr/local/libexec/camstation-nvidia-hook"
CONFIG
cat >/usr/local/libexec/camstation-nvidia-runtime <<'WRAPPER'
#!/bin/sh
export NVIDIA_CTK_CONFIG_FILE_PATH=/etc/camstation-nvidia/config.toml
exec /usr/bin/nvidia-container-runtime "$@"
WRAPPER
cat >/usr/local/libexec/camstation-nvidia-hook <<'HOOK'
#!/bin/sh
exec /usr/bin/nvidia-container-runtime-hook --config /etc/camstation-nvidia/config.toml "$@"
HOOK
chmod 0755 /usr/local/libexec/camstation-nvidia-{runtime,hook}
# Driver discovery consumes no NVENC encoding session.
nvidia-container-cli --root="$runtime_root" --ldcache=/etc/ld.so.cache info
install -d /etc/docker
python3 - "$set_default" <<'PY'
import json, pathlib, sys
p = pathlib.Path('/etc/docker/daemon.json')
d = json.loads(p.read_text()) if p.exists() else {}
d.setdefault('runtimes', {})['camstation-nvidia'] = {
    'path': '/usr/local/libexec/camstation-nvidia-runtime', 'runtimeArgs': []}
if sys.argv[1] == 'true':
    d['default-runtime'] = 'camstation-nvidia'
tmp = p.with_suffix('.camstation-tmp')
tmp.write_text(json.dumps(d, indent=2) + '\n')
tmp.chmod(0o600)
PY
dockerd --validate --config-file=/etc/docker/daemon.camstation-tmp
mv /etc/docker/daemon.camstation-tmp /etc/docker/daemon.json
# Reload is supported for runtimes/default-runtime; no running workload restart.
if ! systemctl reload docker; then
  if [[ -f $backup/etc/docker/daemon.json ]]; then
    cp -a "$backup/etc/docker/daemon.json" /etc/docker/daemon.json
  else
    rm -f /etc/docker/daemon.json
  fi
  echo 'Docker reload failed; restored the previous daemon configuration.' >&2
  exit 1
fi
docker info --format 'Docker default runtime: {{.DefaultRuntime}}'
printf 'Registered camstation-nvidia; use NVIDIA_VISIBLE_DEVICES=all and NVIDIA_DRIVER_CAPABILITIES=compute,video,utility.\n'
