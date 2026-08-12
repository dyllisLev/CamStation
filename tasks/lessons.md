# Lessons

## 2026-08-12 — 1.x→2.0 전환에서 보존 범위와 화면 합격 기준을 별개로 확인한다

- 사용자가 “기존 데이터는 필요 없고 카메라 연결정보만 가져온다”고 말해도 최종 목표가 “현재
  화면처럼 모든 카메라를 본다”면 표시 레이아웃은 별도의 필수 제품 상태다. recordings metadata,
  일반 settings, backup mark/history, Viewer registry는 제외하되 카메라 identity/enabled/input과
  현재 승인된 layout geometry는 함께 이관한다.
- rollback을 위한 1.x DB/media 보존과 2.0 데이터 이관은 별개다. 원본 snapshot은 보존하되 2.0 DB에
  섞지 않으며, camera+layout import 후 녹화·backup·Viewer 이력이 유입되지 않았음을 명시적으로
  검증한다. 최종 acceptance는 row 수가 아니라 모니터링 PC 전체 화면에서 활성 카메라가 기존과 같은
  배치로 모두 실제 재생되는지로 판정한다.
- 모니터링 PC의 현재 1.0 프로세스 집합은 전환 전 관찰값이지 새 PC profile의 영구 불변식이 아니다.
  사용자가 clean 2.0 전환을 승인하면, profile은 여전히 machine/user/session만 식별하고 별도 전환
  runbook이 1.0 정상 종료·자동시작/제품 잔여물 제거와 Viewer 2.0 단독 실행 상태를 검증해야 한다.
- "카메라 연결정보만"은 URL 문자열만 복사한다는 뜻으로 축소하지 않는다. 카메라 identity/name,
  stable stream key, enabled 상태와 입력 정의가 하나의 연결 graph이며, 현재 화면을 유지하라는 요구가
  있으면 layout geometry는 별도의 명시적 이관 대상이다. 반대로 녹화·일반 설정·backup·Viewer
  history를 편의상 함께 복사해서는 안 된다.

## 2026-08-12 — 빠른 WinPC 제어는 정상 경로뿐 아니라 실패 복구와 결과 경계까지 정규화한다

- PowerShell 5.1에서는 JSON 배열이 원소 하나일 때 함수 경계를 지나 스칼라로 풀리고,
  `Get-Process`와 WMI 프로세스 객체의 PID 속성도 각각 `Id`와 `ProcessId`로 다르다. plan reference는
  숫자 segment를 항상 컬렉션 인덱스로 해석하고, launcher가 관리자 권한으로 경로·세션을 검증한
  뒤 worker에는 정규화한 PID만 넘겨야 한다.
- PowerShell 5.1의 success pipeline에서 `if { @() } else { @() }` 결과를 바로 대입하면 빈 배열이
  `$null`로 사라져 StrictMode의 `.Count`가 실패할 수 있다. fresh-install처럼 빈 컬렉션이 정상인
  경로는 먼저 `@()`로 초기화하고 조건문 안에서만 다시 대입하며, 실제 5.1 호스트에서 검증한다.
- 설치 파일의 전체 SHA가 고정돼도 fresh-install 추출 구조를 테스트하지 않으면 최상위 디렉터리를
  예상하지 못해 배포 현장에서 실패할 수 있다. 고정 ZIP의 정확한 root 이름, 직속 6개 파일 이름,
  하위 디렉터리 0개와 개별 해시를 함께 검사하고 그 payload 디렉터리만 원자적으로 승격한다.
- 대화형 세션 설정을 여러 Scheduled Task action으로 한꺼번에 묶으면 어떤 공급자 명령이 실패했는지
  결과 경계가 흐려진다. 특히 임시 Scheduled Task 안에서 공급자의 `autostart enable`을 실행하면
  Windows 11에서 작업 스케줄러를 재진입해 교착될 수 있다. 사용자별 `telemetry disable`만 단일
  임시 작업으로 실행하고, 고정 버전의 정상 PC에서 확인한 정확한 vendor task 정의는 관리자
  세션에서 등록·검증한 뒤 그 작업을 직접 시작한다.
- idempotent setup이 이미 `telemetry_enabled=false`인 PC에서도 공급자 명령을 다시 실행하면 NUC에서
  약 55초가 걸리고 임시 작업 정리에 추가 시간이 든다. 현재 사용자 설정이 이미 목표값이면 이
  mutation을 생략하고, 같은 실행에서 검증한 파일 보고서는 이동 뒤 다시 전부 해시하지 않고 최종
  파일 수와 함께 재사용한다. 상세 진단은 고정 경로의 선택형 progress marker로만 켠다.
- native driver stdout이 JSON으로 파싱되더라도 UIA title/value에 비정상 Unicode가 섞이면
  PowerShell 재직렬화 결과가 깨질 수 있다. driver stdout은 raw bytes로 읽어 strict UTF-8 우선,
  Windows ANSI fallback과 SHA를 기록하고, `complete.json`에는 UIA tree/value 대신 숫자·geometry·
  effect 중심의 안전한 요약만 보존한다.
- `effect=unverifiable`은 screenshot 또는 filtered assertion으로만 성공 판정한다. 단순히
  `verifyWith` 이름이 있다는 것으로 부족하며, 그 관찰 단계에 실제 screenshot이나 assertion이
  연결돼 있어야 한다.
- 실패 시 task/run만 지우면 충분하지 않다. `launch_app` 이후 중간 단계가 실패하면 창 자체가
  남을 수 있고 UWP는 WM_CLOSE 뒤 HWND를 바꾸기도 한다. disposable launch에 명시적으로
  `closeWindowOnFailure`를 설정하고 fresh UIA titlebar close, 원래 geometry와 새 HWND까지 재검사한
  뒤 `RemainingWindowIds=[]`를 요구한다.
- 매번 전체 방화벽·예약 작업 store를 열거하면 간단한 제어의 preflight가 느려진다. 정상 경로는
  COM task 조회, netstat, 고정 firewall registry 범위로 빠르게 검사하고, 전체 ActiveStore 감사는
  명시적인 `FullAudit` 모드로 분리하되 둘의 결과가 같은지 실제 PC에서 주기적으로 대조한다.

## 2026-08-12 — 동일한 WinPC 제어 경로를 Viewer와 범용 조작으로 분리하지 않는다

- Viewer 캡처와 일반 PC 조작은 모두 pinned SSH, active Explorer session, InteractiveToken task,
  interactive driver, observe/act/verify, cleanup이라는 같은 기반을 쓴다. 트리거만 다른 스킬 두
  개로 나누면 세션·보안·실패 처리 규칙이 갈라지고 단순 조작 때 하네스를 다시 발명하게 된다.
- 기존 프로젝트 스킬을 범용 WinPC 제어 스킬로 승격하고 Viewer exact-window 검증은 그 안의
  제한된 모드로 유지한다. 설치, 진단, 화면 조회, 입력, 창 제어와 Viewer 증거 수집은 같은
  launcher/worker 경계를 재사용해야 한다.
