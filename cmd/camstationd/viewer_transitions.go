package main

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"camstation/internal/opslog"
	"camstation/internal/store"
	"camstation/internal/streamkey"
)

var viewerTransitionCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type viewerTransitionTracker struct {
	mu      sync.Mutex
	viewers map[string]viewerHeartbeatSnapshot
}

type viewerHeartbeatSnapshot struct {
	agent    string
	control  string
	viewer   string
	renderer string
	streams  map[string]viewerStreamSnapshot
}

type viewerStreamSnapshot struct {
	state     string
	transport string
}

type viewerTransitionObservation struct {
	Event   store.Event
	Records []viewerTransitionRecord
}

type viewerTransitionRecord struct {
	Level     opslog.Level
	Component string
	Event     string
	Fields    opslog.Fields
}

func newViewerTransitionTracker() *viewerTransitionTracker {
	return &viewerTransitionTracker{viewers: make(map[string]viewerHeartbeatSnapshot)}
}

func (tracker *viewerTransitionTracker) Observe(viewer store.Viewer) (viewerTransitionObservation, bool) {
	if tracker == nil || strings.TrimSpace(viewer.ID) == "" {
		return viewerTransitionObservation{}, false
	}
	next := snapshotViewerHeartbeat(viewer)
	tracker.mu.Lock()
	previous, exists := tracker.viewers[viewer.ID]
	if exists && equalViewerHeartbeatSnapshot(previous, next) {
		tracker.mu.Unlock()
		return viewerTransitionObservation{}, false
	}
	tracker.viewers[viewer.ID] = next
	tracker.mu.Unlock()

	changedFields, records := viewerTransitionRecords(viewer.ID, previous, next, exists)
	state, level := viewerSnapshotHealth(next)
	message := "viewer connected"
	eventName := "connected"
	if exists {
		message = "viewer state changed"
		eventName = "state_changed"
	}
	records = append([]viewerTransitionRecord{{
		Level: level, Component: "viewer.heartbeat", Event: eventName,
		Fields: opslog.Fields{ViewerID: viewer.ID, State: state},
	}}, records...)
	return viewerTransitionObservation{
		Event: store.Event{
			Source: "viewer", Level: level.String(), Message: message,
			Details: map[string]any{
				"viewerId": viewer.ID, "state": state, "changedFields": changedFields,
				"agentState":    safeViewerTransitionCode(next.agent),
				"controlState":  safeViewerTransitionCode(next.control),
				"viewerState":   safeViewerTransitionCode(next.viewer),
				"rendererState": safeViewerTransitionCode(next.renderer),
				"streamCount":   len(next.streams),
			},
		},
		Records: records,
	}, true
}

func snapshotViewerHeartbeat(viewer store.Viewer) viewerHeartbeatSnapshot {
	snapshot := viewerHeartbeatSnapshot{
		agent: viewer.Agent.State, control: viewer.Control.State,
		viewer: viewer.Viewer.State, renderer: viewer.Renderer.State,
		streams: make(map[string]viewerStreamSnapshot, len(viewer.Streams)),
	}
	for _, stream := range viewer.Streams {
		name := strings.TrimSpace(stream.StreamName)
		if !streamkey.Valid(name) {
			continue
		}
		snapshot.streams[name] = viewerStreamSnapshot{state: stream.State, transport: stream.Transport}
	}
	return snapshot
}

func equalViewerHeartbeatSnapshot(left, right viewerHeartbeatSnapshot) bool {
	if left.agent != right.agent || left.control != right.control || left.viewer != right.viewer || left.renderer != right.renderer ||
		len(left.streams) != len(right.streams) {
		return false
	}
	for name, leftStream := range left.streams {
		if right.streams[name] != leftStream {
			return false
		}
	}
	return true
}

func viewerTransitionRecords(viewerID string, previous, next viewerHeartbeatSnapshot, existed bool) ([]string, []viewerTransitionRecord) {
	changed := make([]string, 0, 8)
	records := make([]viewerTransitionRecord, 0, 8)
	appendState := func(name, oldState, state string) {
		if existed && oldState == state {
			return
		}
		safeState := safeViewerTransitionCode(state)
		if safeState == "unknown" && strings.TrimSpace(state) == "" {
			return
		}
		changed = append(changed, name)
		_, level := viewerConnectionHealth(state)
		event := "state_observed"
		if existed {
			event = "state_changed"
		}
		records = append(records, viewerTransitionRecord{
			Level: level, Component: "viewer.heartbeat." + name, Event: event,
			Fields: opslog.Fields{ViewerID: viewerID, State: safeState},
		})
	}
	appendState("agent", previous.agent, next.agent)
	appendState("control", previous.control, next.control)
	appendState("main", previous.viewer, next.viewer)
	appendState("renderer", previous.renderer, next.renderer)

	names := make(map[string]struct{}, len(previous.streams)+len(next.streams))
	for name := range previous.streams {
		names[name] = struct{}{}
	}
	for name := range next.streams {
		names[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		oldStream, hadOld := previous.streams[name]
		stream, hasNext := next.streams[name]
		if existed && hadOld == hasNext && oldStream == stream {
			continue
		}
		changed = append(changed, "stream:"+name)
		state := stream.state
		event := "stream_observed"
		if existed {
			event = "stream_state_changed"
		}
		if !hasNext {
			state = "removed"
			event = "stream_removed"
		}
		_, level := viewerConnectionHealth(state)
		records = append(records, viewerTransitionRecord{
			Level: level, Component: "viewer.heartbeat.playback", Event: event,
			Fields: opslog.Fields{
				ViewerID: viewerID, StreamName: name, State: safeViewerTransitionCode(state),
				Transport: safeViewerTransitionCode(stream.transport),
			},
		})
	}
	if len(changed) == 0 && !existed {
		changed = append(changed, "viewer")
	}
	return changed, records
}

func viewerSnapshotHealth(snapshot viewerHeartbeatSnapshot) (string, opslog.Level) {
	states := []string{snapshot.agent, snapshot.control, snapshot.viewer, snapshot.renderer}
	for _, stream := range snapshot.streams {
		states = append(states, stream.state)
	}
	complete := snapshot.control == "online" && snapshot.viewer == "running" && snapshot.renderer == "ready"
	for _, state := range states {
		if health, level := viewerConnectionHealth(state); level == opslog.Warn {
			return health, level
		}
	}
	if complete {
		return "healthy", opslog.Info
	}
	return "observed", opslog.Info
}

func viewerConnectionHealth(state string) (string, opslog.Level) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "degraded", "failed", "unresponsive", "stalled", "retrying", "fallback", "cooldown", "unsupported", "offline":
		return "degraded", opslog.Warn
	default:
		return "healthy", opslog.Info
	}
}

func safeViewerTransitionCode(value string) string {
	value = strings.TrimSpace(value)
	if viewerTransitionCodePattern.MatchString(value) {
		return value
	}
	return "unknown"
}
