# CamStation production deployment

## 배포 소유권

- 주 저장소: `https://git.loc.hmini.me/dyllislev/CamStation`
- 기본 브랜치: `main`
- GitHub Push Mirror: `https://github.com/dyllisLev/CamStation`
- GitHub는 Forgejo에서 나가는 단방향 mirror다. GitHub Actions는 build 또는 production 배포에
  사용하지 않는다.
- production build와 배포를 시작할 수 있는 source event는 Forgejo `main` push뿐이다. Fork PR이나
  다른 branch는 self-hosted runner를 실행하지 않는다.
- workflow의 `runs-on`은 `self-hosted`이며 현재 연결된 runner는 `ct109-forgejo-ci`
  (`linux/amd64`)다.

## 이미지 계약

| 항목 | 값 |
| --- | --- |
| Registry repository | `git.loc.hmini.me/dyllislev/camstation` |
| immutable tag | `sha-<40자리 Forgejo commit SHA>` |
| 전체 image 형식 | `git.loc.hmini.me/dyllislev/camstation:sha-<commit>` |
| build context | 저장소 root (`.`) |
| Dockerfile | `Dockerfile` |
| build target | `runtime` |
| platform | `linux/amd64` |

이미지는 Web build, Go build와 runtime 단계를 분리한다. Runtime은 UID/GID `10001:10001`, Tini,
digest로 고정된 base/go2rtc와 production 실행 도구를 사용한다. Application credential은 build arg,
label 또는 image layer에 넣지 않는다.

## OpenShip 등록

| 항목 | 값 |
| --- | --- |
| environment | `production` |
| external API | `https://openship.loc.hmini.me/api/proxy/api` |
| project | `CamStation` |
| project ID | `proj_UQBJSJcGaIzbS3Ig` |
| target server/agent | `cctv-production` |
| server ID | `40ef57fc-fad7-43ca-8a23-496f75158913` |
| registry pull credential | `openship-registry-pull-20260901` |
| Git source | 없음 |
| webhook / Auto Deploy | Off / Off |
| sleep mode | `always_on` |
| route strategy | `container-ip` (OpenShip managed route 없음) |

OpenShip 0.6.9는 project에 credential ID를 저장하지 않고 image registry hostname과 조직 범위
credential selector를 대조해 pull 인증을 적용한다. 따라서 위 이름의 credential은
`git.loc.hmini.me` selector에서 `active`이고 마지막 검증 오류가 없는 상태를 유지해야 한다.

OpenShip 서비스는 하나다. `camstationd`가 HTTP console/API, go2rtc, recorder ffmpeg worker, cleanup과
backup scheduler를 감독하므로 worker 또는 scheduler를 별도 장기 실행 서비스로 중복 생성하지 않는다.

| 서비스 | service ID | image/build/dockerfile | 실행 | 내부 port | health | restart/resources |
| --- | --- | --- | --- | --- | --- | --- |
| `camstation` | `svc_BUQkqpLnupDIlAUl` | Forgejo image / 빈 값 / 빈 값 | image 기본 `camstationd` CMD | HTTP `18080/tcp`, WebRTC `8555/tcp`, `8555/udp` | `GET /api/health`; image Docker healthcheck 사용 | `unless-stopped`; stop grace `120s`; 8 CPU, 6144 MiB |

OpenShip 0.6.9의 Docker port parser는 같은 container port/protocol에 host IP가 다른 publish를 여러 개
저장하면 마지막 항목만 남긴다. 기존 두 물리 LOC interface를 모두 유지하기 위해 host의 IPv4 interface
inventory에 공개 또는 Tailscale/overlay interface가 없음을 확인한 뒤 다음 세 wildcard publish로
정규화한다. 이 형식은 두 LOC 경로를 모두 보존하지만 loopback과 local Docker bridge에서도 host port를
수신한다. 배포 직전 interface inventory가 달라졌으면 service 동기화와 배포를 중단한다.

