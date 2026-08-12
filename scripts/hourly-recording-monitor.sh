#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="$ROOT_DIR/data/monitoring"
STATE_DIR="$ROOT_DIR/data/runtime-logs"
mkdir -p "$LOG_DIR" "$STATE_DIR"

END_KST="${1:-2026-07-01 08:00:00}"
END_EPOCH="$(TZ=Asia/Seoul date -d "$END_KST" +%s)"
SUMMARY="$LOG_DIR/hourly-summary.log"

run_check() {
  local now_kst now_utc stamp log_file
  now_kst="$(TZ=Asia/Seoul date '+%Y-%m-%d %H:%M:%S KST')"
  now_utc="$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  stamp="$(TZ=Asia/Seoul date '+%Y%m%d-%H%M%S')"
  log_file="$LOG_DIR/check-$stamp.log"

  {
    echo "CamStation hourly monitor"
    echo "KST: $now_kst"
    echo "UTC: $now_utc"
    echo

    echo "== process =="
    ps -ef | grep -E '[c]amstationd|[g]o2rtc|[f]fmpeg' || true
    echo

    echo "== health =="
    curl -sS --max-time 8 http://127.0.0.1:18080/api/health || true
    echo
    echo

    echo "== recorder status =="
    curl -sS --max-time 8 http://127.0.0.1:18080/api/recorders/status || true
    echo
    echo

    echo "== storage =="
    curl -sS --max-time 8 http://127.0.0.1:18080/api/recordings/storage || true
    echo
    du -sb "$ROOT_DIR/data/recordings" "$ROOT_DIR/data/temp" 2>/dev/null || true
    echo

    echo "== recent recording cleanup events =="
    curl -sS --max-time 8 http://127.0.0.1:18080/api/events \
      | grep -Eo 'automatic recording cleanup completed|recording cleanup completed|automatic recording cleanup failed|recording cleanup failed|"deleted":[0-9]+|"afterBytes":[0-9]+|"beforeBytes":[0-9]+|"maxBytes":[0-9]+' \
      | head -40 || true
    echo

    echo "== newest finalized recordings =="
    find "$ROOT_DIR/data/recordings" -type f -name '*.mp4' -printf '%TY-%Tm-%Td %TH:%TM %.0T@ %s %p\n' 2>/dev/null \
      | sort -k3,3n \
      | tail -30 || true
    echo

    echo "== active temp segments =="
    find "$ROOT_DIR/data/temp" -type f -name '*.mp4' -printf '%TY-%Tm-%Td %TH:%TM %.0T@ %s %p\n' 2>/dev/null \
      | sort -k3,3n || true
    echo

    echo "== stale temp segments older than 15 minutes =="
    find "$ROOT_DIR/data/temp" -type f -name '*.mp4' -mmin +15 -printf '%TY-%Tm-%Td %TH:%TM %.0T@ %s %p\n' 2>/dev/null \
      | sort -k3,3n || true
    echo

    echo "== local rtsp ffprobe =="
    for stream in camera-1 1 2; do
      echo "-- stream=$stream"
      timeout 12 ffprobe -v error -rtsp_transport tcp \
        -show_entries stream=codec_name,width,height \
        -of compact=p=0:nk=1 "rtsp://127.0.0.1:8554/$stream" || true
    done
  } >"$log_file" 2>&1

  local health_ok workers_running cleanup_errors stale_temp newest_temp newest_recording summary_status
  health_ok="$(grep -c '"ok":true' "$log_file" || true)"
  workers_running="$(grep -o '"state":"running"' "$log_file" | wc -l | tr -d ' ')"
  cleanup_errors="$(awk '
    /== recent recording cleanup events ==/{flag=1; next}
    /== newest finalized recordings ==/{flag=0}
    flag && /cleanup failed/{count++}
    END{print count+0}
  ' "$log_file")"
  stale_temp="$(awk '/== stale temp segments older than 15 minutes ==/{flag=1; next} /== local rtsp ffprobe ==/{flag=0} flag && /\\.mp4/{count++} END{print count+0}' "$log_file")"
  newest_temp="$(grep '/data/temp/.*\.mp4' "$log_file" | tail -1 | awk '{print $4, $5, $6}')"
  newest_recording="$(grep '/data/recordings/.*\.mp4' "$log_file" | tail -1 | awk '{print $4, $5, $6}')"
  summary_status="ok"
  if [[ "$health_ok" -lt 1 || "$workers_running" -lt 3 || "$cleanup_errors" -gt 0 || "$stale_temp" -gt 0 ]]; then
    summary_status="check-needed"
  fi

  {
    echo "[$now_kst] status=$summary_status health_ok=$health_ok workers_running=$workers_running cleanup_errors=$cleanup_errors stale_temp=$stale_temp log=$log_file"
    echo "  newest_recording=$newest_recording"
    echo "  newest_temp=$newest_temp"
  } >>"$SUMMARY"
}

echo $$ >"$STATE_DIR/hourly-recording-monitor.pid"
echo "monitor started: $(TZ=Asia/Seoul date '+%Y-%m-%d %H:%M:%S KST'), end=$END_KST KST" >>"$SUMMARY"

run_check

while [[ "$(date +%s)" -lt "$END_EPOCH" ]]; do
  next_hour="$(date -d 'next hour' +%Y-%m-%dT%H:00:00)"
  sleep_seconds="$(( $(date -d "$next_hour" +%s) - $(date +%s) ))"
  if [[ "$sleep_seconds" -lt 60 ]]; then
    sleep_seconds=3600
  fi
  sleep "$sleep_seconds"
  if [[ "$(date +%s)" -le "$END_EPOCH" ]]; then
    run_check
  fi
done

echo "monitor finished: $(TZ=Asia/Seoul date '+%Y-%m-%d %H:%M:%S KST')" >>"$SUMMARY"
