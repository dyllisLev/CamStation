# Live PTZ Camera Control Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add guarded ONVIF PTZ, home, and preset controls for registered cameras and expose them through a capability-gated replacement panel in `/live`.

**Architecture:** Extract the existing SOAP/WS-Security code into a small shared ONVIF transport, then build a concrete camera controller with ordered per-camera command gates. Persist a credential-free capability summary on each camera, expose guarded control routes, and add a focused React PTZ panel that sends one non-overlapping press-and-hold move stream with Stop as the final device command.

**Tech Stack:** Go 1.x standard library, SQLite, `net/http`, React 19, TypeScript 6, TanStack Query 5, Lucide React, existing Vite/Oxlint toolchain.

## Global Constraints

- Follow `docs/superpowers/specs/2026-07-11-live-ptz-camera-control-design.md` exactly.
- Use `streamName` as the stable camera identity; reject recording/live role-stream aliases on control routes.
- Never return or log camera credentials, raw URLs, ONVIF endpoints, SOAP payloads, or raw device faults.
- Keep go2rtc bound locally and keep browser media traffic on existing CamStation proxy paths.
- Do not add a Go, npm, or browser dependency.
- Keep `소리 듣기`, `말하기`, and `사이렌` visible but disabled in this delivery.
- Movement and Stop device calls use 2-second HTTP timeouts; capability discovery may use 8 seconds.
- Run only the task's named narrow test or linter during Tasks 1-6.
- Run the complete Go/frontend verification matrix once, in Task 7.
- If that matrix reports failures, record all failures, fix and rerun only the narrow test/lint/build scope for each failure, and do not rerun the complete matrix while any known failure remains.
- After every known failure passes its narrow verification, rerun the complete matrix once as the integration gate. Repeat this targeted-fix-then-one-integration-gate cycle only when the integration gate reveals a new failure.
- Move the real camera only once, in the bounded Task 7 session; never alter the saved home position automatically.
- If the bounded real-camera session reveals a defect, add or run the narrow automated regression for that defect, fix it, and repeat only the failed real-device action once. Never repeat unrelated camera actions merely to collect duplicate evidence.
- Preserve all pre-existing user changes. In particular, do not stage the existing camera-registration source edits, `.debug-journal.md`, `.superpowers/`, or `docs/camera-registration-ui-mockup.html`.
- `cmd/camstationd/web/index.html` and the existing embedded asset set were already dirty before this feature. Run the required frontend build for verification, but do not stage those generated files in PTZ commits; report them for later user-owned consolidation.

---

## File Map

### New backend files

- `internal/onvif/client.go` — shared SOAP 1.2 transport, service paths, WS-Security digest envelope, and safe transport errors.
- `internal/onvif/client_test.go` — digest, escaping, service path, HTTP status, and password-leak checks.
- `internal/cameracontrol/controller.go` — target extraction, capability/status/preset parsing, PTZ operations, and ordered per-camera command gates.
- `internal/cameracontrol/controller_test.go` — SOAP operation coverage, parsing, credential hygiene, and Stop ordering.
- `internal/store/camera_control_capabilities_test.go` — migration, default decoding, persistence, and exact stable-stream update checks.
- `cmd/camstationd/routes_camera_controls.go` — guarded camera-control routes, validation, safe responses, and bounded events.
- `cmd/camstationd/routes_camera_controls_test.go` — route guard, alias rejection, validation, persistence, redaction, and operation tests.

### New frontend files

- `web/src/app/cameraControlApi.ts` — typed guarded control requests.
- `web/src/app/cameraControlQueries.ts` — non-retrying status/preset queries and bounded mutations.
- `web/src/components/live/usePtzHold.ts` — one-in-flight renewal loop and final Stop lifecycle.
- `web/src/components/live/PtzControlPanel.tsx` — right-panel PTZ, home, preset, and disabled device-feature UI.

### Modified backend files

- `internal/cameraprofile/onvif_client.go` — delegate SOAP calls to `internal/onvif` while preserving scanner behavior.
- `internal/cameraprofile/profile_helpers.go` — remove the moved envelope code and retain camera-profile-only helpers.
- `internal/store/models.go` — add typed control capability fields to `Camera`.
- `internal/store/schema.go` — add `control_capabilities_json` to new databases.
- `internal/store/schema_camera_profiles.go` — migrate existing databases with a safe `{}` default.
- `internal/store/camera_rows.go` — decode the capability JSON.
- `internal/store/cameras.go` — write/select the capability JSON and add an exact-stream update method.
- `cmd/camstationd/routes.go` — add the controller dependency and default construction.
- `cmd/camstationd/routes_cameras.go` — register control routes.
- `cmd/camstationd/routes_camera_mutations.go` — persist scan-provided partial capability summaries without another device call.
- `cmd/camstationd/routes_public_dtos.go` — expose only the normalized capability summary.

### Modified frontend files

- `web/src/app/http.ts` — add an explicit guarded request wrapper that also works for GET.
- `web/src/app/cameraTypes.ts` — add control capability, status, move, and preset types.
- `web/src/app/api.ts` — compose and export the control API.
- `web/src/app/queries.ts` — re-export the control queries.
- `web/src/components/live/LiveWorkspace.tsx` — toolbar state, lazy capability refresh, panel replacement, and stop-on-target-change lifecycle.
- `web/src/styles/index.css` — dense dark PTZ panel styling and disabled capability states.

### Final documentation

- `docs/07-implementation-status.md` — mark the exact delivered PTZ scope and note audio/talk/siren limitations after verification.

---

### Task 1: Extract the shared ONVIF transport

**Files:**
- Create: `internal/onvif/client.go`
- Create: `internal/onvif/client_test.go`
- Modify: `internal/cameraprofile/onvif_client.go`
- Modify: `internal/cameraprofile/profile_helpers.go`

**Interfaces:**
- Produces: `onvif.Target`, `onvif.Service`, `onvif.Client`, `onvif.NewClient(*http.Client)`, `Client.Call(context.Context, Target, Service, action, body string) (string, error)`, and `onvif.Escape(string) string`.
- Consumed by: Task 3 `cameracontrol.Controller` and the existing `cameraprofile.NetworkScannerClient`.

- [ ] **Step 1: Write failing transport tests**

Create `internal/onvif/client_test.go` with focused tests that use `httptest.Server` and never contact a camera:

```go
package onvif

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestClientCallUsesDigestAndEscapesBody(t *testing.T) {
	const password = "camera-secret"
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != string(ServicePTZ) {
			t.Errorf("path = %q, want %q", r.URL.Path, ServicePTZ)
		}
		payload, _ := io.ReadAll(r.Body)
		requestBody = string(payload)
		w.Header().Set("Content-Type", "application/soap+xml")
		_, _ = w.Write([]byte(`<Envelope><Body><ok/></Body></Envelope>`))
	}))
	defer server.Close()

	target := targetFromServerURL(t, server.URL, "operator", password)
	_, err := NewClient(server.Client()).Call(
		context.Background(), target, ServicePTZ,
		"http://www.onvif.org/ver20/ptz/wsdl/GetNodes",
		`<tptz:GetNodes><tt:Name>`+Escape(`A&B`) + `</tt:Name></tptz:GetNodes>`,
	)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if strings.Contains(requestBody, password) {
		t.Fatal("SOAP envelope leaked the plain password")
	}
	for _, required := range []string{"PasswordDigest", "Nonce", "Created", "A&amp;B"} {
		if !strings.Contains(requestBody, required) {
			t.Fatalf("request missing %q", required)
		}
	}
}

func TestClientCallRejectsNon2xxWithoutReturningPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `fault camera-secret`, http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := NewClient(server.Client()).Call(context.Background(), targetFromServerURL(t, server.URL, "u", "p"), ServiceDevice, "action", `<tds:GetDeviceInformation/>`)
	if !errors.Is(err, ErrAuthenticationFailed) || strings.Contains(err.Error(), "camera-secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func targetFromServerURL(t *testing.T, rawURL, username, password string) Target {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}
	return Target{Host: parsed.Hostname(), Port: port, Username: username, Password: password}
}
```

The test helper parses only the `httptest.Server` URL and returns a `Target`; it must call `t.Helper()` and fail on malformed URLs.

- [ ] **Step 2: Run the transport tests and verify the expected failure**

Run:

```bash
go test ./internal/onvif -run '^TestClientCall' -count=1
```

Expected: FAIL because `Target`, `ServicePTZ`, `NewClient`, and `Escape` do not exist.

- [ ] **Step 3: Implement the minimal shared transport**

Create `internal/onvif/client.go` with these concrete types and service paths:

```go
package onvif

type Service string

const (
	ServiceDevice   Service = "/onvif/device_service"
	ServiceMedia    Service = "/onvif/media_service"
	ServicePTZ      Service = "/onvif/ptz_services"
	ServiceDeviceIO Service = "/onvif/deviceio_service"
)

type Target struct {
	Host     string
	Port     int
	Username string
	Password string
}

type Client struct {
	HTTPClient *http.Client
}

func NewClient(client *http.Client) Client {
	return Client{HTTPClient: client}
}

func (c Client) Call(ctx context.Context, target Target, service Service, action, body string) (string, error) {
	envelope, err := envelope(target.Username, target.Password, body)
	if err != nil {
		return "", ErrRequestFailed
	}
	hostPort := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	endpoint := (&url.URL{Scheme: "http", Host: hostPort, Path: string(service)}).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(envelope))
	if err != nil {
		return "", ErrRequestFailed
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	req.Header.Set("SOAPAction", action)
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", ErrRequestFailed
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", ErrRequestFailed
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", ErrAuthenticationFailed
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: HTTP %d", ErrRequestFailed, resp.StatusCode)
	}
	return string(payload), nil
}

func Escape(value string) string {
	var builder strings.Builder
	if err := xml.EscapeText(&builder, []byte(value)); err != nil {
		return ""
	}
	return builder.String()
}
```

Declare `var ErrRequestFailed = errors.New("ONVIF request failed")` and `var ErrAuthenticationFailed = errors.New("ONVIF authentication failed")`. These sentinels are the only transport errors returned for non-context failures; never return response payloads, URLs, or underlying client error text. Move the existing nonce, SHA-1 digest, Base64, and UTC `Created` code from `cameraprofile.soapEnvelope` into the unexported `envelope` function unchanged. Keep the exact ONVIF namespaces already used by the working scanner.

