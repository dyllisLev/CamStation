package main

import (
	"context"
	"fmt"

	"camstation/internal/store"
)

type startupCameraStore interface {
	ListCameras(context.Context, bool) ([]store.Camera, error)
}

type startupStreamer interface {
	Ensure(context.Context, []store.Camera) error
}

type startupRecorder interface {
	Reconcile([]store.Camera)
}

func startCameraPolicies(ctx context.Context, db startupCameraStore, streamer startupStreamer, applier policyApplier, recorders startupRecorder, recordingEnabled bool) error {
	cameras, err := db.ListCameras(ctx, true)
	if err != nil || len(cameras) == 0 {
		return err
	}
	if shouldBootstrapCameraPolicies(cameras) {
		result := applier.Apply(ctx)
		if !result.Applied {
			return fmt.Errorf("initial camera policy apply failed: %s", result.Error)
		}
	} else if err := streamer.Ensure(ctx, cameras); err != nil {
		return err
	}
	fresh, err := db.ListCameras(ctx, true)
	if err != nil {
		return err
	}
	if recordingEnabled {
		recorders.Reconcile(appliedRecordingCameras(fresh))
	}
	return nil
}

func appliedRecordingCameras(cameras []store.Camera) []store.Camera {
	applied := make([]store.Camera, 0, len(cameras))
	for _, camera := range cameras {
		if camera.PolicyState.AppliedRevision == 0 {
			continue
		}
		for _, output := range camera.Outputs {
			if output.Purpose == store.CameraOutputRecording && output.AppliedPolicy.SourceKey != "" {
				applied = append(applied, camera)
				break
			}
		}
	}
	return applied
}

func shouldBootstrapCameraPolicies(cameras []store.Camera) bool {
	if len(cameras) == 0 {
		return false
	}
	for _, camera := range cameras {
		state := camera.PolicyState
		if state.DesiredRevision <= 0 || state.AppliedRevision != 0 || state.ApplyState != store.CameraApplyPending {
			return false
		}
	}
	return true
}
