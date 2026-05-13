# CCTV 유지보수

마지막 정리: 2026-05-13 14:51 KST  
대상 저장소: `dyllisLev/CamStation`  
운영 서버: `cctv` (`10.0.0.26`)

이 문서는 CamStation CCTV 서버의 기본 운영 정보, 배포 절차, 점검 명령, 그리고 2026-05-13 신규 UI 작업 내용을 다음 유지보수 때 바로 참고하기 위한 기준 문서입니다.

---

## 1. 빠른 요약

| 항목 | 값 |
| --- | --- |
| GitHub 저장소 | `https://github.com/dyllisLev/CamStation` |
| 서버 SSH 별칭 | `cctv` |
| 서버 주소 | `10.0.0.26` |
| SSH 사용자 | `root` |
| SSH 포트 | `22` |
| SSH 키 | `~/.ssh/id_ed25519` |
| 운영 경로 | `/opt/camstation` |
| 프론트 운영 경로 | `/opt/camstation/frontend/dist` |
| 릴리즈 보관 경로 | `/opt/camstation/releases/<version>/frontend/dist` |
| 백엔드 포트 | `127.0.0.1:8000` |
| go2rtc 포트 | `127.0.0.1:1984` |
| nginx 포트 | `80` |
| 현재 배포 버전 | `v20260513-29b22a3` |
| 상태 파일 | `/opt/camstation/.current-version` |
| 배포 스크립트 | `/opt/camstation/deploy/deploy.sh` |
| 배포 로그 | `/var/log/camstation-deploy.log` |
| 데이터베이스 | `/opt/camstation/data/camstation.db` |
| 녹화 저장소 | `/opt/camstation/recordings` |
| 임시 녹화 경로 | `/opt/camstation/temp` |

> 주의: `/opt/camstation/.github-token`은 배포 스크립트가 GitHub 릴리즈를 조회할 때 쓰는 토큰 파일입니다. 존재 여부만 확인하고, 내용은 출력하거나 문서화하지 않습니다.

---

## 2. 접속 방법

### 2.1 SSH 접속

```bash
ssh camstation-host
```

현재 로컬 SSH 설정 기준:

```text
Host: 10.0.0.26
User: root
Port: 22
IdentityFile: ~/.ssh/id_ed25519
```

직접 접속 명령이 필요하면:

```bash
ssh -i ~/.ssh/id_ed25519 -p 22 root@10.0.0.26
```

### 2.2 주요 웹 주소

```text
기존 UI: http://10.0.0.26/
신규 UI: http://10.0.0.26/new
헬스체크: http://10.0.0.26/api/system/health
go2rtc 프록시: http://10.0.0.26/go2rtc/
```

### 2.3 바로 상태 확인

```bash
ssh camstation-host "cat /opt/camstation/.current-version"
ssh camstation-host "systemctl is-active camstation-backend nginx go2rtc vstarcam-tls-proxy"
ssh camstation-host "curl -sf http://127.0.0.1:8000/api/system/health"
```

정상 예시:

```text
v20260513-29b22a3
active
active
active
active
{"status":"ok"}
```

---

## 3. 서버 구성

### 3.1 디렉터리 구조

| 경로 | 용도 |
| --- | --- |
| `/opt/camstation` | 운영 배포 루트 |
| `/opt/camstation/backend` | FastAPI 백엔드 코드 |
| `/opt/camstation/backend/.venv` | 백엔드 파이썬 가상환경 |
| `/opt/camstation/frontend/dist` | nginx가 서빙하는 프론트 심링크 |
| `/opt/camstation/releases` | 릴리즈별 프론트 빌드 보관 |
| `/opt/camstation/config/go2rtc.yaml` | go2rtc 카메라 스트림 설정 |
| `/opt/camstation/deploy` | 배포 스크립트, systemd, nginx 설정 |
| `/opt/camstation/data/camstation.db` | SQLite 데이터베이스 |
| `/opt/camstation/recordings` | 녹화 파일 저장소 |
| `/opt/camstation/temp` | 녹화 중 임시 파일 경로 |

현재 프론트 심링크:

```bash
ssh camstation-host "readlink /opt/camstation/frontend/dist"
```

정상 예시:

```text
/opt/camstation/releases/v20260513-29b22a3/frontend/dist
```

### 3.2 systemd 서비스

| 서비스 | 역할 |
| --- | --- |
| `camstation-backend` | FastAPI 백엔드, `127.0.0.1:8000` |
| `nginx` | 외부 HTTP 진입점, 프론트와 API 프록시 |
| `go2rtc` | RTSP/WebRTC 허브, `127.0.0.1:1984` |
| `vstarcam-tls-proxy` | VSTARCAM 443 TLS → RTSP 프록시 |
| `camstation-updater` | `/opt/camstation/deploy/deploy.sh` 실행용 원샷 서비스 |