- 단순 PC 조작에 51분이 걸렸다면 개별 명령을 더 잘 외우는 것이 해결책이 아니다. 반복되는
  예약 작업·JSON quoting·UTF-8·Session 0/1·결과 회수·정리를 결정론적 batch runner로 숨기고,
  agent는 짧은 plan과 사후 화면 판정에만 집중하게 만든다.

## 2026-08-12 — Windows computer-use는 세션 경계와 사후 화면을 함께 검증한다

- 테스트 PC 전 권한 승인은 대상 VM 안의 정상 조작 범위를 넓혀 주지만, 외부 listener·방화벽
  규칙·저장 자격증명이나 보호 해제를 자동으로 허용한다는 뜻은 아니다. host-key 고정 SSH와
  로그인된 대화형 세션의 local named pipe를 유지하고, 일반 조작에는 driver의 `standard` 모드를
  사용한다.
- SSH 유지보수 계정과 대화형 사용자가 다르면 사용자별 named pipe ACL을 약화하지 않는다.
  공급자 daemon은 정확한 interactive session에서 실행하고, SSH 쪽 명령은 사용자·세션을 고정한
  일회성 `InteractiveToken` 작업으로 전달한 뒤 반드시 삭제한다.
- Windows PowerShell 5.1은 여러 필드가 있는 positional JSON 인수의 field-name 따옴표를 제거할 수
  있다. `cua-driver call` JSON은 stdin으로 전달하고, 명령 경계를 넘길 때는 identity/session/exit
  code를 같이 회수한다.
- native driver가 UTF-8 JSON 안에 한글 창 제목을 반환할 때 Windows PowerShell 5.1의 기본 console
  encoding으로 받으면 문자열과 JSON closing quote가 손상될 수 있다. 호출 worker의
  `Console.InputEncoding`, `Console.OutputEncoding`, `$OutputEncoding`을 UTF-8로 먼저 고정한다.
- driver의 `effect=unverifiable`과 프로세스 종료 코드 0은 실제 조작 성공이 아니다. 이번에는
  desktop-scope `Win+R`과 문자 입력이 0을 반환했지만 화면에는 아무 변화가 없었다. 클릭·키·창
  전환 뒤에는 새 UIA snapshot 또는 실제 screenshot을 반드시 취하고, 검증된 실패일 때만
  foreground나 다른 정상 Windows 경로로 올린다.
- UIA의 `max_elements`는 반환 노드 수만 제한할 뿐 탐색 순서나 `value` 속성 수집을 제목 표시줄로
  제한하지 않는다. 기존 문서가 열릴 수 있는 앱에서는 `get_window_state`를 안전한 title-bar
  조회로 가정하지 말고 `list_windows`나 정확한 창 캡처를 우선한다. 예상 밖의 값이 반환되면
  즉시 조회를 멈추고 산출물을 삭제한다.
- RDP 화면 크기는 reconnect 이벤트 없이도 동적으로 바뀔 수 있다. 과거 캡처의 해상도나 창
  좌표를 재사용하지 말고 매 동작 직전에 현재 화면·창 geometry를 다시 읽는다.
- mutable 설치 스크립트를 바로 실행하지 않는다. 공식 릴리스 버전과 전체 archive SHA-256을
  고정하고 설치 후 개별 파일 해시를 다시 확인한다. Authenticode가 `NotSigned`이면 공식 archive
  해시 일치와 별개로 그 제한을 사용자에게 명시한다.

## 2026-08-12 — WinPC 유지보수 제어와 Viewer 제품 명령을 구분한다

- 사용자가 WinPC 제어·캡처 문맥에서 창 최대화나 전체화면 제어를 묻는 경우, 서버의 Viewer
  원격 명령 기능으로 해석하지 않는다. Windows 대화형 세션의 검증된 창 핸들과 제한된 UI
  Automation 조작으로 가능한지 먼저 판단한다.
- `SW_MAXIMIZE`/`SW_RESTORE`로 다루는 Windows 최대화와 Electron 애플리케이션의 전체화면은
  별개다. 전체화면은 검증된 Viewer 창 안의 명시적 전체화면 컨트롤을 제한적으로 호출한다.
- 캡처 절차가 무조건 `SW_RESTORE`를 호출하면 관찰하려는 최대화 상태를 훼손한다. 창 상태를
  제어·검증할 때는 목표 모드를 설정한 뒤 그 상태를 보존하는 캡처 경로와 사후 화면 증거를 쓴다.
- 최대화처럼 명백한 일회성 PC 조작을 Viewer 제품 기능이나 영구적인 캡처 도구 기능 개발로
  확대하지 않는다. 기존 대화형 세션에서 검증된 한 창에 Windows 명령 한 번만 적용하고 정리한다.

## 2026-08-12 — 최종 반영은 실제 package·runtime 경계로 판정한다

- Viewer 관련 파일이 바뀌었다는 이유만으로 MSI 버전을 올리지 않는다. 수락한 게시 revision과
  현재 HEAD 사이에서 설치형 서비스, Electron runtime/assets, package lock, WiX 입력,
  MSI build/manifest script만 명시적으로 비교한다. 이 입력이 0개이면 검증 도구·문서 변경 때문에
  동일한 artifact를 재빌드·재업로드하지 말고, 운영 metadata와 전체 다운로드 해시를 다시
  검증한다.
- Windows Viewer의 `Agent/Control=online`과 `Viewer=closed/Renderer=not_ready`는 서버 연결 장애로
  단정할 수 없다. 특히 `autoStart=false`에서는 서비스만 재연결되고 대화형 Viewer 창은 닫혀 있는
  것이 정상이다. 저장 주소, source-bound health, 실제 process, renderer telemetry를 나눠서 확인한다.
- Active recording 연속성 검사는 temp root 바로 아래에 파일이 있다고 가정하지 않고 실제
  nested stream 디렉터리를 recursive하게 집계한다. segment rollover 중에는 inode·파일명이 바뀐 수
  있으므로 각 stream group의 총 byte 증가를 보고, 검사기 가정 실패와 녹화 실패를 구분한다.
- 여러 shell/SSH 경계를 건너는 복잡한 regex·JSON 검증은 인라인 quoting에 의존하지 말고
  heredoc parser나 구조화된 inspect JSON을 사용한다. assertion이 실패하면 즉시 mutation을 멈추고
  출력 필드의 실제 계약을 최소 요약으로 확인한 뒤 검사기를 교정한다.

## 2026-08-12 — 노출된 media transport와 플레이어 기본 경로를 일치시킨다

- HTTP-only Docker 배포에서 go2rtc WebRTC candidate가 bridge 내부 주소뿐이면 WebRTC는
  signaling 성공 여부와 무관하게 재생 경로가 될 수 없다. 배포 계약이 MSE를 정상 경로로
  정했다면 클라이언트도 MSE를 먼저 선택해야 하며, 매 로드마다 도달 불가능한 WebRTC timeout을
  복구 절차처럼 소비하게 두지 않는다.
