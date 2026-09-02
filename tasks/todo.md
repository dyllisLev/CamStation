# 2026-08-28 서버↔Viewer 재생 진단 운영 배포

## 배포 사양과 안전 경계

- 대상은 현재 clean source에서 만드는 immutable CamStation 서버 image와 `monitoring-pc`의 MSI 소유
  Viewer/ViewerService다. watcher·카메라 설정·DB·녹화 파일·Windows client identity는 변경하지 않는다.
- Viewer 진단 schema는 구버전 서버/Viewer와 양방향 호환을 유지한다. 따라서 Viewer를 먼저 전환해 화면과
  8/8 수신을 확인하고, 그 뒤 서버를 전환한다. 각 artifact는 이전 버전으로 독립 rollback할 수 있어야 한다.
- 직전 container recreate에서 45초 stop timeout과 recorder 직렬 종료가 녹화 partial을 손상시켰다.
  현재 종료 경로와 실제 stop budget이 안전하다는 직접 증거 없이는 서버 container를 재생성하지 않는다.
- 비밀값, URL credential, process 전체 argument는 출력하거나 배포 산출물에 추가하지 않는다. Windows 작업은
  공식 target wrapper의 status/artifact/system/viewer-capture 모드만 사용한다.

## 계획

- [x] Git diff·전체 시험·rollback image/MSI·운영 상태를 고정하고 배포 중단 기준을 확인한다.
- [x] exact clean commit에서 새 서버 image와 Viewer MSI를 만들고 revision/version/size/SHA-256을 고정한다.
- [x] Viewer Service를 공식 wrapper로 업그레이드하고 Service Running/Auto, 설정·identity 보존, 실제 화면과
  Viewer 수신 8/8을 확인한다. 실패 시 기존 MSI로 원복한다.
- [x] recorder stop grace를 120초로 전환해 서버 image만 기동하고, 실제 종료 파일 무결성까지 포함해
  container/API/recorder/Viewer와 새 playback 진단 필드를 확인한다.
- [x] 3회 연속 watcher 8/8, 새 segment 전진·파일 판독, Windows 잔여 작업 0을 확인하고 결과를 기록한다.

## 합격/중단 기준

- Viewer는 전환 후 90초 안에 Service Running/Automatic과 실제 화면 8/8, 30초 안에 갱신되는
  Viewer/renderer/stream progress를 만족해야 한다.
- 서버는 recorder 8개를 Docker stop budget 안에 clean 종료할 수 있어야 한다. 전환 후 90초 안에 healthy,
  7분 안에 camera/stream/recorder/Viewer 8/8과 새 ready/current segment 전진을 만족해야 한다.
- 최종 합격은 watcher 3표본 연속 Viewer receiving 8/8, container restart 0, 새 로그의
  `surface=official_viewer`와 세션 귀속 필드 확인, Windows control/setup/capture/config 잔여 0이다.

## Review

- clean source `fe9cbfdab67e16751c6b6df25163b9ecdb45b382`에서 서버 image
  `camstation:2.0.0-rc.20260828.22-playback-attribution`과 Viewer 2.0.29 MSI를 만들었다. image ID는
  `sha256:0bbcdefcc638824ebef4722a523e6389fa7c12b2446ba9a2977ef620e1e7116c`, MSI ProductCode는
  `{947837A4-1BE1-40FC-B68F-48EBBDE928CB}`, 크기 125,906,944 bytes, SHA-256은
  `bfb61b49e102b0d1b7ab7aa5b5379700a709cd82c6bf022d27b455be6eb3dcfe`다. 이전 image와 2.0.28 MSI,
  root-only Compose 백업을 rollback 자산으로 보존했다.
- Viewer 첫 설치 시 종료 중 process path 정규화 race가 발생했으나 2.0.28 자동 rollback과 설정·identity
  보존을 확인했다. exact-path 검사를 보정한 두 번째 transaction은 exit 0으로 2.0.29를 설치했고 Service는
  Running/Auto다. `LaunchAndCapture`가 임시 working directory를 Viewer에 상속해 cleanup을 막은 별도
  harness 결함은 설치 디렉터리 `WorkingDirectory` 고정과 회귀시험으로 수정·sync했다. exact residue는
  제거됐고 최종 control/setup/capture/config task, driver TCP listener, firewall rule은 모두 0이다.
- 서버는 2026-08-28 07:54:11 KST에 shutdown을 시작해 49초 뒤 clean exit했고, `.22` container는
  07:55:00 KST 시작·07:55:09 KST healthy가 됐다. stop grace는 120초이며 restart 0이다. fresh official
  Viewer document에서 8개 stream 모두 `surface=official_viewer`, 동일 anonymous `documentId`, primary,
  generation 1로 기록됐고 0.89~2.20초 안에 first media/playback started로 수렴했다.
- 종료 budget은 Docker 강제 종료를 막았지만 recorder 파일 합격에는 부족했다. pre-deploy 07:30 partial
  8개는 모두 존재·DB 크기 일치지만 2개만 ffprobe 성공하고 6개가 실패했다. source상 `stopWorker`와
  `waitForProcess`가 같은 stop에서 FFmpeg에 중복 terminate할 수 있어 단순 grace 증가로 해결되지 않는
  배포 transaction 결함이다. 추가 restart는 하지 않았다. 반면 `.22`가 시작 후 만든 07:55~08:00 파일
  8개는 전부 존재·크기 일치·A/V track·ffprobe 성공이고 08:00 current 8개는 모두 양수 크기로 증가한다.
- 08:00:53·08:01:56·08:02:57 KST watcher는 세 번 연속 status ok, alert 0, camera/live/recorder 8/8,
  Viewer healthy 1/1·receiving 8/8, logger error/warn 0이다. 08:01:32 KST exact-window에서도 실제 8개
  영상이 확인됐다. 서버 임시 전송물 4개는 검증 후 제거했으며 local build archive로 복구 가능하다.

# 2026-08-28 서버↔Viewer 재생 진단·일일 감사 축소

## 사양과 안전 경계

- 이번 변경은 카메라 입력 자체가 아니라 서버 playback endpoint에서 공식 Viewer 화면까지의 재생 세션을
  원인별로 구분하는 계측 패치다. 재생 정책·카메라 설정·운영 서비스는 변경하지 않는다.
- 구조화 로그에는 문서 단위 익명 ID, 화면 종류, primary/fallback/probe 역할, 시도 세대, 명령 재구독
  세대와 허용 목록 종료 사유만 추가한다. URL·SDP/ICE·credential·process argument는 수집하지 않는다.
- Paseo schedule `ab13f419`는 `[전날 06:00, 당일 06:00) KST`와 읽기 전용 경계를 유지하되,
  카메라/녹화는 source 정상 여부를 배제하는 최소 대조 증거로만 사용한다. 최종 브리핑은 문제, 예상 원인,
  06시 현재 상태를 중심으로 짧게 제한한다.

## 계획

- [x] 기존 playback 진단의 식별 공백과 공식 Viewer/보조 화면 호출 경로를 확인한다.
- [x] 변경 사양·검증 기준과 운영 무변경 경계를 이 문서에 고정한다.
- [x] Web→서버 JSONL과 Viewer 로컬 JSONL에 새 bounded 진단 필드를 전달하고 validation·redaction 테스트를 보강한다.
- [x] 공식 Viewer와 운영자/미리보기 화면이 정확한 `surface`를 보내도록 호출부를 연결한다.
- [x] 운영 로그 문서와 사용자 피드백 교훈을 갱신한다.
- [x] Web test/lint/build, Go focused/full test와 daemon build를 통과시킨다.
- [x] schedule prompt를 간결한 서버↔Viewer 감사로 실제 갱신하고 재조회해 cadence·target 불변과 prompt 반영을 확인한다.

## 검토

- Web playback record에 `documentId`, `surface`, `candidateRole`, `attemptGeneration`,
  `resubscribeGeneration`, `terminalReason`을 추가했다. 공식 native Viewer는 일반 `?viewer=1` 브라우저와
  구분되며 Viewer Service는 raw lease ID 대신 16자리 `leaseFingerprint`만 로컬 JSONL에 붙인다.
- `npm test` 89개, `npm run lint`, `npm run build`, focused Go 세 package와 `go test ./...`, 임시 경로
  daemon build가 모두 통과했다. 생성 Web asset도 새 hash로 갱신했다.
- schedule `ab13f419` prompt는 12,054자에서 3,983자로 줄였고 active, `0 6 * * * Asia/Seoul`, target
  provider/model/cwd, 무제한 run/expiry 없음이 그대로임을 재조회했다. 출력은 최대 12줄의 문제·예상 원인·
  현재 상태로 제한된다.
- 운영 서버/Viewer에는 배포하지 않았다. 새 진단 필드는 이 source revision이 승인된 배포로 적용된 뒤부터
  발생하며, schedule은 그 전 로그의 공식 Viewer 귀속을 확정하지 않도록 제한한다.

# 2026-08-25 Viewer 관리 채널 자기복구 운영 배포

## 배포 사양과 안전 경계

- 배포 대상은 `ssh cctv`의 현재 Docker production 한 서비스, host operational watcher 한 파일,
  `monitoring-pc`의 MSI 소유 Viewer/ViewerService 한 제품이다. 카메라 설정·DB·녹화 파일·nginx·Windows
  Viewer 설정/client identity·제어 driver는 변경하지 않는다.
- 서버 후보는 clean `main` exact commit에서 만드는 immutable image
  `camstation:2.0.0-rc.20260825.21-viewer-self-healing`, Viewer 후보는 같은 commit의 unsigned internal MSI
  `2.0.28`이다. 배포 전 image ID, MSI size/SHA-256/ProductCode/UpgradeCode와 source commit을 고정한다.
- 롤백 기준은 현재 healthy image `camstation:2.0.0-rc.20260822.20-storage-cleanup`과 설치된 Viewer
  `2.0.27`이다. root-only Compose 백업과 정확한 2.0.27 MSI 복구 자산을 확인하기 전에는 각 전환을
  시작하지 않는다.
- 서버는 Compose의 image 참조만 원자적으로 바꿔 `camstation` 하나만 재생성한다. watcher는 후보
  hash를 검증한 뒤 exact 설치 파일만 교체하고 one-shot 성공 후 timer 상태를 확인한다. Windows는
  공식 target wrapper의 artifact transfer와 bounded `system --intent change`만 사용한다.

## 계획

- [x] 현재 Git diff가 이번 Viewer 복구·frame truth·fast cooldown·watcher freshness 범위인지 재검토하고
  전체 회귀시험을 clean commit에서 다시 통과시킨다.
- [x] 전용 Windows build host에서 Viewer 2.0.28 MSI를 clean exact commit으로 만들고 metadata, size,
  SHA-256, ProductCode/UpgradeCode, sourceDirty=false를 검증한다.
- [x] 서버 이미지를 같은 commit으로 build/save/transfer/load하고 smoke test 후 기존 Compose와 image ID를
  보존한다.
- [x] 서버 image와 watcher를 전환하고 container healthy/restart 0, camera/stream/recorder 8/8, DB/file
  녹화 연속성, watcher 실행 성공을 확인한다. 실패하면 Viewer를 건드리기 전에 서버만 원복한다.
- [x] monitoring-pc에 MSI를 공식 wrapper로 전달해 Service→Viewer 순으로 정확히 중지한 뒤 major upgrade,
  Service/Viewer 복원, 설정/client identity 보존을 검증한다. 실패하면 정확한 2.0.27 MSI로 원복한다.
- [x] exact Viewer와 desktop capture에서 실제 8타일 진행을 확인하고 Viewer/renderer/stream heartbeat가
  새 시각으로 전진하며 watcher 3회 연속 receiving 8/8인지 검증한다.
- [x] control/setup/capture/config task, 원격 run, driver TCP listener/firewall 잔여 0과 배포 후 recorder
  segment rollover를 확인하고 Review·교훈·구현 상태를 갱신한다.

### 배포 중 확인된 client clock skew 보정

- [x] 실제 Viewer 8/8, 단일 Viewer/Service process group, lease-backed 로컬 진단과 서버 heartbeat를 대조해
  관리 채널 단절이 아니라 monitoring PC UTC가 서버보다 약 102초 느린 현상임을 확정한다.
- [x] watcher가 server receipt와 독립적인 control success 시각으로 보수적 clock correction을 계산하되
  15초 안전 여유를 남겨 실제 30초 Viewer/renderer stale을 정상으로 숨기지 않게 한다.
- [x] 정상·실제 stale·과도하거나 근거 없는 skew 회귀시험을 통과시키고 숫자만 남는
  `viewer_clock_skew` 경보와 보정 계측을 검증한다.
- [x] server/container/Viewer를 재시작하지 않고 watcher exact file만 hash-checked 교체한 뒤 세 연속 표본에서
  Viewer healthy 1/1·receiving 8/8이고 기존 stale/media 경보가 사라지는지 확인한다.
- [x] Windows 시간 동기화 미정상은 외부 운영 조치로 분리하고, 승인 없이 OS 시간·서비스 설정은 변경하지 않는다.

## 합격/중단 기준

- 서버 전환 후 container health는 90초 안에, camera/stream/recorder 8/8은 기존 운영에서 확인된
  live-warm 재시도 최악값을 포함한 7분 안에 수렴해야 한다. 이 안에 새 녹화 row/file이 전진하지 않으면
  기존 image/Compose로 즉시 원복하고 Viewer 배포를 중단한다.
- Viewer 전환 후 90초 안에 실제 화면 8/8이 아니면 정확한 2.0.27 MSI로 원복한다. 화면이 살아도
  Viewer/renderer heartbeat 또는 8개 media progress가 30초 안에 전진하지 않으면 새 결함으로 보고
  원복한다.
- 최종 Viewer 합격은 watcher 1분 표본 3회 연속 receiving 8/8·Viewer healthy 1/1이고 clock skew 외
  Viewer stale/media alert 0, Service Running/Automatic,
  container healthy/restart 0, recorder 8/8과 새 segment 전진, Windows 잔여 작업/연결/방화벽 0이다.

## Review

- server/Viewer source `0f5a2de`에서 immutable image
  `camstation:2.0.0-rc.20260825.21-viewer-self-healing`과 Viewer 2.0.28 MSI를 clean build했다. image ID는
  `sha256:2db36aabe704b290dd904bd1ba251fa43b3e863d9b00c85f2df5504b69d60179`, MSI SHA-256은
  `b87767b7d434ed9259dcd53a80d8b42d767b011709779d35cf74b584ef32241c`이고 rollback image/MSI와 root-only
  Compose/watcher 백업을 전환 전에 고정했다.
- production container는 `.21` exact image, healthy, restart 0이고 camera·public stream·recorder가
  8/8이다. Viewer 2.0.28은 ProductCode `{B9E81E3B-D3C7-4957-B851-C0FECCE955B8}`, Service Running/Auto,
  설정/client identity 보존으로 설치됐다. exact-window capture는 실제 8개 영상을 표시했고 최종 공식
  audit의 control/setup/capture/config task, driver TCP connection과 firewall rule은 모두 0이다.
- 최초 watcher 실패는 관리 pipe 단절이 아니었다. 같은 HTTP 왕복에서 monitoring PC UTC가 서버보다
  102.197초 느리고 W32Time source가 동기화되지 않은 상태임을 확인했다. 단일 Viewer/Service process와
  lease-backed 진단은 계속 진행했고 disconnect/expire 및 Windows crash/hang은 0이었다. OS 시각은
  변경하지 않았다.
- watcher commit `a8b3eae`는 server receipt와 control success로 client clock offset을 계산하고 15초
  안전 여유를 남겨 실제 management stale을 숨기지 않는다. 설치 hash는
  `a1c62ea1e8f835eb9ea787ffb50f745e54ab1b80018eb1f4bd34004b8abceafa`다. 10개 회귀시험과 production
  policy가 통과했고, 09:45:09·09:46:13·09:47:18 KST 세 표본은 Viewer healthy 1/1·receiving 8/8,
  freshness 13~18초였다. 남은 alert는 `viewer_clock_skew` 하나다.
- container recreate는 별도 recorder 종료 결함을 노출했다. 45초 stop timeout 아래 8개 worker를
  최대 10초씩 직렬 종료해 09:00 KST partial 중 소방서1/집-마당/집-창고1/집-창고2는 failed recovery,
  소방서3/소방서4/염소장/소방서5는 ready지만 `ffprobe` 실패가 됐다. 09:02~09:30 다음 8개 파일은 모두
  존재·DB size 일치·`ffprobe` 성공이고 09:30 current 8개는 5초 표본에서 모두 증가했다. 추가 routine
  container restart 전에 bounded-parallel recorder shutdown과 충분한 stop grace 회귀시험이 필요하다.

---

# 2026-08-25 Viewer 관리 채널 자기복구·freshness 확정

## 변경 계약

- 실제 영상 경로와 Viewer 관리 경로를 독립 축으로 유지한다. 관리 채널 장애 때문에 이미 재생 중인
  Viewer 창을 setup 화면으로 교체하거나 전체 Viewer·stream·server를 재시작하지 않는다.
- named-pipe request는 유한 deadline을 가지며, heartbeat 실패·half-open·lease 만료를 동일한 bounded
  reconnect 경로로 수렴시킨다. 새 연결은 새 lease를 획득하고 기존 renderer bridge가 telemetry를 다시
  전달하게 하며 중복 Viewer나 중복 lease를 만들지 않는다.
- Service가 서버에 보내는 Viewer/renderer 상태와 watcher의 `healthy`는 timestamp freshness를 포함한다.
  cached `running/ready` enum만으로 건강을 주장하지 않는다.
- frame callback과 background throttling 변경은 media truth/trigger 완화로 유지하되, 관리 채널
  자기복구의 대체물로 취급하지 않는다.

## 계획

- [x] monitoring-pc 공식 preflight와 실제 화면을 보존한 채 Service/lease/renderer/stream 시계, 현재 pipe
  status와 bounded local log를 읽기 전용으로 수집해 최초 trigger와 영구화 결함을 분리한다.
- [x] request 무응답, application-level lease 거부, socket close, reconnect 중 재실패, stale cached state를
  각각 실패 우선 회귀 테스트로 고정한다.
- [x] Viewer management request deadline·단일 disconnect 통지·pending 정리와 heartbeat 실패 수렴을
  최소 변경으로 구현한다.
- [x] 기존 live window를 유지하는 bounded reconnect·lease 재획득을 구현하고 setup/초기 연결 경로와
  명확히 분리한다.
- [x] Service heartbeat payload와 operational watcher에 Viewer/renderer freshness 판정을 추가한다.
- [x] Web/Viewer/Python/Go 전체 관련 테스트·lint·build·diff hygiene를 실행하고 환경상 미검증 항목을
  합격으로 기록하지 않는다.
- [x] 배포 가능한 artifact와 rollback 경계를 검토한 뒤, 운영 반영을 수행하는 경우 실제 화면 8/8,
  새 lease/telemetry, watcher 연속 3표본 8/8과 Windows 잔여 작업 0을 검증한다.

## 합격 기준

- 관리 request는 5초 이내 성공하거나 timeout으로 종료되며 pending request가 남지 않고 disconnect는
  연결 generation당 한 번만 전달된다.
- 15초 lease 만료 또는 half-open 뒤 기존 live document와 8개 영상은 유지되고, 30초 안에 새 연결·lease로
  수렴해 Viewer/renderer/stream timestamp가 다시 전진한다.
- Viewer 또는 renderer heartbeat가 30초 이상 stale이면 API/watcher가 healthy로 세지 않으며, Service
  heartbeat만 신선한 모순 상태를 별도 alert로 식별한다.
- 정상 stream에는 reconnect가 없고, management reconnect가 playback recovery budget을 초기화하거나
  중복 resubscribe를 만들지 않는다.
- 운영 반영 시 watcher 1분 표본 3회 연속 receiving 8/8, 실제 Viewer 8/8 진행, Viewer Service Running,
  control/setup/capture/config task·driver TCP listener·firewall residue 0을 모두 만족한다.

## Review

- 공식 monitoring-pc audit에서 Service는 Running/Auto, interactive Viewer process group은 모두
  `Responding=true`, 실제 Viewer exact-window는 8개 카메라 영상과 카메라 시각이 전진했다. 같은 순간
  Service→server heartbeat만 2~10초로 신선하고 lease/renderer/stream progress는 105~113초부터 함께
  노후화됐다. 따라서 영상 decoder 장애가 아니라 Viewer→Service 관리 채널 정지가 확정된 영구화
  경로다. 최초 trigger가 renderer scheduling pause인지 named-pipe half-open인지는 운영 증거만으로
  확정하지 않았다.
- 최종 read-only 재확인에서도 KST 08:20:45~47의 exact-window에 실제 카메라 8개가 모두 표시됐고,
  KST 08:19:42 서버 watcher는 camera/stream/recorder 8/8과 Viewer telemetry 0/8을 동시에 기록했다.
  Service는 Running, session은 Active, capture task는 삭제됐고 원격 run도 제거됐다. 따라서 현재
  사용자 영상은 살아 있지만 구버전 관리 telemetry blind spot은 운영 반영 전까지 계속된다.
- 실패 우선 테스트에서 silent request가 종료되지 않고 기존 disconnect가 live 문서를 setup으로
  교체하며 stale heartbeat를 watcher가 healthy로 세는 RED를 확인했다. request/connect 5초 deadline,
  generation당 한 번의 terminal failure, pending timer 정리, heartbeat 실패의 socket teardown을
  구현했다. 기존 live URL은 그대로 보존하고 1/2/5/10/30초 재연결에서 새 lease만 획득한다.
- LeaseManager 만료는 exact owner를 한 번만 통지하고 callback은 mutex 밖에서 실행한다. Server는
  connection generation을 기억해 늦은 expire/disconnect가 새 Viewer 상태를 덮지 못하게 했고,
  만료 시 timestamp/stream 증거는 보존한 채 `unresponsive`로 내린다. 예상치 못한 lease disconnect와
  만료는 Service warn에 남고, 복구된 Viewer는 원문 없이 bounded failure class·지속·재시도 수를
  `management_recovered`로 남긴다.
- Service→server payload는 Viewer/renderer heartbeat가 15초 이상 stale이면 cached
  `running/ready`를 `unresponsive`로 파생한다. watcher는 30초 freshness를 독립 확인하며
  `viewer_heartbeat_stale`/`viewer_renderer_stale`과 age/count를 기록한다. 과거 stream progress가
  남아 receiving 8/8이어도 관리 채널 stale을 healthy로 세지 않는다.
- 검증은 Viewer 54/54, Web 87/87, watcher 6/6, 전체 `go test ./...`, ViewerService `-race`, Web lint,
  Web/Viewer production build, Linux daemon build, Windows ViewerService test cross-compile/build,
  `gofmt -d`와 `git diff --check`를 통과했다. 고정 Go 1.25.12 digest를 일회성 컨테이너로 사용했고
  저장소는 read-only mount했다.
- 운영 Viewer는 조사 시점부터 실제 8/8로 살아 있어 재시작하지 않았다. 이번 source는 아직 production
  container/MSI에 배포하지 않았다. server image와 Viewer MSI를 함께 바꾸는 rollout은 immutable
  artifact·rollback과 명시적 운영 적용 단계가 필요하므로, 3개 watcher 표본·새 lease·화면 8/8 gate는
  배포 후 검증으로 남긴다.

---

# 2026-08-25 Viewer 운영 복구 우선·상태 계약 안정화

## 계획

- [x] 기존 작업 변경을 보존하고 monitoring-pc의 공식 target/session/driver/service preflight를 통과한다.
- [x] 기존 Viewer 창을 exact-window와 전체 desktop capture로 확인하고 실제 표시·프레임 진행 상태를 증거로 고정한다.
- [x] Viewer가 이미 8/8 재생 중이면 불필요한 재시작을 생략하고, 실제 화면과 서비스·renderer 상태를 함께 증명한다.
- [x] `online/healthy`와 카메라별 media progress가 모순되지 않도록 Viewer heartbeat·서버 DTO/store·watcher 집계 경계를 추적한다.
- [x] 기존 1회 15초 fast-cooldown 변경을 보존하면서 presented-frame 진행률 회귀시험과 최소 수정안을 구현한다.
- [x] Viewer management request에 유한 deadline을 두고 heartbeat/lease 실패 시 기존 live 창을 유지한 채 bounded reconnect·lease 재획득을 수행한다.
- [x] 서버와 watcher의 `healthy` 판정에 Viewer/renderer heartbeat freshness를 포함해 cached `running/ready`가 건강으로 남지 않게 한다.
- [x] Web/Viewer/Go 관련 테스트와 build, diff hygiene를 통과시키고 운영 반영 여부·잔여 위험을 검토한다.

## 합격 기준

- monitoring-pc의 실제 Viewer 창에서 활성 카메라 8개가 보이고 진행하며, 수정 배포 후 서버 watcher도 연속 3표본에서 receiving 8/8이다.
- Viewer service/control/renderer/media-progress 상태가 서로 다른 사실로 보고되고, 90초 넘게 progress가 없으면 watcher가 `viewer_media_missing`으로 경보한다.
- renderer 또는 heartbeat 정지 회귀에서 90초 이내 degraded가 되고 300초 안에 bounded recovery가 시작되며 정상 타일·recorder에는 영향이 없다.
- 모든 공식 Windows 제어 run이 정리되고 control/setup/capture/config task, TCP listener, firewall 잔여가 0이다.

## Review

- 2026-08-25 06:40~06:51 KST의 공식 exact-window 2회와 전체 desktop capture에서 활성 카메라
  8개가 모두 표시됐고 카메라 자체 시각과 장면이 전진했다. Viewer Service는 Running, renderer는
  ready였으며 공식 제어·setup·capture·config task와 TCP listener/firewall 잔여는 모두 0이었다.
  이미 살아 있는 Viewer를 재시작하거나 창·서비스·설정을 변경하지 않았다.
- 같은 시각 서버 watcher는 `viewer_media_missing`, receiving 0/8을 반복했다. 설치 watcher와 저장소
  watcher의 hash 및 집계 함수는 같았다. 추가 DB 교차 확인에서는 Service→server heartbeat만 약 10초로
  신선하고 Viewer lease heartbeat·renderer heartbeat·stream update·media progress가 모두 같은 시점에서
  112초 이상 멈춰 있었다. 직접 원인은 실제 영상 경로가 아니라 Viewer→Viewer Service 관리 채널의
  무응답/lease 상실이 자동 복구되지 않는 것이며, 최초 trigger가 renderer scheduling인지 named-pipe
  half-open인지는 아직 확정하지 않았다.
- Web hook은 `requestVideoFrameCallback`으로 실제 표시 프레임을 관찰해 최대 초당 1회 progress를
  갱신하고 기존 `timeupdate`를 호환 fallback으로 유지한다. 연결이 없거나 cooldown인 동안에는
  progress를 만들지 않으며 unmount 때 callback을 정확히 취소한다. 항상 켜져 있는 Electron 창에는
  `backgroundThrottling=false`를 고정했다. 이는 renderer scheduling trigger를 줄이고 frame 판정을
  정확하게 하는 부분 완화다. management request timeout·lease 재획득·stale health 파생이 없어 현재
  원인을 완전히 해결하지는 않는다. 기존 fast-cooldown 변경은 그대로 보존했다.
- RED 후 추가한 frame observer 2개와 BrowserWindow 계약 테스트가 통과했다. 전체 Web test 87/87,
  Viewer test 49/49, Web lint, Web production build, Viewer TypeScript/package build와
  `git diff --check`가 통과했다. 현재 Ubuntu 환경에는 Go 실행 파일이 없어 전체 Go test/daemon build는
  수행하지 못했으며 합격으로 기록하지 않는다.
- 새 Web bundle과 Viewer source는 운영 container/MSI에 배포하지 않았다. 따라서 watcher receiving
  연속 8/8은 rollout 후 검증 gate로 남아 있고, 운영 Viewer 2.0.27과 현재 server image는 변경되지 않았다.

---

# 2026-08-24 일일 감사 안정화 중심 브리핑 재구성

## 계획

- [x] 현재 06:00 감사 schedule과 과거 06:30 사용자 reporter 대상 상태를 재조회한다.
- [x] 오늘 처음이거나 달라진 오류 중 시스템/프로그램 결함 가능성이 높은 항목을 우선 판별하는 기준을 고정한다.
- [x] 같은 사건의 camera→server/recorder/Viewer 파생 증거를 하나의 안정화 이슈로 병합하고 핵심 3건을 먼저 쓰는 보고 형식으로 감사 prompt를 갱신한다.
- [x] 현재 사용자가 직접 받는 원 감사 output을 같은 핵심 순서로 고정하고, 종료된 과거 reporter 대상에는 중복 heartbeat를 만들지 않는다.
- [x] schedule의 비변경 필드와 안전·자원 경계를 재검증하고 Review 및 교훈을 기록한다.

## Review

- 기존 보고는 수집 완전성, 발견 문제, 다일 비교, 축별 결과, 현재 상태에서 같은 episode의 증거가
  반복돼 오늘의 시스템 결함 후보가 뒤로 밀렸다. 새 prompt의 최우선 목적을 프로그램 안정화로 명시하고
  원인 영역을 system-likely/external-likely/unknown으로 먼저 분류하도록 바꿨다.
- camera→go2rtc→live-warm/recorder→Viewer로 전파된 동일 시간대 사건은 한 인과 행으로 병합한다.
  외부 source reset 뒤 Viewer cooldown이나 supervisor 복구가 영향을 연장하면 외부 trigger와 시스템
  복구 결함을 분리해 후자만 안정화 핵심 이슈로 올린다.
- 최종 보고는 감사 구간·수집 완전성 최대 5줄 뒤 오늘의 안정화 결론을 먼저 쓰고,
  오늘의 프로그램 안정화 핵심 이슈를 최대 3행으로 제한한다. 나머지 운영 문제·신규/변형은 중복 없는
  압축 표, 반복 재시작·품질 신호·06시 수치는 하나의 후반 절로 모으고 조치도 즉시/개발 각각 최대 3개다.
- 과거 06:30 reporter가 가리키던 agent 5b39b3dd…는 closed 상태이고 현재 사용자는 06:00 schedule
  run output을 직접 확인하고 있다. 종료 대상을 재생성하거나 중복 알림을 추가하지 않고 원 output 자체를
  브리핑 형식으로 고쳤다.
- 재조회 결과 schedule ab13f419은 active, 0 6 * * */Asia/Seoul, 다음 실행
  2026-08-25 06:00 KST, provider/model/cwd/mode/isolation, 만료·run 수 제한 없음, 과거 run 12개를
  유지한다. 24시간 반개구간, 읽기 전용 안전 경계, 15분 수집/25분 전체 상한, ffprobe 40개 상한,
  다일 14일 비교와 run output 외 보고서 파일 금지도 그대로다. 즉시 실행은 하지 않았다.

---

# 2026-08-24 일일 감사 다일 패턴 비교·스케줄 프롬프트 갱신

## 계획

- [x] Paseo 감사 schedule의 설정과 보존된 전체 run 이력을 읽기 전용으로 확인한다.
- [x] 성공한 일별 감사의 새벽 카메라별 사건 시각·오류 연쇄를 비교하고 반복 후보와 당일 신규·변형 패턴을 분리한다.
- [x] 기존 06:00 KST cadence·안전 경계·실행 상한을 보존하면서 다일 기준선 및 당일 특이 이벤트 집중 규칙을 prompt에 추가한다.
- [x] 변경된 schedule을 재조회해 prompt와 비변경 필드가 정확한지 검증한다.
- [x] 비교 결과, 판정 한계와 schedule 변경 내용을 Review에 기록한다.

## Review

- schedule `ab13f419`의 보존 run 12개를 모두 확인했다. 성공·비어 있지 않은 24시간 감사 보고서는
  10개이고, 최초 취소 run 1개는 같은 구간의 후속 성공 run으로 대체됐다. KST
  `[2026-08-18 06:00, 2026-08-19 06:00)`은 Paseo daemon 재시작으로 run이 실패해 비교 보고서가 없다.
- 실제 장치 trigger/completion 로그가 없어 확정 시각은 만들지 않았다. 가장 강한 반복 후보는
  소방서3·4의 `02:01~02:07` KST(서로 다른 2일, 404→worker exit/retry)와 소방서1의
  `03:50~03:51` KST(서로 다른 2일, timeout/404 또는 bad-cseq와 live/Viewer 영향)다. 집-마당의
  `03:08~03:13` KST는 2일 관찰됐지만 단일 축 요약이 섞여 낮은 확신 후보로 남겼다.
- 염소장은 여러 성공 일자에 동일한 `media_stall→setup_timeout→episode_exhausted→약 5분 cooldown`
  연쇄가 반복됐지만 시각이 `02:09~04:27` KST로 넓게 흩어져 고정 restart minute를 추정하지 않았다.
  소방서5와 집-창고1·2도 고정 시각을 뒷받침할 다일 증거가 부족하다.
- 일별 특이 패턴 중 가장 큰 outlier는 08-22 07:31~11:28의 5대 source/producer 장애로, DB gap
  73회와 294-byte ready 파일 ffprobe 실패 32개를 만들었다. 최신 08-23~24에는 소방서5의 주간
  Viewer-only 300초 cooldown 2회가 새 패턴이고, 염소장 02:09·소방서1 02:52는 기존 강한 후보
  시각보다 이른 변형 후보다. `stream.warm context deadline exceeded`는 직전에도 183회였으므로
  최신 128회는 신규가 아닌 반복 품질 신호다.
- prompt는 이전 14일의 성공 일별 output 전부, 실패·결손 날짜, ±15분 다축 군집, 확신도,
  신규/변형 우선 슬롯, 비교 가능한 일별 중앙값·범위, 별도 `반복 새벽 재시작 후보`와
  `당일 신규·변형 패턴` 표를 요구하도록 갱신했다. 과거 raw 로그 재수집과 현재 raw 파일 추가 pass는
  금지했다.
- 재조회 결과 schedule은 active, `0 6 * * *`/`Asia/Seoul`, 다음 실행
  `2026-08-25 06:00 KST`, cwd/provider/model/mode/isolation, 만료 없음, 최대 run 없음, 보존 run 12개를
  유지한다. 읽기 전용 안전 경계, 15분 수집/25분 전체 상한, ffprobe 최대 40개, run output 외 보고서
  파일 금지 문구도 그대로 남아 있다. schedule을 즉시 실행하지 않았고 운영 시스템은 변경하지 않았다.

---

# 2026-08-20 예정 카메라 재시작 후 Viewer 빠른 복구

## 변경 계약

- 새벽 02:00~05:00 KST의 예정된 카메라 재시작으로 생기는 짧은 source reset은 카메라 오류로
  재분류하거나 카메라 설정을 변경하지 않는다.
- Viewer가 한 번의 30초 복구 episode를 소진한 뒤 바로 5분간 멈추지 않게 한다. 첫 소진에만
  15초 fast cooldown을 허용하고 새 전체 후보 episode를 한 번 실행한다.
- fast episode도 실패하면 기존 5분 저빈도 cooldown으로 돌아가 재연결 폭주를 막는다. 5분 연속
  media progress 또는 검증된 primary 복귀 뒤에만 fast cooldown 예산을 다시 채운다.
- 복구는 영향 타일 하나에만 한정하며 정상 타일, Viewer 프로세스, 서버 stream, recorder를
  재시작하지 않는다.

## 계획

- [x] 기존 300초 첫 cooldown을 실패로 고정하는 회귀 테스트를 추가하고 RED를 확인한다.
- [x] `PlaybackRecovery`에 15초 1회 fast cooldown 예산과 안정 재생 후 reset을 구현한다.
- [x] live playback 설계 문서와 구현 상태를 새 복구 계약에 맞춘다.
- [x] focused Web test, 전체 Web test/lint/build를 통과시킨다.
- [ ] 전체 Go test/build를 통과시킨다. 의존성 proxy TLS timeout으로 환경 검증이 막혔다.
- [x] 변경 파일과 생성 asset을 검토하고 운영 미배포 상태 및 검증 결과를 기록한다.

## 검토

- 이전 구현에서 첫 episode 소진 결과가 `until=331000`(300초)이어서 새 기대값
  `until=46000`(15초)에 실패하는 RED를 확인한 뒤 구현했다.
- 첫 소진 15초, 두 번째 소진 300초, 5분 연속 progress 재충전, 검증된 primary 복귀 재충전,
  짧은 progress 비재충전을 직접 테스트했다. Web 전체 test 85/85, lint, production build와
  `git diff --check`가 통과했다.
- 고정 Go 1.25.12 이미지의 일부 독립 패키지는 통과했지만, SQLite/YAML 모듈 다운로드가
  `proxy.golang.org` TLS handshake timeout으로 끝나 전체 Go test/build는 합격 판정을 하지 않았다.
- embedded Web asset은 새 hash로 재생성했다. 운영 서버, container, DB, monitoring PC에는 배포하거나
  변경하지 않았다.

---

# 2026-08-22 백업 비활성 시 녹화 용량 정리 복구

## 계획

- [x] 운영 DB를 read-only/query-only로 열어 700 GiB 제한, 백업 비활성, 미백업 보호 활성,
  ready 3,628개 전부 pending, 최근 자동 정리 삭제 0개를 확인한다.
- [x] `backup.enabled=false`이면 `protectUnbacked`를 비활성화하는 설정 정규화와 cleanup 방어 로직을
  실패 우선 회귀 테스트로 구현한다.
- [x] 관련 store/cleanup/API 테스트와 전체 Go 테스트·빌드를 통과시킨다.
- [x] 기존 작업 트리를 보존한 exact revision 이미지로 운영에 반영하고, 자동 정리가 700 GiB 이하로
  수렴하며 container/camera/stream/recorder/Viewer가 정상인지 검증한다.

## Review

- 운영 DB에서 700 GiB 제한 대비 855,314,752,508 bytes, backup disabled,
  `protectUnbacked=true`, ready 3,628개 전부 pending, backed-up 후보 0개를 확인했다. 자동 cleanup은
  반복 실행됐지만 삭제 0건이었다.
- 실패 우선 테스트는 cleanup과 설정 공개 양쪽에서 재현됐다. 유효 보호 조건을
  `backup.enabled && protectUnbacked`로 고정하고, 비활성 backup의 저장·공개 설정과 importer 기본값을
  false로 정규화했다. focused package와 `go test ./...`, daemon build, immutable image smoke가 통과했다.
- commit `008ece5f12a98c9328e2436f3b4864f6cfa545ac`을 `main`에 fast-forward/push하고 image
  `camstation:2.0.0-rc.20260822.20-storage-cleanup`으로 운영 반영했다. image ID는
  `sha256:37f37493d053a8731aca022875885216a979e723bb053d32805cb5046f2cac7f`다.
- startup cleanup은 522개를 지웠고 11:30 KST 다음 segment-close cleanup이 1개를 추가 정리해 총
  523개가 `capacity cleanup`으로 전환됐다. 저장량은 751,262,717,968 bytes로 700 GiB 한도 아래이며,
  삭제 파일 잔존 0, ready missing/0-byte/size mismatch 0, 최신 8개 ffprobe/A/V 8/8이다.
- 최종 container healthy/restart 0, non-root/read-only/no-new-privileges, camera/stream/recorder/Viewer
  8/8, recorder error 0이다. watcher는 2026-08-22 11:33:49 KST `ok`, alert/error 0,
  media disk 75.6%다. root-only Compose rollback 백업은
  `compose.pre-storage-cleanup-20260822T022526Z.yaml`로 보존했다.

# 2026-08-17 Viewer WebSocket 1시간 종료 수정

## 변경 계약

- 정상 재생 중인 WebRTC signaling WebSocket이 nginx의 3600초 idle timeout으로 종료되지 않게 한다.
- 범위는 Viewer의 정확한 `/player/api/ws` endpoint로 한정하고 일반 HTTP/API proxy timeout은 유지한다.
- 운영 반영은 새 root-owned include를 설치한 뒤 `nginx -t`를 통과해야만 active symlink를 원자 교체하고
  graceful reload한다. container, camstationd, go2rtc와 monitoring PC는 재시작하지 않는다.
- 실패하면 이전 include로 symlink를 되돌리고 nginx 구문·health·Viewer 8/8을 다시 확인한다.
- 새 연결의 `socket_open` 이후 3600초 경계를 실제로 지나 `socket` 실패가 없고 media progress가 계속될
  때 완료로 판정한다.

## 계획

- [x] Viewer WebSocket 전용 nginx location과 정책 회귀 검사를 구현한다.
- [x] shell policy, nginx 구문과 관련 애플리케이션 회귀 테스트를 통과한다.
- [x] 운영 include를 원자 전환하고 graceful reload 뒤 health와 Viewer 8/8을 확인한다.
- [x] 새 WebSocket 연결이 3600초를 초과해 유지되는지 운영 로그로 검증한다.

## 검토

- 저장소 정책 검사와 전체 Go 테스트가 통과했고 운영 nginx 바이너리로 후보 include 구문도 검증했다.
- 저장소 기본 include를 그대로 사용한 첫 후보는 운영 Docker publish upstream과 달라 신규 handshake가
  502가 됐다. 기존 연결은 graceful reload로 유지됐으며 즉시 이전 include로 rollback했다.
- 운영 active include의 upstream·headers를 그대로 보존하고 exact WebSocket location의 timeout만 365일로
  바꾼 후보를 재생성했다. `nginx -t`, graceful reload, 신규 WebSocket 101과 daemon health가 통과했다.
- 첫 후보 시간대에 cooldown으로 들어간 `집-창고1-live`, `집-창고2-live`는 강제 재시작 없이 자동 복구됐고
  2026-08-17 07:15:33 KST watcher에서 Viewer 8/8, alert 0, container healthy/restart 0을 확인했다.
- 이전 nginx worker에 남아 있던 `집-마당-live`, `소방서3-live`는 07:20:58 KST에 마지막 3600초 종료가
  발생했지만 새 설정으로 socket은 0.15초 안에 다시 열렸고 재생은 1.22초 안에 복구됐다. 07:22:00 KST
  watcher도 Viewer 8/8, alert 0이며 이때 열린 새 연결부터 365일 timeout을 적용받는다.
- 소스와 정책 검사는 commit `08e3ac2`로 `origin/main`에 반영했다. 기존 `tasks/lessons.md`,
  `tasks/todo.md`의 별도 작업 기록은 해당 commit에 섞지 않았다.
- 2026-08-17 09:18 KST 재검증에서 실제 운영 nginx는 exact WebSocket location의 read/send timeout
  `365d`와 일반 location의 `3600s`를 그대로 로드하고 있었다. container는 running/healthy/restart 0,
  watcher timer는 enabled/active였으며 최신 표본은 alert 0·Viewer 8/8이었다.
- 새 설정으로 다시 열린 `집-창고1-live`, `집-창고2-live`, `집-마당-live`, `소방서3-live`의 WebRTC
  세션은 09:18 KST까지 같은 세션의 `socket` 실패·종료·재오픈 없이 각각 최소 1시간 49분~1시간
  54분 유지됐다. 공식 Viewer API도 8개 live stream 모두 `playing/webrtc`, 최근 영상 진행 age
  1.3~2.2초를 반환했다.
- 08:55 KST의 `socket` 3건은 세션별 최초 `socket_open`을 역추적한 결과 수정 전날 23:45 KST에 열린
  잔존 세션이었다. 새 설정으로 열린 세션의 재발로 세지 않았고, 해당 watcher 표본도 Viewer 8/8을
  유지했다. 따라서 실제 3600초 경계 검증까지 통과해 완료로 판정한다.

---

# 2026-08-16 일일 감사 사용자 보고 연결

## 전달 계약

- 06:00 KST 감사 schedule `ab13f419`은 새 agent에서 읽기 전용 감사를 수행하고 보관되는 기존 구조를 유지한다.
- 감사 실행 상한 25분 뒤인 매일 06:30 KST에 현재 대화의 reporter가 해당 날짜 run을 재조회해 사용자에게
  한국어 브리핑을 전달한다. 감사를 다시 실행하거나 운영 상태를 변경하지 않는다.
- 성공이면 run ID·감사 구간·종합 판정·현재 상태·실제 문제·녹화 누락 수·권장 조치를 보고한다. 실패,
  canceled, output 누락 또는 지연 상태도 숨기지 않고 정확한 status와 error를 보고한다.
- 가장 최근 06:00 KST run만 대상으로 하며 과거 run이나 다른 날짜 결과를 오늘 보고로 오인하지 않는다.

## 계획

- [x] schedule 완료 전달과 heartbeat 지원 범위 및 기존 중복 구성을 확인한다.
- [x] 매일 06:30 KST reporter heartbeat를 현재 대화에 생성한다.
- [x] reporter와 원 audit schedule의 cadence·상태·다음 실행을 재검증한다.

## 검토

- schedule에는 사용자 대화로 전달하는 completion notification 옵션이 없고, 현재 agent에 반복 prompt를
  보내는 heartbeat가 지원됨을 실제 도구 계약으로 확인했다.
- heartbeat `56d10753` (`camstation-daily-audit-user-report-0630-kst`)을 생성했다. target은 현재 대화 agent
  `5b39b3dd-0416-4bee-b6c3-de049535e812`, cadence는 `30 6 * * *` / `Asia/Seoul`, 상태는 `active`다.
  `expiresAt=null`, `maxRuns=null`, 다음 실행은 2026-08-17 06:30 KST다.
- reporter는 가장 최근 06:00 KST run의 정확한 `scheduledFor`를 검증한다. 성공 시 저장 output을 운영
  기준에 맞춰 직접 브리핑하고, 실패·취소·누락·06:40까지 지연도 status/error와 함께 보고한다.
- 원 audit schedule `ab13f419`은 `active`, `0 6 * * *` / `Asia/Seoul`, 다음 실행 2026-08-17 06:00 KST,
  기존 run 4개로 유지됨을 재조회했다. 감사 재실행이나 운영 서버·DB·서비스 변경은 하지 않았다.

---

# 2026-08-16 일일 운영 감사 스케줄 판정 기준 업데이트

## 변경 계약

- 기존 schedule `ab13f419`의 매일 06:00 KST cadence, 24시간 감사 구간, 실행 환경, 읽기 전용 안전 경계와
  수집 상한은 유지하고 판정·보고 기준만 수정한다.
- 카메라별 연속 라이브 미수신이 5분 미만이면 예정 재시작에서 발생 가능한 정상 운영 신호로 취급해
  `발견 문제`와 종합 장애 판정에서 제외한다. 5분 이상만 재시작 정황과 대조해 문제 또는 확인 필요로 보고한다.
- 녹화 장애는 기대 구간의 DB/file gap, 파일 부재·0-byte·파싱 실패, 실제 재생 불연속처럼 누락·손상이
  확인될 때 판정한다. 재시작으로 파일이 나뉘었어도 전체 구간을 덮고 파일이 유효하면 장애로 세지 않는다.
- DTS 반복은 실제 파일·재생·음성 품질 영향이 확인되지 않으면 품질 관찰 신호로 분리하고 녹화 누락으로
  표현하지 않는다. 최종 보고서는 `파일 누락`, `분할`, `연속성 미검증`을 각각 구분한다.

## 계획

- [x] 현재 스케줄의 프롬프트·cadence·실행 환경을 재조회한다.
- [x] 사용자 운영 기준을 프롬프트의 판정 방식과 최종 보고서 형식에 반영한다.
- [x] 변경 뒤 상태·다음 실행·프롬프트 및 비변경 필드를 재조회한다.

## 검토

- schedule `ab13f419`의 prompt만 업데이트했고 상태는 `active`, cadence는 `0 6 * * *` / `Asia/Seoul`로
  유지됐다. 다음 실행은 2026-08-17 06:00 KST다. provider/model/cwd/mode/isolation과 기존 run 4개도 변하지 않았다.
- 새 기준은 카메라별 연속 미수신을 episode 단위로 계산한다. 300초 미만은 `정상 범위 일시 신호`로만
  집계하고, 서로 떨어진 짧은 episode를 합산하지 않으며, 300초 이상만 `발견 문제`에 포함한다.
- 녹화는 30분 기대 구간의 row coverage, DB gap, 파일 존재·양수 크기·크기 일치·ffprobe/A/V와 필요 시
  실제 재생 연속성으로 판정한다. 분할돼도 coverage와 파일이 정상이면 장애가 아니며, 증명되지 않은 duration
  차이는 `연속성 미검증`, 영향 없는 DTS/PTS 반복은 `품질 관찰 신호`로 분리한다.
- 저장 후 핵심 규칙 7개를 재조회해 모두 일치함을 확인했다. schedule을 즉시 재실행하지 않았으며 운영
  서버·DB·서비스·PC와 과거 run 이력은 변경하지 않았다.

---

# 2026-08-16 오늘 운영 로그 감사 브리핑

## 범위와 판정 기준

- 오늘 06:00 KST에 종료된 정기 24시간 감사 구간 `[2026-08-15 06:00, 2026-08-16 06:00)`의
  저장된 schedule 결과와 성공 여부를 우선 확인한다.
- 저장된 결론을 현재 운영 상태와 읽기 전용으로 교차 확인해, 과거 장애와 현재 지속 장애를 구분한다.
- 카메라/stream 식별자는 조치 가능하도록 유지하고 비밀번호·token·credential URL·SDP/ICE 등 비밀은
  출력하지 않는다. Windows Viewer PC는 서버 증거만으로 원인이 확정되지 않을 때만 명시된 대상에 대해 확인한다.

## 계획

- [x] 오늘 정기 감사 실행의 상태·구간·저장된 결과를 확인한다.
- [x] 장애 항목과 현재 복구 상태를 운영 로그·DB·런타임 증거로 교차 확인한다.
- [x] KST 기준 영향, 우선순위와 권고 조치를 브리핑하고 검토 결과를 기록한다.

## 검토

- 정기 run `0746b191-6a9e-44c9-bc24-fa91fb61ea31`은 2026-08-16 06:00:00~06:17:24 KST에
  `succeeded`했고, 정확한 감사 구간 `[2026-08-15 06:00, 2026-08-16 06:00)`의 한국어 보고서를 저장했다.
- 24시간 daemon 35,940건과 watcher 1,404표본은 무효 JSON·90초 초과 cadence 공백·logger write failure가
  모두 0이었다. ready 파일 386개가 모두 존재했고 최신 8개와 의심 4개는 ffprobe 12/12에 성공했다.
- 종합 판정은 `장애`다. Viewer 전체 경로가 8 episode·총 28분 37초 동안 최저 2/8까지 저하됐고,
  `소방서3/4-live`는 약 5분 59초 404 재시도, `소방서1/5-recording`은 worker 재시작으로 각각 두 파일로
  비정상 분할됐다. 모두 06시 전에 복구됐으며 분할 파일은 판독되지만 실제 영상 연속성은 미확정이다.
- recorder audio DTS 역행은 구간 내 55,424신호로 계속됐고 `집-창고1/2-recording`이 약 94%였다.
  06:00~07:50 KST 재검증에서도 4,113신호, 그중 두 창고 3,593신호가 이어져 ongoing 품질 위험으로 남았다.
- 07:49 KST 최신 watcher는 alert 0, camera/stream/recorder/Viewer 8/8, stale segment 0,
  warn/error window 0/0이었다. container는 healthy/restart 0, watcher timer는 enabled/active였다.
  06시 이후 물리 로그는 warn 21건·error 0건이며 짧은 playback 실패는 모두 현재 상태에서 복구됐다.
- 지속 Viewer 장애가 없으므로 `monitoring-pc`는 중복 조회하지 않았다. 운영 서버·DB·서비스·PC는 변경하지 않았다.
- 사용자 운영 기준을 반영하면 예정 재시작 주변 5분 미만 라이브 단절은 장애가 아니다. 이번 녹화 자료는
  ready 386개 모두 존재, missing·0-byte·DB gap·ffprobe 실패가 0이므로 `녹화 파일 누락`은 확인되지 않았다.
  `소방서1/5`은 파일 분할과 실제 duration 차이 때문에 연속성 확인이 필요한 주의 항목이지 확정 누락은 아니다.

---

# 2026-08-14 매일 06시 운영 로그 24시간 감사 스케줄

## 실행 계약

- Paseo가 매일 06:00 `Asia/Seoul`에 새 agent를 시작한다. 감사 구간은 실행 지연과 무관하게 막 지난
  KST 06:00을 끝 경계로 고정한 반개구간 `[전날 06:00, 당일 06:00)` 정확히 24시간이다.
- 운영 서버 `cctv`의 회전본을 포함한 daemon JSONL, 1분 watcher JSONL, SQLite 녹화 source of truth와
  현재 Docker/systemd 상태를 읽기 전용으로 대조한다. 서비스 재시작·설정 변경·파일 삭제·DB 쓰기·Git
  변경은 하지 않는다.
- 카메라→서버, recorder/30분 segment, 서버→Viewer, container/logger/watcher, disk와 backup/cleanup을
  각각 판정한다. 로그 누락·watcher cadence 공백·판정 자료 부족도 문제로 보고한다.
- 보고서는 한국어·KST로 작성한다. 문제가 있으면 실제 카메라명·stream명, 최초/최종 시각, 지속시간,
  발생 횟수, 관련 event/error code/fingerprint와 필요한 짧은 오류 원문, 현재 복구 여부, 근거와 조치를
  명시한다. 정상이라면 8/8, segment 수·stale 0, Viewer 수신, error/logger failure 0 등 통과 근거를 쓴다.
- 비밀번호·token·cookie·Authorization·RTSP/HTTP credential URL·SDP/ICE와 secret 설정값만 보고서에서
  제외한다. 카메라/stream 식별자는 운영 조치에 필요하므로 숨기지 않는다.
- 서버 증거가 정상일 때 monitoring PC를 중복 감사하지 않는다. 서버에 지속 Viewer 장애가 있고 원인이
  서버 로그만으로 확정되지 않을 때만 공식 `monitoring-pc` wrapper로 로컬 warn/error와 crash/hang을
  읽기 전용으로 대조하며, PC 서비스·창·설정을 변경하지 않는다.
- 결과는 Paseo schedule run의 최종 보고서와 이력에 남기고 저장소 파일 생성, commit 또는 push는 하지 않는다.
- 대용량 로그는 종류별 streaming pass와 최대 20개 top-N으로 제한한다. 수집은 15분에 닫고 전체 실행은
  25분 안에 마치며, SSH·ffprobe timeout과 최대 40개 probe 상한을 넘긴 항목은 판정 한계로 보고한다.

## 계획

- [x] 기존 Paseo schedule과 안정적인 daemon cwd, cron/timezone 지원을 확인한다.
- [x] 사용자 수정에 따라 aggregate-only가 아닌 실제 운영 대상을 특정하는 보고 계약을 고정한다.
- [x] 무기한 매일 06:00 KST schedule을 생성한다.
- [x] schedule ID, cron/timezone, next run, cwd/isolation/provider/model/mode, prompt와 run 제한을 재조회한다.
- [x] 프로젝트 교훈과 검토 결과를 기록하고 저장소·Paseo 상태를 최종 검증한다.

## 검토

- Paseo schedule `ab13f419` (`camstation-daily-24h-operations-audit-0600-kst`)을 생성하고 즉시
  `inspect_schedule`로 재조회했다. cadence는 `0 6 * * *`, timezone은 `Asia/Seoul`, 상태는 `active`다.
- 첫 자동 실행은 2026-08-15 06:00 KST(`2026-08-14T21:00:00Z`)다. `expiresAt=null`, `maxRuns=null`이므로
  일회성이나 기간 제한 없이 매일 반복한다.
- 새 agent target은 안정적인 `/workspace/CamStation`, local isolation, Codex `gpt-5.6-sol`,
  `full-access`, thinking `max`, plan mode off다. full-access는 운영 SSH read에 필요하지만 prompt가 server/PC/
  DB/Git mutation을 명시적으로 금지한다.
- 보고 prompt는 실제 카메라명·stream명과 관련 오류 원문을 포함해 장애를 특정한다. watcher의 숫자만
  재진술하지 않고 daemon 회전 로그, DB segment, ffprobe와 현재 runtime을 교차 검증하며, 비밀번호·token·
  credential URL 같은 운영 비밀만 제외한다.
- 최초 run `7a92cdc3-f466-4562-8d50-70bd6000f044`는 집계가 오래 걸려 실행 중 후속 prompt를 보냈기 때문에
  최종 보고서는 생성됐어도 원 schedule turn이 `canceled`로 기록됐다. 이를 성공 검증으로 인정하지 않고,
  schedule prompt 자체에 streaming·timeout·수집/실행 마감을 추가했다.
- 개입 없이 다시 실행한 run `e6d88c79-7408-44bd-b64b-d437ea069aca`는 2026-08-14 10:00:15~10:22:02
  KST(21분 47초)에 `succeeded`했고 비어 있지 않은 한국어 `output`이 run 이력에 저장됐다. daemon
  608,315줄과 watcher 547표본, read-only SQLite와 최대 40개 ffprobe를 교차했고 운영 상태를 변경하지 않았다.
- 재검증 보고서는 실제 `소방서1/소방서1-recording`, `소방서5`, `염소장` 등 영향 대상을 특정했다. 06시
  연결은 camera·stream·recorder·Viewer 8/8이었지만 최신 녹화 파일 합격은 5/8, daemon/watcher 시작부는
  약 14시간 결손이어서 종합 판정을 `장애`로 냈다. 따라서 표면 8/8만 보고 정상으로 오판하지 않는 계약도
  실제 동작으로 확인했다.

---

# 2026-08-14 녹화 순환·FFmpeg 로그 폭증 운영 개선

## 사양과 안전 경계

- 카메라가 역행하거나 잘못된 PTS/DTS를 보내도 recorder 출력 timestamp는 wall clock 기준으로 안정화하고,
  30분 `segment_atclocktime` 경계에서 파일이 닫혀 DB `ready` row·백업·정리 경로로 넘어가야 한다.
- 동일 FFmpeg stderr가 반복될 때 첫 신호는 원인·worker 문맥을 유지해 남기고, 이후 동일 signature는 메모리에서
  억제한 뒤 제한된 주기로 누적 건수 summary를 남긴다. 실제 process exit, segment open/close 실패와 persistent
  logger 실패는 억제하지 않는다.
- 운영 watcher는 recorder process 8/8 외에 DB의 latest-ready age와 current-segment age를 집계하고,
  설정 segment 길이+안전 여유를 넘긴 stream이 있으면 identity 비노출 alert code로 `degraded`를 만든다.
- 기존 장시간 열린 임시 파일은 삭제하지 않는다. 새 image 전환 때 recorder 정상 종료가 해당 파일을 finalizing한
  뒤 새 wall-clock segment를 만드는지 확인하며, 실패하면 보존 상태로 롤백하고 원본 파일을 훼손하지 않는다.
- 운영 변경은 immutable image와 exact Compose 백업으로 수행하고 container health, camera·stream·recorder·Viewer
  8/8, 로그 증가율, 30분 경계의 실제 segment close를 확인한다. monitoring-pc는 읽기 전용 상태·로그 확인만 한다.

## 계획

- [x] 배포 후 FFmpeg signature·timestamp 축·영향 stream 정책과 열린 파일 상태를 값 비노출 방식으로 확정한다.
- [x] recorder wall-clock timestamp 입력과 동일 stderr rate-limit/summary를 회귀 테스트와 함께 구현한다.
- [x] watcher에 DB segment freshness 축·경보를 추가하고 정상/지연 fixture를 검증한다.
- [x] Go 전체·race·vet, watcher/policy, image smoke를 통과하고 immutable 운영 후보를 만든다.
- [x] 운영 Compose를 백업해 새 image로 전환하고 장시간 열린 파일의 안전한 finalization과 8/8 수렴을 확인한다.
- [x] 다음 30분 경계를 지나 실제 8개 segment 순환, 오류율·보존기간, Viewer 수신 및 잔여물을 검증한다.
- [x] 결과·교훈·구현 상태를 문서화하고 commit/push 및 운영 revision을 일치시킨다.

## 검토

- source revision `61b467250c1c97c3de4b98a8fd1ef1c4d0207299`의 immutable image
  `camstation:2.0.0-rc.20260814.19-recorder-timestamp`를 2026-08-14 07:57 KST에 전환했다. image ID는
  `sha256:f2f6f23d329230ba6beac7368da84c44edc40564b19f20ac87e96d267a0ccef2`이고 container는
  healthy/restart 0, non-root/read-only/no-new-privileges를 유지한다. exact rollback snapshot
  `rollback-recorder-hardening-20260813T225607Z`를 보존했고 실제 rollback은 사용하지 않았다.
- watcher를 image보다 먼저 전환해 기존 장기 segment를 `recorder_segment_stale`로 탐지한 뒤 daemon을
  정상 종료했다. 약 4시간 56분 열려 있던 row는 삭제·격리 없이 `ready`로 닫혔고 최종 파일
  233,570,352 bytes가 존재하며 DB 오류가 없다. 새 runtime에는 recorder 8개와 live-warm 8개 모두
  wall-clock 입력 인자가 적용됐고 camera·stream·recorder는 8/8로 수렴했다.
- 2026-08-14 08:00 KST 첫 실제 30분 경계에서 `segment_closed` 8건과 새 `segment_opened` 8건이 확인됐다.
  닫힌 파일 8개·221,191,153 bytes는 모두 `ffprobe`에 성공했고 video는 H.264 7개/HEVC 1개, audio는
  8개이며 길이는 182.03~187.92초다. DB의 ready/current stream도 각각 8/8이고 stale count는 0이다.
- 재시작 직후 live-warm 준비 경쟁으로 404 signature 4종·8건과 Viewer 재연결 error 1건이 한 번
  기록됐지만 recorder error는 0건이었다. 08:02:26 KST부터 watcher는 alert 0·`ok`, Viewer 수신 8/8이 됐다.
  08:00 이후 정상 구간에는 error 0건, 214초 동안 43,794 bytes가 기록됐고 195건이 summary로 축약됐다.
  단기 증가율 환산은 약 0.74 MB/hour로 64 MiB×32에서 약 121일분이다.
- `monitoring-pc`는 읽기 전용으로 확인했다. Viewer 2.0.27과 Service Running/Auto, warn·5 MiB×3,
  Windows crash/hang 0을 유지한다. 재연결 로그는 08:02:05 KST 이후 증가하지 않았고 최종 capture는
  `WasMaximized=true`, 2576×1408, 실제 화면 8/8이다. 공식 wrapper의 원격 run/task/listener 잔여는 0이다.
- 최종 운영 Compose/watcher/env SHA-256은 각각 `f6cd0f4f...25ae9`, `a0b8ead3...9912`,
  `d1103d11...1f3a`이고 watcher timer는 enabled/active, 최근 service result는 success다.

---

# 2026-08-14 Viewer 2.0.27 배포 후 로그 추이 감사

## 범위와 합격 기준

- Viewer 2.0.27 최종 실행·최대화 검증 시각 `2026-08-13T12:17:55Z` 이후 현재까지 서버의 1분 watcher,
  daemon 구조화 JSONL, container·systemd 상태를 읽기 전용으로 집계한다.
- 정상 합격은 현재 container healthy/restart 0, camera·stream·recorder·Viewer 8/8, watcher 연속 `ok`,
  persistent logger failure 0이며, warn/error가 있으면 배포·검증 중 기대된 연결 단절과 이후 재발을
  시간축으로 구분한다.
- `monitoring-pc`는 Viewer 2.0.27/Service 상태, warn 로컬 JSONL의 배포 이후 record, Windows Application
  crash/hang record만 안전한 count·level·event·timestamp로 확인한다. 원문 message, URL, identity와 설정
  값은 출력하지 않고 서비스·창·설정은 변경하거나 재시작하지 않는다.

## 계획

- [x] 운영 서버 watcher의 배포 이후 status/alert 연속 구간과 현재 8/8 상태를 집계한다.
- [x] daemon JSONL의 level/component/event/error code, 쓰기 실패·파싱 오류·회전·용량을 집계한다.
- [x] `monitoring-pc` status와 Viewer 로컬 warn/error 및 Windows crash/hang 표본을 읽기 전용으로 감사한다.
- [x] 서버·PC 시간축을 대조해 실제 지속·반복 문제와 배포 중 기대된 일시 신호를 분리한다.
- [x] 검토 결과와 남은 관찰 항목을 기록하고 dirty diff·대상 잔여물을 확인한다.

## 검토

- 감사 구간은 Viewer 2.0.27 검증 시각인 2026-08-13 21:17:55 KST부터 2026-08-14 07:10 KST까지다.
  서버 watcher 575개 표본은 JSON 오류 0건·최대 간격 65초였고, 최신 표본에서 container healthy/restart 0,
  camera·stream·recorder·Viewer 8/8, Viewer progress age 5초다. 다만 03:11 KST부터 최근 daemon error 때문에
  현재까지 연속 `degraded`이며, “문제 없음”으로 판정할 수 없다.
- daemon JSONL은 5개·270,622,235 bytes, 유효 849,016건으로 schema 누락·파싱 오류·persistent write failure가
  모두 0이다. 그러나 error가 838,399건이고 최근 한 시간만 210,679건이다. 03:10 KST부터 두 recorder와
  한 live-warm worker에서 timestamp 역행·invalid 계열 FFmpeg 신호가 초당 약 59건으로 계속 발생한다.
  현재 증가율이면 64 MiB×32 보존 범위는 약 30~32시간에 불과해 몇 주 추이 보존 요구를 충족하지 못한다.
- 30분 녹화 정책과 DB·파일을 교차 확인한 결과 8개 중 1개 recorder가 03:00 KST 이후 ready segment를
  만들지 못했다. 현재 segment는 약 4시간 10분 열린 채 223,871,024 bytes이고 5초 표본에서 262,144 bytes
  증가했다. 영상 바이트는 들어오지만 DB 조회·백업·정리 대상인 finalized segment로 순환하지 않는 실제
  장애다. 다른 7개는 latest ready age 45분 이내이며, watcher의 `running/current 8/8`, `errors 0`만으로는
  이 문제를 잡지 못한다.
- watcher상 Viewer media missing은 03:13~03:17, 04:38~04:42, 06:10 KST의 세 구간이었다. 앞의 두 구간은
  Viewer의 `episode_exhausted` 뒤 약 5분 cooldown 재시도로 복구됐고 현재 8/8이다. `monitoring-pc` 로컬에는
  warn 176·error 2건, invalid line 0건이며 Windows Application crash/hang은 0건이다. Viewer 2.0.27,
  Service Running/Auto, warn·5 MiB×3 정책과 session 1 Active가 유지된다.
- 서버의 일시 `api_cameras_failed`는 04:57 KST 한 표본뿐이고 다음 표본부터 회복했다. 서버/Viewer 로그와
  Windows 상태는 모두 읽기 전용으로 확인했으며 container, recorder, Service, Viewer를 재시작하거나 설정을
  변경하지 않았다. 종료 status에서 control/setup/capture/configure task, driver TCP/firewall 잔여는 모두 0이다.
- 후속 수정은 (1) 영향 recorder의 timestamp/segment rollover 원인과 열린 파일의 안전한 복구,
  (2) 반복 FFmpeg message의 rate limit·summary화, (3) watcher에 latest-ready/current-segment age 경보 추가를
  함께 다뤄야 한다. 이번 요청은 감사이므로 운영 상태 변경은 수행하지 않았다.

---

# 2026-08-13 Viewer 재시작 시 최대화 시작

## 사양과 경계

- `monitoring-pc`의 Viewer가 설치 후 실행, `restart_viewer`, 일반 재실행에서 처음 보이는 순간부터 Windows
  최대화 상태여야 한다. 현재 2.0.26 소스는 hidden `BrowserWindow`를 만든 뒤
  `ready-to-show`에서 `show()`만 호출해 Electron 기본 창 크기로 시작할 수 있다.
- 요구 상태는 Windows `maximize`이며 Viewer native fullscreen이 아니다. 제목 표시줄·작업 표시줄과
  사용자의 이후 복원/크기 조절은 유지하고, 매 순간 최대화를 강제하거나 저장된 설정을 추가하지 않는다.
- setup/live가 공유하는 하나의 top-level window에 적용하고, renderer load가 끝날 때마다 재최대화하지
  않는다. 첫 `ready-to-show`에서만 `maximize()` 후 `show()`해 깜빡이는 초기 작은 창을 노출하지 않는다.
- 새 MSI는 2.0.27로 만들고 현재 2.0.26 MSI, config/client identity, Service logging environment를 rollback
  기준으로 보존한다. 운영 서버·카메라·Viewer 연결 설정은 변경하지 않는다.

## 계획

- [x] 공식 wrapper status와 read-only audit/capture로 `monitoring-pc`, Viewer 2.0.26, Service Running/Auto,
      session 1, 현재 수동 최대화 창과 기존 설정 hash를 고정한다.
- [x] first-show 최대화 순서를 회귀 테스트로 고정한 뒤 최소한의 Viewer main-process 변경을 구현한다.
- [x] Viewer 전체 테스트·빌드와 관련 정적 검사를 통과하고 깨끗한 exact commit에서 2.0.27 MSI를 생성한다.
- [x] MSI hash/ProductCode/UpgradeCode를 검증하고 `monitoring-pc`에 공식 artifact 전송·업그레이드를 수행한다.
- [x] 설치 직후 공식 launcher가 실행한 새 Viewer를 exact-window capture로 확인해 `WasMaximized=true`, 실제 8타일 진행,
      config/client/log policy 보존, task/run/listener/firewall 잔여 0을 증명한다.
- [x] 검토·교훈·구현 상태를 갱신하고 완료 커밋을 feature branch와 `main`에 force 없이 게시한다.

## 검토

- 배포 전 exact-window capture `20260813T120410787Z-e4c48b5c9b4f4bfca5b2168d8bdfc938`은
  2576×1408, `WasMaximized=true`였지만 이미 떠 있던 창의 현재 상태일 뿐 시작 동작을 증명하지 않는다.
  코드에는 `maximize()` 호출이 없으므로 재실행 직후 창모드가 되는 사용자 관찰과 일치한다.
- 배포 전 Viewer는 2.0.26 ProductCode `{998E7181-5567-4973-AD1F-0A8361798B32}`, Service Running/Auto,
  config SHA-256 `da085b9b...e8710`, client identity SHA-256 `0f5101ea...f323`, warn·5 MiB×3이다.
- `ready-to-show` handler가 `maximize()` 후 `show()`를 한 번만 호출하도록 변경했다. 변경 전 회귀 테스트는
  48/49로 정확히 실패했고 구현 후 Viewer 49 tests와 TypeScript/package build가 통과했다.
- clean commit `56de740090f37454a6b2013de8833c0b3eb970f9`에서 Viewer 2.0.27 MSI를 만들었다. ProductCode는
  `{9A18B580-AA6E-4F2D-AE45-986523915BBE}`, UpgradeCode는 기존 값 그대로이며, 125,898,752 bytes,
  SHA-256 `52c85f2b...416f`, 76 files, `NotSigned`다.
- 첫 설치 transaction은 MSI 실행 전 `stop_viewer`에서 이미 종료된 Electron 자식 PID를 다시 종료하는
  경쟁 조건을 만나 2.0.26으로 검증 원복됐다. 각 exact PID를 즉시 재확인하도록 배포 script를 고친 뒤
  두 번째 transaction은 exit 0, reboot 불필요로 2.0.27을 설치했다. 2.0.26 rollback MSI
  `4c5358a7...6502`는 보존했다.
- 설치 후 Service 재시작만으로는 새 interactive 창을 만드는 제품 경로가 아니므로 공식
  `LaunchAndCapture`로 새 Viewer를 실행했다. 첫 capture의 원격 폴더 잠금은 exact run cleanup으로
  `Remaining=false`를 확인했고, 이어진 capture
  `20260813T121754992Z-6c822fc3a58c42459c28764cebed2787`에서 새 window PID 9388이 수동 조작 없이
  `WasMaximized=true`, bounds -8/-8/2576×1408이었다. PNG SHA-256은 `62ee1173...a89bb`이며 화면에서
  8개 타일의 최신 영상이 확인됐다.
- 배포 후 Product/version/core hashes, Service Running/Auto, config/client identity hash, warn·5 MiB×3가
  모두 일치했다. 서버 watcher 최신 표본도 container healthy/restart 0, camera·stream·recorder·Viewer
  8/8, progress age 7초, alert와 logger warn/error 0이었다. 최종 target status는 session 1 Active,
  control/setup/capture/configure task 0, driver TCP/firewall 0이다.

---

# 2026-08-13 운영 로그 배포·실시간 추이 관찰

## 범위와 불변 기준

- 실제 운영 서버는 `ssh cctv`의 `/opt/camstation2/docker-production`이며, 공개 확인 주소는
  `https://cctv2.nuc.hmini.me`다. 폐기된 `cctv2` SSH 별칭을 우회하거나 복구 대상으로 사용하지 않는다.
- 서버 기준선은 image `camstation:2.0.0-rc.20260813.17-viewer-reception`, revision `e4411cd`,
  Compose SHA-256 `b4028d10d1b6468f8b649ffbc380c5b4cad941df569f96bbbdcca6eae6f34057`,
  healthy/restart 0, 활성 camera·recorder·Viewer 수신 8/8이다.
- `monitoring-pc` 기준선은 Viewer 2.0.25, Service Running/Auto, 새 Viewer log 환경값 미설정,
  ProgramData 기존 로그 2개·1,661 bytes다. 설정·client identity·현재 2.0.25 MSI 복구 자산을 배포 전에
  다시 고정한다.
- 새 서버 후보는 clean `main`의 immutable image
  `camstation:2.0.0-rc.20260813.18-operational-logging`, Viewer 후보는 2.0.26 MSI다. 현재 운영 revision의
  always-hot/Viewer 재수신 동작을 먼저 포함한 뒤 빌드하며 monitoring-pc에는 소스나 toolchain을 두지 않는다.
- 서버는 `info` 전역과 camera/playback/recorder component debug, 영속 log 64 MiB×32를 초기 몇 주간
  사용한다. Viewer는 `warn`, override 없음, 5 MiB×3을 명시한다.
- 별도 1분 systemd timer는 공개 API·Docker health·영속 logger freshness·warn/error 증가·disk·recorder·
  Viewer media progress를 숫자와 alert code만으로 `operational-watch.jsonl`에 10 MiB×4 회전 기록한다.
  URL, credential, camera host/name, process args, runtime path와 원문 API/log는 저장하지 않는다.

## 배포 계획

- [x] 실제 운영 서버 경로와 현재 server/Viewer/API 기준선을 읽기 전용으로 확정한다.
- [x] `test-pc`의 clean build root, Git revision, Go/Node/WiX 도구와 2.0.26 빌드 가능 상태를 공식 wrapper로
      확인하고 `monitoring-pc`의 exact MSI rollback/config/client identity 기준선을 고정한다.
- [x] 운영 watcher·systemd unit·fixture test와 설치/제거 절차를 구현하고 spec/운영 문서를 갱신한다.
- [x] 현재 변경을 목적별로 commit/push하고 운영 revision을 포함해 clean `main`으로 승격한다.
- [x] clean `main`에서 Web/Viewer/Go/race/vet/policy/secret 검증, Linux image와 Windows 2.0.26 MSI를 만들고
      revision·SHA-256·unsigned 내부 배포 상태를 고정한다.
- [x] 서버의 exact Compose를 root-only timestamp 백업한 뒤 image 한 곳과 log 환경값만 변경해 재생성하고,
      health/security/camera 8/8/recorder 8/8/Viewer 8/8 및 JSONL append를 유한 시간 안에 검증한다.
- [x] watcher script/config/service/timer를 exact hash로 설치해 1분 cadence, flock, 회전, journald와 JSONL
      표본을 검증한다.
- [x] monitoring-pc에 exact MSI를 공식 artifact 경로로 전달·업그레이드하고 Viewer log 환경값을 기존 Service
      Environment와 병합한다. ProductCode/version/file hash/config/client identity/Service/interactive Viewer를
      전후 비교하고 실제 Viewer 창과 server heartbeat/media progress를 확인한다.
- [x] 즉시·5분·15분 이상 표본에서 container restart, logger failure, warn/error 증가, camera/recorder/Viewer
      진행, disk와 watcher alert 추이를 비교하고 검토·운영 문서를 증거와 함께 마감한다.

## 합격과 자동 롤백

- 서버는 container healthy/restart 0, 보안 옵션·publish bind 불변, 활성 camera 8/8, stream media 8/8,
  recorder running/current 8/8, Viewer online/control/renderer/media 8/8, 새 JSONL schema와 persistent append가
  모두 확인돼야 한다.
- Viewer는 2.0.26 설치, Service Running/Auto, interactive Viewer renderer ready, 8대의 최신
  `lastProgressAt`, 평시 warn 정책과 3-file 상한이 확인돼야 한다. MSI가 unsigned인 사실은 숨기지 않고
  기존 내부 unsigned 배포와 같은 잔여 위험으로 기록한다.
- 서버 gate 하나라도 실패하면 같은 transaction에서 exact Compose 백업과 `.17` image로 되돌리고 health와
  server camera/stream/recorder 축을 재확인한다. Viewer 8/8은 제품의 5분 cooldown 또는 누락 타일만의
  `resubscribe_stream`으로 별도 수렴시킨다. 이 지연만으로 기술적 rollback 실패로 판정하지 않는다.
  watcher 실패는 제품 container를 건드리지 않고 unit/config/script만 이전 상태로 복구하거나 제거한다.
- Viewer gate가 실패하면 exact 2.0.25 MSI와 보존한 Service Environment/config/client identity로 원복하고
  Service·Viewer·8/8 media progress를 다시 확인한다. 서버 배포 성공은 Viewer 실패 때문에 자동 원복하지
  않되 서버가 구 Viewer와 호환되는지 검증한다.

## 검토

- 실제 server는 `cctv`이며 `.17` image/revision/Compose, 양쪽 publish bind, security option, persistent volume,
  camera·stream·recorder·Viewer 8/8를 기준선으로 고정했다. stale `cctv2` SSH 별칭은 사용하지 않았다.
- `test-pc` canonical repo는 HEAD `1215d05`, dirty entry 131, remote ref가 과거
  `origin/camstation2-initial` 하나뿐이라 직접 빌드 기준으로 사용할 수 없다. 다만 Node 22.23.2,
  npm 10.9.8, Go 1.25.12, .NET 8.0.423과 24.7GB 여유를 확인했으므로 새 exact commit을 fetch한 별도
  detached worktree에서만 2.0.26을 빌드한다.
- `monitoring-pc`는 ProductCode `{81E12973-4223-462D-92DD-EAE6705C3AC3}`의 2.0.25 하나이며 Service
  Running/Auto다. config SHA-256 `da085b9b...e8710`, client-ID SHA-256 `0f5101ea...f323`, cached rollback
  MSI 124,436,480 bytes/SHA-256 `51448b51...e79f`를 값 비노출 방식으로 고정했다.
- watcher는 1분 oneshot, flock, 10 MiB×4 회전이며 raw API/log를 저장하지 않고 count/age/percent와
  alert code만 기록한다. 정상 8/8, 부분 장애·logger write failure, 회전, 잘못된 credential URL 설정의
  fixture 4개와 systemd unit verify, production policy가 통과했다.
- 새 logger 포함 Web 81 tests/lint/build, Viewer 49 tests/build, 전체 Go test, 관련 5개 package race,
  `go vet ./...`, Linux daemon/Windows Service cross-build가 통과했다. 배포 revision, artifact hash,
  before/after 증거, 관찰 구간과 rollback 사용 여부는 아래 운영 표본에 기록했다.
- 변경 commit `de4938c`를 feature branch와 `main`에 force 없이 게시했다. clean `main`에서 만든 Linux image는
  `sha256:ad1bc15...09326`, Viewer 2.0.26 MSI는 125,898,752 bytes/SHA-256
  `4c5358a7...6502`이며 MSI는 기존 내부 배포와 같이 unsigned다. 격리 image smoke에서 non-root/read-only,
  restart 0과 두 번의 재생성 사이 JSONL append(11건→24건)를 확인했다.
- 첫 `.18` 전환은 210초 안에 Viewer 8/8을 요구한 검사기가 3개 타일의 정상 5분 cooldown을 제품 실패로
  오판해 자동 `.17` 원복을 수행했다. 원복 Compose SHA와 image/health/restart 0, camera·stream·recorder
  8/8은 즉시 확인돼 실제 rollback은 성공했다. Viewer도 별도 명령 없이 저빈도 재시도로 8/8,
  `playing 8/fallback 8`, progress age 3~4초로 수렴했다. 재시도 검사기는 server 복구와 Viewer 수렴을
  분리하고 누락 타일에만 제한된 재구독을 허용하도록 수정했다.
- 두 번째 `.18` 전환에서는 Viewer gate 전에 public warm stream이 4분 동안 6/8이라 정확히 `.17`로
  원복했다. `.17`도 재시작 직후 같은 6/8 지연을 보여 로거 image 고유 회귀가 아님을 확인했고,
  runtime key는 8개 모두 존재하나 일부 producer가 늦게 생기며 `not_found` retry가 반복되는 형태였다.
  server는 이후 8/8로 수렴했고 cooldown 2개에만 command 10·11을 보내 Viewer도 8/8, progress age
  1~2초로 복구했다. 다음 전환 gate는 실제 server warm 수렴을 포함하도록 12분으로 늘렸다.
- 최종 `.18` 전환은 root-only rollback
  `/opt/camstation2/docker-production/compose.pre-operational-logging-retry2-20260813T113014Z.yaml`을 남기고
  완료했다. 운영 Compose SHA-256은 `710748d5...704d`, image ID는 `sha256:ad1bc15...09326`이며,
  2026-08-13 20:56 KST 최종 무중단 표본에서 container는 healthy/restart 0, camera·stream·recorder·Viewer는
  모두 8/8, Viewer progress age는 3초였다. 재시작 뒤 public producer가 8/8이 되기까지 약 6분 걸렸지만
  `.17`에서도 같은 현상이 재현됐고 수렴 뒤 추가 재시작은 하지 않았다.
- host watcher는 `/usr/local/libexec/camstation-log-watch`와 1분 systemd timer로 설치됐다. timer는
  enabled/active다. Viewer MSI 전환 중 Service가 정지된 20:48~20:49 KST의 두 표본은 정확히
  `viewer_health_degraded`/`viewer_media_missing`을 기록했고, 이후 6개 연속 `ok`로 회복했다. 최신 표본은
  alert 0, warn/error/persistent-write-failure window 0, logger age 30초, state/media 사용률 14.4%/9.2%다.
  전환 구간의 error 표본도 삭제하지 않아 이후 추이 비교의 기준으로 남긴다.
- `monitoring-pc`는 Viewer 2.0.26 ProductCode `{998E7181-5567-4973-AD1F-0A8361798B32}`로 업그레이드됐다.
  Service는 Running/Auto이고 `warn`, override 없음, 5 MiB×3이 명시됐다. config/client identity hash는
  배포 전과 같고, 새 MSI와 설치 core hash가 일치한다. 실제 interactive 창과 서버 telemetry에서 8개
  타일의 WebRTC playing/progress를 확인했다. 2.0.25 rollback MSI는 보존했으며 2.0.26 MSI가 내부용
  unsigned라는 잔여 위험은 그대로다.

---

# 2026-08-13 서버 중심 운영 로그 정책

## 범위와 합격 기준

- 운영 판단과 몇 주간의 실시간 관제는 서버의 영속 JSONL을 기준으로 한다.
- `monitoring-pc` 로컬 로그는 서버가 볼 수 없는 Viewer 시작 전 실패, renderer/GPU, management pipe,
  네트워크 단절을 위한 최소 블랙박스로만 유지한다.
- Viewer 기본 level은 `warn`, component override는 없음, 회전은 파일당 5 MiB·active 포함 3개로 한다.
  상세 `debug`는 장애 재현 시간에만 명시적으로 켰다가 원복한다.
- 기존 승인 target profile을 스스로 찾아 공식 wrapper로 `monitoring-pc` 상태를 확인하며, 운영 설치본이나
  Service 설정은 별도 배포 승인 없이 변경하지 않는다.

## 계획

- [x] 이전 작업공간과 로컬 보안 자산에서 승인 target profile을 찾아 현재 worktree에 `0600`으로 복구한다.
- [x] 공식 wrapper status로 `monitoring-pc`의 machine, identity, active session, Service와 제어 잔여물을 확인한다.
- [x] Viewer logger 기본 level·회전 상한과 회귀 테스트를 서버 중심 정책으로 변경한다.
- [x] 운영 문서·설계·구현 계획에서 Viewer 상시 debug 권고와 오래된 profile 차단 문구를 제거한다.
- [x] focused/full 테스트와 정적 검사를 실행하고 실제 PC 변경·미배포 상태를 검토에 기록한다.

## 검토

- 승인 profile 원본은 canonical 이전 작업공간 `/workspace/CamStation`에서 찾았고 key·known-host 파일,
  schema와 wrapper 동일성을 값 비노출 방식으로 확인한 뒤 현재 worktree에 `0600`으로 복구했다.
- 공식 status의 시작·종료 표본 모두 `monitoring-pc -> NUC`, `NUC\\dyllislev/session 1 Active`, Viewer
  Service `Running`, telemetry disabled, TCP/firewall/control/setup/capture task 0, canonical script parity
  전부 true였다. read-only 감사 외에 PC 설정·파일·Service를 변경하지 않았다.
- 현재 ProgramData 로그는 2개·총 1,661 bytes다. 13개 기존 record에는 새 `level` 필드가 없고 Viewer
  전용 로그 환경값도 없으므로 설치본은 이번 정책 이전이다. 새 Viewer 배포 전까지는 기존 동작을 유지한다.
- 소스 기본값은 Viewer `warn`, override 없음, 파일당 5 MiB·active 포함 3개로 변경했다. daemon용
  `CAMSTATION_LOG_LEVEL(S)`를 Viewer가 상속하지 않으며 장애 재현 중에만 Viewer 전용 override로 debug를
  켠다. 서버의 영속 JSONL과 초기 soak debug 정책은 그대로 유지한다.
- `internal/viewerservice` focused test, Viewer race, `go vet ./...`, Windows amd64 Service cross-build와
  단독 `go test ./... -count=1`이 통과했다. 병렬 검증 부하 중 `vieweragent` readiness가 한 번 timeout
  났지만 해당 test 20회와 package 5회, 표준 전체 재실행은 모두 통과해 제품 회귀가 아님을 확인했다.
  Windows Service 후보 SHA-256은 `54e6c8413d75d76640d91f0d61134ced8675ba8d50a4e9ffcd382e89fcdc226f`다.
- 운영상 다음 필수 작업은 (1) 새 daemon/Viewer의 승인 배포, (2) 서버 JSONL의 자동 경보다. 기존
  `scripts/hourly-recording-monitor.sh`는 3-camera·종료일·raw args 가정 때문에 재사용하지 않고,
  카메라 media 정지, recorder segment 정지, Viewer heartbeat 정지, 반복 restart, logger write 실패,
  disk 임계치를 상태변화와 rate 기준으로 감시하는 새 서버 watcher가 필요하다.

---

# 2026-08-13 CamStation 2.0 운영 관측 로거 보강

## 범위와 합격 기준

- 운영 Viewer 기준 대상은 명시된 `monitoring-pc`다. 공식 target wrapper의 status와 읽기 전용
  system 감사로 실제 Service/Viewer 로그를 확인하고, 프로필 부재 상태를 임의 SSH로 우회하지 않는다.
- daemon, go2rtc, live-warm ffmpeg, recorder ffmpeg, HTTP playback, Viewer Service/Electron/renderer가
  같은 JSONL 필드와 `debug/info/warn/error/off` 수준을 사용한다.
- daemon 기본 `info`와 Viewer 기본 `warn`, component-prefix override를 환경 변수로 관리하며 기존
  playback level 설정은 호환한다. 잘못된 level은 조용히 무시하지 않고 시작 단계에서 실패시킨다.
- daemon 로그는 DB와 같은 영속 state volume 아래에서 회전해 컨테이너 재생성에도 남고, Viewer 로그는
  ProgramData의 기존 ACL·회전 경계를 유지한다. raw 카메라 URL, 자격증명, token, SDP/candidate,
  전체 process args와 runtime path는 기록하지 않는다.
- 카메라→서버는 worker 시작/정상 media 진행/종료/분류된 ffmpeg 오류/재시도/segment close를 남기고,
  서버→Viewer는 playback session, transport, phase, first media, failure/fallback/recovery/close 및
  Viewer control/renderer/stream 상태 변화를 상호 검색할 수 있어야 한다.
- 정상 Viewer heartbeat는 DB event를 10초마다 만들지 않는다. 최초 관찰과 의미 있는 상태 변화만
  운영 event로 남기고 세부 heartbeat는 debug JSONL에서만 선택적으로 관찰한다.
- 단위·race·전체 Go 테스트, Web test/lint/build, Viewer test/build, daemon 및 Windows cross-build,
  secret hygiene, 회전·재시작 보존을 통과해야 한다. 운영 배포는 별도 승인 전에는 수행하지 않는다.

## 계획

- [x] 사용자 피드백을 lessons에 반영하고 `monitoring-pc` 공식 status preflight를 시도한다.
- [x] 로그 공백과 기존 daemon/Viewer 로깅·회전·telemetry 경계를 다시 추적한다.
- [x] 상세 설계와 파일 단위 구현 계획을 specs/plans에 고정한다.
- [x] 공통 구조화 logger, level/override 파서, 영속 회전 writer와 보안 테스트를 구현한다.
- [x] go2rtc/live-warm/recorder의 연결·media progress·오류·retry·segment 로그를 공통 logger에 연결한다.
- [x] playback 서버 로그와 Viewer Service/Electron/renderer 로컬 로그를 같은 session으로 연결한다.
- [x] Viewer heartbeat DB spam을 상태 변화 event로 교체하고 관련 회귀 테스트를 추가한다.
- [x] Docker/Windows 운영 설정 예시와 운영 문서를 새 level·보존 계약에 맞춘다.
- [x] 전체 소스 검증과 격리 재기동 검증을 수행하고 `monitoring-pc` status를 다시 시도한다.
- [x] 승인 target profile을 복구한 뒤 `monitoring-pc` 기존 ProgramData 로그 기준선을 확인한다.
- [ ] 새 빌드를 배포한 뒤 실제 장애 표본의 server/Viewer session join을 확인한다.
- [x] 검토란에 구현, 테스트, 운영 미적용 사항과 남은 차단 조건을 기록한다.

## 검토

- 최초 `monitoring-pc` status는 현재 worktree의 ignored profile 부재로 fail-closed 됐다. 이후 canonical
  이전 작업공간과 과거 승인 기록까지 추적해 원본 profile과 전용 known-host 자산을 찾고 현재 worktree에
  `0600`으로 복구했다. 공식 status는 NUC/session 1 Active, Viewer Service Running, driver telemetry off,
  TCP/firewall/control task 0과 7개 script parity를 확인했다. 임의 SSH로 우회하지 않았다.
- 공식 read-only system 감사에서 ProgramData 로그는 2개·총 1,661 bytes였고 유효 JSON 13건 모두
  `level` 필드가 없는 이전 schema였다. Viewer 로그 환경값 4개는 명시돼 있지 않았다. 설치·Service
  재시작·설정 변경은 하지 않았으며 새 기본 warn·5 MiB×3 정책은 다음 승인 배포부터 적용된다.
- daemon 공통 logger는 전역 `info`, longest-prefix component override, legacy playback 입력,
  4 KiB JSONL, stdout+영속 파일, 25 MiB×8 기본 회전과 1–1024 MiB/1–64개 검증을 제공한다. URL,
  자격증명, token, Authorization, SDP/ICE, PEM, Windows/POSIX path를 redact하고, 영속 쓰기 실패는
  1분 rate-limit된 `opslog/persistent_write_failed`로 stdout에 남긴다.
- 카메라 경로는 `stream.go2rtc`, `stream.live_warm`, `recorder(.ffmpeg)`에서 camera/stream, attempt,
  first/periodic media progress, 분류된 child 오류, 종료 시간, retry, 안전한 segment filename/size를 남긴다.
  의도된 go2rtc 재시작은 INFO 종료로 구분해 정상 운영 중 거짓 WARN을 만들지 않는다.
- playback HTTP와 Viewer local diagnostic은 같은 `sessionId`를 쓰며 server record에는 `cameraId`와
  client IP도 포함한다. Viewer management pipe는 활성 lease를 renderer payload보다 우선하고,
  control/lease/renderer/stream 변화만 저장하며 진행 중인 media는 최대 1분 1회 debug로 요약한다.
- Viewer heartbeat DB event는 최초 관찰·의미 있는 상태 변화만 append한다. 반복 heartbeat와 동시
  duplicate는 회귀 테스트에서 event 수를 늘리지 않았다. 정적 검사에서 발견한 기존 Service context
  cancel 조기-return 누수도 context 생성 순서를 조정해 함께 제거했다.
- 검증은 Web 81개 test·lint·production build, Viewer 49개 test·build, `go test ./...`, 변경 패키지와
  `camstationd` race, `go vet ./...`, production policy와 canary/dev Compose config를 통과했다. 최종
  산출물은 정적 Linux daemon SHA-256 `06384751858306dcbbf9710cf73c4b20e012757c4d43831d31786a877322bda0`,
  Windows Service `eb6f92d6388770408788adf54d646acb5ad37d6f4b59f10b8332ceab2e4cf39f`, Windows bootstrap
  `ee7e4331e6ee1883ca6f345b0a359087402168db6dbe11a23e5430fdf4eb3425`다.
- 네트워크가 차단된 임시 state에서 최종 정적 daemon을 세 번 재기동했다. 매회 의도된 probe-only
  종료코드 1, 총 33개 유효 JSON record, `startup_started`/`ready`/`probe_failed` 각 3개와 파일 증가
  5,347 bytes를 확인했으며 금지 URL·경로·credential 문자열은 없었다. 회전·동시 write·재open은
  `internal/opslog` 테스트로 별도 검증했다.

---

# 2026-08-13 CamStation 2.0 운영 로그 수준 감사

## 범위와 합격 기준

- 카메라→서버 입력은 go2rtc/ffmpeg/recorder 각각에서 연결·끊김·재시도·media 진행·오류가 어느
  수준으로 남는지 소스와 실제 운영 로그를 함께 확인한다.
- 서버→브라우저/Viewer 출력은 HTTP, WebSocket, WebRTC/MSE 연결 수명주기와 Viewer heartbeat,
  재생 진행·fallback·재연결을 어느 단위까지 사후 추적할 수 있는지 확인한다.
- 로그의 저장 위치, 구조화 여부, 기본 level, 최근 보존량·회전/상한·민감정보 노출 위험을 확인한다.
- 운영 상태를 바꾸지 않는 읽기 전용 점검만 수행하며 URL·자격증명·client secret·raw process args는
  출력하지 않는다.
- 몇 주간 실시간 관제에 필요한 최소 신호를 현재 로그만으로 충족하는지 `충분/부분/부족`으로 판정하고,
  공백과 우선순위를 증거와 함께 기록한다.

## 계획

- [x] 프로젝트 지침·기존 교훈·워크트리 상태를 확인하고 감사 기준을 고정한다.
- [x] daemon/go2rtc/ffmpeg/HTTP·WebRTC/Viewer의 로그 생성 지점과 level 설정을 추적한다.
- [x] 현재 운영 프로세스·컨테이너와 실제 로그 표본, 저장량, 회전·보존 정책을 확인한다.
- [x] 카메라↔서버 및 서버↔뷰어 연결별로 기록되는 이벤트와 관측 공백을 교차 검증한다.
- [x] 몇 주간 실시간 관제 적합성, 즉시 확인할 명령/화면, 개선 우선순위를 검토란에 정리한다.

## 검토

- 2026-08-13 16:18~16:44 KST 운영 표본에서 Docker `camstation2`는 healthy/restart 0이며 활성
  카메라 8/8, recorder worker 8/8, Viewer 카메라 8/8의 현재 media progress가 정상이다. incident는
  0건이다. 다만 컨테이너 CPU는 세 표본에서 563~715%, host load는 17~19였고 `stream.warm`의
  `context deadline exceeded` warning이 당일 40건 확인돼 별도 원인 진단과 경보가 필요하다.
- 카메라→서버 로그는 `go2rtc` 기본 INFO/WARN과 worker 시작·종료 정도만 Docker stdout에 남는다.
  live-warm ffmpeg 출력은 폐기되고 recorder는 segment open 외 ffmpeg stderr를 폐기하므로 카메라별
  연결 성공, retry 단계, track별 byte/packet 진행, 정확한 단절 원인·기간을 사후 재구성할 수 없다.
  현재 상태 API는 producer/consumer와 worker 상태를 보여 주지만 이력은 남기지 않는다. 판정은 `부분`이다.
- 서버→Viewer playback 진단은 구조화돼 있으나 운영 level은 문서의 diagnostic `debug`가 아니라
  `info`다. attempt/recovery/failure는 남지만 debug 전용 socket open, signaling answer, first track,
  first media, session close는 남지 않는다. session ID와 Viewer ID/client 주소도 연결되지 않는다.
  nginx access/error는 HTTP·WebSocket만 기록하며 WebRTC UDP media는 볼 수 없고 request/upstream
  duration이나 correlation ID가 없다. 판정은 `부분`이다.
- Viewer Service는 `service.log`를 10 MiB×5개로 회전하도록 설계됐지만 실제 호출은 시작·종료·실패
  코드 중심이다. Electron Viewer는 lease의 `logPath`를 받아도 renderer/control/playback 수명주기를
  기록하지 않고 diagnostic event도 서버에서 보존하지 않는다. 따라서 Viewer 로컬 상세 로그는 `부족`이다.
  정확한 Windows 파일 크기는 대상 별칭(`test-pc` 또는 `monitoring-pc`)을 사용자가 지정하지 않아
  PC 제어 규칙에 따라 읽지 않았다.
- Docker는 `json-file` 10 MiB×3개라 현재 컨테이너당 약 30 MiB가 상한이며 컨테이너 재생성 때
  보존이 끊긴다. nginx는 daily rotate 14로 약 14일을 보존한다. DB events는 자동 만료 없이 계속
  증가하고, Viewer 정상 heartbeat도 10초마다 info event를 한 건씩 추가한다. 현재 크기를 기준으로
  Viewer 1대당 약 8,640건/일, 60,480건/주, 약 16.5 MiB/주가 예상되므로 `/logs`는 정상 heartbeat에
  묻히면서도 저장량은 무제한 증가한다.
- 컨테이너 시작 뒤 약 21분 표본은 playback 34건(시작 11, 성공 9, attempt 실패 10, episode 소진 3,
  primary probe 성공 1)이었고 장애는 16:24 KST까지의 시작·재연결 구간에 집중된 뒤 회복했다. nginx의
  같은 구간 1,353건 중 502 32건도 16:18:56~57 KST 컨테이너 기동 순간에만 발생했다. 이후 표본에는
  추가 502가 없었다.
- 몇 주간 운영 관제 기준으로 전체 판정은 `부족`이다. 우선순위는 (1) 컨테이너 재생성에도 남는
  구조화 중앙 로그와 최소 4주 보존, (2) soak 기간 playback debug 및 Viewer 실제 수명주기 로그,
  (3) 카메라 track progress·recorder segment·Viewer progress의 상태변화/주기 요약, (4) nginx
  latency/correlation 필드, (5) 정상 heartbeat event 억제 또는 자동 만료, (6) warm timeout·고부하
  경보다. 기존 `scripts/hourly-recording-monitor.sh`는 종료일·3-camera 가정·raw process args 때문에
  현재 8-camera 운영에 그대로 사용하지 않는다.
- 읽기 전용 API/DB/컨테이너/nginx 표본과 관련 구현을 교차 확인했으며 운영 설정, daemon, Viewer에는
  변경을 가하지 않았다.

---

# 2026-08-13 CamStation 2.0 `main` 승격 및 브랜치 정리

## 계획

- [x] 현재 워크트리의 완료 변경과 로컬·원격 `main`/`camstation2-initial` 포인터를 감사한다.
- [x] 이전 작업의 미커밋 변경을 목적별로 검토하고 테스트한 뒤 2.0 브랜치에 커밋·푸시한다.
- [x] Web lint/build, Viewer 테스트/build, Go 전체 테스트/build로 승격 후보를 검증한다.
- [x] 기존 1.x `main` 커밋을 원격 `archive/camstation-1.x-final` 브랜치와 annotated tag로 보존한다.
- [x] 준비된 release 브랜치에 최신 2.0을 병합하고 결과 tree가 승인된 2.0 tree와 같은지 확인한다.
- [x] 원격 `main`을 force push 없이 2.0 release로 fast-forward하고 원격 포인터·포함 관계를 검증한다.
- [x] obsolete 작업 브랜치의 삭제 여부를 안전하게 판정하고 최종 보존/정리 상태를 기록한다.

## 검토

- 이전 작업은 `da5f804` camera-layout migration, `f68b537` 검증된 Windows artifact 전달,
  `720423b` 운영 전환 기록의 세 커밋으로 분리해 `origin/camstation2-initial`에 반영했다.
- Go 전체 테스트, Web 71개 테스트·lint·production build, Viewer 48개 테스트·build와 embedded Web
  자산을 포함한 `camstationd` build가 모두 통과했다.
- 전환 전 원격 `main` `21e1e24`는 `archive/camstation-1.x-final` 브랜치와
  `camstation-1.x-final` annotated tag에 같은 커밋으로 원자 게시했다.
- release `3336c0b`는 기존 1.x release parent와 최신 2.0 `720423b`를 모두 보존하며, release와
  승인된 2.0의 tree는 `d6fcdab094c364aaba1055158a7fc3b5ad75bb12`로 동일하다.
- 원격 `main`은 force 없이 `21e1e24 -> 3336c0b`로 fast-forward됐다. 재조회에서 기존 1.x와 최신
  2.0이 모두 `main`의 ancestor이고 원격 `main` tree가 승인된 2.0 tree와 같음을 확인했다.
- 새 `main` 포함과 열린 PR 0건을 확인한 뒤 원격 `camstation2-initial`, `viewer-mobile-page`,
  `codex/windows-viewer-session-shortcut`을 원자 삭제했다. 해당 커밋은 모두 `main`에서 도달 가능하다.
- 로컬 `camstation2-initial`, `viewer-mobile-page`, release 브랜치와 clean release 임시 worktree를
  정리하고 현재 기본 worktree를 `main`으로 전환했다.
- `feature/always-hot-video`는 `main` 미병합이며 worktree 변경 2개가 있어 보존했다. prep 브랜치는
  커밋 자체는 병합됐지만 worktree 변경 24개가 있어 보존하고 삭제된 원격 upstream만 해제했다.
  Paseo 관리 worktree의 `analyze-viewer-command-features`도 임의 제거하지 않았다.
- 최종 원격 장기 브랜치는 `main`, `archive/camstation-1.x-final`, 미병합
  `feature/always-hot-video`만 남긴다. 1.x annotated tag `camstation-1.x-final`도 보존한다.

---

# 2026-08-12 로컬 Docker 2.0 전체 카메라 검증·모니터링 PC 준비

## 2026-08-13 즉시 전환 실행 — 07:43 KST 시작

- [x] 전환 전 로컬·운영·모니터링 PC 기준선과 양쪽 네트워크, rollback 자산을 재확인한다.
- [x] 로컬 Docker 2.0을 녹화 비활성으로 시작하고 자체 브라우저에서 8대 실제 영상과 레이아웃을
      확인한다.
- [x] 사용자 요청에 따라 자동 제어는 Viewer 2.0의 로컬 Docker 192 주소 설정까지만 수행한다.
      Viewer 1.0 종료와 Viewer 2.0 실행은 사용자가 직접 수행한다.
- [x] 모니터링 PC가 로컬 2.0으로 서비스되는 동안 운영 1.0 receiver를 종료하고 준비된 운영
      Docker 2.0을 시작한다.
- [x] 운영 10·192 직접 주소에서 8대 영상과 WebRTC를 확인한 뒤 nginx/domain을 2.0으로 전환한다.
- [x] 모니터링 PC Viewer 2.0 주소를 운영 도메인으로 변경하고 8대 전체화면을 확인한다.
- [x] 운영 Viewer 합격 뒤 로컬 Docker를 종료하고 운영 녹화 파일 증가·30분 rollover를 확인한다.
- [x] 각 단계의 KST 시각, 로그·캡처·해시, rollback 여부와 최종 상태를 검토 섹션에 기록한다.

### 즉시 전환 최종 검토 — 2026-08-13 09:31 KST

- 모니터링 PC는 Viewer 2.0.25로 업그레이드됐고 기존 config/client ID를 보존했다. 사용자가 Viewer
  1.0을 종료한 뒤 로컬 Docker에서 8대 영상을 확인했으며, 운영 전환 후에는 서버 주소를
  `https://cctv2.nuc.hmini.me`로 변경하고 `autoStart=true`로 인계했다.
- 운영 1.0 backend/backup/go2rtc를 정지한 뒤 Docker `camstation2`를 시작했다. 첫 실행에서 listener
  검사기의 `ss` 열 선택 오류로 자동 rollback이 한 차례 동작했고, 검사기를 고친 두 번째 실행은
  09:09:36 KST에 `running/healthy/restart 0`, 카메라 9/활성 8, recorder 8/8로 합격했다.
- nginx 후보의 기존 loopback upstream은 Docker bind와 맞지 않아 10대역 HTTP bind로 교정했다.
  graceful reload 직후 구 worker가 한 요청을 처리해 502가 발생한 첫 시도는 symlink를 자동 복구했고,
  reload convergence를 기다리는 재시도는 09:13:28 KST에 합격했다. HTTP는 HTTPS로 307 이동하며
  HTTPS health와 `/live`는 정상이다.
- 운영 직접 브라우저와 도메인 브라우저에서 8개 타일·`8 / 8 online`을 확인했다. 도메인 브라우저는
  약 32초 뒤 video 8개 모두 `readyState=4`, 재생 중으로 수렴했다.
- 로컬 Docker를 정지한 뒤 다시 얻은 모니터링 PC 최종 캡처는
  `work/windows-control-evidence/monitoring-pc/viewer-20260813T002850818Z-f202dc44c37f4182aa2290a85a83839c/viewer-window.png`
  이며 SHA-256은 `e3513d306c175464dbacf6206e9b260a499cf16dfcc1a23e5603d22eeb127296`다.
  실제 1/8 레이아웃의 영상 8개와 우측 `8 / 8 online`을 직접 확인했다. `소방서4`는 primary transport
  연결 제한 뒤 같은 카메라의 focus/MSE 대체 스트림으로 재생 중이며 영상 단절은 없다.
- Viewer 설정 전환은 기존 client ID를 보존했고 Service `Running/Auto`, Viewer 1.0 process 0,
  Viewer 2.0 process 4/session 1, MSI 2.0.25로 감사됐다. 일회성 Windows task/run 잔여물은 0이다.
- recorder는 활성, 30분, 700GB이고 worker 8/8 running이다. 열린 녹화 파일 8개는 5초 표본에서
  동일 inode 모두 증가했다. 09:30:24 KST의 30분 벽시계 경계에서 기존 8개 inode가 모두 교체됐고,
  1MiB보다 큰 완료 파일 8개(합계 1,461,211,303 bytes)가 새로 생성됐다. 뒤이어 열린 새 파일 8개도
  5초 표본에서 모두 증가했으며 recorder 8/8 running을 유지했다.
- 모니터링 PC의 운영 연결 합격 후 로컬 검증 Docker는 compose stop으로 `exited/0`이 됐고 데이터는
  보존했다. 운영 Docker는 무중단으로 `restart: unless-stopped`에 맞췄고, 1.0 세 unit은 설치·설정을
  보존한 채 disabled/inactive로 전환했다. rollback용 compose 백업과 1.0 unit은 삭제하지 않았다.

### Viewer 2.0 로컬 연결 실패 진단

- [x] 모니터링 PC의 Viewer/Service 프로세스와 최근 Windows crash 이벤트를 읽기 전용으로 확인한다.
- [x] Viewer Service의 관리 로그와 로컬 Docker의 Viewer 등록·HTTP/WebSocket 로그를 같은 시각축으로
      대조한다.
- [x] 실제 종료인지 설정 화면 전환인지, 서버 연결 실패인지, lease 충돌인지 원인을 판정한다.
- [x] 원인 판정 전에는 Viewer 재실행·설정 재변경·운영 서버 전환을 수행하지 않는다.

#### 연결 실패 진단 검토

- Viewer 2.0과 Service는 종료·충돌하지 않았다. Viewer browser/renderer/GPU/utility와 Service가 계속
  실행 중이고 Viewer 관련 crash event/dump도 없었다. 실제 창 캡처는 새 주소
  `192.168.0.154:18080`이 저장된 연결 설정 화면을 보여 주었다.
- 07:56:13~07:56:31 KST 로컬 Docker 로그에서 8대 WebRTC 시도와 WebSocket open은 확인됐지만
  `playing`/첫 media 성공 전에 페이지가 다시 로드됐다. 재시도는 07:56:26, 28, 30 KST에 반복됐다.
- 모니터링 PC renderer는 `192.168.0.154:18080`에 established 연결을 유지하지만 Viewer Service는
  변경 전 `192.168.0.172:18080`에 `SynSent`를 반복했다. 새 로컬 HTTP health와 WebRTC 18555 TCP,
  UDP route는 모두 `192.168.0.13` source로 정상이다. 로컬 `/api/viewers`가 빈 배열인 것도 Service가
  새 서버에 heartbeat하지 못한 상태와 일치한다.
- 원인은 설정 저장 후 실행 중인 Service control loop가 새 구성을 다시 읽지 않는 결함이다.
  Electron은 저장 응답의 새 URL로 즉시 live를 열지만 Service는 시작 시 캡처한 이전 URL로 재접속을
  계속해 connection을 `degraded`로 유지한다. Viewer 재연결 경로는 이 상태를 읽으면 setup 화면을
  선택하므로 사용자가 본 `잠깐 live -> 서버 입력 화면` 전이가 발생한다.
- 진단 중 Viewer/Service 재시작, 설정 재변경, 운영 전환은 하지 않았다. 연결 버튼 반복만으로는
  Service의 고정된 이전 URL이 바뀌지 않는다.

### Viewer 2.0 설정 hot-reload 수정·배포

- [x] 설정 commit 뒤 실행 중인 Service control loop가 취소되고 새 `serverUrl`로 재시작되는 실패
      재현 테스트를 먼저 추가한다.
- [x] config-change 신호를 Service lifecycle에 최소 범위로 연결하고 종료·동시 변경·재접속 경계를
      테스트한다.
- [x] Viewer가 저장 직후 잠깐 live를 연 뒤 degraded 상태로 setup에 복귀하지 않도록 상태 전이를
      회귀 테스트한다.
- [x] Go/Viewer 전체 테스트와 빌드, diff/비밀정보 검사를 통과시킨다.
- [x] 새 immutable Viewer MSI를 Windows 빌드 호스트에서 만들고 version/ProductCode/UpgradeCode,
      파일 수, SHA-256과 unsigned 내부 배포 경계를 확인한다.
- [x] 테스트 PC와 모니터링 PC 사이의 MSI 전달을 임의 SSH/SCP가 아니라 대상·파일명·크기·SHA-256을
      전후 검증하는 표준 artifact pull/push 경로로 고정하고 관련 테스트를 통과시킨다.
- [x] 모니터링 PC에 정확한 MSI만 업그레이드하고 기존 config/client ID와 Viewer 1.0 안전망을 보존한다.
- [x] 모니터링 PC에서 Service가 새 로컬 주소로 heartbeat하고 Viewer 2.0이 8대 live를 유지하는지
      구조화 로그·서버 등록·정확한 Viewer 창 캡처로 확인한다.
- [ ] 실패 시 새 Viewer 2.0만 닫고 이전 MSI 또는 Viewer 1.0 화면으로 복구하며 결과를 기록한다.

#### 수정 검토

- 실패 재현 테스트는 구현 전 Service control reload와 live 진입 시 reconnect timer 정리에서 각각
  실패했고, 구현 후 모두 통과했다.
- 수정 revision은 `448bee922b440c4a129e3d0efdc0d7c97d5ced5e`이며 Viewer 관련 5개 파일만
  커밋했다. 기존 camera import·전환 문서의 미커밋 변경은 포함하지 않았다.
- Service race test, Go 전체 패키지, Web 64개, Viewer 48개, Web lint/build와 Viewer build가
  통과했다.
- 모니터링 PC는 target/session/driver/task/script parity Status가 통과했고 Viewer Service는 계속
  Running이다. MSI 빌드용 `test-pc`는 현재 대상 사용자의 Explorer가 0개라 표준 target preflight가
  fail-closed 되었으며, 우회 빌드·모니터링 PC 빌드·설치 파일 직접 덮어쓰기는 수행하지 않았다.

### Viewer 2.0 설치본 동일성 확인

- [x] 테스트 PC와 모니터링 PC의 설치 제품/파일 버전과 Viewer·Service 핵심 SHA-256을 조회한다.
- [x] 두 PC의 `C:\Program Files\CamStation Viewer` 전체 파일 manifest SHA-256을 비교한다.
- [x] 테스트 PC에서 실영상 검증한 최종 설치본과 byte-for-byte 동일한지 판정한 뒤 연결 실패 진단에
      반영한다. 확인 중 프로그램 실행·종료·재설치는 하지 않는다.

#### 설치본 동일성 검토

- 두 PC 모두 Windows Installer 표시 버전 `2.0.24`, ProductCode
  `{3C8F9398-58AD-4F51-A77A-D8E612720CC8}`이다.
- 현재 MSI 소유 runtime 76개는 상대 경로·크기·SHA-256이 모두 일치했다. 핵심 SHA-256은 Viewer
  `9f1684db...2140bd`, Service `b8d84276...374245`, `app.asar`
  `0cfb6489...30f136`으로 동일하다.
- 모니터링 PC의 설치 root에만 구 bootstrap 세대의 비소유 잔여물
  `CamStationViewerBootstrap.exe`와 `current.json`이 있다. 두 파일의 합계가 정확히 전체 file count
  `78-76` 및 byte 차이를 설명하며, 현재 Viewer 2.0 프로세스와 Service는 표준 MSI 경로의 직접
  실행 파일을 사용한다. 따라서 연결 실패는 다른/구형 Viewer runtime 때문이 아니다.
- 테스트 PC는 현재 대화형 Explorer가 없어 일반 GUI preflight가 fail-closed 되었으므로, 고정 SSH
  host key·전용 관리 계정과 machine/identity/hash 검증을 유지한 읽기 전용 설치 감사만 수행했다.
  임시 감사 파일은 제거됐고 두 PC의 Viewer 실행·종료·설정은 변경하지 않았다.

실행 중 한 gate라도 실패하면 다음 단계로 진행하지 않고
[최소중단 전환 계획](../docs/2026-08-12_camstation2-simple-cutover-plan.md)의 해당 복구 경로를 따른다.

## 낮 시간 최소중단 전환 순서 — 고정

- [x] 실행 순서를 `로컬 Docker 자체 브라우저 8대 확인 → Viewer 1.0 종료 후 모니터링 PC의 Viewer
      2.0을 로컬 Docker에 연결 → 운영 1.0 종료 및 Docker 2.0 시작 → 운영 직접 브라우저 확인 →
      Viewer 2.0 주소를 운영 서버로 변경·재시작`으로 고정한다.
- [x] 로컬 Compose가 HTTP `10.0.0.16:18080`·`192.168.0.154:18080`과 WebRTC
      `10.0.0.16:18555`·`192.168.0.154:18555` TCP+UDP를 모두 공개하는지 확인한다.
- [x] 운영 Compose가 HTTP `10.0.0.26:18080`·`192.168.0.160:18080`과 WebRTC
      `10.0.0.26:18555`·`192.168.0.160:18555` TCP+UDP를 모두 공개하는지 확인한다.
- [x] 로컬은 녹화 비활성, 운영은 녹화 활성·30분 세그먼트이며 운영 `camstation2`가 검증 DB와
      이미지로 `Created` 상태인지 확인한다.
- [x] 로컬 Docker를 시작해 브라우저에서 8대 실제 영상과 동일 8타일 레이아웃을 다시 확인한다.
- [x] 모니터링 PC의 Viewer 1.0을 종료하고 Viewer 2.0을 로컬 `192.168.0.154`에 연결해 8대
      전체화면을 확인한다. 이후 모니터링 서비스는 로컬 Docker 2.0으로 계속 유지한다.
- [x] 모니터링 PC가 로컬 Viewer 2.0으로 안정적으로 서비스 중인 상태에서 운영 1.0 receiver를
      종료한 뒤 준비된 Docker 2.0만 시작한다.
- [x] 운영 직접 주소에서 8대와 양쪽 HTTP/WebRTC를 확인한 뒤 nginx/domain을 2.0으로 전환한다.
- [x] 운영 브라우저 검증 뒤 모니터링 PC의 기존 Viewer 2.0 주소를 운영 도메인으로 변경하고
      8대 전체화면을 확인한 뒤 로컬 Docker를 종료한다.
- [x] 화면 복구 뒤 recorder 8개, 열린 파일 증가와 30분 rollover를 확인한다.

상세 실행·90초 복구 기준은
[최소중단 전환 계획](../docs/2026-08-12_camstation2-simple-cutover-plan.md)을 따른다. 운영 1.0과
운영 Docker 2.0은 같은 출발 IP이므로 절대 동시에 카메라를 수신하지 않는다. 로컬 개발 Docker는
다른 출발 IP이므로 운영 1.0을 유지한 채 사전 검증한다.

## Paseo 예약 전환 — 2026-08-13 01:00 KST

- [x] 예약 시각을 `2026-08-13 01:00 Asia/Seoul`로 절대 고정한다.
- [x] 서버·nginx·도메인·모니터링 PC를 read-only 사전 확인하고 현재 전환 입력과 rollback 자산을
      확인한다.
- [x] 서버 direct browser → nginx/domain → monitoring Viewer 2.0 → 30분 rollover 순서와 모든 단계의
      full rollback을 포함한 일회성 Paseo prompt를 준비한다.
- [x] 인증된 Paseo MCP로 cron `0 1 13 8 *`, timezone `Asia/Seoul`, max-runs 1을 등록하고
      schedule ID·nextRunAt·prompt/cwd/provider를 재조회한다.
- [ ] 실행 후 Paseo 로그에서 `CUTOVER_COMPLETE` 또는 rollback 결과와 증거 SHA를 확인한다.

## Paseo 예약 등록 검토

- 실행 프롬프트는 `work/paseo-cutover-20260813/prompt.md`에 준비했고 SHA-256은
  `b49cd6b022eed7deb42ef9f7601de161294b970f1d776b6576010af69e11b2f1`이다. 전환·검증·전체
  rollback과 최종 결과 marker를 포함하며 필수 문구 검사를 통과했다.
- 예약 계약은 cron `0 1 13 8 *`, timezone `Asia/Seoul`, max-runs `1`, cwd
  `/workspace/CamStation`, provider `codex`로 고정했다.
- CLI에는 평문 인증값이 없었으나 현재 Paseo 세션에 주입된 인증 MCP를 사용해 daemon password나
  설정을 변경하지 않고 등록했다. schedule ID는 `8c1a18df`, 상태는 `active`, nextRunAt은
  `2026-08-12T16:00:00Z` = `2026-08-13 01:00 KST`, max-runs는 `1`, 만료는
  `2026-08-13 10:19:39 KST`다.
- 독립 `inspect_schedule` 재조회에서 cwd `/workspace/CamStation`, isolation `local`, provider/model
  `codex/gpt-5.6-sol`, mode `full-access`, thinking `max`, plan mode, 원본 prompt, 실행 이력 0건을
  확인했다.


## 현재 실행 범위

- [x] 운영 서버의 `camstation2-canary`만 정상 정지하고 기존 1.0 서비스·카메라 수신은 유지한다.
- [x] 현재 1.0 전체화면과 camera `9/8/1`, layout `1/8` online snapshot을 보존한다.
- [x] legacy 설정·이력은 승계하지 않고 camera 연결 graph와 layout만 fresh 2.0 DB로 가져온다.
- [x] 별도 IP를 사용하는 로컬 Docker 2.0을 녹화 비활성(`CAMSTATION_RECORDING_ENABLED=false`,
      recorder 0)으로 기동해 활성 카메라 8대의 실제 live 수신을 확인한다.
- [x] 로컬 브라우저에서 현재 1.0과 동일한 8타일 레이아웃과 영상 8개 실제 재생을 확인한다.
- [x] 모니터링 PC의 Viewer 설정·실행 상태는 변경하지 않고 운영 1.0도 유지한다.

## 운영 Docker stopped-stage 준비

- [x] 로컬 브라우저에서 8대 live·동일 레이아웃을 확인한 현재 2.0 컨테이너를 정상 정지하고,
      그 승인된 DB를 SQLite 일관성·해시 확인 후 그대로 운영에 사용한다. 다시 import해 별도 DB를
      만들지 않는다.
- [x] 운영 서버의 카나리와 분리된 production 전용 deploy/state/media 경로를 만들고, 검증한 `.12`
      이미지 ID, 양쪽 LAN HTTP/WebRTC, 운영 녹화 활성 정책을 고정한 Compose를 root 전용으로 배치한다.
- [x] Compose render, DB SHA-256·SQLite quick-check, 파일 소유권과 경로를 검증한다.
- [x] 기존 중지된 카나리 컨테이너·전용 DB/media/deploy 경로를 정확히 제거하고, 새 최종 2.0
      컨테이너는 `docker compose create`까지만 수행한다.
- [x] 새 컨테이너가 `Created`이고 PID·포트 bind·카메라 연결·녹화가 0이며, 기존 1.0은
      active/enabled이고 기존 포트 소유권을 유지하는지 최종 확인한다.
- [x] 운영 staging manifest에 이미지 ID·DB/Compose 해시와 롤백 경계를 기록하고 검토 결과를 남긴다.

## 운영 Docker stopped-stage 검토

- 로컬 검증 컨테이너는 정상 종료(exit 0)했고, 승인 DB를 다시 변환하지 않고 그대로 배치했다.
  DB SHA-256은 `7c60dd8a8a23dafb4eabf6d0893bfed95010deeebb47921b1143c7252b7caa96`이다.
- 운영 DB는 SQLite quick-check `ok`, camera `9/8/1`, `소방서2` 비활성, recording stream 9,
  layout `1/8`, 정책 `30분/30일/700GB`, recording segment 0을 확인했다.
- 기존 `camstation2-canary` 컨테이너·네트워크와 전용 deploy/state/media 경로는 사용자 승인에 따라
  삭제했다. 카나리 데이터는 복구할 수 없지만 검증 이미지와 로컬 승인 DB, 기존 1.0 원본은 보존했다.
- 최종 Compose는 `/opt/camstation2/docker-production`, state는
  `/var/lib/camstation2-production/data`, media는 `/mnt/hdd/camstation2-production`에 분리했다.
  Compose SHA-256은 `9a7eab5fb167004cf06cc64e737c0c24d619d09145b98cb69ad5802dda970880`이다.
- 최종 컨테이너 `camstation2`는 `created`, PID 0, start timestamp 없음, restart `no`, network endpoint
  0이다. 10·192 대역용 18080/18555 TCP·UDP mapping은 구성됐지만 listener는 0이며 녹화 파일도 0이다.
- 기존 1.0 핵심 서비스는 모두 active/enabled이고 PID `248/326/247/396/246`이 작업 전후 동일하다.
  모니터링 PC는 변경하지 않았다.

## 금지 경계

- 운영 1.0 서비스·DB·카메라 설정·Viewer 1.0을 중지하거나 변경하지 않는다.
- 현재 단계에서 production Docker 전환, nginx handoff 또는 운영 Viewer 자동시작 인계를 하지 않는다.
- 운영 카나리 DB를 로컬 전체 카메라 DB나 최종 운영 DB로 재사용하지 않는다.
- 로컬 검증 중에는 녹화 파일을 생성하지 않는다. 운영 이관 때만 운영 저장경로·녹화 정책을 별도로
  적용한다.

---

# 2026-08-12 CamStation 1.0 → Docker 2.0 단순 전환 계획 기록

## 고정된 범위

- [x] 현재 1.0 전체화면을 기준 캡처로 보관한다.
- [x] 카메라 연결정보 `9대/활성 8대/소방서2 비활성`과 현재 8타일 레이아웃 원본을 안전한 온라인 snapshot으로 보존한다.
- [ ] fresh 2.0 DB에 위 두 항목만 반영하고 오프라인 무결성을 확인한다.
- [ ] 최종 Docker 2.0과 Viewer 2.0을 실행하지 않은 상태로 준비한다.
- [ ] 전환 시 Viewer/서버 1.0을 내린 뒤 Docker/Viewer 2.0을 올린다.
- [ ] 처음 수행하는 2.0 실제 연결 시험에서 동일한 전체화면 8대 라이브를 확인한다.
- [ ] 실패 시 Viewer/Docker 2.0을 내리고 보존된 서버/Viewer 1.0을 다시 올린다.

상세 순서는 [단순 전환 계획](../docs/2026-08-12_camstation2-simple-cutover-plan.md)을 따른다.
운영 서버의 1.0과 운영 Docker 2.0은 같은 출발 IP이므로 동시에 카메라를 수신하지 않는다. 로컬
개발 Docker는 다른 출발 IP이므로 운영 1.0을 유지한 채 사전 수신 시험을 수행한다. 기준 화면 캡처는
사전에 한 번만 수행한다.

## 검토

- 기준 전체화면은 2560×1440 PNG로 보관했다. 8대 모두 live이고 현재 1.0 레이아웃이 보인다.
  PNG SHA-256은 `4233fc1e359bdea59469b041598c32d55d42f88e65118d6637e8579a324fb2a4`다.
- 1.0 online snapshot은 root 전용 `0600` 파일로 생성했고 SQLite quick-check `ok`, 카메라
  `9/8/1`, substream 9, layout `1/8`을 확인했다. snapshot SHA-256은
  `b51abc213db94a1aca1472722865ba435771f558fa0838eecf56269d15178fe0`, secret-safe canonical
  fingerprint는 `636af019dce2debb7c30e54b49966be9a1afe2679d3f0a30c0d0fa305bc80874`다.
- 현재 importer는 camera/layout뿐 아니라 legacy settings도 함께 변환하므로 사용자 승인 범위와
  일치하지 않는다. 기존 candidate DB도 canonical mismatch여서 사용하지 않았다. camera/layout-only
  import와 명시적인 fresh 2.0 운영 설정 모드를 추가한 뒤 새 target DB를 만들어야 한다.
- 현재 `.12-canary`는 healthy지만 3-camera/1분/20GB/restart=no 카나리다. production Docker
  Compose/env/start-stop/rollback 계약은 아직 없고 기존 전환 helper는 systemd 전용이다. 카나리 DB나
  systemd rollback helper를 최종 Docker 전환에 재사용하지 않는다.
- 모니터링 PC에는 Viewer 2.0.24와 Viewer Service가 설치돼 있고 UI process 0,
  `autoStart=false`다. 저장 서버 주소는 최종 주소가 아니므로 production Docker 주소가 확정된 뒤
  전환 직전에 한 번만 갱신한다. 기존 CamViewer 1.0 process 6개는 그대로 유지했다.
- 운영 1.0 서비스와 현재 카나리 컨테이너는 중지·재시작·재생성하지 않았고 카메라 연결 시험도
  수행하지 않았다. Windows 제어 task/worker residue는 0이다.

## 감사 후 고정한 준비 구현

- [ ] `camstation-migrate`에 camera+layout-only source scope와 명시적인 fresh 2.0 recording/backup
      정책을 추가하고 기존 import 계약은 보존한다.
- [ ] 새 모드의 synthetic test에서 camera `9/8/1`, layout `1/8`, 과거 recording/backup/viewer 이력
      0, secret-free manifest, atomic/no-overwrite/idempotent verify를 증명한다.
- [ ] 카나리와 경로·project name이 분리된 production Docker Compose/env 예제와 Docker 전용
      start/stop/rollback 절차를 추가한다. 기본 상태에서는 컨테이너를 자동 시작하지 않는다.
- [ ] Compose render/policy test와 전체 Go/Web/Viewer 검사를 통과시킨 뒤에만 운영 서버의 별도
      stopped staging 경로에 파일·fresh DB를 준비한다.

---

# 2026-08-12 과거 확대 전환안 — 실행 기준 아님

> 아래 섹션은 범위가 확대됐던 과거 검토 기록이다. 실제 전환에는 문서 맨 위의 단순 전환 계획과
> [단순 전환 계획](../docs/2026-08-12_camstation2-simple-cutover-plan.md)만 사용한다.

## 목표와 전환 경계

- 소스의 기본 브랜치를 1.x `main`에서 승인된 CamStation 2.0 tree로 교체하되, 두 이력이 공통
  조상이 없으므로 일반 병합이나 파일 혼합을 하지 않는다. 1.x tip을 보호 branch/tag로 보존하고
  두 parent를 가진 교체형 merge commit의 결과 tree가 승인된 2.0 tree와 byte-for-byte 같아야 한다.
- 현재 운영 1.x와 Docker 2.0 카나리, 모니터링 PC의 CamViewer 1.0 화면은 실제 유지보수 창 전까지
  계속 유지한다. 사전 준비는 inactive/side-by-side 상태로 끝내며 nginx active target, 정식 운영
  포트, recorder/backup 소유권, Viewer auto-start를 미리 넘기지 않는다.
- 최종 런타임은 과거 systemd 2.0 초안이 아니라 현재 검증 중인 Docker 2.0을 기준으로 한다. 기존
  systemd 전환 문서·helper를 그대로 실행하지 않고 Docker compose, state/media mount, WebRTC
  monitoring-LAN candidate, nginx, restart policy, rollback image/DB를 다시 하나의 계약으로 맞춘다.
- 모니터링 PC는 Viewer 2.0 목표 장비다. CamViewer 1.0 실행 상태는 전환 전 rollback 기준선일 뿐
  영구 보존 정책이 아니다. 검증된 Viewer 2.0 MSI를 미리 설치·사전 구성하되 `autoStart=false`와
  UI 미실행으로 두고, 실제 전환 gate 통과 뒤 1.0 정상 종료 → 2.0 실행/auto-start 인계 순서를 쓴다.
- 2.0은 fresh DB로 시작한다. 1.x에서 이관하는 것은 카메라 연결 정의와 현재 모니터링 화면의
  레이아웃이다. 기존 recordings와 녹화 metadata, 일반 settings, backup history/mark, Viewer
  registry/command/telemetry는 이관하지 않는다. 1.x 원본 DB/media는 rollback 자산으로만 보존하고
  2.0 DB에 혼합하지 않는다.

## 사전 준비 계획 및 합격 기준

- [ ] Git fetch 후 원격 `main`, 정리 후 깨끗한 `camstation2-initial`, 열린 통합 PR, branch protection,
      merge-base, commit/tree hash를 기록한다. 현재 작업트리의 미커밋 제어 변경은 사용자가 별도로
      정리하므로 전환 blocker나 통합 후보 입력으로 취급하지 않는다.
- [x] `control-camstation-windows-pc`를 테스트 PC/모니터링 PC의 실제 기술 경로에 맞게 완성한다.
      대상 alias 선택, pinned SSH, hostname/maintenance identity, Explorer session, Cua/UIA, 전체 화면과
      exact-window 캡처, foreground 입력, PID/HWND 수명주기, artifact hash, exact cleanup을 테스트한다.
- [x] 위 미커밋 변경을 전체 Viewer 테스트·skill validator·PowerShell 5.1 parser·두 PC `Status`와
      무해 batch로 검증하고 2.0 브랜치에 독립 커밋·push한다.
- [ ] 전용 clean worktree에서 `origin/main` 기반 통합 branch를 만들고, `legacy/1.x` 보호 branch와
      1.x/2.0 release tag 후보를 준비한 뒤 교체형 merge commit을 생성한다. 두 parent와 tree equality,
      1.x 파일 혼입 0건을 검사한다.
- [ ] 교체형 merge 후보에서 Go/Web/Viewer 전체 test·lint·build, production policy, release/importer/
      Docker compose/nginx 파일 존재와 비밀정보 검사를 통과시킨 뒤 review 가능한 원격 branch/PR로
      게시한다. `main` 갱신은 이 gate가 모두 통과한 뒤 수행한다.
- [ ] 모니터링 PC의 현재 CamViewer 1.0/Viewer Service/설치 MSI/registry/auto-start/CPU 기준선을
      수집한다. 운영 서버가 게시한 검증된 Viewer 2.0 artifact의 version, size, SHA-256, signer/API
      compatibility를 release manifest와 대조한다.
- [ ] 검증된 Viewer 2.0을 모니터링 PC에 side-by-side/upgrade 설치하고 management service만 준비한다.
      기존 운영 URL·승인된 display/client identity를 보존하되 `autoStart=false`, 2.0 UI 미실행,
      CamViewer 1.0 PID/화면 유지, 재부팅 미실행을 합격 조건으로 한다.
- [ ] 운영 서버에서 1.x, Docker 2.0 카나리, 포트/nginx, camera `9/8/1`, recorder/backup, state/media
      mount, restart policy, rollback image/DB를 읽기 전용 재확인한다. 카나리 3-camera DB를 최종 DB로
      오인하지 않고 최신 1.x online snapshot에서 카메라 연결 정의와 승인된 현재 레이아웃만 별도
      비활성 Docker state로 import/verify한다. 녹화·backup·Viewer 이력 row는 0건이어야 한다.
- [ ] Docker 최종 release/compose/nginx 전환·rollback 절차를 작성/검증한다. 전환 전에는 정식
      운영 주소, 1.x 서비스, 1.x DB/media, CamViewer 1.0 startup을 바꾸지 않는다.

## 실제 전환 창 순서

1. 변경 freeze 공지 → 최종 1.x online snapshot·hash, 카메라 연결 정의 `9/8/1`, 현재 운영
   레이아웃 fingerprint와 모니터링 PC 전체 화면을 확인.
2. 별도 fresh Docker state에 camera+layout import/verify/idempotency → recordings/settings/
   backup/Viewer 이력 미이관 확인 → release/compose/env/ACL/hash 고정.
3. nginx maintenance → 1.x backend/backup/go2rtc 정확한 정상 정지 → 충돌 포트 해제 확인.
4. Docker 2.0 최종 state/media로 기동 → health, `9/8/1`, 8 live/WebRTC, recorder 8 증가 확인.
5. nginx 정식 운영 주소를 2.0으로 전환 → 외부 UI/API/WebSocket과 임시 1.0 Viewer 표시 경로 확인.
6. 새 2.0 녹화의 첫 rollover 8개·재생 가능·운영 backup upload/mark 8/8 확인 후 60분 server soak 통과.
7. 모니터링 PC CamViewer 1.0 정상 종료 → Viewer 2.0 설정을 정식 URL/`autoStart=true`로 원자 적용
   → 활성 console에서 실행 → 현재 1.0과 같은 레이아웃의 활성 8대가 모두 라이브 재생되는지와
   Viewer telemetry/focus/fullscreen/reconnect를 확인.
8. 승인된 logoff/logon 또는 reboot 1회로 Viewer 2.0 자동 복귀 확인. 1.x 실행 자산은 7일,
   서버/DB snapshot은 최소 30일 보존한다.

## 즉시 중단·롤백 기준

- 카메라 `9/8/1` 불일치, 활성 live/recorder가 8 미만, 비활성 카메라 자동 활성화, DB quick-check 실패,
  crash loop, 운영 backup target/mark 불일치, secret 노출은 server rollback 조건이다.
- 서버 gate가 정상인데 Viewer 2.0 화면/telemetry/auto-start만 실패하면 서버를 되돌리지 않고
  Viewer 2.0만 중지·`autoStart=false`로 복원한 뒤 CamViewer 1.0을 재실행하는 client-only rollback을 쓴다.
- runtime rollback은 Git `main` force-push와 분리한다. 운영 복구는 보존한 1.x branch/tag, DB/media,
  nginx include와 정확한 lifecycle 명령으로 수행한다.

## 검토

- 진행 중. 계획 기준은 정리 후 clean 2.0 branch다. 남은 선행 작업은 교체형 merge 후보 생성,
  모니터링 PC Viewer artifact 재판정, 그리고 과거 systemd 절차와 현재 Docker 런타임의 정합화다.

---

# 2026-08-12 테스트 PC·모니터링 PC 제어 프로필 정규화

## 목표와 안전 경계

- 기존 프로젝트 스킬 `control-camstation-windows-pc` 하나를 유지하되 `test-pc`와
  `monitoring-pc`를 명시적으로 선택하는 로컬 프로필 계층을 추가한다. 두 PC를 별도 스킬로
  복제하지 않는다.
- host, 유지보수 계정, private-key/known-hosts 경로는 Git에 넣지 않고 ignored local profile에만
  둔다. tracked 스킬에는 비밀이 아닌 machine/interactive-user/역할 기준선과 선택 절차만 둔다.
- 모든 원격 동작은 대상 alias → pinned SSH profile → 원격 hostname/maintenance identity → 표준
  `Status`의 TargetUser/session 검증을 통과한 뒤에만 수행한다. 불일치·모호한 대상·stale script는
  mutation 전에 fail closed한다.

## 계획 및 합격 기준

- [x] 현재 두 PC의 hostname, maintenance identity, interactive user/session, Viewer/Cua 기준선과
      canonical script hash를 읽기 전용으로 다시 확인한다.
- [x] ignored local target profile과 tracked schema/example을 만들고, exact alias 외 입력·중복
      machine·누락 파일·느슨한 SSH 옵션을 거부하는 deterministic target wrapper를 구현한다.
- [x] 스킬 본문과 대상별 reference에는 두 PC의 접속·identity/session·display 등 제어 설정만
      기록한다. Viewer 버전, 전환 단계, 운영 역할은 PC 제어 프로필에 고정하지 않고 요청 시점의
      관찰 및 별도 배포 절차에서 결정한다.
- [x] SSH 대상 확정, Windows identity/session preflight, Cua/UIA 호출, background→foreground 입력
      승격, 전체 데스크톱/정확한 창 캡처 선택, 프로세스 수명주기, artifact hash와 exact cleanup 등
      재현 가능한 기술 계약을 스킬 reference에 기록한다.
- [x] CPU 포화 진단, desktop/window capture 차이, UIA `element_index`, UWP host cleanup,
      foreground escalation 등 실제 제어 실패 교훈을 plan/cleanup 기술 절차에 반영한다.
- [x] source-policy/target-profile 회귀 테스트, 전체 Viewer 테스트, skill validator, shell/PowerShell
      parser, local/remote hash parity를 통과시킨다.
- [x] 두 target alias로 실제 `Status`를 실행해 서로 다른 PC·사용자·기준선이 선택되고 mutation과
      임시 task/run 없이 종료됨을 증명한 뒤 Review와 lessons를 갱신한다.
- [x] SSH remote command에는 stdin 전체를 읽는 짧은 고정 PowerShell bootstrap만 두고 실제 source는
      stdin으로 보내 Windows 명령줄 길이 제한과 인라인 quoting 재발을 막는다.
- [x] Viewer 버전·이관 범위·전환 순서·clean-state 정책은 범용 PC 제어 스킬과 target reference에서
      제거하고 독립 운영 문서에 둔다. 스킬에는 명시적으로 요청된 설정을 수행하는 기술 경로만 남긴다.

## 검토

- 기존 프로젝트 스킬 하나를 유지하고 `test-pc`와 `monitoring-pc`를 필수 alias로 만들었다. 실제
  접속 주소·키·known-hosts 경로는 ignored `work/windows-control-targets.json`에만 두고, tracked
  example에는 placeholder와 비밀이 아닌 machine/Windows identity/session 계약만 남겼다. Viewer
  버전이나 전환 정책은 PC 제어 스킬·프로필에서 분리해 독립 운영 문서에만 남겼다.
- 새 `Invoke-CamStationWindowsTarget.mjs`는 임의 host/중복 옵션을 받지 않는다. public-key-only,
  strict known-host, exact machine/maintenance identity, Explorer owner/session, WTS session state를
  검증한다. script sync, pinned setup, elevated system script, Cua/UIA batch, 전체 데스크톱, 정확한
  Viewer 창, exact cleanup을 같은 target 경계로 실행하며 전송 파일 parser/SHA-256과 증거 파일
  SHA-256을 확인하고 원격 staging/run을 삭제한다.
- 일반 Windows 관리와 대화형 GUI를 기술적으로 분리했다. system mode는 1MiB 이하 `.ps1`을
  hash/parser 검사해 maintenance context에서 실행하고, Plan은 one-shot InteractiveToken task와
  UTF-8 stdin으로 session 1 Cua daemon을 사용한다. UIA는 fresh PID/window ID/element token,
  background-first, `verifyWith`, `element_index`, exact PID cleanup을 요구한다. 전체 데스크톱과
  Viewer exact-window 캡처도 서로 대체하지 않는다.
- 정확한 Viewer 캡처가 기존 최대화 창을 `SW_RESTORE`하던 부작용을 제거했다. 최대화 상태는
  보존하고 minimized 창은 배치를 바꾸지 않은 채 실패한다. VM display에서 driver desktop JSON이
  깨질 때만 interactive GDI fallback을 허용하지만, WTS가 `Disconnected`이면 task를 만들기 전에
  중단한다.
- 실제 alias 검증은 `test-pc -> WIN11-DELL\\dyllislev/session 1`, `monitoring-pc ->
  NUC\\dyllislev/session 1`로 일치했다. 최종 Status는 각각 약 3.76초/2.58초였고 Cua 0.19.3,
  telemetry off, TCP/firewall 0, control/setup/capture/configure task 0이다. 일곱 canonical Windows script는
  두 PC 모두 로컬/원격 SHA-256 parity가 참이다.
- WTS 기준으로 WIN11-DELL session 1은 현재 `Disconnected`라 GUI/capture가 mutation 전에 정확한
  재연결 안내와 함께 실패했다. NUC session 1은 `Active`이며 표준 전체 화면 캡처가 worker 8.29초에
  완료됐다. PNG 4,837,036 bytes, SHA-256
  `5b16cdc9deac4977ead552e8ee2cd010ada723b55bab98d78bb28346ad88e9de`, driver capture 2560x1440을
  직접 열어 현재 CamViewer 화면과 8개 온라인 영상을 확인했다. `TaskDeleted=true`, remote run 제거,
  사후 task 0개다.
- 최종 읽기 전용 상태에서 두 PC의 `CamStationViewerService`는 모두 `Running`이다. 이번 script sync는
  서비스·Viewer 프로세스·방화벽을 변경하지 않았고 제어 스킬에도 애플리케이션 상태를 고정하지 않았다.
- 전체 Viewer 테스트 47개, skill validator, Node syntax, 두 PC의 원격 Windows PowerShell 5.1 parser,
  로컬/원격 hash parity와 `git diff --check`가 통과했다. 검증된 변경 묶음은 2.0 브랜치의 독립
  커밋으로 원격에 push한다.

---

# 2026-08-12 모니터링 PC 표준 제어 편입

## 범위와 목표

- 사용자가 모니터링 PC도 테스트 PC와 같은 수준으로 화면 확인·일반 PC 조작·Viewer 검증을
  수행할 수 있도록 명시적으로 승인했다. 테스트 PC 전용 임시 경로를 복제하지 않고 프로젝트
  스킬 `control-camstation-windows-pc`의 동일 설치기·launcher·worker 계약에 편입한다.
- 저장된 환경에서 모니터링 PC의 정확한 host, 고정 host key, 유지보수 계정과 현재 로그인된
  대화형 사용자를 먼저 식별한다. 여러 대상이거나 불일치하면 mutation 전에 멈춘다.
- 새 외부 원격제어 서비스·listener·방화벽 규칙·저장 비밀번호는 추가하지 않는다. 기존 승인된
  SSH 경로와 대상 사용자 session의 local Cua daemon만 사용한다.

## 계획 및 합격 기준

- [x] 저장된 운영 자료와 SSH 자산으로 정확한 모니터링 PC 한 대 및 대화형 사용자/session을
      식별하고 현재 Viewer·네트워크·방화벽·예약 작업 기준선을 수집한다.
- [x] 기존 고정 SSH 경로가 없으면 프로젝트의 reviewed SSH bootstrap 절차로 host key를 직접
      확인하고 전용 유지보수 키/계정을 최소 권한 범위로 구성한다.
- [x] 동일한 pinned Cua 0.19.3 설치기와 프로젝트 control scripts를 동기화하고 파일/archive 해시,
      Authenticode, telemetry, autostart, session daemon을 검증한다.
- [x] 정확한 AnyDesk 서비스·프로세스만 종료하고, 종료 유지 여부와 동일 카운터의 CPU 전후 차이를
      측정해 CPU 여유 회복을 증명한다. SSH·Viewer·Cua는 변경하지 않는다.
- [x] 표준 `Status`와 무해한 한 batch로 화면 확인·입력·창 종료·사후 assertion·실패 cleanup을
      실제 검증하고 경과 시간을 기록한다.
- [x] NUC 제어 지연이 CPU·메모리·디스크 병목인지, SSH/PowerShell·Task Scheduler 지연인지,
      중단된 진단/제어 프로세스나 일회성 task 잔여물인지 분리해 현재 시점 증거로 판정한다.
- [x] 임시 task/run/artifact를 정리하고 Viewer 서비스/프로세스, listener, 방화벽, 로그인 session의
      의도하지 않은 변화가 없음을 최종 감사한다.
- [x] 로컬 프로젝트 테스트·skill validator·PowerShell parser·원격 script hash parity를 재검증한다.
- [x] 사용자의 후속 요청대로 특정 프로그램 창이 아닌 session 1 전체 데스크톱을 표준 Plan으로
      캡처하고, PNG 해시·실제 화면·task/run cleanup 및 Viewer/Cua 연속성을 검증한다.

## 검토

- 2026-08-12 18:37~18:42 KST의 읽기 전용 성능 카운터에서 4개 논리 CPU의 전체 사용률은
  최초 표본과 후속 3개 표본이 모두 100%였다. 같은 시점의 정규화된 상위 점유율은 CamViewer
  renderer/GPU/utility 합계 약 50.7%, AnyDesk 27.5%, DWM 9.0%, TiWorker 3.8%, System 2.5%로,
  제어 응답 지연의 현재 주원인은 CPU 포화다.
- 사용 가능 메모리는 25,975MB/32,653MB, commit 17%, paging 0/s였고 물리 디스크 사용률·대기열·
  처리량은 모두 0이었다. 따라서 메모리나 디스크 압박으로 판정할 증거는 없다.
- CamViewer 6개 프로세스는 main 2, GPU 1, renderer 1, utility 2의 Electron 프로세스 트리이며
  모든 부모 PID가 존재했다. 중단된 control/setup task, Calculator, 이전 진단 PowerShell은 없고
  Cua daemon만 session 1에서 예상대로 유지된다. 중단된 acceptance run에는 비어 있는 run
  directory 하나만 남았으며 실행 중인 task/process나 result/error는 없어 좀비 프로세스가 아니다.
- 모니터링 PC 편입과 실제 무해한 제어 acceptance는 사용자의 성능 확인 질문을 우선하기 위해
  중단했으며, 이후 사용자의 요청에 따라 AnyDesk 종료와 acceptance를 재개했다.
- 2026-08-12 18:48 KST에 정확한 `AnyDesk` 서비스와 설치 경로가 일치하는 프로세스만 종료했다.
  서비스는 `Stopped`, 프로세스는 0개이고 `StartMode=Auto`는 유지했다. 종료 후 5개 CPU 표본은
  28~43%, 평균 33.4%였고 최종 3개 표본도 29~44%, 평균 36%였다. 기존 100%에서 약 64~67%
  여유가 생겼으며 표준 `Status` 내부 시간도 20,288ms에서 2,184~2,286ms로 단축됐다.
- 계산기 무해 batch를 재개하면서 heterogeneous UIA 요소 중 선택 조건 필드가 없는 항목을 전체
  오류로 처리하는 `$select` 결함을 발견했다. canonical worker가 해당 항목을 비일치로 건너뛰도록
  최소 수정하고 회귀 테스트를 추가했으며, 실제 닫기 요소의 정확한 필드 `element_index=4`를 사용했다.
- 최종 run `20260812T100202505Z-27b15e9f25c04b70abefacd918c304bc`는 session 1에서 14,122ms에
  완료됐다. 계산기 `0` 화면을 캡처하고 버튼 `1`을 background UIA로 클릭해 `1` 화면을 캡처한 뒤
  title-bar 버튼으로 닫았으며, 최종 해당 창 0개 assertion과 `TaskDeleted=true`를 통과했다. 두 PNG의
  원격 기록/재계산 SHA-256은 각각 `d3b39c8d43232c49722c864e4b574fd2417c92420bd9dff27a61f62afc963ab4`,
  `b0f71140f3e254a604e4453edb4f1ba0064867f5c3090d15c6f5b7da06e134b0`로 일치했고 직접 `0 -> 1`을
  확인했다.
- 성공 run과 이전 중단 run은 표준 Cleanup으로 제거했다. 전송 계획, 임시 캡처, 창 없는 계산기 host
  PID도 정확히 정리했으며 최종 control/setup task, worker, run directory, Calculator/
  ApplicationFrameHost는 모두 0개다. AnyDesk는 `Stopped`/프로세스 0, Viewer Service는 Running,
  기존 CamViewer 6개 PID와 Cua PID 1608은 그대로이고 Cua TCP/firewall 수는 0이다.
- 전체 Viewer 테스트 42개, skill validator, 원격 PowerShell parser 오류 0, installer/launcher/worker
  로컬·원격 SHA-256 parity와 `git diff --check`가 통과했다. Cua 0.19.3 공식 파일 해시는 일치하며
  실행 파일 Authenticode 상태는 기존과 같이 `NotSigned`다.

---

# 2026-08-12 WinPC 제어 프로젝트 스킬 정규화

## 문제와 목표

- 기존 `verify-windows-viewer-gui`는 Viewer 캡처에만 최적화되어 있어 같은 Windows 세션 제어를
  범용 조작마다 다시 조립하게 만든다. 실제 단순 제어에서 예약 작업, PowerShell JSON/UTF-8,
  Session 0/1, named-pipe 사용자 경계와 사후 검증을 반복 해결하느라 약 51분이 걸렸다.
- 기존 스킬을 별도 스킬로 복제하지 않고 `control-camstation-windows-pc`로 승격한다. Viewer
  exact-window 검증은 보존하고, driver 설치·상태 점검·일반 창/화면/입력 제어를 같은 표준 실행
  경로의 작업 모드로 합친다.
- 단순 제어의 목표는 이미 설치된 테스트 PC에서 하나의 batch를 한 번 실행해
  `preflight -> observe -> act -> verify -> cleanup`을 끝내는 것이다. chat에서 인라인 예약 작업이나
  긴 EncodedCommand를 다시 작성하지 않는다.

## 계획 및 합격 기준

- [x] 기존 스킬·하네스·테스트를 범용 WinPC 제어 기준으로 이름과 트리거를 통합한다.
- [x] 세션 사용자·UTF-8·stdin JSON·일회성 InteractiveToken task·atomic result·cleanup을 구현한
      재사용 가능한 batch launcher/worker를 추가한다.
- [x] Cua driver의 pinned 설치·autostart·telemetry·해시/서명·rollback 상태를 빠르게 감사하는
      표준 setup 경로를 추가한다.
- [x] driver call의 종료 코드뿐 아니라 structured effect와 사후 snapshot assertion을 검사하며,
      임시 artifact와 task가 실패 시에도 남지 않게 한다.
- [x] 기존 Viewer exact-window 캡처 계약과 범용 제어 계약을 자동 테스트하고 skill validator,
      PowerShell parser, Viewer 테스트, 실제 WIN11-DELL 무해 batch를 통과시킨다.
- [x] 실제 warm-control 경과 시간과 최종 listener/firewall/Viewer/cleanup 상태를 기록한다.

## 검토

- 기존 `verify-windows-viewer-gui`를 별도 스킬로 남기지 않고 프로젝트 스킬
  `control-camstation-windows-pc`로 승격했다. 일반 `Status/Plan/Cleanup`과 Viewer의
  `ViewerCapture` exact-window 모드가 하나의 권한·세션·증거 규칙을 공유한다.
- `Invoke-CamStationWindowsControl.ps1` 한 진입점과 interactive worker를 추가했다. 계획의 모든
  변경 단계는 뒤쪽 관찰 단계와 screenshot 또는 assertion을 요구하며, `$ref`/`$select`, UTF-8
  stdin JSON, 단일 InteractiveToken task, 100 ms atomic-result polling, 정확한 task/run 정리를
  실행기가 담당한다. UIA 전체 값은 완료 파일에 저장하지 않고 안전한 요약과 raw-output SHA만
  남긴다.
- 실제 정상 batch는 session 1에서 계산기를 실행하고 `0` 화면을 캡처한 뒤 `1`을 UIA로 클릭해
  `1` 화면을 다시 캡처하고 닫았다. 최신 최종 실행은 13,112 ms였고 `TaskDeleted=true`, 해당
  window ID `countEquals=0` assertion을 통과했다. PNG SHA-256은 각각
  `af25e44e3f88b3ee6da04d28ef9567ac97fd0c7d929069717489559b6cf5e536`와
  `6cdab648753c08fd10230f921188a98a9665bbce359774cd078830164935abe2`이며 실제 화면에서 `0 -> 1`을
  확인했다.
- 정상 설치 PC의 기본 `Status`는 전체 관리 cmdlet 열거를 줄여 13,917 ms에서 3,434 ms로
  단축했다. 최신 정상 batch와 합친 warm 경로는 약 16.5초다. `-FullAudit`는 5,854 ms로 별도
  유지해 ActiveStore 전체 감사가 필요할 때만 비용을 낸다.
- 기존 설치에 표준 setup을 실제 재실행해 `InstalledNow=false`, 6개 파일 해시 일치,
  telemetry 비활성, 정확한 session 1 daemon, vendor autostart, 임시 setup task 0개를 24,524 ms에
  확인했다. 공식 archive/file 해시는 맞지만 실행 파일 Authenticode는 `NotSigned`다.
- 의도적으로 존재하지 않는 컨트롤을 선택한 실패 batch도 실행했다. 결과는 명확히 실패했고,
  `closeWindowOnFailure`가 fresh UIA titlebar로 그 launch의 창을 닫아
  `RemainingWindowIds=[]`, `Passed=true`를 반환했다. 이어진 조회에서 Calculator 프로세스/창은
  0개였다. 실패 run/task도 자동 삭제됐다.
- 프로젝트 스크립트 세 개를 테스트 PC의 canonical repo 경로에 동기화했고 로컬/원격 SHA-256과
  Windows PowerShell 5.1 parser 오류 0개를 확인했다. 전체 Viewer 테스트 42개, 새/기존 WinPC
  계약 테스트, skill validator, `git diff --check`가 모두 통과했다.
- 최종 감사는 TCP listener 27개, 활성 firewall rule 258개로 기존 기준선과 같고 driver TCP와
  Cua firewall rule은 각각 0개다. control/setup task, worker, run directory, Calculator process는
  모두 0개이며 Viewer service와 Explorer session 1은 유지됐다. 원격 임시 계획/증거는 삭제했고
  검증한 로컬 증거 사본은 복구 가능한 휴지통으로 이동했다.

---

# 2026-08-12 WIN11-DELL 범용 데스크톱 제어 구성

## 승인 범위와 설계

- 대상은 사용자가 전 권한을 승인한 테스트 VM `WIN11-DELL` 한 대다. 모니터링 PC와 운영
  서버는 제외한다.
- 기존 host-key 고정 SSH를 제어 채널로 유지하고, 로그인된 session 1에는 검증된 Windows
  computer-use driver만 둔다. 새 인바운드 포트나 외부 공개 원격 데스크톱은 추가하지 않는다.
- 기본 조작은 UIA/백그라운드 방식으로 하고, Electron처럼 필요한 동작에만 foreground 입력을
  사용한다. 화면 확인 후 조작하고 다시 확인하는 폐쇄 루프를 합격 기준으로 삼는다.

## 계획 및 합격 기준

- [x] 공식 소스·릴리스·권한 모델·설치/제거 절차를 확인하고 현재 PC 기준선을 수집한다.
- [x] 버전과 해시를 고정한 driver를 테스트 PC에 설치하고 interactive session 1에서 실행한다.
- [x] SSH에서 driver CLI를 통해 창 목록과 정확한 화면 캡처를 조회한다.
- [x] 무해한 실제 조작으로 창 전환, 클릭 또는 단축키, 입력, 스크롤 중 대표 경로를 검증하고
      각 동작 뒤 화면/상태를 재확인한다.
- [x] 외부 listener·firewall delta, 잔여 설치 task, Viewer/운영 상태를 감사하고 rollback을 기록한다.

## 검토

- 공식 `cua-driver-rs` 0.19.3 Windows x86_64 릴리스 ZIP을 SHA-256
  `e48b0117e343cec2577fc12693c741e094f389f8d4aef91e06284960bb03bce1`로 고정해
  `C:\Program Files\Cua Driver\0.19.3`에 설치했다. 설치된 6개 파일은 공식 ZIP에서 확인한
  개별 해시와 모두 일치한다. 단, 실행 파일·DLL·Node 모듈의 Authenticode 상태는 모두
  `NotSigned`이므로 서명된 배포물로 간주하지 않는다.
- 텔레메트리는 비활성화했고 기본 `standard` 권한 모드를 유지했다. 공급자 로그온 작업
  `cua-driver-serve`가 `dyllislev`의 `Interactive/Highest`로 등록되어 있으며 driver PID 9400은
  정확히 session 1에서 실행 중이다. SSH 유지보수 계정은 사용자별 named pipe에 직접 접근하지
  않고, 매 호출마다 `TASK_LOGON_INTERACTIVE_TOKEN` 일회성 작업으로 실행한 뒤 삭제한다.
- 실제 전체 화면과 창 목록을 조회했다. 첫 캡처는 1920x1200이었고 Viewer PID 13368의 최대화된
  `CamStation 2.0`에서 세 카메라가 모두 재생 중이었다. 작업 중 RDP 표시 크기는 1280x768로
  동적으로 바뀌었지만 해상도 변경 명령이나 세션 재접속 이벤트는 없었고, 마지막 캡처에서도
  Viewer 최대화와 세 영상 재생이 유지됐다.
- 무해한 계산기 창을 열어 UIA background click으로 `1`, 검증된 background 실패 후 foreground
  key 입력으로 `2`를 보내 실제 화면의 `12`를 확인한 뒤 정상 종료했다. 반면 desktop-scope
  `Win+R`/문자 입력은 CLI가 `effect=unverifiable`로 종료 코드 0을 냈지만 사후 캡처에서 실제
  변화가 없어 성공으로 인정하지 않았다. 시스템 전역 hotkey는 향후에도 화면 재검증과 별도
  fallback이 필요하다.
- 앱 단위 문자 입력은 새 임시 파일만 지정해 연 메모장에서 별도로 증명했다. UIA `type_text`가
  안전한 검증 문구를 현재 커서에 추가한 화면과 readback을 확인했고, 짧은 action-scoped 저장
  단축키가 적용되지 않은 뒤에는 정확한 메모장 HWND를 전경에 유지해 `Ctrl+S`를 보냈다. 파일의
  73자 전체 내용이 일치한 뒤 정상 `WM_CLOSE`로 닫고 Viewer PID 13368을 다시 전경으로 복구했다.
- 메모장은 Windows가 기존 미저장 복구 탭을 열어 입력 대상으로 사용하지 않았다. 정상
  `WM_CLOSE`만 session 1에서 보내 저장을 강제하거나 내용을 변경하지 않고 창과 프로세스가
  종료된 것을 확인했다. 예상 밖으로 UIA provider가 한 차례 문서 값을 반환한 산출물은 즉시
  삭제했고 이후 문서 UIA 수집을 중단했다.
- 최종 감사에서 TCP listener 27개와 활성 firewall rule 258개가 기준선과 같고, driver TCP 연결과
  Cua firewall rule은 각각 0개다. 일회성 작업·계산기·메모장·설치 ZIP·원격/로컬 증거 파일은
  모두 0개이며 Viewer service는 `Running`이다. 기존 Viewer GUI 스크립트 두 개도 로컬/원격
  SHA-256이 정확히 일치한다.
- 롤백은 대상 사용자의 `cua-driver autostart disable`, 실행 중 daemon 종료, 정확한 0.19.3 설치
  디렉터리와 해당 사용자 `.cua-driver` 설정 제거 순서다. 외부 port·firewall·계정·저장 자격증명은
  추가하지 않았으므로 별도 네트워크 롤백은 없다.

---

# 2026-08-12 WinPC Viewer 최대화 및 화면 증거

## 범위와 수락 기준

- [x] 승인된 대화형 세션의 기존 Viewer 창에 Windows 최대화 명령 한 번만 적용한다.
- [x] Windows 최대화 상태와 정확한 Viewer 창 캡처를 확인한다.
- [x] 일회성 작업·worker·전송용 임시 파일을 모두 정리하고 Viewer 서비스를 보존한다.

## 검토

- session 1의 기존 Viewer PID 13368에만 명령을 적용했으며 새 Viewer를 실행하지 않았다.
- Windows가 `IsMaximized=true`, 창 크기 `1936x1168`을 반환했고 실제 캡처에서도 최대화된
  `CamStation 2.0` 창과 정상 재생 중인 세 카메라를 확인했다.
- 일회성 task/worker와 전송용 임시 파일은 모두 0개이고 Viewer 서비스는 `Running`, Explorer는
  기존 session 1을 유지한다. Viewer 제품 코드·설정·서버에는 변경이 없다.

---

# CamStation 2.0 Paseo development environment

## Scope and decisions

- Target the checked-out `camstation2-initial` branch and its Go single-daemon +
  React/Vite + Electron Viewer architecture.
- Keep the legacy `main` branch and production CCTV services untouched.
- Install the Go toolchain inside the ignored repository-local `.tools/` directory;
  pin Go 1.25.12 and verify the official Linux amd64 SHA-256 before extraction.
- Paseo's default daemon service uses its assigned port, a worktree-local `data/`
  directory, and recording disabled. Real cameras, go2rtc, and rclone remain explicit
  integration-test dependencies rather than worktree bootstrap requirements.
- Paseo's web service uses its assigned port and discovers the sibling daemon through
  `PASEO_SERVICE_DAEMON_PORT`.

## Acceptance criteria

- [x] Add an idempotent bootstrap that installs the pinned Go toolchain, downloads Go
      modules, installs web/Viewer npm lockfiles, and prepares ignored runtime folders.
- [x] Add reusable Go, daemon, web, test, and full-check launchers with safe defaults.
- [x] Make existing Make targets use the repository-local Go wrapper.
- [x] Make the Vite proxy target configurable, use the Paseo daemon port, proxy `/api`
      and `/player`, and leave the React `/live` route to Vite.
- [x] Add a Paseo 0.3.0-compatible `paseo.json` with setup, daemon/web services, tests,
      and complete verification commands.
- [x] Confirm the registered CamStation project actually loads the lifecycle hooks and
      scripts in Paseo 0.3.0; placeholder-only UI fields are not acceptance.
- [x] Update the root README with the actual 2.0 prerequisites, local workflow, Paseo
      workflow, optional integration tools, and secret/runtime safety constraints.
- [x] Run the bootstrap in this checkout and confirm all required tool versions.
- [x] Prove a daemon health request through the Paseo-style Vite proxy and prove `/live`
      is served by Vite rather than proxied to the daemon.
- [x] Run Go tests/build, web tests/lint/build, and Viewer tests/build.
- [x] Validate shell/JSON/Paseo schemas, inspect the final diff, and document review results.

## Review

- Branch: `camstation2-initial`, tracking `origin/camstation2-initial`; legacy `main`
  and production services were not modified.
- Bootstrap: ran `./scripts/setup-dev.sh` twice successfully. The second run reused
  the verified local Go installation and completed cleanly, proving idempotency.
- Toolchain: Go 1.25.12 (`linux/amd64`), Node 22.22.1, npm 11.12.1. The Go archive
  checksum matched the checksum published by go.dev.
- Paseo: `paseo.json` passed JSON parsing and the installed Paseo 0.3.0 raw config
  schema. `daemon` and `web` commands were exercised with Paseo-style environment
  variables on ports 20580 and 20581.
- Service smoke test:
  - direct `GET /api/health` returned `ok: true`;
  - Vite-proxied `GET /api/health` returned the same daemon response;
  - Vite `/live` returned `/@vite/client` and `/src/main.tsx` markers;
  - `/player` reached the daemon and returned the expected 502 because optional
    go2rtc is not installed, rather than a 403 origin rejection;
  - both smoke-test ports were released after graceful terminal shutdown.
- Verification: final `./scripts/check-dev.sh` passed all Go packages, 52 web tests,
  web lint/build, 23 Viewer tests, Viewer build, and the final daemon build.
- Flake audit: one cold first-run Viewer Agent integration test exceeded its tight
  timing window. It then passed 20 focused repetitions, three full-package repetitions,
  and two subsequent complete checks; no unsupported product/test change was made.
- Scoped SCA: `npm audit` findings were remediated with semver-compatible lockfile
  updates only. Web now uses nanoid 3.3.18, postcss 8.5.26, and React Router 7.18.2;
  Viewer tooling uses brace-expansion 5.0.9 and undici 7.29.0. Both audits report zero
  vulnerabilities, and the web/Viewer tests and builds passed afterward.
- Integrity: shell syntax, JSON, Paseo schema, `git diff --check`, embedded asset
  reference, and changed-file credential scans passed.
- Known optional gaps: go2rtc, rclone, and the SQLite CLI are not installed on this
  host. Real-camera, live-stream, recording, and backup integration were intentionally
  not exercised by the safe default worktree setup. Vite continues to report the
  existing large-chunk advisory for the roughly 659 kB main bundle.
- Host note: Paseo 0.3.0's daemon is running, but direct CLI RPCs require the host's
  `PASEO_PASSWORD`. Project configuration and service behavior were validated without
  weakening that daemon authentication.

## UI integration follow-up

- The 2026-08-09 project-settings screenshot shows empty lifecycle fields and no scripts;
  the visible `npm install` and `docker compose down` strings are placeholders.
- `paseo.json` currently parses and passes the local schema, but is untracked. A new
  worktree created from the selected base therefore cannot inherit it.
- Completion now requires evidence from the registered project/daemon path: lifecycle
  values loaded, five scripts listed, and at least the safe setup or service path started
  through Paseo rather than only by invoking wrappers directly.
- The daemon's own project-config reader returned `./scripts/setup-dev.sh` and all five
  configured scripts. The registered Workspace projection also returned exactly `check`,
  `daemon`, `setup`, `test`, and `web`.
- Paseo ran `setup` successfully with exit code 0. It then assigned ports 20869 and 20765
  to `daemon` and `web`; both reached `healthy`, the web proxy returned the daemon health
  payload, and `/live` returned the CamStation 2.0 Vite page.
- Paseo stopped both services with exit code 0, and neither assigned port remained open.
  The Desktop settings page must be re-entered or reloaded to replace its stale empty draft.
- The config remains intentionally uncommitted. A newly created worktree cannot inherit it
  until the user chooses to commit it on the selected base branch.
- Final audit parsed the config and found all five scripts/two services; JSON, shell syntax,
  and `git diff --check` passed. Its port-forwarding warnings are static wrapper heuristics:
  actual Paseo-assigned ports reached both processes. The state marker is worktree-relative
  `data/`; parallel managed-worktree isolation remains untested until the config is committed.

---

# 2026-08-09 CCTV operations and monitor-PC maintenance audit

## Scope and decisions

- Treat the user's request as authorization for the directly connected
  `192.168.0.0/24` CCTV environment, limited to locating the CCTV server and inspecting
  the named monitor PC at `192.168.0.13`.
- Use passive inspection and low-rate, read-only network/service checks. Do not guess
  passwords, exploit vulnerabilities, change configuration, restart services, install
  software, reboot hosts, or contact camera endpoints directly.
- Reuse only already-provisioned credentials or host keys if they are available to this
  maintenance environment; never record secret values in evidence or documentation.
- Preserve all pre-existing worktree changes and keep this audit limited to case records
  and maintenance documentation.

## Acceptance criteria

- [x] Create a granted scope, rules, timeline, evidence, findings, and execution plan.
- [x] Identify the CCTV server using at least two independent signals.
- [x] Verify server identity, service/process health, camera status, recording activity,
      storage pressure, backup/cleanup state, and recent operational errors where access permits.
- [x] Verify `192.168.0.13` reachability, operating-system/service fingerprints, installed
      monitoring-client evidence, supported remote-management methods, and effective access level.
- [x] Distinguish verified facts from inferences and explicitly record credential or
      reachability blockers without exposing secrets.
- [x] Publish a Korean maintenance runbook/report with a topology, access prerequisites,
      safe inspection commands, troubleshooting flow, and escalation checklist.
- [x] Validate links, redaction, command syntax, evidence references, and final diff.

## Review

- Current production was validated as dual-homed host `cctv`: management address
  `10.0.0.26`, CCTV/monitor-LAN address `192.168.0.160`, identical SSH host key, health
  API `ok`, and working existing-key root access. The documented `cctv2` candidate is offline.
- All five core services were active, the database quick-check passed, 8/9 cameras were
  enabled/online, and the one disabled camera was confirmed intentional. A final per-camera
  ten-second sample proved all eight open recorder files grew.
- The backup chain was verified across log, database marks, local deletion, and remote state:
  392 successful cycles/zero failures in 24 hours and 32 matching remote objects in two hours.
- `192.168.0.13` was proven active through current Camviewer 1.0.4 traffic. It is the same
  Windows `NUC` exposed as Tailscale peer `nuc-moniter`; the direct Tailscale path and matching
  AnyDesk certificate establish the identity mapping.
- The Viewer UI is live but the control-agent heartbeat is stale since 2026-07-01 KST.
  Ten pending commands include five obsolete restarts, so the report requires expiring the
  queue before repairing the agent.
- Monitor-PC management surfaces are AnyDesk plus Tailscale Windows OpenSSH/SMB/RPC.
  RDP and WinRM are closed. The initial `dyllislev` key attempt was correctly denied because
  `sshd_config` permits only `CamStationOps`; the operator later registered the dedicated key
  for that allowed administrator account and strict pinned-host SSH now works.
- The Korean report is `docs/2026-08-09_operations-cctv-maintenance-report.md`; the evidence
  chain is under `work/20260809-cctv-operations/`. Report SHA-256 is
  `a45f99cc974229aa0817c04fa8860c2198bb9e2ed6edcee561c86e7eddc59178`.
- Final validation passed: links, code fences, Evidence references, scope guard fields,
  sensitive-pattern scan, runtime-path scan, and `git diff --check`. Existing unrelated
  dirty-worktree changes were preserved.

## External follow-up required

- [x] Register and verify the dedicated maintenance key for the allowed `CamStationOps`
      administrator without broadening `AllowUsers` or enabling password authentication.
- [ ] Restore the intended cctv2 server, prove whether `10.0.0.29` and `192.168.0.172` are the
      same host, then execute the documented 1.0-to-2.0 cutover with GUI and rollback evidence.
- [ ] Record AnyDesk and break-glass ownership only in the organization password manager.

---

# 2026-08-09 NUC remote-control bootstrap

## Scope and decisions

- Run commands only in an elevated Windows PowerShell session physically or interactively
  approved on monitor PC `192.168.0.13` (`NUC`).
- Honor the active `AllowUsers CamStationOps` restriction and provision only the existing
  maintenance **public** key for that dedicated account; do not broaden SSH access to
  `dyllislev`, copy the private key, or place a password/AnyDesk code in project artifacts.
- Detect whether the target account is an Administrators member and honor Windows OpenSSH's
  effective `AuthorizedKeysFile`; preserve existing keys and avoid changing unrelated services.
- Do not revive the stale Viewer agent or begin the 2.0 upgrade until SSH access is proven and
  the obsolete server-side Viewer command queue is safely expired.

## Acceptance criteria

- [x] Confirm `dyllislev` identity, SID, profile, Administrators membership, `sshd` service,
      and the applicable OpenSSH rules; active `AllowUsers` selects `CamStationOps` instead.
- [x] Add the maintenance key idempotently to the correct file with restrictive Windows ACLs
      while preserving any existing authorized keys.
- [x] Prove a non-interactive SSH login over Tailscale from the maintenance environment and
      record the Windows identity/hostname returned by that authenticated session.
- [x] Document rollback, verification evidence, and the next gated steps for stale-command
      cleanup and the CamStation 2.0 client upgrade.

## Review

- User-provided output proves host `NUC`, active automatic `sshd`, and an unelevated
  `NUC\dyllislev` session. `dyllislev` is an Administrators member, but active configuration
  permits only `CamStationOps`; its administrator match uses
  `%ProgramData%\ssh\administrators_authorized_keys`.
- The user ran the guarded registration successfully. The dedicated key fingerprint and ACL
  were verified, and strict host-key-pinned SSH returned `nuc\camstationops` with an
  administrator token on Windows 11 Pro.
- Windows inventory shows Viewer 2.0.20 MSI registrations and its automatic service, but this
  does not supersede the operator-confirmed 1.0 monitoring baseline. Interactive process and
  local IPC evidence must distinguish staged installation from completed cutover.
- Final pinned-SSH recheck returned six active CamViewer 1.0 processes, zero interactive
  CamStationViewer 2.0 processes, and running `CamStationViewerService`/`sshd`. The staged 2.0
  endpoint and documented cctv2 address are both offline, so no cutover action was taken.

---

# 2026-08-09 cctv same-host 1.x-to-2.0 replacement strategy

## Scope and decisions

- The production destination is the existing dual-homed `cctv` host, not the separate
  historical cctv2 development host.
- Merge `camstation2-initial` into `main` as a source-control release decision, then deploy the
  resulting 2.0 release to a separate same-host runtime slot before switching production.
- Preserve the legacy runtime, database, camera configuration, recordings, backup evidence,
  nginx configuration, and Viewer 1.0 launch assets until the 2.0 acceptance window passes.
- Do not perform the merge, data import, service stop/start, nginx switch, Viewer reconfigure,
  or deletion during strategy work; all production changes require a later approved window.

## Plan

- [x] Confirm `main`/`camstation2-initial` ancestry, divergent commits, changed surfaces, and
      dry-run merge-conflict risk.
- [x] Compare 1.x and 2.0 camera/settings/recording/backup/Viewer schemas and identify the
      required idempotent import contract.
- [x] Design isolated same-host staging paths, ports, DB, services, secrets, and resource limits.
- [x] Define preflight, freeze/import, service and nginx switch, Viewer transition, soak,
      rollback triggers, and post-cutover retention with verifiable gates.
- [x] Publish and validate a Korean strategy document with topology, data mapping, timeline,
      command context, Evidence → Finding → Path, and explicit operator decisions.

## Acceptance criteria

- [x] Quantify branch ancestry, divergent commits, changed surfaces, and merge-conflict risk.
- [x] Map legacy camera/settings/recording/backup/Viewer data to the 2.0 source-of-truth model,
      identifying fields that require a purpose-built idempotent importer.
- [x] Define a same-host deployment that cannot collide with the active 1.x runtime.
- [x] Define go/no-go and rollback evidence for server, cameras, recordings, backup, and Viewer.
- [x] Validate document links, commands, evidence references, sensitive-data handling, and diff.

## Review

- `main` and `camstation2-initial` have no merge base. Their tips contain 165 and 195 unique
  commits, their trees contain 142 and 500 paths with only four common paths, and the direct
  tree comparison spans 631 changed files. A normal content merge is therefore rejected.
- A temporary-clone simulation proved the documented two-parent replacement merge produces
  a result tree exactly equal to the current 2.0 tree while retaining both branch parents.
- The legacy and 2.0 schemas require a purpose-built importer. The strategy preserves the
  stable camera key, `9/8/1` activation invariant, role streams, layouts, secrets, settings,
  and explicitly separates recording archive and Viewer registry decisions.
- Because both generations own the same fixed go2rtc ports, the design stages code/data in
  separate slots but uses a single-active runtime handoff. It retains the 1.x slot and Viewer
  1.0 as rollback assets and writes new 2.0 recordings to a separate root.
- The formal strategy is published at
  `docs/2026-08-09_cctv-1x-to-2x-production-cutover-strategy.md` and linked from `README.md`.
  All 14 relative links resolve; heading depth, placeholder scan, raw credential URL scan, and
  whitespace checks pass.
- `scripts/test-dev.sh` passed the Go suite, all 52 Web tests, and all 23 Viewer tests. This is
  recorded only as a source baseline; production build, service, camera, backup, and Windows
  GUI evidence remain execution gates.
- No production service, database, camera, nginx, Viewer configuration, Git branch, tag, or
  remote was changed during this strategy task.

---

# 2026-08-09 server-first cutover with transitional Viewer 1.0 shell

## Scope and decision to evaluate

- Evaluate the operator-proposed sequence: stage/install 2.0 on `cctv`, import camera data,
  stop the 1.x server and activate 2.0, stabilize the server while leaving the WebView-based
  CamViewer 1.0 client installed, then activate Viewer 2.0 and retire Viewer 1.0 separately.
- Distinguish leaving the 1.x Windows shell installed from leaving the 1.x server runtime
  active. Only the former is under consideration during the 2.0 server phase.
- Do not change the production server, nginx, NUC configuration, services, or Git branches
  while validating and documenting this refinement.

## Plan

- [x] Inspect the 1.x Viewer startup URL, navigation rules, heartbeat/control behavior, and
      the 2.0 SPA routes to determine whether the old shell can display the new `/live` page.
- [x] Define the smallest compatibility bridge and its limits; do not assume API compatibility
      merely because both clients embed a browser.
- [x] Revise the production strategy to make server transition and client transition explicit
      release phases with independent go/no-go and rollback points.
- [x] Validate the revised document, links, sensitive-data handling, and source-backed claims.

## Acceptance criteria

- [x] State whether CamViewer 1.0 works unchanged, works only through a bounded redirect, or
      cannot be used against the 2.0 server, with exact source evidence.
- [x] Keep the 1.x server stopped while 2.0 is active and preserve it only as rollback state.
- [x] Preserve Viewer 1.0 installation/startup assets until Viewer 2.0 passes interactive
      console, telemetry, auto-start, and reconnect acceptance.
- [x] Publish an unambiguous revised sequence and rollback boundary in the strategy document.

## Review

- CamViewer 1.0.4 hard-codes `/new?viewer=1`; the 2.0 SPA's intended Viewer route is
  `/live?viewer=1`, while an unhandled `/new` falls through the SPA wildcard to `/`. The old
  shell therefore is not unchanged-compatible but can follow a bounded same-origin redirect
  because its navigation guard does not restrict `/live`.
- The revised strategy requires a tested server route for `/new?viewer=1` → `/live?viewer=1`
  before production. In that mode the old Electron window is display-only: its preload lacks
  the 2.0 `camstationViewer` bridge, and the old heartbeat/command code came from the retired
  1.x web UI, so 2.0 telemetry, control, and managed updates are not acceptance signals.
- Server and client now have independent gates. Server completion requires 60 minutes, one
  30-minute rollover, one end-to-end backup, camera `9/8/1`, eight live streams, and manual
  eight-camera display through the transitional shell before Viewer 2.0 is launched.
- Server-gate failure restores the preserved 1.x runtime; Viewer-2.0-only failure leaves the
  healthy 2.0 server active and relaunches CamViewer 1.0 through the bridge.
- The strategy document passed local-link, code-fence, heading-depth, placeholder,
  sensitive-pattern, README-reference, whitespace, and source-claim checks. No production
  service, database, camera, nginx, Viewer setting, process, or Git branch was changed.

---

# 2026-08-09 accepted display-only server-first cutover preparation

## Scope and decisions

- Treat the operator's server-first sequence as approved: during server acceptance,
  CamViewer 1.0 only needs to render the 2.0 live video workspace; Viewer management,
  telemetry, remote control, and managed updates are deferred to the later Viewer 2.0 phase.
- Prepare the 2.0 source tree, tests, migration/deployment readiness record, and executable
  preflight artifacts. Do not merge branches or change the production `cctv` runtime, nginx,
  database, cameras, NUC settings, or processes in this preparation task.
- Preserve all unrelated dirty-worktree changes and limit implementation to files confirmed
  clean or to new preparation artifacts.

## Plan

- [x] Re-review the accepted sequence against current 1.x/2.0 Viewer routes, daemon routing,
      camera schemas, lifecycle scripts, implementation status, and existing strategy.
- [x] Implement and test the bounded legacy entry route so only `GET /new?viewer=1` reaches
      `/live?viewer=1`, without exposing or reviving the legacy `/new` application.
- [x] Audit camera-import, production service, configuration, backup, archive, and rollback
      preparation; classify each as ready, implementable now, or requiring an operator/runtime gate.
- [x] Implement the safest self-contained preparation slice(s) needed before a production
      rehearsal, with dry-run behavior and secret-safe evidence.
- [x] Update the cutover document and readiness record to reflect the accepted display-only
      criterion, exact commands, remaining gates, and next authorized production action.
- [x] Run focused tests followed by the repository's full validation path; inspect generated
      assets, diffs, links, and sensitive-pattern output before declaring preparation complete.

## Acceptance criteria

- [x] The compatibility route has automated positive and negative tests and preserves the
      existing SPA/API behavior outside the exact legacy Viewer request.
- [x] Preparation can be executed without modifying the active 1.x database or leaking camera,
      backup, Viewer, or SSH credentials into Git, logs, manifests, or test output.
- [x] The readiness review gives every pre-cutover dependency an owner, proof command, current
      state, and explicit Go/No-Go result.
- [x] Server and client cutovers remain independent: missing legacy-shell telemetry is accepted,
      but eight-camera visual playback remains a production gate.
- [x] No production or NUC state changes occur during preparation, and unrelated user changes
      remain intact.

## Review

- The accepted server-first/display-only transition is implemented as an exact compatibility
  route. Viewer 1.0 management telemetry remains deferred; visible eight-camera playback is
  still a production gate.
- Bounded Hangul camera keys now survive API and store persistence. A new maintenance binary
  performs SQLite online snapshot, schema/quick-check, redacted dry-run, fresh atomic import,
  repeat-safe verification, and strict `9/8/1` plus layout/settings expectation checks.
- Fresh 2.0 DBs no longer inherit the development backup remote. Backup starts disabled with
  an empty target and `protectUnbacked=true`; enabling it requires an explicit target.
- Hardened systemd/nginx packaging and root-guarded release, state, preflight, switch, and
  rollback helpers were added. They use exact services and refuse unresolved paths, hashes,
  symlinks, port collisions, active-generation overlap, and unknown nginx include state.
- Read-only production review confirmed all five legacy services active, go2rtc media ports
  occupied, port 18080 free, required runtime binaries present, and no SQLite CLI. It also found
  two existing nginx server location sets that must be converted to preserved legacy includes
  during an approved staging step.
- Verification passed: Go full suite and vet, Web 52 tests/lint/build, Viewer 23 tests/build,
  daemon and migrator builds, production shell policy, and diff whitespace checks.
- Final documentation audit passed 49 local-link checks, 61 Markdown code-fence checks,
  cutover-scoped credential-pattern and trailing-whitespace scans. The readiness report
  SHA-256 is `e892417451e0250ed8e78a40bf9140a1a05e94b69b4e88ecdb0d358c13b89d3a`.
- No production service, DB, nginx file, camera, NUC process/configuration, Git branch, tag, or
  remote was changed. Operational execution remains No-Go until the readiness report's R1 and
  R4-R10 gates pass.

---

# 2026-08-09 inactive production staging and camera-state preparation

## Scope and decisions

- Treat the operator's latest instruction as approval to mutate the production `cctv` host only
  for reversible, inactive 2.0 staging: immutable release files, a disabled systemd unit,
  root-only configuration, an online legacy snapshot, and an imported 2.0 database.
- Keep nginx's active configuration and all 1.x units unchanged and active. Do not start 2.0,
  stop 1.x, alter cameras, enable backup, touch the NUC, or execute the final handoff.
- Build from an isolated clean source candidate; do not package the mixed dirty worktree or
  overwrite unrelated user changes.

## Plan

- [x] Re-resolve the exact legacy DB, service, nginx, filesystem, account, and port state using
      read-only production checks; stop if it differs from the reviewed topology.
- [x] Create a clean, reproducible 2.0 release candidate containing the approved cutover
      preparation changes and record its commit/tree and complete file hashes.
- [x] Run the full source validation path on that candidate before uploading any artifact.
- [x] Install the immutable release, service account, unit, environment, runtime directories,
      and inactive nginx candidate files without switching the active include.
- [x] Create an online legacy snapshot and import the 9-camera/8-enabled camera graph, layout,
      and 30/30/700 recording settings into an inactive 2.0 DB.
- [x] Prove source/snapshot/target parity, file ownership and modes, 2.0 inactive state, all
      legacy units active, legacy media ports owned, port 18080 free, and current service health.
- [x] Update the readiness report and this review with exact evidence, remaining one-window
      handoff work, rollback state, and any blocked gate.

## Acceptance criteria

- [x] The running 1.x server and Viewer-facing endpoint remain continuously available throughout
      staging, with no legacy service restart; the only nginx reload preserves the exact legacy
      routes and passes before/after health checks.
- [x] The staged release is immutable, hash-verified, and not built from an unreviewed dirty tree.
- [x] The imported target DB passes canonical verification against an immutable online snapshot
      and contains no enabled backup destination by default.
- [x] The final handoff is reduced to maintenance include, exact legacy stop, port release, 2.0
      start/health checks, active include switch, and field video verification.

## Review

- Installed immutable release `2.0.0-rc.20260809.5` from clean two-parent replacement commit
  `db09c6c9d142e9c6d1a360b0b4a59ac098fe8283`; the remote `main` branch was not changed.
- Full Go tests/vet, Web 52 tests/lint/build, Viewer 23 tests/build, release hash verification,
  production shell policy, and exact real-DB read-only inspection passed before installation.
- Production inspection found media on the large dedicated filesystem rather than the root
  filesystem. Packaging now keeps state under its protected root and recording/temp together on
  the media filesystem; preflight enforces ownership, free space, and same-filesystem finalization.
- Three legacy sub-stream values were local go2rtc ffmpeg recipes, not camera URLs. The importer
  now maps only that exact loopback/self-key H.264 form to a recording-backed live output and
  rejects other producer expressions. Synthetic regression and actual production inspection pass.
- Online snapshot and target verification passed at canonical fingerprint
  `636af019dce2debb7c30e54b49966be9a1afe2679d3f0a30c0d0fa305bc80874`: cameras `9/8/1`,
  sub entries 9, layout 1/8, settings 30/30/700, blockers 0, backup disabled and protected.
- Nginx preparation verified the original site hash, preserved both original files, reduced two
  wildcard-loaded server blocks to one, and reloaded with the active symlink still targeting the
  legacy routes. Legacy backend/backup/go2rtc PIDs and restart counts remained unchanged.
- Final server preflight returned `PREFLIGHT_READY`; 2.0 remains inactive/disabled, port 18080 is
  free, all 1.x units are active, health reports eight online cameras, NUC traffic continues, and
  the root-only switch approval remains `NO`.
- Boot ownership is guarded: preflight requires legacy active/enabled and 2.0 inactive/disabled;
  switch and both rollback paths transfer systemd enablement with runtime ownership.
- The complete source history is preserved as a verified root-only Git bundle with SHA-256
  `bac75de5224bd55c3128b5cd2326d757274b601d1af8c63a58aef6e146c323db`.

---

# 2026-08-09 isolated home-camera 2.0 canary

> Superseded before implementation: the user stopped the host-port-plumbing approach and asked
> to evaluate containerizing the complete 2.0 runtime. No canary runtime was started and no
> production service was changed by this plan.

## Scope and decisions

- Keep the complete 1.x production generation active and unchanged while running a separate 2.0
  canary generation with only stable keys prefixed `집-` enabled.
- Never point the canary at the fire-station cameras or modify the verified final-cutover DB.
- Give the canary distinct HTTP, go2rtc API, RTSP, WebRTC, state, media, service, and ingress
  boundaries; stop immediately on any port collision, camera-session impact, or legacy health loss.
- Treat canary success as evidence for the home-camera subset only. It does not waive the final
  fire-station, full recording, backup, Viewer, or rollback gates.

## Plan

- [ ] Inventory every hard-coded daemon/go2rtc/recorder port dependency and design explicit
      production defaults plus isolated canary overrides.
- [ ] Implement configuration plumbing and tests proving two independent port sets without
      changing existing defaults or exposing go2rtc API/RTSP listeners publicly.
- [ ] Create a fresh canary DB from the verified target, disable every non-`집-` camera, and prove
      exactly three enabled home cameras with the final-cutover DB unchanged.
- [ ] Package and install an inactive canary unit, environment, media/state roots, and bounded
      LAN test ingress while preserving the active legacy nginx route.
- [ ] Start the canary, prove 1.x continuity, three live home-camera outputs, recorder growth and
      segment playback, then capture logs/status without raw camera URLs or credentials.
- [ ] Stop or retain the canary only according to verified resource/session impact, and document
      the exact pre-cutover cleanup and remaining gates.

## Acceptance criteria

- [ ] All 1.x services remain active and the eight-camera production status stays healthy.
- [ ] Canary APIs and runtime expose exactly three enabled `집-` cameras and no non-home producer.
- [ ] The three canary videos render through the isolated ingress and their recorder files grow.
- [ ] Canary shutdown releases every alternate port and leaves the final-cutover DB/fingerprint
      and legacy boot ownership unchanged.

# 2026-08-09 containerized home-camera 2.0 canary evaluation

## Scope and decisions

- Stop the host-native parallel-runtime implementation before changing source or production.
- Evaluate one self-contained CamStation 2.0 application image containing `camstationd`, go2rtc,
  FFmpeg/ffprobe, and rclone. Container-internal ports may repeat safely under bridge networking;
  only host-published ports must be unique. Keep the existing production nginx and 1.x units
  outside the canary unless an internal nginx is justified independently.
- Preserve the same fail-closed camera boundary: only stable keys prefixed `집-` may be enabled in
  the trial DB, and the verified final-cutover DB must remain immutable. The active 1.x go2rtc YAML
  is the sole authority for camera keys, enabled state, and main/sub producer definitions; do not
  source canary camera data from the 1.x SQLite DB.
- Treat Docker port/volume isolation as runtime isolation only; it does not make duplicate camera
  sessions safe for the fire-station cameras.

## Plan

- [x] Verify Docker/Compose availability, architecture, storage, listener, and firewall constraints
      on the production server without installing or changing anything.
- [x] Audit the daemon and bundled toolchain for container requirements, process supervision,
      signal handling, HTTP/MSE proxying, health checks, persistent volumes, and permissions.
- [x] Compare host-native and containerized cutover/rollback risks and select the simplest safe
      production topology.
- [x] If approved after review, implement and test the image/Compose definition locally before any
      production deployment.
- [x] Build a separate `집-`-only trial DB directly from the active go2rtc YAML and run a bounded
      container canary while proving 1.x continuity and zero non-home producers. Do not import
      1.x DB camera rows, ONVIF fields, layouts, jobs, backup, or alert state into the trial.
- [x] Document image provenance, volume backup, upgrade, rollback, log, health, and cleanup commands.
- [x] After every gate passes, keep the canary running on HTTP `10.0.0.26:18081` for the operator's
      interactive check; stop it automatically if any safety or continuity gate fails.

## Acceptance criteria

- [x] The recommendation is based on the actual server and repository, not only a conceptual Docker
      design.
- [x] No production package, service, firewall, port, DB, or camera session changes during review.
- [x] The proposed topology supports multiple isolated 2.0 instances without shared writable state
      or host-port collisions.
- [x] Only `10.0.0.26:18081/tcp` is published for the retained canary.

## Review

- Image `camstation:2.0.0-rc.20260809.7-canary` is running healthy with restart count 0 and
  manual restart policy. Its final metadata exposes only `18080/tcp`; Compose publishes it only as
  `10.0.0.26:18081/tcp`.
- The YAML-only manifest selected exactly three home main/sub pairs. The generated public/private
  graph contains no fire-station or goat-farm key, and the original 1.0 YAML hash is unchanged.
- Three live H.264 streams, three recording workers, finalized 60-second MP4 playback, browser MSE,
  logs, resource limits, file ownership/modes, and legacy continuity all passed.
- Full procedures are in `docs/2026-08-09_camstation2-docker-canary-operations.md`.
  Its final SHA-256 is `dc409b1ebe8aa3fa244be2d787115ada4abfadcefaeb08f3d44ef41c6c43cb24`.

# 2026-08-09 2.0 dedicated `/viewer` parity

## Scope and decisions

- Treat the operator-designated 1.0 `https://cctv2.nuc.hmini.me/viewer` surface as the
  compatibility reference: a full-viewport, read-only camera layout with no management console,
  navigation, timeline, editing controls, or side panels.
- Add a distinct 2.0 `/viewer` route instead of reusing responsive `/live` or interpreting
  `?viewer=1` as equivalent behavior.
- Reuse 2.0's redacted camera/layout APIs and same-origin HTTP MSE proxy. The current canary must
  still render only `집-마당`, `집-창고1`, and `집-창고2`; fire-station and goat-farm cameras remain
  absent by construction.
- Keep the initial parity boundary narrow: full-screen grid, online indicator/name, layout-derived
  geometry, tile focus/return, and reliable mobile playback. Do not copy 1.0 management or PWA
  features that are not present on the `/viewer` reference surface.

## Plan

- [x] Inspect the real 1.0 `/viewer` at an iPhone-sized viewport and capture DOM, requests,
      playback state, and screenshot evidence.
- [x] Compare the same viewport against the running 2.0 `/live` canary and identify route, layout,
      console-chrome, overflow, and playback differences.
- [x] Implement an isolated `/viewer` route and tests without changing `/` or `/live` behavior.
- [x] Run web tests/lint/build and Go tests/build, then build a new immutable Docker image.
- [x] Replace only the 2.0 canary container and prove `/viewer` renders and plays all three home
      streams on mobile while `/live`, APIs, recorders, port isolation, and 1.0 continuity remain
      healthy.
- [x] Update the canary operations document and retain the validated container for operator access.

## Acceptance criteria

- [x] `GET /viewer` is a directly addressable SPA route and browser reload does not redirect it.
- [x] At a 393x852 mobile viewport, `/viewer` has no console navigation, settings controls,
      timeline, horizontal page overflow, or non-home camera.
- [x] All three `<video>` elements reach a playable state with advancing time through MSE.
- [x] A tile tap opens a single-camera focus view and the explicit close action restores the grid.
- [x] The retained canary remains healthy with only `10.0.0.26:18081/tcp` published, and legacy
      1.0 service PIDs/restart counts plus the source YAML hash remain unchanged.

## Review

- The production 1.0 reference rendered eight simultaneous MSE videos with only the saved camera
  layout. The former 2.0 `/live` mobile rendering retained management chrome, overflowed, and did
  not satisfy that contract.
- The new top-level `/viewer` renders only the read-only saved layout and uses MSE-first playback.
  At 393x852 all three home videos reached readyState 4 with advancing time; focus rendered one
  1280x720 stream and explicit close restored three playing tiles.
- Direct reload stayed on `/viewer`, document dimensions exactly matched the viewport, browser
  errors were empty, and the container/legacy final audit passed.

# 2026-08-09 eight-camera PID capacity correction

## Scope and decisions

- Correct the container task limit using the real final fleet size of eight cameras, not only the
  three-camera canary subset.
- Keep the current positive `집-` canary allowlist unchanged. This correction changes runtime
  capacity only; it does not contact fire-station or goat-farm cameras.
- Raise `pids_limit` from 256 to 1024. Three-camera live startup measured 226 current tasks, a peak
  of 256, and 343 cgroup PID-limit hits; 512 would be too close to the linear eight-camera estimate
  plus focus/reconnect headroom.

## Plan

- [x] Correlate connection retries with per-stream consumers, process/thread counts, and cgroup
      `pids.current`, `pids.peak`, and `pids.events` rather than relying on quiet application logs.
- [x] Update and validate the repository and root-owned production Compose definitions with an
      exact reversible 256-to-1024 change.
- [x] Recreate only `camstation2-canary` and prove health, unchanged image/state/port policy,
      `pids.max=1024`, and zero new cgroup PID-limit hits during reconnect.
- [x] Prove three-page/three-camera Viewer stability, recorder continuity, and unchanged 1.0
      service PID/restart and source-YAML baselines.
- [x] Update the operations report with the measured root cause, new capacity, and validation.

## Acceptance criteria

- [x] The new container reports `pids.max=1024`, `pids.events max 0`, no OOM, and no CPU throttling.
- [x] Three open Viewer pages settle at exactly three consumers per home live stream without a
      growing excess count.
- [x] The container stays healthy with three streaming cameras and three running recorders.
- [x] Only `10.0.0.26:18081/tcp` remains published; every 1.0 continuity baseline remains unchanged.

## Review

- Root cause was cgroup task exhaustion, not an application-level socket leak: before correction,
  the three-camera live workload reached `pids.peak=256` and recorded 343 PID-limit hits while
  memory events and CPU throttling remained zero.
- Repository and production Compose now use `pids_limit: 1024`. Only `camstation2-canary` was
  recreated; the immutable image ID, state/media mounts, HTTP-only binding, and restart policy
  remained unchanged. The previous production Compose was preserved in a root-only backup.
- Three mobile Viewer pages sustained nine MSE videos at readyState 4 with advancing playback for
  about 160 seconds. Each home live stream settled at exactly three viewers; after closing the
  browser, all viewer and consumer counts returned immediately to zero.
- During load, the container stayed healthy with three streaming cameras and three running
  recorders. It measured 224 PIDs with peak 230 against max 1024, zero PID-limit hits, zero OOMs,
  and zero CPU throttling; error/fatal/panic and task-exhaustion log signatures were also zero.
- The five 1.0 service PID/restart baselines, 8/8 enabled-online camera count, and source go2rtc
  YAML hash remained unchanged. Fire-station cameras and `염소장` remain absent from this canary.
- Local and production Compose validation passed with `pids_limit: 1024`. The closing audit passed
  five relative links, balanced code fences, placeholder, sensitive-pattern, raw-runtime-path,
  and `git diff --check` validation. The operations document SHA-256 is
  `27ae3b57c376a9eea4fcc803442280506f9062547a0eca6373f5fde2fee355fd`.

---

# 2026-08-09 NUC Viewer 2.0 installation verification

## Scope and decisions

- Inspect the already installed CamStation Viewer 2.0 on monitor PC `192.168.0.13` through the
  validated `CamStationOps` administrator SSH path.
- Keep CamViewer 1.0 and the interactive monitoring session untouched. This is a read-only
  installation and launch-readiness audit; do not start Viewer 2.0, change its endpoint, restart
  services, run the updater, uninstall either client, or modify scheduled tasks.
- Distinguish MSI registration from a usable client: verify package identity, executable hashes
  and signatures, service/task registration, configured endpoint, process/session state, local
  IPC/listeners, recent secret-safe logs, and reachability to the retained Docker canary.

## Plan

- [x] Capture Windows identity/session, installed-package, product-code, install-root, version,
      publisher, uninstall-command, and Authenticode/hash evidence without dumping registry secrets.
- [x] Verify Viewer service definition, account, automatic start/recovery, executable path,
      scheduled tasks, interactive processes, and coexistence with CamViewer 1.0.
- [x] Inspect only known CamStation configuration and log locations; report endpoint, release
      identity, permissions, IPC/listeners, and bounded recent errors with secrets redacted.
- [x] Test NUC-to-canary HTTP health and `/viewer` reachability without launching or reconfiguring
      the GUI client.
- [x] Classify installation as complete, staged-but-not-ready, or broken; update the maintenance
      report, evidence chain, lessons if a correction is learned, and final validation record.

## Acceptance criteria

- [x] Every MSI-owned Viewer 2.0 payload resolves to an existing expected file at its recorded
      size; key executable versions, hashes, signature state, owners, and ACLs are recorded without
      exposing credentials.
- [x] Service and startup registrations target the same installed release and have a viable automatic
      startup path for the real interactive user.
- [x] The configured server endpoint is identified and tested from NUC; its mismatch with
      `http://10.0.0.26:18081` is reported as a blocker rather than silently changed.
- [x] CamViewer 1.0 remains running and no process, service, task, file, registry value, or network
      listener is changed during verification.

## Review

- Windows Installer reports CamStation Viewer 2.0.20 in installed state 5. Its cached MSI exists;
  all 76 MSI-owned files are present at their expected sizes, with zero missing or mismatched
  payloads. The install root has two additional, inactive files left by the previous bootstrap
  generation; they are not evidence of a failed current MSI installation.
- The direct LocalSystem service is Running/Auto with exit code 0 and SCM restart recovery. The
  current MSI-owned HKLM Run value and common shortcuts point directly to the installed Viewer.
  Zero Viewer scheduled tasks is expected for this package generation, not a missing component.
- Program Files, ProgramData, and the 64-bit Viewer registry key deny ordinary-user writes. The
  cached MSI, setup wrapper, Viewer executable, and service executable have no embedded
  Authenticode signature, so this remains a development/staging package rather than an approved
  signed production release. No server release metadata is currently published for independent
  hash/provenance comparison.
- The installation is complete but cutover is not configured: endpoint `.172:18080` is offline,
  auto-start is false, no interactive 2.0 process exists, and the retained canary has no Viewer
  registration. NUC reaches `http://10.0.0.26:18081` successfully.
- Final non-mutating verification found six CamViewer 1.0 processes in console session 1, zero
  interactive Viewer 2.0 processes, and the same Viewer service PID. No process, service, task,
  file, registry value, listener, endpoint, or auto-start setting was changed.
- Closing validation passed relative-link, Evidence-reference, code-fence, heading-depth,
  placeholder, sensitive-pattern, trailing-whitespace, and `git diff --check` checks. A changing
  health `startedAt` value was investigated rather than accepted as a restart: Docker remained
  healthy with restart count 0, and source confirms that field is request time rather than uptime.
- Updated Korean maintenance report SHA-256:
  `08844411762fa3c3cb9c63d53e745f670c9345f4d9b520c49b6e2d994c3109f4`.

---

# 2026-08-09 NUC Viewer 2.0 latest-version reinstall

## Scope and decisions

- Treat the operator-reported client bug as a functional defect independent of the previously
  validated MSI file placement. Reinstalling the same artifact is not acceptable evidence of a fix.
- Define “latest” by comparing the checked-out Viewer source and commit with the configured upstream,
  then build a new version greater than the installed MSI 2.0.20 with a deterministic hash.
- Keep CamViewer 1.0 running and preserve the current Viewer 2.0 configuration, stable identity,
  auto-start choice, installed MSI cache/hash, and rollback evidence. Do not reboot the NUC, retire
  1.0, or change the Viewer endpoint during this reinstall.
- This NUC is an internal canary workstation. If no production signing identity is available, an
  explicitly marked unsigned development MSI may be installed only after source tests, package
  policy checks, hash verification on both hosts, and a clear final status record.
- The NUC is an installation/maintenance target, not an MSI build host. Build and sign future MSI
  packages on a dedicated Windows VM or CI runner; transfer only the finished, verified package.
- The operator supplied a first-run screenshot and exact defect: while entering the server address,
  the field loses focus and cannot be edited. Treat delayed setup-state hydration and repeated native
  window `show()` calls as separate focus-stealing paths and cover both with regression tests.

## Plan

- [x] Reproduce the reported focus loss in tests: preserve an active/dirty server field across
      delayed setup-state hydration and prohibit a second native focus acquisition after load.
- [x] Implement the smallest renderer/window-lifecycle fix and verify keyboard entry, tab order,
      retry behavior, validation errors, and setup-state preservation.
- [x] Compare local/upstream source, Viewer package metadata, build scripts, prior artifacts, and
      installed 2.0.20 to select a unique higher release version without discarding dirty worktree changes.
- [ ] Run the Viewer/service/MSI tests, build the Electron payload and service, then produce and
      inspect the new MSI/setup artifact with hashes and expected version/signature state.
- [ ] Capture a secret-safe NUC rollback baseline and preserve the cached 2.0.20 package/configuration;
      verify disk space and keep all six CamViewer 1.0 processes running.
- [ ] Transfer to a restricted staging directory, verify the remote hash, execute a quiet no-reboot
      MSI upgrade with a bounded log, and stop on any non-success installer code.
- [ ] Verify new product version/files/service/recovery/ACL/startup/config preservation, confirm 1.0
      continuity, and inspect bounded install/service logs for errors.
- [ ] Update Evidence → Finding → Path, the Korean maintenance report, lessons, and closing checks;
      retain or remove the staging package according to the documented rollback decision.

## Acceptance criteria

- [x] A delayed initial status or retry response never overwrites an active/dirty server address,
      the initial untouched form focuses that field once, and the native window is shown only once.
- [ ] The installed MSI version is greater than 2.0.20 and its key hashes match the locally built,
      remotely transferred artifact; same-version reinstall is not accepted.
- [ ] MSI completes with success or reboot-required-success only, but the workflow never initiates
      a reboot. Product state is installed, service is Running/Auto with exit code 0, and package
      ownership has no missing or size-mismatched file.
- [ ] Configuration schema, endpoint host/port, stable client-identity presence, display-name
      presence, and `autoStart=false` are preserved without printing secret values.
- [ ] CamViewer 1.0 remains active in session 1 throughout; no 2.0 interactive process is launched,
      no endpoint is changed, and the Docker canary remains healthy with no Viewer registration.
- [ ] Rollback material and an operator-ready next step for interactive bug verification are
      documented, and all relevant tests, links, evidence references, and whitespace checks pass.

## Review — stopped before MSI production

- The focus fix passed all 25 Viewer tests and an automated browser scenario in which a delayed
  setup response arrived while the server-address field was active; both the value and focus were
  preserved. No MSI containing this fix was produced or installed.
- WiX 6.0.2 rejected the Linux build host before producing an MSI. A portable Windows .NET SDK was
  then staged on NUC solely to test the Windows build path, but the operator stopped that approach
  before restore/build completed and clarified that NUC must remain an install/maintenance target.
- The exact NUC build stage `C:\ProgramData\CamStation\Maintenance\Viewer-2.0.21-build`, including
  the portable SDK and build inputs, was removed. No scoped build process remained.
- Post-cleanup verification: installed Viewer remains 2.0.20 in Windows Installer state 5;
  `CamStationViewerService` is Running/Auto; six CamViewer 1.0 processes remain active; no
  CamStationViewer 2.0 interactive process is running. The restricted maintenance root was retained
  for receiving verified MSI packages in future.
- Resume only after a dedicated Windows VM/CI runner is designated. That runner builds/signs and
  validates the MSI; NUC receives only the final hash-verified MSI for install/repair/uninstall.

---

# 2026-08-09 local Viewer MSI build path

## Scope and decisions

- Restore the missing repository-owned Windows build entry point for the existing WiX 6.0.2 MSI.
- The build runs only on a dedicated x64 Windows developer machine or VM. The current Linux host
  may run source/policy tests but must fail before pretending to produce an MSI; the NUC remains an
  install/repair/uninstall target and never receives build tools.
- Produce an explicitly unsigned development package only when `-UnsignedDevelopment` is supplied.
  Do not imply production signing when no signing identity or signing script is configured.
- Build in an ignored, version-specific workspace and never rewrite the tracked
  `installer/Files.generated.wxs` or tracked service executable.

## Plan

- [x] Add failing source-policy tests for platform gating, tool/version checks, deterministic build
      order, isolated generated inputs, locked WiX restore, explicit unsigned policy, MSI property
      verification, and SHA-256 build metadata.
- [x] Implement `scripts/build-viewer-msi.ps1` with one public command, actionable prerequisite
      failures, cleanup limited to its exact generated workspace, and no NUC-specific paths.
- [x] Add a Windows-local quick-start and troubleshooting guide under `installer/README.md`, linked
      to the existing installer design and explicitly separating build and install hosts.
- [x] Run focused and full Viewer tests, TypeScript/package builds, PowerShell parser checks, and
      repository whitespace/sensitive-boundary checks on Linux.
- [ ] Record the remaining real-Windows gate: run the documented command on a dedicated Windows
      host and verify the resulting MSI version, file count, hash metadata, and signature state.

## Acceptance criteria

- [ ] One documented Windows command builds Electron, the versioned Go service, a fresh WiX file
      fragment, and `CamStationViewer.msi` without modifying tracked generated/binary inputs.
- [ ] Missing Windows, Node 22+, Go 1.25+, .NET SDK 8.x, or explicit unsigned/signing policy fails
      before an MSI is published, with an actionable error.
- [ ] The output directory contains the MSI, WiX symbols, and `build-metadata.json`; metadata records
      requested/product version, source commit/dirty flag, byte size, lowercase SHA-256, and
      `developmentUnsigned=true` without machine paths or secrets.
- [ ] MSI database inspection proves product name, version, and fixed UpgradeCode before success is
      reported; the artifact remains uninstalled during the build.
- [x] Linux validation passes, and documentation states honestly that actual MSI production still
      requires the dedicated Windows build-host gate.

## Review — build entry point prepared; Windows artifact gate open

- Added `scripts/build-viewer-msi.ps1` and `installer/README.md`. The documented command builds an
  explicitly unsigned development MSI on a dedicated x64 Windows machine or VM, publishes it under
  an ignored version directory, and never installs it or connects to NUC.
- Added four repository contract tests. The initial RED run failed because the entry point and guide
  did not exist; the completed implementation passes all four tests and the full 29-test Viewer suite.
- Linux-side validation passed: Viewer TypeScript build, Windows Electron packaging, Viewer-service
  Go tests, the official PowerShell 7.5 parser, the non-Windows fail-closed gate, a temporary x64 PE
  service cross-build, fresh WiX fragment generation, and tracked-input hash preservation.
- No MSI was produced. This host has Linux Docker only and no QEMU/KVM-backed Windows VM or Windows
  image. WiX's actual Windows Installer build and COM database inspection therefore remain unproved.
- Next gate: designate a dedicated Windows x64 developer machine/VM, run the documented 2.0.21
  command, and validate the MSI, WiX symbols, hash file, and metadata. NUC remains install,
  repair, and uninstall only.
- Recorded the boundary as E-019 → F-012 → P-004 and updated the Korean maintenance report. Final
  relative-link, Evidence-reference, code-fence, secret-boundary, ignored-output, protected-input,
  trailing-whitespace, and `git diff --check` validation passed. Current report SHA-256 is
  `57db8776a510d08aa4549d7d3659871868791135bc084c964cb9c02d29705fc2`.

---

# 2026-08-09 CamStation 2.0 recording cleanup

## Scope and safety boundary

- Target only the retained Docker 2.0 canary `camstation2-canary` on the verified `cctv` server.
  Preserve every legacy 1.0 recording, service, process, database, go2rtc configuration, and path.
- Resolve container mount sources, the 2.0 SQLite recording rows, finalized media files, and active
  temporary segments before deletion. Stop if any target path overlaps the 1.0 runtime.
- Delete only completed 2.0 recording rows/files through an application-supported path when
  available. Never unlink an active temp/open segment or use a broad recursive filesystem command.
- Record exact pre/post counts and bytes, whether the removed files are recoverable, and prove the
  2.0 live/recorder health plus the 1.0 service/PID baseline after cleanup.

## Plan

- [x] Capture the exact 2.0 container identity, mounts, recording settings, DB/file inventory, and
      legacy 1.0 continuity baseline without printing camera URLs or credentials.
- [x] Inspect the 2.0 recording deletion/cleanup implementation and select the narrow supported
      method that keeps DB and filesystem state consistent.
- [x] Delete all finalized 2.0 recording media requested by the operator while excluding active
      temp/open segments and every legacy 1.0 path.
- [x] Verify zero targeted finalized rows/files remain, no orphan metadata is introduced, current
      2.0 recording continues safely, and all 1.0 services/processes remain unchanged.
- [x] Update the operations evidence/report, lessons, and this review with the exact deletion result
      and recoverability statement; run closing document and diff checks.

## Acceptance criteria

- [x] The deletion scope is proven to be 2.0-only by container mount, canonical path, and DB row.
- [x] Every deleted item was finalized and inactive at deletion time; no open/temp segment is removed.
- [x] Post-cleanup DB/file counts agree, and any new post-cleanup segment is healthy and playable.
- [x] Legacy 1.0 service PIDs/restart counts and recording activity remain unchanged.
- [x] The final report states exact removed file/row count and bytes and whether recovery is possible.

## Review

- The exact target was `camstation2-canary`, with state at `/var/lib/camstation2-canary/data` and
  media at `/mnt/hdd/camstation2-canary`. No legacy path overlapped the deletion scope.
- Preflight proved 1,623 `ready` rows exactly matched 1,623 MP4 files and 9,193,448,264 bytes, with
  zero unsafe paths, missing files, size mismatches, or extra files. The latest file from each of
  the three cameras passed ffprobe as an approximately 60-second H.264/AAC MP4.
- Used only `DELETE /api/recordings/segments/{id}` for guarded, exact finalized-ID snapshots. The
  continuously running recorders finalized nine more files during the first sweep; two guarded
  follow-up sweeps removed those too. Total: 1,632 files and 9,245,386,547 bytes.
- Final checkpoint: zero `ready` rows, zero completed recording files, zero `.deleting-*` files,
  three active temp segments, three running recorder workers, three streaming home cameras, and a
  healthy container with restart 0/no OOM. A ten-second sample proved all three active files grew.
- The five 1.0 units retained MainPIDs `248/326/247/396/246`, active/running state, and restart 0;
  legacy health remained `ok`. A closing ten-second sample found all eight legacy recorder MP4
  files on stable inodes and all eight grew. No 1.0 file, database, configuration, service, or
  process changed.
- All deleted rows remain audit tombstones with `backup_state=pending` and no `backed_up_at`. No
  trash/quarantine copy exists, so CamStation cannot restore the deleted recordings. Recording is
  still enabled and will create new one-minute files after the zero-file checkpoint.
- Evidence chain: E-020 → F-013 → P-005.
- Closing validation passed 54 relative links across ten task/report/evidence documents, balanced
  code fences, Evidence references, secret-pattern scan, and `git diff --check`. Final report
  SHA-256 is `38644e8ed25a58c00de2111b861cb816d3667e15db4fda0e371ac711aa159271`;
  canary operations SHA-256 is `b49583060fa4c21ef17e2f87b7c40fcaaf09e098082f8fc4c7253795288e98b3`.

---

# 2026-08-10 WinPC 10.0.0.30 developer access bootstrap

## Scope and decisions

- Provision only the operator-authorized Windows development PC `10.0.0.30`; do not scan the
  subnet or reuse the NUC monitoring-PC account/key.
- Create one dedicated `CamStationBuildOps` local administrator and one host-specific Ed25519 key.
  Keep the private key on this maintenance host and embed only the public key in the operator-run
  elevated PowerShell script.
- Install/enable Windows OpenSSH Server if absent and restrict the new inbound firewall rule to the
  verified maintenance source. Stage one does not edit `sshd_config`; exact-user/key-only hardening
  follows only after a pinned-host-key login succeeds.
- If the dedicated account or administrator key file already exists, stop and return the diagnostic
  instead of overwriting ownership. Do not enable or change RDP, WinRM, SMB, or unattended GUI.

## Plan

- [x] Resolve the exact route/source address and current TCP/22 state for `10.0.0.30` without broad discovery.
- [x] Generate a dedicated local Ed25519 maintenance key and record only its public fingerprint.
- [x] Implement one pasteable elevated PowerShell bootstrap with guarded account creation, the
      administrator-key ACL, an exact-source firewall rule, service start, and host-key output.
- [x] Validate PowerShell syntax and source-policy boundaries locally, then provide the exact
      operator command and expected success output.
- [x] After the operator runs it, pin the returned host key and prove public-key-only administrative
      SSH before using the PC for Viewer/MSI development.

## Acceptance criteria

- [x] No secret/private key is embedded in the script or printed.
- [x] Only `CamStationBuildOps` with the dedicated key can use the newly provisioned SSH path.
- [x] TCP/22 is allowed only from the verified maintenance source/network, not all remote addresses.
- [x] The first-stage script does not edit `sshd_config`, and it stops instead of replacing an
      existing dedicated account or administrator authorized-key file.
- [x] The operator result, independent host-key comparison, pinned-host-key login, administrator
      identity, password denial, and forwarding denial all succeed.

## Review — access established and hardened

- Exact route verification resolved `10.0.0.30` directly over `eth0` with maintenance source
  `10.0.0.16`. TCP/22 was not accepting connections before provisioning; the existing RDP path was
  observed but not modified.
- Generated a target-specific Ed25519 identity outside the repository. The private key is root-only
  (`0600`) and the public fingerprint is
  `SHA256:E1eBfRkf6wvFxi92ov8iD8xfq6XtssO+So2/sFzo5eE`.
- Prepared one block that can be pasted directly into elevated Windows PowerShell without file
  transfer. It verifies the exact target and elevation, installs OpenSSH Server if needed, creates
  `CamStationBuildOps`, installs only the dedicated public key with the Microsoft-required ACL,
  disables the broad default rule, and permits TCP/22 only from `10.0.0.16` to `10.0.0.30`.
- Stage one intentionally left `sshd_config` untouched. The operator returned ED25519 host
  fingerprint `SHA256:nJJI5bVKmwDuWfRqTpN1XUEd5ZkOZZM0cdmyZFIR40Y`; an independent key scan
  produced the same fingerprint before it was written to a dedicated known-hosts file.
- The paste block stops if the target account or administrator key file already exists. It does not
  modify RDP, WinRM, SMB, the NUC, or any CCTV runtime.
- Local validation passed: PowerShell 7.5 parser and two focused source-policy tests. Final paste
  block SHA-256 is `233d37aa70fd3d370c36ad9a9a11c32c86725cfb681c547a50d2cfe9e54e6845`.
- A strict fresh login proved identity `WIN11-DELL\CamStationBuildOps`, membership in
  `S-1-5-32-544`, and High integrity `S-1-16-12288`. The service is Running/Auto and its TCP listener
  is exactly `10.0.0.30:22`.
- Inspected the actual default configuration and then installed a validated managed policy with
  `ListenAddress 10.0.0.30`, `AllowUsers camstationbuildops@10.0.0.16`, public-key-only
  authentication, and forwarding/tunnel denial. The pre-change configuration is retained at
  `C:\ProgramData\ssh\sshd_config.pre-camstation-buildops-20260810-083727.bak`.
- Post-change tests proved: a fresh pinned-key login succeeds; password-only authentication returns
  `Permission denied (publickey)`; direct TCP forwarding returns `administratively prohibited`;
  `sshd -t` passes; and the only enabled inbound TCP/22 rule is `CamStation-BuildOps-SSH-In`, scoped
  from `10.0.0.16` to `10.0.0.30`.
- A read-only development inventory found x64 Windows PowerShell 5.1, 31.3 GB free on `C:`, and no
  Git, Node/npm, Go, .NET SDK, WiX, winget, SignTool, or MSBuild visible to the dedicated account.
  Toolchain installation remains a separate development-host setup action.
- RDP, WinRM, SMB, the interactive RDP PowerShell process, the NUC, and all CCTV runtime state were
  left unchanged. Temporary remote diagnostic PowerShell processes were identified by exact PID and
  removed; the existing RDP-session PowerShell process was preserved.

---

# 2026-08-10 WinPC Viewer development environment and first MSI build

## Scope and decisions

- Use only the authorized `WIN11-DELL` build host through the pinned `CamStationBuildOps` SSH path.
  Keep the monitoring NUC, CCTV server, RDP user's profile, and installed Viewer fleet outside this
  task.
- Install hash-verified portable tools below `C:\CamStationDev\tools` and persist PATH only for the
  dedicated build account. Do not install compilers into the RDP user's profile or machine-wide
  package managers.
- Treat `/workspace/CamStation` as the canonical source. Clone the exact base commit on Windows,
  apply the canonical tracked diff, and transfer only non-ignored untracked files; never transfer
  ignored runtime data, recordings, credentials, caches, `node_modules`, or prebuilt artifacts.
- Use the existing Viewer MSI build specification and locked WiX 6.0.2 restore. Produce version
  `2.0.21` only as an explicitly unsigned development artifact; do not install it on WIN11-DELL or
  the NUC during this task.

## Toolchain selection

- Node.js `22.23.2` Windows x64 ZIP, SHA-256
  `1177b4137ba5adaa56354ae40f1080c7450e8ae09cecb47da459d1c52ac99f97`.
- Go `1.25.12` Windows amd64 ZIP, SHA-256
  `d5dc82da351b00e5eedd04f41356817d674cc4308131f0f638a5b14c5c3af4cb`.
- .NET SDK `8.0.423` Windows x64 ZIP, release-metadata SHA-512
  `063fcc35c136277e6fd767c66579f3b92db22a078a7f0c7177b6af1edb2c9afae1613f6cfdc01acf7421773d9ac77f0ef73a7fd8b37f469e7e3505e5c1361ba0`.
- MinGit `2.55.0.3` x64 ZIP, SHA-256
  `f48e2d2dc74a24454adc6d8fd0ac25bf9c2386f19cfb06202b9465aaad4f9f05`.
- PowerShell `7.6.4` x64 ZIP, SHA-256
  `80832551c52809301e6071c8bac977beb5a2f1ec953eb4db9f94deb953333793`.
- Microsoft Visual C++ Redistributable x64 `14.51.36247.0`, official Microsoft-signed installer
  SHA-256 `843068991daaa1f73ad9f6239bce4d0f6a07a51f18c37ea2a867e9beca71295c`.
  This machine-wide runtime is the narrow exception to the portable-tool policy because Electron's
  native extract module imports `VCRUNTIME140.dll`; installation completed with exit code `0` and
  no restart requirement.

## Plan

- [x] Recheck disk, architecture, outbound HTTPS, exact tool URLs/hashes, and that the dedicated
      development root is absent before making changes.
- [x] Download each official archive to a bounded cache, verify its published digest before
      extraction, install it under the versioned tools root, and persist the dedicated user's PATH.
- [x] Verify tool versions from a fresh pinned SSH session and record a secret-free toolchain
      manifest with URLs, digests, versions, and installation time.
- [x] Clone base commit `1215d0518a8e74866a5d786af865fdb4967bb18d`, apply the canonical tracked
      diff/deletions, transfer non-ignored untracked files, and prove the Windows source status
      represents the current canonical workspace without ignored data.
- [x] Run Viewer dependency installation, all Viewer tests/build/package checks, and targeted Go
      service tests on Windows; fix any source or Windows-only issue in the canonical workspace and
      resynchronize it.
- [x] Run the locked unsigned `2.0.21` MSI build, inspect its database/metadata/hash outputs, and
      prove tracked installer inputs were unchanged and no MSI was installed.
- [x] Record final evidence, remaining signing/lifecycle gates, rollback paths, free space, and the
      next client-development step.

## Acceptance criteria

- [x] Every downloaded executable archive matches an official published digest before extraction.
- [x] A new SSH session resolves x64 PowerShell 7, Node 22, Go 1.25, .NET SDK 8, Git, npm, and the
      locked WiX dependency from only the dedicated development environment.
- [x] The Windows checkout preserves the exact base commit and honestly reports local source changes;
      no secret, runtime DB, recording, log, `node_modules`, or prior artifact is transferred.
- [x] Windows Viewer tests, TypeScript build, Electron x64 package, Go service tests, locked WiX
      restore/build, and MSI COM inspection all pass.
- [x] Published output contains the MSI, wixpdb, SHA-256 file, and secret-free metadata identifying
      version `2.0.21` as unsigned development output.
- [x] No MSI is installed and no NUC, CCTV, RDP-profile, monitoring, or production state changes.

## Review — dedicated Windows environment and unsigned MSI validated

- The isolated development root is `C:\CamStationDev`. A fresh pinned-key SSH session resolves
  PowerShell `7.6.4`, MinGit `2.55.0.3`, Node `22.23.2`/npm `10.9.8`, Go `1.25.12`, .NET SDK
  `8.0.423`, and locked WiX `6.0.2`. Toolchain manifest SHA-256 is
  `18a30cdea8be173c3a5e3fac6d78165b71bd5dcf81668ede139e35f033f39a19`.
- The Windows checkout keeps base commit `1215d0518a8e74866a5d786af865fdb4967bb18d`; its canonical
  status SHA-256 is `3f32c3c4e0be51f520339623bc169b0dac7ad2673a88a282d9d7f8c9f6e26658`
  and tracked binary-diff SHA-256 is
  `d5aef17691c4f62c5ee6dbda0c9632865be98566358945f80ea509a5fd04cd63`.
  `git diff --check` passes and ignored runtime/build data was not transferred.
- Native Windows execution exposed and fixed the real platform boundaries: named-pipe tests,
  portable npm filesystem commands, ASAR path separators, Visual C++ native runtime availability,
  WiX shortcut/directory ICE validation, supported Windows Installer SQL, explicit COM marshalling,
  and deterministic COM handle release.
- `pwsh -NoProfile -File .\scripts\build-viewer-msi.ps1 -Version 2.0.21
  -UnsignedDevelopment` completed with exit code `0`: Viewer tests passed `33/33`, Electron produced
  a win32-x64 package, both targeted Go service packages passed, and WiX reported zero warnings and
  zero errors.
- Published output is `C:\CamStationDev\src\CamStation\artifacts\viewer-msi\2.0.21` with exactly
  the MSI, wixpdb, SHA-256 sidecar, and metadata. `CamStationViewer.msi` is `124350464` bytes with
  SHA-256 `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e`.
  Independent COM inspection confirms version `2.0.21`, ProductCode
  `{094DC194-180B-4FDC-A399-F5DB6E96A86E}`, UpgradeCode
  `{7D4769BB-89EF-4C36-B4F2-52E33BF8BE87}`, and `76` File rows. Build time was
  `2026-08-10 09:32:20 KST`.
- Independent post-build checks confirm the sidecar/metadata digests and size, no secret or absolute
  path in metadata, `NotSigned` signature state, zero installed Viewer uninstall entries, zero
  `CamStationViewerService` services, zero temporary build workspaces, and unchanged protected
  installer inputs. The duplicate pre-fix build and two one-use source-transfer staging files were
  removed after the final MSI hash was rechecked; free space is `28.34 GB`. The monitoring NUC,
  CCTV runtime, and RDP profile were untouched.
- This is an unsigned development artifact, not a production deployment candidate. Remaining gates
  are Authenticode signing and install/upgrade/repair/uninstall lifecycle testing on a disposable
  Windows target. Rollback of this development setup is removal of the bounded `C:\CamStationDev`
  tree; the machine-wide Visual C++ runtime must only be removed after confirming no other software
  uses it.

---

# 2026-08-10 WIN11-DELL clean Viewer MSI installation

## Scope and clean-state definition

- Work only on the authorized `10.0.0.30` development PC. Preserve `C:\CamStationDev`, all build
  tools and source, unrelated software, user documents, the existing RDP session, the monitoring
  NUC, and CCTV services.
- “Clean” means no CamStation Viewer product registration, exact Viewer service/process, install
  directory, machine configuration/installer marker/Run value, product-created public shortcuts,
  ProgramData Viewer state, or exact Electron `CamStationViewer` profile residue.
- Prefer MSI uninstall for a registered product. Remove only an independently resolved exact Viewer
  residue after processes/services are stopped; never use broad CamStation, profile, Program Files,
  or registry deletion.
- Install the already verified unsigned development MSI `2.0.21` and leave it installed for client
  development. Preserve a verbose MSI log and record the exact rollback command. Do not configure a
  server address or start deployment to the NUC in this task.

## Plan

- [x] Inspect the MSI contract and capture a read-only baseline of product registrations, exact
      processes/services, install/state directories, registry values, shortcuts, profile residues,
      active sessions, and MSI integrity.
- [x] Remove only confirmed Viewer-specific registrations and residues, then prove the clean-state
      predicates are all true before installation.
- [x] Install MSI `2.0.21` with `msiexec.exe`, a bounded verbose log, and an explicit timeout; record
      the process exit code and reboot requirement.
- [x] Verify Windows Installer identity/version, installed file manifest, service binary/config/start
      state, Run value, shortcuts, ProgramData/log state, ACL-relevant ownership, and MSI self-repair
      registration without exposing configuration or credentials.
- [x] Perform a bounded basic runtime check without entering a server address, preserve the installed
      state, and document evidence, rollback, remaining UI/configuration tests, and free disk space.

## Acceptance criteria

- [x] The pre-install baseline is clean by the exact Viewer-only definition, with all unrelated host
      state and the development toolchain preserved.
- [x] `msiexec` returns success or success-with-reboot handling is explicitly recorded; the expected
      ProductCode `{094DC194-180B-4FDC-A399-F5DB6E96A86E}` and version `2.0.21` are registered once.
- [x] `CamStationViewerService` runs automatically from the expected Program Files path, produces a
      bounded service log, and has no unexpected restart/failure state.
- [x] Installed files, shortcuts, machine Run value, installer marker, and MSI cache/source identity
      match the authored package, while the Viewer remains unconfigured and no production endpoint
      is contacted.
- [x] The final report identifies the unsigned-development limitation, verbose install log, exact
      uninstall rollback command, and any remaining interactive desktop validation.

## Review — clean install and service lifecycle passed

- At `2026-08-10 09:45:13 KST`, the pre-install baseline found zero Viewer product registrations,
  services, processes, scheduled tasks, owned paths, registry keys/Run values, public shortcuts, and
  exact Electron profile residues. No cleanup deletion was needed. The active RDP session and
  `C:\CamStationDev` remained intact.
- The hash-pinned unsigned MSI was installed silently from `09:45:53` to `09:46:04 KST` with
  `msiexec` exit code `0` and no reboot requirement. The verbose log contains two invariant
  `MainEngineThread is returning 0` markers, no `Return value 3`, and a matching MsiInstaller success
  event. Install-log SHA-256 is
  `2377e524d46a35d4c94f2b41ab841b74fb8b9f88611d0f5a0381eb8dd2c5902e`.
- Windows Installer registers exactly one `CamStation Viewer` `2.0.21` product with ProductCode
  `{094DC194-180B-4FDC-A399-F5DB6E96A86E}`. The install contains `76` files totaling `370771832`
  bytes. Its cached MSI is byte-for-byte hash-identical to the source MSI, whose SHA-256 remains
  `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e`.
- `CamStationViewerService` runs from the exact Program Files path as `LocalSystem` with automatic
  start. A bounded stop/start test replaced its PID and produced the exact service-log sequence
  `running, stopped, running` with zero failure records. Recovery is configured for three 60-second
  restart actions with a one-day reset period.
- The public Desktop and Start Menu shortcuts, HKLM Run value, installer marker, ProgramData log,
  MSI cached source, and authored service path all exist. The machine configuration key is absent,
  no server endpoint was contacted, and no Viewer GUI process was launched.
- The management pipe correctly rejected the SSH network-logon token according to its deny-network,
  allow-interactive ACL. This boundary was not weakened. Opening the Viewer from the existing RDP
  desktop and checking setup-screen input remains the only interactive validation item.
- Final machine verification returned `INSTALLED_AND_SERVICE_VERIFIED` with every non-interactive
  check true at `2026-08-10 09:53:30 KST`. Evidence is under
  `C:\CamStationDev\evidence\viewer-install-2.0.21`; final report SHA-256 is
  `03d12a278ea036f7e72479f53ff0e71672f76249b387ad2c51e9daf3908a9a69`. Free disk is `27.77 GB`.
- The installed state is intentionally preserved. Exact rollback command:
  `msiexec.exe /x {094DC194-180B-4FDC-A399-F5DB6E96A86E} /qn /norestart /L*V
  "C:\CamStationDev\evidence\viewer-install-2.0.21\uninstall.log"`. The artifact remains unsigned
  development output and is not approved for NUC/production deployment.

---

# 2026-08-10 WIN11-DELL interactive GUI observability

## Specification and security boundary

- Provide a closed loop from Linux SSH to the existing `dyllislev` RDP desktop: launch the installed
  Viewer, capture its real window, return PNG/UIA evidence, and later support bounded focus/input
  actions without asking the operator to take screenshots.
- Use Windows Task Scheduler logon type `TASK_LOGON_INTERACTIVE_TOKEN` (`3`) so the one-shot worker
  runs only when the intended user is already logged on. Store no password and add no network
  listener, firewall rule, VNC/RDP service, remote-control account, or weakened Viewer pipe ACL.
- Register a unique least-privileged task for each operation, limit it to two minutes, and delete the
  exact task in `finally`. Restrict the run directory to SYSTEM, Administrators, and the target user.
- Capture only the verified `CamStationViewer` top-level window rectangle. Do not capture the whole
  desktop by default or collect text-field values. Record bounded control names/types, session ID,
  process IDs, dimensions, and image digest for reproducibility.
- Keep the Viewer installed and unconfigured during the first visual proof. Do not type a server
  address or contact CCTV until the rendered setup screen and input controls have been observed.

## Plan

- [x] Inventory Task Scheduler/session prerequisites and confirm no existing GUI bridge, listening
      service, task, or tool owns the proposed namespace.
- [x] Implement the interactive Viewer window capture worker and one-shot Task Scheduler launcher,
      with source-policy tests for session, ACL, timeout, target-window, and cleanup invariants.
- [x] Validate locally, synchronize only the new scripts/tests/docs to WIN11-DELL, and prove source
      hashes before execution.
- [x] Run `LaunchAndCapture` in the existing RDP session, retrieve the PNG and bounded UIA JSON over
      SSH, visually inspect the image, and prove the task was deleted with no new listener/service.
- [x] Record the working command, evidence hashes, limitations, and the next focus/input action needed
      for Viewer development.

## Acceptance criteria

- [x] The worker runs as `WIN11-DELL\dyllislev` in the same nonzero session as the active Explorer
      process without storing or requesting the user's password.
- [x] The installed Viewer produces a nonempty, target-window-only PNG that Codex retrieves and
      inspects directly; metadata identifies the matching Viewer PID/session and exact SHA-256.
- [x] UIA evidence is bounded and secret-safe and is sufficient to locate the setup form or clearly
      records that visual-coordinate fallback is required.
- [x] The unique scheduled task and worker PowerShell process exit, no GUI bridge port/service remains,
      and the Viewer service/RDP session/source MSI stay intact.

## Review

- Local Viewer tests passed `35/35`; the synchronized Windows source matched all recorded SHA-256
  values, parsed under Windows PowerShell 5.1, passed the native Windows Viewer tests `35/35`, and
  passed `git diff --check` before execution.
- Run `20260810T010741737Z-fd33ae4abe3c4a258cbca81d47526b43` launched Viewer PID `10308` as
  `dyllislev` in session `1` and captured a nonempty 1600x1200 window-only PNG.
- Run `20260810T011009032Z-fe024000cf854df78adc548b6b6801f1` repeated capture without relaunch;
  the PNG SHA-256 remained `e06bfb0520eb12ebf6b13c4de298a985155f665a5aa1b658418959b5027511b8`.
- Direct image inspection showed the complete Korean connection form with the server-address field
  focused. Settled UIA evidence independently confirmed `server-url` had keyboard focus and exposed
  `display-name`, both buttons, and the auto-start checkbox without reading any input values.
- Both tasks deleted themselves. Follow-up checks found zero remaining harness tasks/workers/bridge
  services; `CamStationViewerService` remained running/automatic and Explorer stayed in session `1`.

---

# 2026-08-10 repository commit and cleanup

## Specification

- Commit the completed CamStation server/canary, Windows Viewer/MSI, and GUI-observability work in
  reviewable logical commits without mixing generated runtime evidence into source history.
- Preserve all existing source and documentation changes. Do not pull, rebase, reset, or discard
  work while the branch is dirty; report the branch's upstream divergence separately.
- Keep operational evidence available locally but remove it from `git status` through a narrow
  repository-root ignore rule. Do not delete remote installation evidence, the MSI, or the running
  Viewer/service.
- Verify each intended commit's staged scope before committing, then run the relevant Go, web, and
  Viewer checks plus `git diff --check`. Finish with no unintended tracked or untracked source left.

## Plan

- [x] Inventory the complete dirty worktree, generated evidence, current branch, and upstream state.
- [x] Classify changed files into coherent development tooling, server/canary, Viewer surface,
      Windows Viewer/MSI, GUI observability, and maintenance-history commits.
- [x] Add only the narrow generated-evidence ignore needed to make the working tree maintainable.
- [x] Run the full relevant verification suite against the combined final tree.
- [x] Stage and inspect each logical commit, commit it, and verify the resulting commit contents.
- [x] Confirm final worktree status and record commit IDs, verification results, preserved evidence,
      and any remaining upstream action.

## Acceptance criteria

- [x] Every source change from the completed work is committed exactly once in a coherent commit.
- [x] Generated `work/`, build tools, artifacts, runtime data, and secrets are not committed.
- [x] Full verification passes on the exact committed tree.
- [x] The final status is clean apart from explicitly documented upstream divergence.

## Review

- Created eight scoped local commits: `2a2af55` development tooling, `0116944` migration/cutover,
  `b00f2b1` Viewer web surface, `c41b0ff` setup-focus fix, `7499e0e` MSI build, `25863aa` GUI
  capture, `a4d0479` operational documentation, and `1f3ad40` work history.
- Fetched the two upstream `/viewer` commits and reconciled them in merge `c45ec7f`. The verified
  saved-layout Viewer remained canonical, stale duplicate mobile components/plans were omitted, and
  the useful direct `/viewer` embedded-SPA test was retained. The web build reproduced
  `index-BlQ_LcUs.js` and `index-C8YzIVTY.css`.
- `./scripts/check-dev.sh` passed after reconciliation: all Go packages, 55 web tests, 35 Viewer
  tests, web lint/build, Viewer build, and daemon build. `scripts/production/test-policy.sh` and
  `git diff --check` also passed.
- The production policy check was corrected to distinguish the allowed Docker-internal
  `0.0.0.0:18080` listener from forbidden host-production exposure, while asserting the isolated
  host publication mapping explicitly.
- The reusable WinPC bootstrap was promoted from local evidence into `scripts/windows`; its source
  hash matches the operator-run script, so Viewer tests no longer depend on ignored workspace data.
- `.tools/`, `artifacts/`, `work/`, runtime `data/`, generated binaries, MSI output, screenshots,
  known-host files, and operational evidence remain uncommitted. `work/` was preserved locally.
- Final pre-review status was clean and `origin/camstation2-initial...HEAD` was `0 9`; no push was
  performed.

---

# 2026-08-10 project skill for Windows GUI verification

## Specification

- Register a repository-scoped Codex skill under `.agents/skills` so future CamStation sessions can
  discover the proven Windows Viewer GUI evidence workflow without relying on chat history.
- Reuse the reviewed scripts in `scripts/windows` as the only executable implementation. Keep the
  skill itself instructional and do not copy credentials, host keys, camera/server addresses, or
  environment-specific evidence into source control.
- Cover the complete verification loop: confirm an authorized active desktop session, launch or
  recapture the exact Viewer window, retrieve PNG/UIA/completion artifacts through the existing
  SSH boundary, verify hashes, inspect the image directly, and prove one-shot task cleanup.
- Preserve the established safety boundary: no full-desktop capture, text-field value collection,
  stored password, new listener/firewall rule, remote-control software, or Viewer ACL weakening.

## Plan

- [x] Initialize the repository skill with the supported skill-creator scaffold and Codex UI metadata.
- [x] Write concise triggering instructions and a deterministic runbook referencing the canonical
      project scripts, including settled-render recapture and cleanup/failure handling.
- [x] Add a source-policy test that protects discovery metadata, canonical script references, and
      the security/evidence invariants from accidental regression.
- [x] Validate the skill package, run the Viewer tests and repository whitespace checks, then record
      exact results and repository state.

## Acceptance criteria

- [x] The skill passes the official skill validator and lives at
      `.agents/skills/verify-windows-viewer-gui` with valid `SKILL.md` and `agents/openai.yaml`.
- [x] Its description reliably triggers for CamStation Windows GUI capture, focus, rendering, and
      interactive-session verification requests.
- [x] A future agent can follow it without asking the operator to manually capture the screen and
      without learning any secret or weakening remote-access controls.
- [x] Automated checks prove that exact-window capture, bounded UIA, artifact integrity, visual
      inspection, task cleanup, and prohibited access expansions remain documented.

## Review

- Added the repository-scoped `verify-windows-viewer-gui` skill with English and Korean trigger
  phrases plus Codex UI metadata. The runbook references the two reviewed PowerShell scripts instead
  of creating a second executable implementation.
- The workflow requires active-session preflight, local/remote source hash parity, one-shot
  `LaunchAndCapture` or `Capture`, exact three-file retrieval, SHA-256 verification, direct
  `view_image` inspection, bounded UIA correlation, settled-render recapture, and zero-task/worker
  cleanup proof.
- The skill package passed `quick_validate.py`. All Viewer tests passed `37/37`, including two new
  source-policy cases for trigger discovery and the GUI security/evidence contract; `git diff
  --check` also passed.
- A secret/environment scan found no concrete host IP, desktop username, host-key fingerprint, or
  private-key material in the skill. No Windows host, Viewer installation, camera configuration, or
  runtime evidence was changed while registering it.

---

# 2026-08-10 publish Viewer installer and return WIN11-DELL to clean-install state

## Specification

- Treat the acceptance path as one operator journey: `/settings` download -> Windows install ->
  Viewer launch -> server connection -> monitoring confirmation. The downloaded standard package is
  `CamStationViewer.msi`; it must install `CamStationViewer.exe` and the complete MSI-owned runtime.
  Do not publish a bare application EXE or revive the rejected custom Setup EXE.
- Identify the exact current CamStation Viewer 2.0 Windows installer format, version, size, and
  SHA-256 before changing either host. Do not rename an MSI to EXE or publish an unverified build.
- Publish the verified installer through the already running CamStation 2.0 Docker service using its
  existing release/download boundary when available. Keep the artifact in a persistent host mount,
  expose only the intended download file/metadata, and surface the download action on the 2.0
  `/settings` page rather than handing off only a raw API URL.
- Only after the server-side artifact is durable and an independent HTTP download matches its
  source hash, completely uninstall the existing Viewer product from the authorized Windows
  development PC. Remove only Viewer-owned installed state, service, tasks, shortcuts, registration,
  processes, and configuration; preserve SSH access, RDP, Windows build tools, source checkout, and
  unrelated evidence.
- Finish with two independent proofs: a fresh download from the 2.0 service has the expected hash,
  and the Windows PC has no installed Viewer product/runtime remnants while remaining available for
  the operator's manual installation test.

## Plan

- [x] Inventory the existing release endpoint/container mounts, candidate installer artifacts, and
      exact Windows installed product footprint without mutation.
- [x] Select and verify the canonical installer, stage it into persistent 2.0 release storage, and
      make the Docker settings page/download endpoint serve it with matching metadata.
- [x] Download the artifact independently through the `10.x` service URL and prove filename, size,
      content type, and SHA-256.
- [x] Uninstall the exact Windows Viewer product, then remove only confirmed Viewer-owned remnants.
- [x] Audit Windows product/service/process/task/file/registry state plus preserved SSH/build access;
      record the download URL, hash, rollback boundary, and verification results.

## Acceptance criteria

- [x] The published filename and extension match the real installer format and its version/hash are
      fixed in release metadata.
- [x] The installer appears as a download action on the CamStation 2.0 `/settings` page, remains
      downloadable after a container restart, and a downloaded copy matches the published SHA-256.
- [x] WIN11-DELL contains no installed CamStation Viewer product, Viewer service/process/task,
      auto-start entry, application directory, protected Viewer state, or product-created shortcut.
- [x] SSH maintenance access, the interactive Windows account/RDP session, source tree, and MSI build
      toolchain are preserved for subsequent development and the user's manual clean-install test.

## Review

- Commit `ed6c7df` adds exact-name MSI publication, filename-specific download headers, the visible
  settings filename/action, and an explicit guard that keeps MSI out of the legacy EXE Agent update
  path. The full Go suite, publisher contract, 55 web tests/lint/build, 37 Viewer tests/build, daemon
  build, and `git diff --check` pass.
- The dedicated Windows artifact and both server downloads are 124,350,464 bytes with SHA-256
  `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e`.
  The live browser snapshot shows version 2.0.21, `CamStationViewer.msi`, 118.6 MB, hash prefix, and an
  enabled download button; the browser click produced the same hash with no page/console error.
- The canary runs image `camstation:2.0.0-rc.20260810.8-canary` at image ID
  `sha256:719c6eea290f64251fc8858b8d327dc08296bfc52a746cefeec72b4dfbc69220`, healthy with restart 0,
  the same two bind mounts and port, exactly three streaming home cameras, and three running recorder
  workers. All five legacy 1.0 units retain their baseline PIDs and restart count 0.
- WIN11-DELL MSI uninstall returned 0 with no reboot. Product/service/process/task, five exact owned
  paths, Viewer/installer registry, Run value, and every user-profile Viewer residue are absent.
  SSH, Explorer session 1, development source/tools, and the original hash-matching MSI remain.
- Rollback is the previous immutable image `camstation:2.0.0-rc.20260809.7-canary` and root-only
  `.env.pre-msi-download-20260810-111413.bak`; persistent DB, media, and release storage were not
  deleted or replaced.

---

# 2026-08-10 Viewer 시험 레코드 삭제 및 운영 UI 정리

## Specification

- `QA Viewer`/`viewer-qa-01`은 실제 설치 클라이언트가 아니라 `/viewers`의 수동 하트비트
  시험 폼이 생성한 합성 레코드로 취급한다. 다른 Viewer 레코드는 변경하거나 삭제하지 않는다.
- 운영자 화면에서는 Viewer가 자체적으로 보내야 하는 하트비트를 임의 생성할 수 없게 하고,
  빈 목록 안내도 실제 Viewer 설치·연결 절차를 설명하도록 바꾼다.
- 삭제 가능 여부는 서버의 현재 상태 규칙(`offline` 또는 `stale`)과 동일하게 표시한다.
  최근 하트비트가 있는 Viewer에는 삭제 버튼을 비활성화하고 이유를 한국어로 안내한다.
- 서버가 삭제를 거절할 때 내부 오류 문자열 `validation`을 그대로 노출하지 않고, 현재 상태와
  재시도 조건이 포함된 구조화된 한국어 오류를 반환한다.
- 수정된 빌드는 새 불변 canary 이미지로만 배포한다. 기존 1.0 서비스, 카메라 3대,
  녹화·DB·배포 파일은 그대로 유지한다.

## Plan

- [x] 현재 canary에서 `viewer-qa-01`의 상태와 삭제 실패 HTTP 응답을 브라우저/API/로그로 재현한다.
- [x] 운영 페이지의 수동 하트비트 폼을 제거하고 Viewer 목록·삭제 UX를 서버 규칙과 일치시킨다.
- [x] 서버 삭제 충돌 응답을 구조화하고 route 및 웹 회귀 테스트를 추가한다.
- [x] Go·web 테스트, lint/build, daemon build와 diff 검사를 통과시킨다.
- [x] 새 불변 Docker 이미지를 배포하고 2.0/1.0 연속성 및 3대 카메라 상태를 검증한다.
- [x] 이미 TTL 만료 후 삭제된 정확한 `viewer-qa-01`의 부재와 다른 Viewer 보존을 배포 후
      재조회·새로고침·스크린샷으로 다시 증명한다.

## Acceptance criteria

- [x] `/viewers`에 QA용 하트비트 입력 폼과 미리 채워진 `QA Viewer`가 더 이상 노출되지 않는다.
- [x] 온라인 Viewer는 삭제할 수 없고, 오프라인 Viewer는 2단계 확인 후 삭제되며 원시
      `validation` 오류가 UI에 나타나지 않는다.
- [x] `viewer-qa-01`은 canary DB와 목록에서 제거되고 다른 Viewer 레코드 수·ID는 보존된다.
- [x] canary는 새 이미지로 healthy/restart 0이며 홈 카메라 3대가 유지되고, 1.0 다섯 서비스의
      PID와 재시작 횟수는 작업 전 기준과 동일하다.

## Review

- 수정 전 실제 브라우저에서 합성 레코드의 하트비트를 갱신하고 2단계 삭제를 수행해 정확한
  `DELETE /api/viewers/viewer-qa-01` 409와 원시 `validation` 표시를 재현했다. 30초 TTL이
  지난 뒤 같은 정확한 ID만 200으로 삭제됐고, 실제 Viewer ID는 보존됐다.
- 커밋 `00005dd`는 운영 UI의 수동 하트비트 폼과 웹 heartbeat mutation을 제거했다. 실제
  Viewer Agent용 서버 endpoint는 유지했다. UI/서버 모두 `offline`/`stale`만 삭제하며,
  온라인 경합은 `viewer_not_offline`, 현재 상태, 30초 조건이 있는 한국어 409를 반환한다.
- `./scripts/check-dev.sh`는 모든 Go 패키지, 57개 web 테스트·lint/build, 37개 Viewer
  테스트·build와 daemon build를 통과했다. production policy, focused route tests,
  embedded synthetic-string 검사와 `git diff --check`도 통과했다.
- 새 불변 이미지 `camstation:2.0.0-rc.20260810.9-canary`의 로컬/서버 ID는 모두
  `sha256:178b101f02488bf317ea8c447cb619adb4e151a0d943a634f35ea089ee5f28e4`이고 revision은
  `00005dd37760db1ec5c0e6afc06d1c4c60987d03`이다. 직전 `.8` 이미지는 보존했으며 root 전용
  롤백 포인터는 `.env.pre-viewer-registry-20260810-024232.bak`이다.
- 첫 이미지 포인터 치환 시도는 잘못된 `sed` 표현식으로 변경·재생성 전에 중단됐다. 즉시
  재확인한 `.env`, `.8` container, health는 그대로였고, 정확한 키 교체와 Compose 검증 후
  canary 하나만 재생성했다.
- 배포 후 canary는 healthy/restart 0, bind mount 2개와 `10.0.0.26:18081`을 유지한다.
  카메라 3/3, recorder 3/3, MSI 2.0.21/124,350,464 bytes가 유지되고 로그의 error/panic/fatal은
  0이다. 1.0 다섯 unit은 PID `248/326/247/396/246`, NRestarts 0으로 전후 동일하다.
- 실제 `/viewers` 화면은 Viewer 1대만 표시하고 QA 폼·행이 없으며 온라인 삭제 버튼과
  30초 안내가 정확히 렌더링됐다. 최종 화면 SHA-256은
  `ddf77e131e6eb6e8c096918751460470372d8cbb1e12aca9163efd4eaf3d590f`이고 브라우저
  page/console 오류는 0이다.
---

# 2026-08-10 Viewer command feature analysis

## Scope and specification

- Analyze the checked-out CamStation 2.0 implementation only; use legacy/production evidence only
  where it explains compatibility or a runtime dependency.
- Trace the complete command path: operator UI selection and form state, public HTTP API, database
  state transitions, Viewer Agent polling/claiming, local execution, result acknowledgement, and
  operator-visible history/actions.
- For every exposed command, identify its user-facing intent, required/optional inputs, validation,
  actual execution behavior, observable success result, failure/cancel behavior, and implementation
  status. Do not infer capability from a dropdown label alone.
- Distinguish source-level implementation, automated-test coverage, and live Windows/runtime proof.
  A feature is not called operational unless all dependencies required for execution are present.
- Define the target interaction contract: the user selects one currently controllable Viewer,
  chooses a clearly described supported action, supplies only inputs relevant to that action,
  confirms disruptive actions, executes it, and can see delivery, execution, success/failure, and
  retry/cancel outcomes.
- This task is analysis and documentation. Do not change Viewer command product behavior, contact a
  production Viewer, revive stale agents, or enqueue/cancel/delete live commands.

## Plan

- [x] Inventory the Viewer command UI, API modules, route registration, store schema/queries, Agent
      client, local command dispatcher, and related tests/docs.
- [x] Build a command-by-command behavior matrix with evidence for inputs, validation, execution,
      acknowledgement, cancellation, deletion, timeout/staleness, and operator feedback.
- [x] Run the narrow Go/Web/Viewer tests and safe local checks needed to establish what the source
      actually guarantees, recording any environment-dependent gaps separately.
- [x] Identify root causes behind the current ambiguity and any broken, misleading, unsafe, or
      incomplete flows; rank them by their effect on the operator's select-and-run goal.
- [x] Publish a Korean analysis document containing the current data flow, supported capability
      matrix, verified versus unverified status, target UX/API contract, phased implementation plan,
      and independently reproducible evidence paths.
- [x] Validate document links/evidence, `git diff --check`, and final worktree scope, then complete
      this section's review without claiming an implementation change.

## Acceptance criteria

- [x] Every command visible to the user is mapped to concrete server and Viewer code or explicitly
      classified as unsupported/unimplemented.
- [x] The analysis answers whether a selected Viewer can currently receive and execute each action,
      and states the exact prerequisites and proof level behind that answer.
- [x] Operator-visible defects are tied to root causes rather than only screenshot observations.
- [x] The desired select-action-execute-result flow is specified precisely enough to implement and
      test without another discovery pass.
- [x] No command is sent and no external Viewer/server state is changed during analysis.

## Review

- Published `docs/2026-08-10_viewer-command-feature-analysis.md` with the current path, screenshot
  interpretation, command matrix, root causes, target contract, implementation priorities, and
  completion criteria.
- Confirmed that the five UI commands are not operational in the current standard MSI: three are
  ignored by Viewer Service, while the other two are queued but discarded by Electron's
  request-response-only management client. The current Service also has no server result reporter.
- Distinguished the older, implemented Viewer Agent/Host command path from the current packaged
  Service/direct-Electron architecture; old source and stale status prose are not runtime proof for
  the current MSI.
- Verified existing suites: 55 Web tests, 35 Viewer tests, and focused Go tests for `internal/store`,
  `cmd/camstationd`, `internal/vieweragent`, and `internal/viewerservice` all passed. These suites do
  not contain a current-architecture end-to-end command test.
- All relative document links resolve and `git diff --check` passed. No local daemon was running,
  no command was sent, and no external Viewer/server state was changed.

---

# 2026-08-10 Viewer original control architecture reconciliation

## Scope and specification

- Treat the operator's requirement as normative: the server must monitor and remotely control a
  Viewer because routine recovery cannot require direct access to the Viewer PC.
- Search the working tree and complete Git history, including deleted and renamed documentation,
  for the earliest Viewer feature inventory and the monitoring/control layer separation.
- Reconstruct the decision chronology rather than treating the latest MSI packaging plan as the
  entire product contract.
- Reconcile the recovered intent with the current server, Viewer Service, Electron Viewer, UI, and
  the prior analysis document. Do not restore code or choose a new architecture without evidence.
- This remains a read-only product/architecture analysis except for correcting documentation and
  task lessons. Do not send, cancel, delete, or replay a Viewer command.

## Plan

- [x] Inventory all current Viewer specifications, plans, implementation-status notes, and relevant
      commit/file history for monitoring, control, Agent, Service, watchdog, and remote recovery.
- [x] Identify the earliest authoritative feature statement and build a dated decision chronology,
      including any superseding documents and whether they intentionally removed remote control.
- [x] Map the intended monitoring plane and control plane to concrete server and Windows components,
      including lifecycle ownership, command/result flow, and failure recovery boundaries.
- [x] Compare the recovered product contract with the current implementation and classify preserved,
      migrated, broken, or accidentally dropped responsibilities.
- [x] Correct the Viewer command analysis so it preserves server-side remote-control intent while
      accurately describing the current implementation gap and the required target architecture.
- [x] Validate citations, relative links, repository diff scope, and documentation consistency, then
      record the evidence and any remaining ambiguity in this review.

## Acceptance criteria

- [x] The original or earliest recoverable Viewer feature document and its commit are identified.
- [x] Monitoring and control are described as distinct layers with explicit responsibilities.
- [x] The analysis answers whether remote-control capability was superseded intentionally or lost
      during migration, based on documentary evidence rather than inference.
- [x] The corrected target preserves server-driven recovery without requiring normal PC access.
- [x] No production/runtime Viewer state is changed.

## Review

- Identified `docs/superpowers/specs/2026-07-03-viewer-client-redesign.md` at commit `ee6879c` as the
  earliest recoverable Git-tracked Viewer 2.0 feature specification. Its approved original version
  requires server monitoring and control even when the renderer is frozen, crashed, or unresponsive.
- Checked the earlier removed May CCTV wiki and the plan-referenced `.omo/drafts` path. The wiki only
  records a Viewer nginx route, while the draft is absent from both the worktree and Git history.
- Confirmed that the 2026-07-16 Agent design strengthened the monitoring/control separation and that
  the 2026-07-18 standard MSI design retained server status and UI commands while removing general
  process supervision.
- Located the concrete execution regression in `c6ef57c`: the standard MSI conversion deleted the
  prior Electron `onCommand`, `viewer:command`, and `command_result` path without adding equivalent
  unsolicited-command handling to `ManagementConnection`.
- Corrected `docs/2026-08-10_viewer-command-feature-analysis.md` to make remote control a normative
  requirement, document the monitoring/control/lifecycle layers, preserve the full original command
  catalog, and define the missing narrow lifecycle adapter.
- Corrected the Windows Viewer section of `docs/07-implementation-status.md` so the historical
  Agent/Host implementation is not reported as the active standard-MSI runtime and the current
  command gap is explicit.
- Relative links and whitespace checks pass. Only documentation/task files changed; no Viewer
  command was sent and no local or external runtime state changed.

---

# 2026-08-10 Restore Viewer remote control and verify on WinPC

## Scope and specification

- Restore the operator-facing command set currently exposed by `/viewers`: `ping`, `reload_live`,
  `resubscribe_stream`, `restart_viewer`, and the current control-component restart semantic renamed
  from `restart_agent` to `restart_service` at the UI/API compatibility boundary.
- Preserve the recovered product contract: Viewer monitoring and command control are separate
  planes, and server-driven recovery must not require routine direct operation of the Viewer PC.
- Keep the standard MSI and direct Electron launch model. Add only the narrow lifecycle mechanism
  required for explicit, audited Viewer/service restart; do not add arbitrary shell, desktop-control,
  URL-navigation, process-launch, or file-access commands.
- Use durable command identity, bounded deadlines, exact state transitions, duplicate suppression,
  and post-restart reconciliation so delivery is not mistaken for execution.
- Verify source-level behavior locally, build the served Web UI and Windows MSI, deploy through the
  existing approved WinPC maintenance path, and prove each command through server state plus
  process/UI evidence.
- Preserve unrelated existing worktree changes and runtime evidence; do not expose WinPC endpoints,
  credentials, camera URLs, or sensitive screenshots in tracked files or command output.

## Plan

- [x] Capture the current worktree, Viewer command tests/contracts, standard MSI composition, and
      bounded WinPC access/GUI-harness readiness without changing runtime state.
- [x] Write and review a focused design plus implementation plan covering command schemas, state
      transitions, IPC events/results, lifecycle restart ownership, UI behavior, and Windows proof.
- [x] Add failing-first server/store tests for command whitelist and per-type fields, exact lifecycle
      transitions, TTL/cancel semantics, compatibility naming, and safe error handling.
- [x] Implement server/store command validation and current command delivery/result behavior with
      exact operator-visible states and no raw-secret fields.
- [x] Add failing-first Viewer Service tests for direct `ping`, UI-command dispatch/result reporting,
      duplicate suppression, Viewer restart, service restart handoff, and restart reconciliation.
- [x] Implement the Service command engine and narrow lifecycle adapters without weakening named-pipe,
      session, executable-path, or service-ownership boundaries.
- [x] Add failing-first Electron/Web tests and implement unsolicited management commands, renderer
      result return, approved live reload, targeted stream resubscribe, localized capability-aware
      UI controls, confirmations, and active-command status refresh.
- [x] Restore the Viewer Service monitoring adapter so lease/renderer timestamps and bounded
      per-stream telemetry reach the server heartbeat independently of the command engine; prove an
      offline displayed stream remains selectable for targeted resubscribe.
- [x] Run focused and full Go/Web/Viewer tests, lint/build, Windows cross-build/MSI validation, secret
      scan, `git diff --check`, and review the implementation for simpler failure-safe boundaries.
- [x] Deploy the exact verified MSI to WinPC through the approved maintenance workflow and prove all
      five commands from server creation through final result, including process/renderer evidence
      for both restart commands and continued monitoring independence.
- [x] Update implementation status, analysis/design documentation, task review, and lessons with the
      exact verified behavior and any remaining environmental limitation.

## Acceptance criteria

- [x] A user selects a registered Viewer and sees only supported Korean-named actions with relevant
      inputs; arbitrary Viewer IDs, command types, routes, and irrelevant fields are rejected.
- [x] Command UI distinguishes pending, delivered, acknowledged, running, succeeded, failed,
      rejected, expired, and cancelled and updates active commands without manual refresh.
- [x] `ping`, `reload_live`, and `resubscribe_stream` execute exactly once and return a terminal result.
- [x] `restart_viewer` succeeds only after a new Viewer process/lease and renderer-ready proof.
- [x] `restart_service` succeeds only after a new Service boot/control connection reconciles the
      original durable command; the UI does not claim success before reconnection.
- [x] Service heartbeat/control status remains independent from Viewer/renderer/stream status across
      every injected failure and restart.
- [x] Duplicate delivery, cancellation, TTL expiry, missing Viewer lease, missing interactive session,
      and restart timeout produce bounded, safe, operator-visible outcomes.
- [x] The exact locally verified artifact passes real WinPC installation and end-to-end command proof
      without asking the operator to manipulate the Viewer desktop.

## Review

- Restored the five approved commands across server validation/store transitions, Service durable
  execution, Electron/renderer IPC, fixed-target Windows lifecycle adapters, and the Korean operator
  UI. `restart_agent` remains only a compatibility alias for `restart_service`.
- Kept monitoring independent from command execution and fixed a second migration omission found
  during WinPC acceptance: Service had acknowledged then discarded `stream_telemetry`. Viewer lease,
  renderer heartbeat/progress, and bounded streams now reach server heartbeat; the renderer repeats
  current stream state during long recovery cooldowns.
- `./scripts/check-dev.sh` passed all Go packages, 58 Web tests, 36 Viewer tests, Web lint/build,
  Viewer build, embedded Web regeneration, and daemon build. Native Windows Viewer/Service tests,
  Electron packaging, WiX validation, and the standard MSI build also passed without warnings or
  errors.
- Built and installed unsigned development MSI `2.0.24` (`124436480` bytes; SHA-256
  `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160`) on the authorized Windows 11
  PC. All five commands reached `succeeded` through the normal API. Viewer restart replaced the
  entire Viewer process set and recovered lease/renderer state; Service restart changed PID and boot
  generation, recovered control, and kept the Viewer running.
- Exercised the real `/viewers` user path by selecting the registered Viewer and `제어 연결 확인`, then
  submitting with keyboard focus. The API returned HTTP 201 and the UI automatically rendered the
  terminal succeeded row with exact lifecycle timestamps.
- Used an offline disposable camera to prove both monitoring states and targeted resubscription.
  Automated tests cover duplicate, TTL, cancel, unavailable lease/session, timeout, unsafe payload,
  and restart reconciliation boundaries; a long-running fault/soak matrix remains release work.
- Removed the disposable CamStation server/database/camera, Viewer configuration, local command
  journal/history, GUI evidence directories, and temporary automation state. WinPC retains only the
  verified `2.0.24` installation and automatic Service, with the Viewer closed and configuration
  returned to an unconfigured baseline.
- The remaining deliberate boundaries are an unsigned development MSI, no long-duration Windows
  soak, server-side `restart_stream` remaining on the Streams page, and no advertised
  `capture_diagnostics` Viewer command. If Windows accepts a Service stop but cannot start it again,
  the stopped component cannot report a terminal result and external SCM recovery is required.

---

# 2026-08-10 Merge Viewer remote control into 2.0

## Scope and specification

- Commit the verified Viewer monitoring/control restoration on its feature branch without including
  ignored runtime artifacts or external WinPC evidence.
- Merge it into the active local 2.0 branch `camstation2-initial`, preserving the five newer 2.0
  commits for MSI publication, Viewer registry cleanup, and GUI verification.
- Resolve overlapping server, Web, generated-asset, status, and task-document changes by retaining
  both the newer 2.0 behavior and the verified remote-control implementation.
- Rebuild derived Web assets and run the complete repository check from the merged 2.0 worktree
  before declaring the merge complete.
- Do not push, publish a release, reinstall WinPC, or change runtime configuration as part of this
  local branch merge.

## Plan

- [x] Reconfirm both worktrees are clean apart from the reviewed feature changes and identify every
      file changed by the five newer 2.0 commits.
- [x] Commit the verified feature branch with its source, tests, generated Web output, documentation,
      checklist, and lessons.
- [x] Merge the feature commit into `camstation2-initial` with an explicit merge commit and resolve
      each conflict against both parents rather than choosing one side wholesale.
- [x] Rebuild Web/Viewer/daemon derived output as required and run `./scripts/check-dev.sh` from the
      merged 2.0 worktree.
- [x] Inspect merge ancestry, worktree status, diff summary, whitespace, and added-line secret
      patterns; document the exact merge and verification result.

## Acceptance criteria

- [x] `camstation2-initial` contains the verified Viewer command implementation and all five commits
      that preceded the merge.
- [x] The merged branch exposes the five fixed operator commands while retaining current MSI release
      download and Viewer registry behavior.
- [x] Full Go/Web/Viewer tests, lint, builds, embedded Web output, and daemon build pass on the merged
      branch.
- [x] The feature branch and 2.0 parent are both visible in merge ancestry, with no force update or
      history rewrite.
- [x] No remote push, release publication, WinPC reinstall, or runtime-state mutation occurs.

## Review

- Feature commit `1d87081` contains the reviewed Viewer control restoration and no runtime/WinPC
  evidence. Merge commit `326fbab` has exact parents `4504212` (the pre-merge 2.0 tip) and `1d87081`;
  both histories are ancestors and no force update or rewrite was used.
- Resolved the server route by retaining 2.0's offline-only Viewer deletion and MSI
  `SupportsAgentUpdate` gate together with the strict command decoder and operator allowlist.
  Retained the read-only registry and Settings MSI download surface while adding the five functional
  Viewer controls and separate monitoring telemetry.
- Rebuilt embedded Web output from the combined source. The merged hashes are
  `index-BOMrEqyB.css` and `index-fM96zdPZ.js`; no parent-side hashed asset was selected manually.
- `./scripts/check-dev.sh` passed on the merged 2.0 worktree: all Go packages, 60 Web tests, 38 Viewer
  tests, Web lint/build, Viewer build, embedded Web regeneration, and daemon build.
- Final ancestry showed `camstation2-initial` ahead of `origin/camstation2-initial` by seven commits:
  the existing five 2.0 commits, the feature commit, and the merge commit. The worktree was clean
  immediately after the merge commit.
- No push, release publication, installer deployment, WinPC access, server configuration change, or
  runtime mutation was performed during the merge.

---

# 2026-08-10 Deploy merged Viewer controls to Docker canary

## Scope and deployment specification

- Deploy the exact local `camstation2-initial` HEAD containing the merged Viewer monitoring and
  remote-control restoration to the existing isolated CamStation 2.0 Docker canary.
- Build a new immutable image tag and retain the currently running image as the immediate rollback
  target. Never overwrite an existing tag, remove images, prune Docker state, or delete volumes.
- Recreate only the canary `camstation` service. Preserve its state, media, Viewer-release mounts,
  management-network-only HTTP publication, PID limit, restart policy, and three-camera allowlist.
- Keep the legacy 1.0 services, nginx ownership, databases, recordings, camera configuration, and
  client routing untouched. This deployment is not authorization for the 1.0-to-2.0 cutover.
- Publish Viewer MSI `2.0.24` only if the previously verified artifact can be recovered and its exact
  size and SHA-256 match the recorded build. Otherwise retain the current catalog entry and report
  that limitation instead of substituting or rebuilding an unverified installer.
- On any image/config/health acceptance failure, restore the backed-up image pointer and recreate
  the prior canary service before continuing analysis.

## Plan

- [x] Record the exact Git revision, current image/ref/ID, Compose deployment directory, container
      isolation settings, three-camera/recorder state, Viewer release metadata, and legacy service
      PID/restart baseline without mutating runtime state.
- [x] Build and inspect a new immutable Docker image from the exact merged HEAD, including OCI
      revision/version labels, non-root runtime contract, entry point, health check, and embedded UI.
- [x] Transfer the image without deleting the prior image; verify the remote image ID matches the
      locally built image before changing Compose configuration.
- [x] Create a root-only `.env` rollback copy, replace exactly one `CAMSTATION_IMAGE` key atomically,
      validate rendered Compose configuration, and force-recreate only `camstation`.
- [x] Prove health, zero restart count, unchanged port/mount/resource boundaries, three streaming
      home cameras, three running recorders, clean bounded logs, and unchanged legacy services.
- [x] Verify the real `/viewers` operator surface exposes the five supported controls and no
      synthetic Viewer registration form; close the browser automation session afterward.
- [x] If the exact verified MSI is available, publish it through the persistent release catalog and
      verify version/size/hash through API and `/settings`; otherwise leave the catalog unchanged.
- [x] Update the Docker canary runbook, this review, and lessons; run repository whitespace/link/
      secret-scope checks and commit only the deployment record changes.

## Acceptance criteria

- [x] The running canary image label points to the exact merged Git revision and a new immutable tag;
      the previous image and an exact rollback configuration backup remain available.
- [x] The container is healthy with restart count zero and the same host publication, bind mounts,
      PID limit, restart policy, and positive three-camera scope as before deployment.
- [x] Public APIs report exactly three streaming home cameras and exactly three running recorders;
      recent container logs contain no panic/fatal/credential leakage indicators.
- [x] All legacy 1.0 continuity services retain their pre-deployment active state, main PID, and
      restart count.
- [x] `/viewers` renders a selectable Viewer and the five fixed commands (`ping`, `reload_live`,
      `resubscribe_stream`, `restart_viewer`, `restart_service`) with Korean operator guidance.
- [x] The Viewer release catalog either serves the exact verified `2.0.24` MSI or remains unchanged
      with the missing-artifact reason explicitly recorded; no unverified installer is published.
- [x] The deployment record contains enough evidence for deterministic rollback without recording
      credentials, camera URLs, private runtime paths, or sensitive screenshots.

## Review

- Built exact HEAD `f9f43b7bafa6157b8d3fd32562f378f060689c26` as immutable
  `camstation:2.0.0-rc.20260810.10-canary`. Local and server image IDs both equal
  `sha256:19954a0ff6a2ea89a7453ce2af0975d03e7c52f9e26cc3ca4f227e9ce8c1ccc9`; labels, non-root
  user, entry point, health check, and all five embedded control types/labels passed inspection.
- Recovered the previously proven WinPC MSI rather than rebuilding it. Its 124,436,480-byte size
  and SHA-256 `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160`
  matched the acceptance record before atomic 2.0.24 publication. Metadata and a complete HTTP
  download matched again after container recreation; all bounded staging files were removed.
- Changed exactly one `CAMSTATION_IMAGE` line after a validated root-only backup. Compose rendered
  the intended image and recreated only `camstation`; the service became healthy without rollback.
  The immediate rollback pair is retained `.9-canary` image ID
  `sha256:178b101f02488bf317ea8c447cb619adb4e151a0d943a634f35ea089ee5f28e4` and
  `.env.pre-viewer-controls-20260810-074056.bak`.
- The new container is healthy with restart 0. Its management-only port, two bind mounts and mount
  fingerprint, read-only root, UID/GID, dropped capabilities, PID 1024, and restart `no` settings
  exactly match the baseline. APIs report three enabled/streaming home cameras and three running
  recorder workers; startup logs contain zero error/fatal/panic or credential-pattern matches.
- Browser acceptance selected the real Viewer and found exactly five command choices with no
  synthetic registration form. Because that Viewer is currently offline, command execution is
  correctly disabled. `/settings` displays 2.0.24 and the matching artifact identity, while all
  three `/viewer` videos reached MSE `playing`, `readyState=4`. The browser and screenshot were
  closed and removed without issuing a command.
- All five legacy 1.0 services retained their exact baseline PIDs (`248`, `326`, `247`, `246`,
  `396`) and restart count 0 before and after deployment. A final delayed check again returned
  canary healthy/restart 0, cameras 3/3, recorders 3/3, and legacy continuity 5/5.
- One publication assertion initially expected `application/x-msi`; the implemented and tested
  route deliberately serves MSI as `application/octet-stream`. Work stopped before Docker mutation,
  then metadata, length, disposition, complete content hash, and source contract proved publication
  correct. A later `docker top -eo comm` portability assumption was replaced by a read-only in-
  container process-name check; neither diagnostic mismatch changed runtime state.

---

# 2026-08-12 운영 Docker 라이브 초기 연결 상태 진단

## 범위와 진단 사양

- 사용자가 방금 재현한 운영 Docker 2.0 라이브 화면의 최근 로그를 읽기 전용으로 조사한다.
- `연결 중` → `영상 재연결 중` → `대체 스트림 연결 중` → 재생 전이를 만드는 프런트엔드
  조건·타이머·transport fallback과 같은 시각대의 HTTP/WebSocket/go2rtc 로그를 대조한다.
- 표시 문구만 빠르게 바뀌는 정상 초기화인지, 실제 WebSocket/미디어 연결 실패와 재시도가
  있었는지 구분하고 카메라별·transport별 증거를 제시한다.
- 진단 중 운영 컨테이너, 카메라, 설정, DB, 녹화기, Viewer에는 어떤 변경·재시작·명령도
  수행하지 않는다. 재현이 필요하면 읽기 전용 상태 확인 또는 일반 라이브 접속만 사용한다.

## 계획

- [x] 현재 운영 컨테이너 identity/health와 최근 라이브 관련 로그의 UTC·KST 범위를 수집한다.
- [x] 라이브 플레이어 문구별 발생 조건, timeout, primary/fallback transport 전이를 추적한다.
- [x] 브라우저 요청 및 go2rtc 로그를 코드 상태 전이와 맞춰 실제 실패·재시도 횟수를 판정한다.
- [x] 필요한 최소 검증을 수행하고 비밀정보 없는 타임라인, 결론, 권고를 Review에 기록한다.

## 합격 기준

- [x] 각 사용자 문구가 어떤 코드 상태에서 표시되는지 확인된다.
- [x] 최근 시도에서 primary 연결이 성공했는지 실패했는지, fallback이 실제 사용됐는지 로그로
      판정된다.
- [x] 영상이 나오기까지의 지연이 정상 설계 지연인지 결함인지 근거와 함께 설명된다.
- [x] 운영 상태를 변경하지 않았고, 결과에 원시 카메라 URL·자격증명·민감 경로가 노출되지 않는다.

## Review

- 대상은 revision `f9f43b7bafa6157b8d3fd32562f378f060689c26`의
  `camstation:2.0.0-rc.20260810.10-canary`다. 컨테이너는 2026-08-10 기동 뒤
  `healthy`, restart 0을 유지했고 2026-08-12 09:05 KST 사용자 재현 및 09:11 KST 독립
  재현 시각의 컨테이너 로그에는 player/WebSocket 오류가 기록되지 않았다.
- WIN11-DELL Viewer 2.0.24는 09:04:53 KST에 Service가 정상 재시작됐고 오류 code는 없었다.
  서버에는 09:04:03~09:10:56 KST Viewer heartbeat 43건과 09:05:12.742 KST 마지막 영상
  진행이 남았다. Viewer 전용 로그는 비어 있고 화면 종료 후 streams snapshot도 비워지므로,
  원래 재현의 attempt counter 자체는 사후 로그로 복구할 수 없었다.
- 같은 운영 `/live`를 격리 브라우저로 reload해 DOM/video 상태를 50 ms 간격으로 측정했다.
  세 카메라 모두 `연결 중`에서 4.911초에 `영상 입력 재연결 중`, 9.901초에
  `대체 스트림 연결 중`으로 바뀌었고 각각 10.051초, 10.201초, 10.351초에 `playing`과
  `readyState=4`에 도달했다. 이 전이는 코드의 5초 setup timeout 두 번과 정확히 일치한다.
- 복구 순서는 initial WebRTC/live → WebRTC/live retry → MSE/live → 필요할 때만 MSE/focus다.
  재현 중 public stream status는 세 `live` output이 각각 viewer 1로 running이고 모든
  `focus` output이 viewer 0으로 idle이었다. 따라서 이번 성공은 카메라 대체 스트림이 아니라
  동일한 primary live 스트림의 MSE transport 성공이다.
- Docker는 HTTP `18080/tcp`만 host에 publish한다. 생성된 go2rtc WebRTC candidate는 bridge
  내부 주소의 `8555` 한 개이고 외부에 publish되지 않는다. 운영 문서도 same-origin
  `/player/api/ws` MSE를 정상 경로로 지정한다. 따라서 direct WebRTC가 성립하지 않는 배포에서
  UI가 WebRTC를 기본으로 두 번 시도하는 것이 약 10초 지연의 근본 원인이다.
- `fallback` phase를 무조건 `대체 스트림 연결 중`으로 번역해 transport fallback과 stream
  fallback도 혼동한다. 결론은 실제 연결 실패·재시도가 두 번 있고, 마지막 문구도 부정확하다는
  것이다. 카메라 RTSP나 컨테이너 재시작 장애로 인한 지연은 아니다.
- 격리 브라우저를 닫은 뒤 세 live viewer count가 모두 0으로 돌아왔고 컨테이너는 계속
  healthy/restart 0이었다. focused recovery/selection 테스트 17개도 통과했다. 진단 범위에서는
  소스, 컨테이너, 설정, DB, 카메라, 녹화기 및 Viewer에 변경이나 제어 명령을 수행하지 않았다.

---

# 2026-08-12 Docker WebRTC 즉시 연결 및 재생 진단 로그

## 범위와 구현 사양

- 기존 1.0과 병행 중인 Docker 2.0 카나리에 외부 도달 가능한 WebRTC TCP·UDP 경로를 추가한다.
  관리망 주소에만 bind하고, 기존 1.0의 host port 점유를 확인해 충돌 없는 카나리 포트를 쓴다.
- go2rtc가 자동 발견한 Docker bridge candidate 대신 명시적으로 검증된 외부 candidate를
  광고할 수 있게 한다. 비 Docker 실행의 기존 자동 발견 동작은 그대로 보존한다.
- 라이브 재생 attempt에 correlation ID, stream role, transport, attempt, phase, elapsed time,
  failure category, media readiness를 포함한 secret-safe 구조화 로그를 추가한다. 로그 레벨은
  `off/error/warn/info/debug`로 설정하고, 서버가 레벨을 필터링·크기 제한·rate limit한다.
- WebRTC→MSE transport 전환과 `live`→`focus` stream 전환을 서로 다른 상태로 표현한다.
  실제 두 번째 stream candidate를 사용할 때만 `대체 스트림` 문구와 badge를 표시한다.
- 소스 변경은 테스트 후 새 immutable image로 카나리만 교체한다. 기존 이미지와 root 전용
  Compose 포인터 백업을 유지하고, 실패 시 직전 카나리로 되돌린다.
- WIN11-DELL의 기존 Viewer 2.0 설치·Service·RDP 세션·저장 설정을 보존하면서 정상 사용자
  실행 경로로 첫 WebRTC 재생과 구조화 로그를 확인하고, 프로젝트 GUI evidence loop로 실제
  영상 창을 검증한다.

## 계획

- [x] 현재 host TCP/UDP 포트 점유, firewall, Docker 경계, Viewer 세션, rollback 기준선을 확인한다.
- [x] 외부 WebRTC candidate 설정과 유효성 검사를 failing-first Go 테스트로 구현한다.
- [x] playback 구조화 로그 endpoint, 레벨 필터, redaction/bounds/rate limit를 failing-first로 구현한다.
- [x] 프런트엔드 attempt 계측과 transport/stream fallback 표현 분리를 failing-first로 구현한다.
- [x] 전체 Go/Web/Viewer 테스트와 lint/build를 통과하고, 배포 전 secret scan과 diff 검증을 완료한다.
- [x] 새 immutable Docker image를 배포하고 candidate·TCP/UDP publish·health·rollback 경계를 검증한다.
- [x] 브라우저에서 세 카메라가 첫 WebRTC attempt로 즉시 재생되고 info/debug 로그가 맞는지 증명한다.
- [x] WIN11-DELL Viewer에서 같은 결과와 실제 GUI를 증명하고 evidence harness를 정리한다.
- [x] 실제 검증에서 드러난 GUI launcher 기본 worker 경로 초기화 결함을 회귀 테스트로 수정한다.
- [x] 운영 문서, Review와 lessons를 실제 결과에 맞게 갱신한다.

## 합격 기준

- [x] Docker 외부 클라이언트가 private bridge 주소를 받지 않고 관리망에서 도달 가능한 candidate를 받는다.
- [x] 세 카메라 모두 initial WebRTC attempt에서 5초 timeout 없이 재생되며 MSE fallback은 발생하지 않는다.
- [x] 기본 info 로그만으로 attempt 시작·성공/실패·transport·stream·elapsed time을 상관관계로 복원할 수 있다.
- [x] debug 로그는 signaling과 첫 미디어 진행을 보이되 URL, SDP, ICE 원문, 자격증명을 남기지 않는다.
- [x] 실제 stream candidate가 바뀌지 않으면 `대체 스트림` 문구·badge·counter가 나타나지 않는다.
- [x] 카나리 health/restart, 녹화기, 기존 1.0 서비스, Windows Viewer Service와 interactive session이 유지된다.

## Review

- 근본 원인은 Docker가 HTTP만 publish한 상태에서 go2rtc가 bridge 내부 WebRTC candidate를
  광고한 것이다. `/live`와 설치형 Viewer가 도달 불가능한 WebRTC/live를 두 번 5초씩 실제
  timeout한 뒤, 다른 카메라 stream이 아닌 같은 `live` stream의 MSE로 성공했다. 동시에 UI가
  이 transport 전환을 `대체 스트림`으로 잘못 표시했다.
- revision `dd619b5990b4f05a2b6b56a969acdffd39c97f40`은 검증된 명시 WebRTC candidate,
  secret-safe bounded playback diagnostics, transport/stream fallback 표현 분리, Docker
  TCP·UDP publish를 구현한다. native 실행은 명시 candidate가 없을 때 기존 local-interface
  자동 발견을 유지한다.
- 운영에는 immutable `camstation:2.0.0-rc.20260812.11-canary`, image ID
  `sha256:b4e5fe10099bcd167c34925ac178d2951d2ad01c120e0af77858365dcae5259a`를 배포했다.
  host `10.0.0.26:18555/tcp+udp`가 container `8555`에 매핑되고 생성 설정은 candidate
  `10.0.0.26:18555`만 포함한다. API `1984`와 RTSP `8554`는 계속 내부 전용이다.
- 운영 브라우저의 세 session은 WebRTC attempt 1에서 각각 2.917~4.352초에 재생됐고,
  WIN11-DELL Viewer의 세 session은 3.209~4.210초에 재생됐다. 두 실행 모두 retry 0,
  MSE fallback 0, stream fallback 0이며 `대체 스트림` 문구·badge도 나타나지 않았다.
- debug에서 두 실제 클라이언트의 구조화 event 36건이 attempt, socket, signaling answer,
  first track, playback start를 연결했다. URL/SDP/ICE 원문/자격증명 금지 패턴은 0이었다.
  canary는 진단을 위해 debug를 유지하며 Compose 기본값은 info다.
- Windows session 1의 실제 `CamStation 2.0` 창은 PrintWindow PNG와 17개 UIA 요소로
  검증했다. PNG SHA-256은 `7b58feed11f17db87700e70c3c21bd585beba6705bd76dea0823c2c39419b562`,
  UIA SHA-256은 `489ef95db9896e2c07b9418dc275c6c7ed2a4d5fb5df7e880660aa223bc05a6f`다.
  GUI harness의 `$PSScriptRoot` parameter-default 결함은 failing-first 테스트 후 param 이후
  초기화로 고쳤고, WorkerScript를 생략한 실제 Capture도 성공했다. 최종 scheduled task와
  worker는 0, Explorer는 정확히 session 1 하나, Viewer Service는 running이다.
- 카나리는 healthy/restart 0, camera 3/3 streaming, recorder 3/3 running이다. 세 active
  recording 파일은 같은 inode에서 모두 5초간 1,048,576 bytes 증가했다. 기존 1.0 핵심
  서비스 5개의 PID와 restart 0은 기준선 그대로이며 mount fingerprint, 권한, resource limit도
  변하지 않았다.
- 최종 `./scripts/check-dev.sh`는 전체 Go package, Web 64 tests·lint·build, Viewer 38
  tests·build와 daemon build를 통과했다. production policy, 변경 문서 상대 링크 10개,
  changed-diff secret pattern, `git diff --check`도 모두 통과했다.
- 즉시 rollback용 `.10-canary` image ID
  `sha256:19954a0ff6a2ea89a7453ce2af0975d03e7c52f9e26cc3ca4f227e9ce8c1ccc9`와 root 전용
  `.env.pre-webrtc-20260812-113427.bak`, `compose.yaml.pre-webrtc-20260812-113427.bak`을
  보존했다. 첫 재생은 5초 timeout 전 단일 attempt로 복구됐지만 on-demand public output과
  keyframe 준비 때문에 측정상 2.9~4.4초는 남는다.

---

# 2026-08-12 Docker WebRTC 192 대역 경로 교정

## 범위와 계획

- [x] 기존 검증이 10 대역 candidate에 한정됐음을 확인하고 서버의 192 대역 주소·포트 기준선을 수집한다.
- [x] 기존 WIN11-DELL 테스트 PC의 192 대역 주소와 source route를 확인한다.
- [x] Docker Compose가 10/192 HTTP bind와 단일 192 WebRTC candidate를 명시적으로 지원하도록
      failing-first policy test와 최소 변경을 구현한다.
- [x] 카나리만 재생성하고 192 대역 publish, candidate, health, rollback과 1.0 연속성을 검증한다.
- [x] WIN11-DELL의 192 인터페이스와 192 서버 주소로 첫 WebRTC attempt·구조화 로그·실제 GUI를 검증한다.
- [x] 운영 문서와 lessons를 교정하고 전체 회귀 검사·최종 커밋을 완료한다.

## 합격 기준

- [x] 192 대역 전용 클라이언트가 `192.168.0.160:18081`에 접속하고
      `192.168.0.160:18555` WebRTC candidate로 재생한다.
- [x] 테스트 PC의 실제 source address가 192 대역인 상태에서 세 카메라가 첫 WebRTC attempt로 재생된다.
- [x] 10 대역 관리 경로, Docker 보안·저장소 경계, 녹화기와 기존 1.0 서비스가 유지된다.
- [x] Windows Viewer 설정·Service·interactive session을 보존하고 GUI harness task/worker를 모두 정리한다.

## Review

- WIN11-DELL의 실제 주소는 `192.168.0.178/24`이고 source-bound probe로 서버
  `192.168.0.160:18081` health 및 18081/18555 TCP 도달성을 확인했다. 서버 `eth1`에서는
  이 source가 `192.168.0.160:18555/udp`로 만든 실제 흐름 세 개가 관측됐다.
- 카나리의 최종 publish는 HTTP `10.0.0.26:18081`·`192.168.0.160:18081`, WebRTC
  `192.168.0.160:18555/tcp+udp`이며 candidate는 192 주소 한 개다. API `1984`와 RTSP
  `8554`, 10 대역 WebRTC는 공개하지 않는다.
- 첫 배포는 BusyBox `grep` 검증기 사용 오류로 실패했으며 자동 롤백 뒤 이전 port/candidate,
  health, restart 0과 stage 정리를 증명했다. 검증기를 교정한 두 번째 배포는 성공했고 root 전용
  `.env.pre-monitor-192-20260812-125110.bak`과
  `compose.yaml.pre-monitor-192-20260812-125110.bak`을 보존했다.
- Viewer의 저장 주소만 `http://192.168.0.160:18081`로 바꾸고 ID·표시명·AutoStart=false,
  Service와 session 1을 보존했다. 기존 Viewer가 새 주소를 읽도록 `restart_viewer` 한 건을
  실행해 최종 `succeeded`를 확인했다.
- 최종 세 session은 모두 WebRTC attempt 1에서 0.712~1.102초에 재생됐고 retry, MSE,
  stream fallback은 각각 0이었다. 구조화 event 18건의 금지 URL/SDP/ICE/자격증명 패턴과
  error/fatal/panic도 0이었다.
- 실제 GUI 원본에서 영상과 카메라 control 3개, 재연결·대체 스트림 overlay 부재를 확인했다.
  최종 evidence SHA-256은 PNG
  `57918f52dd239bbc8b22905af2b8c3d14ce0aa3e95f086777044a5fe8bfc40fa`, UIA
  `489ef95db9896e2c07b9418dc275c6c7ed2a4d5fb5df7e880660aa223bc05a6f`, complete JSON
  `e8d43baaa9aff09ed1c3f1138608dcafbcad3a40979d0dfd4979eb058e59e94b`이며 capture/config
  task와 worker는 모두 0으로 정리됐다.
- 컨테이너 healthy/restart 0, 카메라 3/3 streaming, recorder 3/3 running, active 녹화 3개
  증가, UID/GID·read-only·PID/memory/CPU·mount 경계와 기존 1.0 핵심 5개 service PID/restart
  기준을 유지했다.
- `./scripts/check-dev.sh`의 Go 전 패키지, Web 64 tests·lint·build, Viewer 38 tests·build,
  production policy, exact Compose JSON render, 변경 문서 link, diff secret-pattern 및
  `git diff --check`를 통과했다. 로컬에 `pwsh`가 없어 PowerShell AST 검사는 생략했지만 같은
  helper의 원격 parser error 0과 실제 `ConfigureOnly` 성공을 확인했다.
- 지연 재점검에서도 운영 container는 healthy/restart 0이고 정확한 dual HTTP·192 WebRTC
  publish/candidate와 보안 한계를 유지했다. 테스트 PC도 source-bound 192 health 성공,
  Viewer Service running, Viewer process 4, Explorer session 1, capture/config task 0이었다.

---

# 2026-08-12 2.0 소스·클라이언트·운영 이미지 최종 반영

## 범위와 계획

- [x] 로컬 branch/commit/worktree와 GitHub 인증·remote branch를 확인하고 현재 완료 커밋을 push한다.
- [x] 게시 중인 Viewer 2.0.24 기준과 현재 HEAD 사이에서 MSI에 들어가는 runtime/package 입력만
      비교해 새 클라이언트 artifact 필요 여부를 증거로 판정한다.
- [x] Viewer package 입력이 바뀌었으면 승인된 WIN11-DELL 빌드 경로로 새 MSI를 만들고 metadata와
      artifact를 원자적으로 게시한다. 바뀌지 않았으면 기존 2.0.24를 재빌드·재업로드하지 않고 그
      이유와 기존 artifact 무결성을 기록한다.
- [x] pushed HEAD에서 새 immutable 2.0 Docker image를 clean build하고 이미지 ID·revision·tag를
      검증한 뒤 운영 서버에 적재한다.
- [x] 카나리만 새 이미지로 재생성하고 dual HTTP, 192 WebRTC candidate/TCP/UDP, health, security,
      storage, recorder, recording growth, rollback과 기존 1.0 연속성을 검증한다.
- [x] WIN11-DELL의 저장된 192 서버 주소·source-bound health·실제 첫 WebRTC 재생 로그와 프로젝트
      GUI evidence loop를 다시 검증하고 임시 task/worker를 모두 정리한다.
- [x] 운영 문서·implementation status·lessons·Review를 최종 상태로 갱신하고 회귀 검사 후 커밋·push한다.

## 합격 기준

- [x] GitHub `camstation2-initial`이 최종 로컬 HEAD와 일치하고 worktree가 깨끗하다.
- [x] 운영 2.0 container label revision이 최종 application source commit을 가리키며 immutable image
      ID와 rollback pointer가 보존된다.
- [x] 모니터링 PC는 `http://192.168.0.160:18081`을 유지하고 실제 media는
      `192.168.0.160:18555/tcp+udp`의 단일 advertised candidate로 첫 attempt에서 재생된다.
- [x] 새 MSI 게시 여부가 package input diff와 일치하고, 현재 settings metadata/download가 게시
      artifact의 크기·SHA-256과 일치한다.
- [x] container healthy/restart 0, camera 3/3, recorder 3/3, active recording growth, 보안·mount
      경계와 기존 1.0 핵심 서비스가 유지된다.

## Review

- 완료 커밋 `dd619b5`, `48adfc1`, `723dc3d`를 GitHub `camstation2-initial`에 먼저
  push했다. 게시 중인 Viewer 2.0.24 수락 revision `1d87081`과 `723dc3d` 사이의
  실제 MSI service/Electron/WiX/package/build 입력 diff는 0개였다. 따라서 내용이 같은
  MSI를 재빌드·재업로드하지 않았다.
- pushed revision `723dc3d771bd3ad42d300cd4d07e98c99369471f`의 clean worktree에서
  `camstation:2.0.0-rc.20260812.12-canary`, image ID
  `sha256:cb9409da1b9ce659f0722bd568f65131898edd52ac59f7d02338e04a380bc799`를 빌드해
  카나리만 교체했다. local/remote Compose SHA-256은
  `07952df8045df2c6527e07d33e2145c3f5a92a0e5bc53129943702f0d6ef7ba5`로 일치한다.
- 운영은 healthy/restart 0, 정확한 dual HTTP·192 WebRTC publish/candidate, non-root/read-only/
  no-new-privileges/cap-drop/resource 경계와 전용 state/media mount를 유지했다. camera 3/3,
  recorder 3/3이며 recursive active recording group 세 개가 5초 샘플에서 모두 증가했다.
  기존 1.0 핵심 서비스 5개의 PID·restart 0도 기준선과 같다.
- WIN11-DELL은 저장 주소 `http://192.168.0.160:18081`, `autoStart=false`, Service·session 1을
  유지했다. Viewer를 실제 실행한 세 session은 WebRTC attempt 1에서
  3.119~4.967초에 재생됐고 retry·MSE·stream fallback은 모두 0이었다. 서버에서
  `192.168.0.160:18555`→`192.168.0.178` UDP media를 관측했다.
- 실제 `CamStation 2.0` 창에 세 카메라 영상·녹색 상태가 보였다. 최종 PNG SHA-256은
  `f7b41ed84d07f9fbd26d6b1906a88e595186aa391f8e1df984a5299a71d03495`, UIA 17개 기록의
  SHA-256은 `489ef95db9896e2c07b9418dc275c6c7ed2a4d5fb5df7e880660aa223bc05a6f`이며
  capture task/worker는 0으로 정리됐다.
- 운영 Viewer metadata와 전체 다운로드는 기존 2.0.24, 124,436,480 bytes,
  SHA-256 `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160`을 유지했다.
  직전 `.11-canary` image와 root 전용 `.env.pre-final-2x-20260812-042406.bak`을 rollback으로 보존했다.
- `./scripts/check-dev.sh`의 Go 전 패키지, Web 64 tests·lint·build, Viewer 38 tests·build,
  daemon build가 통과했다. production policy, `.12` exact Compose JSON render, 변경 문서
  상대 링크 10개, changed-diff secret pattern과 `git diff --check`도 통과했다.

# 2026-08-13 모니터링 Viewer 영상 정지·커넥션 진단

## 범위와 계획

- [x] 기존 작업 변경을 보존하고 `monitoring-pc` 대상·대화형 세션·Viewer Service·제어 경계를 확인한다.
- [x] Viewer renderer/stream 텔레메트리에서 카메라별 상태, 마지막 프레임 진행, 정지 시간, 재연결 횟수를 수집한다.
- [x] 같은 시간대 서버 로그와 public stream별 producer/consumer 수를 secret-safe하게 수집한다.
- [x] Viewer와 서버 증거를 대조해 클라이언트 정지, 서버 producer 장애, 연결 유실을 구분한다.
- [x] 실패 테스트로 일회성 저빈도 probe가 끝난 뒤 재연결이 영구 정지하는 회귀를 고정한다.
- [x] 30초 집중 복구와 5분 cooldown은 유지하되, 프레임 진행이 돌아올 때까지 단일 저빈도 probe를 반복한다.
- [x] Web 테스트·lint·build, Viewer 테스트, Go 전체 테스트·build와 변경 diff를 검증한다.
- [ ] 배포 전후 서버 연결 수·Viewer 프레임 진행·실제 8타일 화면을 다시 검증하고 Review에 기록한다.

## 합격 기준

- 카메라별로 Viewer 재생 상태와 서버-side consumer 수가 같은 시각 기준으로 대응된다.
- `playing` 표시에 의존하지 않고 실제 media progress가 계속 증가하는지 판정한다.
- 멈춘 채널이 있으면 실패 계층과 현재 재연결 동작을 증거로 특정한다.
- 재연결은 카메라별로 격리되고 cooldown 동안 최대 5분당 한 번만 probe한다.
- 진단 중 Viewer 설정, 서버 설정, 카메라, 프로세스와 서비스 상태를 변경하지 않는다.

## Review

- 2026-08-13 10:38 KST의 세 반복 표본에서 Viewer는 `online`, renderer는 `ready`였고 8개
  카메라 모두 `lastProgressAt`이 주기적으로 최신값으로 바뀌었다. 서버는 `live` 7개에 Viewer 1개씩,
  `소방서4-focus`에 Viewer 1개를 보고했으며 recording consumer 8개는 Viewer 수에서 분리했다.
- 최근 25분 playback log에서 `소방서4-live`는 session 8개, attempt 19개, setup timeout 12개,
  playback success 0개였다. 현재 `소방서4-focus` MSE가 대신 실제 재생 중이다. 나머지 카메라는
  일시 실패 뒤 재생 성공이 있었고, 최근 30분의 non-playback error/warn은 0건이다.
- 10:41 KST exact Viewer 캡처에서 8개 영상과 `8 / 8 online`을 직접 확인했다. PNG SHA-256은
  `1ed312b747af08360edf2a2738722c1b77d31235101d6ac061260c923f73654b`이며 task/run은 정리됐다.
- 근본 결함은 30초 복구 episode 뒤 5분 저빈도 probe를 한 번만 예약한 `lowFrequencyProbeUsed`
  gate다. 마지막 probe도 실패하면 새 timer가 없어 설정 재연결이나 페이지 reload 전까지 멈췄다.
- 회귀 테스트는 새 scheduler export 부재로 먼저 실패했고, 구현 후 cooldown마다 정확히 한 probe를
  다시 예약하고 unmount/새 attempt 때 기존 timer를 취소하도록 통과했다. 정상 타일과 Viewer 전체는
  재시작하지 않는다.
- Web 65 tests·lint·build, Viewer 48 tests, 고정 Go 1.25.12 컨테이너의 `go test ./...`, daemon과
  migration build, `git diff --check`가 통과했다. 수정 커밋은 `d04100e`다.
- 깨끗한 커밋에서 immutable image
  `camstation:2.0.0-rc.20260813.13-viewer-recovery`를 빌드했다. image ID는
  `sha256:5f9eb3ef1bd4fd2760c0678b1c50b9445df7aba8a4a9a691f057d92a15692d90`이며 로컬 스모크
  컨테이너가 healthy/restart 0과 새 embedded asset을 반환한 뒤 제거됐다. 운영 배포는 아직 하지 않았다.

## 사용자 피드백 반영 — 대체 스트림 자동 원복

- [x] `focus/MSE`는 임시 대체 경로이며 정상 재생 중에도 원본 후보를 다시 확인해야 한다는 제품 계약을 확정한다.
- [x] 대체 영상은 중단하지 않은 채 1분마다 원본을 백그라운드 probe하고 WebRTC/MSE 실제 media progress를 검증한다.
- [x] 원본 progress가 확인된 경우에만 해당 타일을 원본으로 전환하고, 실패 시 대체 영상을 유지한 채 다음 probe를 예약한다.
- [x] 자동 원복·반복 실패·unmount 정리·다른 타일 격리를 실패 우선 테스트로 고정한다.
- [x] Web tests/lint/build, Viewer tests, Go tests/build와 embedded asset을 검증한다.
- [ ] 운영 배포 전후 로그·연결 수·Viewer progress와 `소방서4` 화면에서 원본 자동 원복을 확인한다.

### 자동 원복 합격 기준

- 대체 스트림의 실제 프레임 진행 중에는 원본 probe 실패가 현재 화면을 끊거나 `offline`으로 바꾸지 않는다.
- 원본은 선호 전송 방식부터 반대 전송 방식까지 각각 유한 시간만 확인하며, 실제 영상 시간이 연속 진행해야 복구로 판정한다.
- 성공 시 `activeStreamName`이 원본으로 돌아가고 `usingFallback=false`가 되며, 실패 시 최대 1분당 한 번의 probe sequence만 실행한다.
- 원본 전환 뒤 다시 실패하더라도 기존 격리 복구 순서로 대체 스트림을 재사용하고 다른 카메라 타일은 재시작하지 않는다.

### 자동 원복 구현 Review

- 화면 재생과 probe가 서로 다른 판정을 쓰지 않도록 WebRTC/MSE 연결 수명주기를
  `playbackConnection.ts`로 분리했다. probe는 DOM에 붙인 비표시 전용 video에서 원본을 열고,
  8초 유한 deadline 안에 video clock이 연속 1초 이상 진행해야만 성공한다.
- fallback 재생은 그대로 유지한 채 1분 뒤 선호 transport와 반대 transport를 순서대로 확인한다.
  둘 다 실패하면 상태·영상 연결을 바꾸지 않고 다음 1분 cycle을 예약하며, unmount/재시도/현재
  fallback stall은 AbortController로 진행 중 probe를 정리한다.
- 성공할 때만 해당 타일의 원본 후보로 전환하고 recovery episode를 새로 부여한다. 전환 연결이
  다시 실패하더라도 30초 유한 복구 순서가 원본 transport와 fallback을 다시 시도한다.
- 실패 우선 테스트는 scheduler/export 부재로 먼저 실패했다. 구현 후 Web 70 tests, lint, production
  build와 Viewer 48 tests가 통과했고, 고정 Go 1.25.12 컨테이너의 `go test ./...` 및 daemon/migration
  build가 통과했다. 새 embedded JS는 `index-CSV3hA32.js`다.
- 2026-08-13 11:32 KST 배포 전 표본에서 `소방서4-focus/MSE`의 `lastProgressAt`은 최신이었고
  `소방서4-live`는 consumer 0의 on-demand idle이었다. 따라서 배포 후 probe 시점의 일시적인 live
  consumer와 로그를 함께 관찰해야 하며, 원본이 계속 실패하면 정상적으로 fallback 화면을 유지한다.

# 2026-08-13 클라이언트·브라우저 영상 연결 지연 및 보기 전환 재연결 진단

## 범위와 계획

- [x] 현재 운영 배포 revision, 컨테이너/카메라/녹화/Viewer 상태와 기존 연결 로그를 읽기 전용으로 고정한다.
- [x] 카메라→go2rtc→recorder와 go2rtc→WebRTC/MSE 클라이언트 경로를 코드와 런타임에서 분리해 확인한다.
- [x] `/live`, `/viewer`, 설치 Viewer의 transport 우선순위와 다중↔집중 전환 시 컴포넌트·미디어 세션 수명주기를 추적한다.
- [x] 카메라별 upstream producer 준비/byte 진행, downstream consumer, 협상·첫 media 시간을 동일 조건으로 비교한다.
- [x] 격리 브라우저에서 `/live`와 `/viewer`의 다중→집중→다중 전환을 재현하고 설치 Viewer 초기 연결은 서버 telemetry와 로그로 대응시킨다.
- [x] 소방서4가 다른 카메라보다 느린 계층과 원본 `live` 실패/대체 `focus` 성공 차이를 증거로 판정한다.
- [x] 설정·카메라 상태를 변경하지 않고 결과, 외부 배포 교체, 직접 Windows 조작을 생략한 한계와 후속 수정 범위를 Review에 기록한다.

## 합격 기준

- 카메라→서버가 상시 연결인지, 녹화 consumer 때문에 유지되는지, public live/focus producer가 on-demand인지 구분된다.
- 보기 전환 때 실제로 폐기·생성되는 브라우저/Viewer 연결과 서버 consumer가 코드 및 런타임 증거로 대응된다.
- 카메라별 `요청→협상→playing→실제 video progress` 시간이 수치로 비교되고 소방서4 병목 계층이 특정된다.
- `연결됨` 상태와 실제 프레임 진행을 구분하며, 서버 미송신·클라이언트 재생·카메라 입력 문제 중 하나로 성급히 단정하지 않는다.
- 진단 중 운영 Viewer 설정, 서버 설정, 카메라, 서비스, 컨테이너를 재시작하거나 변경하지 않는다.

## Review

- 조사 시작 시 운영 구 이미지 revision `723dc3d`에서 HTTPS `/live`를 새로 열자 일반 7대는 첫
  WebRTC 시도에서 1.02~4.18초에 재생됐지만 소방서4는 `live`의 WebRTC 5초, 재시도 WebRTC 5초,
  MSE 5초를 연속 소모한 뒤 `focus/MSE`로 전환해 16.80초에 재생됐다.
- 같은 시각 소방서4 `recording` 입력은 video byte가 계속 증가했고 녹화 worker도 running이었다.
  반면 별도 `live` 입력은 RTSP producer와 audio packet만 살아 있고 video byte/packet이 반복 표본에서
  정확히 0이었다. public `live` 변환 producer는 생성되지 못했고 `focus`는 정상 video를 송신했다.
- 서버 코드는 private `live` 입력을 preload하지만 public `live/focus` 출력은 현재 모두
  `on_demand`다. 또한 runtime 정상 판정은 producer 메타데이터 존재만 보며 per-track byte 진행을
  읽지 않는다. 따라서 audio-only로 남은 부분 고장을 `running`으로 오판하고 자동 복구하지 못했다.
- 조사 중 별도 운영 작업이 컨테이너를 두 차례 재생성했다. 첫 재생성은 같은 구 이미지였지만
  소방서4 `live` video packet을 즉시 회복시켰고, 두 번째는 최신 revision `491a0f5`를 배포했다.
  이 전후 비교로 카메라 설정의 영구 오류가 아니라 장시간 살아 있던 입력 relay의 부분 정지와
  서버-side per-track watchdog 부재가 소방서4 고유 장애의 직접 원인임을 확인했다.
- 최종 revision에서 HTTPS 브라우저 8대는 첫 WebRTC 시도 1.14~3.28초, 소방서4는 2.30초였다.
  설치 Viewer의 서버 재시작 직후 cold start는 3.07~11.04초였고 일부 타일은 5초 setup timeout을
  한 번 넘겼다. 안정화 뒤 Viewer `n100`은 8대 모두 WebRTC `playing`, 최신 progress를 보고했다.
- 8개 live 출력이 모두 software H.264 변환이고 steady container CPU는 8 vCPU 제한 중
  531~602%, ffmpeg 30개(녹화 8개 포함)였다. 카메라 입력은 상시여도 on-demand 변환기의 cold start와
  동시 CPU 경쟁, 5초 setup deadline 때문에 첫 연결이 1~11초 범위로 늘어난다.
- `/live`는 선택 타일의 일반 `live`만 unmount하고 `focus`를 새로 연다. 다른 7대는 계속 진행했고
  소방서4 focus 첫 프레임은 4.27초, warm live 복귀는 1.04초였다. `/viewer`는 집중 진입 때 grid
  8개를 전부 unmount하고, 복귀 때 8개 MSE session을 모두 다시 생성했다. 복귀 첫 프레임은
  1.37~4.66초, 소방서4는 2.70초였다.
- 최종 `/viewer` 8타일 화면에서 모두 실제 영상과 녹색 상태를 확인했다. 격리 브라우저만 조작했고
  Windows 대상 alias가 요청에 명시되지 않아 Viewer PC GUI를 직접 클릭하지 않았다. 운영
  설정·카메라·서비스·컨테이너 변경은 수행하지 않았으며 위 두 컨테이너 교체는 외부 작업이었다.
- 진단용 브라우저 탭은 종료했으며 자동화 세션에 열린 탭이 0개임을 확인해 추가 viewer 부하를 제거했다.

# 2026-08-13 cooldown 전체 영상 복구 순환 보완

## 범위와 계획

- [x] 5분 cooldown 뒤 새 복구 episode가 원본 WebRTC/MSE, 대체 MSE, 격리 재구독까지 다시 순회하는 실패 우선 테스트를 추가한다.
- [x] 일회성 primary terminal probe를 제거하고, 매 cooldown마다 30초로 제한된 새 per-stream 복구 episode를 시작한다.
- [x] fallback 정상 재생 중 원본 자동 원복 probe와의 역할 분리를 명세에 반영한다.
- [x] Web tests/lint/build, Viewer tests, Go tests/build와 변경 diff를 검증한다.
- [x] immutable 이미지를 운영에 배포하고 Viewer를 갱신한 뒤 8개 스트림의 실제 progress·연결 수·로그를 반복 확인한다.

## 합격 기준

- 원본 영상이 장시간 복구되지 않아도 5분마다 해당 타일만 전체 후보를 유한하게 다시 시도한다.
- 대체 스트림이 실제 재생되면 화면을 유지하면서 1분마다 원본 복구를 별도로 검증하고 성공할 때만 원복한다.
- 실패 순환은 30초를 넘겨 빠르게 반복하지 않으며 다른 타일이나 Viewer 전체를 재시작하지 않는다.
- 서버 연결 수만으로 성공 판정하지 않고 각 영상의 `lastProgressAt` 증가를 함께 확인한다.

## Review

- 기존 cooldown timer는 `live/WebRTC`를 terminal attempt로 한 번만 열고 실패 즉시 다시 5분
  cooldown으로 들어가, 다음 episode에서 `live/MSE`, `focus/MSE`, 격리 재구독을 영구히 건너뛰었다.
  실패 우선 테스트는 `restartEpisode` 부재로 먼저 실패했고, 추가 관측 테스트는 과거
  `stallStartedAt`에 episode가 다시 묶여 즉시 cooldown으로 돌아가는 결합도 잡았다.
- cooldown wake-up은 현재 시각을 새 30초 예산 기준으로 명시하고 단계만 초기화한다. 누적
  `stalledForMs`는 보존한다. fallback이 이미 진행 중일 때는 기존 화면을 유지하는 별도 1분
  primary WebRTC/MSE probe가 실제 video-clock 진행을 확인한 뒤에만 원본으로 승격한다.
- clean commit `01d0f28`에서 Web 71 tests, lint/build, Viewer 48 tests, 고정 Go 1.25.12의 전 패키지
  테스트와 daemon/migration build가 통과했다. embedded asset은 `index-Br62lnoB.js`다.
- immutable image `camstation:2.0.0-rc.20260813.15-viewer-full-recovery`의 ID는
  `sha256:8e3793ac200f7307b965bde3edb5ae3bbd22ee54c40ce4e0f3279d2805dc5a4d`다. 로컬 스모크에서
  healthy/restart 0, non-root/read-only, revision과 새 asset을 확인했다.
- 2026-08-13 12:27 KST 운영 배포 후 compose SHA-256은
  `4e251f4ad4cef0babe0dba6b1d9a605e9e60db24f46b2fc4772f022ce9c6d9fe`다. 배포는 rollback 없이
  성공했고, 직전 `.14` compose는 root 전용
  `/opt/camstation2/docker-production/compose.pre-full-recovery-20260813T032755Z.bak`으로 보존했다.
- Viewer 명령 7 `reload_live`, 초기 cold-start 중 멈춘 한 타일씩 보낸 명령 8 `소방서1-live`,
  9 `소방서5-live` 격리 재구독은 모두 succeeded였다. Viewer 전체 프로세스나 정상 타일은
  재시작하지 않았다.
- 12:32~12:35 KST 반복 표본에서 8개 원본 `*-live`의 `lastProgressAt`이 모두 계속 전진했다.
  최종 소방서4는 대체 `focus`가 아니라 `소방서4-live/MSE`였고, 서버도 카메라별 원본 live를
  producer/consumer/viewer `1/1/1`로 보고했다. 12:31:57 KST의 마지막 WebRTC setup timeout 뒤
  추가 playback warn/error는 없었다. 녹화 worker도 8/8 running/current, error 0이다.
- 컨테이너는 image/revision 일치, healthy/restart 0, user 10001, read-only,
  no-new-privileges, cap-drop ALL과 기존 양쪽 LAN HTTP/WebRTC TCP+UDP bind를 유지했다.
  `monitoring-pc` 최종 status는 NUC/session 1 Active, ViewerService Running, renderer ready,
  제어·설정·캡처 task 0, driver TCP/firewall 0, script parity 전부 true였다.
- 최종 구간에는 모든 타일이 원본에서 재생되어 자연 발생한 fallback→primary 승격 이벤트 자체는
  관측되지 않았다. 자동 승격은 video-clock 검증 회귀 테스트로, cooldown 전체 재순환은 새 실패
  우선 테스트로 검증했다. NUC가 CPU 포화였던 직전 두 캡처 시도는 SSH timeout이어서 배포 후 새
  화면 PNG는 확보하지 못했으며, 최종 판정은 Viewer progress·서버 연결 수·playback log를 사용했다.

# 2026-08-13 모니터링 PC Viewer 1.0 제거

## 범위와 계획

- [x] `monitoring-pc`가 NUC/승인된 사용자 세션과 일치하고 제어 task·listener·firewall 잔여가 0인지 확인한다.
- [x] Viewer 1.0의 정확한 제품 등록, 버전, 실행 파일, PID/창, 자동실행 항목, 서비스·작업·바로가기와 설치 경로를 읽기 전용으로 식별한다.
- [x] 식별된 Viewer 1.0 창을 정상 종료하고, 정확한 1.0 제품 및 자동실행 자산만 제거한다.
- [x] Viewer 1.0 프로세스·창·제품 등록·자동실행·서비스·작업·바로가기·설치 파일 잔여가 0인지 검증한다.
- [x] 현재 Viewer 2.x 제품과 `CamStationViewerService`, Windows-control driver 및 활성 사용자 세션이 보존됐는지 검증한다.
- [x] 최종 status에서 일회성 task/run 잔여와 Cua listener/firewall 변화가 0인지 확인하고 Review를 기록한다.

## 합격 기준

- Viewer 1.0의 실행 및 재실행 경로가 모두 사라지고 정확한 1.0 설치 자산만 제거된다.
- Viewer 2.x 제품·서비스·설정과 CamStation Windows-control 인프라는 변경되지 않는다.
- 제거 전후 대상·세션 identity가 일치하며 제어 task, staging/run, TCP listener, firewall rule 잔여가 0이다.

## Review

- 2026-08-13 12:38~12:44 KST 읽기 전용 감사에서 Viewer 1.0은 설치 제품이나 서비스가 아니라
  `C:\Users\dyllislev\Desktop\CamViewer.exe`의 portable `CamViewer 1.0.4`로 확인됐다. 파일은
  71,284,641 bytes, SHA-256
  `3e9a8b7691224512eef50fd0940ea5b9956cd47bf85e8339e62c73f4fb05c98b`였고 실행 프로세스는 0개였다.
- 사용자 Startup 폴더에는 SHA-256
  `40f0a3948a19a3d6c10da490280f0365bb390280c9779d45566ba607d07cb3b0`인 legacy 바로가기 하나가
  남아 있었고, 1.0 전용 `AppData\Roaming\camviewer`는 reparse point 없이 159 files/23
  directories/53,815,340 bytes였다. exact executable, shortcut, profile root만 영구 삭제했다.
- 별도 사후 감사에서 CamViewer executable/process/startup/Run/task/service/profile은 모두 0이었다.
  Viewer 2.0.25는 Service `Running/Auto`, 동일 PID 2060, session 1 process 4개를 유지했고 설정 및
  auto-start SHA-256도 각각
  `da085b9b21138f1d44f7e6e6d6d059023e3e8b7a550d2a0e03843f2b247e8710`,
  `aeb98a24ada8489b65b37e93a585a12f9c4ef7290fb76a068905c63e01854ae1`로 불변이었다.
- 실제 Viewer 2.0 창에서 8개 영상과 `8 / 8 online`을 확인했다. PNG는
  `work/windows-control-evidence/monitoring-pc/viewer-20260813T034426881Z-f0c61cbd2d484231a560bbef4a1073cb/viewer-window.png`,
  SHA-256 `20e1f6b19eef4acb2d585afd9c20c2cd26f731a6eb8e55cbf1b208a2230b8c4a`다.
  최종 status는 NUC/session 1 Active, 제어·설정·캡처 task 0, Cua TCP/firewall 0,
  telemetry disabled였으며 모든 원격 run/staging은 정리됐다. 재부팅·로그오프는 수행하지 않았다.

# 2026-08-13 Viewer 카메라 수신 현황·미수신 개별 복구

## 범위와 구현 계획

- [x] Viewer가 표시해야 하는 활성 카메라와 `live`/`focus` 후보 스트림을 카메라 단위로 묶는 순수 모델을 추가한다.
- [x] 실제 영상 진행이 확인된 최신 telemetry만 `수신 중`으로 판정하고, 과거 후보 스트림 행은 현재 상태로 오인하지 않게 한다.
- [x] Viewer 레지스트리에 `수신 중/전체` 카운트와 미수신 카메라 이름을 함께 표시한다.
- [x] 선택한 Viewer의 원격 제어 영역에 카메라별 수신 상태를 표시하고, 미수신 항목에서 해당 카메라만 `resubscribe_stream`으로 다시 수신시킨다.
- [x] 기존 재연결 선택 목록을 카메라 단위로 정리하고 상태·사람용 카메라 이름을 함께 표시한다.
- [x] 판정 모델의 정상·부분 장애·fallback·오래된 telemetry·Viewer 오프라인 회귀 테스트를 추가한다.
- [x] Web tests/lint/build, Go tests/build, embedded asset과 diff hygiene를 검증한다.

## 합격 기준

- Viewer 목록의 각 행에서 `8/8`, `7/8`, `1/8` 형식으로 활성 카메라 실제 수신 수를 즉시 확인할 수 있다.
- 일부 카메라만 미수신이면 같은 행과 하단 원격 제어에서 카메라 이름이 명시되어 숫자만 보고 추측할 필요가 없다.
- 원격 제어의 `다시 수신`은 미수신 카메라 한 대의 기본 live 후보만 대상으로 하며 정상 카메라와 Viewer 전체를 재시작하지 않는다.
- live 대신 focus fallback이 실제 진행 중이면 해당 카메라는 정상 수신으로 계산한다.
- `playing` 문자열만 남은 오래된 telemetry나 영상 진행 시각이 없는 행은 정상 수신으로 계산하지 않는다.
- Viewer 제어 채널·Viewer 프로세스·renderer가 재연결 가능 상태가 아니면 기존 안전 사유를 보여 주고 실행을 막는다.

## Review

- 활성 카메라를 분모로 삼고 `live`/`focus`를 카메라 한 대의 후보로 묶었다. Viewer와 renderer가
  준비된 상태에서 최신 후보가 `playing`이며 최근 15초 안에 telemetry와 실제 영상 진행을 함께
  보고한 경우만 수신 중으로 센다. 더 오래된 후보의 과거 `playing` 행은 현재 상태를 가리지 않는다.
- Viewer 레지스트리는 `7/8` 같은 카운트와 `미수신: 소방서5`처럼 이름을 함께 표시한다. 원격 제어에는
  활성 카메라별 상태 카드가 생기며 미수신 카드에만 `이 카메라 다시 수신`을 제공한다. 이 작업은
  기본 live 후보 하나에 기존 `resubscribe_stream` 명령만 보내고 정상 타일이나 Viewer 전체는 재시작하지 않는다.
- 아직 telemetry를 한 번도 내지 못한 카메라도 복구할 수 있도록 서버 명령 경계를 Viewer의 과거 보고가
  아닌 현재 활성 카메라 설정으로 바꿨다. 안정 stream과 live/focus 출력만 허용하고 recording 출력,
  비활성 카메라, 알 수 없는 stream은 거부한다. 진행 중인 동일 재수신 명령도 UI에서 중복 전송하지 않는다.
- Web 78 tests, `oxlint`, TypeScript/Vite production build와 고정 Go 1.25.12 전체 패키지 테스트가
  통과했고 최종 embedded asset은 `index-CEviY5hP.js`다. 해당 asset을 포함한 `camstationd` 빌드와
  `git diff --check`도 통과했다.
- 격리한 브라우저 fixture를 2048px와 1366px viewport에서 확인해 `7/8`, 미수신 카메라명, 8개 상태
  카드, 미수신 한 대에만 활성화된 재수신 버튼, 미수신 우선 카메라 선택 목록을 확인했고 콘솔 오류는
  없었다. 검증 브라우저와 개발 서버는 종료했으며 운영 배포·서비스 제어·실제 재수신 명령은 수행하지 않았다.

# 2026-08-13 Viewer 카메라 수신 현황 main 머지·운영 반영

## 범위와 실행 계획

- [x] 기능 브랜치의 변경 범위와 생성 Web asset을 다시 검증하고 하나의 기능 커밋으로 고정한다.
- [x] 현재 운영 revision `3256be4`의 always-hot 영상 동작을 통합해 이번 배포가 운영 기능을
  되돌리지 않도록 하고, 통합 상태에서 회귀 검증을 다시 수행한다.
- [x] 원격 `main`이 작업 기준점에서 이동하지 않았는지 fetch 후 확인하고, 전용 main worktree에서
  `--ff-only`로 머지해 원격에 게시한다.
- [x] 배포 직전 운영 컨테이너의 image/revision/health/restart, Compose 위치·hash, 카메라·녹화·Viewer
  진행 상태를 허용 필드만으로 고정하고 즉시 복귀할 이전 immutable image를 확인한다.
- [x] 깨끗한 `main`에서 새 immutable Docker image를 빌드하고 로컬 smoke에서 health, revision,
  embedded Web asset, non-root/read-only 보안 경계를 검증한다.
- [x] 운영 Compose를 root 전용 timestamp 백업으로 보존하고 image 참조 한 곳만 새 태그로 바꾼 뒤,
  Compose 검증을 통과한 경우에만 CamStation 컨테이너 하나를 재생성한다.
- [x] 배포 후 container health/restart/revision/image, HTTP health, 활성 카메라 8대, 녹화 worker 8대,
  Viewer 서비스·renderer와 카메라별 실제 `lastProgressAt` 증가를 반복 검증한다.
- [x] 브라우저에서 실제 `/viewers`의 `8/8`·카메라별 카드와 재수신 동작 노출을 확인한다. 명시된
  Windows 대상 alias가 없으므로 PC GUI는 조작하지 않고 설치 Viewer telemetry의 8개 진행으로 검증한다.
- [x] 결과와 rollback 자산을 Review에 기록하고 배포 기록만 별도 커밋으로 main에 게시한다.

## 합격 기준과 중단·복귀 조건

- 머지는 force 없이 fast-forward이며 원격 main은 검증한 기능 커밋을 가리킨다.
- 운영은 새 immutable image와 정확한 source revision을 보고하고, container는 healthy/restart 0,
  기존 non-root/read-only/network/mount 경계를 유지한다.
- 활성 카메라와 녹화 worker가 모두 8/8이고, 설치 Viewer에서 8개 카메라의 실제 영상 진행 시각이
  최소 두 표본 사이에서 증가한다.
- `/viewers`는 운영 데이터로 `8/8`을 표시하고 8개 카메라 상태를 이름과 함께 보여 준다.
- 카메라·녹화·Viewer·HTTP health 또는 보안 경계가 유한 대기 뒤 회복되지 않으면 새 컨테이너를
  계속 두지 않고 보존한 직전 immutable image/Compose로 즉시 복귀한 뒤 상태를 다시 검증한다.
- 검증 중 실제 재수신 버튼은 누르지 않고, 정상 Viewer 전체 재시작이나 카메라 설정 변경도 하지 않는다.

## Review

- 기능은 `5ab52f2`로 고정했다. 배포 전 운영이 `main`보다 앞선 always-hot revision `3256be4`를
  사용 중인 사실을 확인해 해당 동작을 먼저 통합했고, merge `e4411cd`에서 Go 전체 패키지와 focused
  race, Web 78 tests·lint·build, Viewer 48 tests·build, daemon/migrator build 및 `git diff --check`를
  다시 통과했다. 원격 `main`이 기준 `902cec6`에서 움직이지 않은 것을 재확인한 뒤 force 없이
  `e4411cd`까지 fast-forward해 게시했다.
- 깨끗한 main으로 `camstation:2.0.0-rc.20260813.17-viewer-reception`을 빌드했다. image ID는
  `sha256:652b549d44a5ff9240ecf8bf0f6dd91e04680544d9078d33ca87dfa0078e9f94`, revision label은
  `e4411cd31bda3ef71e301bd04a187c528ac27eec`이며 로컬 smoke에서 health, embedded
  `index-CgKK_jIg.js`, user `10001:10001`, read-only, cap-drop ALL, no-new-privileges를 확인했다.
- 2026-08-13 16:18 KST 운영 Compose를 root:root/0600
  `compose.pre-viewer-reception-20260813T071841Z.yaml`로 보존하고 image 참조 한 곳만 교체했다.
  배포 전 Compose SHA-256은 `d92e2db337848b38bd61dc23dffcba952a3b94d847692b3be469943888cea955`,
  배포 후는 `b4028d10d1b6468f8b649ffbc380c5b4cad941df569f96bbbdcca6eae6f34057`다. 직전
  `camstation:2.0.0-rc.20260813.16-always-hot` image와 백업을 그대로 유지해 즉시 복귀할 수 있다.
- 최종 컨테이너는 정확한 새 image/revision, healthy/restart 0, user `10001:10001`, read-only,
  cap-drop ALL, no-new-privileges이고 기존 양쪽 LAN의 HTTP 18080 및 WebRTC TCP/UDP 18555 bind를
  유지했다. 최근 10분 운영 로그 67줄에서 error/panic/fatal 일치는 0이었다.
- 공개 API는 health ok, 활성/streaming 카메라 8/8, 녹화 enabled 및 worker running/current 8/8,
  recorder error 0, stream mediaReady 및 expected/ready live 8/8을 보고했다. 설치 Viewer `n100`도
  online, agent/control online, Viewer running, renderer ready였고 12초 간격의 두 표본에서 카메라
  8대 모두 `lastProgressAt`이 증가했다.
- 재기동 직후 실제 `/viewers`에서 5/8과 `미수신: 소방서1, 소방서3, 소방서4`, 이어 6/8과
  `미수신: 소방서1, 소방서4`를 확인했다. 상세 8개 카드 중 미수신 두 대에만 `이 카메라 다시 수신`
  버튼이 나타났다. 자동 회복 뒤에는 레지스트리와 상세가 모두 8/8, 8개 카드가 `수신 중`, 재수신
  버튼 0개였고 브라우저 오류도 없었다. 검증 중 명령 버튼은 누르지 않았다. Windows 제어 skill의
  명시적 대상 규칙에 따라 alias가 주어지지 않은 PC GUI는 조작하지 않았고 격리 브라우저는 종료했다.
# 2026-08-13 Always-hot 영상 파이프라인 로컬 개발

## 작업 격리

- [x] 기존 `/workspace/CamStation`의 다른 작업 변경을 확인하고 수정하지 않는다.
- [x] 기준 commit `491a0f5`에서 branch `feature/always-hot-video`, worktree
  `/workspace/CamStation-worktrees/always-hot-video`를 생성한다.
- [x] 운영 서버·운영 Docker·운영 DB·카메라 설정은 읽거나 변경하지 않는 경계를 확정한다.

## 구현과 검증

- [x] 상세 설계와 실행 계획을 `docs/superpowers/specs/`, `docs/superpowers/plans/`에 작성한다.
- [x] 서버 소유 live warm consumer와 신규/기존 정책 정규화를 실패 우선 테스트로 구현한다.
- [x] legacy/canary import plan과 staged DB의 live activation 정규화 계약을 일치시킨다.
- [x] `/live`와 `/viewer` 집중보기에서 playback component/session을 유지한다.
- [x] Web/Go 전체 검사와 build를 통과한다.
- [x] 격리 로컬 Docker에 반영하고 health/API/media/runtime 경계를 확인한다.
- [x] 브라우저에서 실제 연결 시간과 다중↔집중 전환 세션 유지 증거를 수집한다.
- [x] `mediaReady`는 활성 카메라 8대의 public live producer와 서버 소유 warm consumer가 viewer
      0명인 상태에서 모두 준비됐을 때만 참으로 판정한다. 단순 daemon health와 혼동하지 않는다.
- [x] 서버 준비 완료 이후의 클라이언트 접속 시간을 별도로 측정하고, 준비 중 발생한 시간을
      클라이언트 연결 지연으로 보고하지 않는다.
- [x] 정적 preload의 순차/실패 동작을 확인하고, 활성 live 8개가 클라이언트 요청과 무관하게
      병렬로 시작·지속·복구되는 계약을 검증한다. `언젠가 8/8`만으로 합격 처리하지 않는다.
- [x] 서버 소유 FFmpeg null consumer manager를 실패 우선 테스트로 구현하고, 정적 live preload와
      API preload 복구 watchdog을 제거한다.
- [x] `mediaReady`, expected/ready live 수를 공개 stream status에 추가하고 viewer 0명 상태의 8/8을
      Docker에서 증명한다.
- [x] 카메라→서버 cold start 및 private relay watchdog 변경은 이번 서버→클라이언트 지연 범위에서
      제거한다. 합격 판단은 warm 완료 후 브라우저 첫 영상과 집중↔다중 세션 연속성으로만 한다.

## Review

- 설계 재점검으로 정적 go2rtc preload는 폐기 대상으로 판정했다. v1.9.14는 시작 후 config map을
  순차 순회하며 AddPreload가 동기식 AddConsumer 완료를 기다리므로 private+public 15개를 넣은 현재
  후보는 느린 항목이 뒤 항목 시작을 막는다. 로컬 10 CPU에서 throttling 0인 상태에서도 8대 준비가
  85.5초 걸린 결과와 일치한다.
- 최종 구현은 public live 1개/카메라의 서버 소유 video-copy/null consumer를 독립 관리한다.
  브라우저를 열기 전에 viewer 0명, public live producer 8개, warm consumer 8개,
  `mediaReady=8/8`을 확인했다. 소방서4 warm consumer 단일 종료 시험은 다른 7개 producer와
  동일 go2rtc PID를 유지한 채 해당 worker만 10.465초에 교체했다.
- 이미 준비된 서버에 새 소비자가 붙은 뒤 첫 `playing`은 일반 브라우저 `/live`에서 소방서4
  0.945초, 전체 0.331~1.536초였고, 별도 웹 `/viewer`에서는 소방서4 0.683초, 전체
  0.208~1.550초였다. Windows 클라이언트가 실제 여는 `/live?viewer=1`은 소방서4 1.395초,
  전체 0.860~2.217초였다. 세 화면 모두 집중→다중 전환 전·중·후 video DOM 8개가 동일했고,
  video 추가/제거·abort·emptied·error가 없었으며 server viewer count는 8로 유지됐다.
- Go 전체 패키지와 focused race test, Web 70 tests·lint·build, daemon build,
  `git diff --check`가 통과했다. image `sha256:4a9d83b3226e7275fd99e2cadc3df006688eec908411c22a6a1058312015738f`를
  전용 `camstation-always-hot-dev` 컨테이너에 반영했다. HTTP 28080/WebRTC 28555, 임시 DB·media,
  10 CPU·6 GiB 경계를 유지했고 운영 서버는 수정하지 않았다.
- 개발 접속 주소는 LAN `http://192.168.0.154:28080`, host network
  `http://10.0.0.16:28080`이며 `/live`와 `/viewer` 모두 HTTP 200을 확인했다. 검증 탭을 모두
  닫은 뒤 viewer 0, warm-only 8/8, container healthy 상태로 인계한다.

---
# 2026-09-01 106 CT Omada 및 소방서 CCTV 연결 복구

## 복구 사양과 안전 경계

- 사용자 추가 지시에 따라 `proxmox-nuc`의 CT 106과 내부 Omada 서버 기동만 범위로 한다.
- CT와 서비스 상태를 먼저 확인하고, 정확히 내려간 Omada unit만 시작한다.
- CCTV 복귀 확인은 사용자가 수행한다. CamStation 서버/Viewer, 카메라 설정, 다른 VM/CT는 변경하지 않는다.
- 관리 credential, session token, camera URL, 장비 주소 세부값은 출력·문서화하지 않는다.

## 계획

- [x] `proxmox-nuc`에서 CT 106과 내부 Omada/MongoDB service 상태를 확인한다.
- [x] failed 상태인 정확한 Omada unit만 시작한다.
- [x] CT running, Omada/MongoDB active와 관리/discovery port listen, HTTPS 응답을 확인한다.
- [x] 사용자 지시에 따라 CCTV/Viewer 추가 조작 없이 종료하고 임시 브라우저 도구를 정리한다.

## Review

- CT 106(`omada`)은 이미 running/onboot였고 `mongod.service`도 active였다. Omada 본체인
  `tpeap.service`만 failed였으며 관련 관리 포트가 열리지 않은 상태였다.
- `systemctl start tpeap.service`로 해당 unit만 시작했다. 결과는 `Result=success`, `ActiveState=active`,
  `SubState=running`, main status 0이다.
- 최종 확인에서 CT 106 running, MongoDB/Omada active, Omada 관리·discovery 포트 listen과 로컬 HTTPS
  200 응답을 확인했다. 다른 VM/CT, CamStation, Viewer, 카메라 설정은 변경하지 않았다.
- 웹 조작으로 전환하기 전에 사용자가 scope를 좁혀 browser session은 열지 않았다. 스킬 bootstrap으로
  잠시 설치했던 `agent-browser`는 미사용 상태에서 즉시 제거했다. CCTV 영상 복귀 확인은 사용자에게 남겼다.

# 2026-09-01 monitoring-pc CCTV 화면 미출력 진단

## 진단 사양과 안전 경계

- 대상은 승인된 `monitoring-pc`의 현재 CamStation Viewer와 운영 CamStation 서버다.
- 먼저 읽기 전용 상태와 exact Viewer 창을 확인한다. 화면이 실제로 멈췄는지, Viewer 계측만 stale인지,
  서버 ingest/stream 자체가 불량인지 분리한다.
- 카메라 설정, Viewer 설정, 서비스·프로세스, 서버 container는 사용자의 별도 수정 승인 없이 변경하지 않는다.
- URL·credential·process 전체 argument·clipboard·UI edit value는 수집하거나 출력하지 않는다.

## 계획

- [x] Windows target preflight에서 machine/user/session, driver, task/listener/firewall, script parity를 확인한다.
- [x] Viewer exact-window 캡처를 직접 판독해 창 존재 여부와 각 영상 타일의 실제 렌더링 상태를 확인한다.
- [x] 운영 서버의 camera/stream/recorder/Viewer 상태를 읽기 전용으로 교차 검증한다.
- [x] 원인 범위, 현재 영향, 안전한 다음 조치와 control residue 0을 Review에 기록한다.

## Review

- `monitoring-pc` NUC session 1은 Active, Viewer Service Running이며 driver telemetry off, TCP/firewall 0,
  control/setup/capture/config task 0, canonical script parity 전부 true였다.
- 2026-09-01 14:27·14:30 KST exact Viewer 창에서 `집-마당`, `집-창고1`, `집-창고2`는 실제 프레임과
  카메라 시각이 전진했다. `소방서1`, `소방서3`, `소방서4`, `소방서5`, `염소장`은 검은 타일과
  자동 복구 한도 도달 상태였고 두 캡처 사이에도 복구되지 않았다.
- 서버 watcher 14:30:53 KST는 container healthy/restart 0, Viewer healthy 1/1과 heartbeat fresh를
  유지하면서 camera/stream/Viewer media가 3/8이라고 기록했다. 장애 5대의 마지막 media progress는
  약 13:58 KST이고 이후 live-warm 및 recorder가 timeout/종료 후 재시도를 반복한다.
- 서버·Viewer 공통 장애나 Viewer 정지가 아니라 같은 시각에 끊긴 원격 카메라 입력 5대의 공통
  전원/현장 네트워크/상위 회선 문제 가능성이 가장 높다. 인증 실패 증거는 없었다. 읽기 전용 진단만
  수행했으며 최종 Windows control residue는 0이다.

---

# 2026-09-01 Forgejo + OpenShip production 배포 전환

## 사양과 안전 경계

- Forgejo `dyllislev/CamStation`을 단일 주 저장소로 사용하고 GitHub는 Forgejo에서 나가는 단방향
  Push Mirror로만 유지한다. GitHub에서 운영 build/deploy가 실행되는 경로는 최종 검증 뒤 제거한다.
- OpenShip은 Git source build, webhook, auto deploy를 사용하지 않는다. Forgejo Registry의
  `sha-<40자리 commit>` immutable image만 실행하고, Actions가 service image PATCH 후 deployment를
  명시적으로 생성한다.
- 기존 운영 container, root-owned Compose/environment, SQLite와 영구 media는 전환 전에 일관된 백업과
  exact rollback image/config를 확보한다. OpenShip 관리 Compose/DB, Forgejo DB, Zoraxy DB는 직접
  편집하지 않는다.
- credential 값은 shell 출력, Actions log, diff, 문서, commit에 남기지 않는다. 완료 증거에는 secret
  이름, API 권한 확인 결과와 존재 여부만 기록한다.
- 기존 LOC 접근 경로·운영 도메인·영구 데이터 경로를 유지한다. 실제 운영 host architecture와 volume
  ownership을 확인하기 전에는 OpenShip service를 생성하거나 기존 production container를 교체하지 않는다.

## 실행 계획

- [x] GitHub 원본, default branch, 전체 refs/LFS/submodule, 기존 workflow와 build/runtime/health/secret-name
      계약을 source와 Forgejo API에서 확인한다.
- [x] 현재 운영 host의 architecture, image/revision, container/port/domain/health, root-owned deployment
      config, SQLite/volume와 rollback 경로를 읽기 전용으로 고정한다.
- [x] SQLite online backup과 영구 파일/config backup을 타임스탬프·권한·hash가 있는 복구 자산으로 만들고
      백업 자체의 integrity를 확인한다.
- [x] recorder worker stop을 먼저 전체 fan-out하고 FFmpeg 종료 소유자를 한 곳으로 제한했다. stdin `q`와
      1초 scheduler wake를 추가하고 full/race test를 통과했다. 운영자는 이번 전환의 active partial 손실을
      명시적으로 허용했으므로 종료 파일 무결성을 hard-cutover 차단 조건에서 제외하고 새 녹화 8/8 복구를 확인했다.
- [x] production Dockerfile을 OpenShip runtime 계약에 맞게 검토·최소 수정하고 로컬 multi-stage build,
      image metadata, non-root/health/entrypoint를 검증한다.
- [x] `.forgejo/workflows/build-publish-deploy.yml`을 기본 branch push 전용, self-hosted, build lock,
      buildx, temporary Docker config, immutable tag, OpenShip PATCH/deploy/poll/log 계약으로 작성한다.
- [x] Forgejo repository/refs/default branch 상태를 공식 API로 검증하고 필요한 source·branch·tag를
      non-force push한다. 로컬 `origin`은 Forgejo, 별도 `github` remote는 원본 GitHub로 유지한다.
- [x] GitHub 운영 workflow 중복 가능성을 제거한 뒤 Forgejo → GitHub Push Mirror를 공식 API로 등록하고
      `last_error` 없음과 main/branch/tag 반영을 검증한다.
- [x] OpenShip API 외부 URL·agent·architecture·registry credential을 확인하고 production project와
      long-running service, env/secret, port/health/restart/volume/network를 공식 API로 등록한다.
- [x] Forgejo Actions Variables와 최소 권한 Secrets를 등록하고 이름/존재 여부만 재조회해 검증한다.
- [x] workflow와 deployment 문서를 commit/push하고 Runner 수신, Registry push, service image PATCH,
      deployment `ready`를 15분 gate 안에서 확인한다.
- [x] container image SHA/restart/log/internal health/external domain/SQLite/volume/worker·scheduler 동작과
      재배포 후 데이터 지속성을 검증한다.
- [x] GitHub가 mirror-only이고 배포 Actions가 실행되지 않음을 최종 확인하며 `docs/deployment.md`, 이 Review,
      `tasks/lessons.md`를 비밀값 없이 갱신한다.

## 합격/롤백 기준

- source/build gate 실패는 운영 변경 없이 중단한다. backup 또는 restore 검증 실패 시 OpenShip 전환을
  시작하지 않는다.
- OpenShip 배포가 15분 안에 `ready`가 아니거나 실제 health/recording/Viewer 계약이 회복되지 않으면
  보존한 exact image와 root-owned config로 기존 LOC deployment를 복구하고 GitHub 배포는 중단하지 않는다.
- GitHub 배포 제거는 Forgejo workflow 성공, Registry image 존재, OpenShip `ready`, 실제 내부·외부 health,
  SQLite/volume 지속성까지 모두 통과한 뒤에만 수행한다.

## Review

- Forgejo `dyllislev/CamStation` main의 exact `44fb80422d81190a303837b942322cff99d66e7a`를 Actions run 15가
  linux/amd64 image로 publish했고 OpenShip deployment `dep_aNIW8dWPeZl-peY_`가 `ready`가 됐다. 실제
  container는 exact image, healthy, restart 0이며 service build/dockerfile은 빈 값이고 두 bind mount와
  세 port publish를 유지한다.
- 최종 watcher는 status ok/alert 0, camera/live/recorder 8/8, Viewer healthy 1/1·receiving 8/8이다.
  SQLite quick check ok, FK 오류 0, current recording 8, 최근 failed 0이며 두 LOC endpoint와 외부 cctv2
  health, legacy cctv domain이 정상이다. 공식 monitoring PC exact-window에도 영상 8개가 보였고 Viewer를
  재시작하지 않았다.
- legacy와 초기 Actions 종료 구간은 운영자가 허용한 hard-cutover partial 손실이다. 녹화물은 image가 아니라
  그대로 보존된 media bind mount에 있으며 새 container는 곧바로 새 segment 8개를 만들었다. FFmpeg 8의
  60초 scheduler wait가 q/TERM 처리를 늦춘 원인을 1초 progress interval과 60초 application log throttle로
  분리했으며 full Go test와 recorder race test가 통과했다. 최종 hard cutover의 active 최대 8구간은 사용자
  지시에 따라 개별 ffprobe를 합격 gate로 검사하지 않았고 전환 구간 손실로 분류한다.
- Forgejo→GitHub mirror는 main과 119 refs가 일치하고 last_error가 비어 있다. GitHub workflow와 배포 SHA
  Actions run은 0이다. 기존 `camstation-updater.timer`는 disabled/inactive이며 root-only 사전 상태 backup
  `github-updater-disabled-20260902T071958+0900`으로 복구할 수 있다.
- 조사 기준선: Forgejo `dyllislev/CamStation`은 public/default `main`이고 로컬 `origin`은 Forgejo,
  별도 `github` remote는 원본 GitHub다. Forgejo → GitHub push mirror는 활성 상태이며 마지막 오류가
  비어 있다. 저장소 Actions와 전역 self-hosted runner가 활성 상태다.
- 운영 기준선: `cctv`는 `linux/amd64`, 기존 `camstation2` container는 healthy/restart 0이고 공개
  `https://cctv2.nuc.hmini.me/api/health`가 HTTP 200이다. exact image/revision, root-owned Compose,
  SQLite와 두 persistent bind mount를 rollback 기준으로 보존한다.
- OpenShip 외부 API는 `https://openship.loc.hmini.me/api/proxy/api`이며 LOC OpenShip control plane의
  restricted 사전 backup set과 dump checksum을 검증했다. CT rootfs PBS backup도 새로 생성·검증·보호했다. 보호된 전역 API
  경로를 확인한 뒤 registry pull credential을 지정된 이름으로 정규화하고, strict host-key 검증과
  SSH agent를 사용하는 `cctv-production` OpenShip server를 등록·검증했다. project/service adoption은
  전체 media backup 완료 뒤 수행한다.
- JAV Gallery의 Forgejo workflow, Variables/Secrets 이름과 최근 성공 run을 대조하되 project-scoped
  credential은 재사용하지 않았다. CamStation에는 Registry/API URL 관련 Variable 3개와 실제 OCI
  upload 권한을 확인한 최소 `write:package` Registry Secret 2개를 등록했다. 나머지 project/service ID
  Variable과 project-scoped OpenShip Secret은 OpenShip adoption 뒤 등록한다.
- 고정 Go builder image에서 `gofmt` 무변경과 `go test ./...` 전체 통과를 재확인했고,
  `go test -race ./internal/recorder`도 통과했다. OpenShip registry pull credential은 공식 verify API에서
  `active`/`lastError` 없음으로 확인했으며, 같은-server migration preview는 blocked service와 이동할
  volume 없이 `registry` image 재사용 대상으로 판정됐다.
- OpenShip 0.6.9의 port parser가 같은 container port/protocol의 host-IP별 binding을 마지막 값으로
  덮어쓰는 것을 설치 source에서 확인했다. 현재 운영 host에는 두 private 물리 LOC interface와 local
  Docker bridge/loopback만 있고 public·overlay IPv4가 없으므로, HTTP 1개와 WebRTC TCP/UDP 각 1개의
  explicit `0.0.0.0` publish로 정규화하고 배포 전후 두 기존 LOC endpoint를 모두 검증한다. 배포 직전
  interface inventory가 바뀌면 service sync를 중단한다.
- 최초 legacy recorder handoff 시각을 Actions build에 맞춰 추측하지 않는다. workflow를 제외한 준비
  commit A를 먼저 Forgejo에 게시해 그 exact SHA image를 보호된 bootstrap credential로 수동 build/push하고,
  recorder one-time gate와 첫 OpenShip 배포를 검증한다. 그 뒤 workflow·최종 문서 commit B를 push해 실제
  Actions 재배포와 persistent data 지속성을 증명한다. 공용 owner credential은 CI에 넣지 않는다.
