# H.264 Short GOP Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Force software H.264 outputs to emit an IDR at most every 20 frames so a damaged startup frame is replaced within roughly 1–2 seconds.

**Architecture:** Keep the existing per-camera policy and ffmpeg producer selection unchanged. Modify only the shared go2rtc libx264 template rendered by `renderPolicyConfig`, then verify the generated config and the real 소방서5 focus output after a controlled daemon restart.

**Tech Stack:** Go tests, go2rtc 1.9.14, ffmpeg/libx264, SQLite-backed camera policies, shell-based runtime verification.

## Global Constraints

- Keep 소방서5 recording as 3840×2160 HEVC copy.
- Keep 소방서5 focus capped at 1920×1080 software H.264.
- Do not change camera desired/applied policy rows, resolution, FPS, audio, or activation settings.
- Do not add a UI setting or dependency for GOP length.
- Manage the daemon only with `scripts/camstationctl.sh`.
- Never print or commit camera URLs, credentials, runtime DB, generated config, or logs.

---

### Task 1: Add the short-GOP libx264 runtime default

**Files:**
- Modify: `internal/stream/policy_test.go`
- Modify: `internal/stream/policy.go`

**Interfaces:**
- Consumes: `renderPolicyConfig(cameras []store.Camera, applied bool)`
- Produces: generated go2rtc `ffmpeg.h264` template containing `-g 20 -keyint_min 20 -sc_threshold 0`

- [ ] **Step 1: Write the failing config-rendering test**

Add this test to `internal/stream/policy_test.go`:

```go
func TestRenderPolicyConfigUsesShortFixedGOPForSoftwareH264(t *testing.T) {
	camera, output := policyFixture("hevc", "yuv420p", 8, 3840, 2160, 10)
	camera.Outputs = []store.CameraOutput{output}

	config, _, err := renderPolicyConfig([]store.Camera{camera}, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	for _, option := range []string{"-g 20", "-keyint_min 20", "-sc_threshold 0"} {
		if !strings.Contains(text, option) {
			t.Fatalf("H.264 template missing %q: %s", option, text)
		}
	}
}
```

- [ ] **Step 2: Run the test and verify RED**

Run:

```bash
go test ./internal/stream -run TestRenderPolicyConfigUsesShortFixedGOPForSoftwareH264 -count=1
```

Expected: FAIL with `H.264 template missing "-g 20"`.

- [ ] **Step 3: Add the minimum template flags**

Change the existing template line in `internal/stream/policy.go` to:

```go
buf.WriteString("  h264: \"-codec:v libx264 -preset:v veryfast -tune:v zerolatency -pix_fmt:v yuv420p -g 20 -keyint_min 20 -sc_threshold 0\"\n")
```

- [ ] **Step 4: Run focused and full tests**

Run:

```bash
go test ./internal/stream -run TestRenderPolicyConfigUsesShortFixedGOPForSoftwareH264 -count=1
go test ./internal/stream -count=1
go test ./... -count=1
```

Expected: all PASS.

- [ ] **Step 5: Commit the tested code**

```bash
git add internal/stream/policy.go internal/stream/policy_test.go
git commit -m "shorten software h264 recovery gop"
```

### Task 2: Roll out and verify the real stream

**Files:**
- Runtime only: `camstationd`, `data/go2rtc.yaml`, `data/camstation.db`
- No source file modifications

**Interfaces:**
- Consumes: built `camstationd` and existing applied camera policies
- Produces: healthy daemon plus short-GOP software H.264 output

- [ ] **Step 1: Build the daemon**

Run:

```bash
go build -o camstationd ./cmd/camstationd
```

Expected: exit 0.

- [ ] **Step 2: Restart through the lifecycle script**

Run:

```bash
scripts/camstationctl.sh restart
```

Expected: health `ok=true`, go2rtc listening on loopback, recorder state unchanged.

- [ ] **Step 3: Verify policy persistence without exposing URLs**

Query `/api/cameras` and assert:

```text
fire-station-5 recording: HEVC 3840×2160, transcoding=false
fire-station-5 live: H.264 640×360, transcoding=true
fire-station-5 focus: H.264 1920×1080, transcoding=true
desiredRevision == appliedRevision and state == applied
```

- [ ] **Step 4: Verify short GOP and recovered focus frame**

Read the local focus RTSP output only. Count H.264 keyframes over six seconds and require at least two. Capture a frame after the second keyframe and confirm it is 1920×1080 without a missing-reference decoder error.

- [ ] **Step 5: Verify the other affected surfaces**

Confirm 소방서3 and 소방서4 live outputs reconnect, and run:

```bash
scripts/camstationctl.sh verify
```

Expected: health passes and no unmanaged recorder process appears.
