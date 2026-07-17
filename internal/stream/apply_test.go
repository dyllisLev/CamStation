package stream

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"camstation/internal/store"
)

func TestApplyConfigAtomicallyRestoresLastGoodAfterFailedHealthCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	if err := os.WriteFile(path, []byte("old-good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := NewGo2RTC(path)
	restarts := 0
	err := g.applyConfig(t.Context(), []byte("new-bad\n"), func(context.Context) error {
		restarts++
		if restarts == 1 {
			return errors.New("health check failed")
		}
		return nil
	})
	if err == nil || restarts != 2 {
		t.Fatalf("err = %v, restarts = %d", err, restarts)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "old-good\n" {
		t.Fatalf("config = %q, err = %v", got, readErr)
	}
	lastGood, readErr := os.ReadFile(path + ".last-good")
	if readErr != nil || string(lastGood) != "old-good\n" {
		t.Fatalf("last good = %q, err = %v", lastGood, readErr)
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v", info.Mode().Perm(), statErr)
	}
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".go2rtc.yaml-*"))
	if len(matches) != 0 {
		t.Fatalf("staging files remain: %v", matches)
	}
}

func TestPrepareConfigRemovesFailedFirstConfigWhenNoRollbackExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	g := NewGo2RTC(path)
	_, err := g.prepareConfig(t.Context(), []byte("bad-first\n"), func(context.Context) error {
		return errors.New("unhealthy")
	})
	if err == nil {
		t.Fatal("expected health failure")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed first config remained: %v", statErr)
	}
}

func TestApplyCoordinatorContinuesWhenNewerRevisionIsSaved(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 1, AppliedRevision: 0}
	db := &fakePolicyStore{camera: camera}
	runtime := &fakePolicyRuntime{}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: 1, StreamName: "old-recording"}}}
	db.onApplied = func(revision int64) {
		if revision != 1 {
			return
		}
		db.camera.PolicyState.DesiredRevision = 2
		db.camera.Outputs[1].VideoMode = store.CameraVideoH264
	}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if result.Error != "" || !result.Applied {
		t.Fatalf("result = %+v", result)
	}
	if got := db.appliedRevisions; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("applied revisions = %v, want [1 2]", got)
	}
	if len(runtime.configs) != 2 {
		t.Fatalf("runtime apply count = %d, want 2", len(runtime.configs))
	}
	if recorder.suspends != 2 || recorder.restores != 2 || len(recorder.active) != 1 || recorder.active[0].PolicyState.AppliedRevision != 2 {
		t.Fatalf("recorder handoff = %+v", recorder)
	}
}

func TestApplyCoordinatorExpectedRevisionCheckRunsInsideApplyLock(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 2, AppliedRevision: 2}
	db := &fakePolicyStore{camera: camera}
	runtime := &fakePolicyRuntime{}
	coordinator := NewApplyCoordinator(db, runtime, &fakeRecorderHandoff{})

	coordinator.mu <- struct{}{}
	resultCh := make(chan PolicyApplyResult, 1)
	go func() { resultCh <- coordinator.ApplyExpected(t.Context(), camera.ID, 2) }()
	db.mu.Lock()
	db.camera.PolicyState.DesiredRevision = 3
	db.mu.Unlock()
	<-coordinator.mu
	result := <-resultCh
	if !result.RevisionConflict || result.Applied || len(runtime.configs) != 0 {
		t.Fatalf("stale expected apply result=%+v runtime=%+v", result, runtime)
	}
}

func TestApplyCoordinatorRestoresActiveRecordersAndMarksFailedOnRuntimeFailure(t *testing.T) {
	camera, output := policyFixture("hevc", "yuv420p", 8, 3840, 2160, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 3, AppliedRevision: 2}
	db := &fakePolicyStore{camera: camera}
	runtime := &fakePolicyRuntime{err: errors.New("go2rtc unhealthy")}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: 1, StreamName: "recording"}}}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if result.Applied || !strings.Contains(result.Error, "go2rtc unhealthy") {
		t.Fatalf("result = %+v", result)
	}
	if db.failedRevision != 3 || recorder.suspends != 1 || recorder.restores != 1 || len(recorder.active) != 1 {
		t.Fatalf("db/recorder state = failed:%d recorder:%+v", db.failedRevision, recorder)
	}
}

