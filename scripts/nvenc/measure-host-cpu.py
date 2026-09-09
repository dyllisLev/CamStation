#!/usr/bin/env python3
"""Read-only Proxmox host and CamStation cgroup CPU sampling; emits JSONL.

Run on the Proxmox host, with permission to read cgroups and inspect the named
container in the selected CT. Never reads container environment or FFmpeg args.
Percentages ending in one_core_pct use one logical CPU = 100%; host percentages
use the entire host = 100%. Guest accounting is excluded from /proc/stat totals
because it is already included in user/nice. I/O wait is reported separately.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import time


def read_snapshot(paths):
    cpu = [int(x) for x in Path('/proc/stat').read_text().splitlines()[0].split()[1:9]]
    usage = {}
    for name, path in paths.items():
        try:
            fields = dict(line.split() for line in path.read_text().splitlines())
            usage[name] = int(fields['usage_usec'])
        except (OSError, KeyError, ValueError):
            usage[name] = None
    return time.monotonic(), cpu, usage


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ctid', type=int, default=113)
    parser.add_argument('--dev-ctid', type=int, default=102)
    parser.add_argument('--container', default='openship-camstation-camstation')
    parser.add_argument('--interval', type=float, default=5)
    parser.add_argument('--samples', type=int, default=60)
    args = parser.parse_args()
    if args.interval <= 0 or args.samples <= 0:
        parser.error('interval and samples must be positive')
    cid = subprocess.check_output([
        'pct', 'exec', str(args.ctid), '--', 'docker', 'inspect',
        '--format', '{{.Id}}', args.container,
    ], text=True).strip()
    if len(cid) != 64 or any(c not in '0123456789abcdef' for c in cid):
        raise SystemExit('container identity is invalid')
    root = Path('/sys/fs/cgroup/lxc')
    paths = {
        'production_ct': root / str(args.ctid) / 'cpu.stat',
        'development_ct': root / str(args.dev_ctid) / 'cpu.stat',
        'camstation': root / str(args.ctid) / 'ns/system.slice' / f'docker-{cid}.scope/cpu.stat',
    }
    prior = read_snapshot(paths)
    if prior[2]['camstation'] is None:
        raise SystemExit('CamStation CPU cgroup is unavailable; no measurement recorded')
    print(json.dumps({
        'type': 'meta', 'time_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'logical_cpus': os.cpu_count(), 'interval_seconds': args.interval,
        'samples': args.samples, 'container_id': cid,
    }), flush=True)
    for _ in range(args.samples):
        time.sleep(max(0, prior[0] + args.interval - time.monotonic()))
        current = read_snapshot(paths)
        ticks = [b - a for a, b in zip(prior[1], current[1])]
        total, elapsed = sum(ticks), current[0] - prior[0]
        if total <= 0:
            raise SystemExit('host CPU counters did not advance')
        row = {
            'type': 'sample', 'time_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'elapsed': elapsed, 'host_active_pct': 100 * (total - ticks[3] - ticks[4]) / total,
            'host_busy_including_iowait_pct': 100 * (total - ticks[3]) / total,
            'host_iowait_pct': 100 * ticks[4] / total, 'loadavg': list(os.getloadavg()),
        }
        for name, value in current[2].items():
            previous = prior[2][name]
            row[name + '_one_core_pct'] = (
                None if value is None or previous is None or value < previous
                else (value - previous) / elapsed / 10000
            )
        print(json.dumps(row), flush=True)
        prior = current


if __name__ == '__main__':
    main()
