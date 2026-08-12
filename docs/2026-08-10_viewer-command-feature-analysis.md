# Viewer 모니터링 및 원격 제어 기능 분석

> 분석일: 2026-08-10 KST
>
> **정정:** Viewer 명령은 과거 Agent 구조의 불필요한 잔존 UI가 아니다. Git 이력에서 복원한
> 최초 Viewer 2.0 사양부터 서버의 원격 모니터링과 제어는 핵심 제품 요구였다. 표준 MSI
> 재설계도 이 요구를 폐기하지 않았다. 현재 명령이 실행되지 않는 것은 요구가 사라진 결과가
> 아니라, 새 Service/Electron 구조로 옮기는 과정에서 제어 실행·결과 폐루프가 빠진 구현 회귀다.
>
> **구현 결과:** 같은 날 해당 회귀를 복구하고 Windows 11 실기에서 표준 MSI `2.0.24`로
> 검증했다. 현재 운영자 명령 5개는 Server → Service → Electron/renderer 또는 Windows
> lifecycle → Server 결과 경로를 닫는다. 이 문서의 “복구 전” 표와 회귀 분석은 원인 기록으로
> 남기며, 현재 상태는 12절을 기준으로 한다.

## 1. 결론

CamStation 서버는 Viewer PC를 평상시 직접 조작하지 않아도 다음을 수행할 수 있어야 한다.

- Viewer Service, 제어 채널, Viewer 앱, renderer, 개별 스트림 상태를 서로 구분해 관찰한다.
- 특정 Viewer를 선택하고 안전하게 정의된 기능을 원격 실행한다.
- renderer가 멈춰도 Service의 heartbeat와 명령 수신은 계속된다.
- Viewer 앱이 비정상 종료되거나 응답하지 않을 때 서버 명령으로 시작·재시작할 수 있다.
- 명령의 전달, 수락, 실행, 완료 또는 실패를 별도 상태로 확인한다.
- 재시작·업데이트처럼 프로세스가 끊기는 작업도 재기동 후 같은 명령으로 조정한다.

복구된 구현은 Service의 heartbeat/control loop를 renderer와 독립적으로 유지하면서,
`Service → Electron → renderer → 결과 보고`를 다시 연결한다. 명시적인 Viewer/Service 재시작은
임의 원격 조작 대신 MSI가 설치한 정확한 실행 파일, 현재 interactive session, 고정 SCM 서비스만
다루는 좁은 lifecycle adapter가 담당한다. 전달과 실행 결과는 durable journal과 서버 상태 전이로
구분된다.

## 2. 최초 문서와 설계 변경 이력

### 최초 확인 문서

저장소 Git 이력에서 확인 가능한 최초 Viewer 2.0 기능 사양은 다음 문서다.

- 문서: [2026-07-03 Viewer client redesign](superpowers/specs/2026-07-03-viewer-client-redesign.md)
- 최초 커밋: `ee6879cf225201424d1c57ee5f36c2ec01929457`
- 최초 상태: `Approved planning record`

현재 파일은 이후 대체 설계를 가리키는 역사 문서로 수정됐지만, 최초 버전은 아래 명령으로
그대로 확인할 수 있다.

```bash
git show ee6879cf225201424d1c57ee5f36c2ec01929457:docs/superpowers/specs/2026-07-03-viewer-client-redesign.md
```

최초 사양의 핵심 제품 계약은 다음과 같다.

- 서버는 web renderer가 freeze, crash, unresponsive 상태여도 Viewer를 모니터링하고 제어한다.
- heartbeat와 명령 수신 주체는 renderer JavaScript가 아니라 renderer 밖의 native 프로세스다.
- 명령 전달·ACK, reload/restart, 단일 스트림 resubscribe, diagnostics가 1차 범위다.
- `/viewers`는 상태 조회와 명령 실행을 위한 운영 화면이다.
- 임의 원격 셸이나 전체 원격 데스크톱은 범위가 아니다.

2026-05-13에 추가됐다가 제거된 `docs/wiki/CCTV-유지보수.md`도 Git에서 확인했지만,
`/viewer/*` nginx 경로만 포함하고 Viewer 제어 아키텍처나 기능 계약은 포함하지 않는다.
초기 계획이 참조한 `.omo/drafts/viewer-client-redesign.md`는 현재 작업공간과 Git 이력에 없다.
따라서 `ee6879c` 문서가 현재 저장소에서 복원 가능한 최초의 명시적 기능 사양이다.

