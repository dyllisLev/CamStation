# OPERATIONAL SCRIPTS GUIDE

## OVERVIEW
`scripts/` contains local development lifecycle and monitoring helpers for the camera-reachable test environment.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Start/stop/restart/status | `camstationctl.sh` | Use for normal dev daemon lifecycle. |
| Runtime smoke verification | `camstationctl.sh verify` | Health, recorders, processes, temp segments. |
| Hourly recording checks | `hourly-recording-monitor.sh` | Logs health/storage/ffprobe evidence. |

## CONVENTIONS
- `camstationctl.sh` scopes process handling to this workspace's `camstationd`, generated go2rtc config, local RTSP inputs, and temp paths.
- Defaults are `0.0.0.0:18080`, `./data/camstation.db`, 5-minute segments, and `CAMSTATION_MAX_STORAGE_GB=0.30`.
- The control script starts recording-enabled dev runs; mention this when interpreting recorder state.
- Monitoring output belongs under `data/monitoring` and `data/runtime-logs`, not in git.
- Use KST when reporting operational monitor timestamps that matter to the user.

## ANTI-PATTERNS
- Do not replace normal lifecycle work with ad hoc `pkill`, `killall`, manual `setsid`, or background shell fragments.
- Do not broaden PID matching so it can kill unrelated production services.
- Do not make scripts depend on raw camera credentials in committed files.
- Do not commit runtime logs, PID files, DB files, temp segments, recordings, or generated go2rtc config.

## VERIFY
```bash
scripts/camstationctl.sh status
scripts/camstationctl.sh verify
```
