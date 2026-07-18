# Standard Windows Viewer Automatic MSI Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the custom EXE updater with an immediate, server-directed, signed MSI major-upgrade flow that safely survives service replacement, restarts the Viewer only when it was open, and reports an exact terminal result without retry loops.

**Architecture:** The server publishes immutable MSI metadata and issues an idempotent command generation. The LocalSystem management service downloads the exact MSI, validates size/hash/Authenticode/publisher before disturbing the Viewer, and stages one transaction below ProgramData. A user-session restart helper preserves the interactive relaunch across the Viewer exit. A signed copy of the service executable runs in a short-lived `--update-broker` mode outside Program Files, re-verifies the package, invokes Windows Installer quietly, records the result, and survives the MSI stopping/upgrading the service. The restarted old or new service reconciles the marker and reports the installed version and failure phase.

**Tech Stack:** Go 1.25, Windows WinTrust/Crypto APIs, Windows Installer (`msiexec.exe`), WiX-authored MSI major upgrades, Electron IPC, existing CamStation Viewer heartbeat/command APIs.

## Global Constraints

- Execute only after the MSI phase exit gate in `2026-07-18-standard-windows-viewer-msi.md` passes.
- Server commands identify exact version, filename, size, SHA-256, download URL, command ID, payload hash, generation, and TTL.
- The service downloads only from the configured CamStation origin and fixed `/api/viewers/app/download` route; reject redirects to a different origin.
- Maximum MSI size is 4 GiB. Use exclusive creation and exact transaction directories.
- Verify size, SHA-256, Authenticode trust, timestamp, and compiled CamStation publisher SPKI SHA-256 before notifying or closing Viewer.
- Production service rejects unsigned packages. A development service permits unsigned packages only when compiled and packaged with explicit development policy; version strings never enable it.
- The service does not modify Program Files. Windows Installer is the only writer of installed files/service/shortcuts/registry resources.
- The update broker runs as LocalSystem from a unique SYSTEM/Administrators-only directory outside Program Files and re-verifies the MSI independently.
- Invoke `msiexec.exe` by absolute System32 path with `/i`, `/qn`, `/norestart`, `REBOOT=ReallySuppress`, and a bounded verbose log path.
- Exit code 0 is success. Exit code 3010 is recorded as `reboot_required` and fails the no-reboot release gate. Other codes are failures; Windows Installer owns rollback.
- If Viewer was open, it starts the user-session helper and exits normally. If Viewer was closed, update never launches it.
- A failed version/hash/generation is terminal until the server sends a new generation or explicit operator retry. Do not automatically loop MSI execution.
- Download failure, bad hash, and bad signature happen before Viewer shutdown.
- Do not reintroduce custom install journals. A bounded per-update result marker is reconciliation state, not installation ownership.
- Do not commit MSI artifacts, transaction markers, Windows Installer logs, certificate data, or update downloads.

## Update Transaction Contract

```go
type DesiredUpdate struct {
	Version             string    `json:"version"`
	Filename            string    `json:"filename"`
	SizeBytes           int64     `json:"sizeBytes"`
	SHA256              string    `json:"sha256"`
	DownloadURL         string    `json:"downloadUrl"`
	CommandID           int64     `json:"commandId"`
	PayloadHash         string    `json:"payloadHash"`
	Generation          int64     `json:"generation"`
	TTLSeconds          int       `json:"ttlSeconds"`
	CreatedAt           time.Time `json:"createdAt"`
	DevelopmentUnsigned bool      `json:"developmentUnsigned"`
}

type UpdateIdentity struct {
	Version, SHA256, PayloadHash string
	CommandID, Generation        int64
}

func (i UpdateIdentity) OperationKey() string {
	return fmt.Sprintf("msi-%s-%s-%d", i.Version, i.SHA256, i.Generation)
}
```

Each transaction directory is the fixed ProgramData Updates root plus a safe digest-derived leaf, never user text:

```text
C:\ProgramData\CamStation\Viewer\Updates\<operation-key-digest>\
  CamStationViewer.msi
  CamStationViewerUpdateBroker.exe
  broker-request.json
  broker-result.json
  msiexec.log
```

`broker-request.json` and `broker-result.json` are schema-versioned, maximum 64 KiB, SYSTEM/Administrators-only, and contain no credentials or server response bodies.

## File Map