### 변경 연표

| 날짜·커밋 | 문서 또는 구현 | 확인된 결정 |
|---|---|---|
| 2026-07-03 `ee6879c` | 최초 Viewer client redesign | Electron main이 renderer 밖에서 heartbeat, command, recovery를 담당하며 서버 원격 제어를 보장한다. |
| 2026-07-16 `6baa371` | [Windows Viewer control/update/playback 설계](superpowers/specs/2026-07-16-windows-viewer-control-and-playback-design.md) | 권한을 machine-wide Agent Service로 이동한다. 모니터링, 제어, renderer, stream 상태를 분리하고 restart/update를 내구성 있게 만든다. |
| 2026-07-16 `6bf2806`, `7603d15` 이후 | Agent와 Electron 구현 | durable command ledger, SSE/long-poll, Viewer 명령 IPC, `command_result`, Viewer generation restart가 실제 구현됐다. [.superpowers Task 5](../.superpowers/sdd/windows-viewer-task-5-report.md), [Task 6](../.superpowers/sdd/windows-viewer-task-6-report.md) |
| 2026-07-18 `1db45c9`, `2db015c` | [표준 MSI 설계](superpowers/specs/2026-07-18-standard-windows-viewer-installer-design.md)와 [로드맵](superpowers/plans/2026-07-18-standard-windows-viewer-roadmap.md) | custom installer와 일반 process supervision을 제거한다. 그러나 Service의 server heartbeat/management channel과 server UI command 전달은 승인 범위로 명시적으로 유지한다. |
| 2026-07-18 `06f8a5c` | management control을 Service로 이동 | Service가 heartbeat와 SSE/long-poll을 소유하고 `reload_live`, `resubscribe_stream`, `shutdown`을 lease owner에게 큐잉한다. |
| 2026-07-19 `c6ef57c` | 표준 MSI Viewer 출하 | 기존 `agentPipe.ts`의 `onCommand`, `viewer:command`, `command_result` 처리를 삭제하고 새 `managementPipe.ts`에 대체 이벤트 처리를 넣지 않았다. 현재 단절이 여기서 생겼다. |

중요한 판정은 다음과 같다.

- 표준 MSI 전환은 **서버 원격 제어 요구를 폐기하지 않았다.** 표준 설계의 `Server Status and
  Control` 절과 로드맵의 `Server status and UI commands` 항목이 이를 유지한다.
- `reload_live`·`resubscribe_stream` 실행/결과 경로의 소실은 설계 변경이 아니라 구현 회귀다.
- `ping`, Viewer restart, control-service restart가 새 Service 계획에 이식되지 않은 것은 계획
  누락이다.
- 일반 supervision 제거는 명시적 설계 변경이지만, 원격 PC 접근 없이 죽은 Viewer를 복구해야
  한다는 최초 요구와 충돌한다. 표준 MSI를 유지하면서 좁은 lifecycle actuator를 다시 설계해야
  한다.

## 3. 의도된 계층 분리

모니터링과 제어는 같은 연결 상태나 같은 배지로 합치면 안 된다.

| 계층 | 방향 | 주 책임자 | 책임 | 독립성 규칙 |
|---|---|---|---|---|
| 모니터링 plane | Viewer/renderer/stream → Service → Server | Viewer Service | 주기적 heartbeat, Viewer·renderer·stream telemetry, 버전, 최근 진행 시각과 복구 상태 보고 | 영상이 보인다는 이유로 제어 채널을 정상으로 판단하지 않는다. Service가 살아 있다는 이유로 renderer를 정상으로 판단하지 않는다. |
| 제어 plane | Server → Service → 실행 주체 → Server | Server command queue + Viewer Service | 명령 수신, dedupe, TTL, 수락, 실행 조정, 결과 보고 | renderer가 freeze/crash여도 Service의 명령 수신은 살아 있어야 한다. heartbeat 성공과 명령 채널 성공을 별도 시각으로 기록한다. |
| UI 실행 adapter | Service ↔ Electron main ↔ Web renderer | Electron main과 renderer | 화면 reload, 단일 stream resubscribe, renderer 결과 반환 | allowlist 명령만 처리하며 임의 URL, 셸, 파일 또는 camera credential을 받지 않는다. |
| lifecycle adapter | Service ↔ Windows session/SCM | 별도 좁은 lifecycle 실행기 | Viewer 시작·종료·재시작, Service 재시작 후 명령 조정 | 일반 원격 데스크톱이 아니며 승인된 실행 파일·세션·작업만 수행한다. operation key와 bounded recovery가 필수다. |

