package main

import (
	"context"
	"time"

	"camstation/internal/store"
)

type liveWarmStore interface {
	ListCameras(context.Context, bool) ([]store.Camera, error)
}

type liveWarmController interface {
	ReconcileLiveWarmers([]store.Camera)
	StopLiveWarmers()
}

func startLiveWarmReconciler(ctx context.Context, db liveWarmStore, controller liveWarmController, interval time.Duration, onError func(error)) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	reconcile := func(reconcileCtx context.Context) error {
		cameras, err := db.ListCameras(reconcileCtx, true)
		if err != nil {
			return err
		}
		controller.ReconcileLiveWarmers(cameras)
		return nil
	}
	if err := reconcile(ctx); err != nil {
		return err
	}
	go func() {
		defer controller.StopLiveWarmers()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pollCtx, cancel := context.WithTimeout(ctx, interval)
				err := reconcile(pollCtx)
				cancel()
				if err != nil && onError != nil {
					onError(err)
				}
			}
		}
	}()
	return nil
}
