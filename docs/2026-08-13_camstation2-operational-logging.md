# CamStation 2.0 운영 로그 관제

CamStation 2.0은 카메라 입력, 녹화와 Viewer playback을 서버의 level 기반 영속 JSONL로 기록한다.
Windows Viewer 로컬 로그는 서버에 보고할 수 없는 마지막 구간만 보완하는 작은 블랙박스다.

> 몇 주간의 운영 관제와 장기 보존은 서버 로그를 기준으로 한다. `monitoring-pc`는 Viewer가 서버에
> 접속하기 전 종료되거나 renderer/GPU, management pipe, 네트워크 단절로 진단을 전송하지 못한 경우에만
> 확인한다. 설정 변경은 daemon 또는 Viewer Service 재시작이 필요하므로 승인된 배포 절차로 수행한다.
> 로그에는 raw URL, 자격증명, token, SDP/ICE, 전체 process args와 runtime path를 남기지 않는다.

## 현재 운영 반영 상태

- 서버: `camstation:2.0.0-rc.20260825.21-viewer-self-healing`, source revision `0f5a2de`,
  image ID `sha256:2db36aabe704b290dd904bd1ba251fa43b3e863d9b00c85f2df5504b69d60179`
- daemon 로그: `/var/lib/camstation2-production/data/logs/camstationd.jsonl`, 64 MiB×32
- 자동 추이: `/var/lib/camstation2-production/data/logs/operational-watch.jsonl`, 1분, 10 MiB×4;
  `camstation-log-watch.timer` enabled/active
- `monitoring-pc`: Viewer 2.0.28, Service Running/Auto, 로컬 로그 `warn`, override 없음, 5 MiB×3;
  새 Viewer 창은 Windows 최대화 상태로 시작
- watcher source revision `a8b3eae`, 설치 SHA-256
  `a1c62ea1e8f835eb9ea787ffb50f745e54ab1b80018eb1f4bd34004b8abceafa`
- 2026-08-25 09:47:18 KST 최종 표본: container healthy/restart 0, camera·stream·recorder 8/8,
  Viewer healthy 1/1·receiving 8/8, recording current/ready 8/8, stale 0. Windows clock skew 때문에
  watcher는 `degraded`와 `viewer_clock_skew` 하나만 유지한다.

monitoring PC의 UTC는 같은 HTTP 왕복에서 서버보다 102.197초 느리고 W32Time source는 동기화되지 않은
상태였다. watcher는 server receipt와 독립적인 control success 시각 차이에서 15초 안전 여유를 뺀
bounded correction을 Viewer/renderer/progress에만 적용한다. 실제 30초 management stale 기준은 유지하고
clock skew 자체는 숨기지 않는다. OS 시간이나 W32Time 설정은 이번 배포에서 변경하지 않았다.

`.21` container 재생성 때 기존 recorder의 직렬 worker 종료 최악값이 Docker 45초 stop budget을 넘겼다.
09:00 KST partial 8개 중 4개는 failed recovery, 4개는 ready지만 `ffprobe` 실패다. 이후 09:02~09:30
8개 segment는 파일 존재·DB size 일치·`ffprobe` 성공이고 09:30 current 8개는 모두 증가 중이다. 추가
container 재시작 전에는 recorder 종료를 bounded parallel로 바꾸거나 충분한 stop grace를 증명해야 한다.

### Paseo 매일 06시 24시간 감사

- schedule ID: `ab13f419`; 상태 `active`; 만료와 최대 실행 횟수 없음
- cadence: 매일 `06:00 Asia/Seoul`; 감사 구간은 `[전날 06:00, 당일 06:00)`의 고정 24시간
- 결과: 각 Paseo run 이력에는 `문제 지점·예상 원인·06시 현재 상태`를 최대 3건, 짧은 한국어 문장으로
  남긴다. 정상일 때는 공식 Viewer의 300초 이상 미수신 수와 현재 수신 수만으로 끝낸다.
