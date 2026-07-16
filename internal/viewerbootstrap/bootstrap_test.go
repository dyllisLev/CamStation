package viewerbootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestBuildLaunchSpecUsesCurrentViewerAndAcceptedIdentity(t *testing.T) {
	installDir := t.TempDir()
	spec, err := BuildLaunchSpec(installDir, "releases/2/viewer/CamStationViewer.exe", LaunchGrant{
		Generation: 7, Nonce: "nonce-7", SessionID: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantExecutable := filepath.Join(installDir, "releases", "2", "viewer", "CamStationViewer.exe")
	wantArgs := []string{"--agent-generation=7", "--agent-nonce=nonce-7", "--agent-session=3"}
	if spec.Executable != wantExecutable || !reflect.DeepEqual(spec.Args, wantArgs) {
		t.Fatalf("spec=%+v", spec)
	}
	if _, err := BuildLaunchSpec(installDir, "../outside.exe", LaunchGrant{Generation: 7, Nonce: "n", SessionID: 3}); err == nil {
		t.Fatal("Viewer executable escaped install directory")
	}
}

func TestResolveViewerPathRejectsSymlinkEscape(t *testing.T) {
	installDir := t.TempDir()
	inside := filepath.Join(installDir, "releases", "2", "CamStationViewer.exe")
	if err := os.MkdirAll(filepath.Dir(inside), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inside, []byte("viewer"), 0o600); err != nil {
		t.Fatal(err)
	}
	insideLink := filepath.Join(installDir, "inside-link.exe")
	if err := os.Symlink(inside, insideLink); err != nil {
		t.Fatal(err)
	}
	resolved, err := ResolveViewerPath(installDir, insideLink)
	if err != nil || resolved != inside {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}

	outside := filepath.Join(t.TempDir(), "outside.exe")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(installDir, "escape.exe")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveViewerPath(installDir, escape); err == nil {
		t.Fatal("symlinked Viewer escaped the resolved install root")
	}
}

func TestGenerationGateAcceptsOnlyStrictlyIncreasingLaunches(t *testing.T) {
	var gate GenerationGate
	if !gate.Accept(7) || gate.Accept(7) || gate.Accept(6) || !gate.Accept(8) || gate.Accept(0) {
		t.Fatal("gate did not enforce strictly increasing generations")
	}
}

func TestBootstrapDeadlinesRemainFiveAndFortyFiveSeconds(t *testing.T) {
	if GracefulShutdownDeadline != 5*time.Second || TotalRecoveryDeadline != 45*time.Second {
		t.Fatalf("graceful=%v total=%v", GracefulShutdownDeadline, TotalRecoveryDeadline)
	}
}

func TestRunAssignsKillJobBeforeResume(t *testing.T) {
	process := &fakeProcess{wait: make(chan error, 1)}
	adapter := &fakeAdapter{grants: []LaunchGrant{{Generation: 7, Nonce: "nonce", SessionID: 3}}, processes: []*fakeProcess{process}, exitAfterReady: true}
	if err := RunWithDeadlines(t.Context(), t.TempDir(), adapter, 5*time.Second, 45*time.Second); err == nil {
		t.Fatal("unexpected Viewer exit was treated as a successful bootstrap")
	}
	events := adapter.Events()
	if eventIndex(events, "assign_job") < 0 || eventIndex(events, "resume") <= eventIndex(events, "assign_job") || eventIndex(events, "wait_ready") < 0 || eventIndex(events, "close_job") < 0 {
		t.Fatalf("events=%v", events)
	}
	if adapter.setupDeadline < 44*time.Second || adapter.setupDeadline > 45*time.Second {
		t.Fatalf("setup deadline=%v", adapter.setupDeadline)
	}
}

func TestRunRequestsGracefulStopBeforeClosingJob(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	process := &fakeProcess{wait: make(chan error), waitStarted: make(chan struct{})}
	adapter := &fakeAdapter{grants: []LaunchGrant{{Generation: 7, Nonce: "nonce", SessionID: 3}}, processes: []*fakeProcess{process}}
	done := make(chan error, 1)
	go func() { done <- RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Millisecond, 45*time.Second) }()
	<-process.waitStarted
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("bootstrap did not honor graceful deadline")
	}
	events := adapter.Events()
	stop := eventIndex(events, "request_stop")
	close := eventIndex(events, "close_job")
	if stop < 0 || close <= stop {
		t.Fatalf("events=%v", events)
	}
}