- HTTP: host `0.0.0.0:18080/tcp` → container `18080/tcp`
- WebRTC: host `0.0.0.0:18555/tcp` → container `8555/tcp`
- WebRTC: host `0.0.0.0:18555/udp` → container `8555/udp`

외부 공개 대상은 HTTP/WebRTC뿐이며 SQLite, worker, scheduler나 내부 go2rtc 관리 listener를 proxy에
공개하지 않는다. 기존 도메인과 host port를 유지하므로 이번 전환에서는 Zoraxy 설정을 변경하지 않는다.
배포 후에는 기존 두 물리 LOC endpoint와 외부 도메인을 각각 실제 요청으로 검증한다.

대상 host의 기존 nginx가 LOC 도메인을 계속 소유한다. OpenShip service/project domain row는 만들지 않고
service는 `exposed=false`를 유지한다. Project route strategy는 `container-ip`다. 이는 비노출 service의
배포에서 OpenShip이 loopback route inventory를 이유로 80/443 OpenResty takeover를 요구하지 않게 하며,
기존 nginx 또는 Zoraxy 설정을 변경하지 않는다.

서비스 간 `dependsOn`은 없고, 단일 `camstation` 서비스만 OpenShip project network에 연결된다. go2rtc와
recorder의 통신은 같은 container 내부 loopback을 사용한다. 두 persistent bind mount는 모두 application
쓰기 대상이므로 read-write이며 별도 read-only mount는 없다.
CamStation application 자체에는 별도 로그인 또는 HTTP authentication challenge가 없다. 접근 경계는
기존 LOC DNS/network와 proxy 정책을 그대로 사용하며, 이번 배포 전환은 새 공개 route나 우회 경로를
추가하지 않는다.

OpenShip service의 `advanced.stopGracePeriod`는 `120s`다. 현재 OpenShip Docker 배포기는 고정 port
서비스의 이전 container 정리를 30초까지만 기다리므로 CamStation daemon은 그보다 먼저 종료되어야 한다.
Recorder 종료는 모든 worker에 stop을 먼저 fan-out한 후 대기하며, 실행 중 FFmpeg의 종료 신호는 각
worker의 process wait 경로 한 곳에서만 보낸다. 새 release의 실제 재배포 합격에는 전체 daemon 종료가
30초 안에 끝나고 종료 시 닫힌 모든 recording 파일이 `ffprobe`를 통과하는 것이 포함된다.

OpenShip 0.6.9는 Compose의 root filesystem `read_only`, `cap_drop`, `security_opt`를 service model에
보존하지 않는다. 따라서 기존 Compose의 세 Docker-level hardening flag는 OpenShip container에 직접
표현되지 않는다. Runtime image는 계속 UID/GID `10001:10001`로 실행되고 application binary와 base
filesystem은 root 소유다. 의도된 영구 쓰기 대상은 state/media bind mount이며 표준 `/tmp`는 runtime
임시 쓰기에 사용된다. OpenShip이 해당 flag를 지원하면 별도 데이터 migration 없이 service runtime
hardening을 다시 적용한다.

## 환경변수와 Secret

OpenShip service가 보존해야 하는 application 환경변수 이름은 다음과 같다. 값은 OpenShip의 보호된
service 설정과 기존 persistent state에서 관리하며 저장소나 이 문서에 기록하지 않는다.

- Runtime/state: `CAMSTATION_ADDR`, `CAMSTATION_DB`, `CAMSTATION_RECORDINGS_DIR`,
  `CAMSTATION_TEMP_DIR`, `CAMSTATION_VIEWER_RELEASES_DIR`
- Recording/retention: `CAMSTATION_RECORDING_ENABLED`, `CAMSTATION_SEGMENT_MINUTES`,
  `CAMSTATION_MAX_STORAGE_GB`
