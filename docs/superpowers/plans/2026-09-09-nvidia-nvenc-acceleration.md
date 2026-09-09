# NVIDIA NVENC acceleration implementation handoff

Date: 2026-09-09 KST

## Task

Implement optional, selective NVIDIA NVENC H.264 transcoding in CamStation. Develop and verify the change in the dev LXC first. Do not modify or restart the production CCTV LXC until the development test and review have passed.

## Why this task exists

The Proxmox host has an otherwise unused NVIDIA GeForce GTX 660 Ti. CamStation currently runs several go2rtc FFmpeg transcodes with `libx264`, which consumes substantial host CPU. Recorders that already use stream copy must remain stream copy; GPU acceleration is only for streams that actually transcode video.

The production service was restored and health-checked immediately after the host maintenance reboot. Minimize any later production interruption and keep the existing CPU path as the default and rollback path.

## Environment and current state

- Proxmox GPU: NVIDIA GeForce GTX 660 Ti, PCI ID `10de:1183`, 1999 MiB.
- Host driver: Debian `nvidia-tesla-470`, version `470.256.02`.
- Host kernel: `6.8.12-43-pve`.
- NVIDIA kernel driver is loaded and `nvidia-smi` succeeds.
- Dev LXC: CT 102, Ubuntu 24.04, unprivileged, Docker available.
- Dev LXC already has persistent mappings for `/dev/nvidia0`, `/dev/nvidiactl`, `/dev/nvidia-uvm`, and `/dev/nvidia-uvm-tools`.
- Production CCTV LXC: CT 113. GPU devices have deliberately not been added yet.
- Existing production CamStation image uses FFmpeg 8 without NVENC support.
- Existing generated go2rtc configuration uses a global `h264` preset backed by `libx264`. All transcoding streams currently refer to that common preset.
- CamStation database rows are canonical. Do not solve this by hand-editing generated `data/go2rtc.yaml`.

## Compatibility evidence

Device passthrough itself works in dev:

- `nvidia-smi` inside a Docker container reports the GTX 660 Ti and driver `470.256.02`.
- Ubuntu 24.04 stock FFmpeg exposes `h264_nvenc`, but actual encoding fails because it requires NVENC API 12.1 while the legacy driver provides API 11.1.
- A compatibility build using `nv-codec-headers` tag `n11.1.5.3` and FFmpeg tag `n5.1.7` successfully encoded a synthetic 1280x720, 30 fps H.264 stream for 3 seconds on the GPU at approximately 4.14x realtime.
- The temporary dev test container is named `gpu-nvenc-compat-test`; the temporary base image is `local/gpu-nvenc-buildbase:470`. Treat these as evidence/prototyping artifacts, not a production build pipeline.

## Required design decisions

1. Keep CPU `libx264` as the default.
2. Add an explicit per-camera or per-output choice for GPU H.264 rather than replacing the global `h264` preset.
3. Define a separate go2rtc/FFmpeg preset for NVENC and select it only for opted-in streams.
4. Limit enabled NVENC transcodes to at most three concurrent sessions. This is a conservative operational limit for this consumer Kepler GPU and 470-series driver.
5. Preserve recorder stream-copy paths. Do not introduce decode/re-encode where `-c:v copy` is currently sufficient.
6. Package the compatible FFmpeg and matching NVIDIA user-space encode libraries reproducibly in the CamStation container image. Do not depend on ad-hoc changes inside a running container.
7. Make capability failure safe: if the device, compatible library, or encoder is unavailable, report a clear diagnostic and keep/fall back to the CPU configuration without breaking unrelated cameras.
8. Never expose raw camera URLs, credentials, local transport URLs, or secrets in APIs, UI, logs, tests, or documentation.

## Suggested code investigation

- Find the code that generates the go2rtc `ffmpeg` presets and each stream's `#video=h264` selector.
- Find the image/Docker build that supplies FFmpeg to the CamStation runtime.
- Determine the smallest persistent setting surface for selecting CPU versus NVENC per camera/output. Follow existing database migration, route DTO, and UI conventions if a persisted user setting is required.
- Add validation that prevents more than three configured/running NVENC transcodes, or implement an equally safe deterministic fallback policy.
- Add startup/runtime capability reporting based on an actual encoder probe, not only the existence of `/dev/nvidia*`.