- transport fallback과 stream-role fallback은 서로 다른 상태다. 동일한 `live` stream을
  WebRTC에서 MSE로 바꾸는 동안 `대체 스트림`이라고 표시하지 말고, 실제 candidate가
  `live`에서 `focus`로 바뀐 경우에만 대체 스트림 문구·badge·counter를 사용한다.
- 컨테이너 로그가 조용하다는 사실은 브라우저 연결 시도가 없었다는 증거가 아니다. player
  attempt, transport, error category, elapsed time을 구조화해 보존하고 Viewer 종료 뒤에도 마지막
  bounded telemetry를 유지해야 사후 진단이 가능하다. 그 전에는 DOM/video 타임라인, 배포 포트,
  ICE candidate, public stream consumer를 같은 시각축으로 대조한다.
- PowerShell script의 `param(...)` 기본값에서 `$PSScriptRoot`에 의존하지 않는다. 원격 호출처럼
  parameter binding 시점에 값이 비어 있을 수 있으므로 기본값은 빈 문자열로 받고 param block
  이후에 script-relative 경로를 해석한다. 정적 회귀 테스트와 실제 기본 호출을 모두 통과해야 한다.
- 파일 동기화 검증용 SHA-256은 눈으로 옮겨 적지 않는다. 실제 hash 명령의 출력을 변수로 받아
  staging·교체 검증에 그대로 사용하고, 불일치 시 대상 교체 전에 멈춘 사실을 확인한다.
- 다중 NIC 환경에서 한 테스트 PC의 성공을 다른 모니터링 PC의 도달성으로 일반화하지 않는다.
  서비스 bind, signaling HTTP 주소, advertised media candidate와 실제 client source address를
  목표 subnet별로 각각 증명한다. 운영 단말이 192 대역 전용이면 10 대역 시험 성공만으로
  배포 완료를 선언하지 않는다.
- BusyBox `grep`로 `-`로 시작하는 고정 문자열을 검사할 때는 패턴을 위치 인수로 넘기지 말고
  `-e`를 사용한다. 배포 후 검증기 자체가 실패해 자동 롤백된 경우에는 서비스 결함과 구분해
  롤백 완료를 먼저 증명하고, 검증기만 수정한 새 배포를 다시 수행한다.

## 2026-08-10 — 최신 Docker 코드와 Viewer 설치파일 포인터를 함께 검증한다

- Viewer 기능을 서버에 배포할 때 Docker 이미지 revision과 Windows MSI release catalog는
  서로 다른 포인터다. 새 서버 UI만 올리고 이전 MSI를 계속 게시하면 사용자는 기능을 보지만
  실행할 수 없는 클라이언트를 다시 설치하게 된다. 두 포인터를 각각 immutable identity와
  전체 다운로드 SHA-256으로 검증하고, 하나의 배포 결과에서 현재 조합을 명시한다.
- 후속 assertion이 실패했다고 이미 성공한 원자 게시를 곧바로 되돌리지 않는다. 다음 mutation을
  멈춘 뒤 metadata, headers, length, complete content hash와 현재 코드의 실제 계약을 분리해서
  확인한다. 이번에는 artifact가 아니라 검사기가 지원되는 MSI MIME을 잘못 가정했다.
- 컨테이너 검증 명령도 이식성을 가정하지 않는다. 공개 포트는 `127.0.0.1`이라고 추정하지 말고
  Docker binding의 `HostIp`에서 파생하고, `docker top`의 임의 `ps` format 대신 대상 daemon이
  지원하는 형식을 먼저 확인하거나 컨테이너 내부의 read-only `ps` 결과를 안전하게 집계한다.
- 실제 UI 수락에서는 Viewer를 선택했을 때 다섯 고정 기능이 보이는 것뿐 아니라, Agent가
  오프라인이면 실행이 비활성인지 함께 확인한다. 서버 제어 기능이 있다는 사실은 전달 불가능한
  대상에 명령을 쌓아도 된다는 뜻이 아니다.

## 2026-08-10 — 시험용 Viewer 등록 기능을 운영 화면과 분리한다

- Viewer 하트비트는 설치된 클라이언트의 생존·상태 증거이지 운영자가 임의로 만드는 등록
  양식이 아니다. 미리 채운 `QA Viewer` 폼을 운영 콘솔에 두면 가상 레코드를 실제 설치로
  오인하게 하고 상태 판단과 유지보수를 방해한다.
- 삭제 버튼은 서버와 같은 삭제 가능 조건을 사용해야 한다. 최근 하트비트로 온라인인 항목에
  “오프라인 Viewer 삭제”를 활성화한 뒤 `validation`만 표시하는 것은 UI 계약 위반이다.
- 충돌 응답은 현재 상태와 필요한 조치를 구조화해 반환하고, UI에서는 한국어로 행동 가능한
  설명을 보여야 한다. QA 데이터 생성은 API 테스트나 별도 개발 도구로만 수행한다.

## 2026-08-10 — 원격 이미지 포인터 변경은 정확한 키 교체로 실패 폐쇄한다

- 셸 변수로 `sed` 치환식을 조립할 때 끝 앵커와 특수 매개변수 확장이 섞이면, 서비스 변경
  전이라도 표현식 오류가 날 수 있다. 이미지 포인터처럼 한 줄만 바꿀 때는 대상 키 개수와
  기존 값을 먼저 검증하고 정확한 키 교체를 사용한다.
- root 전용 백업을 먼저 만들고, Compose 검증 전 오류에서는 컨테이너를 재생성하지 않는다.
  실패 직후 `.env`, 실행 이미지, health를 다시 읽어 무변경을 증명한 뒤 재시도한다.
- 배포 성공 후에는 이미지 ID·mount·port뿐 아니라 기존 세대 서비스 PID/재시작 횟수까지
  전후 비교해 좁은 배포 경계를 입증한다.

## 2026-08-09 — Select the product generation before setup

- When a repository contains separate product generations, do not assume the currently
  checked-out default branch is the requested target.
- Resolve explicit version language such as "2.0" against the branch and architecture
  documentation before installing dependencies or writing environment configuration.
- For CamStation, `main` is the FastAPI/React 1.x line and `camstation2-initial` is the
  Go single-daemon 2.0 line. Development-environment work must state which line it targets.

## 2026-08-09 — Verify Paseo through its registered-project path

- A valid local `paseo.json` and direct wrapper smoke tests do not prove that Paseo has
  loaded project settings.
- Treat placeholder-only lifecycle fields or an empty script list as a failed integration,
  even when the repository config passes schema validation.
- Before declaring Paseo setup complete, verify the registered project or workspace through
  the daemon/UI/CLI path and account for the rule that new worktrees only inherit config
  committed on their selected base branch.

## 2026-08-09 — Separate Viewer UI liveness from control-agent liveness

- Never accept a stored Viewer `state=healthy` value without comparing `last_seen` to the
  current KST time. A stale database row can remain healthy for weeks.
