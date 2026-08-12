# Common Browser Live Output Profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make new and existing browser live outputs use warm H.264 streams capped at 1280x720 and 15 fps, with measured runtime rollback when the four-core server exceeds its CPU budget.

**Architecture:** Reuse the existing three-output camera policy model, apply coordinator, revision checks, and last-good go2rtc rollback. Change only the live defaults in the frontend, request fallback, and store fallback; update existing cameras through the current policy API while preserving each camera's source, recording output, and focus output.

**Tech Stack:** Go, SQLite, go2rtc/FFmpeg, React/TypeScript, Node test runner, curl/jq runtime verification.

## Global Constraints

- Live output: forced H.264, maximum 1280x720, maximum 15 fps, no audio, always active.
- Maximum dimensions are caps; never upscale a smaller source.
- Preserve each camera's selected live source and all recording/focus settings.
- Keep go2rtc API and RTSP listeners on localhost and do not expose camera URLs or credentials.
- Roll back current-camera policy changes if apply/runtime verification fails or 60-second average FFmpeg CPU exceeds 280%.
- Do not modify or commit runtime data, DB files, generated go2rtc configuration, logs, or secrets.

---

### Task 1: Frontend Registration Defaults

**Files:**
- Modify: `web/tests/cameraPolicyModel.test.ts`
- Modify: `web/src/pages/cameras/streamOutputPolicyModel.ts`

**Interfaces:**
- Consumes: `recommendedStreamOutputs(hasLiveSource: boolean)`.
- Produces: the existing `StreamOutputSettingsTuple` with the new live defaults.

- [ ] **Step 1: Change the existing test expectation first**

```ts
test("recommended policies use recording copy, warm capped H.264 live, and capped focus", () => {
  assert.deepEqual(recommendedStreamOutputs(true), [
    { purpose: "recording", sourceKey: "recording", videoMode: "copy", maxWidth: null, maxHeight: null, maxFPS: null, audioMode: "source", activation: "on_demand" },
    { purpose: "live", sourceKey: "live", videoMode: "h264", maxWidth: 1280, maxHeight: 720, maxFPS: 15, audioMode: "none", activation: "always" },
    { purpose: "focus", sourceKey: "recording", videoMode: "auto", maxWidth: 1920, maxHeight: 1080, maxFPS: null, audioMode: "none", activation: "on_demand" },
  ]);
  assert.equal(recommendedStreamOutputs(false)[1].sourceKey, "recording");
});
```

- [ ] **Step 2: Run the focused frontend test and verify RED**

Run: `cd web && node --experimental-strip-types --test tests/cameraPolicyModel.test.ts`

Expected: FAIL because the live default is still `auto`, uncapped, and `on_demand`.

- [ ] **Step 3: Implement the minimal frontend default change**

```ts
export function recommendedStreamOutputs(hasLiveSource: boolean): StreamOutputSettingsTuple {
  return [
    output("recording", "recording", "copy", null, null, null, "source", "on_demand"),
    output("live", hasLiveSource ? "live" : "recording", "h264", 1280, 720, 15, "none", "always"),
    output("focus", "recording", "auto", 1920, 1080, null, "none", "on_demand"),
  ];
}
```

- [ ] **Step 4: Run the focused frontend test and verify GREEN**

Run: `cd web && node --experimental-strip-types --test tests/cameraPolicyModel.test.ts`

Expected: all tests pass.

### Task 2: Server and Store Fallback Defaults

**Files:**
- Modify: `internal/store/camera_policies_test.go`
- Modify: `internal/store/schema_camera_policies.go`
- Modify: `cmd/camstationd/routes_camera_profile_selection_helpers_test.go`
- Modify: `cmd/camstationd/camera_profile_helpers.go`

**Interfaces:**
- Consumes: existing `CameraOutput` fields and `requestedCameraOutputs` fallback path.
- Produces: matching server/store live defaults for clients that omit output settings and cameras missing canonical output rows.

- [ ] **Step 1: Change the store default assertion first**

Update `assertDefaultOutputs` so the live output requires:

```go
live.VideoMode == CameraVideoH264
live.MaxWidth != nil && *live.MaxWidth == 1280
live.MaxHeight != nil && *live.MaxHeight == 720
live.MaxFPS != nil && *live.MaxFPS == 15
live.AudioMode == CameraAudioNone
live.Activation == CameraActivationAlways
```