- Modify `internal/viewerrelease/release.go`, `catalog.go`, and tests: accept only `.msi` release artifacts.
- Modify `cmd/camstationd/routes_viewer_releases.go` and tests: serve MSI content/disposition.
- Modify `cmd/camstationd/routes_viewers.go` and tests: include exact MSI fields/generation and validate installed results without requiring a previously closed Viewer.
- Modify `internal/store/viewer_commands.go`, `viewers.go`, and tests: preserve update idempotency and result fields.
- Modify `scripts/publish-viewer-release.sh` and `publish-viewer-release_test.sh`: publish immutable MSI metadata.
- Modify Viewer download UI types/tests under `web/src`: use `CamStationViewer.msi`.
- Create `internal/viewerservice/update_model.go`: desired update, ledger, phases, redacted errors.
- Create `internal/viewerservice/update_prepare.go`: download, exact-origin policy, size/hash verification.
- Create `internal/viewerservice/update_windows.go`: Authenticode and publisher verification.
- Create `internal/viewerservice/update_other.go`: unsupported verifier.
- Create `internal/viewerservice/update_test.go` and `update_windows_test.go`.
- Create `internal/viewerservice/update_broker.go`: broker request/result validation and `msiexec` execution orchestration.
- Create `internal/viewerservice/update_broker_windows.go` and `update_broker_other.go`.
- Create `cmd/camstation-viewer-restart/main.go`, `main_windows.go`, `main_other.go`, and tests.
- Modify `cmd/camstation-viewer-service`: `--update-broker` mode and service reconciliation.
- Modify `viewer-app/src/main.ts`, `managementPipe.ts`, `setupModel.ts`, `preload.ts`, and tests.
- Modify `installer/Components.wxs` and build scripts to include/sign `CamStationViewerRestart.exe` and embed immutable installed-version/update-policy data.
- Modify `scripts/test-viewer-msi.ps1`: automatic-update scenarios.
- Modify `docs/07-implementation-status.md`.

---

### Task 1: Change Server Publication and Command Contracts from EXE to MSI

**Files:**
- Modify: `internal/viewerrelease/release.go`
- Modify: `internal/viewerrelease/release_test.go`
- Modify: `internal/viewerrelease/catalog_test.go`
- Modify: `cmd/camstationd/routes_viewer_releases.go`
- Modify: `cmd/camstationd/routes_viewer_releases_test.go`
- Modify: `cmd/camstationd/routes_viewers.go`
- Modify: `cmd/camstationd/routes_viewer_control_test.go`
- Modify: `internal/store/viewer_commands.go`
- Modify: `internal/store/viewer_control_test.go`
- Modify: `scripts/publish-viewer-release.sh`
- Modify: `scripts/publish-viewer-release_test.sh`
- Modify: Viewer release frontend types/tests under `web/src` and `web/tests`.

- [ ] **Step 1: Write failing MSI release tests**

Replace old PE expectations with tests that require:

```go
func TestReleaseAcceptsOnlyFixedMSIFilename(t *testing.T) {
	valid := Release{Version: "2.0.1", Filename: "CamStationViewer.msi", SizeBytes: 10, SHA256: strings.Repeat("a", 64)}
	if !valid.validManifest() { t.Fatal("valid MSI rejected") }
	for _, name := range []string{"CamStationViewerSetup.exe", "viewer.zip", `..\CamStationViewer.msi`, "other.msi"} {
		candidate := valid; candidate.Filename = name
		if candidate.validManifest() { t.Fatalf("accepted %q", name) }
	}
}
```