- Correlate at least three surfaces: current reverse-proxy traffic proves the Electron UI is
  alive, heartbeat age proves the control agent is alive, and command history proves remote
  control is being consumed.
- Inspect the route implementation before using a seemingly read-only pending-command GET;
  the legacy endpoint claims and mutates the oldest command. Expire stale restart commands
  before reviving an agent.

## 2026-08-09 — Prove dual-homed and overlay identities cryptographically

- Map management, camera-LAN, and Tailscale addresses with host keys, service certificates,
  and direct-path evidence rather than relying on hostnames or old documentation alone.
- Treat network reachability and authentication as separate gates. An open Windows SSH or
  AnyDesk service does not mean the maintenance environment has an authorized login.
- For recorder liveness, use a per-camera sample long enough to cross write-buffer flushes;
  a three-second sample produced a false negative that a ten-second inode/mtime/size check
  correctly resolved.
- When an operator identifies a stored endpoint as "probably" a known development server,
  retain that attribution as a candidate mapping. Promote it to verified only after hostname,
  interface inventory, and host-key evidence agree through both addresses.

## 2026-08-09 — Bootstrap Windows access in two verified stages

- When an approved operator can run commands on an otherwise unauthenticated Windows target,
  begin with a read-only identity, group, profile, service, and `sshd_config` diagnostic.
- Do not assume that `%USERPROFILE%\.ssh\authorized_keys` is effective: administrator accounts
  normally use the shared `%ProgramData%\ssh\administrators_authorized_keys` rule instead.
- Add only the maintenance public key after the effective account/path is known, preserve
  existing keys, apply restrictive ACLs, and prove login from the actual maintenance client.

## 2026-08-09 — Separate installed Viewer version from the operating Viewer version

- An MSI uninstall entry and a running management service prove that a Viewer generation is
  installed, not that its interactive UI owns the current monitoring session.
- Reconcile Windows process/session evidence, local service IPC status, current server traffic,
  and the operator's observed screen before declaring a 1.0-to-2.0 cutover complete.
- Treat a side-by-side 2.0 service with no active Viewer lease as staged until the interactive
  Viewer is launched, connected, visibly rendering, and producing fresh server telemetry.

## 2026-08-09 — Separate branch integration from production replacement

- Merging the 2.0 branch into `main` establishes the future source line; it does not migrate
  the legacy database, service units, camera credentials, recording history, or Viewer state.
- When the operator chooses the existing production `cctv` host as the replacement target,
  design a same-host staged cutover with separate runtime directories, ports, database, and
  service names. Keep the 1.x runtime intact as the immediate rollback until 2.0 acceptance.
- Treat the historical `cctv2` host as optional pre-production evidence, not as the production
  destination, unless the operator explicitly changes the deployment decision.
- Check `merge-base` before promising a normal branch merge. When product generations have
  unrelated histories, preserve both parents while making the release tree exactly equal to
  the approved replacement tree; do not resolve the repositories as if they were one code line.
- A same-host deployment is not runtime blue/green when both generations own the same fixed
  loopback ports. Stage artifacts and data independently, then use a single-active maintenance
  handoff with an intact runtime rollback.
- Production configuration must override development defaults explicitly. Recording disabled,
  a test backup target, a small cleanup threshold, or a development-only health response are
  release blockers even when unit tests pass.

## 2026-08-09 — Separate a legacy WebView shell from its server generation

- A Windows Viewer being WebView/Electron-based means it may render a newer server-owned UI;
  it does not prove that its hard-coded startup path, navigation allowlist, heartbeat protocol,
  or control commands are compatible with the newer server.
- A server-first cutover is a useful risk boundary: keep the old Viewer installation as a
  temporary display shell and rollback asset, but never keep the 1.x backend/recorder/go2rtc
  runtime active alongside 2.0 on the same fixed ports.
- If a legacy shell is retained temporarily, use one exact, testable compatibility route to the
  new live page and label control/health features unsupported. Remove the bridge only after the
  2.0 Viewer passes interactive-session and auto-start acceptance.

## 2026-08-09 — Apply the operator's accepted transitional success criterion

- Once the operator explicitly accepts video-only behavior from a transitional legacy shell,
  do not keep management telemetry or remote-control parity as a server-cutover blocker.
- Move those capabilities to the later native Viewer 2.0 gate while retaining actual live-video
  rendering, camera-count invariants, recording, backup, and rollback as non-negotiable evidence.
- Translate the accepted compromise into an exact compatibility contract and automated negative
  tests; “video only is enough” does not justify a broad legacy route or an untested redirect.

## 2026-08-09 — Make production-dangerous defaults empty and tests explicit

- A disabled feature must not silently carry a development destination into a production DB.
  Use an empty inert default, require the destination when the feature is enabled, and keep
  deletion protection enabled independently.
- Tests for command execution must configure their synthetic remote explicitly. If they rely on
  a dangerous package default, changing that default can turn channel-based tests into hangs
  instead of useful failures.
- When the host lacks the SQLite CLI and the source may use WAL, never copy only the main DB file.
  Use the driver's online-backup API, promote a new immutable snapshot without overwrite, and
  compare the active source, snapshot, and converted target through secret-safe canonical hashes.

## 2026-08-09 — Pre-stage everything that does not require the outage

- When the operator explicitly approves server preparation, distinguish inactive staging from
  the final cutover instead of leaving all production work for the maintenance window.
- While 1.x remains healthy, install a hash-pinned 2.0 release, disabled unit, runtime paths,
  online source snapshot, and verified target DB. Do not start the port-conflicting generation.
- The maintenance window should contain only irreducible active-state changes: maintenance page,
  exact legacy stop, port release, 2.0 start, health/video proof, and nginx handoff.

## 2026-08-09 — Validate production topology and semantic stream types before packaging

- A configured recording size is not evidence that the proposed runtime path uses the recording
  filesystem. Resolve mounts and free space first, then keep recording and temp on the same media
  filesystem so finalization can remain atomic.
- A legacy field named `sub_stream_url` may contain a go2rtc producer recipe rather than a camera
  endpoint. Detect the exact loopback/self-key form and translate its intent into a 2.0 output;
  never wrap it as another input and create a recursive producer.
- Files named as backups can still be active when they remain under an nginx wildcard include.
  Compare their hashes, move exact duplicates to a root-only recovery location, and prove the
  single active server block continues serving legacy health before declaring nginx ready.
- Runtime ownership and boot ownership are separate state. A start/stop-only cutover can appear
  healthy and still revert into a port collision after reboot; switch enablement inside the same
  automatic rollback boundary as service and nginx ownership.

## 2026-08-09 — Use camera-capability-aware canaries instead of all-or-nothing rehearsal

- If only part of a camera fleet permits duplicate consumers, an isolated canary can validate that
  subset while the legacy generation remains active. Never infer that success applies to devices
  known to reject concurrent sessions.