func TestApplyCoordinatorRollsBackRuntimeDBAndRecordersWhenBulkCommitFails(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 4, AppliedRevision: 3}
	db := &fakePolicyStore{camera: camera, bulkErr: errors.New("bulk DB commit failed")}
	runtime := &fakePolicyRuntime{}
	old := store.Camera{ID: 1, StreamName: "old-recording", PolicyState: store.CameraPolicyState{AppliedRevision: 3}}
	recorder := &fakeRecorderHandoff{active: []store.Camera{old}}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if result.Applied || !strings.Contains(result.Error, "bulk DB commit failed") {
		t.Fatalf("result = %+v", result)
	}
	if len(db.appliedRevisions) != 0 || runtime.commits != 0 || runtime.rollbacks != 1 {
		t.Fatalf("db/runtime advanced: db=%v runtime=%+v", db.appliedRevisions, runtime)
	}
	if len(recorder.active) != 1 || recorder.active[0].StreamName != "old-recording" || recorder.active[0].PolicyState.AppliedRevision != 3 {
		t.Fatalf("old active recorder not restored: %+v", recorder.active)
	}
}

func TestApplyCoordinatorReportsRecoveryFailureWhenRuntimeRollbackFails(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 4, AppliedRevision: 3}
	db := &fakePolicyStore{camera: camera, bulkErr: errors.New("bulk DB commit failed")}
	runtime := &fakePolicyRuntime{rollbackErr: errors.New("rollback restart failed")}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: 1, PolicyState: store.CameraPolicyState{AppliedRevision: 3}}}}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if !result.RecoveryFailed || !strings.Contains(result.Error, "rollback restart failed") {
		t.Fatalf("result = %+v", result)
	}
}

func TestPrepareConfigMarksFailedPreviousProcessRestoreAsRecoveryFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGo2RTC(path)
	restarts := 0
	_, err := g.prepareConfig(t.Context(), []byte("new\n"), func(context.Context) error {
		restarts++
		if restarts == 1 {
			return errors.New("new process failed")
		}
		return errors.New("old process restore failed")
	})
	if err == nil || !runtimeRecoveryFailed(err) {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestApplyCoordinatorRestoresFreshRecordersWhenLastGoodCommitFails(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 5, AppliedRevision: 4}
	db := &fakePolicyStore{camera: camera}
	runtime := &fakePolicyRuntime{commitErr: &lastGoodCommitError{err: errors.New("last-good write failed"), invariantSafe: true}}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: 1, StreamName: "old-recording"}}}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if !result.Applied || !strings.Contains(result.Error, "last-good write failed") {
		t.Fatalf("result = %+v", result)
	}
	if recorder.restores != 1 || len(recorder.active) != 1 || recorder.active[0].PolicyState.AppliedRevision != 5 {
		t.Fatalf("fresh active recorder not restored: %+v", recorder)
	}
}

func TestApplyCoordinatorDoesNotReportAppliedWhenLastGoodInvariantIsUnsafe(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 10, AppliedRevision: 9}
	db := &fakePolicyStore{camera: camera}
	runtime := &fakePolicyRuntime{commitErr: errors.New("last-good invariant unknown")}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: 1, StreamName: "old-recording"}}}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if result.Applied || !strings.Contains(result.Error, "invariant unknown") {
		t.Fatalf("result = %+v", result)
	}
	if len(recorder.active) != 1 || recorder.active[0].PolicyState.AppliedRevision != 10 {
		t.Fatalf("DB-aligned recorder stopped: %+v", recorder.active)
	}
}

