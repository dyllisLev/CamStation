#!/bin/bash
# 뷰어 앱 서버 배포 스크립트
# 사용법: ./deploy/deploy-viewer.sh <버전> <EXE경로>
# 예시:   ./deploy/deploy-viewer.sh 1.0.0 "viewer-app/dist/CamViewer 1.0.0.exe"
set -e

VERSION="${1:?버전을 지정하세요 (예: 1.0.0)}"
EXE="${2:?EXE 경로를 지정하세요}"

if [ ! -f "$EXE" ]; then
  echo "오류: EXE 파일을 찾을 수 없습니다: $EXE"
  exit 1
fi

ssh camstation-host "mkdir -p /opt/camstation/viewer"
scp "$EXE" cctv:/opt/camstation/viewer/CamViewer.exe
echo "$VERSION" | ssh camstation-host "cat > /opt/camstation/viewer/version.txt"
echo "✓ CamViewer v$VERSION 배포 완료"