Route tests require `Content-Type: application/x-msi`, `Content-Disposition: attachment; filename="CamStationViewer.msi"`, exact length, and no-store metadata. Heartbeat desired-release tests require `filename`, `sizeBytes`, and fixed same-origin `downloadUrl` in addition to current command identity.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/viewerrelease ./cmd/camstationd ./internal/store -run 'ViewerRelease|ViewerUpdate|MSI' -count=1
```

Expected: FAIL because release validation and routes still require `.exe`.

- [ ] **Step 3: Implement the strict MSI release manifest**

Accept only the exact filename `CamStationViewer.msi`. Preserve immutable directory selection and open-file verification. Do not add a server filesystem path or arbitrary URL to public JSON. The server derives `downloadUrl` as `/api/viewers/app/download`.

- [ ] **Step 4: Bind exact artifact fields into command identity**

Extend command payload hashing to include version, fixed filename, size, SHA-256, and generation. A metadata change creates a new command payload/generation rather than mutating a delivered command. Preserve idempotent command state transitions.

Update server post-update validation:

- service/agent version and artifact digest must equal the desired release;
- control channel must be healthy;
- if heartbeat update state says `viewerWasOpen=true`, Viewer and renderer must become healthy before success;
- if `viewerWasOpen=false`, service/control health and installed version are sufficient; do not launch Viewer solely to satisfy validation.

- [ ] **Step 5: Update immutable publication**

`publish-viewer-release.sh` accepts one explicit MSI path, verifies exact filename, size/hash, and production/development signing policy supplied by the build metadata, then atomically switches the immutable release pointer. It must reject EXE/ZIP input and never modify an existing immutable release directory.

- [ ] **Step 6: Verify server/UI and build**

```bash
go test ./internal/viewerrelease ./internal/store ./cmd/camstationd -run 'ViewerRelease|ViewerUpdate' -count=1
bash scripts/publish-viewer-release_test.sh
cd web && npm test && npm run lint && npm run build
go build ./cmd/camstationd
```

Expected: all checks PASS and the settings card downloads `CamStationViewer.msi`.

- [ ] **Step 7: Commit Task 1**

```bash
git add internal/viewerrelease internal/store cmd/camstationd scripts/publish-viewer-release.sh scripts/publish-viewer-release_test.sh web/src web/tests cmd/camstationd/web
git commit -m "feat(viewer): publish immutable MSI releases"
```

### Task 2: Prepare and Cryptographically Verify an Update Before Viewer Shutdown

**Files:**
- Create: `internal/viewerservice/update_model.go`
- Create: `internal/viewerservice/update_prepare.go`
- Create: `internal/viewerservice/update_windows.go`
- Create: `internal/viewerservice/update_other.go`
- Create: `internal/viewerservice/update_test.go`
- Create: `internal/viewerservice/update_windows_test.go`
- Modify: `internal/viewerservice/service.go`

**Interfaces:**

```go
type UpdatePolicy struct {
	DevelopmentUnsigned bool
	PublisherSPKI256    string
}

type PackageVerifier interface {
	Verify(path string, expectedSize int64, expectedSHA256 string, policy UpdatePolicy) (SignerIdentity, error)
}

type PreparedUpdate struct {
	Identity       UpdateIdentity
	TransactionDir string
	MSIPath        string
	ViewerWasOpen  bool
}
```

- [ ] **Step 1: Write failing prepare-policy tests**

Cover:

- absolute/same-origin URL derivation and redirect rejection;
- response status/content length/body length enforcement;
- 4 GiB limit and exclusive file creation;
- partial download removal;
- exact size and lowercase SHA-256;
- production unsigned/wrong publisher rejection;
- explicit development unsigned allowance only when both build policy and metadata say development unsigned;
- same failed identity not automatically prepared again;
- no Viewer event or state change before verification success.

```go
func TestPrepareRejectsBadHashBeforeViewerNotification(t *testing.T) {
	var notified bool
	runner := newPrepareHarness(t, func() { notified = true })
	err := runner.Prepare(context.Background(), desiredUpdateWithSHA(strings.Repeat("0", 64)))
	if !errors.Is(err, ErrPackageDigest) || notified { t.Fatalf("err=%v notified=%v", err, notified) }
}
```

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./internal/viewerservice -run 'TestPrepare|TestAuthenticode|TestPublisher' -count=1
```

Expected: FAIL because update preparation does not exist.

- [ ] **Step 3: Implement one-attempt download and ledger state**

Create transaction directory with restrictive permissions through the Windows adapter. Write to `CamStationViewer.msi.partial` with exclusive creation, stream through SHA-256 with a hard byte limit, `Sync`, verify length/digest, then rename within the same directory. A network/download failure records a terminal `download_failed` for that generation; it does not sleep/retry in a loop. A new command generation creates a new operation key.

- [ ] **Step 4: Implement Windows trust and publisher verification**

Use WinVerifyTrust with revocation/trust policy appropriate for downloaded code and extract the leaf signing certificate through Crypto APIs. Compare the SHA-256 of DER SubjectPublicKeyInfo to the production value compiled into the service by the signed build. Require a valid RFC 3161 timestamp for production policy. Return only stable codes: `signature_missing`, `signature_untrusted`, `publisher_mismatch`, `timestamp_invalid`.