- Logging: `CAMSTATION_LOG_DIR`, `CAMSTATION_LOG_LEVEL`, `CAMSTATION_LOG_LEVELS`,
  `CAMSTATION_LOG_MAX_MB`, `CAMSTATION_LOG_FILES`, `CAMSTATION_PLAYBACK_LOG_LEVEL`
- Network/runtime support: `CAMSTATION_WEBRTC_CANDIDATES`, `TZ`, `HOME`, `TMPDIR`,
  `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `PATH`

별도 application Secret 환경변수는 없다. 카메라와 원격 저장소 credential은 기존 SQLite/application
설정 경계 안에 유지하며 Forgejo Actions로 복제하지 않는다.

Forgejo Actions Variables:

- `REGISTRY_HOST`
- `IMAGE_REPOSITORY`
- `OPENSHIP_API_URL`
- `OPENSHIP_PROJECT_ID`
- `OPENSHIP_SERVICE_IDS`

Forgejo Actions Secrets:

- `REGISTRY_USERNAME`
- `REGISTRY_TOKEN`
- `OPENSHIP_DEPLOY_TOKEN`

`REGISTRY_TOKEN`은 Registry push용 `write:package`만 갖는다. `OPENSHIP_DEPLOY_TOKEN`은 이 project의
조회와 배포 변경에 필요한 project-scoped read/write grant만 갖는다. Secret 값은 terminal, Actions
log, 문서 또는 commit에 출력하지 않는다.

## 영구 데이터와 백업

서비스에는 기존 두 bind mount를 그대로 연결한다.

- state volume: SQLite DB와 관련 파일, application 설정, 로그, Viewer release와 runtime cache
- media volume: finalized recording과 active temporary segment

실제 host 경로와 환경값은 root 전용 운영 설정과 OpenShip의 보호된 service 설정에서만 관리한다.
OpenShip 전환이 volume을 새로 만들거나 기존 bind mount를 교체해서는 안 된다.

최초 전환 전 확보한 복구 자산:

- 이전 image/revision: `camstation:2.0.0-rc.20260828.22-playback-attribution` /
  `fe9cbfdab67e16751c6b6df25163b9ecdb45b382`; local rollback tag
  `camstation:rollback-pre-forgejo-20260901T222109`
- restricted application/state/image/config backup set:
  `pre-forgejo-openship-20260901T222109+0900`
- 기존 container rootfs PBS snapshot: `ct/113/2026-09-01T13:21:43Z`
- restricted OpenShip control-plane backup set:
  `camstation-forgejo-cutover-20260901T192623+0900`
- OpenShip CT PBS snapshot: `ct/110/2026-09-01T13:34:55Z`
- media PBS snapshot: `host/camstation-media/2026-09-01T13:33:45Z` (`protected=true`)
- watcher target 변경 전 root-only backup:
  `forgejo-openship-watcher-20260902T064800+0900`

SQLite는 실행 중 data directory를 복사하지 않는다. Python SQLite online backup API로 일관된 사본을
만들고 `PRAGMA quick_check`, foreign-key 검사와 checksum을 확인한다. Media는 짧은 filesystem freeze로
snapshot 경계를 만든 뒤 즉시 unfreeze하고, read-only snapshot을 PBS에 올려 manifest와 보호 상태를
검증한다. 운영 container는 대용량 업로드 동안 계속 실행한다.

### 최초 legacy image handoff

전환 전 image에는 recorder worker를 직렬로 기다리면서 동일 FFmpeg에 종료 신호를 두 번 보낼 수 있는
결함이 있다. 이 image를 일반 `docker restart`나 곧바른 OpenShip deployment로 교체하지 않는다. 전체
media snapshot이 검증·보호된 뒤 다음 one-time gate를 적용한다.

1. daemon의 직접 자식 중 segment muxer인 recorder FFmpeg 8개만 정확히 식별한다. go2rtc/live-warm
   FFmpeg는 대상이 아니다.
2. 8개 recorder에 정상 종료 신호를 한 번씩 동시에 보내고 원래 process가 모두 종료됐는지 확인한다.
3. 5초 retry 전에 application recorder-stop API로 worker를 정지하고 worker 수가 0인지 확인한다.
4. 직전에 `recording`이던 DB row 8개가 `ready`가 됐는지, DB/file size가 일치하는지, 각 파일의 video와
   audio track 및 `ffprobe`가 정상인지 확인한다.
5. 이 gate를 통과한 상태에서만 exact Forgejo image로 첫 OpenShip deployment를 만든다. 실패하면 새
   container를 반복 재시작하지 말고 보존한 기존 image/config로 복구한다.

2026-09-02 실제 전환에서는 legacy FFmpeg 일부가 TERM을 30초 안에 처리하지 못했다. 사용자가 실시간
녹화 중단을 명시적으로 허용한 뒤 recorder worker를 0으로 정지하고 전환을 진행했다. 06:37 KST에 열린
segment 세 개는 size가 DB와 일치하지만 `ffprobe`에 실패한다. 이후 exact Forgejo image는 recorder 8/8,
새 active segment 8/8 증가와 최근 DB `failed` 0으로 수렴했다. 이 세 파일은 legacy handoff 구간의 알려진
데이터 손실이며 정상 운영 segment로 간주하지 않는다.

새 Forgejo image에는 fan-out/single-signal 수정이 포함되므로 이 절차는 최초 legacy handoff에만 쓴다.
이후 배포는 Forgejo workflow와 아래 검증 절차만 사용한다.

최초 전환에서는 recorder handoff 시각을 Actions build 완료 시각에 맞춰 추측하지 않는다. 준비 commit을
먼저 Forgejo `main`에 게시하되 workflow 파일은 그 다음 commit에서 활성화하고, 보호된 bootstrap
credential로 준비 commit의 exact SHA image를 한 번 수동 build/push한 뒤 위 gate와 OpenShip 배포를
수행한다. 이 배포의 application 검증이 끝난 뒤 workflow와 최종 상태 문서를 다음 `main` commit으로
push한다. 그 push의 Actions 배포가 같은 persistent data로 다시 `ready`가 되는 것이 자동화와 데이터
지속성의 최종 합격이다. 공용 owner credential은 이 one-time bootstrap 외 CI에서 사용하지 않는다.

최초 bootstrap image `sha-2d83b8f8c9388e70dbd3a2f934cc5c372fb2ec55`의 OpenShip deployment는
`dep_mEfHp_BAona-cg55`이며 `ready`로 검증됐다. 실제 container의 안정 이름은
`openship-camstation-camstation`이다. Host operational watcher는 이 이름을 사용한다.

## 배포 흐름

`.forgejo/workflows/build-publish-deploy.yml`은 다음 순서로 동작한다.

1. `main`의 exact commit을 credential 비보존 checkout으로 받는다.
2. 임시 Docker config를 만들고 Registry token을 standard input으로 전달한다.
3. 공용 `/var/lock/openship-ci-build.lock`을 최대 7200초 기다린 뒤 buildx로 `linux/amd64` image를
   `sha-<commit>` tag에 push한다.
4. `OPENSHIP_SERVICE_IDS`의 각 서비스 image를 PATCH하고 `build`, `dockerfile`을 빈 값으로 유지한다.
5. production deployment를 만들고 5초 간격, 최대 15분 동안 상태를 확인한다.
6. `ready`만 성공이다. `failed`, `cancelled`, `action_required`, `partial_failure`, `rejected`,
   `no_changes`, API 오류나 timeout은 실패이며 bounded 오류 정보, 최근 deployment log와 pending
   action을 출력한다.

배포 생성 전 일부 service PATCH만 성공한 경우 workflow는 이전 image/build/dockerfile 설정을 복원한다.
배포가 이미 생성된 뒤 실패하면 자동으로 원인을 숨기지 않고 아래 절차로 확인한 뒤 exact 이전 image로
rollback한다.

## 배포 검증

Forgejo/OpenShip 성공 표시는 application 합격의 일부일 뿐이다. 매 배포 후 다음을 확인한다.

1. Forgejo Actions run이 exact `main` SHA로 성공했고 Registry manifest platform이 `linux/amd64`인지 확인한다.
2. OpenShip deployment가 `ready`이며 service의 `image`는 exact SHA tag, `build`와 `dockerfile`은 빈 값인지
   확인한다.
3. 실제 container image label/revision, restart count 0, Docker health와 내부 `/api/health`를 확인한다.
4. 기존 두 물리 LOC endpoint에서 `/api/health`가 모두 정상인지 확인하고, 외부
   `https://cctv2.nuc.hmini.me/api/health`의 TLS/HTTP 200과 application `ok=true`도 확인한다.
