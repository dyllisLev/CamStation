# Standard Windows Viewer Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the supervised Agent/Bootstrap/Host launch chain with one directly launched Electron Viewer and one minimal Windows management service that owns machine configuration, the server connection, and a single-Viewer lease.

**Architecture:** A new `internal/viewerservice` package contains platform-neutral configuration, IPC, lease, status, and server-control logic behind narrow interfaces. Windows adapters store one JSON document in `HKLM\Software\CamStation\Viewer`, host a local named pipe as LocalSystem, and run through SCM. Electron connects directly to that service, shows a local first-run/settings/disconnected surface, and keeps running across recoverable service IPC failures. This phase deliberately excludes MSI authoring and MSI update execution; it produces a runtime that can be launched from an unpacked Windows directory and is ready to be packaged by the next plan.

**Tech Stack:** Go 1.25, `golang.org/x/sys/windows`, `github.com/Microsoft/go-winio`, Windows SCM and named pipes, Electron 43.1.1, TypeScript 6.0.2, Node 24 test runner.

## Global Constraints

- Follow the approved design in `docs/superpowers/specs/2026-07-18-standard-windows-viewer-installer-design.md`.
- `CamStationViewer.exe` is the only normal user-session entry point; it must not require Bootstrap launch arguments.
- The service never launches, kills, or restarts the Viewer during normal operation.
- The service is the only machine-configuration writer. Electron never opens HKLM and no process writes `config.json`, `state.json`, or `current.json`.
- Store the complete mutable configuration as one schema-versioned JSON `REG_SZ` value named `Configuration` beneath `HKLM\Software\CamStation\Viewer`.
- Preserve `clientId` when server URL, display name, or auto-start preference changes. Create it only on the first successful configuration commit.
- Validate a replacement server configuration before replacing the current working configuration.
- Named-pipe messages are newline-delimited JSON, protocol version 2, and at most 64 KiB including the newline.
- Pipe ACL permits LocalSystem, Builtin Administrators, and authenticated interactive users. Reject remote pipe clients.
- A lease belongs to an authenticated pipe connection plus the verified process/session identity, not caller-supplied PID alone.
- Viewer IPC reconnect uses bounded backoff and never exits only because the service restarted.
- Keep `/live?viewer=1`, navigation hardening, renderer telemetry, and existing safe command semantics.
- Do not delete old installer/runtime source until the replacement runtime and MSI tests cover the same required behavior.
- Do not commit Electron packages, Windows binaries, logs, registry exports, or runtime data.

## File Map

- Create `internal/viewerservice/config.go`: public machine configuration and validation types.
- Create `internal/viewerservice/store.go`: `ConfigStore` interface and atomic commit orchestration.
- Create `internal/viewerservice/store_windows.go`: HKLM registry implementation.
- Create `internal/viewerservice/store_other.go`: unsupported-platform constructor used to keep Linux builds explicit.
- Create `internal/viewerservice/config_test.go`: normalization, preservation, and failure-atomicity tests.
- Create `internal/viewerservice/protocol.go`: version-2 request/response envelope and bounded codec.
- Create `internal/viewerservice/lease.go`: one-machine lease manager.
- Create `internal/viewerservice/server.go`: platform-neutral request dispatch and status snapshots.
- Create `internal/viewerservice/protocol_test.go`, `lease_test.go`, and `server_test.go`.
- Create `internal/viewerservice/pipe_windows.go`: local named-pipe listener, ACL, client PID/session verification.
- Create `internal/viewerservice/pipe_other.go`: non-Windows unsupported adapter.
- Create `internal/viewerservice/control.go`: server heartbeat/control loop composed from existing proven transport behavior.
- Create `internal/viewerservice/control_test.go`: service-without-Viewer heartbeat and reconnect tests.
- Create `internal/viewerservice/log.go`: bounded redacted service/Viewer log records and rotation.
- Create `internal/viewerservice/log_windows.go`: ProgramData log creation and per-lease append-only file ACLs.
- Create `internal/viewerservice/log_other.go` and `log_test.go`: portable policy tests and explicit unsupported adapter.
- Create `internal/viewerservice/service.go`: runtime composition and lifecycle.
- Create `cmd/camstation-viewer-service/main.go`: SCM/console entry point.
- Create `cmd/camstation-viewer-service/main_windows.go` and `main_other.go`: platform adapters.
- Create `cmd/camstation-viewer-service/main_test.go`: argument and exit behavior.
- Replace `viewer-app/src/agentPipe.ts` with `viewer-app/src/managementPipe.ts`.
- Create `viewer-app/src/setupModel.ts`: local connection form state and error mapping.
- Create `viewer-app/src/setupWindow.ts`: first-run/disconnected/settings window controller.
- Create `viewer-app/src/setupRenderer.ts`, `viewer-app/assets/setup.html`, and `viewer-app/assets/setup.css`: bundled local connection/settings UI.
- Modify `viewer-app/src/main.ts`, `navigation.ts`, `preload.ts`, and `viewerLifecycle.ts`.
- Add `viewer-app/tests/managementPipe.test.ts`, `setupModel.test.ts`, and `mainLifecycle.test.ts`.
- Modify `viewer-app/scripts/package-win.mjs`: package only the direct Viewer payload.
- Modify `viewer-app/tests/packagePolicy.test.ts` and `installerBuild.test.ts`: reject old launch artifacts.
- Modify `docs/07-implementation-status.md`: record runtime replacement status without claiming MSI completion.