서버 상태 모델도 최소한 다음 축을 분리해야 한다.

- Service/Agent heartbeat: PC의 관리 구성요소가 살아 있는가?
- Control channel: 실제 서버 명령을 받을 수 있는가?
- Viewer process: 실행 중, 닫힘, crash, restarting 중 무엇인가?
- Renderer: ready, unresponsive, failed 중 무엇인가?
- Stream: 각 카메라가 connecting, playing, stalled, recovering, offline 중 무엇인가?
- 시간: 마지막 Service heartbeat, 마지막 control 성공, 마지막 renderer pulse, 마지막 영상 진행

이 분리가 있기 때문에 “영상은 보이지만 제어 채널은 죽음”, “Service는 온라인이지만 Viewer는
닫힘”, “Viewer는 실행 중이지만 한 스트림만 stall”을 서로 다른 장애로 진단할 수 있다.

## 4. 원래 정의된 Viewer 기능

최초 사양과 2026-07-16 승인 설계를 합치면 기능 범위는 현재 드롭다운 5개보다 넓다.

| 기능 | 사용자 목적 | 실행 계층 | 완료 조건 |
|---|---|---|---|
| `ping` | 해당 PC의 제어 채널 확인 | Service | 같은 command ID를 수락하고 `succeeded`를 서버에 보고 |
| `reload_live` | Viewer 라이브 화면 전체 새로고침 | Electron main | 승인된 `/live?viewer=1` 재로드 후 renderer ready |
| `resubscribe_stream` | 특정 카메라의 Viewer-side 재연결 | Web renderer | 해당 stream pipeline 재생성 후 새로운 영상 진행 확인 |
| `restart_viewer` | 멈추거나 crash한 Viewer 앱 시작·재시작 | lifecycle adapter | 목표 generation의 새 Viewer lease와 renderer ready 확인 |
| `restart_agent` / `restart_service` | 고장 난 관리·제어 구성요소 재시작 | SCM/helper | 새 boot generation의 Service가 동일 identity로 control 재연결 |
| `restart_stream` | 서버의 특정 go2rtc/stream worker 복구 | CamStation server | 서버 stream operation의 성공/실패 결과 확인 |
| `capture_diagnostics` | PC 접근 없이 제한된 진단 자료 수집 | Service + Viewer | whitelist/redaction을 통과한 bounded 진단 결과 등록 |
| `update_app` | 지정 Viewer 버전으로 무인 업데이트 | Service + MSI broker | 설치 버전, Service/control, 필요 시 Viewer/renderer 건강성 재확인 |

`restart_stream`은 서버 스트림 복구이고 `resubscribe_stream`은 Viewer 재생 파이프라인 복구다.
사용자 화면에서 이름과 효과를 명확히 구분해야 한다. `update_app`은 일반 명령 드롭다운보다는
버전 배포 정책이 만드는 시스템 작업이 적합하다.

## 5. 복구 전 구현 상태

이 절은 2026-08-10 복구 작업을 시작하기 직전의 회귀 baseline이다. 현재 구현 상태는 12절과
[implementation status](07-implementation-status.md)를 참조한다.

### 현재 경로

