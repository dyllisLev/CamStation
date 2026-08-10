# CamStation Viewer MSI 빌드

> Windows x64 전용입니다. 현재 Linux 개발 호스트는 소스 검증까지만 할 수 있습니다. CCTV
> 모니터링 NUC는 MSI 설치·복구·제거 대상이며 빌드 호스트가 아닙니다. NUC에 Node.js, Go,
> .NET SDK, WiX 소스 또는 NuGet 캐시를 배치하지 마세요.

## 빠르게 빌드하기

전용 Windows 개발 PC 또는 Windows VM의 저장소 루트에서 실행합니다.

```powershell
pwsh -NoProfile -File .\scripts\build-viewer-msi.ps1 `
  -Version 2.0.21 `
  -UnsignedDevelopment
```

이 명령은 의존성 설치, Viewer 테스트, Windows Electron 패키징, Go 서비스 테스트·빌드,
WiX 6.0.2 복원과 MSI 생성을 순서대로 수행합니다. MSI를 설치하거나 원격 PC에 접속하지
않습니다.

## 빌드 환경 준비하기

필요한 도구는 모두 `PATH`에서 실행 가능해야 합니다.

| 도구 | 지원 기준 | 확인 명령 |
|---|---|---|
| Windows | x64 Windows 11 또는 Windows Server VM | `[Environment]::Is64BitOperatingSystem` |
| PowerShell | PowerShell 7 x64 권장 | `$PSVersionTable.PSVersion; [Environment]::Is64BitProcess` |
| Node.js | Node.js 22 이상과 npm | `node --version; npm --version` |
| Go | Go 1.25 이상 | `go version` |
| .NET | .NET SDK 8.x | `dotnet --version` |
| Git | 현재 커밋과 dirty 상태 기록에 필요 | `git --version` |

최초 빌드는 npm, Go module, NuGet과 Electron 런타임을 내려받을 수 있어야 합니다. WiX SDK와
확장은 `installer/packages.lock.json`의 6.0.2 버전으로 잠금 복원됩니다.

반복 빌드에서 이미 `npm ci`를 수행했다면 다음 옵션으로 의존성 재설치만 생략할 수 있습니다.

```powershell
pwsh -NoProfile -File .\scripts\build-viewer-msi.ps1 `
  -Version 2.0.22 `
  -UnsignedDevelopment `
  -SkipDependencyInstall
```

## 산출물 확인하기

기본 출력은 `artifacts\viewer-msi\<version>`이며 이미 존재하는 출력 디렉터리는 덮어쓰지
않습니다.

| 파일 | 용도 |
|---|---|
| `CamStationViewer.msi` | Windows x64 설치 패키지 |
| `CamStationViewer.wixpdb` | WiX 진단 심볼 |
| `CamStationViewer.msi.sha256` | 전송 전후 해시 비교 |
| `build-metadata.json` | MSI 내부 식별자, 크기·해시, 도구 버전, 소스 커밋과 dirty 여부 |

빌드는 MSI 데이터베이스의 `ProductName`, `ProductVersion`, `ProductCode`, 고정 `UpgradeCode`와
File 테이블 개수를 검사합니다. `build-metadata.json`에는 절대 경로, 서버 주소, Viewer 구성,
인증정보를 기록하지 않습니다.

## 실패를 해결하기

- `requires a dedicated Windows build host`: Linux 또는 비 Windows 셸입니다. 전용 Windows
  개발 PC나 VM에서 다시 실행하세요.
- `Output directory already exists`: 새 버전을 사용하거나 `-OutputDirectory`로 비어 있는 새
  경로를 지정하세요. 기존 산출물은 자동 삭제하지 않습니다.
- Node.js, Go 또는 .NET 버전 오류: 표의 지원 기준에 맞춘 뒤 사전 확인 명령을 다시 실행하세요.
- locked restore 오류: `packages.lock.json`을 임의 갱신하지 말고 네트워크와 NuGet 캐시를 먼저
  확인하세요.
- 실패 작업공간이 필요하면 `-KeepBuildWorkspace`를 사용하세요. 기본값은 현재 실행이 만든
  정확한 `.work-*` 디렉터리만 정리합니다.

현재 스크립트는 `-UnsignedDevelopment`가 없으면 중단됩니다. 이 산출물은 내부 검증용이며
운영 배포용으로 표현하면 안 됩니다. 운영용 빌드는 별도의 검토된 Authenticode 서명 절차와
Windows 설치·업그레이드·복구·제거 검증을 통과해야 합니다.

설치 프로그램의 제품 경계와 수명주기 요구사항은
[설계 문서](../docs/superpowers/specs/2026-07-18-standard-windows-viewer-installer-design.md)에서
확인할 수 있습니다.