---

### Task 1: Define the Machine Configuration and Atomic Registry Store

**Files:**
- Create: `internal/viewerservice/config.go`
- Create: `internal/viewerservice/store.go`
- Create: `internal/viewerservice/store_windows.go`
- Create: `internal/viewerservice/store_other.go`
- Test: `internal/viewerservice/config_test.go`

**Interfaces:**

```go
const (
	ConfigSchemaVersion = 1
	RegistrySubkey       = `Software\CamStation\Viewer`
	RegistryValueName    = `Configuration`
)

type MachineConfig struct {
	SchemaVersion int    `json:"schemaVersion"`
	ServerURL     string `json:"serverUrl"`
	DisplayName   string `json:"displayName"`
	ClientID      string `json:"clientId"`
	AutoStart     bool   `json:"autoStart"`
}

type ConfigDraft struct {
	ServerURL   string `json:"serverUrl"`
	DisplayName string `json:"displayName"`
	AutoStart   bool   `json:"autoStart"`
}

type ConfigStore interface {
	Load(context.Context) (MachineConfig, error)
	Save(context.Context, MachineConfig) error
}
```

- [ ] **Step 1: Write failing configuration tests**

Cover these exact cases in `config_test.go`:

```go
func TestBuildConfigNormalizesAndDefaultsAutoStart(t *testing.T) {
	got, err := BuildConfig(ConfigDraft{
		ServerURL: "https://cam.example/", DisplayName: "  관제실  ", AutoStart: true,
	}, MachineConfig{}, func() (string, error) { return "client-1", nil })
	if err != nil || got.ServerURL != "https://cam.example" || got.DisplayName != "관제실" ||
		got.ClientID != "client-1" || !got.AutoStart || got.SchemaVersion != ConfigSchemaVersion {
		t.Fatalf("config=%+v err=%v", got, err)
	}
}

func TestBuildConfigPreservesClientID(t *testing.T) {
	current := MachineConfig{SchemaVersion: 1, ServerURL: "https://old.example", DisplayName: "old", ClientID: "stable", AutoStart: true}
	got, err := BuildConfig(ConfigDraft{ServerURL: "https://new.example", DisplayName: "new"}, current,
		func() (string, error) { t.Fatal("generated a replacement ID"); return "", nil })
	if err != nil || got.ClientID != "stable" { t.Fatalf("config=%+v err=%v", got, err) }
}

func TestCommitDoesNotOverwriteWorkingConfigWhenValidationFails(t *testing.T) {
	// A fake store starts with old; a fake validator rejects new.
	// Assert Save was not called and Load still returns old.
}
```

Also reject credentials, paths, queries and fragments in the server URL; blank/control-character display names; unsupported schema versions; missing IDs in persisted data; unknown JSON fields; and registry values larger than 64 KiB.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
go test ./internal/viewerservice -run 'TestBuildConfig|TestCommit' -count=1
```

Expected: FAIL because `internal/viewerservice` and its public types do not exist.

- [ ] **Step 3: Implement validation and two-phase commit**

Implement:

```go
type ConnectionValidator interface {
	Validate(context.Context, ConfigDraft, string) error
}

type ConfigManager struct {
	Store     ConfigStore
	Validator ConnectionValidator
	NewID     func() (string, error)
}