- [ ] **Step 2: Add a request-fallback test first**

```go
func TestRequestedCameraOutputsUseCommonLiveProfile(t *testing.T) {
  inputs := []store.CameraStream{
    {SourceKey: "recording"},
    {SourceKey: "live"},
  }
  outputs := requestedCameraOutputs(nil, inputs)
  live := outputs[1]
  if live.SourceKey != "live" || live.VideoMode != store.CameraVideoH264 ||
    live.MaxWidth == nil || *live.MaxWidth != 1280 ||
    live.MaxHeight == nil || *live.MaxHeight != 720 ||
    live.MaxFPS == nil || *live.MaxFPS != 15 ||
    live.AudioMode != store.CameraAudioNone || live.Activation != store.CameraActivationAlways {
    t.Fatalf("live default = %#v", live)
  }
}
```

- [ ] **Step 3: Run focused Go tests and verify RED**

Run: `go test ./internal/store ./cmd/camstationd -run 'Test.*(DefaultOutputs|RequestedCameraOutputsUseCommonLiveProfile)' -count=1`

Expected: FAIL on the old `auto`/uncapped/`on_demand` live defaults.

- [ ] **Step 4: Update `requestedCameraOutputs` minimally**

```go
maxLiveWidth, maxLiveHeight, maxLiveFPS := 1280, 720, 15.0
// ...
{Purpose: store.CameraOutputLive, SourceKey: live, VideoMode: store.CameraVideoH264,
 MaxWidth: &maxLiveWidth, MaxHeight: &maxLiveHeight, MaxFPS: &maxLiveFPS,
 AudioMode: store.CameraAudioNone, Activation: store.CameraActivationAlways},
```

- [ ] **Step 5: Update the store SQL fallback minimally**

Replace `defaultOutputInsertSQL` with the same existing branches plus `max_fps` and purpose-specific activation:

```go
func defaultOutputInsertSQL(purpose string) string {
	sourceOrder := "CASE WHEN s.source_key = 'recording' THEN 0 ELSE 1 END"
	streamName := "CASE WHEN c.recording_stream_name != '' THEN c.recording_stream_name ELSE c.stream_name || '-recording' END"
	video, audio, maxWidth, maxHeight, maxFPS, activation := "'copy'", "'source'", "NULL", "NULL", "NULL", "'on_demand'"
	if purpose == "live" {
		sourceOrder = "CASE WHEN s.source_key = 'live' THEN 0 WHEN s.source_key = 'recording' THEN 1 ELSE 2 END"
		streamName = "CASE WHEN c.live_stream_name != '' AND c.live_stream_name != c.recording_stream_name THEN c.live_stream_name ELSE c.stream_name || '-live' END"
		video, audio, maxWidth, maxHeight, maxFPS, activation = "'h264'", "'none'", "1280", "720", "15", "'always'"
	}
	if purpose == "focus" {
		streamName, video, audio, maxWidth, maxHeight = "c.stream_name || '-focus'", "'auto'", "'none'", "1920", "1080"
	}
	return fmt.Sprintf(`INSERT OR IGNORE INTO camera_outputs(
		camera_id, purpose, stream_name, source_stream_id, video_mode, max_width, max_height, max_fps,
		audio_mode, activation, created_at, updated_at
	) SELECT c.id, '%s', %s,
		(SELECT s.id FROM camera_streams s WHERE s.camera_id = c.id ORDER BY %s, s.id LIMIT 1),
		%s, %s, %s, %s, %s, %s, c.created_at, c.updated_at
	FROM cameras c WHERE EXISTS (SELECT 1 FROM camera_streams s WHERE s.camera_id = c.id)`,
		purpose, streamName, sourceOrder, video, maxWidth, maxHeight, maxFPS, audio, activation)
}
```

- [ ] **Step 6: Run focused Go tests and verify GREEN**

Run: `go test ./internal/store ./cmd/camstationd -run 'Test.*(DefaultOutputs|RequestedCameraOutputsUseCommonLiveProfile)' -count=1`

Expected: PASS.

- [ ] **Step 7: Run policy rendering regression tests**