- 범위: 회전 daemon의 `playback`, Viewer heartbeat/renderer/stream 전이와 watcher 표본을 중심으로 본다.
  카메라 media·recorder는 같은 구간의 source 정상 여부를 배제하는 최소 대조 증거로만 사용하며 카메라
  연결 오류 자체는 결과에 싣지 않는다. 서버 증거만으로 지속 Viewer 장애 원인이 불명일 때만
  `monitoring-pc`를 공식 wrapper로 읽기 전용 대조한다.
- 실행 상한: daemon/watcher 종류별 단일 streaming pass, 사건 최대 3건, 수집 15분·전체 25분과 모든
  외부 명령 timeout을 적용한다. 상한 밖의 자료는 정상으로 보간하지 않고 한 줄 증거 한계로 남긴다.
- 안전 경계: schedule은 운영 서비스·container·DB·파일·PC·Git을 변경하거나 자동 복구하지 않는다.

2026-08-14 clean run `e6d88c79-7408-44bd-b64b-d437ea069aca`의 전체 NVR 감사 형식은 과거 기준선이며,
현재 schedule의 간결한 서버↔Viewer 브리핑 형식으로 사용하지 않는다.

조회 명령은 `paseo schedule inspect ab13f419`, 실행 이력은 `paseo schedule logs ab13f419`다.

## 초기 관제를 시작하기

서버의 전역 level은 `info`로 유지하고, 2.0 초기 관제 기간에 필요한 component만 `debug`로 올린다.

```dotenv
CAMSTATION_LOG_LEVEL=info
CAMSTATION_LOG_LEVELS=playback=debug,stream.go2rtc=debug,stream.live_warm=debug,recorder.ffmpeg=debug
CAMSTATION_LOG_DIR=/var/lib/camstation/data/logs
CAMSTATION_LOG_MAX_MB=64
CAMSTATION_LOG_FILES=32
```

Docker에서는 `CAMSTATION_LOG_DIR`이 state bind mount 안에 있으므로 container recreate 후에도
`camstationd.jsonl`과 회전본이 남는다. active 파일을 포함해 최대 32개, 약 2 GiB가 상한이다. 동일한
FFmpeg warning/error는 worker별 첫 message만 redact해 기록하고, 이후 숫자·hex가 다른 같은 signature는
1분 단위 `messageFingerprint`·`suppressedCount` summary로 축약한다. worker exit와 segment 실패는
rate limit하지 않는다.

Windows Viewer는 상시 상세 trace를 남기지 않는다. 코드 기본값은 `warn`, component override 없음,
파일당 5 MiB·active 포함 3개다. 배포 절차에서 값을 명시하려면 elevated PowerShell에서 기존
`Environment` 값을 보존한 뒤 아래 네 항목을 합친다.

```powershell
$serviceKey = "HKLM:\SYSTEM\CurrentControlSet\Services\CamStationViewerService"
$existing = @((Get-ItemProperty -LiteralPath $serviceKey -Name Environment -ErrorAction SilentlyContinue).Environment)
$preserved = @($existing | Where-Object { $_ -notmatch '^CAMSTATION_VIEWER_LOG_(LEVEL|LEVELS|MAX_MB|FILES)=' })
$logging = @(
  "CAMSTATION_VIEWER_LOG_LEVEL=warn"
  "CAMSTATION_VIEWER_LOG_LEVELS="
  "CAMSTATION_VIEWER_LOG_MAX_MB=5"
  "CAMSTATION_VIEWER_LOG_FILES=3"
)
New-ItemProperty -LiteralPath $serviceKey -Name Environment -PropertyType MultiString `
  -Value @($preserved + $logging) -Force | Out-Null