- Isolation must cover the whole runtime, not only HTTP: go2rtc API/RTSP/WebRTC, recorder inputs,
  state DB, temp/recording roots, service identity, ingress, and shutdown verification all need
  separate boundaries.
- Build the canary DB from a verified snapshot, disable out-of-scope cameras fail-closed, and keep
  the final-cutover DB immutable so trial state cannot silently become production state.

## 2026-08-09 — Re-evaluate the deployment boundary before adding host-runtime configuration

- When parallel generations are the goal, check whether container isolation removes more lifecycle
  and dependency coupling than adding host-level port flags. Stop before implementation when the
  user changes this architectural boundary.
- Container isolation solves ports, files, dependencies, and rollback packaging; it does not solve
  upstream camera session limits, so camera allowlisting remains a separate fail-closed control.
- Keep ingress separate from the application image when an existing production reverse proxy owns
  the stable client address; this avoids bundling two process managers and reduces cutover scope.

## 2026-08-09 — Distinguish host-port collisions from container-internal ports

- Do not describe a host-native collision as if it also applies to Docker bridge networking. Each
  container has its own network namespace, so repeated internal ports are safe; only duplicate host
  port publications or `network_mode: host` collide.
- If nginx is inside a bridge-networked container, its internal port 80 does not conflict with host
  nginx when it is published to a distinct host port. The reason to omit it is architectural
  simplicity, not an unavoidable port collision.
- Separately name upstream camera-session contention: bridge NAT can still make both generations
  reach a camera from the same host IP, so Docker network isolation does not remove a camera's
  single-client restriction.

## 2026-08-09 — Verify hybrid legacy configuration authority before migration

- Do not call a legacy DB the sole source of truth until startup and runtime code prove it. This
  1.x installation stores camera registry fields and URLs in SQLite, imports missing rows from
  go2rtc YAML, and treats YAML-enabled streams as authoritative for startup recording.
- Cross-check stable keys, enabled state, and secret-safe URL fingerprints between SQLite and
  go2rtc before selecting canary cameras. A fleet-wide mismatch means a DB-only conversion is not
  acceptable even when the intended subset matches.
- Describe a snapshot precisely as an offline data input, not as the old program or a directly
  reusable database. For a hybrid source, snapshot every required authority or build the new DB
  from a reconciled subset.

## 2026-08-09 — Follow the operator-designated runtime authority

- When the operator identifies the live go2rtc configuration as authoritative because it is the
  configuration currently producing video, use it directly for camera keys, enabled state, and
  producer definitions. Do not reintroduce the legacy DB as a camera source through convenience.
- For a video-only canary, generate a minimal new 2.0 DB containing only the explicitly selected
  active YAML streams. Omit legacy ONVIF metadata, layouts, jobs, backup, and alert state rather
  than guessing or merging them.
- Keep the YAML capture read-only and secret-safe: record file hashes and selected stream-name/
  URL fingerprints, never raw producer URLs in logs, manifests, or documentation.

## 2026-08-09 — Separate container-internal media ports from required ingress

- A bridge-networked all-in-one container may keep camstationd, go2rtc API, RTSP, and WebRTC on
  their normal internal ports without publishing each one on the host.
- CamStation's same-origin MSE/WebSocket player is carried through the public HTTP `/player`
  reverse proxy, so an HTTP-only canary does not need a host RTSP, go2rtc API, or ICE mapping.
- Direct WebRTC media is the explicit exception: it needs a reachable ICE listener. When the
  operator accepts video-only MSE validation, do not publish an unused WebRTC port or mistake an
  automatic five-second WebRTC-to-MSE fallback for direct WebRTC success.

## 2026-08-09 — Report fleet counts by site before canary work

- A fleet-wide phrase such as “eight active cameras” is ambiguous when the operator is explicitly
  separating home cameras from fire-station cameras. Always break the baseline down by site and
  state which generation reported it.
- Keep the existing 1.0 operating state distinct from the 2.0 canary selection: observing an
  enabled legacy camera does not mean the canary enabled or contacted it.
- Express canary selection as a positive `집-` allowlist, not as a fire-station-only denylist;
  this also excludes the goat-farm camera and any future non-home entry by default.
- Before any canary start, re-prove that the canary container and DB are absent and state exactly
  what has merely been staged versus what is running.

## 2026-08-09 — Bind canary ingress to the operator's access network

- A technically reachable CCTV-side address is not automatically the address the operator will
  use. Confirm the requested interface before starting the container and bind only that address.
- For this retained canary, publish only `10.0.0.26:18081/tcp`; keep all internal go2rtc ports
  unpublished and report the exact URL only after runtime and continuity gates pass.

## 2026-08-09 — Treat the operator's Viewer route as a separate product surface

- Do not infer that a responsive management route such as `/live` is the mobile Viewer merely
  because it contains video tiles or accepts a `viewer=1` query. Route semantics must be verified
  against the production surface the operator actually uses.
- For this system, the operator-designated 1.0 `/viewer` contract is a chrome-free, full-viewport,
  read-only camera layout that starts all visible MSE streams immediately. It is materially
  different from the 2.0 live operations workspace with navigation, layout editing, side panels,
  PTZ controls, and timeline.
- Validate Viewer parity using the exact route at a mobile viewport: inspect visible UI, overflow,
  video count, ready state/current-time advancement, transport, tile interaction, and direct-page
  reload. A successful desktop `/live` check does not satisfy this gate.

## 2026-08-09 — Normalize Viewer counts by open pages before diagnosing a leak

- A three-camera Viewer creates three downstream media consumers per open page. Ask for or observe
  the number of simultaneously open pages before treating an aggregate `viewerCount` above three
  as a reconnect storm.
- Compare the expected baseline `open pages × visible cameras` with per-stream counts, then watch
  whether excess consumers drain after reloads. Transient stale sockets and continuously growing
  consumers are different failure modes.
- Correlate excess count with CPU, PID count, browser playback state, and connection age before
  changing retry behavior or container limits.

## 2026-08-09 — Size task limits for the final fleet, not the canary subset

- A three-camera canary proves behavior but does not define production capacity when the real fleet
  has eight cameras. Extrapolate recorder and live-transcoder threads to the final camera count and
  include focus and reconnect headroom.
- Container PID counters include threads. Inspect cgroup `pids.peak` and `pids.events`; quiet app
  logs do not disprove PID exhaustion when the kernel has rejected task creation.
- Preserve camera-safety scope while correcting capacity: raising a cgroup limit does not authorize
  enabling excluded cameras or contacting upstreams outside the positive allowlist.

## 2026-08-09 — Separate installer-owned payload from upgrade remnants

- A directory file count cannot by itself classify an MSI installation as corrupt. Resolve the
  cached MSI Directory, Component, and File tables, then compare every owned path and expected size
  before treating extra files as missing or damaged installation content.
- Record key-binary hashes, signature state, root ACLs, product state, service registration, and
  package provenance separately. Complete payload placement does not make an unsigned development
  package production-approved.
- Installed and service-running do not mean cutover-ready. Verify the active endpoint, auto-start
  setting, interactive process, server registration/heartbeat, renderer state, and visible playback
  while preserving the currently operating client as rollback.