5. 최근 bounded log에서 panic/fatal/OOM/restart loop가 없고 SQLite `quick_check`가 `ok`인지 확인한다.
6. state/media bind mount가 전환 전과 같은 source를 가리키고, camera/live/recorder worker가 8/8이며 새
   recording row와 segment가 전진하는지 확인한다.
7. Backup이 의도대로 비활성이고 active job이 없거나, 활성화된 경우 scheduler/job 상태가 정상인지 실제
   설정과 대조한다.
8. 후속 immutable SHA image를 한 번 재배포한 뒤 stable DB fingerprint, mount identity와 기존 recording이
   유지되는지 확인한다.
9. 재배포 직전 열려 있던 각 recording이 ready 상태로 닫혔고 실제 파일 크기가 DB와 일치하며, 영상·음성
   track과 `ffprobe` 무결성을 통과하는지 확인한다. OpenShip log에서 이전 container 정리 timeout이나
   host-port 충돌이 없어야 한다.

## 장애 확인 순서

1. Forgejo Actions log와 전역 self-hosted runner `ct109-forgejo-ci`의 `idle/running` 상태
2. Docker/buildx, 공용 lock, 대상 platform과 필요 시 QEMU/binfmt
3. Registry login과 push scope, exact Registry manifest
4. `OPENSHIP_API_URL`, project/service ID와 scoped deploy token grant
5. OpenShip deployment error/log/pending action과 target server 연결
6. target host의 Registry DNS/TLS/MTU 1450 경로와 image pull
7. container health/restart/log, port conflict, environment-name 누락과 bind-mount identity
8. SQLite, recorder/worker, 외부 LOC domain/TLS 순서

