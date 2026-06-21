#!/bin/bash
# 실시간 녹화 백업 - inotifywait + rclone
# 백업 완료 → DB 마크 → 로컬 삭제 (DB 마크 실패 시 삭제 안 함)
RECORDINGS_DIR="/mnt/hdd/recordings"
REMOTE="gdrive:cctv"
LOG="/var/log/camstation-backup.log"
DB="/opt/camstation/data/camstation.db"

log() {
    echo "[$(date "+%Y-%m-%d %H:%M:%S")] $*" >> "$LOG"
}

mark_and_delete() {
    local filepath="$1"
    local rel="$2"
    local filename=$(basename "$filepath")
    local camera=$(echo "$rel" | cut -d/ -f1)
    local date_str=$(echo "$rel" | cut -d/ -f2)
    local ts_end=$(date -d "$date_str" +%s 2>/dev/null || echo "")
    # DB 마크 + 로컬 삭제를 atomic하게 (python에서 처리)
    python3 -c "
import sqlite3, os, sys, time
db_path = '$DB'
filename = '$filename'
filepath = '$filepath'
camera = '$camera'
ts_end = $ts_end if '$ts_end' else None
for attempt in range(5):
    try:
        conn = sqlite3.connect(db_path, timeout=10)
        # 모든 매칭 레코드를 backed_up=1로 마크 (ts_end NULL 포함)
        conn.execute('UPDATE recordings SET backed_up=1 WHERE filename=? AND camera_id=?', (filename, camera))
        # stale open 레코드(ts_end IS NULL)가 있으면 ts_end도 닫아줌
        if ts_end:
            conn.execute('UPDATE recordings SET ts_end=? WHERE filename=? AND camera_id=? AND ts_end IS NULL', (ts_end, filename, camera))
        conn.commit()
        changes = conn.total_changes
        conn.close()
        if os.path.exists(filepath):
            os.unlink(filepath)
        print(f'OK changes={changes}')
        sys.exit(0)
    except sqlite3.OperationalError as e:
        if 'locked' in str(e) and attempt < 4:
            time.sleep(2)
            continue
        print(f'FAIL: {e}')
        sys.exit(1)
" >> "$LOG" 2>&1
    return $?
}

log "Backup watcher started"

inotifywait -m -r -e close_write -e moved_to --format "%w%f" "$RECORDINGS_DIR" | while read FILE; do
    if [[ "$FILE" == *.mp4 ]]; then
        REL="${FILE#$RECORDINGS_DIR/}"
        DIR=$(dirname "$REL")

        log "Uploading: $REL"
        rclone copy "$FILE" "$REMOTE/$DIR" --no-traverse --log-level INFO 2>&1 | while read line; do
            log "  rclone: $line"
        done

        if [ ${PIPESTATUS[0]} -eq 0 ]; then
            log "Uploaded: $REL"
            if mark_and_delete "$FILE" "$REL"; then
                log "Deleted local: $REL"
            else
                log "DB mark failed, keeping local: $REL"
            fi
        else
            log "FAILED: $REL"
        fi
    fi
done