- [ ] **Step 4: Delegate scanner SOAP calls to the shared client**

Replace `NetworkScannerClient.soap` calls with:

```go
func (c NetworkScannerClient) call(ctx context.Context, req ScanRequest, service onvif.Service, action, body string) (string, error) {
	target := onvif.Target{
		Host: req.Host, Port: req.ONVIFPort,
		Username: req.Username, Password: req.Password,
	}
	return onvif.NewClient(c.HTTPClient).Call(ctx, target, service, action, body)
}
```

Map device/media/PTZ methods to `ServiceDevice`, `ServiceMedia`, and `ServicePTZ`. Replace the existing `xmlEscape(profileToken)` call in `StreamURI` with `onvif.Escape(profileToken)`. Remove only the now-duplicated `soapEnvelope`, `xmlEscape`, and endpoint URL helpers from `profile_helpers.go`; retain stream URL, redaction, XML text parsing, FPS, and camera-profile helpers.

- [ ] **Step 5: Run only the affected package tests**

Run:

```bash
go test ./internal/onvif ./internal/cameraprofile -count=1
```

Expected: PASS for both packages with no camera network access.

- [ ] **Step 6: Commit Task 1**

```bash
git add internal/onvif internal/cameraprofile/onvif_client.go internal/cameraprofile/profile_helpers.go
git commit -m "refactor: share ONVIF transport"
```

### Task 2: Persist typed camera-control capabilities

**Files:**
- Modify: `internal/store/models.go`
- Modify: `internal/store/schema.go`
- Modify: `internal/store/schema_camera_profiles.go`
- Modify: `internal/store/camera_rows.go`
- Modify: `internal/store/cameras.go`
- Create: `internal/store/camera_control_capabilities_test.go`

**Interfaces:**
- Produces: `store.ControlSupport`, `store.CameraControlFeature`, `store.CameraControlCapabilities`, `Camera.ControlCapabilities`, and `DB.UpdateCameraControlCapabilities(context.Context, string, CameraControlCapabilities) error`.
- Consumed by: Tasks 3-6.

- [ ] **Step 1: Write failing migration and persistence tests**

Create tests covering the safe default and exact stable-stream update:

