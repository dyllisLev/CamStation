package vieweragent

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChildSupervisorUsesThreeBoundedCrashRestarts(t *testing.T) {
	var runs int
	var delays []time.Duration
	err := RunChildSupervisor(t.Context(), func(context.Context) ChildExit {
		runs++
		return ChildExit{Kind: ChildCrashed, Err: errors.New("crashed")}
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

func TestPlannedAgentRestartReloadsImmediatelyWithoutCrashBudget(t *testing.T) {
	var runs int
	var delays []time.Duration
	err := RunChildSupervisor(t.Context(), func(context.Context) ChildExit {
		runs++
		if runs == 1 {
			return ChildExit{Kind: ChildPlannedRestart}
		}
		return ChildExit{Kind: ChildStopped}
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})
	if err != nil || runs != 2 || len(delays) != 0 {
		t.Fatalf("err=%v runs=%d delays=%v", err, runs, delays)
	}
}

func TestMultiplePlannedAgentRestartsNeverConsumeCrashBudget(t *testing.T) {
	var runs int
	var delays []time.Duration
	err := RunChildSupervisor(t.Context(), func(context.Context) ChildExit {
		runs++
		switch {
		case runs <= 3:
			return ChildExit{Kind: ChildPlannedRestart}
		case runs == 4:
			return ChildExit{Kind: ChildCrashed, Err: errors.New("crashed")}
		default:
			return ChildExit{Kind: ChildStopped}
		}
	}, func(_ context.Context, delay time.Duration) error {
		delays = append(delays, delay)
		return nil
	})
	if err != nil || runs != 5 || !reflect.DeepEqual(delays, []time.Duration{5 * time.Second}) {
		t.Fatalf("err=%v runs=%d delays=%v", err, runs, delays)
	}
}

type fakeHostChild struct {
	stopOnce sync.Once
	wait     chan error
	stops    int
	kills    int
}

func (child *fakeHostChild) Wait() error { return <-child.wait }
func (child *fakeHostChild) RequestStop() error {
	child.stops++
	child.stopOnce.Do(func() { child.wait <- nil })
	return nil
}
func (child *fakeHostChild) Kill() error {
	child.kills++
	child.stopOnce.Do(func() { child.wait <- errors.New("killed") })
	return nil
}

func TestHostForwardsGracefulStopBeforeKillDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	child := &fakeHostChild{wait: make(chan error, 1)}
	cancel()
	exit := RunManagedChild(ctx, child, 50*time.Millisecond)
	if exit.Kind != ChildStopped || child.stops != 1 || child.kills != 0 {
		t.Fatalf("exit=%+v stops=%d kills=%d", exit, child.stops, child.kills)
	}
}

type stubbornHostChild struct {
	wait  chan error
	stops int
	kills int
}

func (child *stubbornHostChild) Wait() error { return <-child.wait }
func (child *stubbornHostChild) RequestStop() error {
	child.stops++
	return nil
}
func (child *stubbornHostChild) Kill() error {
	child.kills++
	go func() {
		time.Sleep(3 * time.Millisecond)
		child.wait <- errors.New("killed")
	}()
	return nil
}

func TestHostWaitsForKilledChildBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	child := &stubbornHostChild{wait: make(chan error, 1)}
	cancel()
	started := time.Now()
	exit := RunManagedChild(ctx, child, 5*time.Millisecond)
	if exit.Kind != ChildStopped || child.stops != 1 || child.kills != 1 || time.Since(started) < 8*time.Millisecond {
		t.Fatalf("exit=%+v stops=%d kills=%d elapsed=%v", exit, child.stops, child.kills, time.Since(started))
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
	if _, err := ValidateCurrentRelease(installDir, CurrentRelease{
		SchemaVersion: SchemaVersion,
		AgentPath:     inside,
		ViewerPath:    outside,
	}); err == nil {
		t.Fatal("current Viewer path escaped install directory")
	}
}

func TestHostAcceptsTransactionalReleasePointerMetadata(t *testing.T) {
	installDir := t.TempDir()
	digest := strings.Repeat("a", 64)
	releaseID := "2.0.0-" + digest
	pointer := CurrentRelease{
		SchemaVersion: SchemaVersion, ReleaseID: releaseID, Version: "2.0.0", Digest: digest,
		AgentPath:  filepath.Join("releases", releaseID, "camstation-viewer-agent.exe"),
		ViewerPath: filepath.Join("releases", releaseID, "viewer", "CamStationViewer.exe"),
	}
	if err := atomicWriteJSON(filepath.Join(installDir, "current.json"), pointer); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCurrentRelease(installDir)
	if err != nil || loaded.ReleaseID != releaseID || loaded.Version != "2.0.0" || loaded.Digest != digest {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestHostReadinessRequiresValidRegularAgentAndSuccessfulStart(t *testing.T) {
	installDir := t.TempDir()
	current := CurrentRelease{SchemaVersion: SchemaVersion, AgentPath: filepath.Join("releases", "1", "agent.exe")}
	if err := atomicWriteJSON(filepath.Join(installDir, "current.json"), current); err != nil {
		t.Fatal(err)
	}
	readyCalls := 0
	startCalls := 0
	_, err := EstablishHostReadiness(func() (CurrentRelease, error) {
		return LoadReadyRelease(installDir)
	}, func(CurrentRelease) (HostChild, error) {
		startCalls++
		return &fakeHostChild{wait: make(chan error, 1)}, nil
	}, func() { readyCalls++ })
	if err == nil || startCalls != 0 || readyCalls != 0 {
		t.Fatalf("missing Agent reached readiness: err=%v starts=%d ready=%d", err, startCalls, readyCalls)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(installDir, current.AgentPath)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, current.AgentPath), []byte("agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	child, err := EstablishHostReadiness(func() (CurrentRelease, error) {
		return LoadReadyRelease(installDir)
	}, func(CurrentRelease) (HostChild, error) {
		startCalls++
		return &fakeHostChild{wait: make(chan error, 1)}, nil
	}, func() { readyCalls++ })
	if err != nil || child == nil || startCalls != 1 || readyCalls != 1 {
		t.Fatalf("valid Agent readiness failed: err=%v starts=%d ready=%d", err, startCalls, readyCalls)
	}
}

func TestAgentReadinessRequiresExplicitReadyLine(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		_, _ = io.WriteString(writer, "ready\n")
		_ = writer.Close()
	}()
	if err := WaitForAgentReady(reader, time.Second); err != nil {
		t.Fatal(err)
	}
	blocked, blockedWriter := io.Pipe()
	defer blockedWriter.Close()
	if err := WaitForAgentReady(blocked, 5*time.Millisecond); err == nil {
		t.Fatal("missing Agent ready line was accepted")
	}
}