| 단계 | 현재 동작 | 판정 및 근거 |
|---|---|---|
| Viewer 선택 및 명령 생성 | Viewer ID를 직접 입력하거나 `datalist` 제안을 고르고 5개 영문 명령 중 하나를 선택한다. 모든 명령에 stream·route·message 필드가 노출된다. | 엄격한 대상 선택이나 기능별 폼이 아니다. [ViewerCommandPanel.tsx](../web/src/pages/viewers/ViewerCommandPanel.tsx) |
| 서버 저장 | Viewer 레코드 존재와 비어 있지 않은 type만 확인하고 SQLite에 `pending`으로 저장한다. | command whitelist, 타입별 schema, 온라인/control-ready 검증이 없다. [viewer_commands.go](../internal/store/viewer_commands.go) |
| 서버 전달 | SSE 또는 long-poll로 명령을 내보내며 DB를 `delivered`로 바꾼다. | 네트워크 전달이지 실행이나 수락이 아니다. [routes_viewers.go](../cmd/camstationd/routes_viewers.go) |
| 현재 Service | `reload_live`, `resubscribe_stream`, 내부 `shutdown`만 local pipe로 큐잉한다. `ping`, `restart_viewer`, `restart_agent`는 무시한다. | 제어 transport 일부만 보존됐다. [control.go](../internal/viewerservice/control.go) |
| 현재 Electron | 자신이 보낸 request ID와 일치하는 response만 처리한다. Service가 push한 `command-{id}`는 pending request가 아니므로 버린다. | 두 UI 명령도 실행되지 않는다. [managementPipe.ts](../viewer-app/src/managementPipe.ts) |
| renderer bridge | `resubscribe_stream` 수신 코드는 존재하지만 Electron main이 `viewer:command`를 emit하지 않는다. | 고립된 코드다. [preload.ts](../viewer-app/src/preload.ts), [viewerBridge.ts](../web/src/components/live/viewerBridge.ts) |
| 결과 보고 | 현재 Service에는 acknowledged/running/final result를 서버에 PATCH하는 reporter가 없다. | 명령이 실패해도 서버와 UI가 알 수 없다. |
| UI 상태 갱신 | 명령 생성 후 한 번 invalidate하며 active command 자동 polling이 없다. | 사용자가 수동 새로고침해야 한다. [streamsViewersSystemQueries.ts](../web/src/app/streamsViewersSystemQueries.ts) |

현재 실행 경로는 다음 위치에서 멈춘다.

`사용자 → DB pending → 서버 delivered → Service에서 무시 또는 Electron에서 폐기 → 결과 없음`

당시 모니터링 경로도 완전하지 않았다. Service가 Viewer closed 상태에서 heartbeat를 유지하고
Viewer·renderer 상태를 분리했지만, `stream_telemetry`는 local IPC에서 성공 응답만 하고 버렸으며
lease/renderer/progress 시각과 스트림 목록을 서버 heartbeat에 싣지 않았다. 따라서 **제어
migration뿐 아니라 stream monitoring adapter도 누락된 상태였다.**

### 첨부 화면 해석

화면의 배지 매핑은 실제 상태를 왜곡한다.

| 화면 표시 | 가능한 DB 상태 | 올바른 의미 |
|---|---|---|
| `실행 중` | `delivered`, `acknowledged`, `running` | 전달만 됐을 수도 있으므로 실제 실행 증거가 아니다. |
| `정보` | `pending` | 서버 큐 대기 상태다. |

[viewerFormat.ts](../web/src/pages/viewers/viewerFormat.ts)는 `delivered`, `acknowledged`,
`running`을 같은 배지로 합친다. 따라서 첨부 화면의 `#1 ping / 실행 중`은 전달 시각만 있는
`delivered` 명령으로 읽어야 하며 ping 성공이 아니다. `#2 restart_agent / 정보`는 `pending`이고,
현재 Service가 이 타입을 소비하지 않으므로 재시작 실행을 기대할 수 없다.

`취소`도 실제 실행 중단이 아니다. 서버는 `pending` 또는 `delivered` 레코드를
`cancelled`로 바꾸지만 이미 local queue에 들어간 명령을 회수하지 않는다. 취소는 local 수락
전까지만 허용하거나, 실제 cancellation protocol을 지원하는 명령에서만 제공해야 한다.

## 6. 회귀의 정확한 위치

과거 Electron 연결 모듈 `viewer-app/src/agentPipe.ts`에는 다음이 구현돼 있었다.

- server/Agent가 push한 `command` 이벤트 식별
- `onCommand` handler 호출
- `reload_live`, `resubscribe_stream`, `shutdown` 실행
- `command_result`를 Agent로 반환
- 명령 operation key 검증

다음 명령으로 삭제 diff를 재현할 수 있다.

```bash
git show c6ef57cd8ecc05c93fab9b7dc4bce6751812bd8c -- \
  viewer-app/src/agentPipe.ts \
  viewer-app/src/managementPipe.ts \
  viewer-app/src/main.ts
```

