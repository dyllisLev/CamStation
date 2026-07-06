# VStarcam V400D dual-lens investigation

**작성일:** 2026-07-06 KST
**상태:** 조사 내용 보존. 제품명/UID 분기 구현은 폐기했고, 일반 RTSP 후보 스캔과 다중 채널 UI 설계의 참고 자료로만 사용한다.

## 배경

염소장 카메라는 VStarcam 계열의 V400D 제품으로, 물리 렌즈가 2개인 듀얼 렌즈 카메라다.
당시 CamStation 화면은 ONVIF 스캔 결과를 카메라 프로파일처럼 보여 주고 있었고, ONVIF 기준으로는 단일 채널처럼 보였다. 이 기록은 그 한계를 확인한 조사 자료이며, 현재 구현 방향은 카메라 인스턴스와 재사용 프로파일 템플릿을 분리하는 것이다.

이번 단계의 목표는 프로그램 동작 변경 확정이 아니라, 카메라에서 실제로 두 영상이 어떻게 수신되는지 확인하는 것이었다.

## 확인된 장비 정보

- 대상 카메라: 염소장
- 제품명: VStarcam V400D
- 로컬 IP: `<camera-host>`로 기록을 익명화했다.
- 확인된 열린 포트:
  - `<vendor-port-a>`
  - `<vendor-port-b>`
  - `<http-port>`
  - `<rtsp-port-a>`
  - `<rtsp-port-b>`
- ONVIF 포트 후보: `<onvif-port>`
- RTSP 포트 후보: `<rtsp-port-a>`, `<rtsp-port-b>`

## ONVIF에서 확인된 한계

ONVIF `GetProfiles` 기준으로는 영상 소스/프로파일이 단일 렌즈처럼 나온다.

- Video source: `V_SRC_000`
- Profile candidates:
  - `PROFILE_000` -> `rtsp://<camera-host>:<rtsp-port-a>/tcp/av0_0`
  - `PROFILE_001` -> `rtsp://<camera-host>:<rtsp-port-a>/tcp/av0_1`

따라서 표준 ONVIF 프로파일만 믿으면 V400D의 두 번째 렌즈를 발견하지 못할 가능성이 높다.

## RTSP 수신 확인 결과

동일한 RTSP path가 두 개의 RTSP 포트에서 각각 동작한다.

```text
rtsp://<camera-host>:<rtsp-port-a>/tcp/av0_0
rtsp://<camera-host>:<rtsp-port-a>/tcp/av1_0
rtsp://<camera-host>:<rtsp-port-b>/tcp/av0_0
rtsp://<camera-host>:<rtsp-port-b>/tcp/av1_0
```

캡처 결과를 `1 2 / 3 4` 순서로 놓고 비교했을 때, 사용자가 확인한 실제 매핑은 다음과 같다.

```text
1 2
3 4
```

- `1`, `3`이 하나의 카메라/렌즈 그룹
- `2`, `4`가 다른 하나의 카메라/렌즈 그룹

즉, V400D는 ONVIF profile token 하나로 듀얼 렌즈가 표현되는 구조라기보다, RTSP endpoint 조합을 직접 확인해야 정확한 후보를 만들 수 있다.

## 현재 판단

제품명 또는 UID만으로 V400D 분기를 고정하는 방식은 폐기한다.

이유:

- 같은 VStarcam 계열이라도 제품별 RTSP path/port 구성이 다를 수 있다.
- 중앙 서버나 기존 앱에서는 두 영상이 이미 보이지만, 그 정보가 카메라 네이티브 프로파일로 그대로 노출된다고 볼 수 없다.
- ONVIF가 단일 소스만 노출하는 상황에서는 제품명/UID보다 실제 RTSP 후보를 스캔하는 쪽이 더 정확하다.
- 카메라 화면 구성 자체가 먼저 듀얼 렌즈/다중 스트림을 표현할 수 있어야 한다.

## 다음 설계 메모

카메라 화면은 단순히 “카메라 1개 = 화면 1개”로 두면 안 된다.

필요한 표현:

- 물리 카메라 장치 1개
- 장치 안의 렌즈 또는 채널 N개
- 각 채널의 main/sub 스트림 후보
- 실제 녹화용 스트림과 라이브용 스트림 선택

V400D 같은 장비는 UI에서 다음처럼 보이는 것이 자연스럽다.

```text
염소장
  렌즈 1
    main 후보
    sub 후보
  렌즈 2
    main 후보
    sub 후보
```

스캔 로직은 이후 다음 방향을 검토한다. 아래 예시는 관찰된 후보 패턴이며, 제품명/UID 조건으로 고정하지 않는다.

- ONVIF 프로파일을 먼저 가져온다.
- ONVIF 후보 외에 RTSP 후보를 추가로 생성한다.
- 후보 포트는 기본 RTSP 포트뿐 아니라 인접 포트도 검사한다.
  - 예: `<rtsp-port-a>`, `<rtsp-port-b>`
- 후보 path는 VStarcam에서 확인된 패턴을 넓게 검사한다.
  - 예: `/tcp/av0_0`, `/tcp/av0_1`, `/tcp/av1_0`, `/tcp/av1_1`
- 실제로 열리는 RTSP만 프로파일 후보로 올린다.
- 살아있는 후보들을 장치/렌즈/역할 구조로 묶어 UI에 보여준다.

## 구현 보류 상태

제품명/UID 힌트로 V400D를 분기하는 구현을 잠시 시도했지만, 사용자의 최신 판단에 따라 이 방향은 폐기했다.

현재 우선순위는 다음과 같다.

1. 카메라 화면 구성을 듀얼 렌즈/다중 채널을 표현할 수 있게 재설계한다.
2. 그 다음 RTSP 후보 전체 스캔 방식으로 프로파일 탐지를 설계한다.
3. 마지막으로 저장/녹화/라이브 선택 모델을 다중 채널 기준으로 맞춘다.