Restart-Service -Name CamStationViewerService
if ((Get-Service -Name CamStationViewerService).Status -ne "Running") {
  throw "CamStationViewerService did not return to Running"
}
```

Viewer는 `C:\ProgramData\CamStation\Viewer\Logs`에 `service.log`와 현재 interactive session의
`viewer-<session>-<sid-hash>.log`를 기록한다. 각 로그 스트림은 최대 약 15 MiB이며 정상 시 파일이
비어 있거나 증가하지 않아도 된다.

## 1분 자동 추이 감시를 설치하기

상세 daemon JSONL을 매번 사람이 읽지 않아도 되도록 host에 read-only watcher를 설치한다. watcher는
container를 재시작하거나 API를 변경하지 않는다. 공개 API와 상세 로그를 메모리에서 즉시 숫자로 줄이고,
camera/Viewer 이름·ID·host·stream 이름·오류 원문·경로는 결과에 넣지 않는다.

```bash
sudo install -D -o root -g root -m 0755 \
  scripts/production/camstation_log_watch.py \
  /usr/local/libexec/camstation-log-watch
sudo install -D -o root -g root -m 0644 \
  packaging/systemd/camstation-log-watch.service \
  /etc/systemd/system/camstation-log-watch.service
sudo install -D -o root -g root -m 0644 \
  packaging/systemd/camstation-log-watch.timer \
  /etc/systemd/system/camstation-log-watch.timer
sudo install -D -o root -g root -m 0600 \
  packaging/systemd/camstation-log-watch.env.example \
  /etc/camstation/camstation-log-watch.env
sudo systemctl daemon-reload
sudo systemctl enable --now camstation-log-watch.timer
sudo systemctl start camstation-log-watch.service
```

환경 파일의 API bind, container, SQLite DB, daemon/output log, state/media 경로는 실제 Compose mount와
맞아야 한다. 운영 기본은 1분 간격, API·Docker probe당 5초, logger freshness 180초,
Viewer/renderer heartbeat freshness 30초, Viewer progress 90초,
`operational-watch.jsonl` active 포함 10 MiB×4다. state는 2 GiB, media는 20 GiB 미만이거나 사용률
90% 이상이면 경고하고 95% 이상이면 오류로 올린다. watcher는 DB를 read-only로 열어 활성 segment의
시작 age와 최신 ready 종료 age를 집계하며, 설정된 segment 길이보다 300초 이상 늦으면
`recorder_segment_stale` 오류를 낸다. 결과에는 stream identity가 아니라 count와 최대 age만 남는다.

설치 직후 timer와 두 연속 표본을 확인한다.

```bash
systemctl is-enabled camstation-log-watch.timer
systemctl is-active camstation-log-watch.timer
systemctl show camstation-log-watch.timer -p LastTriggerUSec -p NextElapseUSecRealtime
sudo python3 - <<'PY'
import json
from collections import deque

path = "/var/lib/camstation2-production/data/logs/operational-watch.jsonl"
with open(path, encoding="utf-8") as source:
    for line in deque(source, maxlen=2):
        print(json.dumps(json.loads(line), ensure_ascii=False, separators=(",", ":")))
PY
```

watcher만 원복할 때는 제품 container를 건드리지 않는다.

```bash
sudo systemctl disable --now camstation-log-watch.timer
sudo systemctl reset-failed camstation-log-watch.service
sudo systemctl daemon-reload
```

파일을 제거해야 한다면 timer가 정지된 것을 확인한 후 배포 시 기록한 exact path와 hash만 대상으로 한다.

## 실시간으로 확인하기

서버에서는 영속 JSONL을 follow하고 `warn` 이상을 우선 확인한다. 컨테이너 이름은 현재 운영 이름으로
지정한다.

```bash
CAMSTATION_CONTAINER=camstation2
docker exec "$CAMSTATION_CONTAINER" tail -n 200 -F /var/lib/camstation/data/logs/camstationd.jsonl \
  | python3 -c '
import json, sys
for line in sys.stdin:
    try:
        record = json.loads(line)
    except json.JSONDecodeError:
        continue
    if record.get("level") in {"warn", "error"}:
        print(json.dumps(record, ensure_ascii=False, separators=(",", ":")), flush=True)