```go
func TestCameraControlCapabilitiesDefaultToUnknown(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name: "No controls", URL: "rtsp://camera.invalid/live",
		StreamName: "no-controls", State: "streaming",
	})
	if err != nil {
		t.Fatalf("UpsertCamera: %v", err)
	}
	if camera.ControlCapabilities.PTZ.Support != ControlSupportUnknown || camera.ControlCapabilities.PTZ.Available {
		t.Fatalf("unexpected PTZ default: %#v", camera.ControlCapabilities.PTZ)
	}
}

func TestUpdateCameraControlCapabilitiesUsesStableStreamOnly(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name: "염소장", URL: "rtsp://operator:secret@camera.invalid/main",
		StreamName: "goat-yard", LiveStreamName: "goat-yard-live", State: "streaming",
	})
	if err != nil {
		t.Fatalf("UpsertCamera: %v", err)
	}
	caps := CameraControlCapabilities{
		PTZ: CameraControlFeature{Support: ControlSupportSupported, Available: true},
		Home: CameraControlFeature{Support: ControlSupportSupported, Available: true},
		MaxPresets: 100,
	}
	if err := db.UpdateCameraControlCapabilities(t.Context(), camera.StreamName, caps); err != nil {
		t.Fatalf("UpdateCameraControlCapabilities: %v", err)
	}
	stored, err := db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil || !stored.ControlCapabilities.PTZ.Available || stored.ControlCapabilities.MaxPresets != 100 {
		t.Fatalf("stored capabilities/error = %#v/%v", stored.ControlCapabilities, err)
	}
	if err := db.UpdateCameraControlCapabilities(t.Context(), camera.LiveStreamName, caps); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("role alias error = %v, want sql.ErrNoRows", err)
	}
}

func TestCameraControlCapabilitiesMalformedJSONDefaultsToUnknown(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{StreamName: "malformed", URL: "rtsp://camera.invalid/live"})
	if err != nil { t.Fatalf("UpsertCamera: %v", err) }
	if _, err := db.db.ExecContext(t.Context(), `UPDATE cameras SET control_capabilities_json = ? WHERE id = ?`, `{broken`, camera.ID); err != nil {
		t.Fatalf("corrupt capability fixture: %v", err)
	}
	stored, err := db.GetCameraByStream(t.Context(), camera.StreamName)
	if err != nil { t.Fatalf("GetCameraByStream: %v", err) }
	if stored.ControlCapabilities.PTZ.Support != ControlSupportUnknown || stored.ControlCapabilities.PTZ.Available {
		t.Fatalf("malformed default = %#v", stored.ControlCapabilities.PTZ)
	}
}
```

Use the existing `openMigratedStore` helper from `profile_templates_test_helpers_test.go`; do not add another database setup helper.

- [ ] **Step 2: Run the store tests and verify failure**

Run:

```bash
go test ./internal/store -run '^Test(CameraControlCapabilities|UpdateCameraControlCapabilities)' -count=1
```

Expected: FAIL because the types, field, column, and update method do not exist.

- [ ] **Step 3: Add the typed model and safe defaults**

Add to `internal/store/models.go`:

```go
type ControlSupport string

const (
	ControlSupportUnknown     ControlSupport = "unknown"
	ControlSupportSupported   ControlSupport = "supported"
	ControlSupportUnsupported ControlSupport = "unsupported"
)

type CameraControlFeature struct {
	Support   ControlSupport `json:"support"`
	Available bool           `json:"available"`
	Reason    string         `json:"reason,omitempty"`
}

type CameraControlCapabilities struct {
	PTZ          CameraControlFeature `json:"ptz"`
	Home         CameraControlFeature `json:"home"`
	Presets      CameraControlFeature `json:"presets"`
	Listen       CameraControlFeature `json:"listen"`
	Talk         CameraControlFeature `json:"talk"`
	Siren        CameraControlFeature `json:"siren"`
	MaxPresets   int                  `json:"maxPresets,omitempty"`
	DiscoveredAt string               `json:"discoveredAt,omitempty"`
}
```

Add `ControlCapabilities CameraControlCapabilities` to `store.Camera`. When decoded JSON is `{}` or invalid, normalize every empty `Support` to `ControlSupportUnknown` and `Available` to false.

Use one normalization helper for reads and writes:

```go
func normalizeControlCapabilities(value CameraControlCapabilities) CameraControlCapabilities {
	features := []*CameraControlFeature{&value.PTZ, &value.Home, &value.Presets, &value.Listen, &value.Talk, &value.Siren}
	for _, feature := range features {
		switch feature.Support {
		case ControlSupportSupported, ControlSupportUnsupported, ControlSupportUnknown:
		default:
			feature.Support = ControlSupportUnknown
			feature.Available = false
		}
		if feature.Support != ControlSupportSupported {
			feature.Available = false
		}
	}
	if value.MaxPresets < 0 { value.MaxPresets = 0 }
	return value
}
```

In `scanCamera`, scan the new column into `controlCapabilitiesJSON`; initialize an empty value, ignore malformed legacy JSON, then assign `camera.ControlCapabilities = normalizeControlCapabilities(decoded)`. This prevents malformed stored JSON from breaking camera listing.

- [ ] **Step 4: Add schema, row scanning, and exact update persistence**

Add `control_capabilities_json TEXT NOT NULL DEFAULT '{}'` immediately after `last_scan_json` in the base `cameras` table, and add `{"control_capabilities_json", "TEXT NOT NULL DEFAULT '{}'"}` to `ensureCameraProfileSchema`. In `UpsertCamera`, marshal `normalizeControlCapabilities(camera.ControlCapabilities)`, add the column/argument to INSERT, and assign `control_capabilities_json=excluded.control_capabilities_json` on conflict. Add the column immediately after `last_scan_json` in `ListCameras` and `GetCameraByStream`, then scan it in that exact position in `scanCamera`.

Implement the exact stable-stream update:

```go
func (d *DB) UpdateCameraControlCapabilities(ctx context.Context, streamName string, capabilities CameraControlCapabilities) error {
	payload, err := json.Marshal(normalizeControlCapabilities(capabilities))
	if err != nil {
		return err
	}
	result, err := d.db.ExecContext(ctx,
		`UPDATE cameras SET control_capabilities_json = ?, updated_at = ? WHERE stream_name = ?`,
		string(payload), time.Now().UTC().Format(time.RFC3339Nano), streamName,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return sql.ErrNoRows
	}
	return nil
}
```

- [ ] **Step 5: Run only the store capability tests**

Run:

```bash
go test ./internal/store -run 'CameraControlCapabilities|UpdateCameraControlCapabilities' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 2**

```bash
git add internal/store/models.go internal/store/schema.go internal/store/schema_camera_profiles.go internal/store/camera_rows.go internal/store/cameras.go internal/store/camera_control_capabilities_test.go
git commit -m "feat: persist camera control capabilities"
```

### Task 3: Implement the ordered ONVIF camera controller

**Files:**
- Create: `internal/cameracontrol/controller.go`
- Create: `internal/cameracontrol/controller_test.go`

**Interfaces:**
- Consumes: Task 1 `onvif.Client`; Task 2 capability types; existing `store.Camera` and role-stream profile tokens.
- Produces: `cameracontrol.Controller`, `New(onvif.Client) *Controller`, `Status`, `Preset`, `MoveVector`, and methods `Discover`, `Status`, `Move`, `Stop`, `GotoHome`, `SetHome`, `ListPresets`, `CreatePreset`, `GotoPreset`, and `DeletePreset`.

- [ ] **Step 1: Write failing controller behavior and ordering tests**

The tests use an unexported injected call function, not a new public mock framework:

```go
func TestControllerStopIsFinalWireCommand(t *testing.T) {
	moveStarted := make(chan struct{})
	releaseMove := make(chan struct{})
	var mu sync.Mutex
	var actions []string
	controller := newWithCall(func(ctx context.Context, _ onvif.Target, _ onvif.Service, action, _ string) (string, error) {
		mu.Lock()
		actions = append(actions, path.Base(action))
		mu.Unlock()
		if strings.HasSuffix(action, "/ContinuousMove") {
			close(moveStarted)
			select {
			case <-releaseMove:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return `<Envelope><Body/></Envelope>`, nil
	})
	camera := controlTestCamera()
	moveDone := make(chan error, 1)
	go func() { moveDone <- controller.Move(context.Background(), camera, MoveVector{Pan: .4}) }()
	<-moveStarted
	stopDone := make(chan error, 1)
	go func() { stopDone <- controller.Stop(context.Background(), camera) }()
	if err := <-stopDone; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	close(releaseMove)
	_ = <-moveDone
	mu.Lock()
	defer mu.Unlock()
	if got := actions[len(actions)-1]; got != "Stop" {
		t.Fatalf("last action = %q, want Stop; all=%v", got, actions)
	}
}

func TestControllerParsesCapabilitiesStatusAndPresets(t *testing.T) {
	controller := controllerWithFixtureResponses(t, map[string]string{
		"GetNodes": ptzNodeFixture(true, 100),
		"GetAudioSources": audioSourcesFixture(1),
		"GetStatus": ptzStatusFixture("IDLE", "IDLE"),
		"GetPresets": presetsFixture("preset-1", "입구"),
	})
	camera := controlTestCamera()
	caps, err := controller.Discover(t.Context(), camera)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !caps.PTZ.Available || !caps.Home.Available || caps.MaxPresets != 100 {
		t.Fatalf("capabilities = %#v", caps)
	}
	if caps.Listen.Support != store.ControlSupportSupported || caps.Listen.Available {
		t.Fatalf("listen = %#v", caps.Listen)
	}
	status, err := controller.Status(t.Context(), camera)
	if err != nil || status.PanTilt != "IDLE" || status.Zoom != "IDLE" {
		t.Fatalf("status/error = %#v/%v", status, err)
	}
	presets, err := controller.ListPresets(t.Context(), camera)
	if err != nil || len(presets) != 1 || presets[0].Token != "preset-1" || presets[0].Name != "입구" {
		t.Fatalf("presets/error = %#v/%v", presets, err)
	}
}
```

Define the test-only fixtures in the same file so no production mock surface is added:

```go
func controlTestCamera() store.Camera {
	return store.Camera{
		Name: "염소장", StreamName: "goat-yard", Host: "192.0.2.10", ONVIFPort: 80,
		URL: "rtsp://operator:camera-secret@192.0.2.10/live",
		Streams: []store.CameraStream{
			{Role: store.CameraStreamRoleRecording, ProfileToken: "PROFILE_000"},
			{Role: store.CameraStreamRoleLive, ProfileToken: "PROFILE_001"},
		},
	}
}

func controllerWithFixtureResponses(t *testing.T, responses map[string]string) *Controller {
	t.Helper()
	return newWithCall(func(_ context.Context, _ onvif.Target, _ onvif.Service, action, _ string) (string, error) {
		operation := path.Base(action)
		response, ok := responses[operation]
		if !ok {
			t.Fatalf("unexpected ONVIF operation %q", operation)
		}
		return response, nil
	})
}

func ptzNodeFixture(home bool, maxPresets int) string {
	return fmt.Sprintf(`<Envelope><Body><GetNodesResponse><PTZNode token="node-1"><HomeSupported>%t</HomeSupported><MaximumNumberOfPresets>%d</MaximumNumberOfPresets><SupportedPTZSpaces><ContinuousPanTiltVelocitySpace><XRange><Min>-1</Min><Max>1</Max></XRange><YRange><Min>-1</Min><Max>1</Max></YRange></ContinuousPanTiltVelocitySpace><ContinuousZoomVelocitySpace><XRange><Min>-1</Min><Max>1</Max></XRange></ContinuousZoomVelocitySpace></SupportedPTZSpaces></PTZNode></GetNodesResponse></Body></Envelope>`, home, maxPresets)
}

func audioSourcesFixture(count int) string {
	var sources strings.Builder
	for index := 0; index < count; index++ {
		fmt.Fprintf(&sources, `<AudioSources token="audio-%d"/>`, index)
	}
	return `<Envelope><Body><GetAudioSourcesResponse>` + sources.String() + `</GetAudioSourcesResponse></Body></Envelope>`
}

func ptzStatusFixture(panTilt, zoom string) string {
	return fmt.Sprintf(`<Envelope><Body><GetStatusResponse><PTZStatus><MoveStatus><PanTilt>%s</PanTilt><Zoom>%s</Zoom></MoveStatus></PTZStatus></GetStatusResponse></Body></Envelope>`, panTilt, zoom)
}

func presetsFixture(token, name string) string {
	return fmt.Sprintf(`<Envelope><Body><GetPresetsResponse><Preset token="%s"><Name>%s</Name></Preset></GetPresetsResponse></Body></Envelope>`, token, name)
}
```

Also add these named focused tests, each with a `newWithCall` closure that captures the outgoing body: `TestControllerEscapesPresetNameAndToken`, `TestControllerClampsMoveVectors`, `TestTargetForCameraUsesURLCredentialsAndRecordingToken`, `TestTargetForCameraRejectsMissingProfileToken`, and `TestControllerErrorsDoNotContainCredentials`. The escaping test uses `A&B<입구>` and `preset/a?b&c`; the clamp test supplies `{Pan: 3, Tilt: -4, Zoom: .5}` and asserts serialized values `1`, `-1`, `.5`, and device timeout `PT2S`; the safe-error test returns `errors.New("camera-secret at http://192.0.2.10/onvif")` from the injected call and asserts the public controller error contains neither substring.

- [ ] **Step 2: Run the controller tests and verify failure**

Run:

```bash
go test ./internal/cameracontrol -count=1
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Add the concrete types and target resolution**

Use these public types:

```go
type Status struct {
	PanTilt string `json:"panTilt"`
	Zoom    string `json:"zoom"`
}

type Preset struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

type MoveVector struct {
	Pan  float64 `json:"pan"`
	Tilt float64 `json:"tilt"`
	Zoom float64 `json:"zoom"`
}

type callFunc func(context.Context, onvif.Target, onvif.Service, string, string) (string, error)

type Controller struct {
	call   callFunc
	states sync.Map
}

func New(client onvif.Client) *Controller {
	return newWithCall(client.Call)
}

func newWithCall(call callFunc) *Controller {
	return &Controller{call: call}
}
```

Implement `targetForCamera(camera store.Camera) (onvif.Target, string, error)`. It parses credentials only from the stored `Camera.URL`, uses `Camera.Host` and `Camera.ONVIFPort`, requires a nonempty stable `StreamName`, and returns the recording profile token before the live token. It never includes the parsed target in errors.

- [ ] **Step 4: Implement discovery, status, home, and preset SOAP operations**

Use the verified operation/action pairs:

```go
const (
	actionGetNodes        = "http://www.onvif.org/ver20/ptz/wsdl/GetNodes"
	actionGetAudioSources = "http://www.onvif.org/ver10/media/wsdl/GetAudioSources"
	actionGetStatus       = "http://www.onvif.org/ver20/ptz/wsdl/GetStatus"
	actionContinuousMove  = "http://www.onvif.org/ver20/ptz/wsdl/ContinuousMove"
	actionStop            = "http://www.onvif.org/ver20/ptz/wsdl/Stop"
	actionGotoHome        = "http://www.onvif.org/ver20/ptz/wsdl/GotoHomePosition"
	actionSetHome         = "http://www.onvif.org/ver20/ptz/wsdl/SetHomePosition"
	actionGetPresets      = "http://www.onvif.org/ver20/ptz/wsdl/GetPresets"
	actionSetPreset       = "http://www.onvif.org/ver20/ptz/wsdl/SetPreset"
	actionGotoPreset      = "http://www.onvif.org/ver20/ptz/wsdl/GotoPreset"
	actionRemovePreset    = "http://www.onvif.org/ver20/ptz/wsdl/RemovePreset"
)
```

Parse XML by local element name with `encoding/xml`, as the scanner already does. `Discover` requires PTZ `GetNodes` with body `<tptz:GetNodes/>` and then makes one best-effort Media `GetAudioSources` call with body `<trt:GetAudioSources/>`. It sets PTZ/home/preset availability from `GetNodes`; an audio-source response with at least one `AudioSources` element sets Listen support to supported but availability false with reason `browser_audio_unavailable`, while an unavailable audio query leaves Listen unknown with reason `protocol_unverified` without discarding the confirmed PTZ result. Talk/Siren remain unknown and unavailable with reasons `standard_control_unverified` and `protocol_unverified`. Set `DiscoveredAt` to the current UTC RFC3339Nano timestamp. `CreatePreset`, `GotoPreset`, and `DeletePreset` trim names but preserve opaque tokens, validate valid UTF-8 and `utf8.RuneCountInString` in the range 1-64, and call `onvif.Escape` before interpolation.

For `GetStatus`, use a token decoder that enters the `MoveStatus` element and reads only its child `PanTilt` and `Zoom` text. Do not reuse the first `PanTilt` element from `Position`, because that element carries coordinates rather than movement state. Normalize empty status values to `UNKNOWN`.

Build movement and Stop bodies exactly so the camera has a device-side timeout even if the browser disappears:

```go
func continuousMoveBody(token string, move MoveVector) string {
	return fmt.Sprintf(`<tptz:ContinuousMove><tptz:ProfileToken>%s</tptz:ProfileToken><tptz:Velocity><tt:PanTilt x="%g" y="%g"/><tt:Zoom x="%g"/></tptz:Velocity><tptz:Timeout>PT2S</tptz:Timeout></tptz:ContinuousMove>`, onvif.Escape(token), move.Pan, move.Tilt, move.Zoom)
}

func stopBody(token string) string {
	return `<tptz:Stop><tptz:ProfileToken>` + onvif.Escape(token) + `</tptz:ProfileToken><tptz:PanTilt>true</tptz:PanTilt><tptz:Zoom>true</tptz:Zoom></tptz:Stop>`
}
```

Home bodies contain only escaped `ProfileToken`. Preset set/goto/remove bodies contain escaped `ProfileToken` plus escaped `PresetName` or `PresetToken`. `CreatePreset` parses the opaque token from `SetPresetResponse` and returns `Preset{Token: token, Name: trimmedName}`; an empty returned token is `ErrUnavailable`.

Normalize every injected transport failure before returning it from an exported controller method:

```go
var (
	ErrUnavailable         = errors.New("camera control unavailable")
	ErrAuthenticationFailed = errors.New("camera authentication failed")
	ErrInvalidCommand      = errors.New("invalid camera control command")
	ErrTimeout             = errors.New("camera control timeout")
)

func safeControlError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, onvif.ErrAuthenticationFailed) {
		return ErrAuthenticationFailed
	}
	return ErrUnavailable
}
```

Validation failures wrap only `ErrInvalidCommand`; missing host/port/profile data wraps only `ErrUnavailable`. Never wrap the underlying URL, SOAP, or transport error with `%w` at the exported boundary.

- [ ] **Step 5: Implement one command gate per camera**

Use a metadata mutex plus a wire-command mutex:

```go
type commandState struct {
	gate       sync.Mutex
	meta       sync.Mutex
	generation uint64
	callID     uint64
	cancel     context.CancelFunc
}

func (c *Controller) runNonStop(ctx context.Context, camera store.Camera, action, body string) error {
	target, _, err := targetForCamera(camera)
	if err != nil {
		return err
	}
	state := c.state(camera.StreamName)
	state.meta.Lock()
	generation := state.generation
	state.meta.Unlock()

	state.gate.Lock()
	defer state.gate.Unlock()
	state.meta.Lock()
	if generation != state.generation {
		state.meta.Unlock()
		return context.Canceled
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	state.callID++
	callID := state.callID
	state.cancel = cancel
	state.meta.Unlock()
	defer func() {
		cancel()
		state.meta.Lock()
		if state.callID == callID {
			state.cancel = nil
		}
		state.meta.Unlock()
	}()
	_, err = c.call(callCtx, target, onvif.ServicePTZ, action, body)
	return err
}

func (c *Controller) Stop(ctx context.Context, camera store.Camera) error {
	target, token, err := targetForCamera(camera)
	if err != nil {
		return err
	}
	state := c.state(camera.StreamName)
	state.meta.Lock()
	state.generation++
	cancel := state.cancel
	state.meta.Unlock()
	if cancel != nil {
		cancel()
	}
	state.gate.Lock()
	defer state.gate.Unlock()
	stopCtx, stopCancel := context.WithTimeout(ctx, 2*time.Second)
	defer stopCancel()
	_, err = c.call(stopCtx, target, onvif.ServicePTZ, actionStop, stopBody(token))
	return err
}
```

`state(streamName)` uses `sync.Map.LoadOrStore` and returns one `*commandState` per stable camera stream. `Move` clamps each component before calling `runNonStop` and rejects an all-zero vector.

- [ ] **Step 6: Run only the controller tests**

Run:

```bash
go test ./internal/cameracontrol -count=1
```

Expected: PASS, including `TestControllerStopIsFinalWireCommand`.

- [ ] **Step 7: Commit Task 3**

```bash
git add internal/cameracontrol
git commit -m "feat: add ordered ONVIF camera controller"
```

### Task 4: Expose guarded camera-control APIs

**Files:**
- Create: `cmd/camstationd/routes_camera_controls.go`
- Create: `cmd/camstationd/routes_camera_controls_test.go`
- Modify: `cmd/camstationd/routes.go`
- Modify: `cmd/camstationd/routes_cameras.go`
- Modify: `cmd/camstationd/routes_camera_mutations.go`
- Modify: `cmd/camstationd/routes_public_dtos.go`

**Interfaces:**
- Consumes: Task 2 store capability persistence and Task 3 controller methods.
- Produces: the exact HTTP API in the design and `publicCamera.controlCapabilities` for Task 5.

- [ ] **Step 1: Write failing guarded route tests with a narrow fake**

Define the route-local interface and fake in the test file. Cover GET management headers, role alias rejection, refresh persistence, move clamping, fixed preset token bodies, and secret redaction:

```go
func TestCameraControlRoutesRequireManagementHeaderForGET(t *testing.T) {
	server := newCameraControlRouteServer(t, &fakeCameraController{})
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodGet, "/api/cameras/goat-yard/controls", "", nil)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", status, http.StatusForbidden)
	}
}

func TestCameraControlRoutesStopAndPresetBodies(t *testing.T) {
	fake := &fakeCameraController{presets: []cameracontrol.Preset{{Token: "preset/a?b", Name: "입구"}}}
	server := newCameraControlRouteServer(t, fake)
	headers := trustedConsoleHeaders()
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets/goto", `{"token":"preset/a?b"}`, headers)
	if status != http.StatusOK || fake.gotoPresetToken != "preset/a?b" {
		t.Fatalf("status/token = %d/%q", status, fake.gotoPresetToken)
	}
	status, _ = requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/stop", `{}`, headers)
	if status != http.StatusOK || fake.stopCalls != 1 {
		t.Fatalf("status/stopCalls = %d/%d", status, fake.stopCalls)
	}
}
```

The server helper injects `cameraController` directly into `routeDeps`; it must not change the existing `routes(...)` function signature.

Implement the test server with the existing `testRouteServer` return type:

```go
func newCameraControlRouteServer(t *testing.T, controller cameraControlService) testRouteServer {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil { t.Fatalf("open store: %v", err) }
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil { t.Fatalf("migrate store: %v", err) }
	camera, err := db.UpsertCamera(t.Context(), store.Camera{
		Name: "염소장", URL: "rtsp://operator:camera-secret@192.0.2.10/main",
		StreamName: "goat-yard", State: "streaming", Host: "192.0.2.10", ONVIFPort: 80,
	})
	if err != nil { t.Fatalf("seed camera: %v", err) }
	if err := db.ReplaceCameraStreams(t.Context(), camera.ID, []store.CameraStream{
		{Role: store.CameraStreamRoleRecording, URL: camera.URL, Go2RTCStreamName: "goat-yard-recording", ProfileToken: "PROFILE_000", State: "streaming"},
		{Role: store.CameraStreamRoleLive, URL: camera.URL, Go2RTCStreamName: "goat-yard-live", ProfileToken: "PROFILE_001", State: "streaming"},
	}); err != nil { t.Fatalf("seed streams: %v", err) }
	handler, err := (routeDeps{db: db, cameraController: controller}).handler()
	if err != nil { t.Fatalf("build routes: %v", err) }
	return testRouteServer{handler: handler, db: db}
}
```

Define the fake in the same file (tests in this package are sequential, so no extra mutex is needed):

```go
type fakeCameraController struct {
	capabilities store.CameraControlCapabilities
	status cameracontrol.Status
	presets []cameracontrol.Preset
	move cameracontrol.MoveVector
	moveCalls int
	gotoPresetToken, deletePresetToken, createdPresetName string
	stopCalls int
	err error
}

func (f *fakeCameraController) Discover(context.Context, store.Camera) (store.CameraControlCapabilities, error) { return f.capabilities, f.err }
func (f *fakeCameraController) Status(context.Context, store.Camera) (cameracontrol.Status, error) { return f.status, f.err }
func (f *fakeCameraController) Move(_ context.Context, _ store.Camera, move cameracontrol.MoveVector) error { f.move = move; f.moveCalls++; return f.err }
func (f *fakeCameraController) Stop(context.Context, store.Camera) error { f.stopCalls++; return f.err }
func (f *fakeCameraController) GotoHome(context.Context, store.Camera) error { return f.err }
func (f *fakeCameraController) SetHome(context.Context, store.Camera) error { return f.err }
func (f *fakeCameraController) ListPresets(context.Context, store.Camera) ([]cameracontrol.Preset, error) { return f.presets, f.err }
func (f *fakeCameraController) CreatePreset(_ context.Context, _ store.Camera, name string) (cameracontrol.Preset, error) {
	f.createdPresetName = name
	return cameracontrol.Preset{Token: "created-token", Name: name}, f.err
}
func (f *fakeCameraController) GotoPreset(_ context.Context, _ store.Camera, token string) error { f.gotoPresetToken = token; return f.err }
func (f *fakeCameraController) DeletePreset(_ context.Context, _ store.Camera, token string) error { f.deletePresetToken = token; return f.err }
```

Reuse the existing shared `requestJSONWithHeaders` and `trustedConsoleHeaders` helpers rather than redefining them.

Add four more named route tests in this file:

- `TestCameraControlRoutesRejectRoleStreamAlias`: call `/api/cameras/goat-yard-live/ptz/stop` with trusted headers; assert `404` and `fake.stopCalls == 0`.
- `TestCameraControlRoutesRefreshPersistsCapabilities`: configure supported PTZ/home/presets and `MaxPresets:100`; POST refresh; reload `goat-yard` from `server.db`; assert the stored summary and response match and contain no `camera-secret` or `192.0.2.10`.
- `TestCameraControlRoutesClampMoveAndRejectZero`: POST `{ "pan":3,"tilt":-2,"zoom":0.5 }`; assert the fake received `{1,-1,0.5}`; POST all zeros and assert `400` without another Move call.
- `TestCameraControlRoutesNormalizeControllerErrors`: make the fake return each controller sentinel and assert the exact status/Korean message map from Step 4; also marshal every payload and assert it contains neither the camera secret, host, `rtsp://`, nor `onvif`.

- [ ] **Step 2: Run the route tests and verify failure**

Run:

```bash
go test ./cmd/camstationd -run '^TestCameraControlRoutes' -count=1
```

Expected: FAIL because the routes and dependency do not exist.

- [ ] **Step 3: Add the route dependency without changing existing callers**

Add to `routes.go`:

```go
type cameraControlService interface {
	Discover(context.Context, store.Camera) (store.CameraControlCapabilities, error)
	Status(context.Context, store.Camera) (cameracontrol.Status, error)
	Move(context.Context, store.Camera, cameracontrol.MoveVector) error
	Stop(context.Context, store.Camera) error
	GotoHome(context.Context, store.Camera) error
	SetHome(context.Context, store.Camera) error
	ListPresets(context.Context, store.Camera) ([]cameracontrol.Preset, error)
	CreatePreset(context.Context, store.Camera, string) (cameracontrol.Preset, error)
	GotoPreset(context.Context, store.Camera, string) error
	DeletePreset(context.Context, store.Camera, string) error
}
```

Add `cameraController cameraControlService` to `routeDeps`. At the start of `handler`, when it is nil, construct `cameracontrol.New(onvif.NewClient(&http.Client{Timeout: 8 * time.Second}))`. Existing tests and `routes(...)` callers remain unchanged.

- [ ] **Step 4: Implement stable-target loading, strict JSON, and safe route handlers**

Use one exact target helper:

```go
func (d routeDeps) controlCamera(ctx context.Context, streamName string) (store.Camera, error) {
	camera, err := d.db.GetCameraByStream(ctx, streamName)
	if err != nil || camera.StreamName != streamName {
		return store.Camera{}, sql.ErrNoRows
	}
	return camera, nil
}
```

Every handler calls `requireCameraManagementRequest`, uses `context.WithTimeout`, and writes only normalized values. The move decoder uses `DisallowUnknownFields`, clamps each component to `[-1, 1]`, and rejects the all-zero vector. Preset bodies are exactly `{name}` or `{token}` and accept at most 64 characters.

Use one bounded decoder for every request body:

```go
func decodeControlJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "제어 요청 형식이 올바르지 않습니다."})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "제어 요청은 하나의 JSON 객체여야 합니다."})
		return false
	}
	return true
}
```

Map controller errors without returning `err.Error()`:

```go
func writeCameraControlError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cameracontrol.ErrInvalidCommand):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "카메라 제어 명령이 올바르지 않습니다."})
	case errors.Is(err, cameracontrol.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "카메라 제어 응답 시간이 초과되었습니다."})
	case errors.Is(err, cameracontrol.ErrAuthenticationFailed):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "카메라 인증에 실패했습니다."})
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "등록된 카메라를 찾을 수 없습니다."})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "카메라 제어를 사용할 수 없습니다."})
	}
}
```

An unavailable/offline capability check returns `409` with `카메라가 제어 가능한 상태가 아닙니다.`; apply it to Move, home, and preset actions using their matching feature capability. Stop remains callable for every registered camera even when its status/capability is unavailable, and refresh remains callable for unknown capabilities. The management guard retains its existing forbidden response. `GET /controls` returns persisted capabilities plus `{panTilt:"UNKNOWN",zoom:"UNKNOWN"}` without a device call when PTZ is not available, otherwise it calls `Status` once. Authentication classification comes only from the safe HTTP 401/403 sentinel; raw SOAP faults are still normalized to unavailable.

If `Move` returns an error, the route invokes one best-effort `Stop` before returning the normalized failure. A failed Stop is returned once and never starts a retry loop.

Register these fixed routes:

```go
GET  /api/cameras/{streamName}/controls
POST /api/cameras/{streamName}/controls/refresh
POST /api/cameras/{streamName}/ptz/move
POST /api/cameras/{streamName}/ptz/stop
POST /api/cameras/{streamName}/ptz/home/goto
POST /api/cameras/{streamName}/ptz/home/set
GET  /api/cameras/{streamName}/ptz/presets
POST /api/cameras/{streamName}/ptz/presets
POST /api/cameras/{streamName}/ptz/presets/goto
POST /api/cameras/{streamName}/ptz/presets/delete
```

`controls/refresh` persists through `UpdateCameraControlCapabilities`. Home set, preset create/delete, and normalized failures append bounded events; move renewals do not append events.

Use `Source: "camera-control"` and details containing only `streamName` and an operation enum from `refresh`, `move`, `stop`, `home_goto`, `home_set`, `preset_list`, `preset_create`, `preset_goto`, or `preset_delete`. Success events are emitted only for `home_set`, `preset_create`, and `preset_delete`; failures emit `Level:"warning"`, `Message:"카메라 제어 요청 실패"`, and a safe category `invalid`, `timeout`, `authentication`, `unavailable`, or `not_found`. Never put a preset name/token, controller `error`, URL, host, or SOAP value in event details. Event append failure does not replace the control response.

- [ ] **Step 5: Persist scan-provided partial capability summaries without another network request**

Add a small helper in `routes_camera_controls.go`:

```go
func controlCapabilitiesFromProfile(profile cameraprofile.DeviceProfile) store.CameraControlCapabilities {
	unknown := store.CameraControlFeature{Support: store.ControlSupportUnknown, Available: false, Reason: "protocol_unverified"}
	caps := store.CameraControlCapabilities{Home: unknown, Presets: unknown, Talk: unknown, Siren: unknown}
	if profile.Capabilities.PTZ {
		caps.PTZ = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: true}
	} else {
		caps.PTZ = unknown
	}
	if profile.Capabilities.MaxPresets > 0 {
		caps.Presets = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: true}
		caps.MaxPresets = profile.Capabilities.MaxPresets
	}
	if profile.Capabilities.Microphone {
		caps.Listen = store.CameraControlFeature{Support: store.ControlSupportSupported, Available: false, Reason: "browser_audio_unavailable"}
	} else {
		caps.Listen = unknown
	}
	return caps
}
```

After successful camera create/update, persist this summary only when the request contains a scanned profile. Leave unknown fields unknown so the live workspace performs one full guarded refresh. Do not modify the already-dirty `camera_profile_helpers.go`.

Treat the request as scanned only when `len(req.Profile.Channels) > 0`. After `UpdateCameraControlCapabilities` succeeds, reload `saved` with `GetCameraByStream` before building `publicSaved`; an update request without profile channels preserves the existing summary instead of overwriting it with unknown values.

- [ ] **Step 6: Add the safe public DTO field**

Add:

```go
ControlCapabilities store.CameraControlCapabilities `json:"controlCapabilities"`
```

to `publicCamera` and copy the normalized store value in `publicCameraFromStore`. Do not expose the raw JSON column or discovery SOAP.

- [ ] **Step 7: Run only the camera-control route tests**

Run:

```bash
go test ./cmd/camstationd -run '^TestCameraControlRoutes' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit Task 4**

```bash
git add cmd/camstationd/routes.go cmd/camstationd/routes_cameras.go cmd/camstationd/routes_camera_controls.go cmd/camstationd/routes_camera_controls_test.go cmd/camstationd/routes_camera_mutations.go cmd/camstationd/routes_public_dtos.go
git commit -m "feat: expose guarded PTZ APIs"
```

### Task 5: Add typed frontend control requests and queries

**Files:**
- Modify: `web/src/app/http.ts`
- Modify: `web/src/app/cameraTypes.ts`
- Create: `web/src/app/cameraControlApi.ts`
- Create: `web/src/app/cameraControlQueries.ts`
- Modify: `web/src/app/api.ts`
- Modify: `web/src/app/queries.ts`

**Interfaces:**
- Consumes: Task 4 HTTP responses.
- Produces: `api.cameraControls`, `api.refreshCameraControls`, move/stop/home/preset methods, `useCameraControls`, `useCameraPresets`, and bounded mutations for Task 6.

- [ ] **Step 1: Add the guarded request wrapper and exact frontend types**

Add to `http.ts` without changing ordinary GET behavior:

```ts
export function managementRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("X-CamStation-Management", "1");
  return request<T>(path, { ...init, headers });
}
```

Add to `cameraTypes.ts`:

```ts
export type ControlSupport = "unknown" | "supported" | "unsupported";

export type CameraControlFeature = {
  readonly support: ControlSupport;
  readonly available: boolean;
  readonly reason?: string;
};

export type CameraControlCapabilities = {
  readonly ptz: CameraControlFeature;
  readonly home: CameraControlFeature;
  readonly presets: CameraControlFeature;
  readonly listen: CameraControlFeature;
  readonly talk: CameraControlFeature;
  readonly siren: CameraControlFeature;
  readonly maxPresets?: number;
  readonly discoveredAt?: string;
};

export type CameraControlStatus = { readonly panTilt: string; readonly zoom: string };
export type CameraPreset = { readonly token: string; readonly name: string };
export type PTZMoveVector = { readonly pan: number; readonly tilt: number; readonly zoom: number };
```

Add `controlCapabilities: CameraControlCapabilities` to `Camera`.

- [ ] **Step 2: Implement the typed API with fixed token-body routes**

Create `cameraControlApi.ts`:

```ts
import { managementRequest } from "./http";
import type { CameraControlCapabilities, CameraControlStatus, CameraPreset, PTZMoveVector } from "./cameraTypes";

const cameraPath = (streamName: string) => `/api/cameras/${encodeURIComponent(streamName)}`;

export const cameraControlApi = {
  cameraControls: (streamName: string) =>
    managementRequest<{ readonly capabilities: CameraControlCapabilities; readonly status: CameraControlStatus }>(`${cameraPath(streamName)}/controls`),
  refreshCameraControls: (streamName: string) =>
    managementRequest<{ readonly capabilities: CameraControlCapabilities }>(`${cameraPath(streamName)}/controls/refresh`, { method: "POST", body: "{}" }),
  cameraPresets: (streamName: string) =>
    managementRequest<readonly CameraPreset[]>(`${cameraPath(streamName)}/ptz/presets`),
  moveCamera: (streamName: string, move: PTZMoveVector, signal?: AbortSignal) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/move`, { method: "POST", body: JSON.stringify(move), signal }),
  stopCamera: (streamName: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/stop`, { method: "POST", body: "{}" }),
  gotoCameraHome: (streamName: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/home/goto`, { method: "POST", body: "{}" }),
  setCameraHome: (streamName: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/home/set`, { method: "POST", body: "{}" }),
  createCameraPreset: (streamName: string, name: string) =>
    managementRequest<CameraPreset>(`${cameraPath(streamName)}/ptz/presets`, { method: "POST", body: JSON.stringify({ name }) }),
  gotoCameraPreset: (streamName: string, token: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/presets/goto`, { method: "POST", body: JSON.stringify({ token }) }),
  deleteCameraPreset: (streamName: string, token: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/presets/delete`, { method: "POST", body: JSON.stringify({ token }) }),
} as const;
```

Compose it in `api.ts` and export the new types.

- [ ] **Step 3: Add non-retrying control queries and invalidation**

Create `cameraControlQueries.ts` with:

```ts
export function useCameraControls(streamName: string, enabled: boolean) {
  return useQuery({
    queryKey: ["camera-controls", streamName],
    queryFn: () => cameraControlApi.cameraControls(streamName),
    enabled: Boolean(streamName && enabled),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 30_000,
  });
}

export function useCameraPresets(streamName: string, enabled: boolean) {
  return useQuery({
    queryKey: ["camera-presets", streamName],
    queryFn: () => cameraControlApi.cameraPresets(streamName),
    enabled: Boolean(streamName && enabled),
    retry: false,
    refetchOnWindowFocus: false,
  });
}
```

Export these exact mutation hooks: `useRefreshCameraControls()`, `useGotoCameraHome()`, `useSetCameraHome()`, `useCreateCameraPreset()`, `useGotoCameraPreset()`, and `useDeleteCameraPreset()`. Each accepts its stable target through `mutate` rather than closing over a changing camera:

```ts
type StreamTarget = { readonly streamName: string };
type PresetNameTarget = StreamTarget & { readonly name: string };
type PresetTokenTarget = StreamTarget & { readonly token: string };

export function useRefreshCameraControls() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ streamName }: StreamTarget) => cameraControlApi.refreshCameraControls(streamName),
    retry: false,
    onSuccess: async (_data, { streamName }) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["cameras"] }),
        queryClient.invalidateQueries({ queryKey: ["camera-controls", streamName] }),
      ]);
    },
  });
}

