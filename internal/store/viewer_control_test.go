package store

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestViewerCommandResultIsIdempotent(t *testing.T) {
	db := openMigratedStore(t)
	viewer, err := db.UpsertViewerHeartbeat(t.Context(), ViewerHeartbeat{
		ID:          "viewer-1",
		DisplayName: "Viewer 1",
		Route:       "/live?viewer=1",
		Mode:        "live",
	})
	if err != nil {
		t.Fatalf("seed viewer: %v", err)
	}
	command, err := db.CreateViewerCommand(t.Context(), viewer.ID, ViewerCommandCreate{Type: "restart_viewer"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	first, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State:        ViewerCommandSucceeded,
		OperationKey: "op-1",
	})
	if err != nil {
		t.Fatalf("apply first result: %v", err)
	}
	second, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State:        ViewerCommandSucceeded,
		OperationKey: "op-1",
	})
	if err != nil {
		t.Fatalf("apply duplicate result: %v", err)
	}
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("duplicate result updated timestamp: first=%s second=%s", first.UpdatedAt, second.UpdatedAt)
	}
}

func TestViewerCommandResultOmittedOperationKeyIsIdempotent(t *testing.T) {
	db := openMigratedStore(t)
	viewer, err := db.UpsertViewerHeartbeat(t.Context(), ViewerHeartbeat{
		ID: "viewer-optional-key", DisplayName: "Optional Key Viewer", Route: "/live?viewer=1", Mode: "live",
	})
	if err != nil {
		t.Fatalf("seed viewer: %v", err)
	}
	command, err := db.CreateViewerCommand(t.Context(), viewer.ID, ViewerCommandCreate{Type: "restart_viewer"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if _, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandAcknowledged, OperationKey: "op-1",
	}); err != nil {
		t.Fatalf("acknowledge command: %v", err)
	}
	running, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandRunning,
	})
	if err != nil {
		t.Fatalf("mark running without operation key: %v", err)
	}
	retry, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandRunning,
	})
	if err != nil {
		t.Fatalf("retry running without operation key: %v", err)
	}
	if retry.OperationKey != "op-1" || !retry.UpdatedAt.Equal(running.UpdatedAt) {
		t.Fatalf("retry changed command: %#v, first updatedAt=%s", retry, running.UpdatedAt)
	}
}

func TestViewerCommandResultRejectsDifferentOperationKey(t *testing.T) {
	db := openMigratedStore(t)
	viewer, err := db.UpsertViewerHeartbeat(t.Context(), ViewerHeartbeat{
		ID: "viewer-key-mismatch", DisplayName: "Key Mismatch Viewer", Route: "/live?viewer=1", Mode: "live",
	})
	if err != nil {
		t.Fatalf("seed viewer: %v", err)
	}
	command, err := db.CreateViewerCommand(t.Context(), viewer.ID, ViewerCommandCreate{Type: "restart_viewer"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	if _, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandAcknowledged, OperationKey: "op-1",
	}); err != nil {
		t.Fatalf("acknowledge command: %v", err)
	}
	running, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandRunning,
	})
	if err != nil {
		t.Fatalf("mark running without operation key: %v", err)
	}
	if _, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandRunning, OperationKey: "op-2",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("different operation key error = %v, want validation", err)
	}
	stored, err := db.GetViewerCommand(t.Context(), viewer.ID, command.ID)
	if err != nil || stored.State != ViewerCommandRunning || stored.OperationKey != "op-1" || !stored.UpdatedAt.Equal(running.UpdatedAt) {
		t.Fatalf("different operation key changed command: %#v err=%v", stored, err)
	}
}

func TestViewerCommandConcurrentDuplicateResultUpdatesOnce(t *testing.T) {
	db := openMigratedStore(t)
	viewer, err := db.UpsertViewerHeartbeat(t.Context(), ViewerHeartbeat{
		ID: "viewer-concurrent", DisplayName: "Concurrent Viewer", Route: "/live?viewer=1", Mode: "live",
	})
	if err != nil {
		t.Fatalf("seed viewer: %v", err)
	}
	command, err := db.CreateViewerCommand(t.Context(), viewer.ID, ViewerCommandCreate{Type: "restart_viewer"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	const callers = 24
	start := make(chan struct{})
	results := make(chan ViewerCommand, callers)
	errorsCh := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
				State: ViewerCommandSucceeded, OperationKey: "op-concurrent",
			})
			if err != nil {
				errorsCh <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("apply concurrent duplicate: %v", err)
	}
	var updatedAt time.Time
	for result := range results {
		if updatedAt.IsZero() {
			updatedAt = result.UpdatedAt
		}
		if !result.UpdatedAt.Equal(updatedAt) {
			t.Fatalf("duplicate changed updatedAt: first=%s got=%s", updatedAt, result.UpdatedAt)
		}
	}
}