'
```

특정 카메라의 입력과 녹화를 같이 본다.

```bash
CAMSTATION_STREAM='camera-a-live'
sudo env CAMSTATION_STREAM="$CAMSTATION_STREAM" python3 - <<'PY'
import json
import os
from collections import deque

target = os.environ["CAMSTATION_STREAM"]
base = target.removesuffix("-live")
components = {"stream.live_warm", "recorder", "recorder.ffmpeg"}
path = "/var/lib/camstation2-production/data/logs/camstationd.jsonl"
with open(path, encoding="utf-8") as source:
    lines = deque(source, maxlen=500)
for line in lines:
    record = json.loads(line)
    stream = record.get("streamName", "")
    if record.get("component") in components and (stream == target or stream.startswith(base)):
        print(json.dumps(record, ensure_ascii=False, separators=(",", ":")))
PY
```

서버 로그로 원인이 설명되지 않을 때만 `monitoring-pc`에서 Service와 가장 최근 Viewer 파일을 확인한다.

```powershell
$root = "C:\ProgramData\CamStation\Viewer\Logs"
$viewerLog = Get-ChildItem -LiteralPath $root -Filter "viewer-*.log" |
  Sort-Object LastWriteTimeUtc -Descending | Select-Object -First 1
if ($null -eq $viewerLog) { throw "Viewer session log is missing" }
$path = $viewerLog.FullName
Get-Content -LiteralPath $path -Tail 200 -Wait | ForEach-Object {
  $record = $_ | ConvertFrom-Json
  if ($record.level -in @("warn", "error")) {
    $record | ConvertTo-Json -Compress
  }
}
```

두 번째 PowerShell 창에서는 같은 filter를 사용하되 `$path = Join-Path $root "service.log"`로 바꾼다.

자동 추이는 최신 record를 보거나 시간대별 status/alert 수를 집계한다.

```bash
WATCH_LOG=/var/lib/camstation2-production/data/logs/operational-watch.jsonl
sudo env WATCH_LOG="$WATCH_LOG" python3 - <<'PY'
import json
import os
from collections import Counter, deque

keys = ("timestamp", "status", "alerts", "cameras", "streams", "recorders", "recordingSegments", "viewers", "logs", "disk")
with open(os.environ["WATCH_LOG"], encoding="utf-8") as source:
    rows = [json.loads(line) for line in source]
for row in deque(rows, maxlen=20):
    print(json.dumps({key: row.get(key) for key in keys}, ensure_ascii=False, separators=(",", ":")))
print(dict(Counter(row.get("status") for row in rows)))
PY
```

`status=ok`는 해당 시점의 camera/stream/recorder/Viewer 수와 media progress, logger, disk가 모두
기준을 만족했다는 뜻이다. `degraded` 또는 `error`는 `alerts`의 고정 code로 원인을 좁힌 뒤 같은 시각의
`camstationd.jsonl*`을 상세 조사한다.

Viewer의 문자열 상태가 `running/ready`여도 heartbeat가 30초 이상 오래되면 watcher는 해당 Viewer를
healthy로 세지 않는다. 이때 `viewer_heartbeat_stale` 또는 `viewer_renderer_stale`을 내고,
`viewerHeartbeatFresh`, `rendererHeartbeatFresh`, 두 최신 heartbeat age를 함께 기록한다. 과거 stream
진행이 남아 있어 `receivingCameras`가 8이어도 관리 채널이 stale이면 정상으로 판정하지 않는다.

Viewer에 경고·오류가 있거나 장애 조사 중 debug를 켰다면 서버와 Viewer에 같은 playback `sessionId`가
남는다. Viewer log에서 세션을 고른 뒤 서버에서 같은 값을 찾으면 signaling, first media, fallback,
recovery, close를 한 흐름으로 연결할 수 있다.

새 playback record는 `documentId`로 같은 renderer 문서의 여러 카메라 세션을 묶고, `surface`로
`official_viewer`와 `viewer_browser`·운영자 미리보기를 분리한다. `candidateRole`, `attemptGeneration`,
`resubscribeGeneration`, `terminalReason`은 fallback/probe와 늦은 callback, 명령 재구독, 실제 종료 원인을
구분한다. Viewer 로컬 JSONL의 `leaseFingerprint`는 Service가 검증한 lease ID의 짧은 SHA-256 지문이며
원본 lease ID를 기록하지 않는다. 이 필드가 없는 과거 revision은 `sessionId`·client IP까지만 근거로 삼고
공식 Viewer 귀속을 확정하지 않는다.

```bash
PLAYBACK_SESSION='playback-example1234'
sudo env PLAYBACK_SESSION="$PLAYBACK_SESSION" python3 - <<'PY'
import glob
import json
import os