export function useGotoCameraHome() {
  return useMutation({ mutationFn: ({ streamName }: StreamTarget) => cameraControlApi.gotoCameraHome(streamName), retry: false });
}

export function useSetCameraHome() {
  return useMutation({ mutationFn: ({ streamName }: StreamTarget) => cameraControlApi.setCameraHome(streamName), retry: false });
}

export function useCreateCameraPreset() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ streamName, name }: PresetNameTarget) => cameraControlApi.createCameraPreset(streamName, name),
    retry: false,
    onSuccess: async (_data, { streamName }) => queryClient.invalidateQueries({ queryKey: ["camera-presets", streamName] }),
  });
}

export function useGotoCameraPreset() {
  return useMutation({ mutationFn: ({ streamName, token }: PresetTokenTarget) => cameraControlApi.gotoCameraPreset(streamName, token), retry: false });
}

export function useDeleteCameraPreset() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ streamName, token }: PresetTokenTarget) => cameraControlApi.deleteCameraPreset(streamName, token),
    retry: false,
    onSuccess: async (_data, { streamName }) => queryClient.invalidateQueries({ queryKey: ["camera-presets", streamName] }),
  });
}
```

Preset create/delete invalidate only `['camera-presets', streamName]`. Goto does not invalidate because it changes position, not the preset list. Re-export this module from `queries.ts`; all hooks explicitly set `retry: false`.

- [ ] **Step 4: Run a narrow frontend lint only**

Run:

```bash
cd web && npx oxlint src/app/http.ts src/app/cameraTypes.ts src/app/cameraControlApi.ts src/app/cameraControlQueries.ts src/app/api.ts src/app/queries.ts
```

Expected: exit 0 with no diagnostics. Do not run the frontend build yet.

- [ ] **Step 5: Commit Task 5**

```bash
git add web/src/app/http.ts web/src/app/cameraTypes.ts web/src/app/cameraControlApi.ts web/src/app/cameraControlQueries.ts web/src/app/api.ts web/src/app/queries.ts
git commit -m "feat: add PTZ client API"
```

### Task 6: Add the live PTZ replacement panel

**Files:**
- Create: `web/src/components/live/usePtzHold.ts`
- Create: `web/src/components/live/PtzControlPanel.tsx`
- Modify: `web/src/components/live/LiveWorkspace.tsx`
- Modify: `web/src/styles/index.css`

**Interfaces:**
- Consumes: Task 5 API and query hooks.
- Produces: live toolbar capability state, full right-panel replacement, safe press-and-hold controls, home/preset UI, and disabled listen/talk/siren controls.

- [ ] **Step 1: Implement the one-in-flight hold hook**

Create `usePtzHold.ts` around the direct move/stop API calls. The hook owns generation, timer, abort controller, and in-flight promise refs:

```ts
export function usePtzHold(streamName: string, onError: (message: string) => void) {
  const generationRef = useRef(0);
  const intentRef = useRef(0);
  const timerRef = useRef<number | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const inFlightRef = useRef<Promise<unknown> | null>(null);
  const activeRef = useRef(false);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) window.clearTimeout(timerRef.current);
    timerRef.current = null;
  }, []);

  const stopCurrent = useCallback(async (invalidateIntent: boolean): Promise<boolean> => {
    if (invalidateIntent) {
      intentRef.current += 1;
      activeRef.current = false;
    }
    generationRef.current += 1;
    clearTimer();
    abortRef.current?.abort();
    await inFlightRef.current?.catch(() => undefined);
    abortRef.current = null;
    inFlightRef.current = null;
    if (!streamName) return true;
    try {
      await cameraControlApi.stopCamera(streamName);
      return true;
    } catch (error: unknown) {
      onError(error instanceof Error ? error.message : "카메라 정지 요청에 실패했습니다.");
      return false;
    }
  }, [clearTimer, onError, streamName]);

  const stop = useCallback(async () => {
    await stopCurrent(true);
  }, [stopCurrent]);

  const start = useCallback((move: PTZMoveVector) => {
    if (!streamName) return;
    const hadMovement = activeRef.current || inFlightRef.current !== null || timerRef.current !== null;
    const intent = ++intentRef.current;
    activeRef.current = true;
    const ready = hadMovement ? stopCurrent(false) : Promise.resolve(true);
    void ready.then((stopped) => {
      if (intentRef.current !== intent) return;
      if (!stopped) { activeRef.current = false; return; }
      const generation = ++generationRef.current;
      const dispatch = () => {
        if (generationRef.current !== generation) return;
        const startedAt = performance.now();
        const abort = new AbortController();
        abortRef.current = abort;
        const request = cameraControlApi.moveCamera(streamName, move, abort.signal);
        inFlightRef.current = request;
        void request.then(() => {
          if (generationRef.current !== generation) return;
          const delay = Math.max(0, 1000 - (performance.now() - startedAt));
          timerRef.current = window.setTimeout(dispatch, delay);
        }).catch((error: unknown) => {
          if (abort.signal.aborted || generationRef.current !== generation) return;
          onError(error instanceof Error ? error.message : "카메라 이동 요청에 실패했습니다.");
          void stop();
        }).finally(() => {
          if (inFlightRef.current === request) inFlightRef.current = null;
          if (abortRef.current === abort) abortRef.current = null;
        });
      };
      dispatch();
    });
  }, [onError, stop, stopCurrent, streamName]);

  useEffect(() => {
    const stopIfActive = () => { if (activeRef.current) void stop(); };
    const stopOnVisibility = () => { if (document.hidden) stopIfActive(); };
    const stopOnEscape = (event: KeyboardEvent) => { if (event.key === "Escape") stopIfActive(); };
    window.addEventListener("blur", stopIfActive);
    document.addEventListener("visibilitychange", stopOnVisibility);
    window.addEventListener("keydown", stopOnEscape);
    return () => {
      window.removeEventListener("blur", stopIfActive);
      document.removeEventListener("visibilitychange", stopOnVisibility);
      window.removeEventListener("keydown", stopOnEscape);
      stopIfActive();
    };
  }, [stop]);
  return { start, stop };
}
```

Do not add an automatic retry. Starting a new direction first stops the previous direction.

- [ ] **Step 2: Build the focused PTZ panel component**

`PtzControlPanel.tsx` accepts:

```ts
type PtzControlPanelProps = {
  readonly camera: Camera;
  readonly onBack: () => void;
  readonly onStopReady: (stop: () => Promise<void>) => void;
};
```

Inside the component, create `handleError` with `useCallback` so `usePtzHold` does not tear down and send Stop on ordinary rerenders. Store the latest message in local state and render it once as `<p className="new-ptz-error" role="alert">{error}</p>`.

Register the current Stop callback with the parent using:

```ts
useEffect(() => {
  onStopReady(stop);
}, [onStopReady, stop]);
```

`LiveWorkspace` stores it in `const ptzStopRef = useRef<() => Promise<void>>(async () => undefined)` through a stable `useCallback`, so hiding or replacing the panel can await the current Stop before state changes.

Define these local helpers in the same file; they are not separate abstractions:

```ts
type HoldButtonProps = {
  readonly label: string;
  readonly move: PTZMoveVector;
  readonly className: string;
  readonly direction?: "up" | "down" | "left" | "right";
  readonly onStart: (move: PTZMoveVector) => void;
  readonly onStop: () => void;
  readonly children: ReactNode;
};

