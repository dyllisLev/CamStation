# Camera Source Relay Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route every private camera input through a restartable FFmpeg copy relay so a stalled source can recover without restarting go2rtc or changing camera/output policies.

**Architecture:** Keep private source aliases, preload, and public recording/live/focus outputs unchanged. Change only the producer stored under each private source alias: FFmpeg copies video/audio for every protocol, adding a five-second timeout only to RTSP/RTSPS inputs.

**Tech Stack:** Go 1.24, net/url, go2rtc 1.9.14 FFmpeg source syntax, FFmpeg copy mode, SQLite-backed camera policies

## Global Constraints

- Apply the copy relay to every persisted camera input source.
- Never re-encode in the private relay; use `video=copy` and `audio=copy`.
- Add `timeout=5` only for RTSP and RTSPS URL schemes.
- Preserve public output producers, short-GOP H.264 settings, preload behavior, desired/applied policy revisions, rollback, and recorder handoff.
- Keep camera URLs and credentials private and never print generated config during runtime verification.
- Use `scripts/camstationctl.sh` for daemon lifecycle operations and do not modify the legacy `cctv` server.

---

### Task 1: Render restartable private source relays

**Files:**
- Modify: `internal/stream/policy_test.go`
- Modify: `internal/stream/policy.go`

**Interfaces:**
- Consumes: `store.CameraStream.URL` and its parsed URL scheme.
- Produces: `privateInputProducer(rawURL string) string`, returning an FFmpeg copy source with a five-second timeout for RTSP/RTSPS.

- [ ] **Step 1: Write failing protocol-specific relay tests**

Add to `internal/stream/policy_test.go`:

```go
func TestPrivateInputProducerUsesRestartableCopyRelay(t *testing.T) {
	for _, tc := range []struct {
		name, rawURL, want string
	}{
		{
			name:   "rtsp",
			rawURL: "rtsp://user:pass@192.0.2.1/live",
			want:   "ffmpeg:rtsp://user:pass@192.0.2.1/live#video=copy#audio=copy#timeout=5",
		},
		{
			name:   "rtsps",
			rawURL: "rtsps://user:pass@192.0.2.1/live",
			want:   "ffmpeg:rtsps://user:pass@192.0.2.1/live#video=copy#audio=copy#timeout=5",
		},
		{
			name:   "http-flv",
			rawURL: "http://192.0.2.1/flv?user=test&password=secret",
			want:   "ffmpeg:http://192.0.2.1/flv?user=test&password=secret#video=copy#audio=copy",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := privateInputProducer(tc.rawURL); got != tc.want {
				t.Fatalf("producer = %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./internal/stream -run TestPrivateInputProducerUsesRestartableCopyRelay -count=1
```

Expected: build FAIL with `undefined: privateInputProducer`.

- [ ] **Step 3: Implement the minimal producer helper and use it once**

Add `net/url` to `internal/stream/policy.go` imports and define:

```go
func privateInputProducer(rawURL string) string {
	producer := "ffmpeg:" + rawURL + "#video=copy#audio=copy"
	parsed, err := url.Parse(rawURL)
	if err == nil && (strings.EqualFold(parsed.Scheme, "rtsp") || strings.EqualFold(parsed.Scheme, "rtsps")) {
		producer += "#timeout=5"
	}
	return producer
}
```

In the private source rendering loop, replace only `quoteYAML(source.URL)` with `quoteYAML(privateInputProducer(source.URL))`.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
go test ./internal/stream -run 'TestPrivateInputProducerUsesRestartableCopyRelay|TestRenderPolicyConfigPreloadsPrivateLiveSourceOnce|TestRenderPolicyConfigUsesShortFixedGOPForSoftwareH264' -count=1
go test ./internal/stream -count=1
```

Expected: both commands PASS. Existing public output producer and preload tests remain green.

- [ ] **Step 5: Commit the isolated renderer change**

```bash
git add internal/stream/policy.go internal/stream/policy_test.go
git commit -m "fix camera source relay recovery"
```

### Task 2: Verify and deploy the common relay

**Files:**
- Modify: `docs/07-implementation-status.md`

**Interfaces:**
- Consumes: the common private copy relay generated in Task 1.
- Produces: repository verification plus sanitized runtime evidence for all eight live cameras and one bounded relay replacement.

- [ ] **Step 1: Run full automated verification in isolation**

```bash
go test ./... -count=1
go build -o /tmp/camstationd-camera-source-relay ./cmd/camstationd
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 2: Apply only the tested commit to the cctv2 branch and build**

Confirm the main worktree has no overlapping Go changes, cherry-pick the tested commit, and run:

```bash
go build -o camstationd ./cmd/camstationd
```

Expected: build exits 0 without staging or altering unrelated frontend work.

- [ ] **Step 3: Restart with the managed lifecycle and verify source activity**

```bash
scripts/camstationctl.sh restart
scripts/camstationctl.sh verify
```

Without printing config or process arguments, confirm eight private live source producers exist, all eight byte counters increase across a ten-second sample, and all eight public live outputs acquire one browser consumer when `/live` is open.

Expected: all eight private source counters increase and 소방서1·5 are no longer stuck at zero.

- [ ] **Step 4: Perform one bounded child-relay recovery check**

Identify exactly one FFmpeg copy-relay child belonging to 소방서1 by matching its public camera host in `/proc/<pid>/cmdline` without printing the command. Send SIGTERM to that exact child only; do not stop camstationd, go2rtc, another camera, or use broad process matching. Poll for a replacement child PID and resumed source-byte growth.

Expected: replacement relay appears and source bytes resume within 15 seconds. If it does not, stop and report the failed recovery rather than restarting all services.

- [ ] **Step 5: Record KST runtime evidence**

Add a concise status entry containing test/build/managed-verify results, eight increasing source counters, 소방서1·5 recovery state, bounded relay replacement time, and confirmation that legacy `cctv` and camera settings were untouched. Do not include raw URLs, credentials, internal aliases, PIDs, or generated commands.

- [ ] **Step 6: Run final verification and commit documentation**

```bash
go test ./... -count=1
go build -o camstationd ./cmd/camstationd
scripts/camstationctl.sh verify
git diff --check
git add docs/07-implementation-status.md
git commit -m "verify camera source relay recovery"
```

Expected: all verification commands exit 0 and only the status document is committed in this step.
