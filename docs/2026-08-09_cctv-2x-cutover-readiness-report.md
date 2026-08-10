# CCTV 2.0 서버 우선 전환 준비도 보고서

> 기준 시각: 2026-08-09 19:11 KST
>
> 현재 판정: **운영 사전 설치 통과 / 실제 전환 No-Go 유지**
>
> 운영 변경: 2.0 비활성 release·unit·DB와 legacy 유지형 nginx 전환 구조만 설치

## 결론

운영자가 승인한 시나리오는 실행 가능한 형태로 구체화됐다. CamViewer 1.0은 서버 전환
후 한시적으로 2.0의 라이브 화면만 표시하고, 관리·telemetry·원격 명령·자동 업데이트는
Viewer 2.0 단계까지 보류한다. 정확한 `GET /new?viewer=1` 요청은 비캐시 `302`로
`/live?viewer=1`에 연결된다.

다만 같은 서버에서 1.x와 2.0을 동시에 실행하는 방식은 사용할 수 없다. 현재 1.x의 별도
`go2rtc`가 1984/8554/8555를 사용하고 2.0도 같은 포트를 소유한다. 따라서 다음 두 구간을
구분해야 한다.

- 1.x 가동 중 가능: 2.0 release 설치, SQLite 온라인 snapshot, 비활성 2.0 DB 이관·검증
- 유지보수 창에서만 가능: nginx maintenance 전환, 정확한 1.x 세 유닛 정지, 포트 해제,
  2.0 기동, nginx 2.0 전환, CamViewer 1.0 정상 재실행

검증된 release와 카메라 DB는 운영 서버의 비활성 2.0 slot에 설치됐다. nginx는 원본 1.x
동작을 보존한 active include 구조로 무중단 reload됐고 전체 preflight가 통과했다. 1.x의
backend·backup·go2rtc PID와 재시작 횟수는 설치 전후 동일하며 2.0은 inactive/disabled,
`CUTOVER_APPROVED=NO` 상태다.

아직 교체 commit을 원격 `main`에 통합하지 않았고, 2.0 실카메라·녹화·운영 백업·현장 화면·
server rollback rehearsal도 수행하지 않았다. 따라서 유지보수 창의 실제 switch는 계속
차단한다.

## 준비된 기능

| 영역 | 준비 결과 | 자동 증거 |
| --- | --- | --- |
| Viewer 1.0 bridge | 정확한 `/new?viewer=1`만 `/live?viewer=1`로 `302`; `no-store`; 나머지 query는 `/`; 비-GET은 `405` | route 양성·음성·subpath 테스트 |
| 카메라 키 | 한글 글자·숫자·`_`·내부 `-`를 허용하고 경로·URL·공백·제어문자·잘못된 UTF-8·128바이트 초과를 차단 | 운영에서 관찰한 9개 키와 위험 입력 테스트 |
| 1.x snapshot | 실행 중 SQLite를 modernc 온라인 백업 API로 일관된 새 파일에 복사; 기존 파일·심볼릭 링크 덮어쓰기 금지 | snapshot 생성·quick-check·재실행 거부 테스트 |
| DB 이관 | `inspect`, `dry-run`, `import`, `verify`; fresh DB 원자 승격; 기존 동일 DB는 `already-current`; 다른 DB는 거부 | 합성 `9/8/1` 이관·반복·불일치·부분 승격 방지 테스트 |
| 카메라 graph | main/sub 입력, 세 출력, enabled 상태, 레이아웃 키, policy `pending` 상태 보존 | private target DB와 canonical fingerprint 비교 |
| 설정 | 30분 segment, 30일 retention, 700GB를 기대값으로 고정; 백업은 빈 대상·disabled·`protectUnbacked=true` | 설정·기대값·안전 기본값 테스트 |
| 배포 경계 | root 전용 0600 config, 불변 release, systemd cgroup 종료, online snapshot/import, preflight, switch, rollback helper | Bash syntax·정책 검사 |
| 비밀정보 | manifest에는 URL fingerprint와 선택 필드 존재 여부만 기록 | 합성 RTSP 계정·비밀번호·token stdout/stderr 부재 테스트 |

HTTP/HTTPS 카메라 입력은 2.0의 HTTP-FLV 지원 경로로 이관되며 RTSP/RTSPS와 구분해 포트
metadata를 만든다. 서로 다른 ONVIF 전용 계정은 현재 2.0 모델에 안전하게 표현할 수 없으므로
영상 이관을 차단하지 않는다. 해당 control은 fail-closed로 비활성화하고 원문은 root 전용 1.x
snapshot에 보존하며 manifest에는 `CAMERA_CONTROL_DEFERRED`만 기록한다.

## 운영 서버 재확인

