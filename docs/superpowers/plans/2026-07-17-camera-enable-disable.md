# Camera Enable/Disable Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist camera activation and let operators enable or disable cameras from `/cameras` without disabled cameras connecting, playing, recording, probing, or receiving control commands.

**Architecture:** Add an additive SQLite `enabled` column and a narrow store setter. Reuse the existing serialized policy apply transaction, filtering disabled cameras at the shared stream-rendering and recorder boundaries, and expose one guarded PATCH endpoint. Keep the common camera query for administration while filtering disabled cameras out of live/operational frontend surfaces.

**Tech Stack:** Go 1.x, `net/http`, SQLite, go2rtc policy renderer, React 19, TypeScript, TanStack Query, Vite.

## Global Constraints

- Disabled cameras remain in camera settings but do not create camera network, go2rtc, playback, recorder, preview, probe, PTZ, or retry activity.
- Existing installations migrate with `enabled = 1`; this server is seeded before the new daemon starts.
- This server starts with only `집-마당`, `집-창고1`, and `집-창고2` enabled; `염소장` and every `소방서` camera are disabled.
- Recording remains disabled during deployment verification.
- Do not expose camera URLs, credentials, private aliases, or generated commands.
- Review only each task's changed files; perform the whole-feature review only after all tasks are implemented.
- Do not add test-only production hooks or a new frontend test framework.

---

### Task 1: Persist Camera Activation

**Files:**
- Modify: `internal/store/models.go`
- Modify: `internal/store/schema.go`
- Modify: `internal/store/schema_camera_profiles.go`
- Modify: `internal/store/cameras.go`
- Modify: `internal/store/camera_rows.go`
- Test: `internal/store/camera_policies_test.go`

**Interfaces:**
- Produces: `Camera.Enabled bool`
- Produces: `(*DB).SetCameraEnabled(context.Context, string, bool) error`
- Preserves: `UpsertCamera` inserts default-enabled rows and does not overwrite activation during ordinary camera edits.

- [ ] **Step 1: Write failing persistence tests**

Add focused tests proving new and legacy cameras default to enabled, `SetCameraEnabled` survives a reread, and a normal `UpsertCamera` edit preserves a disabled value:

```go
func TestCameraActivationPersistsAndOrdinaryUpsertPreservesIt(t *testing.T) {
    db := openMigratedStore(t)
    camera, err := db.UpsertCamera(t.Context(), Camera{Name: "yard", StreamName: "yard", URL: "rtsp://camera/main"})
    if err != nil || !camera.Enabled { t.Fatalf("new camera enabled=%v err=%v", camera.Enabled, err) }
    if err := db.SetCameraEnabled(t.Context(), camera.StreamName, false); err != nil { t.Fatal(err) }
    camera.Name = "yard renamed"
    if _, err := db.UpsertCamera(t.Context(), camera); err != nil { t.Fatal(err) }
    got, err := db.GetCameraByStream(t.Context(), camera.StreamName)
    if err != nil || got.Enabled { t.Fatalf("disabled camera=%#v err=%v", got, err) }
}
```

- [ ] **Step 2: Run the focused test and confirm RED**

Run: `go test ./internal/store -run 'TestCameraActivation|TestCameraPolicyLegacyMigration'`

Expected: compile failure because `Camera.Enabled` and `SetCameraEnabled` do not exist.

- [ ] **Step 3: Add the minimal schema, scan, and setter implementation**

Add `enabled INTEGER NOT NULL DEFAULT 1` to fresh schema and additive migration. Select and scan it as an integer-backed bool. Keep the column out of `UpsertCamera` insert/update lists so SQLite supplies the default for new rows and edits preserve the existing value. Implement the setter as one checked update:

```go
func (d *DB) SetCameraEnabled(ctx context.Context, streamName string, enabled bool) error {
    result, err := d.db.ExecContext(ctx,
        `UPDATE cameras SET enabled=?, updated_at=? WHERE stream_name=?`,
        enabled, time.Now().UTC().Format(time.RFC3339Nano), streamName)
    if err != nil { return err }
    affected, err := result.RowsAffected()
    if err != nil { return err }
    if affected != 1 { return sql.ErrNoRows }
    return nil
}
```

- [ ] **Step 4: Run focused store tests and confirm GREEN**

Run: `go test ./internal/store -run 'TestCameraActivation|TestCameraPolicyLegacyMigration'`

Expected: PASS.

- [ ] **Step 5: Review only Task 1 and commit**

Run: `git diff --check -- internal/store` and inspect only Task 1's diff.

Commit: `feat(store): persist camera activation`

---

### Task 2: Enforce Activation in Runtime and HTTP APIs

