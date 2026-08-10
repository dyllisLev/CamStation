# CamStation 2.0 Docker 카나리 운영 문서

최종 확인: 2026-08-10 06:03 KST

> 화면 캡처와 런타임 검증 원본은 민감한 운영 증거이므로 Git에 넣지 않고, 유지보수
> 워크스페이스의 무시된 `work/20260809-camstation2-docker-canary/` 아래에만 보존한다.

## 바로 접속

- 모바일·무인 뷰어: [http://10.0.0.26:18081/viewer](http://10.0.0.26:18081/viewer)
- 2.0 운영 화면: [http://10.0.0.26:18081/live](http://10.0.0.26:18081/live)
- 상태 API: [http://10.0.0.26:18081/api/health](http://10.0.0.26:18081/api/health)

`/viewer`가 1.0의 `https://cctv2.nuc.hmini.me/viewer`에 대응하는 전용 화면이다.
메뉴, 설정, 타임라인, 배치 편집 기능 없이 저장된 카메라 배치와 영상만 표시한다.
`/live` 또는 `/live?viewer=1`은 관리용 라이브 작업공간이므로 모바일 뷰어 경로로
사용하지 않는다.

이 주소는 `10.0.0.0/24`에 접근 가능한 단말에서만 열린다. 페이지 자체가 열리지 않으면
경로 문제가 아니라 먼저 단말의 10.x 네트워크 연결을 확인한다.

## 현재 운영 상태

| 항목 | 확인값 |
|---|---|
| 서버 | `cctv` / `10.0.0.26` |
| 컨테이너 | `camstation2-canary` — running, healthy, restart 0 |
| 이미지 | `camstation:2.0.0-rc.20260809.7-canary` |
| 이미지 ID | `sha256:628da2dbd0a7bbe94280d45284fe975617e3b8a56e02f8389db4ca84d68202e9` |
| 공개 포트 | `10.0.0.26:18081/tcp` 한 개만 공개 |
| 내부 HTTP | `18080/tcp` |
| go2rtc | API `1984`, RTSP `8554`, WebRTC `8555` 모두 컨테이너 내부 전용 |
| 재시작 정책 | `no` — 시험 중 서버 재부팅 후 수동 시작 |
| PID 정책 | `pids_limit: 1024` — 최종 8대 운용과 일시 재연결 여유를 반영 |
| 권한 | UID/GID `10001:10001`, read-only root, capabilities 없음 |
| 상태 저장소 | root 전용 `.env`가 지정하는 canary 전용 bind mount |
| 녹화 저장소 | root 전용 `.env`가 지정하는 canary 전용 media bind mount |
| 녹화 정리 기준점 | 2026-08-10 06:03 KST 완료 파일 0, 작성 중 3; 녹화 계속 활성 |

2.0 카나리에 들어 있는 카메라는 다음 세 대뿐이다.

- `집-마당`
- `집-창고1`
- `집-창고2`

소방서 전체와 `염소장`은 DB와 생성된 go2rtc 설정에서 제외했다. 이 경계는 이름을
하나씩 제외하는 방식이 아니라 `집-`으로 시작하는 main/sub 세 쌍만 허용하고 정확히
3대가 아니면 이관을 실패시키는 positive allowlist다.

## 확인된 동작

| 검증 | 결과 |
|---|---|
| `/viewer` 직접 요청과 새로고침 | HTTP 200, 경로 유지 |
| iPhone 크기 `393×852` | 문서 overflow 없음, 관리 UI 없음 |
| 그리드 영상 | 3/3 MSE, `readyState=4`, 재생시간 지속 증가 |
| 집중 보기 | 타일 선택 후 단일 1280×720 MSE 재생, 닫기 후 3타일 복귀 |
| 3페이지 동시 부하 | 약 160초 동안 9/9 MSE가 `playing`, 각 live stream의 Viewer가 정확히 3 |
| 연결 회수 | 시험 페이지 종료 직후 세 live stream 모두 Viewer/consumer 0으로 회수 |
| 카메라 API | 정확히 3대, 모두 `streaming` |
| 녹화 워커 | 정확히 3개, 모두 `running` |
| 60초 녹화 재생성 | 세 카메라 H.264 MP4를 ffprobe로 확인 |
| 컨테이너 로그 | URL 인증정보 패턴 0, error/fatal/panic 행 0 |
| PID 원인·조치 | 종전 상한 256 도달·거부 343회 확인 후 1024로 증설; 재시험 peak 230, 거부 0 |
| 3페이지 자원 표본 | CPU 39.37%, 메모리 238.2 MiB / 3 GiB, PID 224, OOM/throttling 0 |
| 1.0 연속성 | 기존 8대 online, 핵심 5개 unit PID/restart 0 유지 |
| 1.0 설정 | 원본 go2rtc YAML SHA-256 불변 |

## 2026-08-10 녹화 정리 이력

운영자 요청으로 2.0 카나리의 완료 녹화만 정리했다. 대상은 Docker inspect로 확인한
카나리 전용 state `/var/lib/camstation2-canary/data`와 media
`/mnt/hdd/camstation2-canary`에 한정했고, 1.0 경로와 서비스는 제외했다.

- 삭제 전: DB `ready` 1,623행과 실제 MP4 1,623개, 9,193,448,264바이트가 정확히 일치했다.
- 무결성: 외부 경로·누락·크기 불일치·추가 MP4는 모두 0이었고, 카메라별 최신 완료본은
  약 60초 H.264/AAC MP4로 ffprobe를 통과했다.
- 삭제 방식: 파일시스템 재귀 삭제가 아니라 `DELETE /api/recordings/segments/{id}`로
  완료 상태인 정확한 ID snapshot만 삭제했다. 동작 중 새로 완료된 9개도 별도 guard 후
  같은 방식으로 정리했다.
- 최종 결과: 완료 영상 1,632개, 총 9,245,386,547바이트 삭제. 기준 시점에 `ready` 0,
  완료 파일 0, `.deleting-*` 0, 작성 중 temp 3개였다.
- 연속성: 작성 중 세 파일이 모두 증가했고 recorder 3개·카메라 3대·컨테이너 health가
  정상, restart 0이었다. 1.0 핵심 5개 unit의 PID와 restart 0도 변하지 않았고, 별도 10초
  표본에서 기존 1.0 녹화 MP4 8개가 같은 inode를 유지하며 모두 증가했다.
- 복구: 삭제 행은 감사용 `deleted` tombstone으로 남지만 1,632개 모두
  `backup_state=pending`이고 완료 백업 시각이 없다. 휴지통이나 격리본도 만들지 않았으므로
  CamStation으로 복구할 수 없다.

녹화 기능은 계속 활성이다. 이 기준점 이후에도 매분 새 완료 파일이 생성되므로, 앞으로
파일을 남기지 않으려면 삭제 반복이 아니라 별도 승인으로 녹화를 비활성화해야 한다.
세부 증거는 [E-020](../work/20260809-cctv-operations/evidence/E-020.md)에 있다.

모바일 비교 증거는
[1.0 `/viewer` 화면](../work/20260809-camstation2-docker-canary/browser/legacy-mobile-viewer.png)과
[2.0 `/viewer` 화면](../work/20260809-camstation2-docker-canary/browser/canary-mobile-viewer-verified.png)에
보존했다. 검증용 브라우저에는 한글 글꼴이 없어 카메라 이름 글리프가 사각형으로 보이지만,
DOM의 실제 이름과 영상 재생 상태는 별도로 검사했다.
[브라우저 검증 기록](../work/20260809-camstation2-docker-canary/browser/verification.md)에
viewport, 재생 상태, 집중 보기와 직접 reload 결과를 함께 정리했다.

## 일상 관리

다음 명령은 유지보수 SSH 키로 `cctv` 서버에 접속한 뒤 실행한다. 배포 디렉터리의
`.env`, Compose 파일과 이관 manifest는 root 전용 mode 0600이다.

```bash
ssh cctv
CANARY_DEPLOY_DIR=$(docker inspect camstation2-canary \
  --format '{{ index .Config.Labels "com.docker.compose.project.working_dir" }}')
test -n "$CANARY_DEPLOY_DIR" && test -d "$CANARY_DEPLOY_DIR"
cd "$CANARY_DEPLOY_DIR"
```

상태와 공개 포트를 확인한다.

```bash
docker compose ps
docker inspect camstation2-canary \
  --format 'status={{.State.Status}} health={{.State.Health.Status}} restarts={{.RestartCount}} image={{.Image}}'
docker port camstation2-canary
curl -fsS http://10.0.0.26:18081/api/health
```

카메라와 녹화 워커는 URL이나 자격증명을 노출하지 않는 public API로 확인한다.

```bash
curl -fsS http://10.0.0.26:18081/api/cameras
curl -fsS http://10.0.0.26:18081/api/recorders/status
```

로그는 서버 안에서 먼저 확인하고, 외부에 전달해야 할 때는 URL을 마스킹한다.

```bash
docker compose logs --since 30m --tail 300 --no-color camstation \
  | sed -E 's#(rtsp|rtsps|http|https)://[^[:space:]"[:cntrl:]]+#<redacted-url>#g'
```

원본 로그, `.env`, DB, 생성된 go2rtc YAML을 메신저나 작업 문서에 첨부하지 않는다.

## 시작·중지·재부팅

현재 `restart: "no"`이므로 CCTV 서버나 Docker가 재부팅되면 자동으로 올라오지 않는다.
시험을 계속할 때만 다음과 같이 수동 시작한다.

```bash
CANARY_DEPLOY_DIR=$(docker inspect camstation2-canary \
  --format '{{ index .Config.Labels "com.docker.compose.project.working_dir" }}')
test -n "$CANARY_DEPLOY_DIR" && test -d "$CANARY_DEPLOY_DIR"
cd "$CANARY_DEPLOY_DIR"
docker compose up -d
docker compose ps
curl -fsS http://10.0.0.26:18081/api/health
```

카나리만 정상 종료하려면 다음을 사용한다.

```bash
docker compose stop camstation
```

컨테이너와 전용 bridge network까지 제거하되 DB와 녹화 bind mount를 보존하려면 다음을
사용한다. 이것이 이번 병행 시험의 즉시 롤백이다. 1.0은 별도 서비스로 계속 운영되므로
nginx나 1.0 unit을 조작하지 않는다.

```bash
docker compose down
```

`docker compose down -v`, `docker system prune`, state/media 디렉터리 삭제는 사용하지 않는다.

## 이미지 업데이트와 이전 이미지 복귀

업데이트는 기존 태그를 덮어쓰지 않고 새 immutable 태그로 이미지를 먼저 적재한다.
그다음 `.env`의 `CAMSTATION_IMAGE` 한 줄만 바꾸고 Compose 검증 후 카나리만 재생성한다.

```bash
cd "$CANARY_DEPLOY_DIR"
cp -p .env ".env.pre-update-$(date +%Y%m%d-%H%M%S).bak"
# CAMSTATION_IMAGE를 새로 검증한 immutable 태그로 변경
docker compose config --quiet
docker compose up -d --no-deps --force-recreate camstation
docker compose ps
```

문제가 생기면 `.env`의 이미지 태그를 직전 값으로 되돌린 뒤 같은 `up` 명령을 실행한다.
현재 Viewer 추가 전 이미지는 `camstation:2.0.0-rc.20260809.6-canary`이며, Viewer 추가 시점의
root 전용 백업은 배포 디렉터리의 `.env.pre-viewer-20260809-2118.bak`이다.
PID 상한 변경 전 Compose 백업은 `compose.yaml.pre-pids-20260809-2134.bak`이며, 현재 운영
정의와 저장소 정의는 모두 `pids_limit: 1024`다.

## 상태 백업

SQLite는 WAL을 사용하므로 실행 중인 `camstation.db` 하나만 복사하면 안 된다. 원시 파일
백업이 필요하면 카나리를 정상 종료한 뒤 state 디렉터리 전체를 함께 보존한다.

```bash
cd "$CANARY_DEPLOY_DIR"
set -a
. ./.env
set +a
: "${CANARY_STATE_DIR:?CANARY_STATE_DIR is required}"
: "${CANARY_BACKUP_FILE:?승인된 백업 파일을 지정해야 합니다}"
docker compose stop camstation
tar -C "$(dirname "$CANARY_STATE_DIR")" -czf "$CANARY_BACKUP_FILE" \
  "$(basename "$CANARY_STATE_DIR")"
docker compose up -d
```

녹화는 `.env`의 `CANARY_MEDIA_DIR`이 가리키는 전용 mount에 있으므로 필요한 보존 범위와
용량을 확인한 뒤 백업한다. 카나리 설정은 백업 schedule 비활성, 미백업 녹화 보호 활성
상태다.

## 장애 판단 순서

### 페이지가 열리지 않음

1. 단말이 `10.0.0.0/24`에 접근 가능한지 확인한다.
2. 주소가 정확히 `http://10.0.0.26:18081/viewer`인지 확인한다. 아직 HTTPS/DNS 경로는 없다.
3. 서버에서 `docker compose ps`, `docker port camstation2-canary`를 확인한다.
4. 재부팅 이후라면 수동 시작 절차를 실행한다.

### 화면은 열리지만 영상이 검음

1. 최초 연결을 10초까지 기다린 뒤 한 번 새로고침한다.
2. `/api/cameras`에서 세 대가 모두 `streaming`인지 확인한다.
3. `/api/recorders/status`와 마스킹된 로그를 확인한다.
4. `docker inspect`에서 PID 상한이 1024인지, `docker stats`에서 PID가 상한에 근접하지
   않는지 확인한다. 앱 로그가 조용해도 task 거부가 있으면 PID 고갈로 분류한다.
5. RTSP, go2rtc API 또는 WebRTC 포트를 추가로 공개하지 않는다. `/viewer`는 HTTP의
   same-origin `/player/api/ws`를 통한 MSE가 정상 경로다.

### 1.0에 영향이 의심됨

1. `docker compose stop camstation`으로 카나리만 중지한다.
2. 1.0 핵심 unit과 기존 `/viewer`의 8대 영상을 재확인한다.
3. 원본 go2rtc YAML 해시가 아래 기준값과 같은지 확인한다.

```text
8c94606e0f99d6ea2574f8163a89fad755004fe31704f94fc4cb2dfbedcee9eb
```

## 알려진 경계

- 이것은 집 카메라 3대의 병행 카나리이며 전체 2.0 전환 완료가 아니다.
- 소방서와 염소장은 한 번도 2.0 카나리 검증 대상으로 포함하지 않았다.
- PID 1024는 전체 8대 운용을 위한 용량 설정이지, 카나리에서 8대를 활성화했다는 뜻이 아니다.
- 외부 진입은 HTTP 한 개뿐이며 direct WebRTC는 공개·검증하지 않았다.
- 백업 원격지, 알림, ONVIF/PTZ, Windows Viewer 2.0 전환은 이번 모바일 Viewer 합격 범위가 아니다.
- 자동 부팅은 의도적으로 꺼져 있다. 정식 전환 때 boot ownership과 안정 주소를 별도로 결정한다.

## 증거 → 결론 → 경로

### E-001 — 1.0 Viewer 기준 화면

- observed_at: 2026-08-09 21:08 KST
- source_type: browser
- source_ref: `https://cctv2.nuc.hmini.me/viewer`, iPhone 15 emulation
- content_hash: `0a652d91068149f5cb1bfcb14ef67d7c6dd255722a49b531fd91075ad245c772`
- finding: 메뉴 없는 전체 배치에서 기존 8개 MSE 영상이 재생됨

### E-002 — 2.0 Viewer 합격 화면

- observed_at: 2026-08-09 21:21 KST
- source_type: browser
- source_ref: `http://10.0.0.26:18081/viewer`, 393×852
- content_hash: `7003f593676bb1c05eb421d5fea7b3fa0cb2d94ee7d58aa128c9f7b9d9114366`
- finding: 집 카메라 3개만 MSE 재생, overflow 없음, 집중 보기/복귀 성공

### E-003 — 이관 원본과 결과

- source YAML SHA-256: `8c94606e0f99d6ea2574f8163a89fad755004fe31704f94fc4cb2dfbedcee9eb`
- secret-safe canonical fingerprint: `039c57724fffb2396296ae100be4c0c3fe1038f72a45b8a9ab2c7973464207b5`
- import manifest SHA-256: `66e06cd602ccacee2edfcbeb2685e25d6f4f3aff1e198b52d7704632b7f3a135`
- finding: 1.0 DB가 아니라 현재 동작 중인 go2rtc YAML의 `집-` 세 쌍만 새 DB로 변환됨

### E-004 — 3페이지 동시 재생과 PID 용량

- title: 3개 모바일 페이지의 9개 MSE 재생과 컨테이너 task 여유
- observed_at: 2026-08-09 21:39 KST
- source_type: browser, command, public API
- source_ref: `/viewer` 3개 페이지와 `camstation2-canary`의 secret-safe 상태 표본
- content_hash: n/a — 브라우저·API·실시간 counter 표본
- repro_command: |
    `agent-browser`로 `/viewer` 탭 세 개를 열어 각 `video`의 `readyState`, `paused`,
    `currentTime`, `data-phase`, `data-transport`를 읽고, 서버에서 `docker inspect`,
    `docker stats`, public stream/recorder API와 root 전용 cgroup counter를 함께 확인한다.
- raw_excerpt: |
    세 탭 모두 영상 3개가 `readyState=4`, `paused=false`, `phase=playing`,
    `transport=mse`; live Viewer `3/3/3`; PID current 225, max 1024, peak 230,
    limit event 0, OOM 0, throttling 0.
- pre-change: `pids.max=256`, `pids.peak=256`, PID-limit 거부 343회
- post-change: `pids.max=1024`, `pids.peak=230`, PID-limit 거부 0회, OOM/throttling 0회
- finding: 9개 MSE가 약 160초 동안 모두 재생되고 각 live stream이 Viewer 3으로 안정됨
- linked_workitem: eight-camera PID capacity correction
- supersedes: none

### F-001 — 전용 `/viewer`가 필요함

- severity: operational
- evidence_ids: E-001, E-002
- finding: `/live`의 반응형 축소나 `viewer=1` query는 1.0 Viewer 계약과 같지 않음
- resolution: 2.0 최상위 `/viewer`에 read-only 화면과 MSE-first 재생 경로를 분리함

### F-002 — 앱 로그가 조용해도 컨테이너 PID 고갈이 연결을 지연시킴

- title: 낮은 컨테이너 PID 상한으로 인한 FFmpeg task 생성 거부
- severity: operational
- category: misconfiguration
- status: validated
- evidence_ids: E-004
- location: `packaging/docker/compose.canary.yaml`의 `pids_limit`
- finding: FFmpeg thread도 컨테이너 PID에 포함되어 256 상한에서 task 생성이 343회 거부됨
- impact: 동시 Viewer 연결 때 MSE 시작 지연과 재시도가 발생할 수 있음
- confidence: high
- repro_steps: 3페이지 부하에서 브라우저 재생 상태와 cgroup PID counter를 같은 시각에 비교
- resolution: 최종 8대와 focus/reconnect 여유를 반영해 1024로 증설하고 3페이지 부하로 재검증함

### P-001 — 모바일 영상 경로

- title: 모바일 Viewer 요청부터 집 카메라 MSE 재생까지
- path_type: callflow
- start: `http://10.0.0.26:18081/viewer`
- goal: positive allowlist의 집 카메라 영상만 read-only MSE로 재생
- steps:
  1. 단말이 `/viewer`를 요청한다. — evidence: E-002 — finding: F-001
  2. `camstationd`가 embedded SPA와 redacted camera/layout API를 제공한다. — evidence: E-002
  3. 각 타일은 same-origin `/player/api/ws`에 MSE로 연결한다. — evidence: E-004 — finding: F-002
  4. 컨테이너 내부 go2rtc가 `집-*-live` 출력 세 개만 제공한다. — evidence: E-003, E-004
  5. grid 타일 선택 시 grid 구독을 내리고 해당 카메라 focus 출력 하나만 재생한다. — evidence: E-002
- residual_risks: HTTP-only 접근, 수동 재시작, 집 카메라 3대에 한정된 실제 부하 검증

잔여 위험은 HTTP-only 접근, 수동 재시작, 집 카메라에 한정된 검증이다. PID 용량은 8대
기준으로 준비했지만 실제 8대 동시 영상 검증을 대신하지 않는다. 이 조건을 해소하기 전에는
본 카나리를 정식 전체 전환으로 취급하지 않는다.
