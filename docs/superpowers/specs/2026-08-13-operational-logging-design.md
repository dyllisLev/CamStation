# CamStation 2.0 운영 관측 로거 설계

## 목표

CamStation 2.0의 카메라 입력부터 Windows Viewer 재생까지를 서버의 영속 로그에서 몇 주 뒤에도 같은
시간축과 playback session으로 재구성할 수 있게 한다. 로그는 사람이 `tail`로 읽을 수 있고 JSONL
처리기로 집계할 수 있어야 하며, Viewer PC는 서버에 진단을 보내지 못하는 마지막 구간만 보완한다.

운영 Viewer의 기준은 `monitoring-pc`다. 승인 target profile을 복구해 공식 Windows target wrapper로
NUC/session 1, Viewer Service와 실제 ProgramData 로그를 확인한다. 현재 설치본이 새 schema 이전이면
소스 검증과 운영 배포 완료를 구분한다.

## 공통 로그 계약

모든 신규 운영 레코드는 한 줄 JSON으로 기록한다. 공통 필드는 다음으로 제한한다.

- `timestamp`, `level`, `component`, `event`
- 선택적 `correlationId`, `sessionId`, `viewerId`, `cameraId`, `streamName`
- 선택적 `transport`, `phase`, `state`, `attempt`, `durationMs`, `retryMs`
- 선택적 `frame`, `mediaTimeMs`, `sizeBytes`, `errorCode`, `message`
- 반복 message의 선택적 `messageFingerprint`, `suppressedCount`, `windowMs`

level은 `debug`, `info`, `warn`, `error`, `off` 다섯 개다. 전역 기준은
`CAMSTATION_LOG_LEVEL`이고 기본은 `info`다. `CAMSTATION_LOG_LEVELS`는
`playback=debug,stream.live_warm=debug` 같은 comma-separated override를 받는다. component는 점으로
계층화하고 가장 긴 prefix가 이긴다. 값·component·중복 key가 잘못되면 daemon은 시작을 거부한다.
기존 `CAMSTATION_PLAYBACK_LOG_LEVEL`은 명시적 `playback` override가 없을 때만 호환 입력으로 사용한다.

Windows Viewer Service도 같은 level 의미와 component 규칙을 사용하지만 daemon 정책을 상속하지 않는다.
기본은 `warn`이고, 서버만으로 설명되지 않는 장애 재현 중에만 `viewer.playback=debug`,
`viewer.control=debug`를 선택한다.

## 저장과 보존

daemon은 stdout과 영속 JSONL 파일에 같은 레코드를 쓴다. 기본 파일은 DB 디렉터리 아래
`logs/camstationd.jsonl`이며 public API에는 경로를 노출하지 않는다. 기본 회전은 25 MiB×8개이고
환경 변수로 엄격한 범위 안에서 조절한다. DB와 같은 bind volume이므로 Docker container recreate에도
남는다. stdout의 기존 10 MiB×3 Docker 회전은 즉시 관찰용 보조 복사본으로 유지한다.

Viewer Service는 `C:\ProgramData\CamStation\Viewer\Logs`의 기존 ACL 경계에서 기본 5 MiB×3 회전을
사용한다. Electron/renderer가 파일 경로를 임의로 열지 않고 인증된 management pipe의
`diagnostic_event`로 레코드를 보내면 Service가 검증·filter·회전·append한다.

DB `events`는 운영 상태 전이와 incident 후보를 위한 저장소이지 고빈도 trace 저장소가 아니다.
정상 heartbeat 매회 기록은 제거하고 최초 연결, control/viewer/renderer/stream 건강 상태 변화만 남긴다.

## 카메라에서 서버까지

`go2rtc` stdout/stderr는 기존 credential redaction 뒤 공통 logger의 `stream.go2rtc` component로
감싼다. 원본 line에서 안전하게 판별 가능한 level만 매핑하고 message는 길이를 제한한다.

live-warm과 recorder ffmpeg에는 bounded progress 출력을 사용한다. 최초 media progress는 `info`,
이후 주기 progress는 `debug`로 남긴다. worker 시작은 process spawn 사실이고 media 연결 성공으로
표현하지 않는다. 종료 시 실행 시간, attempt, 다음 retry를 남긴다. ffmpeg stderr는 bounded scanner로
읽고 URL·credential을 redact한 뒤 warning/error만 평시에 보존하며 전체 command와 raw input/output
path는 기록하지 않는다.

RTSP packet의 DTS/PTS가 큰 폭으로 역행해도 장시간 worker의 timestamp 축이 고정되지 않도록 recorder와
live-warm 입력은 wall clock timestamp를 사용한다. 녹화 codec 표본에 B-frame이 없는 것을 배포 전에
확인하고, recorder의 30분 `segment_atclocktime` 분할은 실제 격리 stream으로 검증한다. 동일한 ffmpeg
message는 worker별 첫 record만 원문을 redact해 남기고, 숫자·hex를 정규화한 fingerprint 기준으로 1분 동안
억제한 뒤 `suppressedCount` summary 한 건으로 축약한다. process/segment lifecycle failure는 이 억제 대상이
아니다.

recorder는 segment open/close, 안전한 filename, 종료 크기와 실패 code를 남긴다. file growth 확인은
ffmpeg progress와 segment close를 함께 사용하고, active path는 로그에 넣지 않는다.

## 서버에서 Viewer까지

브라우저 playback diagnostic은 공통 daemon logger를 사용한다. server record에는 기존 session,
stream, transport, phase, attempt, elapsed, fallback/reconnect 정보와 요청 client IP를 안전한 필드로
남긴다. debug는 socket/signaling/first track/first media/session close를, info 이상은 시작·성공·실패·
소진을 유지한다.