## Acceptance criteria

- [ ] Existing CPU-only behavior remains unchanged when GPU acceleration is not selected.
- [ ] A reproducible dev image contains an FFmpeg build compatible with NVENC API 11.1 and the host's 470.256.02 driver.
- [ ] The dev container can see the mapped GPU and complete an actual H.264 NVENC encode probe.
- [ ] One selected development stream uses `h264_nvenc`; process-command or equivalent runtime evidence proves it, without exposing its source URL.
- [ ] Non-selected streams continue using `libx264` or stream copy as appropriate.
- [ ] The three-session safety limit is enforced or safely handled.
- [ ] `go test ./...` passes; run the web lint/build and final Go build if UI or embedded web files change.
- [ ] Generated go2rtc configuration and public APIs contain no newly exposed secrets.
- [ ] A documented rollback returns all streams to the current CPU configuration and previous image.
- [ ] Production deployment is a separate final step after dev validation; it includes a prebuilt image, CT 113 device mappings, one controlled restart, immediate health/stream/recording checks, and rollback on failure.

## Production sequencing constraint

Do not apply this code directly to CT 113 as part of development. Prepare and test everything in CT 102 first. When production deployment is later authorized and ready, pre-stage the image and configuration, add the four NVIDIA device mappings to CT 113, perform only the necessary controlled restart, and immediately verify the external service, go2rtc/FFmpeg workers, recent recordings, and GPU utilization. Start with no more than one GPU-transcoded camera, then expand only after stability is demonstrated.

## Preserve unrelated work

This checkout already has unrelated modified files. Inspect `git status --short --branch`, use a dedicated Paseo worktree/branch for implementation, and do not overwrite or fold unrelated working-tree changes into this task.

---

## 실행 계획

용어: 여기서 가속하는 대상은 **카메라 내부 인코더가 아니라 서버의 재인코딩 작업**이다. 서버가 원본을 그대로 중계·녹화하는 copy 경로는 GPU를 사용하지 않는다. **3세션은 서버에서 동시에 실행되는 GPU 인코딩 작업 3개**를 뜻한다. 일반 상태 조회·기존 영상 ffprobe는 별도 세션이 아니며, 독립적인 합성 영상 NVENC 호환성 검사를 실행할 때만 추가 슬롯이 필요하다. on-demand 출력 검사로 해당 출력이 시작되면 그 출력의 작업을 계산한다.

상태: **구현·개발 검증 진행 중**. 사용자는 후속 대화에서 개발 검증 후 운영 배포와 Proxmox 호스트 CPU 사용률 전후 비교까지 명시적으로 승인했다. 위 인수인계 원문은 보존한다. 아래는 현재 체크아웃과 [현재 구현 상태](../../07-implementation-status.md)를 대조한 실행안이다. 동작 계약은 [NVENC 설계](../specs/2026-09-09-nvidia-nvenc-acceleration-design.md)에 둔다.

### 1. 확인한 구현과 선행 위험