func TestRunKillsViewerThatNeverBecomesReadyWithinTotalDeadline(t *testing.T) {
	process := &fakeProcess{wait: make(chan error, 1), closeDoesNotExit: true}
	adapter := &fakeAdapter{
		grants: []LaunchGrant{{Generation: 7, Nonce: "nonce", SessionID: 3}}, processes: []*fakeProcess{process}, readyBlock: true,
	}
	started := time.Now()
	err := RunWithDeadlines(t.Context(), t.TempDir(), adapter, 250*time.Millisecond, 15*time.Millisecond)
	if err == nil || time.Since(started) > 100*time.Millisecond {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(started))
	}
	events := adapter.Events()
	if eventIndex(events, "wait_ready") < 0 || eventIndex(events, "close_job") < 0 {
		t.Fatalf("events=%v", events)
	}
}

func TestRunRelaunchesOnlyAnAuthorizedHigherGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first := &fakeProcess{wait: make(chan error, 1)}
	second := &fakeProcess{wait: make(chan error), waitStarted: make(chan struct{})}
	adapter := &fakeAdapter{
		grants: []LaunchGrant{
			{Generation: 7, Nonce: "nonce-7", SessionID: 3},
			{Generation: 8, Nonce: "nonce-8", SessionID: 3},
		},
		processes: []*fakeProcess{first, second}, authorized: []bool{true}, exitAfterReady: true,
	}
	done := make(chan error, 1)
	go func() { done <- RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Millisecond, 45*time.Second) }()
	<-second.waitStarted
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized next generation did not run")
	}
	if adapter.grantIndex != 2 || adapter.processIndex != 2 {
		t.Fatalf("grants=%d processes=%d events=%v", adapter.grantIndex, adapter.processIndex, adapter.Events())
	}
}

type fakeAdapter struct {
	mu             sync.Mutex
	grants         []LaunchGrant
	grantIndex     int
	processes      []*fakeProcess
	processIndex   int
	authorized     []bool
	authorizeIndex int
	events         []string
	setupDeadline  time.Duration
	readyBlock     bool
	exitAfterReady bool
}

func (adapter *fakeAdapter) record(event string) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.events = append(adapter.events, event)
}

func (adapter *fakeAdapter) Events() []string {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return append([]string(nil), adapter.events...)
}

func (adapter *fakeAdapter) CurrentViewer(context.Context, string) (string, error) {
	adapter.record("load_current")
	return "releases/2/viewer/CamStationViewer.exe", nil
}

func (adapter *fakeAdapter) RequestGrant(ctx context.Context) (LaunchGrant, error) {
	adapter.record("grant")
	if deadline, ok := ctx.Deadline(); ok {
		adapter.setupDeadline = time.Until(deadline)
	}
	if adapter.grantIndex >= len(adapter.grants) {
		return LaunchGrant{}, errors.New("no authorized grant")
	}
	grant := adapter.grants[adapter.grantIndex]
	adapter.grantIndex++
	return grant, nil
}

func (adapter *fakeAdapter) StartSuspended(_ context.Context, _ LaunchSpec) (ManagedProcess, error) {
	adapter.record("start_suspended")
	process := adapter.processes[adapter.processIndex]
	adapter.processIndex++
	process.record = adapter.record
	return process, nil
}

func (adapter *fakeAdapter) WaitReady(ctx context.Context, _ int64) error {
	adapter.record("wait_ready")
	if adapter.readyBlock {
		<-ctx.Done()
		return ctx.Err()
	}
	if adapter.exitAfterReady && adapter.processIndex == 1 {
		process := adapter.processes[adapter.processIndex-1]
		go func() {
			time.Sleep(time.Millisecond)
			process.wait <- nil
		}()
	}
	return nil
}

func (adapter *fakeAdapter) RelaunchAuthorized(context.Context, int64) (bool, error) {
	adapter.record("relaunch_authorized")
	if adapter.authorizeIndex >= len(adapter.authorized) {
		return false, nil
	}
	value := adapter.authorized[adapter.authorizeIndex]
	adapter.authorizeIndex++
	return value, nil
}

type fakeProcess struct {
	record           func(string)
	wait             chan error
	waitStarted      chan struct{}
	closeDoesNotExit bool
}

func (process *fakeProcess) AssignKillOnCloseJob() error {
	process.record("assign_job")
	return nil
}
func (process *fakeProcess) Resume() error {
	process.record("resume")
	return nil
}
func (process *fakeProcess) Wait() error {
	process.record("wait")
	if process.waitStarted != nil {
		close(process.waitStarted)
	}
	return <-process.wait
}
func (process *fakeProcess) RequestStop() error {
	process.record("request_stop")
	return nil
}
func (process *fakeProcess) CloseJob() error {
	process.record("close_job")
	if process.closeDoesNotExit {
		return nil
	}
	select {
	case process.wait <- errors.New("Job closed"):
	default:
	}
	return nil
}

func eventIndex(events []string, value string) int {
	for index, event := range events {
		if event == value {
			return index
		}
	}
	return -1
}
