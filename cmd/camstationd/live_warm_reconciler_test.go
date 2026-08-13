package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"camstation/internal/store"
)

type liveWarmStoreFake struct {
	mu      sync.Mutex
	cameras []store.Camera
}

func (f *liveWarmStoreFake) ListCameras(context.Context, bool) ([]store.Camera, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Camera(nil), f.cameras...), nil
}

func (f *liveWarmStoreFake) set(cameras []store.Camera) {
	f.mu.Lock()
	f.cameras = append([]store.Camera(nil), cameras...)
	f.mu.Unlock()
}

type liveWarmControllerFake struct {
	reconciled chan []store.Camera
	stopped    chan struct{}
}

func (f *liveWarmControllerFake) ReconcileLiveWarmers(cameras []store.Camera) {
	f.reconciled <- append([]store.Camera(nil), cameras...)
}

func (f *liveWarmControllerFake) StopLiveWarmers() { close(f.stopped) }

func TestStartLiveWarmReconcilerTracksDBAndStopsWithDaemon(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first := store.Camera{ID: 1, Enabled: true}
	second := store.Camera{ID: 2, Enabled: true}
	db := &liveWarmStoreFake{cameras: []store.Camera{first}}
	controller := &liveWarmControllerFake{reconciled: make(chan []store.Camera, 8), stopped: make(chan struct{})}

	if err := startLiveWarmReconciler(ctx, db, controller, 5*time.Millisecond, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case cameras := <-controller.reconciled:
		if len(cameras) != 1 || cameras[0].ID != first.ID {
			t.Fatalf("initial cameras = %#v", cameras)
		}
	case <-time.After(time.Second):
		t.Fatal("initial live warm reconcile did not run")
	}

	db.set([]store.Camera{first, second})
	deadline := time.After(time.Second)
	for {
		select {
		case cameras := <-controller.reconciled:
			if len(cameras) == 2 {
				cancel()
				select {
				case <-controller.stopped:
					return
				case <-time.After(time.Second):
					t.Fatal("live warm workers were not stopped")
				}
			}
		case <-deadline:
			t.Fatal("updated camera set was not reconciled")
		}
	}
}