표준 MSI 커밋은 `agentPipe.ts`를 삭제하면서 위 기능도 삭제했다. 새
`viewer-app/src/managementPipe.ts`는 request-response pending map만 구현했고, Service push
command를 처리하는 별도 message type이나 handler를 추가하지 않았다. 기존 테스트도 새 pipe의
요청 응답과 Service queue를 각각만 검사했기 때문에 이 단절을 잡지 못했다.

Viewer restart 쪽은 별도 문제다. 2026-07-18 설계는 Service가 정상 운영 중 Viewer를 시작하거나
일반 process tree를 supervise하지 않는다고 명시했다. 이 결정은 “사용자가 수동 종료한 Viewer를
자동으로 다시 열지 않는다”는 desktop-app 동작에는 맞지만, “장애 시 서버가 원격 복구한다”는
제품 요구를 충족하지 못한다. 다음처럼 경계를 정정해야 한다.

- 정상적인 사용자 종료는 자동 재실행하지 않는다.
- 명시적이고 감사 가능한 서버 `start_viewer`/`restart_viewer` 명령은 실행할 수 있다.
- crash/unresponsive 자동 복구는 제한된 budget 안에서만 수행한다.
- 실행 대상은 MSI가 설치한 정확한 Viewer와 승인된 interactive session으로 고정한다.
- 임의 process, argument, shell, desktop-control 기능은 허용하지 않는다.

## 7. 목표 아키텍처

### CamStation server

- Viewer registry와 각 health axis를 저장한다.
- 명령을 타입별 schema로 검증해 durable queue에 넣는다.
- SSE를 우선 사용하고 bounded long-poll을 fallback으로 사용한다.
- 전달, 수락, 실행, 결과를 별도 상태로 보관한다.
- command TTL, cooldown, operation key, 감사 사유와 safe error code를 관리한다.
- Viewer UI 상태와 무관한 `restart_stream`은 서버 내부 stream manager로 직접 보낸다.

### Viewer Service

- Windows boot부터 실행되는 모니터링·제어 authority다.
- heartbeat sender와 command consumer는 같은 프로세스에 있어도 독립 상태 기계로 둔다.
- renderer가 죽어도 server control channel을 유지한다.
- command를 durable local ledger에 기록하고 중복 실행을 막는다.
- `ping`, diagnostics, update, lifecycle 명령을 직접 또는 narrow adapter로 조정한다.
- UI 명령은 active Viewer lease로 전달하고 결과를 server에 보고한다.

### Viewer lifecycle adapter

복구 구현에는 이 역할이 추가됐다. Windows session 0 Service가 임의 GUI를 띄우는 방식이 아니라,
MSI가 설치한 Service 실행 파일과 인접한 정확한 Viewer만 active console session에서
시작·재시작하는 좁은 실행 경계다. 다음 계약을 지킨다.

- PC에 사용자가 로그인한 상태라면 죽은 Viewer를 서버 명령으로 다시 시작할 수 있다.
- 로그인 세션이 없으면 `not_logged_in`으로 거부하고 성공으로 표시하지 않는다.
- target generation, PID/session, lease, renderer ready까지 확인한 뒤 성공한다.
- restart budget과 cooldown은 Service 재시작 후에도 유지한다.
- Service 자체 재시작은 helper/SCM recovery와 다음 boot generation 조정을 사용한다.

### Electron main과 Web renderer

- Electron main은 unsolicited `command` 이벤트를 request response와 별도로 수신한다.
- `reload_live`는 현재 승인된 live URL만 다시 로드한다.
- `resubscribe_stream`은 renderer에 stream name만 전달한다.
- renderer는 해당 stream pipeline만 재생성하고 결과를 Electron으로 반환한다.
- Electron은 결과를 Service로, Service는 동일 command ID/operation key로 서버에 보고한다.

## 8. 목표 사용자 흐름

1. 사용자는 표시명, PC명, Service/control/Viewer/renderer 상태가 보이는 단일 선택 상자에서
   Viewer를 고른다.