## 2026-08-09 — Treat functional client defects separately from install integrity

- A complete MSI payload and healthy service do not disprove an operator-observed application bug.
  Installation integrity answers whether the package landed correctly; only a newer, identified
  build plus reproduction-focused interactive testing can answer whether the bug is fixed.
- Before a “latest version” reinstall, compare upstream and local source, assign a version greater
  than the installed product, and verify artifact hashes on both sides. Never spend the maintenance
  window reinstalling the exact same package under an ambiguous label.

## 2026-08-09 — Keep MSI production off the monitoring workstation

- A monitoring workstation is an installation and maintenance target, not a convenient build host.
  Do not stage compilers, SDKs, package restores, or installer source there even when administrative
  access and disk capacity make it technically possible.
- For this WiX 6 package, build and sign on a dedicated Windows VM or CI runner. Transfer only the
  completed MSI plus its hash/signature evidence to the restricted NUC maintenance staging area.
- When the operator corrects the boundary, stop before installation, remove only the exact temporary
  build stage, and prove the installed client, service, and legacy monitoring session are unchanged.

## 2026-08-09 — Separate build-path readiness from artifact readiness

- A Linux host can validate Viewer tests, Windows Electron packaging, a cross-compiled service,
  source policy, and PowerShell syntax, but those checks do not prove that WiX produced a valid MSI.
- Report the repository entry point as prepared while keeping the real-Windows build, MSI database
  inspection, signature state, and lifecycle tests as an explicit open gate.
- Do not substitute Wine or a monitoring workstation for the missing Windows build environment.
  Designate a dedicated Windows VM or CI runner, then retain its version, hash, source commit, dirty
  state, and tool versions as artifact provenance.

## 2026-08-10 — Clean live recording stores through finalized-ID snapshots

- A recording cleanup against active one-minute workers is a moving target. Capture an exact
  finalized-ID cutoff, delete only that snapshot through the application's checked delete path,
  and expect newly finalized rows while the sweep runs.
- Prove the database/file set is one-to-one before deletion: canonical managed-root containment,
  row/file count, byte size, missing/extra files, and representative ffprobe results. Do not use a
  recursive filesystem deletion when the application maintains recording tombstones.
- After the first sweep, repeat only bounded guarded snapshots until a zero-ready checkpoint is
  observed; never include `recording`/`finalizing` rows or active temp files. State clearly that new
  files will recur unless recording is separately disabled.
- Recovery status comes from backup evidence, not assumption. An operator-deleted file with
  `backup_state=pending`, no backed-up timestamp, and no trash/quarantine copy is not recoverable
  through CamStation even though its audit row remains.

## 2026-08-10 — Separate development-host access from monitoring-host maintenance

- Give a dedicated Windows build PC its own local maintenance principal and host-specific key;
  never reuse the monitoring NUC account, key, or broader access policy for development work.
- Minimize the access surface, not merely the number of script lines: bind sshd to the authorized
  target address, restrict TCP/22 and `AllowUsers` to the independently resolved maintenance source,
  require public-key authentication, and disable forwarding.
- An operator-run bootstrap is only staged access. Record the returned server host-key fingerprint,
  pin it independently, and prove the intended administrative identity before reporting that the PC
  is controllable or installing build tools.
- Existing SSH services, authorized keys, authentication policy, or competing port-22 firewall rules
  are ownership boundaries. Stop for review instead of merging them into an automated bootstrap.
- When the operator says file transfer is unavailable, deliver the first-stage bootstrap directly as
  one pasteable PowerShell block. Establish only a source-restricted key path first; inspect and
  harden `sshd_config` from the verified session instead of making the operator transport a full
  maintenance package before access exists.
- On Windows PowerShell over SSH, avoid per-rule firewall joins across the full rule set: they can
  leave expensive orphaned diagnostic processes if the transport times out. Query protocol filters
  first, narrow by local port, then resolve the associated rule; if cleanup is needed, inspect
  session, parent PID, and command line and terminate only the exact diagnostics created by the task.

## 2026-08-10 — Keep remote Windows bootstrap output separate from control values

- In Windows PowerShell, every success-stream message emitted inside a function becomes part of its
  return value. A helper that both prints download progress and returns an archive path can therefore
  pass a malformed array to `tar`; use `Write-Host`/the information stream for progress, or make the
  function return only one typed control value.
- Windows PowerShell 5.1 recursively unwraps nested arrays in the success pipeline. Do not encode a
  table of validation tuples as nested arrays and rely on row boundaries; use objects with named
  properties or perform explicit scalar checks before any extraction.
- After a remote setup script stops, inspect the bounded destination before retrying. Resume only
  after proving that no partially extracted tool directory was promoted into the versioned tools
  root.
- Avoid nested Bash → SSH → PowerShell `-Command` quoting for scripts that contain PowerShell
  strings or variables. Send a literal script block to `pwsh -File -` over standard input so error
  policy, paths, and comparison values retain their exact meaning.
- Normalize `CRLF` only at the text-comparison boundary when comparing Windows-generated manifests
  with Linux output. A raw manifest hash can differ solely because PowerShell emits `CRLF`, even
  when every recorded file digest and path is identical; never rewrite the transferred source to
  make the diagnostic match.
- Parenthesize both operands when combining PowerShell's `-join` operator with comparisons such as
  `-ne`. Without explicit grouping, operator precedence can compare the wrong expression and make
  an exact artifact-name set appear invalid.
- Treat a missing JSON property as a verification failure, not as an empty report field. Inspect the
  artifact schema first and assert every required field is present and non-null before publishing a
  summary; PowerShell otherwise returns `$null` for a misspelled or wrongly nested property without
  stopping the script.

## 2026-08-10 — Exercise native Windows paths before declaring Viewer packaging ready

- A Unix-domain-socket test path such as `service.sock` is not a valid substitute for a Windows
  named pipe on native Windows. Integration tests for the Viewer management channel must select a
  unique `\\.\pipe\...` endpoint on Windows and retain a temporary Unix socket only elsewhere.
- npm scripts intended for native Windows must not depend on POSIX `rm` or `mv`. Put filesystem
  preparation/finalization in a small Node script so the same locked command runs under `cmd.exe`
  and Unix shells.
- `@electron/asar` reports archive entries with the host separator (`\\` on Windows). Normalize its
  returned entries to one leading slash and `/` separators before required/leaked-file checks;
  otherwise a valid Windows package is falsely reported as missing its runtime files.
- A per-machine MSI must not mix non-advertised profile shortcuts with a file-keyed machine
  component. Keep the executable as the file KeyPath, make its all-users shortcuts advertised, and
  author an uninstall `RemoveFolder` row for the product-created Start Menu directory; keep ICE43,
  ICE57, and ICE64 enabled so this boundary is proved by real Windows Installer validation.