func TestViewerCommandResultRejectsOutOfOrderRegression(t *testing.T) {
	db := openMigratedStore(t)
	viewer, err := db.UpsertViewerHeartbeat(t.Context(), ViewerHeartbeat{
		ID: "viewer-order", DisplayName: "Ordered Viewer", Route: "/live?viewer=1", Mode: "live",
	})
	if err != nil {
		t.Fatalf("seed viewer: %v", err)
	}
	command, err := db.CreateViewerCommand(t.Context(), viewer.ID, ViewerCommandCreate{Type: "restart_viewer"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}
	running, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandRunning, OperationKey: "op-order",
	})
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if _, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandAcknowledged, OperationKey: "op-order",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("running to acknowledged error = %v, want validation", err)
	}
	stored, err := db.GetViewerCommand(t.Context(), viewer.ID, command.ID)
	if err != nil || stored.State != ViewerCommandRunning || !stored.UpdatedAt.Equal(running.UpdatedAt) {
		t.Fatalf("regression changed command: %#v err=%v", stored, err)
	}
	terminal, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandSucceeded, OperationKey: "op-order",
	})
	if err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	if _, err := db.ApplyViewerCommandResult(t.Context(), viewer.ID, command.ID, ViewerCommandResult{
		State: ViewerCommandAcknowledged, OperationKey: "op-order",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("terminal to acknowledged error = %v, want validation", err)
	}
	stored, err = db.GetViewerCommand(t.Context(), viewer.ID, command.ID)
	if err != nil || stored.State != ViewerCommandSucceeded || !stored.UpdatedAt.Equal(terminal.UpdatedAt) {
		t.Fatalf("terminal regression changed command: %#v err=%v", stored, err)
	}
}