function HoldButton({ label, move, className, direction, onStart, onStop, children }: HoldButtonProps) {
  const pointerActive = useRef(false);
  const keyboardActive = useRef(false);
  const begin = () => onStart(move);
  const end = onStop;
  return (
    <button
      type="button"
      className={className}
      data-direction={direction}
      aria-label={label}
      onPointerDown={(event) => { event.currentTarget.setPointerCapture(event.pointerId); pointerActive.current = true; begin(); }}
      onPointerUp={() => { if (pointerActive.current) end(); pointerActive.current = false; }}
      onPointerCancel={() => { if (pointerActive.current) end(); pointerActive.current = false; }}
      onPointerLeave={() => { if (pointerActive.current) end(); pointerActive.current = false; }}
      onLostPointerCapture={() => { if (pointerActive.current) end(); pointerActive.current = false; }}
      onKeyDown={(event) => { if ((event.key === " " || event.key === "Enter") && !keyboardActive.current) { event.preventDefault(); keyboardActive.current = true; begin(); } }}
      onKeyUp={(event) => { if ((event.key === " " || event.key === "Enter") && keyboardActive.current) { event.preventDefault(); keyboardActive.current = false; end(); } }}
    >
      {children}
    </button>
  );
}
```

`HoldButton` is a top-level helper in the same file and receives stable `onStart` and `onStop` callbacks. Implement `HomeControls`, `PresetControls`, and `DeviceFeatureButtons` as local JSX blocks rather than undefined child components: home buttons call the Task 5 home mutations; preset controls bind `useCameraPresets` and the three preset mutations; feature buttons read the three capability objects and remain disabled.

Inside `PtzControlPanel`, initialize `speed` to `0.6`, `presetName` to `""`, and bind the exact Task 5 hooks. Define the three JSX blocks before `return`:

```tsx
const controls = camera.controlCapabilities;
const controlQuery = useCameraControls(camera.streamName, true);
const presetsQuery = useCameraPresets(camera.streamName, controls.presets.available);
const gotoHome = useGotoCameraHome();
const setHome = useSetCameraHome();
const createPreset = useCreateCameraPreset();
const gotoPreset = useGotoCameraPreset();
const deletePreset = useDeleteCameraPreset();
const mutationError = (value: unknown) => handleError(value instanceof Error ? value.message : "카메라 제어 요청에 실패했습니다.");

