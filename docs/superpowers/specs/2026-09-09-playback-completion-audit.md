# 라이브·녹화 개발 완료 독립 감사

**현재 최종 판정 — 2026-09-09 21:39 KST 이후 재감사: 기존 P1 두 건이 해소되어, 합의된 개발·필수 검증 범위는 완료다.** Viewer 전체 55/55 통과와 수정 MSI 2.0.30의 테스트 PC 관리 연결 복구·녹화 상태 보존을 확인했다. 아래 최초 감사의 미완료 판정은 당시 기록으로 보존하며, 현재 근거는 문서 끝의 「후속 재감사」를 따른다. 운영 PC 적용·운영 배포는 수행하지 않았다.

감사일: 2026-09-09. 대상: 현재 미커밋 워킹트리의 라이브·녹화 기능. 기준: [R/E/B/V 분석](2026-09-09-playback-screen-to-backend-analysis.md)의 최신 축소 범위와 [구현 계획](../plans/2026-09-09-playback-screen-to-backend-plan.md). 구현 담당의 완료 보고와 구분하여 소스·검사 결과·기존 Windows 원본 증거를 대조했다. 제품 수정, 배포, PC 조작, 카메라 추가 연결, 전체 검사 반복은 하지 않았다.

## 판정

**핵심 기능 구현과 공통 재생·Windows 화면 검증은 확인되지만, “모든 개발 구현·필수 검증이 끝났다”는 판정은 아직 불가하다.** 필수 Viewer 검사에 재현되는 실패 1개가 남아 있다. 관리 연결 복구 수정은 소스·단위 검사·빌드까지 확인했으나 수정된 네이티브 코드의 실기기 검증은 없다. 따라서 소스 구현 완료, 필수 검사 전체 통과, 수정된 설치물의 검증 완료를 구분해야 한다.

이번 감사에서 새로운 재생·녹화 운영 차단 결함은 확인하지 못했다. 운영 미배포는 사실이지만, 배포를 개발 감사의 추가 완료 조건으로 만들지 않는다. 부하·장시간·crash·호환 행렬·정밀 동기 오차 검사는 합의된 제외 범위다.

## 우선순위 발견

| 우선순위 | 확인 사실과 근거 | 완료 판단에 미치는 영향 |
| --- | --- | --- |
| P1 · 검사 완료 | `viewer-app/tests/winPcGuiSkill.test.ts:24`의 기존 소개 문구 정규식과 현재 `.agents/skills/control-camstation-windows-pc/SKILL.md:3`이 불일치한다. 감사 중 작은 재검에서도 같은 실패를 재현했다. 기존 전체 결과는 54/55 통과다. | 기능 회귀라고 단정할 근거는 없지만, 계획 종료 기준의 **필수 검사 전체 통과는 미충족**이다. 기존 무관한 실패라는 설명을 전체 PASS로 대체할 수 없다. 문구 검사와 실제 스킬 계약의 정합성을 별도 변경으로 해결할 잔여다. |
| P1 · 실기기 검증 범위 | `viewer-app/src/main.ts:367–370`은 현재 녹화 문서도 `isNavigationAllowed`로 보존하도록 바뀌었다. `viewer-app/tests/mainLifecycle.test.ts:49`는 실제 predicate와 lifecycle 분기를 stub 환경에서 검사하며 감사 재검도 통과했다. 테스트 PC의 MSI 2.0.29에는 이 수정이 없다. | **R11/E20/B07/V07은 소스 수준 확인, 수정 설치물의 실기기 확인은 미완료**다. 2.0.29에서 확인한 녹화/전체화면 동작을 새 native 복구 코드의 실행 증거로 사용할 수 없다. 새 설치물 확인은 향후 패키징·테스트 PC 적용 시 해당 복구 경계만 확인하면 되며 포괄적인 장애 캠페인은 불필요하다. |
| P3 · 표시 후순위 | 서버 삭제 응답 410은 검사되지만 `web/src/components/playback/usePlaybackWorkspace.ts:103–105`는 조회 실패를 일반 오류로 표시한다. `PlaybackControls.tsx:93–113`에는 지원 음성 여부에 따른 사용 불가 표시가 없고, `RecordedVideo.tsx:53–58`에는 재생 중 `waiting` 표시를 바꾸는 handler가 없다. | 분석의 세부 표시 계약 전부가 구현되었다고 표현하면 과장이다. 실제 재생 차단을 확인한 사항은 아니며 최신 범위의 표시 개선 후순위로 분리한다. 이것만으로 이번 종료를 막거나 기능 검사를 추가하지 않는다. |