백엔드 systemd 핵심 환경값:

```text
WorkingDirectory=/opt/camstation/backend
ExecStart=/opt/camstation/backend/.venv/bin/uvicorn main:app --host 127.0.0.1 --port 8000
RECORDINGS_DIR=/opt/camstation/recordings
CAMSTATION_TEMP_DIR=/opt/camstation/temp
CAMSTATION_DB_PATH=/opt/camstation/data/camstation.db
GO2RTC_URL=http://127.0.0.1:1984
GO2RTC_CONFIG=/opt/camstation/config/go2rtc.yaml
```

### 3.3 nginx 라우팅

nginx 설정 파일:

```text
저장소: deploy/nginx/camstation.conf
서버: /etc/nginx/sites-available/camstation
```

주요 라우팅:

```text
/         -> /opt/camstation/frontend/dist/index.html
/assets/  -> 정적 asset 장기 캐시
/api/     -> http://127.0.0.1:8000
/go2rtc/  -> http://127.0.0.1:1984/
```

프론트는 단일 페이지 앱이라 `/new`, `/new/recordings`, `/new/settings`도 nginx의 `try_files ... /index.html`로 진입합니다.

---

## 4. 배포 방법

### 4.1 전체 흐름

1. 로컬 저장소에서 작업 후 `main` 브랜치에 푸시
2. GitHub Actions `Release` 워크플로가 실행됨
3. 워크플로가 프론트를 빌드하고 `frontend-dist.tar.gz`를 릴리즈 asset으로 업로드
4. 서버에서 `/opt/camstation/deploy/deploy.sh` 실행
5. 서버 배포 스크립트가 최신 GitHub Release를 내려받음
6. 백엔드, config, deploy 디렉터리는 `origin/main`에서 갱신
7. 프론트 dist 심링크를 새 릴리즈로 교체
8. systemd/nginx 설정 동기화
9. 서비스 재시작 후 헬스체크
10. 성공 시 `/opt/camstation/.current-version` 갱신

### 4.2 로컬에서 배포 준비

```bash
cd /root/projects/camstation

git status --short -b

# 프론트 검증
cd frontend
npm test -- --run
npx eslint src/pages/new-ui src/hooks/useLayouts.ts src/layoutGrid.ts src/__tests__/layoutGrid.test.ts
npm run build

# 백엔드 layouts 검증
cd /root/projects/camstation
backend/.venv/bin/python -m pytest backend/tests/test_layouts.py -q
```

커밋과 푸시:

```bash
cd /root/projects/camstation
git add <변경파일>
git commit -m "작업 요약"
git push origin main
```

### 4.3 GitHub Actions 확인

```bash
gh run list --repo dyllisLev/CamStation --branch main --limit 3
gh run watch <run-id> --repo dyllisLev/CamStation --exit-status
```

릴리즈 태그 형식:

```text
vYYYYMMDD-<short-sha>
```

예시:

```text
v20260513-29b22a3
```

### 4.4 서버 반영

```bash
ssh camstation-host "bash /opt/camstation/deploy/deploy.sh"
```

성공 로그 예시:

```text
Deployed v20260513-29b22a3 successfully
Old releases cleaned up
```

### 4.5 배포 후 검증

```bash
ssh camstation-host "cat /opt/camstation/.current-version"
ssh camstation-host "systemctl is-active camstation-backend nginx go2rtc vstarcam-tls-proxy"
ssh camstation-host "curl -sf http://127.0.0.1:8000/api/system/health"
```

브라우저 확인:

```text
http://10.0.0.26/
http://10.0.0.26/new
```

---

## 5. 유지보수 점검 명령

### 5.1 서비스 로그

```bash
ssh camstation-host "journalctl -u camstation-backend -n 200 --no-pager"
ssh camstation-host "journalctl -u go2rtc -n 200 --no-pager"
ssh camstation-host "journalctl -u vstarcam-tls-proxy -n 200 --no-pager"
ssh camstation-host "journalctl -u nginx -n 200 --no-pager"
```

### 5.2 배포 로그

```bash
ssh camstation-host "tail -n 200 /var/log/camstation-deploy.log"
```

### 5.3 nginx 설정 검증

```bash
ssh camstation-host "nginx -t"
```

### 5.4 DB 확인

SQLite 직접 확인 전에는 백업을 먼저 권장합니다.