const homeActionButtons = (
  <>
    <h3>위치 / 홈</h3>
    <button type="button" disabled={!controls.home.available || gotoHome.isPending} onClick={() => gotoHome.mutate({ streamName: camera.streamName }, { onError: mutationError })}>홈으로 이동</button>
    <button type="button" disabled={!controls.home.available || setHome.isPending} onClick={() => {
      if (window.confirm("현재 카메라 위치를 홈으로 설정할까요?")) setHome.mutate({ streamName: camera.streamName }, { onError: mutationError });
    }}>현재 위치를 홈으로 설정</button>
  </>
);

const presetControls = (
  <>
    <header className="new-ptz-card-title"><h3>프리셋</h3><span>{presetsQuery.data?.length ?? 0} / {controls.maxPresets ?? "-"}</span></header>
    <form onSubmit={(event) => {
      event.preventDefault();
      const name = presetName.trim();
      if (!name) return;
      createPreset.mutate({ streamName: camera.streamName, name }, { onSuccess: () => setPresetName(""), onError: mutationError });
    }}>
      <input value={presetName} maxLength={64} onChange={(event) => setPresetName(event.target.value)} aria-label="새 프리셋 이름" />
      <button type="submit" disabled={!controls.presets.available || !presetName.trim() || createPreset.isPending}>현재 위치 저장</button>
    </form>
    <ul className="new-ptz-preset-list">
      {(presetsQuery.data ?? []).map((preset) => (
        <li key={preset.token}>
          <span>{preset.name || "이름 없는 프리셋"}</span>
          <button type="button" onClick={() => gotoPreset.mutate({ streamName: camera.streamName, token: preset.token }, { onError: mutationError })}>이동</button>
          <button type="button" onClick={() => {
            if (window.confirm(`‘${preset.name || "이름 없는 프리셋"}’ 프리셋을 삭제할까요?`)) deletePreset.mutate({ streamName: camera.streamName, token: preset.token }, { onError: mutationError });
          }}>삭제</button>
        </li>
      ))}
    </ul>
  </>
);

