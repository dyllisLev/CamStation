# Camera Preset Name Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve operator-entered PTZ preset names in SQLite even when VStarcam returns generated names such as `PRESET_0`.

**Architecture:** Add a camera/token keyed name table and four focused store operations. Keep the public DTO and React code unchanged; Go routes persist after `SetPreset`, overlay after `GetPresets`, reconcile missing tokens, and remove the name after successful device deletion.

**Tech Stack:** Go 1.25, `database/sql`, modernc SQLite, ONVIF PTZ, existing React/TanStack Query UI.

## Global Constraints

- SQLite is the source of truth for operator-entered preset names.
- The opaque camera token remains the identity for goto and delete.
- Keep the existing `CameraPreset {token, name}` DTO and frontend component unchanged.
- Keep existing trim, UTF-8, and 64-rune validation.
- Never store or expose camera URLs, credentials, or ONVIF targets in the new table, API, events, tests, or reports.
- Do not add rename UI, offline management, browser storage, dependencies, or vendor-specific commands.
- Manage the daemon only with `scripts/camstationctl.sh`.
- Preserve unrelated dirty working-tree files.

---

### Task 1: Add persistent preset-name storage

**Files:**
- Modify: `internal/store/schema.go`
- Create: `internal/store/camera_preset_names.go`
- Create: `internal/store/camera_preset_names_test.go`

**Interfaces:**
- Consumes: `store.DB`, `cameras.id`, SQLite foreign keys.
- Produces:
  - `UpsertCameraPresetName(ctx context.Context, cameraID int64, token, name string) error`
  - `ListCameraPresetNames(ctx context.Context, cameraID int64) (map[string]string, error)`
  - `DeleteCameraPresetName(ctx context.Context, cameraID int64, token string) error`
  - `ReconcileCameraPresetNames(ctx context.Context, cameraID int64, activeTokens []string) error`

- [ ] **Step 1: Write the failing store test**

Create `internal/store/camera_preset_names_test.go`:

```go
package store

import "testing"

func TestCameraPresetNamesPersistReconcileAndCascade(t *testing.T) {
	db := openMigratedStore(t)
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("idempotent migrate: %v", err)
	}
	camera, err := db.UpsertCamera(t.Context(), Camera{
		Name: "Preset Camera", StreamName: "preset-camera", URL: "rtsp://camera.invalid/live", State: "streaming",
	})
	if err != nil {
		t.Fatalf("upsert camera: %v", err)
	}
	for token, name := range map[string]string{"PRESET_0": "입구", "PRESET_1": "창고"} {
		if err := db.UpsertCameraPresetName(t.Context(), camera.ID, token, name); err != nil {
			t.Fatalf("upsert %s: %v", token, err)
		}
	}
	if err := db.UpsertCameraPresetName(t.Context(), camera.ID, "PRESET_0", "정문"); err != nil {
		t.Fatalf("update name: %v", err)
	}
	names, err := db.ListCameraPresetNames(t.Context(), camera.ID)
	if err != nil || names["PRESET_0"] != "정문" || names["PRESET_1"] != "창고" {
		t.Fatalf("names/error = %#v/%v", names, err)
	}
	if err := db.ReconcileCameraPresetNames(t.Context(), camera.ID, []string{"PRESET_0"}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	names, _ = db.ListCameraPresetNames(t.Context(), camera.ID)
	if len(names) != 1 || names["PRESET_0"] != "정문" {
		t.Fatalf("reconciled names = %#v", names)
	}
	if err := db.DeleteCameraPresetName(t.Context(), camera.ID, "PRESET_0"); err != nil {
		t.Fatalf("delete name: %v", err)
	}
	if err := db.UpsertCameraPresetName(t.Context(), camera.ID, "PRESET_2", "후문"); err != nil {
		t.Fatalf("upsert cascade row: %v", err)
	}
	if _, err := db.DeleteCamera(t.Context(), camera.StreamName); err != nil {
		t.Fatalf("delete camera: %v", err)
	}
	names, err = db.ListCameraPresetNames(t.Context(), camera.ID)
	if err != nil || len(names) != 0 {
		t.Fatalf("cascade names/error = %#v/%v", names, err)
	}
}
```

- [ ] **Step 2: Run RED**

Run:

```bash
go test ./internal/store -run TestCameraPresetNamesPersistReconcileAndCascade -count=1
```

Expected: build failure because the four store methods do not exist.

- [ ] **Step 3: Add the idempotent schema**

Add after `camera_streams` in `internal/store/schema.go`:

```go
`CREATE TABLE IF NOT EXISTS camera_preset_names (
	camera_id INTEGER NOT NULL,
	preset_token TEXT NOT NULL,
	name TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY (camera_id, preset_token),
	FOREIGN KEY (camera_id) REFERENCES cameras(id) ON DELETE CASCADE
)`,
```

- [ ] **Step 4: Implement the store methods**

Create `internal/store/camera_preset_names.go` with parameterized SQL. Use this upsert exactly:

