package vieweragent

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestChildSupervisorUsesThreeBoundedRestarts(t *testing.T) {
	var runs int
	var delays []time.Duration
	err := RunChildSupervisor(t.Context(), func(context.Context) error {
		runs++
		return errors.New("crashed")
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})
	if err == nil || runs != 4 {
		t.Fatalf("err=%v runs=%d", err, runs)
	}
	want := []time.Duration{5 * time.Second, 30 * time.Second, 120 * time.Second}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays=%v want=%v", delays, want)
	}
}

func TestCurrentReleaseCannotEscapeInstallDirectory(t *testing.T) {
	installDir := t.TempDir()
	outside := filepath.Join(filepath.Dir(installDir), "outside", "agent.exe")
	if _, err := ValidateCurrentRelease(installDir, CurrentRelease{SchemaVersion: SchemaVersion, AgentPath: outside}); err == nil {
		t.Fatal("current release escaped install directory")
	}
	inside := filepath.Join(installDir, "releases", "1", "agent.exe")
	if _, err := ValidateCurrentRelease(installDir, CurrentRelease{AgentPath: inside}); err == nil {
		t.Fatal("schema-less current release pointer was accepted")
	}
}
