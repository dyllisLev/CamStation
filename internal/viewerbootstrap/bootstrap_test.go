package viewerbootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"camstation/internal/vieweragent"
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
	ctx, cancel := context.WithCancel(t.Context())
	process := &fakeProcess{wait: make(chan error, 1)}
	adapter := &fakeAdapter{grants: []LaunchGrant{{Generation: 7, Nonce: "nonce", SessionID: 3}}, processes: []*fakeProcess{process}, exitAfterReady: true}
	done := make(chan error, 1)
	go func() { done <- RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Second, 45*time.Second) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && eventIndex(adapter.Events(), "dispose") < 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	events := adapter.Events()
	if eventIndex(events, "assign_job") < 0 || eventIndex(events, "resume") <= eventIndex(events, "assign_job") || eventIndex(events, "wait_ready") < 0 || eventIndex(events, "terminate_job") < 0 || eventIndex(events, "dispose") < 0 {
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
	close := eventIndex(events, "terminate_job")
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
	if eventIndex(events, "wait_ready") < 0 || eventIndex(events, "terminate_job") < 0 {
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

func TestRunningBootstrapReceivesAuthorizedRelaunchGeneration(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first := &fakeProcess{wait: make(chan error), waitStarted: make(chan struct{})}
	second := &fakeProcess{wait: make(chan error), waitStarted: make(chan struct{})}
	adapter := &fakeAdapter{
		grants: []LaunchGrant{
			{Generation: 7, Nonce: "nonce-7", SessionID: 3},
			{Generation: 8, Nonce: "nonce-8", SessionID: 3},
		},
		processes: []*fakeProcess{first, second}, authorized: []bool{true},
	}
	done := make(chan error, 1)
	go func() { done <- RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Millisecond, time.Second) }()
	select {
	case <-second.waitStarted:
		cancel()
	case <-time.After(200 * time.Millisecond):
		cancel()
		<-done
		t.Fatalf("authorized generation did not reach the running bootstrap: events=%v", adapter.Events())
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	events := adapter.Events()
	if eventIndex(events, "request_stop") < 0 || eventIndex(events, "terminate_job") < eventIndex(events, "request_stop") {
		t.Fatalf("bootstrap did not own graceful/forced recovery: %v", events)
	}
}

func TestRecoveryFailedMonitorObservesFutureForcedRestart(t *testing.T) {
	for _, test := range []struct {
		name           string
		exitAfterReady bool
		authorized     []bool
	}{
		{name: "running child", authorized: []bool{false, true}},
		{name: "exited child", exitAfterReady: true, authorized: []bool{false, false, true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			first := &fakeProcess{wait: make(chan error, 1)}
			second := &fakeProcess{wait: make(chan error), waitStarted: make(chan struct{})}
			adapter := &fakeAdapter{
				grants: []LaunchGrant{
					{Generation: 7, Nonce: "nonce-7", SessionID: 3},
					{Generation: 8, Nonce: "nonce-8", SessionID: 3},
				},
				processes: []*fakeProcess{first, second}, authorized: test.authorized, exitAfterReady: test.exitAfterReady,
			}
			done := make(chan error, 1)
			go func() { done <- RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Millisecond, 10*time.Millisecond) }()
			select {
			case <-second.waitStarted:
				cancel()
			case err := <-done:
				cancel()
				t.Fatalf("bootstrap stopped before forced generation: err=%v events=%v", err, adapter.Events())
			case <-time.After(500 * time.Millisecond):
				cancel()
				<-done
				t.Fatalf("future forced generation was not observed: events=%v", adapter.Events())
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if adapter.AuthorizationCalls() < 2 {
				t.Fatalf("authorization monitor was disabled: calls=%d events=%v", adapter.AuthorizationCalls(), adapter.Events())
			}
		})
	}
}

func TestRecoveryFailedMonitorDoesNotHotLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	process := &fakeProcess{wait: make(chan error)}
	adapter := &fakeAdapter{
		grants:    []LaunchGrant{{Generation: 7, Nonce: "nonce-7", SessionID: 3}},
		processes: []*fakeProcess{process},
	}
	done := make(chan error, 1)
	go func() { done <- RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Millisecond, 10*time.Millisecond) }()
	time.Sleep(45 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	calls := adapter.AuthorizationCalls()
	if calls < 2 || calls > 6 {
		t.Fatalf("authorization probes hot-looped or stopped: calls=%d events=%v", calls, adapter.Events())
	}
}

func TestRecoveryWindowIncludesDelayedAuthorizationAndNextRendererReadiness(t *testing.T) {
	first := &fakeProcess{wait: make(chan error, 1)}
	second := &fakeProcess{wait: make(chan error, 1)}
	authorization := make(chan struct{})
	adapter := &fakeAdapter{
		grants: []LaunchGrant{
			{Generation: 7, Nonce: "nonce-7", SessionID: 3},
			{Generation: 8, Nonce: "nonce-8", SessionID: 3},
		},
		processes: []*fakeProcess{first, second}, authorization: authorization,
		exitAfterReady: true, blockReadyAt: 2,
	}
	go func() {
		time.Sleep(70 * time.Millisecond)
		close(authorization)
	}()
	err := RunWithDeadlines(t.Context(), t.TempDir(), adapter, 5*time.Millisecond, 100*time.Millisecond)
	if err == nil {
		t.Fatal("missing second-generation renderer readiness succeeded")
	}
	deadlines := adapter.SetupDeadlines()
	if len(deadlines) != 2 || deadlines[1] >= 60*time.Millisecond {
		t.Fatalf("setup deadlines=%v; authorization time was not charged to the recovery window", deadlines)
	}
}

func TestExitedViewerWaitsForFutureAuthorizationUntilCanceled(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	process := &fakeProcess{wait: make(chan error, 1)}
	adapter := &fakeAdapter{
		grants:    []LaunchGrant{{Generation: 7, Nonce: "nonce-7", SessionID: 3}},
		processes: []*fakeProcess{process}, authorizationNever: true, exitAfterReady: true,
	}
	started := time.Now()
	err := RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Millisecond, 15*time.Millisecond)
	if err != nil || time.Since(started) < 150*time.Millisecond {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(started))
	}
}

