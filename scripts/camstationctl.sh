#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
BIN="$ROOT_DIR/camstationd"
DATA_DIR="$ROOT_DIR/data"
LOG_DIR="$DATA_DIR/runtime-logs"
PID_FILE="$LOG_DIR/camstationd.pid"
LOG_FILE="$LOG_DIR/camstationd.out"
ADDR="${CAMSTATION_ADDR:-0.0.0.0:18080}"
DB_PATH="${CAMSTATION_DB:-./data/camstation.db}"
SEGMENT_MINUTES="${CAMSTATION_SEGMENT_MINUTES:-5}"
MAX_STORAGE_GB="${CAMSTATION_MAX_STORAGE_GB:-0.30}"
RECORDING_ENABLED="${CAMSTATION_RECORDING_ENABLED:-false}"
HEALTH_URL="${CAMSTATION_HEALTH_URL:-http://127.0.0.1:18080/api/health}"
RECORDER_URL="${CAMSTATION_RECORDER_URL:-http://127.0.0.1:18080/api/recorders/status}"

usage() {
  cat <<EOF
Usage: $0 {status|start|stop|restart|verify}

Environment overrides:
  CAMSTATION_ADDR=$ADDR
  CAMSTATION_DB=$DB_PATH
  CAMSTATION_SEGMENT_MINUTES=$SEGMENT_MINUTES
  CAMSTATION_MAX_STORAGE_GB=$MAX_STORAGE_GB
  CAMSTATION_RECORDING_ENABLED=$RECORDING_ENABLED
EOF
}

camstation_pids() {
  pgrep -f "$ROOT_DIR/camstationd" 2>/dev/null || true
  pgrep -f "^./camstationd .*18080" 2>/dev/null || true
}

owned_go2rtc_pids() {
  pgrep -f "go2rtc -config (./data/go2rtc.yaml|$ROOT_DIR/data/go2rtc.yaml)" 2>/dev/null || true
}

owned_ffmpeg_pids() {
  pgrep -f "ffmpeg .*rtsp://127[.]0[.]0[.]1:8554/.* $ROOT_DIR/data/temp/" 2>/dev/null || true
  pgrep -f "ffmpeg .*rtsp://127[.]0[.]0[.]1:8554/.* data/temp/" 2>/dev/null || true
}

unique_pids() {
  awk 'NF && !seen[$1]++ {print $1}'
}

print_processes() {
  echo "== camstationd =="
  camstation_pids | unique_pids | xargs -r ps -o pid,ppid,lstart,cmd -p
  echo
  echo "== managed go2rtc =="
  owned_go2rtc_pids | unique_pids | xargs -r ps -o pid,ppid,lstart,cmd -p
  echo
  echo "== managed ffmpeg =="
  owned_ffmpeg_pids | unique_pids | xargs -r ps -o pid,ppid,lstart,cmd -p
  echo
  echo "== ports =="
  ss -ltnp | grep -E ':18080|:1984|:8554|:8555' || true
}

wait_for_exit() {
  local pid="$1"
  local waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if (( waited >= 15 )); then
      return 1
    fi
    sleep 1
    waited=$((waited + 1))
  done
}

terminate_pids() {
  local label="$1"
  shift
  local pids=("$@")
  if (( ${#pids[@]} == 0 )); then
    return 0
  fi

  echo "Stopping $label: ${pids[*]}"
  kill "${pids[@]}" 2>/dev/null || true
  local pid
  for pid in "${pids[@]}"; do
    wait_for_exit "$pid" || true
  done

  local survivors=()
  for pid in "${pids[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      survivors+=("$pid")
    fi
  done
  if (( ${#survivors[@]} > 0 )); then
    echo "Force stopping $label: ${survivors[*]}"
    kill -9 "${survivors[@]}" 2>/dev/null || true
  fi
}

status() {
  print_processes
}

stop() {
  mapfile -t cams < <(camstation_pids | unique_pids)
  terminate_pids "camstationd" "${cams[@]}"

  mapfile -t ffmpegs < <(owned_ffmpeg_pids | unique_pids)
  terminate_pids "managed ffmpeg leftovers" "${ffmpegs[@]}"

  mapfile -t go2rtcs < <(owned_go2rtc_pids | unique_pids)
  terminate_pids "managed go2rtc leftovers" "${go2rtcs[@]}"

  rm -f "$PID_FILE"
}

start() {
  mkdir -p "$LOG_DIR"
  if [[ ! -x "$BIN" ]]; then
    echo "Missing executable: $BIN" >&2
    echo "Run: go build -o camstationd ./cmd/camstationd" >&2
    return 1
  fi
  mapfile -t cams < <(camstation_pids | unique_pids)
  if (( ${#cams[@]} > 0 )); then
    echo "camstationd already running: ${cams[*]}" >&2
    return 1
  fi

  local recording_args=()
  case "$RECORDING_ENABLED" in
    1|true|TRUE|yes|YES|on|ON)
      recording_args=(-recording-enabled)
      ;;
    0|false|FALSE|no|NO|off|OFF)
      ;;
    *)
      echo "Invalid CAMSTATION_RECORDING_ENABLED=$RECORDING_ENABLED; use true or false" >&2
      return 1
      ;;
  esac

  (
    cd "$ROOT_DIR"
    exec setsid "$BIN" \
      -addr "$ADDR" \
      -db "$DB_PATH" \
      "${recording_args[@]}" \
      -segment-minutes "$SEGMENT_MINUTES" \
      -max-storage-gb "$MAX_STORAGE_GB" \
      >"$LOG_FILE" 2>&1 < /dev/null
  ) &
  local pid=$!
  echo "$pid" > "$PID_FILE"
  echo "Started camstationd pid=$pid log=$LOG_FILE"
}

verify() {
  local ok=0
  echo "== health =="
  for _ in $(seq 1 15); do
    if curl -fsS --max-time 3 "$HEALTH_URL" 2>/dev/null; then
      ok=1
      break
    fi
    sleep 1
  done
  echo
  if (( ok != 1 )); then
    echo "Health check failed: $HEALTH_URL" >&2
    tail -80 "$LOG_FILE" 2>/dev/null || true
    return 1
  fi

  echo "== recorder status =="
  curl -fsS --max-time 5 "$RECORDER_URL"
  echo

  echo "== current processes =="
  print_processes

  echo "== newest temp segments =="
  sleep 2
  find "$DATA_DIR/temp" -type f -name '*.mp4' -printf '%TY-%Tm-%Td %TH:%TM:%TS %p\n' 2>/dev/null \
    | sort \
    | tail -10 || true
}

restart() {
  stop
  start
  verify
}

cmd="${1:-}"
case "$cmd" in
  status) status ;;
  start) start ;;
  stop) stop ;;
  restart) restart ;;
  verify) verify ;;
  -h|--help|help) usage ;;
  *) usage >&2; exit 2 ;;
esac