Do not shell out to PowerShell or parse localized `signtool` output at runtime.

- [ ] **Step 5: Verify Windows build and isolated signature fixtures**

```bash
go test ./internal/viewerservice -run 'TestPrepare|TestPackagePolicy' -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/viewerservice-update.test.exe ./internal/viewerservice
```

On Windows, run the signature fixture test with one trusted development-signed MSI, one unsigned MSI, and one wrong-publisher MSI. Expected: only policy-matching package passes.

- [ ] **Step 6: Commit Task 2**

```bash
git add internal/viewerservice/update_model.go internal/viewerservice/update_prepare.go internal/viewerservice/update_windows.go internal/viewerservice/update_other.go internal/viewerservice/update_test.go internal/viewerservice/update_windows_test.go internal/viewerservice/service.go
git commit -m "feat(viewer): verify MSI updates before shutdown"
```

### Task 3: Build the User-Session Restart Helper and Viewer Update Handshake

**Files:**
- Create: `cmd/camstation-viewer-restart/main.go`
- Create: `cmd/camstation-viewer-restart/main_windows.go`
- Create: `cmd/camstation-viewer-restart/main_other.go`
- Create: `cmd/camstation-viewer-restart/main_test.go`
- Modify: `viewer-app/src/main.ts`
- Modify: `viewer-app/src/managementPipe.ts`
- Modify: `viewer-app/src/setupModel.ts`
- Modify: `viewer-app/tests/mainLifecycle.test.ts`
- Modify: `viewer-app/tests/managementPipe.test.ts`
- Modify: `installer/Components.wxs`
- Modify: `scripts/build-viewer-msi.ps1`

**Interfaces:**

```text
CamStationViewerRestart.exe
  --transaction <64 lowercase hex>
  --service-pipe \\.\pipe\CamStationViewerService
  --viewer "C:\Program Files\CamStation Viewer\CamStationViewer.exe"
  --timeout-seconds 180
```

- [ ] **Step 1: Write failing helper and Viewer handshake tests**

Prove:

- helper accepts only the exact stable Viewer path below Program Files and a safe transaction ID;
- helper waits for service status matching that transaction and a terminal broker result;
- success or Windows Installer rollback both launch the stable Viewer once;
- timeout/error displays one recoverable Korean message and never loops;
- helper does not inherit elevation and runs in the same user/session as Viewer;
- Viewer copies the signed installed helper to a unique user temp directory, starts it, acknowledges `update_applying`, then releases lease and exits normally;
- a failed helper start cancels update application before Viewer exits;
- no Viewer lease means service skips handshake and no helper is launched.

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./cmd/camstation-viewer-restart -count=1
cd viewer-app && npm test -- mainLifecycle.test.ts managementPipe.test.ts
```

Expected: FAIL because helper and update IPC event do not exist.

- [ ] **Step 3: Implement bounded helper behavior**

The helper is a small Windows GUI-subsystem Go executable. It polls the service pipe with 1/2/5/10-second bounded delay up to 180 seconds. It launches Viewer through `CreateProcess` with the helper's current user token and exact stable path, no shell, and no inherited handles. It exits after one launch attempt.

The helper does not read registry config, inspect MSI internals, or run Windows Installer. It deletes its copied user-temp directory on success when possible; stale copies are removed by age on the next Viewer launch.

- [ ] **Step 4: Implement Viewer update UI and acknowledgment**

Add service event `update_applying` with only transaction ID and target version. Viewer shows a noninteractive `업데이트 적용 중` local surface, copies/launches helper, sends `update_helper_ready`, releases lease, and calls normal `app.quit()`. Do not expose MSI path, hash, publisher, or broker arguments to renderer JavaScript.

- [ ] **Step 5: Package and sign the helper**

Build `cmd/camstation-viewer-restart` as `CamStationViewerRestart.exe`, sign it before MSI build, and install it as a stable MSI component. Add forbidden-policy checks preventing any helper location outside the approved install path.

- [ ] **Step 6: Verify tests and Windows PE behavior**

```bash
go test ./cmd/camstation-viewer-restart -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags '-H=windowsgui' -o /tmp/CamStationViewerRestart.exe ./cmd/camstation-viewer-restart
cd viewer-app && npm test && npm run build
```

Expected: tests/builds PASS.

- [ ] **Step 7: Commit Task 3**

```bash
git add cmd/camstation-viewer-restart viewer-app/src viewer-app/tests installer/Components.wxs scripts/build-viewer-msi.ps1
git commit -m "feat(viewer): preserve user session across MSI update"
```

### Task 4: Implement the Short-Lived SYSTEM Update Broker

**Files:**
- Create: `internal/viewerservice/update_broker.go`
- Create: `internal/viewerservice/update_broker_windows.go`
- Create: `internal/viewerservice/update_broker_other.go`
- Create: `internal/viewerservice/update_broker_test.go`
- Modify: `cmd/camstation-viewer-service/main.go`
- Modify: `cmd/camstation-viewer-service/main_windows.go`
- Modify: `cmd/camstation-viewer-service/main_test.go`

**Interfaces:**

```go
type BrokerRequest struct {
	SchemaVersion   int    `json:"schemaVersion"`
	TransactionID  string `json:"transactionId"`
	MSIPath         string `json:"msiPath"`
	ExpectedVersion string `json:"expectedVersion"`
	ExpectedSize    int64  `json:"expectedSize"`
	ExpectedSHA256  string `json:"expectedSha256"`
	ViewerWasOpen   bool   `json:"viewerWasOpen"`
}