const disabledFeatureButtons = (
  <>
    {[
      { key: "listen", label: "소리 듣기", reason: "오디오 경로 준비 필요", capability: controls.listen },
      { key: "talk", label: "말하기", reason: "표준 제어 미확인", capability: controls.talk },
      { key: "siren", label: "사이렌", reason: "프로토콜 미확인", capability: controls.siren },
    ].map((feature) => (
      <button key={feature.key} type="button" className="new-ptz-feature" data-support={feature.capability.support} disabled title={feature.reason}>
        {feature.label}<small>{feature.reason}</small>
      </button>
    ))}
  </>
);
```

Set `handleSpeed` to `setSpeed(Number(event.currentTarget.value))`. Set `handleBack` to `async () => { await stop(); onBack(); }`. Render the stored `error` immediately below the panel header when nonempty. The feature buttons deliberately read the normalized capability container for stable layout but remain disabled in this delivery regardless of reported physical support.

Render these exact sections in order:

```tsx
<section className="new-ptz-panel" aria-label={`${camera.name} PTZ 제어`}>
  <header className="new-ptz-header">
    <button type="button" className="new-icon-button" onClick={handleBack} aria-label="PTZ 제어 닫기"><ChevronLeft /></button>
    <div><strong>PTZ 제어</strong><em>{camera.name} · ONVIF</em></div>
    <span className="new-state" aria-label="PTZ 준비됨" title={`팬/틸트 ${controlQuery.data?.status.panTilt ?? "UNKNOWN"} · 줌 ${controlQuery.data?.status.zoom ?? "UNKNOWN"}`} />
  </header>
  {error && <p className="new-ptz-error" role="alert">{error}</p>}
  <div className="new-ptz-pad" role="group" aria-label="팬 틸트 방향">
    <HoldButton label="위" direction="up" move={{ pan: 0, tilt: speed, zoom: 0 }} className="new-ptz-direction" onStart={start} onStop={() => { void stop(); }}><ChevronUp /></HoldButton>
    <HoldButton label="왼쪽" direction="left" move={{ pan: -speed, tilt: 0, zoom: 0 }} className="new-ptz-direction" onStart={start} onStop={() => { void stop(); }}><ChevronLeft /></HoldButton>
    <button type="button" className="new-ptz-stop-center" onClick={() => void stop()}>■</button>
    <HoldButton label="오른쪽" direction="right" move={{ pan: speed, tilt: 0, zoom: 0 }} className="new-ptz-direction" onStart={start} onStop={() => { void stop(); }}><ChevronRight /></HoldButton>
    <HoldButton label="아래" direction="down" move={{ pan: 0, tilt: -speed, zoom: 0 }} className="new-ptz-direction" onStart={start} onStop={() => { void stop(); }}><ChevronDown /></HoldButton>
  </div>
  <div className="new-ptz-zoom" role="group" aria-label="줌">
    <HoldButton label="확대" move={{ pan: 0, tilt: 0, zoom: speed }} className="new-ptz-zoom-button" onStart={start} onStop={() => { void stop(); }}><ZoomIn /></HoldButton>
    <HoldButton label="축소" move={{ pan: 0, tilt: 0, zoom: -speed }} className="new-ptz-zoom-button" onStart={start} onStop={() => { void stop(); }}><ZoomOut /></HoldButton>
  </div>
  <label className="new-ptz-speed">이동 속도 <output>{Math.round(speed * 100)}%</output><input type="range" min="0.2" max="1" step="0.1" value={speed} onChange={handleSpeed} /></label>
  <button type="button" className="new-ptz-emergency" onClick={() => void stop()}>■ 즉시 정지</button>
  <div className="new-ptz-card new-ptz-home">{homeActionButtons}</div>
  <div className="new-ptz-card">{presetControls}</div>
  <div className="new-ptz-card new-ptz-features">{disabledFeatureButtons}</div>
</section>
```

Each hold button uses pointer capture, `onPointerDown`, `onPointerUp`, `onPointerCancel`, `onLostPointerCapture`, and keyboard Space/Enter keydown/keyup. Prevent the native keyboard click while a hold is active. `handleBack` awaits Stop before calling `onBack`.

Home set and preset delete use `window.confirm`. Preset names use a controlled input with `maxLength={64}`. The three device-feature buttons are disabled and display mapped Korean reasons: `오디오 경로 준비 필요`, `표준 제어 미확인`, and `프로토콜 미확인`.

- [ ] **Step 3: Integrate capability state and one lazy refresh into LiveWorkspace**

Add:

```ts
const [ptzPanelOpen, setPtzPanelOpen] = useState(false);
const refreshAttemptedRef = useRef(new Set<string>());
const selectedControls = selectedCamera?.controlCapabilities;
const ptzEnabled = Boolean(
  selectedCamera?.state === "streaming" &&
  selectedControls?.ptz.support === "supported" &&
  selectedControls.ptz.available,
);
```

Use `discoveredAt` to distinguish a full controller discovery from a scan-provided partial summary, then run the exact one-attempt refresh effect:

```ts
const refreshCameraControls = useRefreshCameraControls();
useEffect(() => {
  const streamName = selectedCamera?.streamName;
  if (!streamName || selectedCamera.controlCapabilities.discoveredAt) return;
  if (refreshAttemptedRef.current.has(streamName)) return;
  refreshAttemptedRef.current.add(streamName);
  refreshCameraControls.mutate({ streamName });
}, [refreshCameraControls.mutate, selectedCamera]);
```

Do not retry automatically after failure. Derive `ptzDisabledReason` in this priority order: no camera → `카메라를 선택하세요.`, non-streaming → `카메라가 온라인 상태가 아닙니다.`, unknown → `PTZ 지원 여부를 확인하지 못했습니다.`, unsupported → `이 카메라는 PTZ를 지원하지 않습니다.`, otherwise → `PTZ 제어를 사용할 수 없습니다.`.

Insert `PTZ 제어` before the right-panel visibility button. Keep it visible and disabled with `title={ptzDisabledReason}` when unavailable. Clicking it unhides the side panel and opens PTZ.

Register and close through stable callbacks:

```ts
const ptzStopRef = useRef<() => Promise<void>>(async () => undefined);
const registerPtzStop = useCallback((stop: () => Promise<void>) => { ptzStopRef.current = stop; }, []);
const closePtzPanel = useCallback(async () => {
  await ptzStopRef.current();
  setPtzPanelOpen(false);
}, []);
const previousSelectedStreamRef = useRef(selectedStream);

useEffect(() => {
  if (previousSelectedStreamRef.current !== selectedStream) {
    previousSelectedStreamRef.current = selectedStream;
    if (ptzPanelOpen) void closePtzPanel();
  }
}, [closePtzPanel, ptzPanelOpen, selectedStream]);

useEffect(() => {
  if (ptzPanelOpen && (!ptzEnabled || sideHidden)) void closePtzPanel();
}, [closePtzPanel, ptzEnabled, ptzPanelOpen, sideHidden]);