- Windows Installer database SQL is deliberately restricted and does not support a normal
  `SELECT COUNT(*)` aggregate. For an inspected table count, select its primary-key column and count
  successive COM `Fetch()` records; an unsupported query can surface as an unhelpful COM type or
  dispatch error after an otherwise valid MSI build.
- Windows Installer automation accepts `OpenDatabase(path, 0)` when the mode is marshalled as a
  signed `Int32`, while a `UInt32` variant reproduces `DISP_E_TYPEMISMATCH`. Cast COM paths, open
  modes, SQL strings, and record indices explicitly instead of relying on PowerShell's implicit
  automation marshalling.
- Closing query views is not enough to release an MSI file handle: the Windows Installer COM
  `Database` remains open until its RCW is released. Initialize the COM references before the build
  try block and call `FinalReleaseComObject` for Database then Installer in `finally` before deleting
  the exact temporary workspace, so a successful artifact never returns a cleanup failure.
- A native npm addon may be present and hash-correct yet fail with `ERR_DLOPEN_FAILED` when its PE
  imports are unavailable. Inspect the binary imports and system DLL evidence before blaming npm's
  optional-dependency warning. On a dedicated x64 Windows build host, install Microsoft's current
  signed x64 Visual C++ Redistributable, record its installer hash/version/exit code, suppress
  restart, and prove the exact native import before retrying the build.
- Every operational named-pipe probe must bound both connect and response reads. Use an asynchronous
  read with a timeout and dispose the pipe on timeout; a plain `ReadLine()` can strand the SSH child
  process even after the local transport is interrupted.
- Do not use an SSH session to prove a desktop-only Viewer management pipe. The service pipe
  intentionally denies the Windows Network SID and permits interactive users, administrators, and
  SYSTEM under its reviewed ACL; preserve that boundary and run UI/pipe acceptance from an actual
  interactive desktop token.
- Windows service and MSI verification must not depend on localized display text. Prefer process
  exit codes, event IDs, registered values, invariant engine markers, and numeric recovery settings;
  Korean `sc.exe` text can also be mojibake when captured through a differently encoded SSH stream.

## 2026-08-10 — Remote GUI development requires an interactive evidence loop

- Administrative SSH proves installation and service state, but it does not prove what an Electron
  window renders or whether real keyboard focus works in another user's RDP session. Never present
  command-line health as desktop acceptance.
- Do not make the operator act as the agent's camera by repeatedly supplying screenshots. Establish
  a session-aware loop that can launch the target, capture only its window, collect bounded UIA
  metadata, apply an intentional input action, and return fresh evidence over the existing secured
  transport.
- Prefer a passwordless, one-shot `TASK_LOGON_INTERACTIVE_TOKEN` task in the already logged-on test
  user's session over a new VNC server, listening port, stored RDP credential, or weakened named-pipe
  ACL. Use a unique task name, least-privileged interactive token, bounded execution, exact target
  process, restricted evidence directory, and guaranteed task deletion.
- A full-desktop screenshot can expose unrelated windows. Default GUI evidence to the verified
  CamStation Viewer window rectangle and record the user/session/process identity with every image.
- Never carry patch-marker `+` prefixes into a literal remote here-string. Require an explicit
  success sentinel before treating a remote validation payload as executed.
- Electron's UIA tree can lag behind the first rendered frame. If a first bounded scan misses edit
  controls, repeat a capture after the renderer settles before declaring UIA unavailable or falling
  back to coordinates.

## 2026-08-10 — Keep committed tests independent of local evidence

- Before cleanup, trace every test fixture back to its source. A test that reads an untracked
  `work/` artifact can pass in the active workspace and fail in a clean clone.
- Promote reusable maintenance scripts into a reviewed source directory, point tests there, and
  keep raw screenshots, runtime evidence, known-host files, and operator records outside Git.

## 2026-08-10 — Reconcile long-running work before handing off commits

- For a long dirty session, inventory tracked, untracked, ignored, and upstream state before staging.
  Split completed work by responsibility and inspect every staged name/stat/check result.
- If upstream gained overlapping work, first secure the local work in logical commits, then fetch and
  reconcile explicitly. Keep the implementation proven against the real runtime, retain useful
  upstream tests, and remove duplicate or superseded source and plans.
- Embedded frontend assets are derived from resolved source. Rebuild them after conflict resolution
  and confirm the expected content hashes before the final full-suite verification.

## 2026-08-10 — Preserve remote GUI knowledge as a repository skill

- A proven remote GUI evidence path should not remain only in chat history or an operator's memory.
  Register a repository-scoped skill under `.agents/skills` so later project sessions discover the
  same procedure from the repository.
- Keep operational code canonical in the reviewed project scripts and make the skill a narrow
  decision/runbook layer. Copying PowerShell into skill resources creates two implementations that
  can silently diverge.
- GUI verification instructions must require direct image inspection plus bounded UIA evidence;
  installation, service state, process existence, and nonempty screenshot files are not substitutes
  for seeing the rendered window.
- Put Korean and English task phrases in the skill description when the project's operator language
  is Korean. Protect the trigger metadata, exact-window boundary, artifact integrity, and cleanup
  rules with a source-policy test that also rejects embedded environment IPs, key fingerprints, and
  private-key material.

## 2026-08-10 — Publish operator downloads through the settings surface

- When the operator asks to download a client from the 2.0 server, a working API endpoint alone is
  incomplete. The canonical operator entry point is `/settings`; verify the real rendered page and
  require a visible download action backed by the same release metadata and artifact hash.
- Inspect the live page before adding UI. The source may already contain the correct card while the
  deployed release catalog is empty; in that case publish and verify the artifact instead of
  duplicating the component or inventing a second download route.
- Define completion by the operator journey: settings-page download, Windows installation, Viewer
  launch, server connection, and live monitoring. Do not confuse the installed application EXE with
  the distributable installer. The current standard package is the MSI and must install the Viewer
  EXE, service, shortcuts, and uninstall registration as one lifecycle-owned product; reviving the
  rejected custom Setup EXE or publishing a bare application EXE would not satisfy that journey.
- For an optional Windows registry value, read the parent key and inspect
  `PSObject.Properties[name]`. `Get-ItemPropertyValue -ErrorAction SilentlyContinue` can still emit a
  localized missing-property diagnostic even when absence is the expected success state, making a
  successful cleanup appear failed.
- In nested SSH shell validation, avoid `$1`-based `awk` snippets under a remote `set -u` unless the
  quoting boundary is proven. Prefer shell parameter trimming or a local parser so validation does
  not fail after the underlying publication already succeeded.

## 2026-08-10 — Recover the product contract before judging an architecture migration

- When a current package no longer carries an older supervisor component, do not infer that remote
  control was intentionally removed. Search the original specifications and Git history first.
- Separate the normative product requirement from the current implementation state. A broken or
  incomplete migration is evidence of a gap, not evidence that the requirement disappeared.
- For Viewer analysis, preserve the operator requirement that the server can recover a remote
  display without routine PC access, and map monitoring and control as separate planes before
  recommending which process owns lifecycle actions.

