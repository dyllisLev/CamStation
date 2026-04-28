#!/bin/bash
# deploy/install-updater.sh
# 로컬에서 실행: ./deploy/install-updater.sh
set -euo pipefail

SERVER="cctv"
INSTALL_DIR="/opt/camstation"
GITHUB_REPO="dyllisLev/CamStation"
GITHUB_REMOTE="https://github.com/$GITHUB_REPO.git"

echo "=== CamStation 배포 환경 초기화 ==="
echo "서버: $SERVER"
echo ""

# 1. 서버에 deploy 디렉토리 생성 및 스크립트 복사
echo "[1/5] 배포 스크립트 복사..."
ssh "$SERVER" "mkdir -p $INSTALL_DIR/deploy $INSTALL_DIR/releases"
scp deploy/deploy.sh deploy/health-check.sh "$SERVER:$INSTALL_DIR/deploy/"
ssh "$SERVER" "chmod +x $INSTALL_DIR/deploy/deploy.sh $INSTALL_DIR/deploy/health-check.sh"

# 2. 서버 git repo에 GitHub remote 추가
echo "[2/5] GitHub remote 설정..."
ssh "$SERVER" "
  cd $INSTALL_DIR
  git remote add origin $GITHUB_REMOTE 2>/dev/null || git remote set-url origin $GITHUB_REMOTE
  git fetch origin
  echo 'Remote added: $GITHUB_REMOTE'
"

# 3. dist 디렉토리를 심링크 구조로 마이그레이션
echo "[3/5] frontend/dist 심링크 구조로 마이그레이션..."
ssh "$SERVER" "
  DIST_PATH=\"$INSTALL_DIR/frontend/dist\"
  VERSION_FILE=\"$INSTALL_DIR/.current-version\"
  if [ ! -L \"\$DIST_PATH\" ]; then
    CURRENT_TAG=\$(cat \"\$VERSION_FILE\" 2>/dev/null || echo 'v00000000-initial')
    RELEASE_DIR=\"$INSTALL_DIR/releases/\$CURRENT_TAG\"
    mkdir -p \"\$RELEASE_DIR/frontend\"
    cp -r \"\$DIST_PATH\" \"\$RELEASE_DIR/frontend/dist\"
    rm -rf \"\$DIST_PATH\"
    ln -sfn \"\$RELEASE_DIR/frontend/dist\" \"\$DIST_PATH\"
    echo \"\$CURRENT_TAG\" > \"\$VERSION_FILE\"
    echo 'Migrated to symlink: '\$RELEASE_DIR
  else
    echo 'Already a symlink, skipping migration'
  fi
"

# 4. main.py에 system 라우터 등록
echo "[4/5] main.py에 system 라우터 등록..."
ssh "$SERVER" "
  MAIN=\"$INSTALL_DIR/backend/main.py\"
  if ! grep -q 'system' \"\$MAIN\"; then
    sed -i 's/from routers import /from routers import system, /' \"\$MAIN\"
    sed -i 's/layouts\.router\]/layouts.router, system.router]/' \"\$MAIN\"
    echo 'system router registered in main.py'
  else
    echo 'system router already in main.py, skipping'
  fi
"

# 5. systemd 유닛 설치
echo "[5/5] systemd 유닛 설치..."
scp deploy/systemd/camstation-updater.service \
    deploy/systemd/camstation-updater.timer \
    "$SERVER:/etc/systemd/system/"

ssh "$SERVER" "
  systemctl daemon-reload
  systemctl enable camstation-updater.timer
  systemctl start camstation-updater.timer
  echo 'Timer status:'
  systemctl is-active camstation-updater.timer
"

echo ""
echo "=== 완료 ==="
echo ""
echo "다음 단계: 서버에 GitHub 토큰 설정"
echo "  ssh $SERVER"
echo "  echo 'YOUR_PAT_TOKEN' > $INSTALL_DIR/.github-token"
echo "  chmod 600 $INSTALL_DIR/.github-token"
echo ""
echo "수동 배포 테스트:"
echo "  ssh $SERVER '$INSTALL_DIR/deploy/deploy.sh'"