| 확인 위치 | 현재 동작 | 계획에 반영할 사항 |
|---|---|---|
| `internal/stream/policy.go` | 출력별 auto/copy/h264 판정, 공통 libx264 프리셋, applied snapshot 기반 시작 | 기존 판정 후 인코더만 선택하고 시작·재적용 경로 모두 반영 |
| `internal/stream/apply.go` | 적용 직렬화, 녹화 suspend/restore, 설정 transaction/rollback | 자동 GPU 장애 복귀에 전체 적용 경로를 사용하면 다른 카메라도 중단될 수 있음 |
| `internal/store/{models.go,schema_camera_policies.go,camera_policies.go}` | 출력 정책, desired/applied revision, 검증 결과 보관 | 단일 임시 환경변수가 아닌 영속 출력 설정 및 snapshot 확장 |
| `cmd/camstationd/routes_camera_stream_outputs.go`, `routes_public_dtos.go` | 출력 저장·probe·재적용, desired/applied/effective DTO | 새 선택값과 확인된 실행 상태를 구분하고 probe 동시성 처리 |
| `internal/recorder/recorder.go`, `internal/stream/live_warm.go` | 로컬 출력의 영상 copy | GPU 선택과 무관하게 그대로 유지 |
| `Dockerfile` | Alpine 3.23.2, FFmpeg 8.0.1, 비root UID/GID 10001 | 호환 FFmpeg 복사만으로 해결된다고 가정하지 않고 libc·권한·라이브러리 조합 검증 |
| `.forgejo/workflows/` | 이미지 빌드와 운영 배포가 연결됨 | 개발용 이미지 검증을 운영 자동 배포와 분리 |
| `web/src/pages/cameras/StreamOutputPolicyForm.tsx` | 변환 결과를 software H.264로 표시 | CPU/NVENC/원본 전달 및 복귀 사유를 실제 상태에 따라 표시 |

인수인계의 GTX 660 Ti/470 드라이버/CT 매핑/3초 합성 실험은 전달받은 증거다. 이번에는 원격 장비를 조회하지 않았으며, 3세션 실시간 처리나 장기 안정성을 확인한 것으로 취급하지 않는다.