type BrokerResult struct {
	SchemaVersion  int       `json:"schemaVersion"`
	TransactionID string    `json:"transactionId"`
	State         string    `json:"state"`
	MSIExitCode   uint32    `json:"msiExitCode"`
	FinishedAt    time.Time `json:"finishedAt"`
	ErrorCode     string    `json:"errorCode,omitempty"`
}
```

- [ ] **Step 1: Write failing broker safety tests**

Cover:

- broker mode requires LocalSystem token;
- request/result paths must be inside the exact transaction directory;
- transaction directory owner/ACL must be SYSTEM/Administrators only and contain no reparse points;
- request is strict, bounded JSON and transaction IDs match directory;
- broker rechecks MSI size/hash/signature/publisher;
- command line uses absolute `%SystemRoot%\System32\msiexec.exe`, correct switches, and safely quoted fixed paths;
- exit 0 => `installed`; 3010 => `reboot_required`; other code => `msi_failed`;
- result writes atomically and contains no localized/raw output;
- broker never launches Viewer or service itself.

- [ ] **Step 2: Run tests and verify RED**

```bash
go test ./internal/viewerservice ./cmd/camstation-viewer-service -run 'TestBroker|TestUpdateBrokerMode' -count=1
```

Expected: FAIL because broker mode does not exist.

- [ ] **Step 3: Implement secure broker staging**

The running LocalSystem service copies its own signed executable into the transaction directory as `CamStationViewerUpdateBroker.exe`, verifies the copy's hash/signature, writes `broker-request.json`, and starts the copy with `--update-broker --request <exact-path>`. The child inherits no pipe/server handles and is assigned no service lifetime/job object that would kill it when MSI stops the service.

- [ ] **Step 4: Implement Windows Installer execution**

Broker revalidates identity and package, then starts:

```text
%SystemRoot%\System32\msiexec.exe /i <exact-msi> /qn /norestart REBOOT=ReallySuppress /l*v <exact-log>
```

Use `CreateProcess`/`exec.Command` argument APIs, not `cmd.exe`. Wait for completion. Windows Installer rollback is authoritative; the broker never copies old files or changes SCM/registry/shortcuts itself.

- [ ] **Step 5: Verify broker survival in an isolated Windows upgrade**

Run an A-to-B MSI upgrade initiated by the A service. During `StopServices`, prove broker PID remains alive while service PID changes. After completion, require result marker, B service running, and broker exit. Then inject MSI failure and require A product/service restored and result `msi_failed`.

- [ ] **Step 6: Commit Task 4**

```bash
git add internal/viewerservice/update_broker.go internal/viewerservice/update_broker_windows.go internal/viewerservice/update_broker_other.go internal/viewerservice/update_broker_test.go cmd/camstation-viewer-service
git commit -m "feat(viewer): apply updates through a SYSTEM MSI broker"
```

### Task 5: Orchestrate Idempotency, Reconciliation, and Server Reporting

**Files:**
- Modify: `internal/viewerservice/service.go`
- Modify: `internal/viewerservice/server.go`
- Create: `internal/viewerservice/update_coordinator.go`
- Create: `internal/viewerservice/update_coordinator_test.go`
- Modify: `internal/viewerservice/control.go`
- Modify: `cmd/camstationd/routes_viewers.go`
- Modify: `cmd/camstationd/routes_viewer_control_test.go`
- Modify: `internal/store/viewer_update_validation.go`
- Modify: `internal/store/viewer_control_test.go`

- [ ] **Step 1: Write failing coordinator state-machine tests**

Required states:

```text
idle -> downloading -> verified -> waiting_for_viewer -> broker_started
broker_started -> installed | rolled_back | failed | reboot_required
any pre-broker state -> download_failed | verification_failed | cancelled
```

Tests prove:

- duplicate delivery of same command/generation returns current state without re-downloading or launching a second broker;
- one operation key owns one broker PID/result;
- same terminal failed generation is not retried;
- new generation may retry same version/hash;
- service startup reconciles result marker with MSI-authored `InstalledVersion` and its own file version;
- installed target reports success; old installed version after failed MSI reports rolled back/failure;
- stale partial/transaction directories are removed only after 7 days and only below exact Updates root;
- service remains responsive and continues heartbeat throughout download/preparation;
- Viewer open flag is captured at verified-to-apply transition.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/viewerservice ./internal/store ./cmd/camstationd -run 'TestUpdateCoordinator|TestViewerUpdateValidation' -count=1
```