2026-08-09 읽기 전용 점검에서 다음을 확인했다.

| 확인 항목 | 결과 | 영향 |
| --- | --- | --- |
| 호스트 | 기존 운영 `cctv` | 교체 대상은 과거 `cctv2`가 아님 |
| 1.x 유닛 | backend, backup, go2rtc, nginx, VStarcam TLS proxy 모두 active | switch는 정확한 유닛 단위여야 함 |
| 포트 | 80, 1984, 8554, 8555 사용; 18080 비어 있음 | 설치는 병행 가능하지만 runtime은 single-active |
| 필수 도구 | go2rtc, ffmpeg, ffprobe, rclone, nginx 존재 | 2.0 runtime 기본 도구 존재 |
| SQLite CLI | 없음 | WAL 파일 셸 복사 금지; 내장 online backup 사용 |
| root 파일시스템 | 약 13% 사용 | 현재 즉시 압박 없음; recordings filesystem은 별도 임계치 검사 필요 |
| nginx backend | 두 server block에서 `/api/` → loopback 8000, `/go2rtc/` → loopback 1984; `/assets/`, `/` 별도 | 현재 설정을 그대로 두고 2.0 location을 추가하면 충돌함 |

nginx에는 이미 `location /`이 있으므로 제공된 2.0 location 파일을 단순 추가해서는 안 된다.
사전 설치에서 내용이 같은 중복 server block을 활성 wildcard 밖의 root 전용 보존소로 옮기고,
단일 server block이 `active-backend`를 include하도록 변경했다. active target은 여전히
byte-equivalent legacy location이며 `nginx -t`, reload 후 1.x health와 NUC 트래픽을 확인했다.

## 실제 비활성 사전 설치 결과

| 증거 | 검증 결과 |
| --- | --- |
| Release | `2.0.0-rc.20260809.5`, 교체 commit `db09c6c9d142e9c6d1a360b0b4a59ac098fe8283` |
| Daemon SHA-256 | `590d49b501a4523e6a8d5d6b5e4e966d85b891ca632efd19f79bc57d8ef4beba` |
| Migrator SHA-256 | `6fb2718b8c0b7be50a3315904128e7e5d84be72b0690de37b53401910d27c57c` |
| Source 보존 | 두-parent 교체 commit과 후보 이력을 포함한 root 전용 Git bundle; SHA-256 `bac75de5224bd55c3128b5cd2326d757274b601d1af8c63a58aef6e146c323db` |
| DB 이관 | `ready=true`, `verified`, canonical fingerprint `636af019dce2debb7c30e54b49966be9a1afe2679d3f0a30c0d0fa305bc80874` |
| 카메라·layout | 9 registered, 8 enabled, `소방서2` disabled, legacy sub 항목 9, layout 1/8, blocker 0 |
| Legacy 파생 live | 세 loopback ffmpeg recipe를 재귀 input이 아닌 recording 기반 H.264 live output으로 매핑 |
| 설정 | segment 30분, retention 30일, 700GB; backup disabled, target 없음, `protectUnbacked=true` |
| 저장소 | DB/Viewer state와 media recording/temp를 분리; 두 media 디렉터리는 같은 대용량 filesystem |
| systemd·port | 2.0 inactive/disabled, restart 0, 18080 free; 1.x 세 유닛 active/enabled, restart 0 |
| 재부팅 소유권 | switch가 legacy disable 후 2.0 enable; 자동·수동 rollback은 2.0 disable 후 legacy enable |
| nginx | server block 1개, active target=legacy, 원본 두 파일 보존, config test와 reload 통과 |
| 1.x 연속성 | health `ok`, cameras online 8; backend/backup/go2rtc PID 설치 전후 동일 |
| NUC 연속성 | nginx 최근 50,000행 표본 중 `192.168.0.13` 요청 13,333건, 마지막 19:11:35 KST, 200/101 지속 |
| 안전 잠금 | root 전용 config 0600, `CUTOVER_APPROVED=NO` |

## 준비도 표