func (m ConfigManager) Commit(ctx context.Context, draft ConfigDraft) (MachineConfig, error) {
	current, err := loadOrEmpty(ctx, m.Store)
	if err != nil { return MachineConfig{}, err }
	candidate, err := BuildConfig(draft, current, m.NewID)
	if err != nil { return MachineConfig{}, err }
	if err := m.Validator.Validate(ctx, draft, candidate.ClientID); err != nil {
		return MachineConfig{}, err
	}
	if err := m.Store.Save(ctx, candidate); err != nil { return MachineConfig{}, err }
	return candidate, nil
}
```

`ConnectionValidator` performs URL reachability, heartbeat API compatibility, and registration acceptance using a bounded request. It must not persist before success. Map failures to stable codes: `invalid_input`, `server_unreachable`, `api_incompatible`, `registration_rejected`, `storage_failed`.

- [ ] **Step 4: Implement the Windows registry adapter**

Use `registry.LOCAL_MACHINE`, open only `RegistrySubkey`, read/write only `RegistryValueName`, and write the whole encoded JSON with one `SetStringValue` call. The service must not create or broaden the key ACL; MSI owns key creation and ACL in the packaging phase. Return `ErrNotConfigured` for missing key/value and distinguish access, decode, and schema errors.

`store_other.go` returns `ErrUnsupportedPlatform`; tests use an in-memory fake rather than a filesystem substitute.

- [ ] **Step 5: Verify tests and Windows compilation**

Run:

```bash
go test ./internal/viewerservice -run 'TestBuildConfig|TestCommit|TestRegistryDocument' -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/viewerservice-config.test.exe ./internal/viewerservice
```

Expected: all focused tests PASS and the Windows test binary compiles.

- [ ] **Step 6: Commit Task 1**

```bash
git add internal/viewerservice/config.go internal/viewerservice/store.go internal/viewerservice/store_windows.go internal/viewerservice/store_other.go internal/viewerservice/config_test.go
git commit -m "feat(viewer): add service-owned machine configuration"
```

### Task 2: Add the Versioned IPC Contract and Single-Viewer Lease

**Files:**
- Create: `internal/viewerservice/protocol.go`
- Create: `internal/viewerservice/lease.go`
- Create: `internal/viewerservice/server.go`
- Create: `internal/viewerservice/protocol_test.go`
- Create: `internal/viewerservice/lease_test.go`
- Create: `internal/viewerservice/server_test.go`

**Interfaces:**

```go
const PipeProtocolVersion = 2
const ViewerServicePipeName = `\\.\pipe\CamStationViewerService`