**Files:**
- Modify: `internal/stream/policy.go`
- Modify: `internal/stream/apply.go`
- Modify: `internal/stream/go2rtc.go`
- Modify: `internal/recorder/recorder.go`
- Modify: `cmd/camstationd/camera_policy_startup.go`
- Create: `cmd/camstationd/routes_camera_activation.go`
- Modify: `cmd/camstationd/routes_camera_mutations.go`
- Modify: `cmd/camstationd/routes_camera_profiles.go`
- Modify: `cmd/camstationd/routes_camera_stream_outputs.go`
- Modify: `cmd/camstationd/routes_camera_controls.go`
- Modify: `cmd/camstationd/routes_streams.go`
- Modify: `cmd/camstationd/routes_core.go`
- Modify: `cmd/camstationd/routes_public_dtos.go`
- Modify: `cmd/camstationd/spa_proxy.go`
- Modify: `cmd/camstationd/camera_profile_helpers.go`
- Test: `internal/stream/policy_test.go`
- Test: `internal/stream/apply_test.go`
- Test: `internal/recorder/policy_test.go`
- Create: `cmd/camstationd/routes_camera_activation_test.go`

**Interfaces:**
- Consumes: `Camera.Enabled`, `DB.SetCameraEnabled`
- Produces: `PATCH /api/cameras/{streamName}/enabled` with `{ "enabled": boolean }`
- Produces: public camera DTO `enabled: boolean`
- Reuses: `policyApplier.Apply` and its go2rtc last-good rollback.

- [ ] **Step 1: Write failing runtime exclusion tests**

Add one renderer test and one apply-coordinator test. The renderer test passes one enabled and one disabled camera and asserts the disabled public name, private alias, and source URL are absent. The coordinator test starts with an active recorder for a now-disabled camera and asserts the empty config commits while that recorder is not restored.

```go
func TestRenderPolicyConfigOmitsDisabledCamera(t *testing.T) {
    enabled, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
    enabled.Enabled, enabled.Outputs = true, []store.CameraOutput{output}
    disabled := enabled
    disabled.ID, disabled.Enabled, disabled.StreamName = 2, false, "disabled"
    disabled.Streams[0].ID, disabled.Streams[0].URL = 20, "rtsp://secret@192.0.2.2/main"
    disabled.Outputs[0].StreamName = "disabled-focus"
    config, _, err := renderPolicyConfig([]store.Camera{enabled, disabled}, false)
    if err != nil { t.Fatal(err) }
    if strings.Contains(string(config), "disabled-focus") || strings.Contains(string(config), "192.0.2.2") {
        t.Fatalf("disabled camera rendered: %s", config)
    }
}
```

- [ ] **Step 2: Run runtime tests and confirm RED**

Run: `go test ./internal/stream ./internal/recorder -run 'Disabled|Activation'`

Expected: disabled camera data is still rendered or recorder start is accepted.

- [ ] **Step 3: Filter at shared runtime boundaries**

Add an unexported `enabledCameras` slice filter in `internal/stream`, use it before startup/apply rendering and policy snapshots, and exclude disabled cameras from failure marking and recorder restoration. In recorder manager, skip disabled rows during reconciliation and reject direct `Start` calls for disabled cameras. Update existing pure unit fixtures to state `Enabled: true` explicitly.

- [ ] **Step 4: Run runtime tests and confirm GREEN**

Run: `go test ./internal/stream ./internal/recorder -run 'Policy|Apply|Disabled|Activation'`

Expected: PASS.

- [ ] **Step 5: Write failing endpoint and connection-guard tests**

Create route tests proving:

```go
status, payload := requestJSONWithHeaders(t, server.handler, http.MethodPatch,
    "/api/cameras/policy-camera/enabled", `{"enabled":false}`, trustedConsoleHeaders())
if status != http.StatusOK || payload["applied"] != true { t.Fatalf("response=%d %#v", status, payload) }
stored, _ := server.db.GetCameraByStream(t.Context(), "policy-camera")
if stored.Enabled { t.Fatal("camera remained enabled") }
```

Also prove an apply failure restores `Enabled == true`, and disabled stream probe, registered preview/scan, recorder start, and control lookup return conflict before invoking their network collaborators.

- [ ] **Step 6: Run route tests and confirm RED**

Run: `go test ./cmd/camstationd -run 'CameraActivation|DisabledCamera'`

Expected: 404 for the missing PATCH route and disabled operations are not blocked.

- [ ] **Step 7: Implement the guarded PATCH route and shared guards**

Decode only `enabled`, set the value, call the existing serialized apply coordinator, and on failure restore the prior value then reapply the previous state. On success append a redacted event and reconcile enabled applied cameras only when global recording is enabled. Return `409` with `비활성 카메라입니다.` from connection-producing routes before camera access.

