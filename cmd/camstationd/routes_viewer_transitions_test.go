package main

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"camstation/internal/store"
)

func TestViewerHeartbeatPersistsOnlyFirstObservationAndStateChanges(t *testing.T) {
	server := newTestRouteServer(t)
	healthy := `{
		"id":"monitoring-pc","displayName":"Monitoring PC","appVersion":"2.0.25",
		"hostname":"monitoring-pc","deviceLabel":"control-room","route":"/live","mode":"grid",
		"agent":{"state":"online","version":"2.0.25"},"control":{"state":"online"},
		"viewer":{"state":"running","version":"2.0.25"},"renderer":{"state":"ready"},
		"streams":[{"streamName":"gate-live","state":"playing","transport":"webrtc"}]
	}`
	for index := 0; index < 2; index++ {
		status, body := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", healthy)
		if status != http.StatusOK {
			t.Fatalf("healthy heartbeat[%d]=%d body=%+v", index, status, body)
		}
	}
	assertViewerEvents(t, server.db, []expectedViewerEvent{{message: "viewer connected", level: "info"}})

	degraded := `{
		"id":"monitoring-pc","displayName":"Monitoring PC","appVersion":"2.0.25",
		"hostname":"monitoring-pc","deviceLabel":"control-room","route":"/live","mode":"grid",
		"agent":{"state":"online","version":"2.0.25"},"control":{"state":"online"},
		"viewer":{"state":"running","version":"2.0.25"},"renderer":{"state":"unresponsive"},
		"streams":[{"streamName":"gate-live","state":"stalled","transport":"webrtc"}]
	}`
	for index := 0; index < 2; index++ {
		status, body := requestJSON(t, server.handler, http.MethodPost, "/api/viewers/heartbeat", degraded)
		if status != http.StatusOK {
			t.Fatalf("degraded heartbeat[%d]=%d body=%+v", index, status, body)
		}
	}
	assertViewerEvents(t, server.db, []expectedViewerEvent{
		{message: "viewer state changed", level: "warn"},
		{message: "viewer connected", level: "info"},
	})
}

func TestViewerTransitionTrackerSuppressesConcurrentDuplicates(t *testing.T) {
	tracker := newViewerTransitionTracker()
	viewer := store.Viewer{
		ID: "monitoring-pc", Agent: store.ViewerAgentHealth{State: "online"},
		Control:  store.ViewerControlHealth{State: "online"},
		Viewer:   store.ViewerProcessHealth{State: "running"},
		Renderer: store.ViewerRendererHealth{State: "ready"},
		Streams:  []store.ViewerStreamHealth{{StreamName: "집-마당-live", State: "playing", Transport: "webrtc"}},
	}
	var emitted atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, changed := tracker.Observe(viewer); changed {
				emitted.Add(1)
			}
		}()
	}
	wait.Wait()
	if got := emitted.Load(); got != 1 {
		t.Fatalf("concurrent transition records=%d want=1", got)
	}
}

type expectedViewerEvent struct {
	message string
	level   string
}

func assertViewerEvents(t *testing.T, db *store.DB, expected []expectedViewerEvent) {
	t.Helper()
	events, err := db.ListEvents(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	viewerEvents := make([]store.Event, 0, len(events))
	for _, event := range events {
		if event.Source == "viewer" {
			viewerEvents = append(viewerEvents, event)
		}
	}
	if len(viewerEvents) != len(expected) {
		t.Fatalf("viewer events=%d want=%d all=%+v", len(viewerEvents), len(expected), viewerEvents)
	}
	for index, want := range expected {
		if viewerEvents[index].Message != want.message || viewerEvents[index].Level != want.level {
			t.Fatalf("viewer event[%d]=%+v want=%+v", index, viewerEvents[index], want)
		}
	}
}