```bash
ssh camstation-host "cp /opt/camstation/data/camstation.db /opt/camstation/data/camstation.db.$(date +%Y%m%d-%H%M%S).bak"
```

layouts 테이블 컬럼 확인:

```bash
ssh camstation-host "python3 - <<'PY'
import sqlite3
con = sqlite3.connect('/opt/camstation/data/camstation.db')
for row in con.execute('pragma table_info(layouts)'):
    print(row)
PY"
```

2026-05-13 기준 `layouts` 테이블에는 다음 컬럼이 있어야 합니다.

```text
id
name
data
timeline_collapsed
created_at
updated_at
grid_cols
grid_rows
```

### 5.5 안전 재시작

```bash
ssh camstation-host "systemctl restart camstation-backend nginx"
ssh camstation-host "curl -sf http://127.0.0.1:8000/api/system/health"
```

`go2rtc.yaml` 또는 VSTARCAM 프록시를 수정한 경우:

```bash
ssh camstation-host "systemctl restart vstarcam-tls-proxy go2rtc camstation-backend nginx"
```

---

## 6. 2026-05-13 신규 UI 작업 정리

### 6.1 목표

- 기존 UI(`/`)는 유지
- 신규 UI는 `/new`와 `/new/*`에서만 접근
- 향후 기존 UI와 교체 가능한 독립 구조로 작성
- 라이브 화면, 녹화 화면, 설정 화면을 신규 디자인으로 구성
- 배포까지 서버에 반영

### 6.2 신규 UI 라우팅

주요 파일:

```text
frontend/src/main.tsx
frontend/src/routes.ts
frontend/src/pages/new-ui/NewCamStation.tsx
frontend/src/pages/new-ui/newCamStation.css
frontend/src/pages/new-ui/newUiUtils.ts
frontend/src/__tests__/routes.test.ts
frontend/src/__tests__/newUiUtils.test.ts
```

라우팅 구조:

```text
/                 -> 기존 App
/viewer/*          -> viewer
/mobile/*          -> mobile
/new               -> 신규 NewCamStation
/new/recordings    -> 신규 녹화 화면
/new/settings      -> 신규 설정 화면
그 외              -> 기존 App
```

핵심 원칙:

- 기존 `App.tsx`, 기존 `LiveView`, `Recordings`, `Settings`는 신규 UI 구현 때문에 교체하지 않음
- `/new` 경로에서만 신규 UI 컴포넌트 사용
- nginx는 그대로 `index.html`로 진입시키고, 클라이언트 라우팅에서 분기

### 6.3 라이브 타임라인 표시 수정

문제:

- 기존 배치의 `timeline_collapsed=true` 값이 신규 UI 라이브 화면에도 적용되어 타임라인이 기본으로 접혀 보일 수 있었음

해결:

- 신규 UI 전용 localStorage 키 사용

```text
camstation-new-live-timeline-collapsed
```

- 신규 UI 라이브 타임라인은 기본 펼침
- 상단 헤더와 하단 타임라인 모두에서 `타임라인 보기/숨기기` 가능
- 기존 UI의 배치 저장값과 신규 UI 표시 상태를 분리

관련 함수:

```text
readNewLiveTimelineCollapsedPreference
getTimelineToggleLabel
```

### 6.4 라이브 그리드 영역 제한

문제:

- 카메라 타일이 리사이즈되면서 하단 타임라인 영역을 침범할 수 있었음

해결:

- React Grid Layout에 영역 제한 적용
- `maxRows`, `isBounded`, `autoSize=false` 사용
- 라이브뷰 실제 높이에 맞춰 rowHeight 계산
- bounds 초과 리사이즈는 마지막 정상 배치로 되돌림
- `.new-grid-stage`에 `overflow: hidden` 적용

관련 함수:

```text
calculateLiveGridRowHeight
calculateGridRowsPixelHeight
layoutFitsWithinGridRows
clampLayoutToGridBounds
getBoundedLayoutOrFallback
```

### 6.5 카메라 크기 조절 세밀화

문제:

- React Grid Layout은 정수 그리드 단위로만 리사이즈됨
- 기존 신규 UI의 12×12 그리드는 한 칸 크기가 커서 약간 움직여도 변경되지 않거나, 변경될 때 한 번에 너무 크게 줄어드는 느낌이 있었음

해결:

- 신규 UI 라이브 그리드를 12×12에서 48×48로 변경

```text
GRID_COLS = 48
LIVE_GRID_MAX_ROWS = 48
LIVE_GRID_MARGIN = [4, 4]
```

효과:

