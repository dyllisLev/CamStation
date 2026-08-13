# CamStation 2.0 운영 관측 로거 구현 계획

**목표:** 카메라→서버→Windows Viewer의 연결과 media 진행을 level 기반 JSONL로 영속 기록하고,
정상 heartbeat DB spam을 상태 변화 기록으로 교체한다.

**설계:** [운영 관측 로거 설계](../specs/2026-08-13-operational-logging-design.md)

## 1. 공통 daemon logger

- [x] `internal/opslog`에 level, component-prefix override, 고정 record schema, redaction, dual writer와
      bounded rotating JSONL writer의 실패 테스트를 작성한다.
- [x] DB directory 기반 기본 log root와 환경 변수 validation을 composition root에 연결한다.
- [x] startup/shutdown/config record를 남기고 invalid level/size/file count는 daemon 시작 전에 실패시킨다.
- [x] `CAMSTATION_PLAYBACK_LOG_LEVEL` 호환 우선순위와 compose/env 예시를 테스트한다.

## 2. 카메라 ingest와 recorder 관측

- [x] go2rtc redacting line writer가 공통 logger로 level·component·bounded message를 전달하게 한다.
- [x] live-warm ffmpeg에 progress pipe를 추가하고 first media, periodic progress, stderr warning/error,
      exit duration와 bounded retry를 stream별로 기록한다.
- [x] recorder ffmpeg에 progress pipe와 bounded stderr 분류를 추가하고 worker lifecycle, retry,
      segment open/close size를 기록한다.
- [x] synthetic runner/scanner 테스트에서 URL credential, local input/output path, raw process args가
      어느 level에도 나타나지 않는지 검사한다.

## 3. playback과 Windows Viewer local log

- [x] 기존 playback sink를 공통 logger adapter로 바꾸되 standalone route tests와 legacy env를 유지한다.
- [x] Viewer preload bridge에 allowlisted `reportDiagnostic`을 추가하고 playback event를 HTTP와 local
      management pipe 양쪽에 같은 session ID로 보낸다.
- [x] Viewer Service `diagnostic_event`를 no-op에서 검증된 `WriteViewer`로 바꾸고 level filter와
      매-write rotation을 적용한다.
- [x] Service control/lease/renderer/command/stream 상태 변화 record를 추가하고 정상 pulse 반복은
      억제한다.
- [x] Viewer log schema, invalid payload, redaction, lease authorization, session correlation 및
      기본 warn·5 MiB×3 회전 회귀를 테스트한다.

## 4. DB 운영 event 정리

- [x] route-local transition tracker의 최초 관찰, 변화, 반복 억제와 동시 heartbeat 테스트를 작성한다.
- [x] 매 heartbeat `viewer heartbeat` append를 제거하고 Viewer/control/renderer/stream 요약 변화만
      DB event로 저장한다.
- [x] `/logs`, Viewer 상태와 command/update heartbeat 동작이 기존대로 유지되는지 route/store 테스트로
      검증한다.

## 5. 구성·문서·정적 검증

- [x] Docker compose/env와 운영 문서에 default/soak component level, 영속 경로, 회전 용량,
      rollback 방법을 추가한다.
- [x] `docs/07-implementation-status.md`를 실제 구현 상태와 맞춘다.
- [x] `gofmt`, focused Go/TS tests, changed-package plus `camstationd` race tests, `go test ./...`,
      Web test/lint/build, Viewer test/build, daemon build와 Windows cross-build를 수행한다.
- [x] generated embedded Web assets가 source build와 일치하고 `git diff --check`가 통과하는지 확인한다.

## 6. 실행 표면 검증

- [x] 격리된 temp DB/log root로 daemon을 시작해 append, filter, rotation과 재기동 보존을 검증한다.
- [x] synthetic camera/playback 요청으로 동일 session의 server JSONL과 Viewer Service test log를 join한다.
- [x] 공식 wrapper로 `monitoring-pc` 기존 로그를 읽기 전용 재감사하고 현재 설치본이 새 level schema
      이전임을 확인한다.
- [x] fixture 입력을 받는 host watcher와 1분 systemd oneshot/timer, 회전·flock·금지 필드 테스트를 구현한다.
- [x] 사용자 승인에 따라 clean `main`의 immutable server image와 Viewer 2.0.26 MSI를 만들고 hash를 고정한다.
- [x] 운영 서버에 exact Compose rollback을 남겨 server logger와 watcher를 배포하고 camera/recorder/Viewer
      8/8 및 log append를 검증한다.
- [x] 공식 wrapper로 `monitoring-pc`를 2.0.26으로 업그레이드하고 warn·5 MiB×3 정책, identity/config,
      Service/interactive Viewer와 8/8 media progress를 검증한다.
- [x] 즉시·5분·15분 이상 watcher/server/Viewer 표본을 비교하고 배포·잔여 위험·복구 경로를 문서화한다.

## 7. 배포 후 timestamp·보존량 개선

- [x] FFmpeg 폭증 signature와 장시간 열린 DB segment를 identity 비노출 집계로 재현하고 실제 파일 증가를
      교차 검증한다.
- [x] 전체 녹화 표본의 B-frame 0을 확인하고 recorder/live-warm RTSP 입력에 wall-clock timestamp를 적용한다.
- [x] 숫자·hex를 정규화한 message fingerprint와 worker별 1분 rate limit을 공통 logger에 추가하고,
      1,000개 반복이 첫 record+summary로 축약되는 회귀를 고정한다.
- [x] watcher가 SQLite를 read-only로 집계해 current/latest-ready age와 stale count를 기록하고,
      30분+300초 초과를 `recorder_segment_stale`로 판정하는 fixture를 추가한다.
- [x] immutable server image와 watcher script/env를 운영에 전환하고 열린 파일의 finalize 및 8/8 수렴을 확인한다.
- [x] 다음 30분 경계에서 8개 모두 segment를 닫는지, error 증가율·예상 보존기간과 Viewer 수신을 확인한다.