2. 선택한 Viewer 버전과 현재 상태가 지원하는 기능만 한국어 이름으로 표시한다.
3. 기능 설명에는 실행 대상, 영향, 예상 완료 조건을 표시한다.
4. 관련 입력만 받는다. 스트림 기능은 해당 Viewer에 배치된 카메라를 선택하게 한다.
5. restart/update 같은 disruptive action은 한 번 더 확인하고 감사 사유를 남긴다.
6. 실행 후 `대기 → 전달됨 → 수락됨 → 실행 중 → 완료/실패/거부/만료`를 자동 갱신한다.
7. 실패 시 safe error code, 실패한 계층, 재시도 가능 여부를 보여준다.

공통 `경로`와 `메시지` 입력은 제거한다. `reload_live`가 임의 URL 이동 기능이 되어서는 안 된다.
메시지가 필요하면 실행 payload가 아니라 `실행 사유`라는 감사 필드로 분리한다.

권장 사용자 기능명은 다음과 같다.

| 화면 이름 | 내부 명령 | 사용자 입력 |
|---|---|---|
| 제어 연결 확인 | `ping` | 없음 |
| 라이브 화면 새로고침 | `reload_live` | 없음 |
| 카메라 영상 다시 연결 | `resubscribe_stream` | 카메라 1개 |
| 서버 스트림 다시 시작 | `restart_stream` | 카메라/stream 1개, 확인 |
| Viewer 앱 시작 또는 다시 시작 | `restart_viewer` | 확인, 감사 사유 |
| Viewer 관리 서비스 다시 시작 | `restart_service` | 관리자 확인, 감사 사유 |
| Viewer 진단 수집 | `capture_diagnostics` | 선택적 safe 사유 |

## 9. 구현 우선순위

### P0 — 제품 계약과 상태 표시 정정

- 서버 원격 제어를 필수 Viewer 기능으로 문서와 구현 상태에 고정한다.
- `delivered`를 `실행 중`으로 표시하지 않고 모든 상태를 정확히 번역한다.
- 동작하지 않는 명령은 삭제하지 말고 `현재 버전에서 실행 불가`로 명확히 표시한다.
- server-side command whitelist와 타입별 입력 검증을 추가한다.

### P1 — 기본 제어 폐루프 복구

- Service가 `ping`을 직접 처리하고 결과를 PATCH한다.
- management IPC에 unsolicited `command`와 `command_result`를 복원한다.
- Electron `reload_live`와 renderer `resubscribe_stream`을 연결한다.
- active command 상태를 자동 갱신하고 error/result를 UI에 표시한다.
- durable operation key와 local dedupe로 at-least-once delivery를 안전하게 처리한다.

### P2 — PC 무접촉 lifecycle 제어 복구

- 표준 MSI와 호환되는 narrow Viewer lifecycle adapter를 설계·구현한다.
- `restart_viewer`를 새 process/session/lease/renderer ready까지 검증한다.
- `restart_agent`를 현 구조의 `restart_service`로 이름과 조정 방식을 갱신한다.
- 정상 사용자 종료, crash 자동 복구, 명시적 서버 restart를 서로 다른 정책으로 처리한다.

### P3 — 운영 기능 완성

- `restart_stream`과 `capture_diagnostics`를 Viewer 제어 화면에 명확히 구분해 제공한다.
- background TTL expiration, newest-first pagination, 감사 기록과 cooldown을 추가한다.
- server-directed update는 실제 MSI update/rollback/result reconciliation이 완성된 뒤 시스템
  작업으로 노출한다.

### P4 — 실제 Windows 폐루프 검증

- server API → Service → management pipe → Electron/renderer → result PATCH → UI를 한 테스트로
  연결한다.
- Viewer 정상, renderer hang, Viewer crash, Service restart, server outage, PC 로그인 없음,
  duplicate delivery, TTL expiry, cancellation 경계를 검증한다.
- 실제 Windows 10/11 설치본에서 명령별 화면·process·server DB 증거를 남긴다.

## 10. 완료 조건

다음 조건이 모두 충족돼야 Viewer 원격 제어 기능이 완료된 것이다.

