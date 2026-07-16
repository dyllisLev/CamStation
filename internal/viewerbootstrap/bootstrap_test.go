package viewerbootstrap

import (
	"context"
	"path/filepath"
	"reflect"
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

func TestGenerationGateAcceptsOnlyOneLaunch(t *testing.T) {
	var gate GenerationGate
	if !gate.Accept(7) || gate.Accept(7) || gate.Accept(8) || gate.Accept(0) {
		t.Fatal("gate accepted more than one launch")
	}
}

func TestBootstrapDeadlinesRemainFiveAndFortyFiveSeconds(t *testing.T) {
	if GracefulShutdownDeadline != 5*time.Second || TotalRecoveryDeadline != 45*time.Second {
		t.Fatalf("graceful=%v total=%v", GracefulShutdownDeadline, TotalRecoveryDeadline)
	}
}

func TestRunAssignsKillJobBeforeResume(t *testing.T) {
	process := &fakeProcess{wait: make(chan error, 1)}
	process.wait <- nil
	adapter := &fakeAdapter{grant: LaunchGrant{Generation: 7, Nonce: "nonce", SessionID: 3}, process: process}
	if err := RunWithDeadlines(t.Context(), t.TempDir(), adapter, 5*time.Second, 45*time.Second); err != nil {
		t.Fatal(err)
	}
	want := []string{"load_current", "grant", "start_suspended", "assign_job", "resume", "wait", "close_job"}
	if !reflect.DeepEqual(adapter.events, want) {
		t.Fatalf("events=%v want=%v", adapter.events, want)
	}
	if adapter.setupDeadline < 44*time.Second || adapter.setupDeadline > 45*time.Second {
		t.Fatalf("setup deadline=%v", adapter.setupDeadline)
	}
}

func TestRunRequestsGracefulStopBeforeClosingJob(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	process := &fakeProcess{wait: make(chan error), waitStarted: make(chan struct{})}
	adapter := &fakeAdapter{grant: LaunchGrant{Generation: 7, Nonce: "nonce", SessionID: 3}, process: process}
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
	stop := eventIndex(adapter.events, "request_stop")
	close := eventIndex(adapter.events, "close_job")
	if stop < 0 || close <= stop {
		t.Fatalf("events=%v", adapter.events)
	}
}

type fakeAdapter struct {
	grant         LaunchGrant
	process       *fakeProcess
	events        []string
	setupDeadline time.Duration
}

func (adapter *fakeAdapter) CurrentViewer(context.Context, string) (string, error) {
	adapter.events = append(adapter.events, "load_current")
	return "releases/2/viewer/CamStationViewer.exe", nil
}

func (adapter *fakeAdapter) RequestGrant(ctx context.Context) (LaunchGrant, error) {
	adapter.events = append(adapter.events, "grant")
	if deadline, ok := ctx.Deadline(); ok {
		adapter.setupDeadline = time.Until(deadline)
	}
	return adapter.grant, nil
}

func (adapter *fakeAdapter) StartSuspended(_ context.Context, _ LaunchSpec) (ManagedProcess, error) {
	adapter.events = append(adapter.events, "start_suspended")
	adapter.process.events = &adapter.events
	return adapter.process, nil
}

type fakeProcess struct {
	events      *[]string
	wait        chan error
	waitStarted chan struct{}
}

func (process *fakeProcess) AssignKillOnCloseJob() error {
	*process.events = append(*process.events, "assign_job")
	return nil
}
func (process *fakeProcess) Resume() error {
	*process.events = append(*process.events, "resume")
	return nil
}
func (process *fakeProcess) Wait() error {
	*process.events = append(*process.events, "wait")
	if process.waitStarted != nil {
		close(process.waitStarted)
	}
	return <-process.wait
}
func (process *fakeProcess) RequestStop() error {
	*process.events = append(*process.events, "request_stop")
	return nil
}
func (process *fakeProcess) CloseJob() error {
	*process.events = append(*process.events, "close_job")
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