Run: `go test ./internal/stream -count=1`

Expected: PASS, confirming existing H.264 caps and always-on preload rendering remain valid.

### Task 3: Documentation and Static Verification

**Files:**
- Modify: `docs/07-implementation-status.md`
- Generated by build: `cmd/camstationd/web/**`

**Interfaces:**
- Consumes: completed default changes.
- Produces: served frontend assets and implementation-status evidence.

- [ ] **Step 1: Record the common profile behavior**

Add this shipped-status entry:

```markdown
- Common browser-live output defaults:
  - forced server H.264 with GOP 20
  - maximum 1280x720 at 15 fps without upscaling smaller live sources
  - audio disabled and output kept warm with always-on activation
```

- [ ] **Step 2: Run all frontend checks and build embedded assets**

Run:

```bash
cd web
node --experimental-strip-types --test tests/*.test.ts
npm run lint
npm run build
```

Expected: tests and lint pass; Vite build exits 0 and updates `cmd/camstationd/web`.

- [ ] **Step 3: Run all Go tests and build the daemon**

Run:

```bash
go test ./...
go build -o camstationd ./cmd/camstationd
```

Expected: both commands exit 0.

### Task 4: Apply Existing Cameras With Automatic Runtime Rollback

**Files:**
- No repository file changes; use the running API and in-memory shell state.

**Interfaces:**
- Consumes: `GET /api/cameras`, `PUT /api/cameras/{streamName}/stream-outputs`, and current desired revisions.
- Produces: updated desired/applied policies for the eight registered cameras or a verified restoration of the captured policies.

- [ ] **Step 1: Capture and validate the runtime snapshot in memory**

Fetch `/api/cameras` and assert there are eight cameras, each with exactly recording/live/focus desired outputs. Keep the complete JSON in one shell process; do not write it to the repository or `data/`.

- [ ] **Step 2: Apply each camera through the existing policy endpoint**

For each camera, build a request with its current `expectedDesiredRevision`; preserve recording and focus objects and replace only the live object's policy fields with:

```json
{
  "videoMode": "h264",
  "maxWidth": 1280,
  "maxHeight": 720,
  "maxFPS": 15,
  "audioMode": "none",
  "activation": "always"
}
```

Preserve the live `purpose` and `sourceKey`. Require each response to report an applied runtime; on any failure, restore all captured desired policies through the same endpoint using their new current revisions.

- [ ] **Step 3: Verify runtime state and generated behavior**

Require all eight public `*-live` outputs to report `running` with one producer. Verify desired/applied public settings match the common profile and sanitized go2rtc configuration contains preload entries for each public live stream without printing source URLs.

- [ ] **Step 4: Measure 60-second CPU acceptance threshold**

Sample aggregate FFmpeg `%CPU` once per second for 60 seconds with `pidstat -C ffmpeg 1 60`. Compute the average sum across FFmpeg processes. If the result exceeds 280%, restore the captured policies and verify restoration; otherwise retain the profile.

- [ ] **Step 5: Measure reconnect behavior**

With all browser consumers disconnected, monitor public live consumer transitions at 100 ms resolution while reloading `/live`. Record first and last consumer creation times and measure `fire-station-1-live` keyframe arrival intervals. The target is a warm-server keyframe wait near one second rather than the previous 6.36–8.14 seconds.

### Task 5: Final Verification and Commit

**Files:**
- Review all files changed by Tasks 1–3.

**Interfaces:**
- Consumes: code, tests, built assets, and runtime evidence.
- Produces: one reviewed implementation commit without unrelated workspace changes.

- [ ] **Step 1: Run fresh final verification**

Run:

```bash
git diff --check
cd web && node --experimental-strip-types --test tests/*.test.ts && npm run lint && npm run build
cd .. && go test ./... && go build -o camstationd ./cmd/camstationd
scripts/camstationctl.sh verify
```

Expected: every command exits 0 and runtime verification reports healthy daemon/go2rtc behavior.

- [ ] **Step 2: Review scope**

Run `git status --short` and `git diff --stat`. Stage only the common-live-profile source, tests, documentation, and generated embedded web assets; preserve all unrelated user changes.

- [ ] **Step 3: Commit**

```bash
git commit -m "feat: standardize browser live outputs"
```