Expose `Enabled` in `publicCamera`, require it in `isRegisteredPublicStream`, skip disabled cameras in bulk probes, and let ordinary edits of disabled cameras persist without automatic scan/probe calls.

- [ ] **Step 8: Run focused backend tests and confirm GREEN**

Run: `go test ./internal/store ./internal/stream ./internal/recorder ./cmd/camstationd -run 'CameraActivation|DisabledCamera|CameraPolicy|PublicCamera|RegisteredPublicStream'`

Expected: PASS.

- [ ] **Step 9: Review only Task 2 and commit**

Run `git diff --check` on Task 2 files, inspect only Task 2's diff, and verify no secret or test-only production hook was added.

Commit: `feat: enforce camera activation`

---

### Task 3: Add the Camera Settings Switch and Hide Disabled Playback Targets

**Files:**
- Modify: `web/src/app/cameraTypes.ts`
- Modify: `web/src/app/cameraApi.ts`
- Modify: `web/src/app/queries.ts`
- Modify: `web/src/pages/CamerasPage.tsx`
- Modify: `web/src/pages/cameras/RegisteredCameraTable.tsx`
- Modify: `web/src/components/live/LiveWorkspace.tsx`
- Modify: `web/src/pages/ControlRoomPage.tsx`
- Modify: `web/src/pages/StreamsPage.tsx`
- Modify: `web/src/layouts/ConsoleLayout.tsx`

**Interfaces:**
- Consumes: camera `enabled` and the PATCH endpoint.
- Produces: a pending-safe `활성화` / `비활성화` control in the registered-camera table.

- [ ] **Step 1: Add the typed API and mutation**

Add `enabled: boolean` to `Camera`, `cameraApi.setCameraEnabled(streamName, enabled)`, and a TanStack mutation that invalidates existing camera/stream/recorder query keys after success.

- [ ] **Step 2: Add the settings-table activation control**

Pass `onSetEnabled` and the pending stream name into `RegisteredCameraTable`. Show a clear `활성`/`비활성` badge and one native button per row, disabling the button while its request is pending. Keep `카메라 수정` available for disabled rows.

- [ ] **Step 3: Filter playback and operational consumers**

Use `camera.enabled` when constructing the camera rows in `LiveWorkspace`, `ControlRoomPage`, and `StreamsPage`; count only enabled cameras in `ConsoleLayout`. Do not filter the `/cameras` administration page.

- [ ] **Step 4: Verify frontend without adding test scaffolding**

Run: `cd web && npm run lint`

Expected: PASS with no warnings.

Run: `cd web && npm run build`

Expected: PASS and refreshed embedded assets under `cmd/camstationd/web`.

- [ ] **Step 5: Review only Task 3 and commit**

Inspect only Task 3's source and generated-asset diff. Confirm the switch is labelled, pending-safe, and does not appear on live surfaces.

Commit: `feat(web): control camera activation`

---

### Task 4: Document, Deploy, and Verify the Requested State

**Files:**
- Modify: `docs/07-implementation-status.md`
- Runtime only, never commit: `data/camstation.db`, `data/go2rtc.yaml`, daemon logs.

**Interfaces:**
- Consumes: completed backend/frontend binary.
- Produces: running service with the requested persistent activation state.

- [ ] **Step 1: Update implementation status**

Document the persistent settings switch, disabled-camera runtime exclusion, and guarded operations. Set `Last updated` to `2026-07-17`.

- [ ] **Step 2: Build the daemon**

Run: `go build -o camstationd.next ./cmd/camstationd`

Expected: exit 0.

- [ ] **Step 3: Stop through the lifecycle script and seed activation before startup**

Run `scripts/camstationctl.sh stop`, back up the database, add the migration column if absent, and update names in one SQLite transaction so only `집-마당`, `집-창고1`, and `집-창고2` are enabled. Replace the daemon binary only after the transaction commits.

- [ ] **Step 4: Start with recording disabled and verify surfaces**

Run: `env PATH=/usr/local/bin:/usr/bin:/bin CAMSTATION_RECORDING_ENABLED=false scripts/camstationctl.sh start`

Verify:

- API and database report the three requested cameras enabled and `염소장` plus all `소방서` cameras disabled;
- go2rtc stream definitions, preloads, and producers contain no disabled-camera public or private names;
- recorder status is disabled with zero workers;
- `/cameras` serves the activation fields and the embedded UI build;
- no `/live` page is opened during verification.

- [ ] **Step 5: Run final whole-feature verification and review**

Run the focused backend packages, frontend lint/build, `go build`, `scripts/camstationctl.sh status`, and `scripts/camstationctl.sh verify`. Review the entire feature diff only now.

- [ ] **Step 6: Commit documentation and remove the temporary binary**

Commit: `docs: record camera activation controls`

Remove `camstationd.next` after successful installation; do not stage runtime data or unrelated untracked files.