- 가로 조절 단위가 화면 폭의 1/12에서 1/48로 세밀해짐
- 세로 조절도 48행 기준으로 더 부드럽게 반응
- 기존 배치는 비율 유지 상태로 48×48에 자동 변환

추가 파일:

```text
frontend/src/layoutGrid.ts
frontend/src/__tests__/layoutGrid.test.ts
```

주요 함수:

```text
inferGridRows
scaleLayoutGridResolution
```

### 6.6 배치 저장 메타데이터 추가

문제:

- 기존 layouts API는 `x`, `y`, `w`, `h`만 저장하고 그리드 해상도 정보를 저장하지 않았음
- 12칸 배치와 48칸 배치를 같은 숫자로 해석하면 화면 비율이 깨질 수 있음

해결:

- layouts API와 DB에 그리드 해상도 메타데이터 추가

```text
grid_cols INTEGER NOT NULL DEFAULT 12
grid_rows INTEGER
```

변경 파일:

```text
backend/database.py
backend/models.py
backend/routers/layouts.py
backend/tests/test_layouts.py
frontend/src/types/index.ts
frontend/src/api/client.ts
frontend/src/hooks/useLayouts.ts
```

동작 방식:

- 신규 UI에서 저장하면 `grid_cols=48`, `grid_rows=48` 저장
- 메타데이터 없는 기존 배치는 `grid_cols=12`로 간주
- `grid_rows`가 없으면 저장된 layout의 최대 `y+h`로 행 수 추론
- 기존 UI가 48칸 신규 배치를 읽으면 가로는 12칸 기준으로 축소하고, 세로 행 해상도는 유지해서 최대한 깨지지 않게 표시

### 6.7 배포된 작업 이력

최근 관련 커밋:

```text
29b22a3 Make new UI camera resizing finer
fad31ed Constrain new live grid bounds
5eeecc1 Fix new UI live timeline visibility
2097d14 Add new CamStation UI route
```

현재 운영 배포:

```text
v20260513-29b22a3
```

---

## 7. 검증 이력

2026-05-13 작업 후 수행한 검증:

```bash
# 프론트 전체 테스트
npm test -- --run
# 결과: 24 passed

# 신규 UI 관련 eslint
npx eslint src/pages/new-ui src/hooks/useLayouts.ts src/layoutGrid.ts src/__tests__/layoutGrid.test.ts
# 결과: 통과

# 프론트 빌드
npm run build
# 결과: 성공

# 백엔드 layouts 테스트
backend/.venv/bin/python -m pytest backend/tests/test_layouts.py -q
# 결과: 10 passed
```

서버 검증:

```text
camstation-backend: active
nginx: active
go2rtc: active
vstarcam-tls-proxy: active
health: {"status":"ok"}
현재 버전: v20260513-29b22a3
```

브라우저 확인:

```text
http://10.0.0.26/     기존 UI 정상 접근
http://10.0.0.26/new  신규 UI 정상 접근
```

참고:

- 백엔드 전체 테스트에서는 기존 카메라 mock의 온라인 상태 기대값과 실제 구현 차이로 `test_get_cameras_returns_list` 1건이 실패한 이력이 있음
- 이번 작업 범위인 layouts API/DB 테스트는 통과

---

## 8. 다음 작업 시 주의사항

1. 기존 UI와 신규 UI는 아직 공존 상태입니다. `/new` 관련 작업이라도 `/` 동작 확인을 같이 합니다.
2. 배치 저장 구조를 건드릴 때는 `grid_cols`, `grid_rows` 호환성을 반드시 고려합니다.
3. DB 변경은 `init_db()`의 additive migration 방식으로 처리합니다.
4. 배포 전 최소 검증:

```bash
cd /root/projects/camstation/frontend
npm test -- --run
npm run build

cd /root/projects/camstation
backend/.venv/bin/python -m pytest backend/tests/test_layouts.py -q
```

5. 배포 후 최소 검증:

```bash
ssh camstation-host "cat /opt/camstation/.current-version"
ssh camstation-host "systemctl is-active camstation-backend nginx go2rtc vstarcam-tls-proxy"
ssh camstation-host "curl -sf http://127.0.0.1:8000/api/system/health"
```

6. 비밀값은 문서화하지 않습니다. 특히 `/opt/camstation/.github-token` 내용은 출력하지 않습니다.
7. 라이브 카메라 그리드 작업 시 React Grid Layout은 정수 단위 기반이라는 점을 기억합니다. 더 세밀한 조정이 필요하면 grid 해상도를 올리고 저장 메타데이터 변환을 함께 수정해야 합니다.