| ID | 의존성 / 소유자 | 증명 방법 | 현재 상태 | 판정 |
| --- | --- | --- | --- | --- |
| R1 | 교체 merge / 소스 관리자 | 두 parent와 승인 2.0 tree hash, clean `main` | 교체 commit과 Git bundle 검증; 원격 `main` 미통합 | **No-Go** |
| R2 | 코드 검증 / 개발 담당 | `go test ./...`, vet, Web/Viewer test·lint·build, 두 Go binary build | Go 전체, vet, Web 52, Viewer 23, lint/build, 두 Go binary build 통과 | **Go** |
| R3 | importer / 개발 담당 | 합성 `9/8/1`, layout 1/8, 30/30/700, secret scan, repeat import | 합성 및 실제 운영 snapshot 검증 통과; blocker 0 | **Go** |
| R4 | 최종 source snapshot / 서버 운영자 | `prepare-state.sh`; active/snapshot canonical fingerprint 동일 | immutable online snapshot과 target parity 통과 | **Go** |
| R5 | systemd / 서버 운영자 | unit/env hash, inactive service, boot enablement, owner/mode/path, 18080 free | 설치·hash·owner/mode 검증; legacy enabled, 2.0 inactive/disabled | **Go** |
| R6 | nginx / 서버 운영자 | legacy/maintenance/2.0 include, active symlink, `nginx -t`, rollback link | legacy active 상태로 무중단 준비·health 검증 | **Go** |
| R7 | 실카메라·녹화 / 현장+서버 운영자 | 8 live, recorder 8 증가, 30분 rollover 8 ready/playable | 미실행 | **No-Go** |
| R8 | 운영 backup / 백업 담당 | 실제 remote, upload 8, DB mark 8, failure 0, premature delete 0 | imported DB는 의도적으로 disabled | **No-Go** |
| R9 | Viewer 1.0 표시 / 현장 확인자 | 앱 정상 재실행, redirect, 활성 8대 영상 | NUC 변경 없음 | **No-Go** |
| R10 | server rollback / 서버 운영자 | 2.0 정지, 세 legacy 유닛과 nginx 복원, 8 live/record/backup | helper 정적 검사만 통과 | **No-Go** |
| R11 | Viewer 2.0 / 현장 확인자 | 서버 완료 후 GUI·telemetry·auto-start·reconnect | 독립 후속 단계 | 대기 |

## 실행 패킷

실제 경로와 hash는 Git 밖의 root 전용 0600 설정 파일에만 기록한다. 아래 명령의
`$CUTOVER_CONFIG`에는 credential을 넣지 않고, 카메라 URL과 backup secret은 별도 운영
저장소에서 관리한다.

### 1. 깨끗한 `main`에서 release 생성

```bash
scripts/production/build-release.sh \
  --output "$RELEASE_BUNDLE" \
  --release-id "$RELEASE_ID"
```

이 명령은 `main`이 아니거나 worktree가 dirty이면 실패한다. Web embedded asset과 두 Go
binary를 생성하고 전체 bundle file의 `SHA256SUMS`를 만든다.

### 2. 1.x를 유지한 채 release 설치

```bash
sudo scripts/production/stage-release.sh \
  --config "$CUTOVER_CONFIG" \
  --bundle "$RELEASE_BUNDLE" \
  --execute
```

이 단계는 2.0 service를 시작하거나 nginx active include를 바꾸지 않는다.

### 3. 운영 변경 freeze 후 snapshot과 DB 이관

```bash
sudo scripts/production/prepare-state.sh \
  --config "$CUTOVER_CONFIG" \
  --execute
```

1.x 카메라·layout·settings 변경을 금지한 뒤 실행한다. 1.x는 가동 상태를 유지하고 online
backup을 만들며, 새 2.0 DB를 생성한 후 서비스 계정 소유 0600으로 바꾸고 다시 검증한다.
preflight는 직전에 active 1.x와 snapshot fingerprint를 다시 비교하므로 freeze 후 drift가
있으면 switch를 차단한다.

### 3A. 1.x 동작을 유지한 nginx active include 준비

```bash
sudo scripts/production/prepare-nginx.sh \
  --config "$CUTOVER_CONFIG" \
  --execute
```

검토된 원본 hash가 정확히 일치할 때만 legacy include 구조로 reload한다. 기존 upstream은
바뀌지 않으며 실패하면 원본 site와 중복 block을 복원한다.

### 4. 유지보수 창 preflight와 server switch

```bash
sudo scripts/production/preflight.sh --config "$CUTOVER_CONFIG"

sudo scripts/production/switch-to-2x.sh \
  --config "$CUTOVER_CONFIG" \
  --execute
```

switch helper는 maintenance 응답을 적용하고 `camstation-backend`, `camstation-backup`,
`go2rtc`만 정지한다. VStarcam TLS proxy는 유지한다. 1984/8554/8555 해제 뒤 2.0을 시작하고
health, camera API, recorder API, Viewer bridge를 확인한 뒤 nginx를 2.0으로 바꾼다. 하나라도
실패하면 2.0을 정지하고 legacy 유닛과 include 복원을 시도한다.

### 5. 즉시 server rollback

```bash
sudo scripts/production/rollback-to-1x.sh \
  --config "$CUTOVER_CONFIG" \
  --execute
```

2.0 DB와 그 시간대 recordings는 삭제하지 않는다. rollback 후 CamViewer 1.0은 저장된 운영
주소를 바꾸지 않고 정상 재실행한다.