func TestLastGoodFinalizeFailureInvalidatesStaleConfigAndStartupUsesCurrentApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	if err := os.WriteFile(path, []byte("revision-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".last-good", []byte("revision-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := NewGo2RTC(path)
	tx, err := g.prepareConfig(t.Context(), []byte("revision-2\n"), func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".last-good"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".last-good", 0o700); err != nil {
		t.Fatal(err)
	}
	err = tx.Commit()
	if err == nil || !lastGoodInvariantPreserved(err) {
		t.Fatalf("commit error = %v, want safe invalidation warning", err)
	}
	camera := store.Camera{Enabled: true, PolicyState: store.CameraPolicyState{
		DesiredRevision: 3, AppliedRevision: 2, ApplyState: store.CameraApplyPending,
	}}
	config, preserve, err := g.startupConfig([]store.Camera{camera})
	if err != nil {
		t.Fatal(err)
	}
	if !preserve || string(config) != "revision-2\n" {
		t.Fatalf("startup selected stale revision: config=%q preserve=%v", config, preserve)
	}
}

func TestApplyCoordinatorAppliesEmptyConfigAfterFinalCameraDeletion(t *testing.T) {
	db := &fakePolicyStore{empty: true}
	runtime := &fakePolicyRuntime{}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: 99, StreamName: "deleted"}}}
	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if !result.Applied || result.Error != "" || runtime.commits != 1 {
		t.Fatalf("result/runtime = %+v %+v", result, runtime)
	}
	if len(recorder.active) != 0 {
		t.Fatalf("deleted recorder restored: %+v", recorder.active)
	}
}

func TestApplyCoordinatorStopsRecorderForDisabledCamera(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Enabled = false
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: camera.ID, DesiredRevision: 1, AppliedRevision: 1, ApplyState: store.CameraApplyApplied}
	db := &fakePolicyStore{camera: camera}
	runtime := &fakePolicyRuntime{}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: camera.ID, Enabled: true, StreamName: "camera-recording"}}}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if !result.Applied || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
	if len(recorder.active) != 0 {
		t.Fatalf("disabled recorder restored: %+v", recorder.active)
	}
	if len(runtime.configs) != 1 || strings.Contains(string(runtime.configs[0]), "camera-recording") {
		t.Fatalf("disabled runtime config = %q", runtime.configs)
	}
}

func TestNewerRevisionExistsDetectsEnabledCameraRemoval(t *testing.T) {
	before := []store.Camera{{ID: 1, Enabled: true, PolicyState: store.CameraPolicyState{DesiredRevision: 3}}}
	if !newerRevisionExists(before, nil) {
		t.Fatal("enabled camera removal was not detected")
	}
}

func TestApplyCoordinatorKeepsNewRecorderRunningWhenFreshRevisionReadFails(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 6, AppliedRevision: 5}
	db := &fakePolicyStore{camera: camera, listErrAfter: 1}
	runtime := &fakePolicyRuntime{}
	recorder := &fakeRecorderHandoff{active: []store.Camera{{ID: 1, StreamName: "old-recording"}}}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if !result.Applied || !strings.Contains(result.Error, "fresh list failed") {
		t.Fatalf("result = %+v", result)
	}
	if len(recorder.active) != 1 || recorder.active[0].PolicyState.AppliedRevision != 6 {
		t.Fatalf("new applied recorder not running: %+v", recorder.active)
	}
}

func TestApplyCoordinatorRollsBackBeforeDBCommitWhenNewRecorderRestorePartiallyFails(t *testing.T) {
	camera, output := policyFixture("h264", "yuv420p", 8, 1920, 1080, 20)
	camera.Outputs = threeOutputs(output)
	camera.PolicyState = store.CameraPolicyState{CameraID: 1, DesiredRevision: 8, AppliedRevision: 7}
	db := &fakePolicyStore{camera: camera}
	runtime := &fakePolicyRuntime{}
	old := store.Camera{ID: 1, StreamName: "old-recording", PolicyState: store.CameraPolicyState{AppliedRevision: 7}}
	recorder := &fakeRecorderHandoff{active: []store.Camera{old}, failRestoreOnce: true}

	result := NewApplyCoordinator(db, runtime, recorder).Apply(t.Context())
	if result.Applied || !strings.Contains(result.Error, "restore failed") {
		t.Fatalf("result = %+v", result)
	}
	if len(db.appliedRevisions) != 0 || runtime.rollbacks != 1 {
		t.Fatalf("DB/runtime advanced: revisions=%v runtime=%+v", db.appliedRevisions, runtime)
	}
	if recorder.suspends != 2 || recorder.restores != 2 || len(recorder.active) != 1 || recorder.active[0].PolicyState.AppliedRevision != 7 {
		t.Fatalf("old recorder set not recovered: %+v", recorder)
	}
}

