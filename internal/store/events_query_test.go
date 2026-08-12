package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newEventsIncidentTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	return db
}

func syntheticEventSecret() string {
	return "rt" + "sp://operator:" + "synthetic" + "-secret@" + "camera.internal:554/live"
}

func TestEvents_QueryFiltersSearchCursorDateAndRedactsPublicPayloads(t *testing.T) {
	t.Parallel()

	// Given
	db := newEventsIncidentTestDB(t)
	secret := syntheticEventSecret()
	base := time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC)
	fixtures := []Event{
		{CreatedAt: base, Source: "recorder", Level: "info", Message: "worker started"},
		{CreatedAt: base.Add(time.Minute), Source: "recorder", Level: "error", Message: "camera timeout", Details: map[string]any{"url": secret}},
		{CreatedAt: base.Add(2 * time.Minute), Source: "backup", Level: "error", Message: "remote copy failed"},
		{CreatedAt: base.Add(3 * time.Minute), Source: "recorder", Level: "warning", Message: "late frame"},
	}
	for _, event := range fixtures {
		if err := db.AppendEvent(t.Context(), event); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}

	// When
	filtered, err := db.QueryEvents(t.Context(), EventQuery{
		Level:  "error",
		Source: "recorder",
		Search: "timeout",
		From:   base.Add(30 * time.Second),
		To:     base.Add(90 * time.Second),
		Limit:  10,
	})
	firstPage, pageErr := db.QueryEvents(t.Context(), EventQuery{Limit: 2})
	secondPage, cursorErr := db.QueryEvents(t.Context(), EventQuery{Cursor: firstPage.NextCursor, Limit: 10})

	// Then
	if err != nil {
		t.Fatalf("query filtered events: %v", err)
	}
	if len(filtered.Events) != 1 || filtered.Events[0].Message != "camera timeout" {
		t.Fatalf("filtered events = %#v, want only camera timeout", filtered.Events)
	}
	assertStoreJSONDoesNotContain(t, filtered, secret)
	if pageErr != nil || cursorErr != nil {
		t.Fatalf("paged queries: %v / %v", pageErr, cursorErr)
	}
	if len(firstPage.Events) != 2 || firstPage.NextCursor == "" {
		t.Fatalf("first page = %#v, want two events and cursor", firstPage)
	}
	if len(secondPage.Events) != 2 {
		t.Fatalf("second page len = %d, want remaining two", len(secondPage.Events))
	}
}

func TestRedactEventMasksInternalGo2RTCEndpointsRecursively(t *testing.T) {
	event := RedactEvent(Event{
		Message: "dial http://127.0.0.1:1984/api/streams failed",
		Details: map[string]any{"output": "rtsp://admin:secret@localhost:8554/private", "nested": []any{"http://localhost:1984/api"}},
	})
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"127.0.0.1", "localhost", ":1984", ":8554", "admin", "secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public event leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestEvents_PruneRequiresSafeFilterAndAutoIncidentDeduplicatesErrorEvents(t *testing.T) {
	t.Parallel()

	// Given
	db := newEventsIncidentTestDB(t)
	base := time.Date(2026, 7, 2, 2, 0, 0, 0, time.UTC)
	for _, event := range []Event{
		{CreatedAt: base, Source: "recorder", Level: "error", Message: "camera failed"},
		{CreatedAt: base.Add(time.Minute), Source: "recorder", Level: "error", Message: "camera failed"},
		{CreatedAt: base.Add(2 * time.Minute), Source: "recorder", Level: "info", Message: "worker recovered"},
	} {
		if err := db.AppendEvent(t.Context(), event); err != nil {
			t.Fatalf("append event: %v", err)
		}
	}

	// When
	_, unsafeErr := db.PruneEvents(t.Context(), EventPrune{
		Confirm: true,
		Limit:   2,
	})
	pruned, pruneErr := db.PruneEvents(t.Context(), EventPrune{
		Confirm: true,
		Before:  base.Add(90 * time.Second),
		Level:   "error",
		Limit:   10,
	})
	incidents, incidentErr := db.ListIncidents(t.Context(), IncidentQuery{Status: "open"})

	// Then
	if unsafeErr == nil {
		t.Fatalf("unsafe prune error = nil, want validation failure")
	}
	if pruneErr != nil {
		t.Fatalf("prune events: %v", pruneErr)
	}
	if pruned.Deleted != 2 {
		t.Fatalf("pruned count = %d, want duplicate error events deleted", pruned.Deleted)
	}
	if incidentErr != nil {
		t.Fatalf("list incidents: %v", incidentErr)
	}
	if len(incidents) != 1 || incidents[0].Title != "camera failed" {
		t.Fatalf("auto incidents = %#v, want one deduplicated incident", incidents)
	}
}

func TestEvents_ExportUsesExportLimitAboveNormalPageLimit(t *testing.T) {
	t.Parallel()

	// Given
	db := newEventsIncidentTestDB(t)
	base := time.Date(2026, 7, 2, 4, 0, 0, 0, time.UTC)
	for i := 0; i < 650; i++ {
		if err := db.AppendEvent(t.Context(), Event{
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			Source:    "export",
			Level:     "info",
			Message:   "export batch event",
		}); err != nil {
			t.Fatalf("append export event %d: %v", i, err)
		}
	}

	// When
	events, err := db.ExportEvents(t.Context(), EventQuery{Source: "export", Limit: 600})

	// Then
	if err != nil {
		t.Fatalf("export events: %v", err)
	}
	if len(events) != 600 {
		t.Fatalf("exported event count = %d, want 600", len(events))
	}
}

func assertStoreJSONDoesNotContain(t *testing.T, value any, forbidden string) {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if strings.Contains(string(encoded), forbidden) {
		t.Fatalf("public JSON leaked forbidden value")
	}
}
