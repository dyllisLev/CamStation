# Viewer 설정 페이지 다운로드 및 clean-host 인계 보고서

> 주의: `CamStationViewer.msi` 2.0.21은 Authenticode 서명이 없는 내부 개발 빌드다.
> 승인된 시험 PC와 `10.0.0.0/24` 접근 환경에서만 사용하며 운영용 또는 외부 배포본으로
> 취급하지 않는다.

CamStation 2.0 설정 페이지에서 표준 Viewer MSI를 내려받고 수동 설치 시험을 시작할 수
있도록 서버를 게시했으며, WIN11-DELL은 설치된 Viewer가 전혀 없는 상태로 인계했다.

## 바로 수동 시험하기

1. [설정 페이지](http://10.0.0.26:18081/settings)를 연다.
2. `Windows 설치 파일 다운로드`를 선택하고 파일명이 `CamStationViewer.msi`인지 확인한다.
3. MSI를 설치한 뒤 `CamStation Viewer`를 실행한다.
4. 서버 주소에 `http://10.0.0.26:18081`을 입력하고 모니터링 PC를 식별할 Viewer 이름을
   입력한다.
5. 연결 후 `집-마당`, `집-창고1`, `집-창고2`의 영상 진행을 확인한다.

| 항목 | 검증값 |
|---|---|
| 버전 | `2.0.21` |
| 파일 | `CamStationViewer.msi` |
| 크기 | `124350464` bytes |
| SHA-256 | `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e` |
| 설정 URL | `http://10.0.0.26:18081/settings` |
| 서버 입력값 | `http://10.0.0.26:18081` |
| 현재 Docker 이미지 | `camstation:2.0.0-rc.20260810.8-canary` |
| 직전 이미지 | `camstation:2.0.0-rc.20260809.7-canary` |

설치파일의 해시를 직접 확인하려면 다운로드 폴더에서 다음을 실행한다.

```powershell
(Get-FileHash -LiteralPath .\CamStationViewer.msi -Algorithm SHA256).Hash.ToLowerInvariant()
```

결과는 표의 SHA-256과 정확히 같아야 한다. 다르면 설치하지 않는다.

## 범위와 결과

- 범위: 2.0 canary 설정 화면, Viewer 릴리스 API/영구 저장소, Windows 개발 PC의 기존
  Viewer 2.0.21 설치 상태.
- 제외: 1.0 서비스·카메라 설정, 소방서·염소장 카메라, 2.0 운영 전환, Authenticode 서명.
- 서버 결과: 설정 카드와 다운로드 버튼이 표시되고 직접 HTTP 및 브라우저 다운로드가
  동일한 MSI 해시를 반환한다.
- 영상 결과: 집 카메라 3대는 `streaming`, 녹화 워커 3개는 `running`이며 1.0 핵심 서비스
  5개의 PID와 재시작 횟수는 변하지 않았다.
- Windows 결과: 제품·서비스·프로세스·작업·설치 경로·바로가기·레지스트리·Run 값·사용자
  프로필 잔여물이 모두 없다. SSH, 대화형 데스크톱, 개발 소스·도구·원본 MSI는 보존됐다.

장애 시 서버 이미지만 되돌리려면 root 전용
`.env.pre-msi-download-20260810-111413.bak`의 이미지 포인터를 복원하고 canary 서비스만
재생성한다. DB, 녹화, `viewer-releases` 저장소는 삭제하지 않는다. 자세한 명령은
[Docker canary 운영 문서](2026-08-09_camstation2-docker-canary-operations.md#이미지-업데이트와-이전-이미지-복귀)에
있다.

## 증거 → 결론 → 경로

### E-001 — Windows 빌드 산출물

- observed_at: 2026-08-10 11:05 KST
- source_type: file
- source_ref: `work/20260810-viewer-download/artifact/build-metadata.json`
- content_hash: `15770c94d3e4506049c6b8835ef4d8e190a7405f35042bd8212d4617f01dd4a2`
- repro_command: `sha256sum work/20260810-viewer-download/artifact/CamStationViewer.msi`
- raw_excerpt: version 2.0.21, size 124350464, developmentUnsigned true, signatureStatus NotSigned
- linked_workitem: Viewer settings download
- supersedes: none

### E-002 — HTTP 설치파일 다운로드

- observed_at: 2026-08-10 11:15 KST
- source_type: network
- source_ref: `GET http://10.0.0.26:18081/api/viewers/app/download`
- content_hash: `9a1d9f853a19c7f6e46cc8d392915a1fe38e2bfef61115627e0ba1ad0506753e`
- repro_command: `curl -fsS -o CamStationViewer.msi http://10.0.0.26:18081/api/viewers/app/download && sha256sum CamStationViewer.msi`
- raw_excerpt: HTTP 200, attachment filename CamStationViewer.msi, length 124350464, nosniff
- linked_workitem: Viewer settings download
- supersedes: none

### E-003 — 설정 페이지 브라우저 화면

- observed_at: 2026-08-10 11:15 KST
- source_type: screenshot
- source_ref: `work/20260810-viewer-download/browser/settings-msi-published.png`
- content_hash: `709879020280088b5813538a9da5222943d64af4b62620246f6abb6beb16b2fc`
- repro_command: `agent-browser open http://10.0.0.26:18081/settings && agent-browser snapshot -c; agent-browser close`
- raw_excerpt: version 2.0.21, CamStationViewer.msi, 118.6 MB, SHA prefix, enabled download link
- linked_workitem: Viewer settings download
- supersedes: none

### E-004 — Docker 및 1.0 연속성

- observed_at: 2026-08-10 11:16 KST
- source_type: command
- source_ref: `camstation2-canary` inspect, public camera/recorder APIs, five legacy systemd units
- content_hash: `719c6eea290f64251fc8858b8d327dc08296bfc52a746cefeec72b4dfbc69220`
- repro_command: `ssh cctv 'docker inspect camstation2-canary --format "{{.State.Health.Status}} {{.Image}} {{.RestartCount}}"; systemctl show camstation-backend camstation-backup go2rtc nginx vstarcam-tls-proxy -p ActiveState -p MainPID -p NRestarts'`
- raw_excerpt: canary healthy/restart 0; three home cameras streaming; three recorder workers running; legacy unit restart counts 0
- linked_workitem: Viewer settings download
- supersedes: none

### E-005 — Windows clean-host 감사

- observed_at: 2026-08-10 11:18 KST
- source_type: log
- source_ref: `C:\CamStationDev\evidence\viewer-install-2.0.21\uninstall-manual-test-20260810-021807.log`
- content_hash: `9935cac9c354e85c6ce8721c1a26176007e6b3eb412be6dcee098b75e3d0302c`
- repro_command: elevated PowerShell에서 아래 clean-state 명령 실행
- raw_excerpt: MSI exit 0, reboot false, success marker true; post-audit counts all zero
- linked_workitem: Viewer manual clean-install handoff
- supersedes: none

```powershell
$products = @(Get-ItemProperty `
  'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*', `
  'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*' `
  -ErrorAction SilentlyContinue | Where-Object DisplayName -eq 'CamStation Viewer')
$service = Get-CimInstance Win32_Service -Filter "Name='CamStationViewerService'" `
  -ErrorAction SilentlyContinue
$profileResidues = @(Get-ChildItem -LiteralPath 'C:\Users' -Directory -Force |
  ForEach-Object {
    $candidate = Join-Path $_.FullName 'AppData\Roaming\camstation-viewer'
    if (Test-Path -LiteralPath $candidate) { $candidate }
  })
[pscustomobject]@{
  Products = $products.Count
  ServiceExists = $null -ne $service
  InstallDirExists = Test-Path -LiteralPath 'C:\Program Files\CamStation Viewer'
  StateDirExists = Test-Path -LiteralPath 'C:\ProgramData\CamStation\Viewer'
  UserProfileResidues = $profileResidues.Count
}
```

### F-001 — 설정 페이지 MSI 전달 경로 정상

- severity: info
- category: design
- status: validated
- evidence_ids: [E-001, E-002, E-003]
- location: `http://10.0.0.26:18081/settings`
- impact: 운영자가 API 주소를 따로 알지 않아도 검증된 MSI를 같은 화면에서 받을 수 있다.
- confidence: high
- repro_steps: 설정 페이지를 열고 다운로드 버튼으로 받은 MSI의 크기와 SHA-256을 확인한다.
- remediation: n/a

### F-002 — Windows 수동 재설치 준비 완료

- severity: info
- category: other
- status: validated
- evidence_ids: [E-005]
- location: WIN11-DELL
- impact: 이전 설치와 사용자 설정의 영향을 받지 않는 수동 clean-install 시험이 가능하다.
- confidence: high
- repro_steps: E-005의 PowerShell 감사를 실행하고 모든 Viewer 설치 상태가 0/false인지 확인한다.
- remediation: n/a

### F-003 — 미서명 개발 빌드 제한

- severity: low
- category: design
- status: accepted_risk
- evidence_ids: [E-001]
- location: `CamStationViewer.msi` 2.0.21
- impact: Windows가 알 수 없는 게시자 경고를 표시하며 외부 또는 운영 배포 후보로 사용할 수 없다.
- confidence: high
- repro_steps: MSI의 Authenticode 상태가 NotSigned인지 확인한다.
- remediation: 운영 후보를 만들 때 MSI와 설치된 EXE를 승인된 인증서로 서명하고 검증한다.

### P-001 — 운영자 수동 설치 수락 경로

- path_type: solve
- start: 2.0 설정 페이지
- goal: Viewer 연결 및 집 카메라 모니터링 확인
- steps:
  1. 설정 카드의 버전·파일명·해시를 확인한다 — evidence: E-003 — finding: F-001
  2. 버튼으로 MSI를 받고 SHA-256을 대조한다 — evidence: E-001, E-002 — finding: F-001
  3. clean-host에 MSI를 설치하고 Viewer를 실행한다 — evidence: E-005 — finding: F-002
  4. 서버 주소와 Viewer 이름을 저장하고 연결한다 — evidence: E-002 — finding: none
  5. 집 카메라 3대의 영상 진행을 확인한다 — evidence: E-004 — finding: none
- residual_risks: 미서명 개발 빌드이며 실제 수동 설치 후 영상 수락은 운영자가 이어서 수행해야 한다.

시간 순서는 11:05 KST 산출물 재검증, 11:13 릴리스 게시, 11:14 새 canary 시작,
11:15 설정/다운로드 검증, 11:16 카메라·녹화·1.0 연속성 검증, 11:18 Windows 제거 및
clean-state 감사다.