func TestAgentRestartConvergenceDrivesBootstrapToReadyNextGeneration(t *testing.T) {
	dir := t.TempDir()
	paths := vieweragent.MachinePaths{
		State: filepath.Join(dir, "state.json"), Commands: filepath.Join(dir, "commands.json"), Update: filepath.Join(dir, "update.json"),
	}
	if err := vieweragent.SaveMachineState(paths.State, vieweragent.MachineState{
		ViewerGeneration: 7, ExpectedViewerGeneration: 7, ViewerState: "running", RendererState: "ready",
		ViewerNonce: "old", ExpectedViewerPID: 99, ExpectedViewerSession: 3,
	}); err != nil {
		t.Fatal(err)
	}
	authorization := make(chan struct{})
	secondReady := make(chan struct{})
	first := &fakeProcess{wait: make(chan error, 1)}
	second := &fakeProcess{wait: make(chan error), waitStarted: make(chan struct{})}
	adapter := &fakeAdapter{
		grants: []LaunchGrant{
			{Generation: 7, Nonce: "old", SessionID: 3},
			{Generation: 8, Nonce: "new", SessionID: 3},
		},
		processes: []*fakeProcess{first, second}, authorization: authorization, exitAfterReady: true,
		readyHook: func(generation int64) {
			if generation != 8 {
				return
			}
			state, err := vieweragent.LoadMachineState(paths.State)
			if err != nil {
				return
			}
			state.ViewerGeneration = 8
			state.ExpectedViewerGeneration = 8
			state.ViewerState = "running"
			state.RendererState = "ready"
			if vieweragent.SaveMachineState(paths.State, state) == nil {
				close(secondReady)
			}
		},
	}
	agentResult := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && eventIndex(adapter.Events(), "relaunch_authorized") < 0 {
			time.Sleep(time.Millisecond)
		}
		agentCtx, cancelAgent := context.WithCancel(t.Context())
		agent := vieweragent.NewAgent(vieweragent.Config{
			ClientID: "recovery-client", ServerURL: "http://127.0.0.1:1", DisplayName: "Viewer", InstallDir: dir,
		}, paths)
		agent.ServePipe = func(ctx context.Context, _ vieweragent.Config, _ func(vieweragent.PipeMessage) (vieweragent.PipeMessage, error), ready func()) error {
			ready()
			<-ctx.Done()
			return nil
		}
		agent.Ready = cancelAgent
		if err := agent.Run(agentCtx); err != nil {
			agentResult <- err
			return
		}
		state, err := vieweragent.LoadMachineState(paths.State)
		if err != nil || state.ViewerState != "restart_authorized" || state.ExpectedViewerGeneration != 8 ||
			state.ViewerNonce != "" || state.ExpectedViewerPID != 0 {
			agentResult <- fmt.Errorf("startup state=%+v err=%v", state, err)
			return
		}
		close(authorization)
		agentResult <- nil
	}()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- RunWithDeadlines(ctx, dir, adapter, 5*time.Millisecond, 5*time.Second) }()
	select {
	case <-secondReady:
		cancel()
	case err := <-done:
		cancel()
		var startupErr error
		select {
		case startupErr = <-agentResult:
		default:
		}
		state, stateErr := vieweragent.LoadMachineState(paths.State)
		t.Fatalf("bootstrap stopped before next generation ready: %v agent=%v state=%+v stateErr=%v events=%v deadlines=%v",
			err, startupErr, state, stateErr, adapter.Events(), adapter.SetupDeadlines())
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("next generation did not become ready")
	}
	if err := <-agentResult; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	state, err := vieweragent.LoadMachineState(paths.State)
	if err != nil || state.ViewerGeneration != 8 || state.ViewerState != "running" || state.RendererState != "ready" {
		t.Fatalf("final state=%+v err=%v", state, err)
	}
}