- PC에 직접 접속하지 않고 서버에서 대상 Viewer를 선택해 기능을 실행할 수 있다.
- 모니터링 heartbeat와 control-channel 건강성이 별도로 표시된다.
- `ping`, `reload_live`, `resubscribe_stream`이 실제 대상에서 한 번 실행되고 최종 결과가 돌아온다.
- 죽은 Viewer는 로그인된 승인 세션에서 서버 명령으로 다시 시작할 수 있다.
- Service 재시작 명령은 재기동 후 같은 command ID를 조정해 최종 결과를 남긴다.
- 전달만 된 명령을 실행 중이나 성공으로 표시하지 않는다.
- 중복 전달, Service/Viewer restart, 네트워크 단절에도 side effect가 중복되지 않는다.
- 취소 가능 시점과 실제 취소 효과가 UI 설명과 일치한다.
- 임의 URL, process, shell, 파일, credential을 원격 제어 payload로 전달할 수 없다.
- Windows 실기에서 장애 주입과 복구까지 검증된다.

## 11. 복구 전 분석 검증 범위

복구 구현 전 분석에서는 다음을 확인했다.

- 현재 및 전체 Git history의 Viewer 관련 Markdown 파일명과 내용
- 최초 `ee6879c` 사양 원문
- 2026-07-16 Agent control 설계와 구현 보고서
- 2026-07-18 표준 MSI 승인 설계·로드맵·runtime plan
- `06f8a5c` Service control migration과 `c6ef57c` Electron pipe 교체 diff
- 현재 Web UI, server route/store, Viewer Service, Electron/renderer 연결 코드

당시 기존 자동 테스트는 모두 통과했다.

- Web: 55 tests passed
- Viewer app: 35 tests passed
- Go: `internal/store`, `cmd/camstationd`, `internal/vieweragent`, `internal/viewerservice` passed

이 테스트들은 당시 각 조각만 검증했고 표준 아키텍처의 전체 명령 폐루프는 검증하지 않았다.
이 최초 분석 단계에서는 실제 Viewer에 명령을 보내거나 외부 Viewer/server 상태를 변경하지
않았다.

복구 전 판정은 다음과 같았다.

> Viewer 모니터링과 원격 제어는 원래부터 서로 분리된 필수 제품 계층이다. 현재 표준 MSI는
> 모니터링 계층과 제어 transport 일부만 이식했고, UI 실행 결과와 lifecycle 제어를 누락했다.
> 해결 방향은 명령 UI를 폐기하는 것이 아니라, Service 중심 제어 plane과 좁은 Windows
> lifecycle adapter를 복구해 서버에서 실제 실행·결과 확인이 가능하게 만드는 것이다.

## 12. 복구 구현 및 WinPC 검증 결과

2026-08-10 복구 작업으로 위 판정의 P0~P2 범위를 구현하고 Windows 11 실기에서 확인했다.

### 현재 사용자 기능

| 화면 기능 | 내부 명령 | 실제 동작과 성공 조건 |
|---|---|---|
| 제어 연결 확인 | `ping` | Service가 같은 명령 ID를 내구성 있게 처리하고 성공 결과를 보고한다. |
| 라이브 화면 새로고침 | `reload_live` | Electron이 현재 승인된 CamStation live URL만 다시 로드하고 완료를 반환한다. |
| 카메라 영상 다시 연결 | `resubscribe_stream` | 선택한 Viewer가 실제 보고한 스트림 하나만 renderer에 전달해 재구독한다. |
| Viewer 앱 시작 또는 다시 시작 | `restart_viewer` | 활성 lease가 있으면 Electron relaunch를 사용하고, 없으면 로그인된 console session에서 MSI 인접 Viewer만 시작한다. 새 lease와 renderer ready가 확인돼야 성공한다. |
| Viewer 관리 서비스 다시 시작 | `restart_service` | 고정 SCM 서비스만 helper로 재시작한다. 다음 Service boot generation이 원래 명령을 조정하고 control 재연결 뒤 성공을 보고한다. |

`/viewers`에서는 등록 Viewer와 한국어 기능을 선택한다. 스트림 기능에만 해당 Viewer가 보고한
stream selector가 나타나고, 두 재시작 기능에만 감사 사유와 2단계 확인이 나타난다. 서버는
allowlist 밖의 명령, 임의 Viewer ID, URL 형태 stream, route/mode/update 필드, 관련 없는 필드와
범위를 벗어난 TTL을 거부한다. 명령 행은 `pending`, `delivered`, `acknowledged`, `running`과 모든
terminal 상태를 서로 다른 한국어 상태·시각으로 표시하며 active 명령만 자동 갱신한다.