## Rollback과 데이터 복구

코드 rollback은 persistent volume을 바꾸지 않고 직전 exact image로 되돌린다.

1. OpenShip API에서 실패 deployment와 현재 runtime을 보존하고 추가 deployment를 중단한다.
2. Registry에 존재하는 직전 `sha-<commit>`을 service image로 PATCH하고 새 deployment가 `ready`인지 확인한다.
3. 최초 전환처럼 직전 SHA image가 Registry에 없으면 보존한 rollback image와 root 전용 기존 deployment
   config로 기존 `camstation` 서비스 하나만 복구한다. OpenShip 관리 compose는 직접 편집하지 않는다.
4. 내부/external health, SQLite quick check, mount identity, recorder 8/8과 새 segment 전진을 다시 확인한다.

현재 배포 commit과 직전 운영 commit 사이에는 SQLite schema 변경이 없으므로 일반 코드 rollback은 기존
DB를 그대로 사용한다. 데이터가 손상된 경우에만 서비스를 안전하게 중지하고 검증된 SQLite online
backup을 원자적으로 복원한다. Media 복구는 PBS snapshot에서 별도 위치로 우선 복원·검증한 뒤 필요한
파일만 되돌린다. 기존 volume을 백업 없이 초기화하거나 교체하지 않는다.
