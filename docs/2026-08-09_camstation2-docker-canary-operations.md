# CamStation 2.0 Docker 카나리 운영 문서

최종 확인: 2026-08-12 11:54 KST

> 화면 캡처와 런타임 검증 원본은 민감한 운영 증거이므로 Git에 넣지 않고, 유지보수
> 워크스페이스의 무시된 `work/20260809-camstation2-docker-canary/`와
> `work/20260810-viewer-download/`, `work/20260810-viewer-registry/`,
> `work/20260812-docker-webrtc/` 아래에만 보존한다.

## 바로 접속

- 모바일·무인 뷰어: [http://10.0.0.26:18081/viewer](http://10.0.0.26:18081/viewer)
- 2.0 운영 화면: [http://10.0.0.26:18081/live](http://10.0.0.26:18081/live)
- 설정·Windows 설치파일: [http://10.0.0.26:18081/settings](http://10.0.0.26:18081/settings)
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
| 이미지 | `camstation:2.0.0-rc.20260812.11-canary` |
| 이미지 ID | `sha256:b4e5fe10099bcd167c34925ac178d2951d2ad01c120e0af77858365dcae5259a` |
| 소스 revision | `dd619b5990b4f05a2b6b56a969acdffd39c97f40` |
| 공개 포트 | HTTP `10.0.0.26:18081/tcp`, WebRTC `10.0.0.26:18555/tcp+udp` |
| 내부 HTTP | `18080/tcp` |
| go2rtc | API `1984`와 RTSP `8554`는 내부 전용; WebRTC `8555`만 host `18555/tcp+udp`에 매핑 |
| WebRTC candidate | `10.0.0.26:18555` 한 개만 광고; Docker bridge 주소는 광고하지 않음 |
| 재생 로그 | 현재 canary `debug`; 운영 기본값 `info`, `off/error/warn/info/debug` 지원 |
| 재시작 정책 | `no` — 시험 중 서버 재부팅 후 수동 시작 |
| PID 정책 | `pids_limit: 1024` — 최종 8대 운용과 일시 재연결 여유를 반영 |
| 권한 | UID/GID `10001:10001`, read-only root, capabilities 없음 |
| 상태 저장소 | root 전용 `.env`가 지정하는 canary 전용 bind mount |
| 녹화 저장소 | root 전용 `.env`가 지정하는 canary 전용 media bind mount |
| 녹화 정리 기준점 | 2026-08-10 06:03 KST 완료 파일 0, 작성 중 3; 녹화 계속 활성 |

## Windows Viewer 수동 설치 시험

설정 페이지의 `Windows 모니터링 클라이언트` 카드가 현재 수동 설치의 단일 진입점이다.

| 항목 | 확인값 |
|---|---|
| 설치파일 | `CamStationViewer.msi` |
| 제품 버전 | `2.0.24` |
| 파일 크기 | `124436480` bytes (`118.7 MB` 표시) |
| SHA-256 | `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160` |
| 서명 정책 | 내부 시험용 미서명 개발 빌드 |
| 서버 주소 입력값 | `http://10.0.0.26:18081` |

운영자가 수행할 수락 순서는 다음과 같다.

1. `http://10.0.0.26:18081/settings`를 열고 `Windows 설치 파일 다운로드`를 선택한다.
2. 내려받은 파일명이 `CamStationViewer.msi`인지 확인하고 Windows Installer로 설치한다.
3. 설치된 `CamStation Viewer`를 실행한다. 서버 주소에는 위 표의 HTTP 주소를 입력하고,
   Viewer 이름에는 해당 모니터링 PC를 식별할 이름을 입력한다.
4. 연결 후 카나리에 허용된 집 카메라 3대의 화면과 영상 진행을 확인한다.
5. 시험 실패 시 Windows의 `설치된 앱`에서 `CamStation Viewer 2.0.24`만 제거한다. 서버
   Docker나 카메라 설정을 함께 롤백하지 않는다.

이 MSI는 아직 Authenticode 서명이 없는 내부 개발 빌드다. 운영 배포로 표현하거나 외부에
배포하지 않는다. 설치 전 Windows가 알 수 없는 게시자 경고를 보일 수 있으며, 승인된 시험
PC에서만 사용한다.

2.0.24는 2026-08-10 16:39 KST에 이전 2.0.21을 보존한 채 원자적으로 게시했다. 16:45 KST
실제 설정 카드에 버전·파일명·크기·해시 접두사·다운로드 버튼이 표시됐고, 독립 HTTP
다운로드는 위 크기와 SHA-256에 일치했다. `.10-canary` 컨테이너 재생성 뒤에도 같은
metadata와 artifact를 다시 검증했다. 2.0.21 당시 증거는 Git에서 제외된
`work/20260810-viewer-download/`에 보존하며, 현재 게시 기준은 이 문서의 2.0.24 값이다.

WIN11-DELL은 2.0.21 clean-host 시험을 마친 뒤, 원격 제어 수락용 2.0.24를 설치했다.
2026-08-12 11:41~11:50 KST에는 저장된 canary 주소로 실제 Viewer를 실행해 세 카메라가
모두 첫 WebRTC attempt에서 재생되는 것을 확인했다. `CamStationViewerService`와 기존
interactive session은 유지했고, GUI 증거 수집용 일회성 작업과 worker는 모두 정리했다.
개발 소스·도구와 해시가 일치하는 2.0.24 원본 MSI도 그대로 보존했다.

증거·결론·수동 수락 경로를 한 문서에서 인계하려면
[Viewer 설정 다운로드 보고서](2026-08-10_viewer-settings-download-report.md)를 사용한다.

## Viewer 등록과 삭제

`/viewers`의 Viewer 레코드는 설치된 Viewer Agent가 주기적으로 보내는 하트비트로 자동
등록된다. 운영 화면에는 수동 등록 폼이 없다. 과거 화면의 `QA Viewer`/`viewer-qa-01`은
미리 채워진 시험 폼이 만든 합성 레코드였으며 Windows 설치나 연결 증거가 아니었다. 이 행은
2026-08-10 11:33 KST에 삭제됐고 실제 Viewer 행은 유지됐다.

Viewer를 정리할 때는 다음 순서를 사용한다.

1. Windows에서 해당 Viewer를 정상 종료하고 필요하면 Viewer 서비스를 중지한다.
2. 마지막 하트비트 후 최대 30초를 기다린 다음 `/viewers`에서 `새로고침`을 선택한다.
3. Agent가 `오프라인`인 정확한 Viewer를 선택한다. 온라인 상태에서는 삭제 버튼이 비활성이다.
4. `오프라인 Viewer 삭제`와 `삭제 확인`을 차례로 선택한다.
5. 목록을 다시 새로고침해 정확한 ID만 사라졌는지 확인한다.

선택 이후 하트비트가 다시 들어오는 경합에서는 서버가 HTTP 409와
`viewer_not_offline`을 반환한다. 이때 강제로 DB를 지우지 말고 Viewer 프로세스·서비스가
실제로 종료됐는지 확인한 뒤 다시 기다린다. Viewer ID가 아닌 표시명만 보고 삭제하지 않는다.

## Viewer 원격 제어

`/viewers`는 상태를 받는 모니터링 계층과 명령을 실행하는 제어 계층을 나눠 보여준다.
Agent/Viewer/Renderer 상태가 과거 값으로 남아 있더라도 Agent 하트비트와 제어 연결이
오프라인이면 새 명령은 보낼 수 없다. 전달만으로 성공 처리하지 않고, Viewer Service가
실행 결과를 서버에 되돌려 최종 상태가 된 경우에만 성공으로 표시한다.

사용 순서는 다음과 같다.

1. `Viewer 레지스트리`에서 실제 Viewer 행의 `선택`을 누른다.
2. `Viewer 원격 제어`의 대상이 선택한 Viewer인지 확인한다.
3. 아래 고정 기능 중 하나를 고르고, 카메라 재연결이면 대상 카메라도 선택한다.
4. 파괴적 재시작 기능은 화면의 추가 확인을 거친 뒤 `기능 실행`을 누른다.
5. 명령 표에서 `대기 → 전달 → 확인 → 실행 → 성공/실패` 시각과 결과를 확인한다.

| 화면 기능 | 명령 | 사용 목적 |
|---|---|---|
| 제어 연결 확인 | `ping` | Viewer Service가 서버 명령을 실제 처리하는지 확인 |
| 라이브 화면 새로고침 | `reload_live` | 승인된 Viewer 라이브 화면만 다시 로드 |
| 카메라 영상 다시 연결 | `resubscribe_stream` | 선택한 카메라의 재생 구독만 다시 생성 |
| Viewer 앱 시작 또는 다시 시작 | `restart_viewer` | Viewer 프로세스와 새 renderer 준비 상태까지 복구 |
| Viewer 관리 서비스 다시 시작 | `restart_service` | Windows Service와 서버 제어 연결을 다시 수립 |

2026-08-10 WinPC 수락에서는 다섯 명령 모두 실제로 `succeeded`까지 확인했다. 이번 Docker
배포 후에는 동일한 다섯 옵션과 명령 이력·상태 표시를 실제 `/viewers`에서 다시 확인했다.
당시 정리된 WinPC가 서버 미설정 상태였으므로 실행 버튼이 비활성인 것을 확인했고, 배포
검증을 위해 오프라인 Viewer에 새 명령을 억지로 만들지 않았다. 이후 2026-08-12 WebRTC
수락에서는 canary에 다시 연결했지만 원격 제어 명령은 실행하지 않았다.

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
| `/live` WebRTC | 3/3 첫 attempt 성공, 2.917~4.352초, retry/MSE/stream fallback 0 |
| Windows Viewer WebRTC | 3/3 첫 attempt 성공, 3.209~4.210초, retry/MSE/stream fallback 0 |
| 재생 진단 로그 | 브라우저·Windows 구조화 event 36건, 금지 URL/SDP/ICE/자격증명 패턴 0 |
| 집중 보기 | 타일 선택 후 단일 1280×720 MSE 재생, 닫기 후 3타일 복귀 |
| 3페이지 동시 부하 | 약 160초 동안 9/9 MSE가 `playing`, 각 live stream의 Viewer가 정확히 3 |
| 연결 회수 | 시험 페이지 종료 직후 세 live stream 모두 Viewer/consumer 0으로 회수 |
| 카메라 API | 정확히 3대, 모두 `streaming` |
| 녹화 워커 | 정확히 3개, 모두 `running` |
| Viewer 원격 제어 | 실제 Viewer 선택, 고정 기능 5개 표시, 오프라인 대상 실행 차단 |
| Viewer 설치파일 | 2.0.24 metadata/크기/SHA-256 및 컨테이너 재생성 후 다운로드 일치 |
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
현재 WebRTC 배포의 즉시 복귀 이미지는
`camstation:2.0.0-rc.20260810.10-canary`, image ID
`sha256:19954a0ff6a2ea89a7453ce2af0975d03e7c52f9e26cc3ca4f227e9ce8c1ccc9`다.
root 전용 포인터 백업은 `.env.pre-webrtc-20260812-113427.bak`, Compose 백업은
`compose.yaml.pre-webrtc-20260812-113427.bak`이다. 그 이전 Viewer 원격 제어 배포의
직전 이미지는
`camstation:2.0.0-rc.20260810.9-canary`이고, root 전용 이미지 포인터 백업은 배포
디렉터리의 `.env.pre-viewer-controls-20260810-074056.bak`이다. Viewer 레지스트리 변경의
직전 이미지는 `camstation:2.0.0-rc.20260810.8-canary`이고 당시 백업은
`.env.pre-viewer-registry-20260810-024232.bak`이다. MSI 다운로드 변경의
직전 이미지는 `camstation:2.0.0-rc.20260809.7-canary`이고 당시 백업은
`.env.pre-msi-download-20260810-111413.bak`이다. 더 이전 Viewer 추가 전 이미지는
`camstation:2.0.0-rc.20260809.6-canary`이며 당시 백업은
`.env.pre-viewer-20260809-2118.bak`이다.
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

1. `/live`와 설치형 Windows Viewer는 첫 WebRTC attempt가 5초 안에 재생돼야 한다. 화면에
   `영상 재연결 중`이 나오거나 MSE로 넘어가면 정상 초기화로 간주하지 말고 실패로 조사한다.
2. host의 `10.0.0.26:18555/tcp+udp` publish와 생성 설정의 candidate
   `10.0.0.26:18555`가 일치하는지 확인한다. API `1984`와 RTSP `8554`는 공개하지 않는다.
3. `/api/cameras`에서 세 대가 모두 `streaming`인지 확인한다.
4. `/api/recorders/status`와 `playback_event` 구조화 로그를 확인한다. 현재 debug는 signaling,
   첫 track과 첫 media까지 남기며, 평시에는 `.env`의 `CAMSTATION_PLAYBACK_LOG_LEVEL=info`로
   attempt 시작·성공·실패만 보존할 수 있다.
5. `docker inspect`에서 PID 상한이 1024인지, `docker stats`에서 PID가 상한에 근접하지
   않는지 확인한다. 앱 로그가 조용해도 task 거부가 있으면 PID 고갈로 분류한다.
6. 전용 `/viewer`는 저사양·호환용 MSE-first 화면이므로 HTTP의 same-origin
   `/player/api/ws`가 정상 경로다. `/live`나 설치형 Viewer의 WebRTC-first 결과와 혼동하지 않는다.

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
- 외부 진입은 관리망에 bind한 HTTP와 WebRTC `18555/tcp+udp`뿐이다. direct WebRTC는
  브라우저와 Windows Viewer에서 검증했지만 인터넷 공개·HTTPS/TLS 환경은 검증하지 않았다.
- 백업 원격지, 알림, ONVIF/PTZ와 Windows Viewer 운영 전환은 아직 완료되지 않았다. Viewer
  2.0.24 설치파일과 원격 제어 코드는 게시됐고 WinPC 실시간 연결도 확인했지만, 장기 soak와
  정식 Authenticode 서명은 별도 수락 작업이다.
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

### E-005 — Viewer 합성 등록 제거와 삭제 상태 정합성

- observed_at: 2026-08-10 11:27-11:44 KST
- source_type: browser, public API, Docker/systemd inspection, automated tests
- source_ref: `/viewers`, `/api/viewers`, immutable image labels, ignored
  `work/20260810-viewer-registry/` screenshots
- content_hash: 수정 전 충돌 화면
  `31559b85d4a321f8d57767610d79fe67bda7bc86a0f50a5439a6e79079afacdd`, 수정 후 화면
  `ddf77e131e6eb6e8c096918751460470372d8cbb1e12aca9163efd4eaf3d590f`
- raw_excerpt: 수정 전 정확한 `DELETE /api/viewers/viewer-qa-01`은 최근 하트비트 동안 409였고
  화면은 `validation`을 표시했다. 수정 후 목록은 실제 Viewer 1대만 포함하고 QA 폼·행은
  없으며, 온라인 Viewer 삭제 버튼은 비활성이고 30초 안내가 표시됐다.
- continuity: `.9` image healthy/restart 0, enabled cameras 3, running recorders 3, legacy 1.0
  five-unit PID/restart baseline unchanged
- finding: 합성 QA 행을 실제 설치로 오인하게 한 등록 경로와 서버/UI 삭제 조건 불일치가 제거됨

### E-006 — Viewer 원격 제어 Docker 배포

- observed_at: 2026-08-10 16:39-16:47 KST
- source_type: immutable image, public API, browser, Docker/systemd inspection
- source_ref: `/viewers`, `/settings`, `/viewer`, image labels, release catalog
- content_hash: n/a — 민감한 transient 화면은 직접 검사 후 삭제
- image: `camstation:2.0.0-rc.20260810.10-canary`,
  `sha256:19954a0ff6a2ea89a7453ce2af0975d03e7c52f9e26cc3ca4f227e9ce8c1ccc9`
- source_revision: `f9f43b7bafa6157b8d3fd32562f378f060689c26`
- raw_excerpt: Viewer 1대 선택, 명령 옵션 5개, 합성 등록 UI 없음, 오프라인 실행 차단;
  `/settings` 2.0.24 표시와 124,436,480-byte artifact SHA-256 일치; `/viewer` 3/3 MSE
  `playing`, `readyState=4`.
- continuity: container healthy/restart 0, mount/port/security fingerprint 불변, camera 3/3
  streaming, recorder 3/3 running, 새 로그 error/fatal/panic/credential 패턴 0, legacy 1.0
  five-unit PID/restart baseline unchanged
- rollback: `.9-canary` 이미지와 root 전용
  `.env.pre-viewer-controls-20260810-074056.bak` 보존
- finding: 병합된 Viewer 제어와 최신 검증 MSI가 기존 canary 격리·녹화·1.0 연속성을
  바꾸지 않고 실제 운영 화면에 반영됨

### E-007 — Docker direct WebRTC와 재생 진단

- observed_at: 2026-08-12 11:34-11:50 KST
- source_type: immutable image, generated config, Docker port inspection, browser telemetry,
  Windows interactive GUI capture, public API, recorder growth, systemd inspection
- source_ref: `/live`, 설치된 Windows Viewer 2.0.24, `playback_event`, image labels
- image: `camstation:2.0.0-rc.20260812.11-canary`,
  `sha256:b4e5fe10099bcd167c34925ac178d2951d2ad01c120e0af77858365dcae5259a`
- source_revision: `dd619b5990b4f05a2b6b56a969acdffd39c97f40`
- transport: host `10.0.0.26:18555/tcp+udp` → container `8555`; advertised candidate
  `10.0.0.26:18555`; API/RTSP는 내부 전용
- browser_result: 세 카메라 모두 WebRTC attempt 1에서 2.917~4.352초에 재생,
  retry/MSE/stream fallback 0
- windows_result: 세 카메라 모두 WebRTC attempt 1에서 3.209~4.210초에 재생,
  retry/MSE/stream fallback 0; 실제 `CamStation 2.0` 창에서 영상과 카메라 UIA 3개 확인
- evidence_hashes: Windows PNG
  `7b58feed11f17db87700e70c3c21bd585beba6705bd76dea0823c2c39419b562`, UIA
  `489ef95db9896e2c07b9418dc275c6c7ed2a4d5fb5df7e880660aa223bc05a6f`, complete JSON
  `3f40da6b76c830ca968ca4543f684bcd849f971713dd6b9a710e62e36389ec8e`
- diagnostics: 두 실제 클라이언트의 구조화 event 36건, URL/SDP/ICE 원문/자격증명 패턴 0
- continuity: container healthy/restart 0, camera 3/3 streaming, recorder 3/3 running,
  세 active recording inode 모두 증가, legacy 1.0 five-unit PID/restart baseline 불변
- rollback: `.10-canary` image와 root 전용 `.env.pre-webrtc-20260812-113427.bak`,
  `compose.yaml.pre-webrtc-20260812-113427.bak` 보존
- finding: 이전 약 10초 지연은 camera 장애가 아니라 공개되지 않은 WebRTC candidate를 두 번
  timeout한 뒤 같은 `live` stream의 MSE로 바뀐 결함이었다. 현재는 첫 WebRTC attempt에서
  재생되며, MSE transport 전환은 더 이상 `대체 스트림`으로 표시하지 않는다.

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

### F-003 — 수동 하트비트 폼이 가짜 Viewer와 삭제 오류를 만듦

- severity: operational
- category: state-management
- status: resolved
- evidence_ids: E-005
- location: `/viewers` 등록·삭제 UI와 `DELETE /api/viewers/{id}`
- finding: 수동 시험 폼이 최근 하트비트 상태의 가짜 행을 만들고, UI는 이를 삭제 가능하게
  표시한 반면 서버는 409로 거절해 원시 `validation`만 노출했다.
- impact: 설치 성공 오판, 불필요한 레지스트리 행, 유지보수 삭제 혼선
- confidence: high
- resolution: 시험 폼 제거, 실제 Agent 자동 등록만 유지, `offline`/`stale` 삭제 조건 공유,
  구조화된 한국어 409와 회귀 테스트 추가

### F-004 — Docker bridge WebRTC candidate 때문에 매번 두 번 timeout함

- severity: operational
- category: network-configuration
- status: resolved
- evidence_ids: E-007
- location: Docker port publish, go2rtc candidate 렌더링, live playback recovery
- finding: host에 WebRTC를 publish하지 않은 채 bridge 내부 candidate를 광고해 `/live`와
  Windows Viewer가 WebRTC를 두 번 5초 timeout한 뒤 같은 stream의 MSE로 전환했다.
- impact: 정상 카메라도 화면마다 약 10초 뒤 재생되고 `대체 스트림`이라는 잘못된 문구가 노출됨
- confidence: high
- resolution: 관리망의 충돌 없는 `18555/tcp+udp`를 publish하고 검증된 명시 candidate만
  광고했다. transport fallback과 실제 `live`→`focus` stream fallback을 분리하고 bounded
  구조화 재생 로그를 추가해 두 실제 클라이언트에서 첫 attempt 성공을 검증했다.

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

### P-002 — 오프라인 Viewer 정리 경로

- title: 실제 Viewer 종료부터 레지스트리 안전 삭제까지
- path_type: maintenance
- start: Windows Viewer/서비스 정상 종료
- goal: 실행 중인 Viewer를 건드리지 않고 정확한 오프라인 ID만 삭제
- steps:
  1. 설치된 Viewer Agent의 하트비트가 중단된다. — evidence: E-005 — finding: F-003
  2. 서버가 30초 TTL 뒤 상태를 `offline`으로 계산한다. — evidence: E-005 — finding: F-003
  3. 운영자가 `/viewers`를 새로고침하고 정확한 ID를 선택한다. — evidence: E-005
  4. 두 단계 삭제 확인 후 서버가 상태를 다시 검사하고 행을 삭제한다. — evidence: E-005
  5. 경합 하트비트가 있으면 `viewer_not_offline` 409로 중단하고 삭제하지 않는다. — evidence: E-005 — finding: F-003
- residual_risks: 목록 자동 갱신 주기는 15초이므로 30초 뒤에도 화면이 오래됐으면 수동
  새로고침이 필요함

### P-003 — Viewer 선택부터 원격 명령 결과까지

- title: 운영자 선택에서 Viewer Service 실행 결과까지
- path_type: callflow
- start: `/viewers`의 실제 Viewer 행 선택
- goal: PC 직접 조작 없이 고정된 복구 기능을 실행하고 결과를 확인
- steps:
  1. 운영자가 Viewer와 고정 기능 하나를 선택한다. — evidence: E-006
  2. 서버가 대상 상태·명령 type·필수 입력을 검증하고 durable command를 만든다. — evidence: E-006
  3. 독립 제어 연결이 명령을 받아 Service 또는 renderer의 좁은 adapter로 전달한다.
  4. Viewer/Service 재시작은 새 process·lease·boot generation이 확인된 뒤 결과를 확정한다.
  5. `/viewers`가 전달·확인·실행·최종 결과 시각을 자동 갱신한다. — evidence: E-006
- residual_risks: 현재 Viewer 실시간 연결은 검증했지만 장기 soak와 정식 서명 MSI 검증은 남아 있음

잔여 위험은 HTTP/WebRTC의 관리망 직접 접근, 수동 재시작, 집 카메라에 한정된 검증이다. PID 용량은 8대
기준으로 준비했지만 실제 8대 동시 영상 검증을 대신하지 않는다. 이 조건을 해소하기 전에는
본 카나리를 정식 전체 전환으로 취급하지 않는다.
