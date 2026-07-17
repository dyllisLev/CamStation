package main

import (
	"context"
	"testing"

	"camstation/internal/store"
	"camstation/internal/stream"
)

type startupCameraStoreFake struct {
	reads [][]store.Camera
	calls int
}

func (f *startupCameraStoreFake) ListCameras(context.Context, bool) ([]store.Camera, error) {
	index := f.calls
	f.calls++
	if index >= len(f.reads) {
		index = len(f.reads) - 1
	}
	return f.reads[index], nil
}

type startupStreamerFake struct{ ensures int }

func (f *startupStreamerFake) Ensure(context.Context, []store.Camera) error {
	f.ensures++
	return nil
}

type startupRecorderFake struct{ reconciled []store.Camera }

func (f *startupRecorderFake) Reconcile(cameras []store.Camera) { f.reconciled = cameras }

func TestStartCameraPoliciesBootstrapsAllZeroPendingThroughCoordinatorAndReloads(t *testing.T) {
	pending := store.Camera{ID: 1, Enabled: true, PolicyState: store.CameraPolicyState{DesiredRevision: 1, AppliedRevision: 0, ApplyState: store.CameraApplyPending}}
	applied := pending
	applied.PolicyState.AppliedRevision = 1
	applied.PolicyState.ApplyState = store.CameraApplyApplied
	applied.Outputs = []store.CameraOutput{{Purpose: store.CameraOutputRecording, AppliedPolicy: store.CameraOutputPolicySnapshot{SourceKey: "recording"}}}
	db := &startupCameraStoreFake{reads: [][]store.Camera{{pending}, {applied}}}
	streamer := &startupStreamerFake{}
	recorder := &startupRecorderFake{}
	applyCalls := 0
	applier := policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
		applyCalls++
		return stream.PolicyApplyResult{Applied: true}
	})

	if err := startCameraPolicies(t.Context(), db, streamer, applier, recorder, true); err != nil {
		t.Fatal(err)
	}
	if applyCalls != 1 || streamer.ensures != 0 || db.calls != 2 {
		t.Fatalf("bootstrap calls apply=%d ensure=%d reads=%d", applyCalls, streamer.ensures, db.calls)
	}
	if len(recorder.reconciled) != 1 || recorder.reconciled[0].PolicyState.AppliedRevision != 1 {
		t.Fatalf("recorder used stale cameras: %#v", recorder.reconciled)
	}
}

func TestStartCameraPoliciesDoesNotAutoApplyMixedOrFailedPolicies(t *testing.T) {
	tests := []struct {
		name          string
		cameras       []store.Camera
		wantRecorders int
	}{
		{"mixed", []store.Camera{
			{ID: 1, Enabled: true, PolicyState: store.CameraPolicyState{DesiredRevision: 2, AppliedRevision: 2, ApplyState: store.CameraApplyApplied}, Outputs: []store.CameraOutput{{Purpose: store.CameraOutputRecording, AppliedPolicy: store.CameraOutputPolicySnapshot{SourceKey: "recording"}}}},
			{ID: 2, Enabled: true, PolicyState: store.CameraPolicyState{DesiredRevision: 1, AppliedRevision: 0, ApplyState: store.CameraApplyPending}},
		}, 1},
		{"failed", []store.Camera{{ID: 1, Enabled: true, PolicyState: store.CameraPolicyState{DesiredRevision: 1, AppliedRevision: 0, ApplyState: store.CameraApplyFailed}}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &startupCameraStoreFake{reads: [][]store.Camera{tt.cameras, tt.cameras}}
			streamer := &startupStreamerFake{}
			recorder := &startupRecorderFake{}
			applyCalls := 0
			applier := policyApplyFunc(func(context.Context) stream.PolicyApplyResult {
				applyCalls++
				return stream.PolicyApplyResult{Applied: true}
			})
			if err := startCameraPolicies(t.Context(), db, streamer, applier, recorder, true); err != nil {
				t.Fatal(err)
			}
			if applyCalls != 0 || streamer.ensures != 1 || db.calls != 2 {
				t.Fatalf("calls apply=%d ensure=%d reads=%d", applyCalls, streamer.ensures, db.calls)
			}
			if len(recorder.reconciled) != tt.wantRecorders {
				t.Fatalf("recorder cameras = %#v, want %d", recorder.reconciled, tt.wantRecorders)
			}
		})
	}
}
