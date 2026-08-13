# Always-hot 라이브와 무재연결 집중보기 설계

> 2026-08-13 재점검: 아래 초안의 `private + public go2rtc preload` 방식은 목표를 충족하지
> 못하는 것으로 판정했다. go2rtc 1.9.14는 정적 preload를 순차 실행하고 초기 AddConsumer
> 실패 항목을 준비 상태로 유지하지 않는다. 구현을 더 진행하기 전에 아래의 수정된 제품 계약과
> 서버 소유 warm-holder 구조로 교체한다.

## 목표

CamStation의 카메라별 브라우저용 `live` 출력을 첫 준비 이후 서버가 계속 유지하고, `/live`와
`/viewer`의 다중보기↔집중보기 전환이 현재 재생 세션을 폐기하거나 새 세션을 만들지 않게 한다.
카메라→서버 ingest는 기존 정상 동작을 전제로 하며, 이번 변경과 합격 판단은 warm 완료 이후의
서버→클라이언트·브라우저 구간에 한정한다.

## 범위와 안전 경계

- 개발은 `feature/always-hot-video` 전용 Git worktree에서만 수행한다.
- 운영 서버, 운영 컨테이너, 운영 DB, 카메라 설정 및 Windows Viewer 설치 상태는 변경하지 않는다.
- 로컬 Docker는 전용 container/project 이름, HTTP/WebRTC 포트, state/media 디렉터리를 사용한다.
- 카메라 URL·자격증명, raw go2rtc JSON 및 전체 프로세스 인자는 로그·문서·공개 API에 노출하지 않는다.
- 녹화와 focus 출력의 저장 정책은 변경하지 않는다. 기존 카메라의 live 소스·화질·FPS 정책도
  보존하며, live activation만 항상 준비로 정규화한다.

## 서버 동작

### 항상 준비된 live 출력

서버가 enabled 카메라별 public `live`에 하나의 내부 warm consumer를 소유한다. 8개 consumer는
go2rtc 기동 직후 서로 독립적으로 병렬 시작하고, 브라우저 수가 0이어도 종료하지 않는다. public
`live` producer가 source 연결과 browser-safe 변환을 소유하며 브라우저는 이미 실행 중인 producer에
새 consumer로만 붙는다.

go2rtc 정적 preload는 이 계약의 실행 수단으로 사용하지 않는다. private source와 public live를
각각 preload하면 항목 수가 중복되고, go2rtc 1.9.14의 동기식 순차 AddPreload 때문에 느린 카메라가
다른 카메라의 시작까지 막는다. 서버 warm consumer manager는 정확히 public live 1개/카메라만
병렬 유지하고, 그 연결이 필요에 따라 private source를 시작하게 한다.

신규 카메라의 권장 live 정책은 browser-safe H.264, 최대 1280×720·15fps, 무음, `always`다. 이미
저장된 카메라는 명시적으로 선택한 source/video/size/FPS/audio 정책을 보존하고 activation만
`always`로 정규화한다.

이 구조에서 브라우저 접속 때 카메라 연결이나 public FFmpeg 변환기를 시작하지 않는다. 클라이언트별
WebRTC/MSE transport 협상은 여전히 필요하지만, 서버-side cold start와 첫 변환 keyframe 생성은
클라이언트 요청 전에 완료되어 있어야 한다.

`/api/health`와 media readiness를 분리한다. media ready는 활성 카메라 모두에서 public live
producer 및 서버 소유 consumer가 존재할 때만 참이다. 일부 실패는 daemon healthy가 아니라 media
degraded로 보고하며, 실패 카메라가 다른 카메라 준비를 직렬로 막지 않는다.

## 클라이언트 동작

### `/viewer`

8개 `ViewerVideo`를 항상 같은 React key와 DOM 위치에 유지한다. 집중보기는 선택 타일에 fullscreen CSS
class를 적용하고 나머지 타일을 시각적으로 숨길 뿐 hook을 unmount하지 않는다. 닫을 때 class만 되돌린다.
집중 여부는 playback candidate 순서를 바꾸지 않는다.

### `/live`

React Grid Layout 안의 선택된 기존 `CameraTile` wrapper를 CSS로 grid 전체 크기로 확대한다. 별도
zoom-layer `CameraTile`을 만들지 않고, 원래 타일도 suspend하지 않는다. 따라서 선택 카메라와 나머지
카메라의 `LiveVideo` hook, session ID, WebRTC/MSE 연결이 모두 유지된다. 집중보기는 영상 화질 전환이
아니라 현재 warm live 영상의 표현 전환이다. `focus`는 live 장애 시 기존 fallback 후보로 남는다.
Windows Viewer가 실제 여는 `/live?viewer=1`도 동일한 `LiveWorkspace`와 연결 유지 계약을 사용한다.

## 합격 기준

- viewer 0명에서도 활성 8대의 public live producer와 서버 소유 warm consumer가 계속 존재한다.
- 8대 warm consumer는 병렬 시작한다. 전체 준비 시간은 8대 시간의 합이 아니라 가장 느린 단일
  카메라의 준비 시간에 수렴하며, 한 카메라 실패가 나머지 7대 시작을 막지 않는다.
- media ready가 된 이후 새 클라이언트는 카메라/변환기 프로세스를 만들지 않고 기존 producer에만
  붙는다. 서버 준비 시간과 클라이언트 transport 협상 시간을 별도로 기록한다.
- 신규/수정 정책의 live activation은 `always`이고 신규 권장 profile은 H.264 1280×720 15fps다.
- public warm consumer가 끊기면 브라우저 요청 없이 해당 카메라 consumer만 재시작하고 다른
  producer와 전체 go2rtc는 유지한다.
- `/viewer`, `/live`, `/live?viewer=1`에서 다중→집중→다중 전환 전후 모든 playback session ID와
  server viewer count가 유지된다. 선택 영상의 `currentTime`도 계속 증가한다.
- 로컬 Docker health, public camera/stream API, warm producer 유지, 브라우저 실제 영상과 전환을
  확인한다. 진단용 브라우저와 로컬 컨테이너의 잔여 세션을 명시적으로 정리하거나 사용자에게 실행
  상태를 인계한다.