Expected: FAIL because the coordinator/reconciliation contract does not exist.

- [ ] **Step 3: Implement immediate apply ordering**

After package verification:

1. capture whether an active lease exists;
2. if yes, send `update_applying` and wait up to 15 seconds for `update_helper_ready` plus normal lease release;
3. if helper start fails, mark failed and keep Viewer open;
4. if Viewer is unresponsive, proceed to broker after timeout but record `viewer_restart_unavailable`; Restart Manager owns closure and no automatic relaunch is promised;
5. if no lease exists, launch broker immediately;
6. set `broker_started` before starting child, then include child PID/start time atomically.

- [ ] **Step 4: Reconcile after service start**

Read only bounded markers below the exact Updates root. Compare transaction target to MSI-authored `InstalledVersion` and service-linked version. Never trust marker text alone. Publish a redacted terminal state and preserve it long enough for Viewer/server acknowledgment. Clean it after acknowledgment and retention expiry.

- [ ] **Step 5: Update server command completion**

Heartbeat update state includes transaction ID, command identity, phase, installed version, `viewerWasOpen`, and redacted failure code. Server marks command running when broker begins and succeeds only after the correct installed version/control health (and Viewer health when it was open) remain healthy for the existing validation window. Rollback/failure marks command failed with phase/code, not raw MSI log text.

- [ ] **Step 6: Verify state machine and race tests**

```bash
go test -race ./internal/viewerservice ./internal/store ./cmd/camstationd -run 'TestUpdateCoordinator|TestViewerUpdateValidation|TestDuplicate' -count=1
```

Expected: PASS with no duplicate broker launch and no race report.

- [ ] **Step 7: Commit Task 5**

```bash
git add internal/viewerservice/update_coordinator.go internal/viewerservice/update_coordinator_test.go internal/viewerservice/service.go internal/viewerservice/server.go internal/viewerservice/control.go internal/store cmd/camstationd
git commit -m "feat(viewer): reconcile and report MSI update results"
```

### Task 6: Add Explicit Update Failure Injection Tests

**Files:**
- Modify: `scripts/test-viewer-msi.ps1`
- Create: `scripts/windows/viewer-update-fixtures/README.md`
- Modify: `internal/viewerservice/update_test.go`
- Modify: `internal/viewerservice/update_broker_test.go`
- Modify: `docs/07-implementation-status.md`

- [ ] **Step 1: Add isolated fixtures and expected results**

The Windows test harness accepts explicit fixture paths for:

- valid signed A and B MSIs;
- correct-size/wrong-hash MSI;
- unsigned MSI;
- wrong-publisher signed MSI;
- MSI that intentionally fails after service stop to exercise Windows Installer rollback.

Fixtures are generated in CI/test scratch space and never committed. The README documents generation and expected signing identity without private key data.

