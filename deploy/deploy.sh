#!/bin/bash
# deploy/deploy.sh
set -euo pipefail

INSTALL_DIR="/opt/camstation"
RELEASES_DIR="$INSTALL_DIR/releases"
GITHUB_REPO="dyllisLev/CamStation"
TOKEN_FILE="$INSTALL_DIR/.github-token"
VERSION_FILE="$INSTALL_DIR/.current-version"
LOG_FILE="/var/log/camstation-deploy.log"
# 백엔드에 직접 접속 (nginx 우회) - nginx 재시작 중에도 backend 상태를 정확히 확인
HEALTH_URL="http://127.0.0.1:8000/api/system/health"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"; }

# 토큰 읽기
if [ ! -f "$TOKEN_FILE" ]; then
  log "ERROR: GitHub token not found at $TOKEN_FILE"
  exit 1
fi
GITHUB_TOKEN=$(cat "$TOKEN_FILE")

# 최신 릴리즈 태그 확인
LATEST=$(curl -sf \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | \
  python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])")

if [ -z "$LATEST" ]; then
  log "ERROR: Could not fetch latest release tag"
  exit 1
fi

# 현재 버전과 비교
CURRENT=$(cat "$VERSION_FILE" 2>/dev/null || echo "")
if [ "$LATEST" = "$CURRENT" ]; then
  log "Already at latest: $LATEST"
  exit 0
fi

log "Updating: $CURRENT → $LATEST"

# 스테이징 디렉토리 준비
RELEASE_DIR="$RELEASES_DIR/$LATEST"
mkdir -p "$RELEASE_DIR/frontend"

# frontend-dist.tar.gz 다운로드
ASSET_URL=$(curl -sf \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | \
  python3 -c "
import sys, json
r = json.load(sys.stdin)
assets = [a for a in r['assets'] if a['name'] == 'frontend-dist.tar.gz']
print(assets[0]['url'])
")

curl -sfL \
  -H "Authorization: token $GITHUB_TOKEN" \
  -H "Accept: application/octet-stream" \
  "$ASSET_URL" -o "$RELEASE_DIR/frontend-dist.tar.gz"

tar -xzf "$RELEASE_DIR/frontend-dist.tar.gz" -C "$RELEASE_DIR/frontend"
rm "$RELEASE_DIR/frontend-dist.tar.gz"
log "Frontend artifact extracted to $RELEASE_DIR/frontend/dist"

# 백엔드 + 배포 스크립트 업데이트 (tracked 파일만)
cd "$INSTALL_DIR"
git fetch origin 2>&1 | tee -a "$LOG_FILE"
git checkout origin/main -- \
  backend/config.py backend/database.py backend/main.py backend/models.py backend/requirements.txt \
  backend/routers/ backend/services/ backend/tests/ \
  config/ deploy/ 2>&1 | tee -a "$LOG_FILE"
log "Backend, config, and deploy scripts updated"

# 롤백을 위해 이전 심링크 대상 저장
PREV_DIST=$(readlink "$INSTALL_DIR/frontend/dist" 2>/dev/null || echo "")
PREV_VERSION="$CURRENT"

# 아토믹 심링크 교체
ln -sfn "$RELEASE_DIR/frontend/dist" "$INSTALL_DIR/frontend/dist"
log "Symlink swapped to $RELEASE_DIR/frontend/dist"

# systemd 서비스 파일 동기화
for svc in "$INSTALL_DIR/deploy/systemd/"*.service; do
  name=$(basename "$svc")
  dest="/etc/systemd/system/$name"
  if ! diff -q "$svc" "$dest" > /dev/null 2>&1; then
    cp "$svc" "$dest"
    log "Updated systemd unit: $name"
  fi
done
systemctl daemon-reload

# nginx 설정 동기화
NGINX_SRC="$INSTALL_DIR/deploy/nginx/camstation.conf"
NGINX_AVAIL="/etc/nginx/sites-available/camstation"
NGINX_ENABLED="/etc/nginx/sites-enabled/camstation"
if ! diff -q "$NGINX_SRC" "$NGINX_AVAIL" > /dev/null 2>&1; then
  cp "$NGINX_SRC" "$NGINX_AVAIL"
  ln -sf "$NGINX_AVAIL" "$NGINX_ENABLED"
  nginx -t 2>&1 | tee -a "$LOG_FILE"
  log "Updated nginx config"
fi

# vstarcam-tls-proxy 재시작 (proxy 스크립트 변경 시)
systemctl enable vstarcam-tls-proxy 2>/dev/null || true
systemctl restart vstarcam-tls-proxy 2>&1 | tee -a "$LOG_FILE" || true

# go2rtc config 반영
systemctl restart go2rtc 2>&1 | tee -a "$LOG_FILE" || true

# 서비스 재시작
if ! systemctl restart camstation-backend nginx 2>&1 | tee -a "$LOG_FILE"; then
  log "ERROR: Service restart failed, rolling back..."
  [ -n "$PREV_DIST" ] && ln -sfn "$PREV_DIST" "$INSTALL_DIR/frontend/dist"
  systemctl restart camstation-backend nginx || true
  exit 1
fi

# 헬스체크 (30초 타임아웃, 2초 간격)
TIMEOUT=30
ELAPSED=0
while [ "$ELAPSED" -lt "$TIMEOUT" ]; do
  if curl -sf "$HEALTH_URL" > /dev/null 2>&1; then
    break
  fi
  sleep 2
  ELAPSED=$((ELAPSED + 2))
done

if [ "$ELAPSED" -ge "$TIMEOUT" ]; then
  log "ERROR: Health check failed after ${TIMEOUT}s, rolling back..."
  [ -n "$PREV_DIST" ] && ln -sfn "$PREV_DIST" "$INSTALL_DIR/frontend/dist"
  [ -n "$PREV_VERSION" ] && echo "$PREV_VERSION" > "$VERSION_FILE"
  systemctl restart camstation-backend nginx || true
  exit 1
fi

# 버전 기록
echo "$LATEST" > "$VERSION_FILE"
log "Deployed $LATEST successfully"

# 오래된 릴리즈 정리 (최근 2개만 보관)
ls -dt "$RELEASES_DIR"/v* 2>/dev/null | tail -n +3 | xargs rm -rf 2>/dev/null || true
log "Old releases cleaned up"
