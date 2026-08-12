# go2rtc 기준 Docker canary 구현 계획

**목표:** 1.0 go2rtc YAML의 활성 `집-` main/sub 세 쌍만 엄격히 변환해, 기존 1.0과
동시에 실행 가능한 CamStation 2.0 Docker canary를 검증한다.

**설계:** [go2rtc 기준 Docker canary 설계](../specs/2026-08-09-go2rtc-container-canary-design.md)

## 작업 1: go2rtc YAML subset importer

- YAML parser가 주석 처리된 항목을 제외하고 scalar/list producer를 안전하게 해석한다.
- `집-` prefix, 정확한 3대, main/sub 쌍, non-loopback URL을 fail-closed로 검증한다.
- manifest는 stream key와 URL SHA-256만 출력하고 원본 URL을 노출하지 않는다.
- 기존 target, symlink target, 잘못된 prefix, 다중 producer는 덮어쓰지 않는다.

검증:

```bash
./scripts/dev-go.sh test ./internal/legacyimport ./cmd/camstation-migrate
```

## 작업 2: container compatibility

- Dockerfile은 Web UI와 Go binaries를 재현 가능하게 build한다.
- pinned go2rtc 1.9.14 runtime에 FFmpeg/ffprobe, rclone, CA/timezone을 포함한다.
- 외부에는 HTTP만 공개하고 go2rtc API/RTSP/WebRTC는 container 내부 기본 포트를 유지한다.
- canary 영상 판정은 same-origin `/player/api/ws` MSE 경로로 명시한다.
- root filesystem read-only, non-root UID, dropped capabilities로 smoke test한다.

검증:

```bash
docker build --tag camstation:2.0-canary .
docker run --rm --read-only --user 10001:10001 --cap-drop ALL \
  --security-opt no-new-privileges --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  camstation:2.0-canary /bin/sh -c \
  'test -x /usr/local/bin/camstationd && test -x /usr/local/bin/go2rtc && test ! -w /'
```

## 작업 3: production canary data

- 배포 직전 1.0 unit/PID/health/8대 상태와 YAML SHA-256을 기록한다.
- live YAML을 read-only mount한 일회성 importer로 새 canary DB를 생성한다.
- manifest에서 정확히 세 `집-` key, 6 producer fingerprint, backup/alert off를 확인한다.
- final-cutover DB와 1.0 DB/config의 내용 및 mode/owner를 변경하지 않았음을 재검증한다.

## 작업 4: bounded runtime verification

- host `18081/tcp`만 공개해 canary를 수동 시작한다.
- 2.0 API와 generated config에서 비대상 camera/producer가 없음을 증명한다.
- 세 live stream, recorder worker, 10초 이상 byte 증가, 1분 segment playback을 확인한다.
- 1.0 health, 8대 online, 서비스 PID/restart count를 시작 전후 비교한다.
- 브라우저에서 `/live`와 전용 `/viewer` 세 타일을 확인하고 secret 없는 screenshot을 보존한다.

## 작업 5: 유지와 문서화

- 모든 합격 검증 후 canary를 실행 상태로 유지하고 `http://10.0.0.26:18081` 접속을 확인한다.
- 실패 시에만 canary를 중지하고 alternate listener와 producer/recorder가 모두 사라졌는지 확인한다.
- image ID/digest, source/YAML hash, DB manifest, 시작/중지/업데이트/롤백 명령을 운영 문서에 기록한다.

## 실행 결과

- 최종 이미지: `camstation:2.0.0-rc.20260809.7-canary`, image ID
  `sha256:628da2dbd0a7bbe94280d45284fe975617e3b8a56e02f8389db4ca84d68202e9`.
- 최종 주소: `http://10.0.0.26:18081/viewer`; 공개 포트는 이 HTTP listener 하나뿐이다.
- 2.0은 집 카메라 3/3 streaming, recorder 3/3 running이며 소방서와 염소장은 없다.
- 모바일 393×852 direct reload에서 세 video 모두 MSE, readyState 4, currentTime 증가를
  확인했다. 타일 선택은 1280×720 focus 한 개만 재생하고 닫으면 3개 MSE grid로 복귀했다.
- 1.0 핵심 unit PID/restart count와 source YAML SHA-256은 시작 전후 동일했고 기존 8대가
  계속 online이었다.
- 전체 Go, Web 55 tests/lint/build, Windows Viewer 23 tests/build와 container smoke가 통과했다.
- 운영 절차와 증거는 [Docker 카나리 운영 문서](../../2026-08-09_camstation2-docker-canary-operations.md)에 있다.