func TestViewerControlMigrationUpgradesLegacyTables(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	legacy := []string{
		`CREATE TABLE viewers (
			id TEXT PRIMARY KEY, display_name TEXT NOT NULL, app_version TEXT NOT NULL,
			hostname TEXT NOT NULL, device_label TEXT NOT NULL, route TEXT NOT NULL,
			mode TEXT NOT NULL, label TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '', streams_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL, last_heartbeat_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE viewer_commands (
			id INTEGER PRIMARY KEY AUTOINCREMENT, viewer_id TEXT NOT NULL, type TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '', route TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
			sent_at TEXT, completed_at TEXT, updated_at TEXT NOT NULL,
			FOREIGN KEY(viewer_id) REFERENCES viewers(id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range legacy {
		if _, err := db.db.ExecContext(t.Context(), statement); err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
	}
	legacyAt := "2026-07-16T01:02:03Z"
	if _, err := db.db.ExecContext(t.Context(),
		`INSERT INTO viewers(id, display_name, app_version, hostname, device_label, route, mode, created_at, last_heartbeat_at, updated_at)
		 VALUES ('legacy-viewer', 'Legacy Viewer', '1.0.0', 'legacy-host', 'wall', '/live', 'grid', ?, ?, ?)`,
		legacyAt, legacyAt, legacyAt,
	); err != nil {
		t.Fatalf("seed legacy viewer: %v", err)
	}
	if _, err := db.db.ExecContext(t.Context(),
		`INSERT INTO viewer_commands(viewer_id, type, state, created_at, sent_at, updated_at)
		 VALUES ('legacy-viewer', 'refresh', 'sent', ?, ?, ?)`, legacyAt, legacyAt, legacyAt,
	); err != nil {
		t.Fatalf("seed legacy command: %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate legacy store: %v", err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}

	assertTableColumns(t, db, "viewers", []string{
		"agent_state", "agent_version", "control_state", "viewer_state", "viewer_version",
		"renderer_state", "last_control_success_at", "last_viewer_heartbeat_at",
		"last_renderer_heartbeat_at", "last_video_progress_at", "update_state",
		"update_target_version", "update_generation",
	})
	assertTableColumns(t, db, "viewer_commands", []string{
		"payload_hash", "ttl_seconds", "operation_key", "generation", "delivered_at",
		"acknowledged_at", "running_at", "result_at",
	})
	migrated, err := db.GetViewerCommand(t.Context(), "legacy-viewer", 1)
	if err != nil {
		t.Fatalf("load migrated legacy command: %v", err)
	}
	if migrated.State != ViewerCommandDelivered || migrated.DeliveredAt == nil {
		t.Fatalf("legacy sent command not normalized: %#v", migrated)
	}
}

func TestViewerAdminListDoesNotDeliverAndDeliveryTimestampIsStable(t *testing.T) {
	db := openMigratedStore(t)
	viewer, err := db.UpsertViewerHeartbeat(t.Context(), ViewerHeartbeat{
		ID: "viewer-delivery", DisplayName: "Viewer delivery", Route: "/live?viewer=1", Mode: "live",
	})
	if err != nil {
		t.Fatalf("seed viewer: %v", err)
	}
	created, err := db.CreateViewerCommand(t.Context(), viewer.ID, ViewerCommandCreate{Type: "ping"})
	if err != nil {
		t.Fatalf("create command: %v", err)
	}

	listed, err := db.ListViewerCommands(t.Context(), viewer.ID)
	if err != nil {
		t.Fatalf("admin list commands: %v", err)
	}
	if len(listed) != 1 || listed[0].State != ViewerCommandPending || listed[0].DeliveredAt != nil {
		t.Fatalf("admin list delivered command: %#v", listed)
	}
	first, ok, err := db.DeliverNextViewerCommand(t.Context(), viewer.ID)
	if err != nil || !ok {
		t.Fatalf("first delivery: ok=%v err=%v", ok, err)
	}
	if first.ID != created.ID || first.State != ViewerCommandDelivered || first.DeliveredAt == nil {
		t.Fatalf("first delivered command = %#v", first)
	}
	second, ok, err := db.DeliverNextViewerCommand(t.Context(), viewer.ID)
	if err != nil || !ok {
		t.Fatalf("duplicate delivery: ok=%v err=%v", ok, err)
	}
	if second.ID != first.ID || second.DeliveredAt == nil || !second.DeliveredAt.Equal(*first.DeliveredAt) || !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("redelivery changed command: first=%#v second=%#v", first, second)
	}
}

func TestViewerHeartbeatPersistsIndependentControlHealth(t *testing.T) {
	db := openMigratedStore(t)
	controlAt := time.Now().UTC().Add(-2 * time.Second).Truncate(time.Millisecond)
	viewerAt := controlAt.Add(time.Second)
	rendererAt := viewerAt.Add(250 * time.Millisecond)
	progressAt := rendererAt.Add(250 * time.Millisecond)
	got, err := db.UpsertViewerHeartbeat(t.Context(), ViewerHeartbeat{
		ID:          "viewer-health",
		DisplayName: "Health Viewer",
		Route:       "/live?viewer=1",
		Mode:        "live",
		Agent:       ViewerAgentHealth{State: "online", Version: "2.1.0"},
		Control:     ViewerControlHealth{State: "control_degraded", LastSuccessAt: &controlAt},
		Viewer:      ViewerProcessHealth{State: "running", Version: "2.0.0", LastHeartbeatAt: &viewerAt},
		Renderer:    ViewerRendererHealth{State: "ready", LastHeartbeatAt: &rendererAt, LastProgressAt: &progressAt},
		Update:      ViewerUpdateHealth{State: "idle", Generation: 4},
		Streams: []ViewerStreamHealth{{
			StreamName: "gate-main", State: "playing", Transport: "webrtc", LastProgressAt: &progressAt,
		}},
	})
	if err != nil {
		t.Fatalf("upsert independent health: %v", err)
	}
	if got.Status != "control_degraded" || got.Agent.State != "online" || got.Control.State != "control_degraded" {
		t.Fatalf("independent health collapsed: %#v", got)
	}
	if got.Control.LastSuccessAt == nil || !got.Control.LastSuccessAt.Equal(controlAt) {
		t.Fatalf("control success timestamp = %v", got.Control.LastSuccessAt)
	}
	if got.Renderer.LastProgressAt == nil || !got.Renderer.LastProgressAt.Equal(progressAt) {
		t.Fatalf("renderer progress timestamp = %v", got.Renderer.LastProgressAt)
	}
	if len(got.Streams) != 1 || got.Streams[0].LastProgressAt == nil || !got.Streams[0].LastProgressAt.Equal(progressAt) {
		t.Fatalf("stream timestamps = %#v", got.Streams)
	}
}

func assertTableColumns(t *testing.T, db *DB, table string, names []string) {
	t.Helper()
	rows, err := db.db.QueryContext(t.Context(), "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatalf("list %s columns: %v", table, err)
	}
	defer rows.Close()
	found := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan %s columns: %v", table, err)
		}
		found[name] = true
	}
	for _, name := range names {
		if !found[name] {
			t.Errorf("%s column %q missing", table, name)
		}
	}
}