```go
func (d *DB) UpsertCameraPresetName(ctx context.Context, cameraID int64, token, name string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.db.ExecContext(ctx, `INSERT INTO camera_preset_names(camera_id,preset_token,name,created_at,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(camera_id,preset_token)
		DO UPDATE SET name=excluded.name,updated_at=excluded.updated_at`, cameraID, token, name, now, now)
	return err
}
```

`ListCameraPresetNames` must select `preset_token,name` into a non-nil map. `DeleteCameraPresetName` must issue an idempotent parameterized delete. `ReconcileCameraPresetNames` must:

1. Build a set from `activeTokens`.
2. Begin a transaction.
3. Read existing tokens and close rows before issuing deletes, because the DB has one connection.
4. Delete only tokens absent from the active set.
5. Commit, with deferred rollback on every earlier return.

- [ ] **Step 5: Run GREEN and store regression tests**

```bash
go test ./internal/store -run TestCameraPresetNamesPersistReconcileAndCascade -count=1
go test ./internal/store -count=1
```

Expected: both PASS.

- [ ] **Step 6: Commit Task 1**

```bash
git add internal/store/schema.go internal/store/camera_preset_names.go internal/store/camera_preset_names_test.go
git commit -m "persist camera preset names"
```

### Task 2: Connect preset routes to SQLite

**Files:**
- Modify: `cmd/camstationd/routes_camera_controls.go`
- Modify: `cmd/camstationd/routes_camera_controls_test.go`

**Interfaces:**
- Consumes: Task 1 store methods and existing `cameraControlService` methods.
- Produces: unchanged preset HTTP APIs returning DB-backed display names.

- [ ] **Step 1: Add failing route tests**

Add three tests to `routes_camera_controls_test.go`:

```go
func TestCameraControlRoutesPersistOverlayAndReconcilePresetNames(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	headers := trustedConsoleHeaders()
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets", `{"name":"입구"}`, headers)
	if status != http.StatusOK || payload["name"] != "입구" {
		t.Fatalf("create = %d/%v", status, payload)
	}
	fake.presets = []cameracontrol.Preset{{Token: "created-token", Name: "PRESET_0"}, {Token: "camera-token", Name: "카메라 이름"}}
	status, list := requestJSONArrayWithHeaders(t, server.handler, http.MethodGet, "/api/cameras/goat-yard/ptz/presets", "", headers)
	encoded, _ := json.Marshal(list)
	if status != http.StatusOK || !strings.Contains(string(encoded), `"name":"입구"`) || !strings.Contains(string(encoded), `"name":"카메라 이름"`) {
		t.Fatalf("list = %d/%s", status, encoded)
	}
	fake.presets = []cameracontrol.Preset{{Token: "camera-token", Name: "카메라 이름"}}
	status, _ = requestJSONArrayWithHeaders(t, server.handler, http.MethodGet, "/api/cameras/goat-yard/ptz/presets", "", headers)
	camera, _ := server.db.GetCameraByStream(t.Context(), "goat-yard")
	names, err := server.db.ListCameraPresetNames(t.Context(), camera.ID)
	if status != http.StatusOK || err != nil || len(names) != 0 {
		t.Fatalf("reconcile = %d/%#v/%v", status, names, err)
	}
}

func TestCameraControlRoutesDeletePresetNameOnlyAfterDeviceSuccess(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	camera, _ := server.db.GetCameraByStream(t.Context(), "goat-yard")
	_ = server.db.UpsertCameraPresetName(t.Context(), camera.ID, "saved-token", "입구")
	fake.err = cameracontrol.ErrUnavailable
	status, _ := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets/delete", `{"token":"saved-token"}`, trustedConsoleHeaders())
	names, _ := server.db.ListCameraPresetNames(t.Context(), camera.ID)
	if status != http.StatusBadGateway || names["saved-token"] != "입구" {
		t.Fatalf("failed delete = %d/%#v", status, names)
	}
	fake.err = nil
	status, _ = requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets/delete", `{"token":"saved-token"}`, trustedConsoleHeaders())
	names, _ = server.db.ListCameraPresetNames(t.Context(), camera.ID)
	if status != http.StatusOK || len(names) != 0 {
		t.Fatalf("successful delete = %d/%#v", status, names)
	}
}

func TestCameraControlRoutesCompensateWhenPresetNamePersistenceFails(t *testing.T) {
	fake := &fakeCameraController{}
	server := newCameraControlRouteServer(t, fake)
	fake.onCreatePreset = func() { _, _ = server.db.DeleteCamera(t.Context(), "goat-yard") }
	status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPost, "/api/cameras/goat-yard/ptz/presets", `{"name":"입구"}`, trustedConsoleHeaders())
	encoded, _ := json.Marshal(payload)
	if status != http.StatusBadGateway || fake.deletePresetToken != "created-token" {
		t.Fatalf("compensation = %d/%q/%s", status, fake.deletePresetToken, encoded)
	}
	for _, secret := range []string{"rtsp://", "camera.invalid", "FOREIGN KEY"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("leaked %q: %s", secret, encoded)
		}
	}
}
```

Add `onCreatePreset func()` to `fakeCameraController`, and invoke it inside its `CreatePreset` before returning `created-token`.

- [ ] **Step 2: Run RED**

```bash
go test ./cmd/camstationd -run 'TestCameraControlRoutes(PersistOverlayAndReconcilePresetNames|DeletePresetNameOnlyAfterDeviceSuccess|CompensateWhenPresetNamePersistenceFails)' -count=1
```

Expected: list returns `PRESET_0`, successful delete leaves the DB name, and failed persistence does not call device cleanup.

- [ ] **Step 3: Persist and overlay in create/list routes**

After `ListPresets` succeeds, load names, replace only matching tokens, then reconcile the complete token list:

```go
names, err := d.db.ListCameraPresetNames(ctx, camera.ID)
if err != nil { d.recordCameraControlFailure(r.Context(), camera.StreamName, "preset_list", err); writeCameraControlError(w, err); return }
tokens := make([]string, 0, len(presets))
for i := range presets {
	tokens = append(tokens, presets[i].Token)
	if name, ok := names[presets[i].Token]; ok { presets[i].Name = name }
}
if err := d.db.ReconcileCameraPresetNames(ctx, camera.ID, tokens); err != nil {
	d.recordCameraControlFailure(r.Context(), camera.StreamName, "preset_list", err)
	writeCameraControlError(w, err)
	return
}
```

After `CreatePreset`, persist `req.Name`. On failure, best-effort remove the new device preset using `context.WithoutCancel` and return the existing sanitized control error:

```go
if err := d.db.UpsertCameraPresetName(ctx, camera.ID, preset.Token, req.Name); err != nil {
	cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(r.Context()), cameraControlRouteTimeout)
	defer cancelCleanup()
	_ = d.cameraController.DeletePreset(cleanupCtx, camera, preset.Token)
	d.recordCameraControlFailure(r.Context(), camera.StreamName, "preset_create", err)
	writeCameraControlError(w, err)
	return
}
preset.Name = req.Name
```

- [ ] **Step 4: Delete the DB name only after device success**

After successful `DeletePreset` and before the success event:

```go
if delete {
	if err := d.db.DeleteCameraPresetName(ctx, camera.ID, req.Token); err != nil {
		d.recordCameraControlFailure(r.Context(), camera.StreamName, operation, err)
		writeCameraControlError(w, err)
		return
	}
	d.recordCameraControlSuccess(r.Context(), camera.StreamName, operation)
}
```

- [ ] **Step 5: Run GREEN and all Go tests**

```bash
go test ./cmd/camstationd -run 'TestCameraControlRoutes(PersistOverlayAndReconcilePresetNames|DeletePresetNameOnlyAfterDeviceSuccess|CompensateWhenPresetNamePersistenceFails)' -count=1
go test ./cmd/camstationd -count=1
go test ./... -count=1
```

Expected: all PASS.

- [ ] **Step 6: Commit Task 2**

```bash
git add cmd/camstationd/routes_camera_controls.go cmd/camstationd/routes_camera_controls_test.go
git commit -m "preserve operator preset names"
```

### Task 3: Deploy and verify the real VStarcam round trip

**Files:**
- Runtime only: `camstationd`, `data/camstation.db`
- No source modifications.

**Interfaces:**
- Consumes: 소방서5 preset API and the migrated SQLite store.
- Produces: verified create, refresh, restart, goto, and delete with no test preset left behind.

- [ ] **Step 1: Build and controlled restart**

```bash
go build -o camstationd ./cmd/camstationd
scripts/camstationctl.sh restart
```

Expected: exit 0 and health `ok=true`.

- [ ] **Step 2: Create a temporary current-position preset**

POST to `/api/cameras/fire-station-5/ptz/presets` with management header and name `코덱스 검증 2026-07-12`. Capture only the returned token; do not print camera URLs, credentials, DB contents, or ONVIF payloads.

Expected: HTTP 200 and exact returned name.

- [ ] **Step 3: Verify query refresh and daemon restart persistence**

List through the guarded API and assert the token maps to `코덱스 검증 2026-07-12`. Restart only with `scripts/camstationctl.sh restart`, list again, and assert the same pair remains.

Expected: both checks PASS even when the camera reports `PRESET_n`.

- [ ] **Step 4: Verify token actions and remove the temporary preset**

Goto the newly saved current-position token, then delete it. Because it was saved at the current position, goto must not intentionally move to another scene. List again and assert the token is absent.

Expected: goto/delete HTTP 200 and no temporary preset remains.

- [ ] **Step 5: Final checks**

```bash
scripts/camstationctl.sh verify
git diff --check
```

Expected: healthy daemon/go2rtc and no unrelated staged or committed files.