- [ ] **Step 2: Test every pre-shutdown failure**

For bad size/hash/signature/publisher and interrupted download, assert:

- Viewer PID/session remains unchanged;
- service remains running;
- installed product/version remains A;
- command ends with the exact stable failure phase;
- same generation is not retried;
- no broker or `msiexec` process is launched.

- [ ] **Step 3: Test every post-shutdown outcome**

For healthy B and failing B:

- healthy B: helper relaunches one B Viewer in original user/session; B service/control becomes healthy;
- failed B: Windows Installer restores A, helper launches one A Viewer, and UI shows saved update error;
- Viewer closed before command: update applies but no Viewer process starts;
- duplicate command: one broker/MSI transaction only;
- reboot-required code: command fails release gate and no silent success is reported.

- [ ] **Step 4: Run full update verification**

```powershell
.\scripts\test-viewer-msi.ps1 `
  -InstalledMsi C:\fixtures\viewer-a\CamStationViewer.msi `
  -UpgradeMsi C:\fixtures\viewer-b\CamStationViewer.msi `
  -WrongHashMsi C:\fixtures\wrong-hash\CamStationViewer.msi `
  -UnsignedMsi C:\fixtures\unsigned\CamStationViewer.msi `
  -WrongPublisherMsi C:\fixtures\wrong-publisher\CamStationViewer.msi `
  -FailingUpgradeMsi C:\fixtures\failing\CamStationViewer.msi `
  -ScratchDirectory C:\CamStationUpdateTest
```

Expected: all scenario assertions PASS; every MSI log correlates to one transaction ID.

- [ ] **Step 5: Update status truthfully and commit**

Record automatic MSI update and failure injection as implemented only after actual Windows evidence. Keep two-hour soak and ten-cycle release acceptance pending until the acceptance plan.

```bash
git add scripts/test-viewer-msi.ps1 scripts/windows/viewer-update-fixtures/README.md internal/viewerservice docs/07-implementation-status.md
git commit -m "test(viewer): cover MSI update failure boundaries"
```

### Task 7: Run the Automatic Update Phase Gate

- [ ] **Step 1: Run repository verification**

```bash
go test ./... -count=1
cd web && npm test && npm run lint && npm run build
cd ../viewer-app && npm test && npm run build && npm run package:win
cd .. && go build ./cmd/camstationd ./cmd/camstation-viewer-service ./cmd/camstation-viewer-restart
git diff --check
```

Expected: every test/build passes.

- [ ] **Step 2: Run one real server-directed Windows update**

Publish signed B to a non-production CamStation server, connect a clean A Viewer, and issue/observe the normal server command rather than invoking local test hooks. Record:

- immutable release metadata and digest;
- command ID/payload hash/generation;
- A service/Viewer processes and session;
- broker and `msiexec` lifecycle;
- B product/service/file versions;
- helper-launched B Viewer in the original session;
- server command success after health validation.

- [ ] **Step 3: Verify no forbidden mechanisms returned**

```powershell
Get-ScheduledTask | Where-Object TaskName -Like 'CamStationViewer*'
Get-CimInstance Win32_Service -Filter "Name='CamStationViewerService'"
Get-ItemProperty 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Run' -Name CamStationViewer
```

Expected: no Viewer scheduled tasks; exactly one automatic service; Run target is the direct stable Viewer EXE.

- [ ] **Step 4: Commit final phase evidence index**

Commit only a redacted evidence index and commands/results, not logs/artifacts:

```bash
git add docs/test-evidence/windows-viewer-msi docs/07-implementation-status.md
git commit -m "docs(viewer): record automatic MSI update gate"
```

## Automatic Update Phase Exit Gate

Do not begin final acceptance until:

- server publishes and serves only immutable verified MSI artifacts;
- service rejects bad size/hash/signature/publisher before Viewer shutdown;
- valid update uses one SYSTEM broker and one Windows Installer transaction;
- broker survives service replacement and never writes Program Files itself;
- Viewer-open update relaunches exactly once in the original user session;
- Viewer-closed update does not open Viewer;
- failed MSI rolls back through Windows Installer and old Viewer relaunches;
- duplicate/failed command generation never loops or launches duplicate MSI work;
- service startup reconciles actual installed version, not marker claims;
- real server-directed update evidence exists on target Windows.