const toggleSidePanel = useCallback(async () => {
  if (!sideHidden && ptzPanelOpen) await closePtzPanel();
  setSideHidden((value) => !value);
}, [closePtzPanel, ptzPanelOpen, sideHidden]);

const hideSidePanel = useCallback(async () => {
  if (ptzPanelOpen) await closePtzPanel();
  setSideHidden(true);
}, [closePtzPanel, ptzPanelOpen]);

useEffect(() => () => { void ptzStopRef.current(); }, []);
```

Insert this toolbar button immediately before the existing right-panel button, then bind the existing header panel button to `toggleSidePanel` and the aside chevron to `hideSidePanel`:

```tsx
<>
  <button
    className={ptzEnabled ? "new-primary" : "new-ghost"}
    type="button"
    disabled={!ptzEnabled}
    title={ptzEnabled ? "선택 카메라 PTZ 제어" : ptzDisabledReason}
    aria-describedby={ptzEnabled ? undefined : "ptz-disabled-reason"}
    onClick={() => { setSideHidden(false); setPtzPanelOpen(true); }}
  >
    PTZ 제어
  </button>
  {!ptzEnabled && <span id="ptz-disabled-reason" className="new-sr-only">{ptzDisabledReason}</span>}
</>
```

Replace the current `<aside>` contents with `<PtzControlPanel camera={selectedCamera} onBack={() => setPtzPanelOpen(false)} onStopReady={registerPtzStop} />` only while `ptzPanelOpen && ptzEnabled && selectedCamera`; otherwise render the existing saved-layout and camera-status cards unchanged. `PtzControlPanel.handleBack` already awaits Stop, so this callback only changes state and does not send a duplicate Stop.

- [ ] **Step 4: Add dense panel styles without changing video transforms**

Append focused styles to `web/src/styles/index.css`:

```css
.new-ptz-panel { display:grid; gap:12px; align-content:start; min-height:100%; color:var(--new-fg); }
.new-ptz-header { position:sticky; top:0; z-index:3; display:grid; grid-template-columns:auto 1fr auto; align-items:center; gap:10px; padding:4px 0 10px; background:#071017; border-bottom:1px solid var(--new-border); }
.new-ptz-header strong,.new-ptz-header em { display:block; }
.new-ptz-header em { margin-top:3px; color:var(--new-muted); font:10px/1.2 var(--new-font-mono); font-style:normal; }
.new-ptz-pad { position:relative; width:152px; height:152px; margin:2px auto; border:1px solid color-mix(in oklch,var(--new-accent),var(--new-border) 58%); border-radius:50%; background:radial-gradient(circle,#163744 0 23%,#0b222d 24% 49%,#071720 50% 70%,#041016 71%); }
.new-ptz-direction,.new-ptz-stop-center { position:absolute; display:grid; place-items:center; border:0; color:var(--new-accent); background:transparent; }
.new-ptz-direction[data-direction="up"] { top:10px; left:57px; }
.new-ptz-direction[data-direction="down"] { bottom:10px; left:57px; }
.new-ptz-direction[data-direction="left"] { top:57px; left:10px; }
.new-ptz-direction[data-direction="right"] { top:57px; right:10px; }
.new-ptz-stop-center { inset:57px; border:1px solid #477383; border-radius:50%; background:#183b49; }
.new-ptz-zoom,.new-ptz-home { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:8px; }
.new-ptz-features { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:8px; }
.new-ptz-speed { display:grid; grid-template-columns:1fr auto; gap:8px; color:var(--new-muted); font-size:11px; }
.new-ptz-speed input { grid-column:1/-1; width:100%; accent-color:var(--new-accent); }
.new-ptz-emergency { min-height:38px; border-color:color-mix(in oklch,var(--new-danger),black 25%); background:color-mix(in oklch,var(--new-danger),black 72%); color:#ffb4b2; }
.new-ptz-error { margin:0; padding:8px; border:1px solid color-mix(in oklch,var(--new-danger),black 35%); border-radius:6px; color:#ffb4b2; font-size:10px; }
.new-ptz-card { display:grid; gap:8px; padding:10px; border:1px solid var(--new-border); border-radius:8px; background:var(--new-surface); }
.new-ptz-card h3 { margin:0; color:var(--new-fg); font-size:11px; }
.new-ptz-home h3 { grid-column:1/-1; }
.new-ptz-card-title { display:flex; align-items:center; justify-content:space-between; color:var(--new-muted); font:10px/1.2 var(--new-font-mono); }
.new-ptz-card form { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:6px; }
.new-ptz-card input,.new-ptz-card button { min-width:0; }
.new-ptz-preset-list { display:grid; gap:6px; margin:0; padding:0; list-style:none; }
.new-ptz-preset-list li { display:grid; grid-template-columns:minmax(0,1fr) auto auto; gap:6px; align-items:center; }
.new-ptz-preset-list span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.new-ptz-feature[disabled] { opacity:.48; cursor:not-allowed; }
.new-ptz-feature small { display:block; color:var(--new-muted); font-size:9px; }
.new-sr-only { position:absolute; width:1px; height:1px; padding:0; margin:-1px; overflow:hidden; clip:rect(0,0,0,0); white-space:nowrap; border:0; }
```

Do not change `.new-live-video`, its transform, wheel zoom, pan, native controls, or focus view behavior.

- [ ] **Step 5: Run a narrow frontend lint only**

Run:

```bash
cd web && npx oxlint src/components/live/usePtzHold.ts src/components/live/PtzControlPanel.tsx src/components/live/LiveWorkspace.tsx
```

Expected: exit 0 with no diagnostics. Do not run the build yet.

- [ ] **Step 6: Commit Task 6 source only**

```bash
git add web/src/components/live/usePtzHold.ts web/src/components/live/PtzControlPanel.tsx web/src/components/live/LiveWorkspace.tsx web/src/styles/index.css
git commit -m "feat: add live PTZ control panel"
```

### Task 7: Run one integration pass and one bounded real-camera session

**Files:**
- Modify after successful verification: `docs/07-implementation-status.md`
- Generated but do not stage due pre-existing ownership: `cmd/camstationd/web/index.html`, `cmd/camstationd/web/assets/*`

**Interfaces:**
- Consumes: Tasks 1-6 complete implementation.
- Produces: one final software verification record, one real-device acceptance session, one UI screenshot, and updated implementation status.

- [ ] **Step 1: Run the complete software verification once**

Run exactly once after all source tasks are assembled:

```bash
go test ./...
cd web
npm run lint
npm run build
cd ..
go build -o camstationd ./cmd/camstationd
```

Expected: every command exits 0. If any command fails:

1. Record all observed failures from that integration run.
2. For each failure, write or select the smallest regression command that reproduces it (`go test` package/test name, `oxlint` file list, frontend build, or Go build as appropriate).
3. Fix that failure and rerun only its reproducing command until it passes; do not run the complete matrix between individual fixes.
4. When every known failure passes its narrow command, rerun the complete four-command matrix above once.
5. If the new integration run exposes another failure, repeat steps 1-4. Do not proceed to the daemon or real camera until one complete matrix exits 0.

- [ ] **Step 2: Restart through the managed lifecycle and verify the daemon once**

Run:

```bash
scripts/camstationctl.sh restart
scripts/camstationctl.sh verify
```

Expected: `camstationd` and managed go2rtc are running, local health checks pass, and no legacy `cctv` service is touched.

- [ ] **Step 3: Perform the one read-and-actuate real-camera session**

Use the guarded CamStation API, never direct ad hoc camera URLs:

1. Select `염소장`; allow its one capability refresh; verify PTZ/home/presets are available and listen/talk/siren remain disabled with reasons.
2. Send Stop once and confirm `panTilt=IDLE`, `zoom=IDLE`.
3. At 20% speed, hold left for one second, release/Stop, then hold right for one second, release/Stop. Do not repeat this sequence.
4. Create one temporary preset named `CamStation QA`, list it once, then delete it once.
5. Do not set home. Invoke `홈으로 이동` once only if the operator confirms the existing home destination is safe; otherwise record it as intentionally not actuated.
6. Switch to a non-PTZ camera and confirm the panel closes and the toolbar button disables.
7. Confirm mouse wheel video zoom, drag pan, focus view, layout save, and timeline behavior still work during the same browser session.

Expected: the camera stops on every release, the final status is IDLE, no temporary preset remains, and no camera credential appears in API responses, events, or logs.

If this session exposes a defect, stop the acceptance sequence at the failed action. Reproduce it with the smallest synthetic/unit/route/UI check, fix it, and rerun only that check. Then repeat only the failed real-device action once. After all discovered defects are resolved, rerun the complete software matrix once before continuing the remaining real-device acceptance steps; do not replay already-passed movement or preset actions unless the fix directly changed that behavior.

- [ ] **Step 4: Capture one UI screenshot and one bounded evidence excerpt**

Capture the final `/live` screen with `염소장` selected and the PTZ panel open. Save operational evidence outside tracked source (for example under the existing ignored runtime diagnostics location); do not add camera URLs, credentials, raw SOAP, DB files, or logs to Git.

- [ ] **Step 5: Update implementation status with only verified facts**

Add a concise `Live PTZ control` entry to `docs/07-implementation-status.md` stating:

```markdown
- Live PTZ control for capability-advertised cameras:
  - guarded ONVIF continuous pan/tilt/zoom and explicit Stop
  - Stop is the final ordered command with a 2-second device timeout backstop
  - home navigation and confirmation-gated home-setting action
  - camera-owned preset list/create/goto/delete
  - `/live` toolbar capability gating and full right-panel replacement
  - listen/talk/siren controls remain disabled until their transport or protocol is implemented
  - final verification used one bounded real-camera movement and temporary-preset session
```

Do not mark browser audio, talk-back, or siren as implemented.

- [ ] **Step 6: Verify commit scope and commit the status update**

Run:

```bash
git diff --check -- docs/07-implementation-status.md
git status --short
```

Confirm user-owned dirty files and generated embedded web output are not staged. Then:

```bash
git add docs/07-implementation-status.md
git commit -m "docs: record live PTZ verification"
```

Do not stage `cmd/camstationd/web/**` in this feature branch because those paths contained pre-existing user-owned build changes before PTZ work began.