### 모니터링과 제어의 독립성

Service heartbeat와 명령 consumer는 renderer와 독립적으로 계속 동작한다. Service는 Viewer
lease, renderer heartbeat/progress, bounded stream telemetry를 snapshot에 저장해 서버 heartbeat로
전달한다. Web renderer는 긴 재연결 cooldown 중에도 현재 stream 상태를 5초마다 다시 보고하므로
offline 스트림도 서버에서 식별하고 `resubscribe_stream` 대상으로 선택할 수 있다. Viewer 연결이
끊기면 lease 소유 stream은 제거돼 오래된 상태가 정상처럼 남지 않는다.

제어 plane은 bounded atomic journal에 수락·실행·결과를 먼저 기록한다. 같은 payload의 중복 전달은
side effect를 반복하지 않고 저장 결과를 다시 보고하며, 같은 ID의 변경된 payload는 거부한다.
Service restart는 재시작 전에 목표 boot generation을 기록하고, 새 Service가 서버의 재전달을
기다리지 않고 보고되지 않은 원래 terminal 결과를 다시 보고한다.

### 검증 증거

- `./scripts/check-dev.sh` 전체 통과: 모든 Go package, Web 58 tests, Viewer 36 tests, Web
  lint/build, Viewer build, embedded Web 갱신, daemon build.
- Windows 11에서 소스와 동일한 unsigned development MSI `2.0.24`를 native build/install했다.
  결과물은 `124436480` bytes, SHA-256
  `5e4a7b59bc457fb9c3dbace25db58009c61e2b6258957f123d7cb4ff30683160`이며 WiX build는 warning과
  error 없이 끝났다.
- 정상 서버 API 경로로 다섯 명령이 모두 terminal `succeeded`에 도달했다.
  `restart_viewer` 전후에는 Viewer process 집합이 전부 교체됐고 새 lease/renderer ready가
  확인됐다. `restart_service` 전후에는 Service PID와 boot generation이 바뀌었고 기존 Viewer를
  중복 재시작하지 않은 채 control, renderer, stream 상태가 복구됐다.
- 실제 운영 화면에서 Viewer와 기본 기능 `제어 연결 확인`을 선택하고 키보드로 실행했다. 생성
  요청은 HTTP 201이었고 같은 행이 `succeeded`와 실제 단계별 시각으로 자동 갱신됐다.
- disposable offline 카메라를 화면에 띄워 `cooldown/webrtc`와 `fallback/mse` stream telemetry가
  서버에 도달하는 것과 그 스트림의 targeted resubscribe를 확인했다.

실기 검증 후 임시 서버·DB·카메라, Viewer 설정, 명령 journal/history와 화면 증거 디렉터리를
삭제했다. WinPC에는 검증한 `2.0.24` 설치와 자동 시작 Service만 남겼고 Viewer 앱은 종료된
미설정 상태다.

### 남은 범위

- MSI는 개발용 unsigned artifact다. 운영 배포 전 Authenticode signing과 장시간 Windows soak가
  필요하다.
- 서버 측 `restart_stream`은 기존 Streams 화면의 별도 기능으로 유지된다.
- `capture_diagnostics`와 Viewer 자동 업데이트는 이번 운영자 명령 allowlist에 포함하지 않았다.
- Windows가 Service stop을 수락한 뒤 재시작 자체에 실패하면 정지된 Service는 결과를 보고할 수
  없다. 이 경우 명령은 성공으로 표시되지 않으며 외부 SCM recovery가 필요하다.
- 정상·offline 경로와 주요 재시작/중복/TTL/취소/결손 경계는 자동화와 실기로 검증했지만, 모든
  장애 조합을 포함하는 장시간 fault-injection 운전은 별도 운영 검증 범위다.

현재 판정은 다음과 같다.

> Viewer 모니터링과 원격 제어는 분리된 계층으로 복구됐다. 사용자는 서버에서 등록 Viewer와
> 허용된 기능을 선택해 실행하고, 전달이 아니라 실제 실행의 최종 결과를 확인할 수 있다.
> Viewer와 관리 Service 재시작도 임의 PC 조작 없이 고정된 Windows lifecycle 경계 안에서
> 실행되고 재연결 증거가 있어야 성공한다.