func TestNormalExitTerminatesJobAfterWaitAndDisposesHandlesLast(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	process := &fakeProcess{wait: make(chan error, 1)}
	adapter := &fakeAdapter{
		grants:    []LaunchGrant{{Generation: 7, Nonce: "nonce", SessionID: 3}},
		processes: []*fakeProcess{process}, exitAfterReady: true,
	}
	done := make(chan error, 1)
	go func() { done <- RunWithDeadlines(ctx, t.TempDir(), adapter, 5*time.Millisecond, time.Second) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && eventIndex(adapter.Events(), "dispose") < 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	events := adapter.Events()
	waitDone := eventIndex(events, "wait_done")
	terminate := eventIndex(events, "terminate_job")
	dispose := eventIndex(events, "dispose")
	if waitDone < 0 || terminate <= waitDone || dispose <= terminate {
		t.Fatalf("events=%v", events)
	}
}

func TestTimeoutTerminatesJobThenDisposesOnlyAfterWaitCompletes(t *testing.T) {
	process := &fakeProcess{wait: make(chan error, 1), closeDoesNotExit: true}
	adapter := &fakeAdapter{
		grants:    []LaunchGrant{{Generation: 7, Nonce: "nonce", SessionID: 3}},
		processes: []*fakeProcess{process}, readyBlock: true,
	}
	started := time.Now()
	if err := RunWithDeadlines(t.Context(), t.TempDir(), adapter, 5*time.Millisecond, 15*time.Millisecond); err == nil {
		t.Fatal("never-ready Viewer succeeded")
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatalf("cleanup exceeded bounded recovery: %v", time.Since(started))
	}
	events := adapter.Events()
	if eventIndex(events, "request_stop") < 0 || eventIndex(events, "terminate_job") <= eventIndex(events, "request_stop") {
		t.Fatalf("graceful/Job termination order=%v", events)
	}
	if eventIndex(events, "dispose") >= 0 {
		t.Fatalf("process handle disposed while Wait was pending: %v", events)
	}
	process.wait <- nil
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && eventIndex(adapter.Events(), "dispose") < 0 {
		time.Sleep(time.Millisecond)
	}
	events = adapter.Events()
	if eventIndex(events, "wait_done") < 0 || eventIndex(events, "dispose") <= eventIndex(events, "wait_done") {
		t.Fatalf("deferred handle disposal order=%v", events)
	}
}

type fakeAdapter struct {
	mu                 sync.Mutex
	grants             []LaunchGrant
	grantIndex         int
	processes          []*fakeProcess
	processIndex       int
	authorized         []bool
	authorizeIndex     int
	authorizationCalls int
	events             []string
	setupDeadline      time.Duration
	setupDeadlines     []time.Duration
	readyBlock         bool
	exitAfterReady     bool
	blockReadyAt       int
	authorization      <-chan struct{}
	authorizationNever bool
	readyHook          func(int64)
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

func (adapter *fakeAdapter) SetupDeadlines() []time.Duration {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return append([]time.Duration(nil), adapter.setupDeadlines...)
}

func (adapter *fakeAdapter) CurrentViewer(context.Context, string) (string, error) {
	adapter.record("load_current")
	return "releases/2/viewer/CamStationViewer.exe", nil
}

func (adapter *fakeAdapter) RequestGrant(ctx context.Context) (LaunchGrant, error) {
	adapter.record("grant")
	if deadline, ok := ctx.Deadline(); ok {
		adapter.setupDeadline = time.Until(deadline)
		adapter.setupDeadlines = append(adapter.setupDeadlines, adapter.setupDeadline)
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

func (adapter *fakeAdapter) WaitReady(ctx context.Context, generation int64) error {
	adapter.record("wait_ready")
	if adapter.readyHook != nil {
		adapter.readyHook(generation)
	}
	if adapter.readyBlock || adapter.blockReadyAt == adapter.processIndex {
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

func (adapter *fakeAdapter) RelaunchAuthorized(ctx context.Context, _ int64) (bool, error) {
	adapter.record("relaunch_authorized")
	adapter.mu.Lock()
	adapter.authorizationCalls++
	adapter.mu.Unlock()
	if adapter.authorizationNever {
		<-ctx.Done()
		return false, ctx.Err()
	}
	if adapter.authorization != nil {
		select {
		case <-adapter.authorization:
			return true, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	if adapter.authorizeIndex >= len(adapter.authorized) {
		return false, nil
	}
	value := adapter.authorized[adapter.authorizeIndex]
	adapter.authorizeIndex++
	return value, nil
}

func (adapter *fakeAdapter) AuthorizationCalls() int {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return adapter.authorizationCalls
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
	err := <-process.wait
	process.record("wait_done")
	return err
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

func (process *fakeProcess) TerminateJob() error {
	process.record("terminate_job")
	if process.closeDoesNotExit {
		return nil
	}
	select {
	case process.wait <- errors.New("Job terminated"):
	default:
	}
	return nil
}

func (process *fakeProcess) Dispose() error {
	process.record("dispose")
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