func threeOutputs(base store.CameraOutput) []store.CameraOutput {
	result := make([]store.CameraOutput, 0, 3)
	for _, purpose := range []store.CameraOutputPurpose{store.CameraOutputRecording, store.CameraOutputLive, store.CameraOutputFocus} {
		item := base
		item.Purpose = purpose
		item.StreamName = "camera-" + string(purpose)
		result = append(result, item)
	}
	return result
}

type fakePolicyStore struct {
	mu               sync.Mutex
	camera           store.Camera
	appliedRevisions []int64
	failedRevision   int64
	onApplied        func(int64)
	bulkErr          error
	empty            bool
	listCalls        int
	listErrAfter     int
}

func (f *fakePolicyStore) ListCameras(context.Context, bool) ([]store.Camera, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.listErrAfter > 0 && f.listCalls > f.listErrAfter {
		return nil, errors.New("fresh list failed")
	}
	if f.empty {
		return nil, nil
	}
	return []store.Camera{f.camera}, nil
}

func (f *fakePolicyStore) MarkCameraPolicyApplied(_ context.Context, _ int64, revision int64, results []store.CameraOutputApplyResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appliedRevisions = append(f.appliedRevisions, revision)
	f.camera.PolicyState.AppliedRevision = revision
	for i := range f.camera.Outputs {
		f.camera.Outputs[i].AppliedPolicy = results[i].Policy
	}
	if f.onApplied != nil {
		f.onApplied(revision)
	}
	return nil
}

func (f *fakePolicyStore) MarkCameraPoliciesApplied(ctx context.Context, snapshots []store.CameraPolicyApplySnapshot) error {
	if f.bulkErr != nil {
		return f.bulkErr
	}
	for _, snapshot := range snapshots {
		if err := f.MarkCameraPolicyApplied(ctx, snapshot.CameraID, snapshot.Revision, snapshot.Results); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakePolicyStore) MarkCameraPolicyFailed(_ context.Context, _ int64, revision int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failedRevision = revision
	return nil
}

type fakePolicyRuntime struct {
	configs     [][]byte
	err         error
	commits     int
	rollbacks   int
	commitErr   error
	rollbackErr error
}

func (f *fakePolicyRuntime) PrepareConfig(_ context.Context, config []byte) (runtimeConfigTransaction, error) {
	f.configs = append(f.configs, append([]byte(nil), config...))
	if f.err != nil {
		return nil, f.err
	}
	return &fakeRuntimeTransaction{runtime: f}, nil
}

type fakeRuntimeTransaction struct{ runtime *fakePolicyRuntime }

func (f *fakeRuntimeTransaction) Commit() error {
	f.runtime.commits++
	return f.runtime.commitErr
}

func (f *fakeRuntimeTransaction) Rollback(context.Context) error {
	f.runtime.rollbacks++
	return f.runtime.rollbackErr
}

type fakeRecorderHandoff struct {
	active             []store.Camera
	suspends, restores int
	failRestoreOnce    bool
}

func (f *fakeRecorderHandoff) SuspendActive() []store.Camera {
	f.suspends++
	active := f.active
	f.active = nil
	return active
}

func (f *fakeRecorderHandoff) RestoreActive(cameras []store.Camera) error {
	f.restores++
	f.active = append([]store.Camera(nil), cameras...)
	if f.failRestoreOnce {
		f.failRestoreOnce = false
		return errors.New("restore failed")
	}
	return nil
}