Viewer renderer는 같은 playback diagnostic을 preload bridge에도 보낸다. Electron main은 이를
lease에 묶어 management pipe로 보내고 Service가 `viewer.playback` JSONL로 기록한다. 같은
`sessionId`가 server와 `monitoring-pc` 양쪽에 존재하므로 시간과 session으로 직접 join할 수 있다.

Service는 control connection, lease, renderer lifecycle, command 결과와 stream phase/transport 변화를
상태 변화에 한해 기록한다. 5초 renderer pulse나 10초 server heartbeat의 정상 반복은 info에 쓰지
않고 debug에서도 주기 summary로 제한한다.

## 보안과 실패 정책

- camera URL, URL userinfo/query, Authorization, token/nonce, SDP/ICE candidate, PEM, response body,
  전체 process args와 runtime path는 금지한다.
- 외부 입력인 browser/renderer diagnostic은 allowlist field, enum, 길이, 숫자 범위를 검증한다.
- logging failure가 영상 재생을 중단시키지는 않지만 Service/daemon 자체 오류 record와 상태로 드러나야 한다.
- record는 4 KiB 이하, child-process line은 64 KiB 이하로 제한하고 oversized 입력은 버리면서 counter
  또는 안전한 error event를 남긴다.
- 로그 내용이나 파일 경로를 새 public API로 제공하지 않는다.

## 서버 운영 감시기

영속 상세 로그와 별도로 host의 1분 systemd timer가 한 번 실행될 때 한 개의 bounded JSON record를 만든다.
감시기는 상태를 바꾸거나 자동 재시작하지 않고 다음의 안전한 숫자만 수집한다.

- container running/healthy 여부, restart count와 image tag
- `/api/health`, 활성 camera 수, stream expected/ready, recorder enabled/running/current 수
- online Viewer 수와 대상 Viewer의 agent/control/viewer/renderer 건강 여부
- 활성 camera별 `live` 또는 `focus` 후보 중 `playing`이고 최근 90초 안에 `lastProgressAt`이 전진한 수
- daemon JSONL의 최근 timestamp, 직전 5분 warn/error 수와 `persistent_write_failed` 수
- SQLite source of truth의 활성 recording/finalizing segment 시작 age와 최신 ready 종료 age
- state/media filesystem 사용률과 남은 bytes

감시기는 API 응답과 상세 log를 메모리에서 즉시 aggregate하고 원문을 복사하지 않는다. camera 이름·host,
stream 이름, Viewer ID, process argument, 파일 경로와 오류 message는 출력 record에 넣지 않는다. 결과에는
`timestamp`, `status`, 각 count/age/percent와 정해진 alert code만 존재한다. alert code는 API/JSON parsing,
container down/unhealthy/restart, logger stale/write failure, camera/stream/recorder/Viewer 부족,
`segmentMinutes+300초`를 넘긴 녹화 순환, state/media disk 임계치로 한정한다. watcher는 SQLite를 read-only로
열며 stream identity는 출력하지 않고 stale/current/ready 수와 최대 age만 남긴다.

결과는 daemon JSONL과 분리된 host 파일 `operational-watch.jsonl`에 active 포함 10 MiB×4로 원자 append하고
동일 record를 stdout/journald에도 쓴다. `flock`으로 중복 실행을 막고 네트워크·subprocess timeout을 각 5초
이하로 제한한다. 한 probe가 실패해도 가능한 나머지 숫자를 수집하며 전체 status는 `ok`, `degraded`,
`error` 중 가장 높은 상태가 된다. 감시기 실패나 설치/제거는 CamStation container를 재시작하지 않는다.

timer는 boot 후 2분 뒤 시작해 1분마다 실행하고 persistent timer로 missed run 하나를 보완한다. API base,
container와 host path는 root-only 환경 파일에서 명시하며 script 자체에 production 주소나 경로를 넣지 않는다.
설치 전후 script/config/unit hash, timer enabled/active, 단독 service exit와 두 개 이상의 연속 표본을 확인한다.

## 합격 기준

- level parser, longest-prefix override, invalid startup, JSON schema, redaction, concurrent write와 회전이
  테스트된다.
- synthetic ffmpeg progress/error/exit에서 stream별 first media, progress, retry와 recorder segment
  close record가 생성되고 raw URL/path가 없다. 반복 1,000건은 첫 record와 주기 summary로 제한된다.
- Viewer 경고·오류 또는 임시 debug가 발생한 playback session은 server와 Viewer 로컬 record에 같은
  session ID가 남으며 threshold가 독립적으로 동작한다.
- 반복 정상 Viewer heartbeat가 DB event 수를 증가시키지 않고 상태 변화는 한 건씩 남는다.
- server JSONL은 process 재기동 뒤 append되고 rotation 상한을 지킨다. Viewer log는 기존 ACL·3-file
  상한을 지킨다.
- Go race/full test, Web test/lint/build, Viewer test/build, Linux daemon build와 Windows service/
  bootstrap cross-build가 통과한다.
- 배포 전 `monitoring-pc`의 기존 로그 기준선, 배포 후 새 record/rotation과 장애 표본의 session join을
  실제 파일로 확인한다. 운영 감시기는 fixture test와 실제 1분 표본에서 정상 count와 의도한 alert만
  기록하고 상세 API/log 원문이나 금지 필드를 포함하지 않아야 한다. 30분 경계를 넘긴 fixture는
  `recorder_segment_stale` 오류가 되고 stream identity는 노출되지 않아야 한다.
