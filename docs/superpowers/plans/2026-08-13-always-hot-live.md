# Always-hot 라이브 구현 계획

## 1. live 출력 상시 준비 계약

- [x] frontend·server·store 신규 기본값을 warm H.264 1280×720 15fps `always`로 고정하는 실패 테스트를 작성한다.
- [x] 기존 DB live output의 source/video/limit/audio는 보존하고 activation 및 applied snapshot만 idempotent하게 `always`로 정규화한다.
- [x] legacy/canary importer도 source plan과 staged DB 양쪽에서 live activation을 `always`로 생성해 검증 불일치를 만들지 않는다.
- [x] config renderer에서 live private/public 정적 preload를 제거하고, non-live `always` 정책만 기존 의미를 보존하는지 검증한다.
- [x] 설정 UI에서 live 실행 정책은 “항상 준비” 고정값으로 표시하고 on-demand 저장을 만들지 않는다.

## 2. 서버 소유 병렬 warm consumer

- [x] enabled camera의 public live output을 정확히 한 번 선택하는 순수 spec 테스트를 작성한다.
- [x] 로컬 go2rtc RTSP를 video-copy/null로 계속 읽는 FFmpeg 명령을 비밀정보 없이 생성한다.
- [x] 8개 worker가 동시에 시작하고, 한 worker의 연결 대기가 다른 worker 시작을 막지 않는 실패 테스트를 작성한다.
- [x] worker 종료 시 해당 stream만 제한된 backoff로 재시작하고 Reconcile/StopAll이 정확한 worker만 변경한다.
- [x] go2rtc status에 expected/ready live 수와 `mediaReady`를 추가하되 FFmpeg warm consumer는 viewer로 세지 않는다.
- [x] daemon 시작·카메라 변경·go2rtc 재시작 후 DB의 현재 enabled live 집합으로 manager를 계속 reconcile한다.

## 3. 집중보기 연결 재사용

- [x] `/viewer`와 `/live`의 focus presentation model 테스트를 추가한다.
- [x] `/viewer` grid를 항상 mount한 채 선택 타일만 fullscreen class로 전환한다.
- [x] `/live`의 별도 zoom tile/suspend를 제거하고 기존 grid tile wrapper를 확대한다.
- [x] 집중 전환이 playback candidate key를 바꾸지 않도록 한다.

## 4. 정적·통합 검증

- [x] Web tests/lint/build와 전체 Go tests/build, `git diff --check`를 통과한다.
- [x] 전용 로컬 Docker image/compose를 build하고 운영과 겹치지 않는 포트·volume·container 경계를 확인한다.
- [x] health/API/config-safe 상태와 viewer 0명에서 `mediaReady=8/8`, public producer 유지 상태를 확인한다.
- [x] 8 warm process가 독립적으로 시작하고 한 항목의 초기 대기가 다른 항목의 시작·복구를 막지 않는지 확인한다.
- [x] 로컬 live warm consumer 하나를 종료해 해당 worker만 자동 복귀하고 나머지 7개 producer가 유지되는지 확인한다.
- [x] 브라우저 `/live`·`/viewer`와 Windows 클라이언트 실제 경로 `/live?viewer=1`에서 실제 영상,
      focus 전후 session/consumer 유지와 시간을 측정하고 screenshot을 남긴다.
- [x] tasks Review와 lessons를 갱신하고 개발 주소·검증 결과·남은 한계를 기록한다.