target = os.environ["PLAYBACK_SESSION"]
for path in sorted(glob.glob("/var/lib/camstation2-production/data/logs/camstationd.jsonl*")):
    with open(path, encoding="utf-8") as source:
        for line in source:
            record = json.loads(line)
            if record.get("sessionId") == target:
                print(json.dumps(record, ensure_ascii=False, separators=(",", ":")))
PY
```

예시 session 값은 실제 Viewer JSONL의 `sessionId`로 바꾼다. 로그 timestamp는 UTC RFC3339이고 운영
보고 시 KST는 UTC에 9시간을 더한다.

## 무엇을 정상과 장애로 볼지

| 경로 | 정상 신호 | 조사 신호 |
| --- | --- | --- |
| go2rtc | `ready`, INFO child output | `startup_timeout`, error/warn child output, process exit |
| live warm | `media_started`, debug `media_progress` | `ffmpeg_warning`, `ffmpeg_error`, `worker_exited`, `retry_scheduled` |
| recorder | `media_started`, `segment_opened`, `segment_closed`; watcher freshness 정상 | process/segment failure, `recorder_segment_stale`, disk pause, 반복 retry |
| server playback | `attempt_started` → `playback_started`; debug `first_media` | `attempt_failed`, `episode_exhausted`, fallback/reconnect 증가 |
| Viewer 상태(서버) | heartbeat의 control/renderer/stream 정상 상태 | 상태 변화 event의 `degraded`, `stalled`, 진행 시각 정지 |
| Viewer 로컬 블랙박스 | 평시 warn/error 없음; 빈 파일도 정상 | 시작 실패, pipe reject, renderer/process/GPU 오류, 서버 미전송 장애 |
| persistent logger | active JSONL의 timestamp/size 증가 | stdout `opslog/persistent_write_failed`, active 파일 갱신 정지 |

worker spawn은 media 성공이 아니다. `media_started` 또는 Viewer의 `first_media`/진행 신호까지 확인해야
연결 성공으로 판정한다. 같은 정상 heartbeat와 5초 renderer pulse는 INFO에 반복 저장하지 않는다.

## Level과 보존량을 조정하기

level 순서는 `debug < info < warn < error < off`다. `CAMSTATION_LOG_LEVELS`와
`CAMSTATION_VIEWER_LOG_LEVELS`는 `component=level` 목록이며 가장 긴 component prefix가 이긴다.
잘못된 level, 중복 component, 범위를 벗어난 회전 설정은 시작 시 오류로 처리한다.

초기 관제가 끝난 서버는 다음처럼 평시로 낮춘다.

```dotenv
CAMSTATION_LOG_LEVEL=info
CAMSTATION_LOG_LEVELS=
CAMSTATION_LOG_MAX_MB=25
CAMSTATION_LOG_FILES=8
```

Viewer 평시 값은 다음 네 줄이다. Viewer는 daemon용 `CAMSTATION_LOG_LEVEL(S)`를 상속하지 않는다.

```text
CAMSTATION_VIEWER_LOG_LEVEL=warn
CAMSTATION_VIEWER_LOG_LEVELS=
CAMSTATION_VIEWER_LOG_MAX_MB=5
CAMSTATION_VIEWER_LOG_FILES=3
```

서버 로그만으로 원인을 특정할 수 없는 장애를 재현할 때만 같은 PowerShell 절차에서 아래 level을 적용하고
Service를 재시작한다. 10~30분 재현과 증거 수집이 끝나면 즉시 위 평시 값으로 원복한다.

```text
CAMSTATION_VIEWER_LOG_LEVEL=info
CAMSTATION_VIEWER_LOG_LEVELS=viewer.playback=debug,viewer.control=debug
CAMSTATION_VIEWER_LOG_MAX_MB=5
CAMSTATION_VIEWER_LOG_FILES=3
```

기존 `CAMSTATION_PLAYBACK_LOG_LEVEL`은 서버 playback 호환 입력으로만 남는다. 새 구성은 component
override를 사용하며, `playback`이 양쪽에 있으면 `CAMSTATION_LOG_LEVELS`가 우선한다.

rate limit summary는 원래 event와 level을 유지하므로 level filter와 alert 의미는 바뀌지 않는다.
`message`가 있는 첫 record에서 원인을 확인하고, 같은 `messageFingerprint`의 이후 record는
`suppressedCount`와 `windowMs`로 빈도를 판단한다.

## 문제가 생겼을 때

- daemon이 시작하지 않으면 level 문자열, 중복 component, `LOG_MAX_MB`의 1–1024 범위와
  `LOG_FILES`의 1–64 범위를 먼저 확인한다.
- 서버 stdout은 즉시 관찰용 보조 복사본이다. 장기 조사는 container 수명에 종속된 `docker logs`가
  아니라 state volume의 `camstationd.jsonl*`을 사용한다.
- Viewer 평시 `warn`에서는 `service.log`가 없거나 파일이 비어 있어도 정상이다. 장애가 발생했는데도
  Viewer 파일이 전혀 없으면 interactive Viewer lease 획득과 파일 ACL 준비를 확인한다.
- 평시 playback session이 서버에만 있는 것은 정상이다. Viewer 경고·오류 또는 임시 debug session이
  한쪽에만 있으면 management pipe나 HTTP playback diagnostic 전달을 조사한다.
- 로깅 실패는 영상을 중단시키지 않도록 설계됐지만 `logging_unavailable` 또는 Service error record는
  운영 장애로 다룬다. daemon stdout의 `opslog/persistent_write_failed`도 같은 우선순위로 조사한다.
- `recorders running/current`가 정상인데 `recorder_segment_stale`가 있으면 process 수로 정상 판정하지
  않는다. watcher의 `oldestCurrentAgeSeconds`와 `oldestReadyAgeSeconds`, 같은 시각의 `segment_closed`를
  대조하고 열린 임시 파일을 수동 삭제하지 않는다.

## 검증 근거와 경로

### E-001

- observed_at: 2026-08-13
- source_type: file/test
- source_ref: `internal/opslog`, `internal/stream`, `internal/recorder`, `internal/viewerservice`
- content_hash: n/a
- repro_command: `go test ./internal/opslog ./internal/stream ./internal/recorder ./internal/viewerservice`
- finding: level, redaction, 회전, ffmpeg progress, Viewer 상태 전이가 단위 테스트로 검증된다.

### E-002

- observed_at: 2026-08-13
- source_type: file/test
- source_ref: `web/tests/playbackDiagnostics.test.ts`, `cmd/camstationd/routes_viewer_transitions_test.go`
- content_hash: n/a
- repro_command: `cd web && npm test; cd .. && go test ./cmd/camstationd`
- finding: 같은 playback session이 서버와 Viewer record에 전달되고 반복 heartbeat는 DB event를 늘리지 않는다.

### E-003

- observed_at: 2026-08-13
- source_type: isolated runtime
- source_ref: network-disabled Docker run with temporary DB/state and final static daemon
- content_hash: daemon SHA-256 `06384751858306dcbbf9710cf73c4b20e012757c4d43831d31786a877322bda0`
- repro_command: final daemon `-probe-only` repeated against the same temporary state root
- finding: 세 번의 재기동 뒤 JSONL은 5,347 bytes/33 records로 append됐고 `startup_started`, `ready`,
  `probe_failed`가 각각 3개였다. 모든 줄이 JSON schema를 만족했고 raw URL/path/credential 표본은 없었다.

### E-004

- observed_at: 2026-08-13
- source_type: command
- source_ref: official `monitoring-pc` target wrapper status and read-only system audit
- content_hash: audit script SHA-256 `734e06db749fcca4743f9f9ecee0a563d1a6f3408f2d0b0150a76660acdfa295`
- repro_command: `node scripts/windows/Invoke-CamStationWindowsTarget.mjs --target monitoring-pc --mode status`
- finding: `NUC` session 1은 Active, Viewer Service는 Running/Auto, control task와 driver listener 잔여물은
  0이다. ProgramData 로그는 2개·총 1,661 bytes였고 현재 설치본의 13개 record는 `level` 없는 이전
  schema이며 새 Viewer 로그 환경값은 명시돼 있지 않다.

### E-005

- observed_at: 2026-08-13
- source_type: file/test
- source_ref: `scripts/production/camstation_log_watch.py`, systemd service/timer packaging
- content_hash: 배포 후보 commit에서 고정
- repro_command: `PYTHONDONTWRITEBYTECODE=1 scripts/production/test-policy.sh`
- finding: 정상 8/8, stream/recorder/Viewer 부족과 logger write failure, 10 MiB×4 회전, credential이 든
  잘못된 URL 설정을 포함한 fixture가 통과한다. output은 count/age/percent와 고정 alert code만 포함하며
  camera/Viewer identity, host, stream, URL, 오류 원문과 경로를 포함하지 않는다.

### E-006

- observed_at: 2026-08-13 20:56 KST
- source_type: production runtime
- source_ref: `cctv` Docker inspect, daemon JSONL and host watcher JSONL
- content_hash: image `sha256:ad1bc15d0bbd3b6e3aac55660dccbb0b6109a9f075238c649e746ad7c9a09326`;
  Compose `710748d53eaa87168a9766294c2125bc9629b6b6533311595b74baa59ea1704d`
- finding: `.18-operational-logging` container는 healthy/restart 0이고 camera·stream·recorder·Viewer가
  8/8이다. watcher timer는 enabled/active이며 최신 6개 표본은 연속 `ok`, alert/warn/error/persistent
  write failure 0, Viewer progress age 3초다. Viewer MSI 중 Service 정지를 감지한 두 error 표본과 전환
  직후 약 6분의 6/8→8/8 수렴 표본도 삭제하지 않았다.

### E-007

- observed_at: 2026-08-13 20:52 KST
- source_type: production Windows runtime
- source_ref: official `monitoring-pc` target wrapper install audit and interactive capture
- content_hash: Viewer 2.0.26 MSI
  `4c5358a7c983f7decd8a097a663fa441fd7111dc27db6651330a61cefcda6502`; screenshot
  `2c6d7c5c04bbc8eba9aa6beb0a608284e550bb8f1a9e4743879a4ca4084d53db`
- finding: Viewer 2.0.26과 Service가 실행 중이고 warn·5 MiB×3 정책이 명시됐다. config/client identity는
  배포 전 hash와 같으며 실제 창과 서버 telemetry 모두 8개 WebRTC stream의 진행을 확인했다.

### E-008

- observed_at: 2026-08-14 07:57~08:04 KST
- source_type: production runtime
- source_ref: `cctv` aggregate Docker/DB/log/watcher and `ffprobe` checks
- content_hash: source `61b467250c1c97c3de4b98a8fd1ef1c4d0207299`; image
  `sha256:f2f6f23d329230ba6beac7368da84c44edc40564b19f20ac87e96d267a0ccef2`; Compose
  `f6cd0f4ff7f7f64aa99d0505e10fd1770391f021d3cff56e4c3f010148525ae9`; watcher
  `a0b8ead344c1e8b22394dc89bd454a656df895574cd06c737a065945fe969912`
- finding: 배포 전 watcher가 기존 장기 segment를 `recorder_segment_stale`로 탐지했다. 전환 때 약 4시간
  56분 열린 row가 233,570,352-byte `ready` 파일로 정상 종결됐고, 08:00 KST 경계에서 8개 recorder가
  모두 닫고 다시 열었다. 닫힌 8개·221,191,153 bytes는 모두 `ffprobe`에 성공했으며 video H.264 7/HEVC 1,
  audio 8/8이다. steady window에는 error 0, 43,794 bytes/214 seconds, repeated suppression 195였고 watcher는
  camera·stream·recorder·Viewer 8/8, stale 0, alert 0의 `ok`다. container는 healthy/restart 0이며 watcher
  timer는 enabled/active다.

### E-009

- observed_at: 2026-08-14 08:02~08:04 KST
- source_type: production Windows runtime
- source_ref: official `monitoring-pc` target status, read-only log audit and exact Viewer capture
- content_hash: audit script
  `2d7747aec3f329f0ff65e513cb43dcb7e14135c20b6e2b5c9eecd984ee8fabbc`; screenshot
  `f832948d82a06605386ffb25e01319b35be46bb21c9a2b4284bfb8023b91f5c7`
- finding: Viewer 2.0.27과 Service Running/Auto, warn·5 MiB×3가 유지되고 Windows crash/hang은 0이다.
  재시작 구간의 setup timeout/cooldown 기록은 08:02:05 KST 이후 증가하지 않았다. capture는
  `WasMaximized=true`, 2576×1408이고 실제 화면과 서버 telemetry 모두 8/8이다. 원격 run/task/listener
  잔여는 0이다.

### F-001

- status: deployed and observed
- evidence_ids: E-001, E-002, E-003, E-004, E-005, E-006, E-007, E-008, E-009
- finding: 카메라 media 진행에서 Viewer playback까지는 공통 JSONL 필드와 session으로 추적할 수 있고,
  고빈도 정상 pulse는 서버의 상태 전이·주기 요약으로 제한된다. 서버 logger와 watcher, Viewer 최소
  블랙박스와 recorder timestamp 개선이 운영 배포됐고 실제 30분 경계의 정상 8/8 순환이 확인됐다.
- remaining acceptance: 1분 watcher를 유지해 몇 주간 stale segment와 오류 증가율을 계속 본다. 실제
  playback 장애가 재발할 때 같은 `sessionId`의 server/Viewer record를 결합하며, 평시 warn 정책에서는
  Viewer record가 새로 생기지 않는 것이 정상이다.

### P-001

- start: 카메라 입력
- goal: `monitoring-pc`의 실제 영상 진행
- steps:
  1. `stream.go2rtc`와 `stream.live_warm`에서 input process 및 media 진행을 확인한다 — E-001.
  2. `recorder`의 progress와 segment close로 서버 수신·저장을 확인한다 — E-001.
  3. 서버 `playback`과 Viewer `viewer.playback`을 같은 `sessionId`로 결합한다 — E-002.
  4. Viewer control/renderer/stream 상태 전이와 DB 운영 event를 대조한다 — E-001, E-002.
- residual_risks: 내부 배포 Viewer MSI는 code signing되지 않았다. 재시작 직후 live-warm/Viewer가
  준비 순서와 cooldown 때문에 일시 오류를 남긴 이력이 있어 watcher로 재발 빈도와 지속시간을 계속
  관찰해야 한다.