## 완료 기준별 대조

| 기준 | 실제 구현 근거 | 확인 근거와 판정 |
| --- | --- | --- |
| V01 / R01~02 / B 화면·목록·탭 | `RecordingBrowserWorkspace.tsx`, `recording-browser.css`, `RecordingsPage.tsx`, `ConsoleLayout.tsx`. 플레이어 인스턴스를 유지하고 관리 탭 진입 시 pause·mute. 초기에는 `hasSelection=false`. | 로컬 B/드로어 화면 자료, 관리 탭 왕복 실행 결과, Windows 최종 B 이미지에서 큰 영상·우측 독립 목록·하단 시간축·단일 메뉴 확인. **핵심 확인**. 모든 기존 관리 버튼을 새로 조작한 것은 아님. |
| V02 / R03~04 / 공통 시각·최신 seek | `usePlaybackWorkspace.ts` 공통 시각·요청 취소·generation 검사, `LiveWorkspace.tsx:136` 최신 PTZ Stop 완료만 반영. `PlaybackTimeline.tsx` 후보 drag 후 확정. | 로컬 두 영상 절대시각 환산 결과와 Windows 두 영상 녹화 탐색·pause 자료. **핵심 확인**. 정밀 오차 SLA의 증거는 아님. |
| V03 / R05~06 / 집중 보기·전체화면·LIVE | 동일 workspace를 유지하는 Live grid/focus, `goLive`, `viewerBridge.ts` native 확인 상태 복원. | Windows 19:37~19:39 KST 원본 이미지·`complete.json` 확인. 복귀 그리드 pause 시각 18:34:34 KST, B 전체화면 종료/재진입 시 18:51:04 KST 유지와 제목 표시줄 전환을 직접 대조했다. **확인**. |
| V04 / R07,R09 / 제어·공백·다음·retry·최근 대기 | 공통 controls/clock, `nextRecording`, 카메라별 retry, `edge_wait` polling, adapter 준비 timeout. | 로컬 pause/rate/관리 탭·공백→다음·수신 차단 후 retry 실행 결과, clock 검사와 Windows play/pause. **핵심 확인**. 새 조각 대기 overlay의 직접 화면 확인은 없으며 표시 차이는 위 P3에 기록. |
| V05 / R08 / B03~05 / 녹화 중 재생·파일 전환·legacy | PRFT 기반 `internal/recordingmedia`, `internal/recorder/media.go`, 공개 확정 offset, HLS manifest와 legacy file adapter. | `media-probe.json`은 작성 중 두 입력에서 실제 프레임 디코딩, `legacy-probe.json`은 `legacy_filename`/8초 탐색/readyState 4. `TestGrowingRecordingPublishesPacketTimeAndFinalizesSameMedia` 및 정상 다음 파일 이동 실행 결과. **확인**. |
| V06 / R10 / 기존 배치·줌·관리·PTZ | `LiveWorkspace.tsx` 기존 handlers 유지, 재생 전환 PTZ Stop 및 playback 비활성, `RecordedVideoViewport.tsx` transform 줌/팬, 관리 패널 재사용. | 변경 diff·관련 기존 Go/Web 검사, 로컬 줌/초기화·관리→같은 플레이어 흐름. **변경 경계 확인**. 모든 PTZ 프리셋·관리 CRUD의 실카메라 재조작은 하지 않았고 필수 조건도 아님. |
| V07 / R11 / B07 / 관리 연결 복구 | 위 P1 native predicate와 actual-source stub 검사. | **소스 검사 통과 / 수정 native 실기기 미확인**. |
| V08 / B01~06 / 데이터·API·삭제 보호 | `internal/store/playback*.go`, `routes_playback*.go`, immutable prefix 검사, media open/move lock, 기존 미백업 보호. | 기존 전체 Go PASS 로그와 store/route/recorder 테스트 본문을 대조. 겹침·반개구간·안정 cursor·보관 카탈로그·마감 이동·삭제 410·미완성 조각 비공개·backup pending 보존 포함. **확인**. 실제 검사에서 지원되지 않은 코덱 때문에 재생이 막힌 증거가 없어 B06 범용 변환 미구현은 누락으로 보지 않음. |
| V09 / R04,R08 / 최대 2대 통합·수신 복구 | 격리 서버·기존 중계 2개와 같은 공통 adapter. | 로컬 수신 route abort→오류→차단 해제→retry의 실제 결과에서 전/중/후 커서 모두 `1788946219997`, 두 영상 paused 유지, errors 없음. **수신 측 미디어 복구 확인**. 네이티브 관리 연결 복구와 다른 검사다. |

