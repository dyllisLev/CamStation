//go:build linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGo2RTCParentDeathStopsFFmpeg(t *testing.T) {
	switch os.Getenv("CAMSTATION_TEST_PROCESS_ROLE") {
	case "wrapper":
		os.Args = []string{"camstation-ffmpeg", "-c:v", "libx264", "-f", "null", "-"}
		main()
		return
	case "parent":
		c := exec.Command(os.Args[0], "-test.run=^TestGo2RTCParentDeathStopsFFmpeg$")
		c.Env = append(os.Environ(), "CAMSTATION_TEST_PROCESS_ROLE=wrapper")
		if c.Start() != nil {
			os.Exit(2)
		}
		_ = c.Wait()
		os.Exit(0)
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "ffmpeg")
	pidFile := filepath.Join(dir, "child-pid")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$$\" > '"+pidFile+"'\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(os.Args[0], "-test.run=^TestGo2RTCParentDeathStopsFFmpeg$")
	parent.Env = append(os.Environ(), "CAMSTATION_TEST_PROCESS_ROLE=parent", "CAMSTATION_FFMPEG_REAL="+binary)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Process.Kill(); _ = parent.Wait() })
	deadline := time.Now().Add(4 * time.Second)
	pid := ""
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(pidFile)
		if err == nil && len(b) > 0 {
			pid = string(b)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == "" {
		t.Fatal("ffmpeg did not start")
	}
	_ = parent.Process.Kill()
	_ = parent.Wait()
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
		if os.IsNotExist(err) || strings.Contains(string(b), ") Z ") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ffmpeg survived go2rtc parent death")
}