## 2026-08-10 — Prove Viewer control and monitoring as separate end-to-end paths

- A local IPC handler returning success is not proof that telemetry is monitored. Trace every status
  field through renderer report, Service snapshot, server heartbeat, database, and operator UI; the
  standard Service had accepted `stream_telemetry` while silently discarding it.
- Stream telemetry must be repeated independently of playback-state transitions. A renderer can sit
  in a long recovery cooldown without changing state; periodic reports keep the stream selectable for
  targeted remote resubscription and make its latest progress time truthful.
- Persist a Service-restart command and target boot generation before stopping the Service. On the
  next boot, reconcile and retry any unreported terminal result from the local journal without
  executing the side effect again or waiting for server redelivery.
- Before a standard MSI upgrade, close only the exact installed Viewer process set so Windows
  Installer can replace the payload deterministically. After installation, verify the registered
  version, exact service state, packaged files, size, and hash rather than trusting installer exit 0.
- Clean a disposable Viewer identity as one unit: remove both its server configuration and local
  command journal. Journal command IDs are machine-local, and retaining test IDs beside a newly
  generated client identity can contaminate later acceptance runs.
- Windows PowerShell 5.1 does not support every newer convenience behavior. Poll the exact process
  for bounded waits instead of assuming `Wait-Process -Timeout`, and inspect a property object before
  reading an optional registry value because a missing `Get-ItemPropertyValue` property can still
  emit an error under `SilentlyContinue`.

## 2026-08-10 — Merge verified Viewer control without discarding newer 2.0 work

- Before merging a long-running feature branch, inspect the target worktree and every target-only
  commit. The active 2.0 branch had newer Viewer registry, MSI download, and GUI-skill work that
  overlapped the command restoration and had to remain authoritative in those responsibilities.
- Resolve source conflicts by responsibility, not with a blanket ours/theirs choice. In this merge,
  offline-only Viewer deletion and `SupportsAgentUpdate` stayed from 2.0 while strict command JSON,
  the five-command allowlist, and durable Service execution came from the feature branch.
- Never hand-resolve hashed frontend output. Resolve React/API source first, run the canonical Web
  build with `emptyOutDir`, and stage only the newly referenced CSS/JS hashes.
- A successful feature-branch suite is not merge proof. Rerun the complete repository check after
  conflict resolution; this also proves target-only tests for read-only registry and Settings MSI
  delivery coexist with the new Viewer command tests.

## 2026-08-12 — Separate Windows CPU saturation from zombie suspicion

- Windows does not have the Unix zombie-process state. Diagnose the operator's "zombie" suspicion
  with the focused process tree, parent PID presence, one-shot scheduled tasks, run artifacts, and
  application responsiveness; an intentionally detached session daemon is not a zombie merely
  because its launcher has exited.
- Normalize `Win32_PerfFormattedData_PerfProc_Process.PercentProcessorTime` by the number of logical
  processors and corroborate it with repeated `_Total` samples. Do not report an unnormalized sum of
  `Get-Process` CPU deltas as machine utilization.
- Capacity and pressure are different. Free disk space does not rule out an I/O bottleneck, and
  installed RAM does not rule out memory pressure; verify disk busy time/queue, available/committed
  memory, and paging alongside CPU.
- On a four-logical-CPU monitoring PC, video renderer/GPU work plus active remote-screen encoding can
  saturate the machine while every control process is healthy. Establish this resource baseline
  before attributing slow SSH, PowerShell, or Task Scheduler responses to the control harness.
- On a saturated Windows target, prefer one bounded encoded PowerShell diagnostic with focused CIM
  queries over broad `tasklist` or repeated Task Scheduler enumeration. Record and distinguish an
  empty interrupted-run directory from an executing task or process.

## 2026-08-12 — Make Windows UI selection tolerant but exact

- A selector over a heterogeneous accessibility collection must treat a candidate that lacks one
  requested property as a non-match, not abort the entire selection. Keep zero and multiple matches
  fail-closed, and protect the missing-property behavior with a regression test.
- Before writing a control plan, read the exact driver output contract or the already-proven
  canonical cleanup code. Do not infer property names such as `index`; the title-bar contract here
  is `element_index`. Likewise, do not infer a localized close label when a stable role/index token
  is available.
- Match every observation option used by a proven cleanup path. `include_screenshot`, element limit,
  and depth can change the returned accessibility subset, so a selector is only reusable when its
  observation contract is the same.
- A successful window close can leave a disposable UWP `ApplicationFrameHost` alive without a
  window. Record the exact launch PID, verify the target window count is zero, and clean only that
  PID before declaring the control host residue-free.
- On this four-logical-CPU monitoring PC, ending the active AnyDesk session and stopping its service
  reduced repeated total CPU from 100% to roughly one third and cut standard control Status from
  about 20 seconds to about 2.2 seconds. Always measure the same counters before and after changing
  a remote-display workload.

## 2026-08-12 — Keep application rollout policy out of the PC-control skill

- Do not encode a Viewer version, migration stage, or application rollout goal in a generic Windows
  PC target profile. Those belong to the deployment request and its runbook. The control profile
  should contain only stable connection, machine identity, interactive-session, and display facts;
  observe application state fresh for each request.
- Enforce that boundary across the skill description, trigger phrases, loaded references, target
  notes, and regression tests—not only the profile JSON. A rollout runbook linked from a generic
  control skill still mixes deployment policy into PC control even if the profile itself is neutral.
- Do not pass growing preflight/status programs as a Windows OpenSSH `-EncodedCommand`. Adding only a
  few canonical file checks can cross the Windows remote-command length limit and make every target
  fail before identity verification. Keep the SSH command fixed and short with an encoded bootstrap
  that reads all of stdin, and send the trusted script source over stdin. Plain `-Command -` is not a
  drop-in replacement on Windows PowerShell 5.1 because it can emit interactive prompts around JSON.
- A Windows-control skill must preserve the technical execution contract, not only policy prose.
  Document exact target resolution, pinned SSH and identity/session preflight, interactive-token
  execution, Cua/UIA selector contracts, background-to-foreground escalation, screenshot scope,
  process ownership, artifact hashes, and exact cleanup so a later agent can operate quickly without
  re-deriving the machinery.
- The same Cua binary can enumerate windows on two PCs while `get_desktop_state` serializes correctly
  only on the physical-display host and returns non-JSON on a VM's Basic Display Adapter. Narrow a
  failure with `get_screen_size`/`list_windows`, compare driver hashes and display adapters, then use
  an interactive-session GDI fallback only for the known invalid desktop-JSON response. Record the
  fallback capture mode and keep all other driver, identity, timeout, and session failures closed.
- Explorer/session IDs alone do not prove an operable desktop: a disconnected RDP session retains
  Explorer and UIA-visible windows while desktop serialization returns non-JSON and GDI capture fails
  with an invalid handle. Read the locale-independent WTS connection-state enum and require `Active`
  before screenshot, foreground input, or visually verified GUI control.
