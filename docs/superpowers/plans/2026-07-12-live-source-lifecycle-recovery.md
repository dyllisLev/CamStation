# Live Source Lifecycle Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep every applied live input producer warm so ordinary `/live` connections no longer fail during on-demand private-source startup.

**Architecture:** Preserve the SQLite policy model, public recording/live/focus outputs, FFmpeg producer selection, and atomic apply coordinator. Extend only go2rtc config rendering so each private source referenced by a live output is preloaded once with video and audio, while transformed public outputs remain on demand unless their existing activation policy is `always`.

**Tech Stack:** Go 1.24, SQLite-backed camera policies, go2rtc 1.9.14 preload configuration, FFmpeg, React/Vite embedded console

## Global Constraints

- Do not change camera URLs, credentials, profile selection, or the legacy `cctv` server.
- Do not expose raw camera URLs, private producer names, localhost transport URLs, or credentials through public APIs, UI, events, or committed evidence.
- Keep recording/live/focus policies, desired/applied revisions, last-good rollback, recorder handoff, and short-GOP FFmpeg settings unchanged.
- Preload private live inputs with `video&audio`; do not preload every transformed public output.
- Deduplicate preload entries when multiple outputs reference the same source.
- Use `scripts/camstationctl.sh` for daemon lifecycle operations.

---

### Task 1: Render warm private live sources

**Files:**
- Modify: `internal/stream/policy_test.go`
- Modify: `internal/stream/policy.go:149-168`

**Interfaces:**
- Consumes: `resolvedOutput.SourceName`, `store.CameraOutputLive`, and the existing `renderPolicyConfig(cameras []store.Camera, applied bool)` desired/applied resolution.
- Produces: a deduplicated `preload` entry of `<private source name>: "video&audio"` for every live output while retaining existing public-output preload entries.

- [ ] **Step 1: Write the failing private-source preload test**

Add this focused regression beside the existing preload test in `internal/stream/policy_test.go`:

```go
func TestRenderPolicyConfigPreloadsPrivateLiveSourceOnce(t *testing.T) {
	camera, live := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	live.Purpose = store.CameraOutputLive
	live.StreamName = "camera-live"
	recording := live
	recording.Purpose = store.CameraOutputRecording
	recording.StreamName = "camera-recording"
	focus := live
	focus.Purpose = store.CameraOutputFocus
	focus.StreamName = "camera-focus"
	camera.Outputs = []store.CameraOutput{recording, live, focus}

	config, _, err := renderPolicyConfig([]store.Camera{camera}, false)
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	entry := fmt.Sprintf("  %q: %q", PrivateSourceName(camera.ID, camera.Streams[0].ID), "video&audio")
	if strings.Count(text, entry) != 1 {
		t.Fatalf("private live preload count = %d, want 1: %s", strings.Count(text, entry), text)
	}
	for _, publicName := range []string{recording.StreamName, live.StreamName, focus.StreamName} {
		if strings.Contains(text, fmt.Sprintf("  %q: %q", publicName, "video&audio")) {
			t.Fatalf("on-demand public output %q was preloaded: %s", publicName, text)
		}
	}
}
```

Add `fmt` to the test imports.

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./internal/stream -run TestRenderPolicyConfigPreloadsPrivateLiveSourceOnce -count=1
```

Expected: FAIL with `private live preload count = 0, want 1` because current rendering preloads only public outputs whose activation is `always`.

- [ ] **Step 3: Implement one deduplicated preload writer**

Replace the existing preload block in `renderPolicyConfig` with:

```go
	preload := false
	preloaded := make(map[string]bool)
	writePreload := func(name, tracks string) {
		if preloaded[name] {
			return
		}
		if !preload {
			buf.WriteString("preload:\n")
			preload = true
		}
		fmt.Fprintf(&buf, "  %s: %s\n", quoteYAML(name), quoteYAML(tracks))
		preloaded[name] = true
	}
	for _, camera := range cameras {
		for i, output := range camera.Outputs {
			if applied && output.AppliedPolicy.SourceKey != "" {
				output = outputFromSnapshot(output, output.AppliedPolicy)
			}
			if i >= len(resolved[camera.ID]) {
				continue
			}
			if output.Purpose == store.CameraOutputLive {
				writePreload(resolved[camera.ID][i].SourceName, "video&audio")
			}
			if output.Activation != store.CameraActivationAlways {
				continue
			}
			tracks := "video&audio"
			if output.AudioMode == store.CameraAudioNone {
				tracks = "video"
			}
			writePreload(output.StreamName, tracks)
		}
	}