go2rtc 1.9.14의 `parseArgs`는 `video` 값을 사용자 정의 FFmpeg 프리셋에서 조회한다. 따라서 `h264/nvenc`를 후보로 삼되 실제 실행·출력 협상까지 2단계에서 확인한다. 근거: [고정 버전 공식 소스](https://raw.githubusercontent.com/AlexxIT/go2rtc/v1.9.14/internal/ffmpeg/ffmpeg.go). 이 경로에서는 `hardware=cuda`를 자동 추가하지 않는다.

### 2. 작업 A — 호환 이미지와 실행 경계 검증

의존성: 없음. 구현 시 전용 worktree/branch를 만들고 기존 미커밋 변경은 보존한다. 원격 운영 도구를 사용하기 전에 전역 운영 절차를 읽는다.

대상: 신규 `Dockerfile.nvenc`, 재현 빌드/검사 스크립트, 필요 시 신규 `cmd/camstation-ffmpeg/` 실행 supervisor. 기존 `Dockerfile` CPU 경로는 유지한다.

- [ ] CT 102의 장치 매핑, 드라이버 버전, 다른 GPU 소비자, 실제 실행 UID의 접근 권한을 확인한다. CT 113은 변경하지 않는다.
- [ ] glibc GPU 이미지에 호환 FFmpeg/ffprobe, libx264, CUDA/NVENC 사용자 공간 의존 라이브러리, 기존 go2rtc/rclone/tini를 재현 가능하게 포함한다. 런타임 컨테이너 수동 설치나 임시 테스트 이미지에 의존하지 않는다.
- [ ] 기존 이미지의 디렉터리·UID·환경변수·healthcheck·signal 계약을 유지한다. NVIDIA 커널 모듈은 이미지에 설치하지 않는다.
- [ ] 합성 입력으로 720p30 3초 실험을 재현하고 CPU 인코딩, 스케일/FPS 필터, RTSP/HTTP-FLV, AAC, MP4 녹화, ffprobe, 정상 종료를 검사한다. 5.1.7에 맞지 않는 기존 FFmpeg 옵션이 있으면 영향 범위를 기록하고 수정한다.
- [ ] 별도 NVENC 프리셋에 GOP 20, B-frame 비활성, 8-bit 4:2:0, 저지연을 적용한다. x264 전용 tune/scenecut 옵션을 그대로 복사하지 않고 실제 Kepler에서 지원되는 옵션을 확인한다.
- [ ] supervisor는 go2rtc의 FFmpeg 실행 진입점으로 연결하고 CPU/copy 및 버전 조회는 정상 전달한다. NVENC 프로세스의 종료·CPU 대체·슬롯 반환을 합성 스트림으로 증명한다. FFmpeg 버전 판정에 사용한 바이너리와 실제 바이너리가 일치해야 한다.

완료 증거: 고정된 빌드 입력, image digest, 비root encode 성공, CPU/copy 성공, URL을 제외한 encoder/종료 결과. 실패 시 사용자 설정 구현보다 이 경계를 먼저 해결한다.

### 3. 작업 B — 출력 정책 영속화

의존성: A의 호환성과 실행 방식 확정.

대상: `internal/store/models.go`, `schema_camera_policies.go`, `camera_policies.go`, `camera_policies_test.go`, `cmd/camstationd/routes_public_dtos.go`, `routes_camera_stream_outputs.go` 및 관련 DTO 변환.

- [ ] `videoEncoder=cpu|nvenc`를 출력, 저장 요청, applied policy snapshot에 추가한다. 기존 행과 누락 JSON 필드는 CPU로 정규화한다.
- [ ] migration 반복 실행, scan/SELECT 순서, 초기 카메라 생성/프리셋 적용/출력 수정 시 기본값을 함께 처리한다.
- [ ] 잘못된 enum 및 copy+nvenc를 저장 전에 거부한다. auto+nvenc의 copy 판정은 정상 허용한다.
- [ ] 저장·적용 실패·revision 충돌·재시작 시 선택값이 보존되는지 검사한다. 일시적 fallback은 desired revision을 수정하지 않는다.

완료 증거: 기존 DB migration, round-trip, 구형 클라이언트 요청, applied snapshot 복구 테스트.

### 4. 작업 C — 선택적 렌더링과 3세션 제한

의존성: A, B.

대상: `internal/stream/policy.go`, `policy_test.go`, `apply.go`, `apply_test.go`, `go2rtc.go`, 신규 인코더 관리 코드, `cmd/camstationd/camera_policy_startup.go` 및 시작 테스트.

- [ ] 기존 transcode 판정 이후에만 CPU/NVENC를 선택한다. 입력 릴레이, recorder, live warm의 copy 명령을 유지한다.
- [ ] desired 적용과 applied 시작에 동일한 결정 로직을 사용한다. 미적용 desired 정책을 시작 시 몰래 활성화하지 않는다.
- [ ] 활성 출력 중 변환이 필요한 GPU 요청을 안정적인 순서로 최대 3개 배정한다. 나머지는 CPU와 `session_limit` 상태로 처리한다. focus/on-demand도 예약 대상으로 포함한다.
- [ ] 실제 프로세스 시작에는 공통 원자적 슬롯 제한을 적용한다. 동일 출력의 중복 실행, 독립적인 합성 NVENC 검사, 설정 교체, 종료 지연에도 3을 넘지 않는다. 3슬롯이 모두 사용 중이면 합성 NVENC 검사는 인코더를 추가 실행하지 않고 보류 상태를 반환한다. 일반 상태 조회·기존 출력 ffprobe에는 이 보류 규칙을 적용하지 않는다.
- [ ] 합성 4개 요청과 동시 시작 경쟁으로 제한을 검증한다. 실제 GPU가 3개보다 적게 수용하거나 메모리가 부족해도 시작 실패를 CPU로 처리한다.

완료 증거: CPU 설정 회귀 테스트, 혼합 출력 설정, 재시작 동일 배정, 동시성 테스트 및 실제 프로세스/슬롯 수.

### 5. 작업 D — capability와 실행 중 CPU 복귀

의존성: C.

대상: 인코더 관리/supervisor 코드, `internal/stream/go2rtc.go`, `cmd/camstationd/main.go`, `routes.go`, 기존 시스템 상태 경로와 `routes_public_dtos.go`.

- [ ] NVENC 요청이 있을 때 실제 인코딩 capability probe를 실행한다. 장치 존재나 encoder 목록만으로 성공 처리하지 않는다. 짧은 timeout과 취소 시 자식 회수를 구현한다.
- [ ] 공개 사유는 `device_unavailable`, `permission_denied`, `library_unavailable`, `api_incompatible`, `encoder_failed`, `session_limit` 등 제한된 코드로 매핑한다. 원문 stderr/명령행을 응답하지 않는다.
- [ ] GPU 초기화 실패와 실행 중 장애에서 해당 출력만 CPU로 복귀한다. GPU 옵션을 CPU에 남기지 않고 같은 필터·FPS·오디오 계약을 유지한다.
- [ ] 자동 복귀는 전체 `ApplyCoordinator` 실행과 분리한다. 다른 카메라의 producer/recorder 재시작이 없어야 한다. CPU까지 실패하면 원인과 출력 장애를 보고하며 무한 재시작하지 않는다.
- [ ] 실행 상태 보고는 관리되는 자식의 증거를 사용하고 재시작 시 stale 상태를 초기화한다. H.264 ffprobe 결과는 미디어 검증에만 사용한다.
- [ ] 장치 없는 별도 개발 컨테이너, 라이브러리 불일치 fixture, NVENC 실패 주입으로 검사한다. 호스트 GPU 장치/드라이버를 제거하는 장애 실험은 하지 않는다.

완료 증거: 각 실패 코드, timeout/취소, 출력 단위 CPU 재생 복구와 비대상 카메라 연속성. 전체 재시작밖에 불가능하면 이 요건을 충족했다고 보고하지 않고 실행 구조를 재검토한다.

### 6. 작업 E — API와 한국어 UI

의존성: B–D.

대상: `web/src/app/cameraTypes.ts`, `cameraApi.ts`, `cameraPolicyQueries.ts`, `web/src/pages/cameras/{StreamOutputPolicyForm.tsx,streamOutputPolicyModel.ts,CameraStreamPolicyEditor.tsx}`, 관련 route 테스트와 `web/tests/cameraPolicyModel.test.ts`.

- [ ] 출력별 인코더 선택을 추가하고 기본값 CPU, copy 시 비활성화를 적용한다.
- [ ] 요청/적용 인코더와 확인된 실제 CPU/NVENC/copy 상태를 구분한다. 실행 전은 미검증으로 표시한다.
- [ ] GPU 불가·세션 제한·CPU 복귀를 한국어로 표시하고 기존 저장/재검사/재적용 흐름을 사용한다. 시스템 상태에는 검사 시각과 배정/실행 슬롯 상태를 구분한다.
- [ ] 공개 DTO, 에러, 이벤트, 로그에 비밀정보가 추가 유출되지 않는 회귀 검사를 수행한다. 내부 생성 설정 전체를 테스트 실패 메시지나 보고서에 출력하지 않는다.
- [ ] 실제 화면에서 CPU 기본/선택 NVENC/복귀/원본 복사 상태를 확인한다.

완료 증거: route 및 UI 모델 검사, 개발 UI 화면, 공개 응답의 마스킹 확인.

### 7. 작업 F — 통합 검사와 CT 102 검증

의존성: A–E. 아래 시간은 계획상 관찰 기준이며 실측 완료를 뜻하지 않는다.

```bash
go test ./...
cd web
npm test
npm run lint
npm run build
cd ..
go build -o camstationd ./cmd/camstationd
```

세션/프로세스 관리 변경에는 관련 패키지의 race 검사를 추가한다. `web`에는 모델 테스트가 있으므로 lint/build 외에 `npm test`도 수행한다.

- [ ] GPU 미선택 상태로 개발 이미지에서 CPU 기준선을 수집한다. 같은 입력·해상도·FPS·시청자 수로 비교한다.
- [ ] 선택 출력 1개를 30분 관찰한 뒤 2개·3개를 각각 30분 관찰한다. 각 단계에서 CPU 사용량, 처리 FPS, drop, 메모리, GPU 지표(지원 시), 미디어 진행과 복귀 횟수를 기록한다.
- [ ] 성능 향상은 실제 CPU 사용량 감소로 판단한다. 최소 실시간 처리와 지속적인 프레임 진행을 요구하고 화질·비트레이트도 함께 비교한다. 합성 4.14x 결과를 실서비스 향상률로 환산하지 않는다.
- [ ] live/focus 전환, 복수 시청자, 네 번째 GPU 요청, 동시 검사, GPU 실패, 재적용·daemon 재시작을 확인한다. 프로세스 증거에는 인코더·PID·상태만 남기고 입력 인수는 제외한다.
- [ ] GPU를 선택하지 않은 출력은 CPU 또는 copy 유지, recorder와 warm worker는 copy 유지, 브라우저 H.264 재생과 재접속 정상임을 확인한다.
- [ ] 녹화 활성 개발 조건에서 5분 세그먼트 최소 2개가 ready로 닫히고 DB 크기 일치 및 ffprobe에 통과하는지 확인한다. active/temp 파일은 삭제하지 않는다.
- [ ] 모든 GPU 선택을 CPU로 되돌리고 이전 CPU 이미지로 복귀하는 리허설을 수행한다. 추가된 DB 필드가 이전 바이너리에서 허용되는지 실제로 확인한다.
- [ ] 완료 후에만 `docs/07-implementation-status.md`에 구현/개발 검증 범위와 미완료 운영 항목을 기록한다.

### 8. 작업 G — 운영 전환 준비 및 별도 실행

의존성: F 통과와 최종 검토. **후속 사용자 요청으로 운영 배포 및 호스트 CPU 전후 비교까지 승인됨.**

- [ ] Forgejo/OpenShip 절차에 따라 검증한 commit/image SHA를 고정하고 사전 빌드·준비한다. 개발 브랜치 작업을 운영 자동 배포에 연결하지 않는다.
- [ ] 현재 이미지 digest, 정책, 일관된 DB 백업과 복구 절차를 준비한다. DB 복원은 신규 녹화 메타데이터를 잃을 수 있으므로 이전 바이너리가 추가 필드를 수용하면 DB를 유지하는 이미지 rollback을 우선한다.
- [ ] CT 113 장치 매핑과 내부 Docker 장치/권한/라이브러리를 함께 계획한다. CT 재시작과 서비스 교체가 각각 필요한지 확인하여 중복 재시작 없이 한 번의 통제된 중단 구간으로 묶는다.
- [ ] 운영 실행 승인 후 먼저 CPU 기본으로 health·녹화 정상성을 확인하고 GPU 출력은 1개로 시작한다. 개발 안정성 결과 없이 3개로 확대하지 않는다.
- [ ] 즉시 외부 health, exact image SHA, go2rtc/FFmpeg, 전체 카메라 프레임 진행, 신규 녹화 증가를 확인한다. 다음 세그먼트 종료 시 ready/크기/미디어 검사를 수행한다.
- [ ] health 실패, 비대상 카메라 중단, 녹화 진행 정지 또는 반복 GPU 실패 시 GPU 선택을 CPU로 복귀한다. 해결되지 않으면 준비한 이전 이미지로 복귀하고 동일한 검증을 수행한다. 운영 결과 시각은 KST로 보고한다.

### 운영 CPU 비교

동일 출력은 인코딩 결과를 모든 시청자가 공유한다. 시청자별 인코딩을 추가하지 않는다. 배포 전후 같은 카메라·출력 규격·시청자 조건을 기록하고, 각각 5분 이상 Proxmox 호스트 전체 CPU(`/proc/stat`)와 CamStation cgroup CPU를 동시 측정한다. 전체 48논리 CPU 기준 비율과 1코어=100%인 cgroup 비율을 구분한다. 개발 빌드·부하 테스트는 측정 중 중단하고 I/O wait 및 다른 워크로드 변동을 함께 기록한다. GPU가 처리 가능한 선택 출력부터 전환하며 3출력 안정성이 확인되면 확대한다.

### 완료 판정

A–F의 체크와 증거가 모두 충족되어야 개발 구현 완료다. G는 독립적으로 미실행/완료를 기록한다. 호환 이미지, 출력 단위 장애 격리, 실제 3세션 제한 중 하나라도 미확인 상태면 운영 준비 완료로 표시하지 않는다.