## 전환 당일 판정 순서

1. `PREFLIGHT_READY`와 active/snapshot parity를 확인한다.
2. server switch 후 활성 카메라 8대의 live output을 확인한다.
3. 192.168.0.13에서 CamViewer 1.0을 정상 종료·재실행하고 8대 영상을 육안 확인한다.
4. recorder 8개가 10초 이상 증가하는지 확인한다.
5. 30분 rollover에서 8개 파일이 `ready`이고 재생 가능한지 확인한다.
6. 운영 backup remote를 설정해 upload/mark를 확인한다.
7. 최소 60분 server soak 후에만 Viewer 2.0 전환을 별도로 시작한다.

Viewer 1.0 telemetry가 없는 것은 예상 상태다. 반대로 활성 8대 영상, recorder, rollover,
backup 중 하나가 실패한 상태를 “영상 셸 제한”으로 정당화해서는 안 된다.

## Evidence → Finding → Path

| Evidence | Finding | Path |
| --- | --- | --- |
| 1.x와 2.0이 모두 1984/8554/8555를 사용 | 같은 호스트 동시 실행 불가 | artifact/data만 병행 staging하고 runtime은 single-active switch |
| 운영 카메라 키 9개가 한글이고 layout이 해당 키를 참조 | ASCII slug 변환은 identity와 layout을 파괴 | bounded Unicode key를 store와 API 양쪽에서 검증하고 원문 보존 |
| 운영 서버에 sqlite3 CLI 없음, 1.x DB는 WAL 사용 가능 | 단순 DB 파일 복사는 최신 row를 잃을 수 있음 | embedded SQLite online backup으로 불변 snapshot 생성 |
| 기존 nginx에 두 세트의 `/api/`, `/go2rtc/`, `/assets/`, `/` location 존재 | 2.0 `location /` 단순 추가는 duplicate/conflict | 기존 location 세트를 legacy include로 보존한 뒤 active symlink 방식 설치 |
| Viewer 1.0은 `/new?viewer=1`로 시작 | 2.0 `/live`에 직접 도달하지 않음 | exact redirect와 음성 테스트; client telemetry는 Viewer 2.0 gate로 이동 |
| 기본 2.0 backup target이 개발 remote였음 | 운영에서 잘못된 remote를 조용히 사용할 위험 | 새 DB 기본 target을 비우고 enabled일 때만 target 필수; 운영 backup은 별도 gate |
| 운영 recordings filesystem은 대용량 media mount이고 root filesystem은 약 50GB | state와 영상을 같은 root에 두면 preflight와 장기 보존 모두 실패 | DB/Viewer state는 state root, recording/temp는 동일 media filesystem으로 분리 |
| 세 legacy sub 값은 loopback go2rtc ffmpeg recipe | 일반 URL로 복사하면 2.0에서 재귀 producer가 됨 | exact local recipe만 recording 기반 H.264 live output으로 변환; 다른 recipe는 차단 |
| 동일한 legacy nginx backup site가 wildcard 아래에서도 활성 | active include만 바꿔도 중복 server block이 남음 | hash 일치 확인 후 root 전용 보존소로 이동하고 단일 server block을 reload |

## 다음 승인 작업

다음 작업은 운영 switch가 아니라 소스 통합 작업이다.

1. 보존된 two-parent replacement commit을 원격 integration branch/PR로 검토해 `main`에 통합한다.
2. 전환 직전 camera/layout/settings 변경을 freeze하고 preflight를 다시 실행한다. drift가 있으면
   새 release가 아니라 새 immutable snapshot만 승인 절차로 준비한다.
3. 유지보수 승인 시에만 root config의 `CUTOVER_APPROVED`를 `YES`로 바꾼다.
4. switch helper 실행 후 R7~R10의 live, recorder, backup, 현장 Viewer, rollback 증거를 수집한다.

R1과 R7~R10이 남아 있다. 이 보고서는 사전 설치 완료 증거이지 실제 전환 완료 증거가 아니다.

## 관련 자료

- [상세 전환 전략](2026-08-09_cctv-1x-to-2x-production-cutover-strategy.md)
- [운영 접근 및 유지보수 실행서](2026-08-09_operations-cctv-maintenance-report.md)
- [준비 설계](superpowers/specs/2026-08-09-production-cutover-preparation-design.md)
- [준비 구현 계획](superpowers/plans/2026-08-09-production-cutover-preparation.md)
- [이관 명령](../cmd/camstation-migrate/main.go)
- [production helper](../scripts/production/)
- [systemd unit](../packaging/systemd/camstationd-2x.service)
- [nginx 2.0 location](../packaging/nginx/camstation2-location.inc)