## 필수 검사 감사

| 검사 | 대조 결과 |
| --- | --- |
| Go 전체 | `work/playback-integration/go-test.log`의 전체 패키지 PASS 확인. 신규 media/store/route 테스트가 요구를 검사하는지도 본문 대조. 반복 실행하지 않음. |
| Web 검사 | 최초 로그는 92/93. 실패한 기존 외부 플레이어 구조 검사는 현재 playback-only Viewer 계약으로 수정됨을 대조했고, 감사 재검에서 해당 테스트 통과. 최종 fullscreen bridge 15개와 clock 5개도 감사 재검 통과. |
| Web lint/build + daemon build | lint PASS 결과 및 최종 19:32 KST Web 빌드의 `index--UASJ6d6.css`, `index-CD77_dwR.js`와 현재 산출물 일치 확인. 이어진 daemon build exit 0 확인. 저장된 `web-build.log` 자체는 이전 산출물 로그이므로 그것만을 최종 빌드 증거로 사용하지 않음. |
| Viewer test/build | 전체 54/55, 소개 문구 실패 잔여. 수정 predicate의 소스 검사 통과와 수정 후 `build/main.js` 확인. build PASS를 전체 test PASS로 해석하지 않음. |
| 감사 중 작은 재검 | `node --experimental-strip-types --test`로 `mainLifecycle.test.ts`, `winPcGuiSkill.test.ts`, `recordingViewerPolicy.test.ts`, `viewerBridge.test.ts`, `playbackClock.test.ts`만 실행: **30개 중 29개 통과, 위 소개 문구 1개 실패, exit 1**. 전체 스위트/실카메라 검사를 반복하지 않음. |

Windows 직접 대조 원자료: `work/windows-control-evidence/test-pc/control-20260909T103653523Z-621332ee9c104a05887464da3d5642eb`, `control-20260909T103758738Z-65702047e0e24ad582cb11d3a7cd1163`, `control-20260909T103905239Z-2348fd9107804e7aa2c12d7e1a4a82f7`의 `complete.json`과 이미지. 최종 `enterAfter.png` SHA-256은 `745eb5c38f0ef44ed84d02faf3eb54a4e8a8c1505134dfada9030bc99fba9c18`이다. 자동 클릭 반환의 `unverifiable`나 assertion 개수만으로 시각 검증을 대신하지 않았다.

남은 판단은 **기존 문구 검사 실패 해결**과 **native 수정이 포함된 설치물의 해당 복구 동작 확인**이다. 나머지 핵심 흐름을 다시 전수 검사할 근거는 이번 감사에서 발견하지 못했다.


## 후속 재감사 — 2026-09-09 21:39 KST 이후

사용자의 계속 진행 지시에 따라 최초 감사의 P1 두 건을 해결한 결과만 다시 대조했다. 감사자는 제품 코드·PC 상태를 변경하거나 검사를 추가 실행하지 않았다. 최초 감사에서 확인한 공통 재생·Go/Web 검사·Windows 화면 결과는 유지하고, 후순위 P3 표시 차이를 새 종료 조건으로 확대하지 않았다.