```

Do not change `resolveOutputWithEffective`, FFmpeg templates, stream names, or the later `streams:` rendering.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
go test ./internal/stream -run 'TestRenderPolicyConfigPreloadsPrivateLiveSourceOnce|TestRenderPolicyConfigKeepsSourcesPrivateEscapesYAMLAndPreloadsAlways|TestStartupConfigUsesAppliedPolicyInsteadOfPendingDesiredPolicy' -count=1
go test ./internal/stream -count=1
```

Expected: both commands PASS. The existing `activation=always` public preload and applied startup snapshot tests must remain green.

- [ ] **Step 5: Commit the tested renderer change**

```bash
git add internal/stream/policy.go internal/stream/policy_test.go
git commit -m "fix live source preload lifecycle"
```

### Task 2: Verify the complete repository and cctv2 runtime

**Files:**
- Modify: `docs/07-implementation-status.md`

**Interfaces:**
- Consumes: the generated private live-source preload entries from Task 1 and the managed daemon lifecycle.
- Produces: fresh automated and real-camera evidence that the normal UI path acquires all available live producers without the prior on-demand 404.

- [ ] **Step 1: Run repository verification before deployment**

Run:

```bash
go test ./... -count=1
go build -o camstationd ./cmd/camstationd
git diff --check
```

Expected: all commands exit 0. Do not run the web build because this recovery changes no frontend source and the worktree already contains unrelated frontend and embedded-asset changes.

- [ ] **Step 2: Inspect generated policy without exposing secrets**

Use package tests plus a redacted/count-only inspection to verify that every live output maps to one private preload, duplicate source identities occur once, and transformed public outputs remain absent from preload unless `activation=always`. Do not print `data/go2rtc.yaml`.

Expected: eight registered live outputs resolve to private preload entries, with fewer entries only when live outputs intentionally share the same source identity.

- [ ] **Step 3: Deploy through the managed lifecycle**

Run:

```bash
scripts/camstationctl.sh restart
scripts/camstationctl.sh verify
```

Expected: daemon and managed go2rtc restart successfully; health verification exits 0. Do not use `kill`, `pkill`, `nohup`, or alter the legacy `cctv` server.

- [ ] **Step 4: Verify producers before and during ordinary `/live` playback**

Record KST timestamps and sanitized go2rtc state only. Confirm private live inputs acquire producers after restart, then load `http://10.0.0.29:18080/live` through the available real browser and verify each camera reaches a decoded first frame. Check `data/runtime-logs/camstationd.out` for the previous on-demand `404`/`Invalid data found when processing input` signature without printing camera URLs.

Expected: all camera endpoints that were healthy before the regression produce frames; no ordinary UI connection emits the prior on-demand 404 signature. If any camera remains unavailable, report the exact private-source producer state and stop rather than modifying the legacy server or camera configuration.

- [ ] **Step 5: Record the verified recovery**

Add a concise entry to `docs/07-implementation-status.md` containing the KST verification time, commands run, producer/frame result, absence or presence of the 404 signature, and the fact that legacy `cctv` was not modified. Do not include private names, URLs, credentials, or raw config.

- [ ] **Step 6: Re-run final verification and commit documentation**

Run:

```bash
go test ./... -count=1
go build -o camstationd ./cmd/camstationd
scripts/camstationctl.sh verify
git diff --check
```

Expected: every command exits 0 after runtime verification.

```bash
git add docs/07-implementation-status.md
git commit -m "verify live source lifecycle recovery"
```
