//go:build linux

package ffmpegsupervisor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeFFmpeg(t *testing.T, body string) Config {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + filepath.Join(dir, "calls") + "'\n" + body
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return Config{Binary: bin, StateDir: dir, ProbeTimeout: time.Second}
}
func calls(t *testing.T, c Config) []string {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(c.StateDir, "calls"))
	if e != nil {
		t.Fatal(e)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}
func TestPassthrough(t *testing.T) {
	c := fakeFFmpeg(t, "echo version-output\nexit 7\n")
	var out bytes.Buffer
	c.Stdout = &out
	if n := Run(context.Background(), c, []string{"-version"}); n != 7 {
		t.Fatal(n)
	}
	if out.String() != "version-output\n" || len(calls(t, c)) != 1 {
		t.Fatal(out.String())
	}
}
func TestFallbackOncePreservesMediaOptions(t *testing.T) {
	c := fakeFFmpeg(t, `case "$*" in *lavfi*) exit 0;; *h264_nvenc*) echo 'Driver does not support required nvenc API rtsp://private:secret@camera' >&2; exit 1;; esac
exit 0
`)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	args := []string{"-camstation-output", "cam1_live", "-c:v", "h264_nvenc", "-preset", "llhp", "-tune", "ll", "-vf", "scale=640:360,fps=10", "-c:a", "aac", "-f", "null", "-"}
	if n := Run(context.Background(), c, args); n != 0 {
		t.Fatal(n)
	}
	got := calls(t, c)
	if len(got) != 3 {
		t.Fatal(got)
	}
	if !strings.Contains(got[2], "libx264 -preset veryfast -tune zerolatency -vf scale=640:360,fps=10 -c:a aac") || strings.Contains(got[2], "camstation-") {
		t.Fatal(got[2])
	}
	if stderr.Len() != 0 {
		t.Fatal("raw stderr leaked")
	}
	states, err := ReadStatuses(c.StateDir)
	if err != nil || len(states) != 1 {
		t.Fatal(states, err)
	}
	if states[0].ActualEncoder != "cpu" || states[0].FallbackReason != "api_incompatible" || states[0].Running {
		t.Fatal(states)
	}
	release, err := acquire(c.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
func TestProbeTimeoutAndCancellation(t *testing.T) {
	c := fakeFFmpeg(t, `case "$*" in *lavfi*) exec sleep 30;; esac
exit 0
`)
	c.ProbeTimeout = 40 * time.Millisecond
	start := time.Now()
	if n := Run(context.Background(), c, []string{"-camstation-output", "cam_probe", "-c:v", "h264_nvenc", "-f", "null", "-"}); n != 0 {
		t.Fatal(n)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("probe not reaped promptly")
	}
	ss, _ := ReadStatuses(c.StateDir)
	if len(ss) != 1 || ss[0].FallbackReason != "probe_timeout" {
		t.Fatal(ss)
	}
	c = fakeFFmpeg(t, "exec sleep 30\n")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if n := Run(ctx, c, []string{"-c:v", "h264_nvenc", "-f", "null", "-"}); n != 130 {
		t.Fatal(n)
	}
	if len(calls(t, c)) != 1 {
		t.Fatal("CPU fallback started after cancellation")
	}
}
func TestCPUAllocationMarker(t *testing.T) {
	c := fakeFFmpeg(t, "exit 0\n")
	if Run(context.Background(), c, []string{"-camstation-output", "cam2_live", "-camstation-requested", "nvenc", "-camstation-reason", "session_limit", "-c:v", "libx264", "-"}) != 0 {
		t.Fatal("run")
	}
	ss, _ := ReadStatuses(c.StateDir)
	if len(ss) != 1 || ss[0].RequestedEncoder != "nvenc" || ss[0].FallbackReason != "session_limit" {
		t.Fatal(ss)
	}
	if strings.Contains(calls(t, c)[0], "camstation-") {
		t.Fatal("marker leaked")
	}
}
func TestSlotProcess(t *testing.T) {
	if dir := os.Getenv("CAMSTATION_TEST_SLOT_DIR"); dir != "" {
		release, err := acquire(dir)
		if err != nil {
			fmt.Println("full")
			os.Exit(0)
		}
		fmt.Println("acquired")
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		release()
		os.Exit(0)
	}
	dir := t.TempDir()
	var children []*exec.Cmd
	t.Cleanup(func() {
		for _, c := range children {
			_ = c.Process.Kill()
			_ = c.Wait()
		}
	})
	acquired := 0
	var readers []io.Reader
	for i := 0; i < 8; i++ {
		c := exec.Command(os.Args[0], "-test.run=^TestSlotProcess$")
		c.Env = append(os.Environ(), "CAMSTATION_TEST_SLOT_DIR="+dir)
		_, err := c.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := c.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, c)
		readers = append(readers, out)
	}
	for _, out := range readers {
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "acquired\n" {
			acquired++
		}
	}
	if acquired != 3 {
		t.Fatalf("acquired %d", acquired)
	}
	for _, c := range children {
		_ = c.Process.Kill()
		_ = c.Wait()
	}
	children = nil
	var releases []func()
	for i := 0; i < 3; i++ {
		release, err := acquire(dir)
		if err != nil {
			t.Fatal("crashed process leaked slot", err)
		}
		releases = append(releases, release)
	}
	for _, r := range releases {
		r()
	}
}
func TestStalePIDIdentity(t *testing.T) {
	dir := t.TempDir()
	writeStatus(dir, Status{Output: "cam1_live", Running: true})
	path := filepath.Join(dir, "cam1_live.json")
	b, _ := os.ReadFile(path)
	b = bytes.Replace(b, []byte(processIdentity(os.Getpid())), []byte("wrong-start"), 1)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	ss, _ := ReadStatuses(dir)
	if len(ss) != 1 || ss[0].Running {
		t.Fatal(ss)
	}
}

func TestChildDiesWithWrapper(t *testing.T) {
	if dir := os.Getenv("CAMSTATION_TEST_WRAPPER_DIR"); dir != "" {
		os.Exit(Run(context.Background(), Config{Binary: filepath.Join(dir, "ffmpeg"), StateDir: dir}, []string{"-c:v", "h264_nvenc", "-f", "null", "-"}))
	}
	c := fakeFFmpeg(t, `case "$*" in *lavfi*) exit 0;; esac
printf '%s' "$$" > "$(dirname "$0")/child-pid"
exec sleep 30
`)
	wrapper := exec.Command(os.Args[0], "-test.run=^TestChildDiesWithWrapper$")
	wrapper.Env = append(os.Environ(), "CAMSTATION_TEST_WRAPPER_DIR="+c.StateDir)
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wrapper.Process.Kill(); _ = wrapper.Wait() })
	deadline := time.Now().Add(3 * time.Second)
	var pid string
	for time.Now().Before(deadline) {
		b, e := os.ReadFile(filepath.Join(c.StateDir, "child-pid"))
		if e == nil && len(b) > 0 {
			pid = string(b)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == "" {
		t.Fatal("child did not start")
	}
	_ = wrapper.Process.Kill()
	_ = wrapper.Wait()
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
		if os.IsNotExist(err) || strings.Contains(string(b), ") Z ") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
	if err == nil && !strings.Contains(string(b), ") Z ") {
		t.Fatal("ffmpeg survived wrapper")
	}
	var releases []func()
	for i := 0; i < 3; i++ {
		release, err := acquire(c.StateDir)
		if err != nil {
			t.Fatal("slot survived child", err)
		}
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}

func TestSourceErrorDoesNotRetryCPU(t *testing.T) {
	for _, message := range []string{"rtsp://private:secret@camera: Server returned 404 Not Found", "input.mp4: No such file or directory", "rtsp: permission denied"} {
		t.Run(message[:4], func(t *testing.T) {
			c := fakeFFmpeg(t, "case \"$*\" in *lavfi*) exit 0;; esac\necho '"+message+"' >&2\nexit 1\n")
			args := []string{"-camstation-output", "cam_source", "-c:v", "h264_nvenc", "-f", "null", "-"}
			if Run(context.Background(), c, args) != 1 {
				t.Fatal("source error masked")
			}
			if len(calls(t, c)) != 2 {
				t.Fatal("source failure retried CPU")
			}
			if readFallback(c.StateDir, "cam_source", currentParent()) != "" {
				t.Fatal("source failure latched CPU")
			}
		})
	}
}
func TestLateGPUErrorLatchesForSameParent(t *testing.T) {
	c := fakeFFmpeg(t, `case "$*" in *lavfi*) exit 0;; *h264_nvenc*)
 i=0; while [ "$i" -lt 1000 ]; do echo 'frame=2000 fps=30 benign long running statistics' >&2; i=$((i+1)); done
 echo '[h264_nvenc] EncodePicture failed: NV_ENC_ERR_GENERIC (injected development failure)' >&2
 exit 1;; esac
exit 0
`)
	args := []string{"-camstation-output", "cam_late", "-c:v", "h264_nvenc", "-f", "null", "-"}
	if Run(context.Background(), c, args) != 0 {
		t.Fatal("CPU retry failed")
	}
	if len(calls(t, c)) != 3 {
		t.Fatal(calls(t, c))
	}
	if Run(context.Background(), c, args) != 0 {
		t.Fatal("latched CPU failed")
	}
	got := calls(t, c)
	if len(got) != 4 || !strings.Contains(got[3], "libx264") {
		t.Fatal("automatic restart lost CPU latch", got)
	}
	states, _ := ReadStatuses(c.StateDir)
	if len(states) != 1 || states[0].ActualEncoder != "cpu" || states[0].FallbackReason != "encoder_failed" {
		t.Fatal(states)
	}
	// An old generation must not block a new go2rtc parent's NVENC attempt.
	old := currentParent()
	old.StartIdentity += "-previous"
	writeFallback(c.StateDir, "cam_late", old, "encoder_failed")
	if Run(context.Background(), c, args) != 0 {
		t.Fatal("fresh generation failed")
	}
	if got := calls(t, c); len(got) != 7 || !strings.Contains(got[4], "lavfi") || !strings.Contains(got[5], "h264_nvenc") {
		t.Fatal("new generation did not retry GPU", got)
	}
}
func TestGPUErrorLatchSurvivesCancellation(t *testing.T) {
	c := fakeFFmpeg(t, `case "$*" in *lavfi*) exit 0;; esac
 echo '[h264_nvenc] EncodePicture failed: NV_ENC_ERR_GENERIC' >&2
 exec sleep 30
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, c, []string{"-camstation-output", "cam_cancel", "-c:v", "h264_nvenc", "-f", "null", "-"})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for readFallback(c.StateDir, "cam_cancel", currentParent()) == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if readFallback(c.StateDir, "cam_cancel", currentParent()) != "encoder_failed" {
		t.Fatal("GPU diagnostic not persisted before process exit")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatal(code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not reap")
	}
	if readFallback(c.StateDir, "cam_cancel", currentParent()) != "encoder_failed" {
		t.Fatal("cancellation lost GPU latch")
	}
}
func TestTailBufferRetainsRecentErrors(t *testing.T) {
	var b limitedBuffer
	_, _ = b.Write([]byte(strings.Repeat("x", 10000)))
	_, _ = b.Write([]byte("recent error"))
	if b.Len() != 8192 || !strings.HasSuffix(b.String(), "recent error") {
		t.Fatal("stderr tail not retained")
	}
}

func TestProbeCapabilityFailureLatchesUntilParentChanges(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint("timeout=", timeout), func(t *testing.T) {
			body := "case \"$*\" in *lavfi*) echo '[h264_nvenc] Cannot load libnvidia-encode.so.1' >&2; exit 1;; esac\nexit 0\n"
			reason := "library_unavailable"
			if timeout {
				body = "case \"$*\" in *lavfi*) exec sleep 30;; esac\nexit 0\n"
				reason = "probe_timeout"
			}
			c := fakeFFmpeg(t, body)
			c.ProbeTimeout = 40 * time.Millisecond
			args := []string{"-camstation-output", "cam_capability", "-c:v", "h264_nvenc", "-f", "null", "-"}
			if Run(context.Background(), c, args) != 0 {
				t.Fatal("initial fallback")
			}
			if got := readFallback(c.StateDir, "cam_capability", currentParent()); got != reason {
				t.Fatal(got)
			}
			if Run(context.Background(), c, args) != 0 {
				t.Fatal("latched fallback")
			}
			got := calls(t, c)
			if len(got) != 3 || !strings.Contains(got[2], "libx264") {
				t.Fatal("repeated probe", got)
			}
			old := currentParent()
			old.StartIdentity += "-previous"
			writeFallback(c.StateDir, "cam_capability", old, reason)
			if Run(context.Background(), c, args) != 0 {
				t.Fatal("new generation fallback")
			}
			if got = calls(t, c); len(got) != 5 || !strings.Contains(got[3], "lavfi") {
				t.Fatal("new generation failed to probe", got)
			}
		})
	}
}
func TestProbeCancellationDoesNotLatch(t *testing.T) {
	c := fakeFFmpeg(t, "exec sleep 30\n")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if Run(ctx, c, []string{"-camstation-output", "cam_cancel_probe", "-c:v", "h264_nvenc", "-f", "null", "-"}) != 130 {
		t.Fatal("cancel")
	}
	if readFallback(c.StateDir, "cam_cancel_probe", currentParent()) != "" {
		t.Fatal("user cancellation latched capability failure")
	}
}

func TestReplacementStatusCannotBeOverwritten(t *testing.T) {
	if dir := os.Getenv("CAMSTATION_TEST_STATUS_DIR"); dir != "" {
		role := os.Getenv("CAMSTATION_TEST_STATUS_ROLE")
		status := Status{Output: "cam_replace", ActualEncoder: role, Running: true}
		writeStatus(dir, status)
		fmt.Println("written")
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		if role == "nvenc" {
			writeStatus(dir, status)
			status.Running = false
			writeStatus(dir, status)
		}
		fmt.Println("done")
		_, _ = os.Stdin.Read(b[:])
		os.Exit(0)
	}
	dir := t.TempDir()
	start := func(role string) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
		t.Helper()
		c := exec.Command(os.Args[0], "-test.run=^TestReplacementStatusCannotBeOverwritten$")
		c.Env = append(os.Environ(), "CAMSTATION_TEST_STATUS_DIR="+dir, "CAMSTATION_TEST_STATUS_ROLE="+role)
		in, err := c.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := c.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		reader := bufio.NewReader(out)
		if line, err := reader.ReadString('\n'); err != nil || line != "written\n" {
			t.Fatal(line, err)
		}
		return c, in, reader
	}
	_, older, olderOut := start("nvenc")
	_, _, _ = start("cpu")
	_, _ = older.Write([]byte("x"))
	if line, err := olderOut.ReadString('\n'); err != nil || line != "done\n" {
		t.Fatal(line, err)
	}
	states, err := ReadStatuses(dir)
	if err != nil || len(states) != 1 || states[0].ActualEncoder != "cpu" || !states[0].Running {
		t.Fatal("old retry stole replacement status", states, err)
	}
}
func TestStatusOwnershipResetsAcrossBoots(t *testing.T) {
	existing := record{PID: 200, StartIdentity: "999999", BootID: "previous-boot"}
	current := record{PID: 100, StartIdentity: "100", BootID: "new-boot"}
	if newerOwner(existing, current) {
		t.Fatal("old boot blocked current status")
	}
	existing.BootID = current.BootID
	if !newerOwner(existing, current) {
		t.Fatal("newer same-boot owner not protected")
	}
}

func TestManagedRTSPGPUFailureWaitsForProducerRecreation(t *testing.T) {
	c := fakeFFmpeg(t, `case "$*" in *lavfi*) exit 0;; *h264_nvenc*) echo '[h264_nvenc] EncodePicture failed: NV_ENC_ERR_GENERIC' >&2; exit 1;; esac
exit 0
`)
	args := []string{"-camstation-output", "cam_rtsp", "-c:v", "h264_nvenc", "-f", "rtsp", "rtsp://localhost/output"}
	if Run(context.Background(), c, args) != 1 {
		t.Fatal("managed RTSP must return failed GPU child exit")
	}
	if len(calls(t, c)) != 2 {
		t.Fatal("inline CPU retried against stale RTSP waiter", calls(t, c))
	}
	if Run(context.Background(), c, args) != 0 {
		t.Fatal("recreated producer CPU failed")
	}
	got := calls(t, c)
	if len(got) != 3 || !strings.Contains(got[2], "libx264") {
		t.Fatal("new producer did not use CPU", got)
	}
}