| 기존 잔여 | 독립 대조한 원자료와 결과 | 후속 판정 |
| --- | --- | --- |
| Viewer 필수 검사 1건 실패 | `viewer-app/tests/winPcGuiSkill.test.ts` diff를 검토했다. 고정 소개 문구를 frontmatter의 명시적 대상·권한·기능·경계, 필수 runbook 링크와 metadata 검사로 교체했다. 기존 보안·타깃 선택·정리·정확한 창 캡처 검사는 유지했고 skip/only로 우회하지 않았다. 로컬 전체 실행 결과는 **55/55 통과, fail 0, skipped 0**이며 Windows canonical 빌드 중에도 **55/55 통과**했다. | **해소.** 검사 계약을 약화한 변경으로 볼 근거 없음. |
| 수정 native 코드의 설치물·실기기 미확인 | `native-2.0.30-source-manifest.json`의 7개 파일은 현재 승인 소스 해시와 일치한다. `native-2.0.30-build-result.json`과 `native-2.0.30-installed-proof.json`에서 설치 MSI **2.0.30**, EXE/앱 번들 해시 일치, 설치 ASAR의 새 복구 predicate 포함을 확인했다. 이어 아래 실제 관리 연결 복구와 UI 전후를 대조했다. | **해소.** R11/E20/B07/V07의 수정 설치물 복구 경계 확인 완료. |

설치물은 기준 commit `45c478947a0275a4ae0a5c8accdd9c36914986bb`에 명시적 7개 파일 overlay를 더한 dirty source 빌드다. EXE/package의 고정 `2.0.0`을 MSI 설치 버전으로 오인하지 않았다. MSI SHA-256은 `6c36a335ac326e64bed04fa5ec482fae75fd570bc5612682ecb55a4f916fc0be`, 설치 앱 번들 SHA-256은 빌드와 같은 `d22fb21cf0b16989692e6ab877a832d753a50f9de012b18e254213961ff60d67`이다.

실제 복구 검증은 테스트 PC에서만 수행됐다. `native-recovery-cycle-result.json`에서 **21:38:11~18 KST** 관리 서비스 정상 중지·시작과 PID `8776 → 440`, Viewer PID `7044` 및 session `2` 보존을 확인했다. `native-recovery-log-result.json`에는 **21:38:20 KST** `management_recovered`, `running`, `pipe_closed`, `durationMs=8054`, `reconnectCount=2`가 기록됐다. 단순 화면 유지뿐 아니라 관리 연결이 실제로 끊겼다가 복구됐음을 구분할 근거다.

전후 원본 `complete.json`과 이미지를 직접 대조했다. 동일 Viewer PID `7044`, window `263010`이며, 양쪽 모두 B 녹화 화면의 **집-마당 / 18:51:00–18:52:00 선택 구간 / 재생 시각 18:51:04 KST / 일시정지**를 유지했다. 녹화 화면이 setup 또는 LIVE로 교체되지 않았다. 두 이미지의 실제 로컬 SHA-256도 반환값과 일치한다.

- 전: `work/windows-control-evidence/test-pc/control-20260909T123715910Z-0a79ca07d0cf4b8fbe221156358a97a4/native-recovery-before.png`, SHA-256 `a7f3a801287d781e02cefc1e67acd9c7e00075041aed05b41163d0d5b0b627eb`.
- 후: `work/windows-control-evidence/test-pc/control-20260909T123846899Z-6e95c00c984f46f2896376458b8323f1/native-recovery-after.png`, SHA-256 `745eb5c38f0ef44ed84d02faf3eb54a4e8a8c1505134dfada9030bc99fba9c18`.

`native-recovery-before-result.json`과 `native-recovery-after-result.json` 모두 assertion PASS, `TaskDeleted=true`, `RemoteRunRemoved=true`다. 후속 상태에서 service Running, session Active, 제어/설정/캡처 작업 0, 스크립트 해시 일치, driver TCP/firewall 0을 확인했다.

**최종 결론: 최초 감사의 종료 차단 P1 두 건은 해소됐다. 합의된 개발·필수 검증 범위에 미해결 종료 차단 사항은 없다.** 테스트 PC에는 패치 MSI가 적용됐으며 운영 PC·운영 배포는 별개로 미실시다. 기존 P3 표시 개선과 제외된 부하·장시간·crash·호환 행렬 검사는 완료 주장에 포함하지 않는다.