type Request struct {
	Version   int             `json:"version"`
	RequestID string          `json:"requestId"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	Version   int             `json:"version"`
	RequestID string          `json:"requestId"`
	OK        bool            `json:"ok"`
	ErrorCode string          `json:"errorCode,omitempty"`
	Message   string          `json:"message,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Peer struct { PID, SessionID uint32; Interactive bool }
type Lease struct { ID string; PID, SessionID uint32; ExpiresAt time.Time }
```

Supported requests in this phase are exactly `get_status`, `configure`, `acquire_lease`, `lease_heartbeat`, `release_lease`, `viewer_status`, `renderer_status`, `stream_telemetry`, and `diagnostic_event`.

- [ ] **Step 1: Write failing codec and lease tests**

Tests must prove:

- unknown fields, extra JSON, wrong protocol versions, empty request IDs, empty types, and messages over 64 KiB are rejected;
- structured request errors keep the connection usable;
- only malformed framing/JSON or identity failure closes a client;
- the first interactive peer gets the lease;
- a second session receives `lease_busy` with no secret lease data;
- refresh requires the same connection, PID, session, and lease ID;
- closing a connection releases its lease immediately;
- an unrefreshed lease expires after 15 seconds using an injected clock.

Representative test:

```go
func TestLeaseIsOwnedByConnectionAndVerifiedPeer(t *testing.T) {
	clock := newFakeClock(time.Unix(100, 0))
	leases := NewLeaseManager(clock.Now, 15*time.Second)
	first, err := leases.Acquire("connection-a", Peer{PID: 10, SessionID: 2, Interactive: true})
	if err != nil { t.Fatal(err) }
	if _, err := leases.Acquire("connection-b", Peer{PID: 11, SessionID: 3, Interactive: true}); !errors.Is(err, ErrLeaseBusy) {
		t.Fatalf("second acquire err=%v", err)
	}
	if err := leases.Refresh("connection-b", first.ID, Peer{PID: 10, SessionID: 2, Interactive: true}); !errors.Is(err, ErrLeaseOwner) {
		t.Fatalf("foreign refresh err=%v", err)
	}
}
```

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/viewerservice -run 'TestProtocol|TestLease|TestRequestError' -count=1
```

Expected: FAIL because protocol, lease, and server types are undefined.

- [ ] **Step 3: Implement bounded codec and request dispatcher**

Use `json.Decoder.DisallowUnknownFields`, require EOF after one object, and never place raw storage/server errors in `Response.Message`. Return Korean-safe display messages and correlation IDs from an injected logger.

`get_status` returns:

```go
type StatusSnapshot struct {
	Configured     bool           `json:"configured"`
	Config         *PublicConfig  `json:"config,omitempty"`
	Connection     string         `json:"connection"`
	Viewer         string         `json:"viewer"`
	Renderer       string         `json:"renderer"`
	Installed      string         `json:"installedVersion"`
	Update         UpdateSnapshot `json:"update"`
	AutoStart      bool           `json:"autoStart"`
	LeaseAvailable bool           `json:"leaseAvailable"`
}
```

`PublicConfig` includes server URL and display name but never client ID, registry path, tokens, or raw server responses. `configure` is accepted only from an interactive verified peer.

- [ ] **Step 4: Implement lease expiry and disconnect cleanup**

The dispatcher receives a generated connection ID and verified `Peer`. `HandleDisconnect(connectionID)` releases that connection's lease. Lease refresh is five seconds; expiry is fifteen seconds. No process polling or Viewer termination is added.

- [ ] **Step 5: Verify tests and race behavior**

```bash
go test -race ./internal/viewerservice -run 'TestProtocol|TestLease|TestRequestError|TestDisconnect' -count=1
```

Expected: PASS with no race report.

- [ ] **Step 6: Commit Task 2**

```bash
git add internal/viewerservice/protocol.go internal/viewerservice/lease.go internal/viewerservice/server.go internal/viewerservice/protocol_test.go internal/viewerservice/lease_test.go internal/viewerservice/server_test.go
git commit -m "feat(viewer): add management IPC and viewer lease"
```

### Task 3: Host the Local Pipe and Minimal SCM Service

**Files:**
- Create: `internal/viewerservice/pipe_windows.go`
- Create: `internal/viewerservice/pipe_other.go`
- Create: `internal/viewerservice/service.go`
- Create: `internal/viewerservice/log.go`
- Create: `internal/viewerservice/log_windows.go`
- Create: `internal/viewerservice/log_other.go`
- Create: `internal/viewerservice/log_test.go`
- Create: `cmd/camstation-viewer-service/main.go`
- Create: `cmd/camstation-viewer-service/main_windows.go`
- Create: `cmd/camstation-viewer-service/main_other.go`
- Create: `cmd/camstation-viewer-service/main_test.go`
- Test: `internal/viewerservice/server_test.go`

- [ ] **Step 1: Write failing service lifecycle tests**

Use fake listener/store/control dependencies to prove:

- service starts pipe handling even when no configuration exists;
- service reports ready without a Viewer;
- service cancellation closes listeners and waits for handlers;
- one failed request does not terminate the service;
- a stopped service never attempts to start or terminate Electron;
- SCM stop and preshutdown both produce bounded clean shutdown.

```go
func TestServiceRunsWithoutConfigurationOrViewer(t *testing.T) {
	listener := newFakeListener()
	runtime := Service{Store: missingConfigStore{}, Listener: listener, Control: newFakeControl()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	listener.WaitReady(t)
	if runtime.Status().Configured || runtime.Status().Viewer != "closed" { t.Fatal(runtime.Status()) }
	cancel()
	if err := <-done; err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/viewerservice ./cmd/camstation-viewer-service -run 'TestService|TestRun' -count=1
```

Expected: FAIL because the service composition and command do not exist.

- [ ] **Step 3: Implement the Windows named-pipe listener**

Create the pipe with `go-winio` using a fixed SDDL granting full access to `SY` and `BA`, read/write to authenticated users, and denying network logon. Set `RejectRemoteClients: true`. Before dispatch, obtain the named-pipe client PID, open that process with query-only rights, read its token session ID, and compare it with the session attached to the pipe. Ignore caller-supplied identity fields.

All identity-changing requests require an interactive token. Record only PID/session/correlation ID in logs; never log payloads.

- [ ] **Step 4: Implement SCM and console modes**

The installed image path is only `CamStationViewerService.exe`; SCM starts it with no subcommand. A developer-only `--console` mode runs the same composition from an elevated Windows terminal. Unknown flags exit 2. Non-Windows builds return a clear unsupported error.

Handle `svc.Stop`, `svc.Shutdown`, and `svc.PreShutdown`; report `StartPending`, `Running`, `StopPending`, and `Stopped` with checkpoints. Do not register the service in application code—MSI will own registration.

- [ ] **Step 5: Implement bounded service and Viewer logs**

Create separate `service.log` and per-lease `viewer-<session-id>-<user-sid-hash>.log` below the fixed ProgramData Logs root. Rotate each at 10 MiB and retain five files. The service creates the Viewer file and applies a file-specific ACL granting only the verified interactive user append/synchronize access; directory traversal remains read-only and other users receive no append rights. The lease response returns only that assigned path to the verified local Viewer.

Every record is one bounded JSON line with UTC timestamp, component, state/code, and correlation ID. Redact URL userinfo/query, authorization material, RTSP/camera URLs, update tokens/nonces, PEM blocks, and raw response bodies before writing. The service owns rotation; Viewer opens, appends one bounded record, and closes the handle so rotation is not blocked. If secure file ACL creation fails, lease acquisition returns a recoverable logging error rather than broadening directory permissions.

Tests must prove rotation bounds, redaction, per-user filename stability, traversal rejection, and that a Viewer cannot select an arbitrary log path.

- [ ] **Step 6: Verify lifecycle, logging, race tests, and Windows builds**

```bash
go test -race ./internal/viewerservice ./cmd/camstation-viewer-service -run 'TestService|TestRun|TestLog' -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/CamStationViewerService.exe ./cmd/camstation-viewer-service
```

Expected: tests PASS and the PE builds without invoking PowerShell, `sc.exe`, or Task Scheduler.

- [ ] **Step 7: Commit Task 3**

```bash
git add internal/viewerservice cmd/camstation-viewer-service
git commit -m "feat(viewer): add minimal Windows management service"
```

### Task 4: Move Server Heartbeat and UI Commands into the Minimal Service

**Files:**
- Create: `internal/viewerservice/control.go`
- Create: `internal/viewerservice/control_test.go`
- Modify: `internal/viewerservice/service.go`
- Modify: `internal/viewerservice/server.go`
- Modify: `cmd/camstation-viewer-service/main.go`
- Reference, then retire from active composition: `internal/vieweragent/control.go`, `heartbeat_update.go`

**Interfaces:**

```go
type ControlLoop interface {
	Run(context.Context, MachineConfig, StatusSource, CommandSink) error
}

type CommandSink interface {
	DeliverViewerCommand(Command) error
	SetDesiredUpdate(UpdateNotice)
}

type UpdateNotice struct {
	Version, Filename, SHA256, DownloadURL string
	SizeBytes, CommandID, Generation       int64
}
```

- [ ] **Step 1: Write failing service control tests**

Prove that heartbeat continues while Viewer state is `closed`, service restart does not change `clientId`, server outage backs off to at most 30 seconds, and a UI command is queued only for the active lease connection. Reuse transport characterization from `internal/vieweragent/control_test.go`; move tests rather than silently dropping SSE/long-poll limits.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
go test ./internal/viewerservice -run 'TestControl|TestHeartbeat|TestViewerCommand' -count=1
```

Expected: FAIL because the minimal service has no control loop.

- [ ] **Step 3: Implement the compatible heartbeat payload**

Keep the current server API contract. Report the service as `agent.state=online`, but derive `viewer` and `renderer` strictly from lease telemetry. Closing Viewer must produce `viewer.state=closed`, not service failure. Use installed version injected at link time and leave update execution as `idle` until the update plan.

Move reusable HTTP/SSE code into `internal/viewerservice` or a neutral `internal/viewercontrol` package. Do not import process supervision, Bootstrap grants, state-file replacement, or custom update journals.

- [ ] **Step 4: Add reconnect and command delivery behavior**

Heartbeat begins only after a valid config exists. Failed network calls preserve config, update `connection` status, and use 1/2/5/10/30-second bounded retry. `reload_live` and `resubscribe_stream` are sent only to the lease owner. `shutdown` closes the Viewer only when explicitly commanded; pipe failure is not shutdown.

- [ ] **Step 5: Verify service-only heartbeat**

```bash
go test -race ./internal/viewerservice -run 'TestControl|TestHeartbeat|TestViewerCommand' -count=1
go test ./cmd/camstationd -run 'Viewer.*Heartbeat|ViewerControl' -count=1
```

Expected: both packages PASS, including a test with no Viewer connection.

- [ ] **Step 6: Commit Task 4**

```bash
git add internal/viewerservice cmd/camstation-viewer-service internal/vieweragent
git commit -m "refactor(viewer): move management control into service"
```

### Task 5: Refactor Electron for Direct Launch, Setup, Settings, and Reconnect

**Files:**
- Create: `viewer-app/src/managementPipe.ts`
- Create: `viewer-app/src/setupModel.ts`
- Create: `viewer-app/src/setupWindow.ts`
- Create: `viewer-app/src/setupRenderer.ts`
- Create: `viewer-app/assets/setup.html`
- Create: `viewer-app/assets/setup.css`
- Create: `viewer-app/tests/managementPipe.test.ts`
- Create: `viewer-app/tests/setupModel.test.ts`
- Create: `viewer-app/tests/mainLifecycle.test.ts`
- Modify: `viewer-app/src/main.ts`
- Modify: `viewer-app/src/navigation.ts`
- Modify: `viewer-app/src/preload.ts`
- Modify: `viewer-app/src/viewerLifecycle.ts`
- Delete after replacement tests pass: `viewer-app/src/agentPipe.ts`
- Delete after replacement tests pass: `viewer-app/tests/agentPipe.test.ts`

**Interfaces:**

```ts
export type ViewerStatus = {
  readonly configured: boolean;
  readonly config?: { readonly serverUrl: string; readonly displayName: string };
  readonly connection: "unconfigured" | "connecting" | "online" | "offline" | "service_unavailable";
  readonly autoStart: boolean;
  readonly leaseAvailable: boolean;
};

export class ManagementConnection {
  static connect(pipeName?: string): Promise<ManagementConnection>;
  status(): Promise<ViewerStatus>;
  configure(draft: ConfigDraft): Promise<ViewerStatus>;
  acquireLease(): Promise<LeaseGrant>; // opaque lease ID plus service-assigned append-only log path
  reportRenderer(payload: unknown): void;
  reportStream(payload: unknown): void;
}
```

- [ ] **Step 1: Write failing direct-launch and setup tests**

Tests must prove:

- no `--agent-generation`, `--agent-session`, or nonce is required;
- `--autostart` exits 0 when service config says disabled;
- a busy lease exits 0 without opening a window;
- no config opens the local connection form;
- unreachable saved server opens the same form prefilled and keeps the values;
- retry delay sequence is 1/2/5/10/30/30 seconds;
- successful configure switches to `/live?viewer=1`;
- failed replacement configure keeps the old live URL/config;
- service disconnect shows `service_unavailable`, reconnects, reacquires the lease, and does not terminate Electron;
- settings are reachable through an Electron application menu item `연결 설정` even when the remote page is unavailable.

Representative test:

```ts
test("service disconnect is recoverable", () => {
  assert.equal(disconnectAction({ explicitShutdown: false, retryCount: 0 }), "show-service-error-and-reconnect");
  assert.equal(disconnectAction({ explicitShutdown: true, retryCount: 0 }), "quit");
});
```

- [ ] **Step 2: Run tests and verify RED**

```bash
cd viewer-app && npm test
```

Expected: new tests FAIL because management IPC and setup model do not exist.

- [ ] **Step 3: Implement version-2 management IPC with reconnect**

Retain the bounded decoder/request map pattern but remove launch identity and Agent nonce fields. Keep request timeouts per operation. On pipe close, reject current requests, keep application state, reconnect at 1/2/5/10/30 seconds, refresh status, and reacquire the lease. A second Viewer instance uses Electron's single-instance lock and focuses the existing local window.

- [ ] **Step 4: Implement the local setup/settings surface**

Render the exact bundled `viewer-app/assets/setup.html` and `setup.css` plus compiled `setupRenderer.js` through a hardened `BrowserWindow` with context isolation and no Node integration. The form is Korean-first and includes server URL, Viewer display name, auto-start toggle in settings, `연결하고 저장`, and `다시 시도`. Error codes map to distinct messages without raw response bodies.

Use a normal Electron application menu with `연결 설정` and `종료`; settings opens the local form prefilled. The live BrowserWindow retains current navigation/permission/download/devtools restrictions.

- [ ] **Step 5: Wire direct runtime states**

Startup sequence is:

1. acquire Electron single-instance lock;
2. connect to service;
3. read status;
4. honor `--autostart` preference;
5. acquire lease;
6. show setup/disconnected or live window;
7. refresh lease every five seconds;
8. report Viewer/renderer/stream status;
9. release lease during normal quit.

Do not exit on a single request timeout or pipe close. Do exit quietly for lease busy, disabled auto-start, and explicit service `shutdown` command.

- [ ] **Step 6: Verify Viewer tests and build**

```bash
cd viewer-app && npm test && npm run build && npm run package:win
```

Expected: all tests PASS; package contains direct Viewer resources and no Bootstrap launch arguments.

- [ ] **Step 7: Commit Task 5**

```bash
git add viewer-app/src viewer-app/tests viewer-app/scripts/package-win.mjs viewer-app/package.json viewer-app/package-lock.json
git commit -m "feat(viewer): launch directly with local setup and settings"
```

### Task 6: Remove Old Runtime Artifacts from the Active Build

**Files:**
- Modify: `viewer-app/scripts/package-win.mjs`
- Modify: `viewer-app/tests/packagePolicy.test.ts`
- Modify: `viewer-app/tests/installerBuild.test.ts`
- Modify: `Makefile` if it references old Viewer commands.
- Modify: `docs/07-implementation-status.md`
- Retain temporarily but mark obsolete until MSI plan deletes: `cmd/camstation-viewer-agent`, `cmd/camstation-viewer-bootstrap`, `cmd/camstation-viewer-host`, `internal/viewerbootstrap`, supervision-only `internal/vieweragent` files.

- [ ] **Step 1: Add failing forbidden-artifact assertions**

The package policy test must fail if the packaged runtime or build manifest includes any of:

```text
CamStationViewerAgent.exe
CamStationViewerBootstrap.exe
CamStationViewerHost.exe
current.json
release.zip
schtasks.exe
CamStationViewerRecovery
--agent-generation
--agent-nonce
```

- [ ] **Step 2: Run package policy and verify RED**

```bash
cd viewer-app && npm test -- packagePolicy.test.ts installerBuild.test.ts
```

Expected: FAIL while the active package/build scripts still expect the custom installer chain.

- [ ] **Step 3: Make the direct package the only active runtime output**

`package-win.mjs` emits the Electron folder used as MSI input. Stop building embedded ZIPs or Go installer/Agent/Bootstrap/Host binaries. Keep obsolete source only until the MSI plan proves clean install/uninstall and then deletes it in one traceable commit.

- [ ] **Step 4: Run the runtime phase gate**

```bash
go test ./internal/viewerservice ./cmd/camstation-viewer-service ./cmd/camstationd -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/CamStationViewerService.exe ./cmd/camstation-viewer-service
cd viewer-app && npm test && npm run build && npm run package:win
git diff --check
```

Expected: all tests/builds PASS. Inspect the package manifest and confirm the forbidden list is absent.

- [ ] **Step 5: Update status truthfully**

Record direct-launch runtime and minimal service as implemented but state that standard MSI installation, production signing, automatic MSI updates, and real-Windows acceptance remain pending. Mark the prior custom installer path rejected/obsolete.

- [ ] **Step 6: Commit Task 6**

```bash
git add viewer-app Makefile docs/07-implementation-status.md
git commit -m "build(viewer): retire supervised runtime packaging"
```

## Runtime Phase Exit Gate

Do not start MSI authoring until all of the following are true:

- direct Viewer package launches without Agent/Bootstrap arguments on Windows;
- first-run and saved-server failure behavior is demonstrated on Windows;
- service restart does not terminate Viewer and the Viewer reconnects;
- service heartbeats continue with Viewer closed;
- one lease is enforced across two Windows sessions;
- no state/config JSON file is written under ProgramData;
- no scheduled task, Bootstrap, Host, or process supervision is used;
- Linux tests, Windows cross-builds, and the focused real-Windows smoke log are attached to the task report.
