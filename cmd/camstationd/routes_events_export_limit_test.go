package main

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"camstation/internal/store"
)

func TestEventsAPI_ExportUsesLimitAboveNormalPageLimit(t *testing.T) {
	t.Parallel()

	// Given
	server := newIncidentRouteServer(t)
	base := time.Date(2026, 7, 2, 5, 0, 0, 0, time.UTC)
	for i := 0; i < 650; i++ {
		if err := server.db.AppendEvent(t.Context(), store.Event{
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			Source:    "export-api",
			Level:     "info",
			Message:   "export api event " + strconv.Itoa(i),
		}); err != nil {
			t.Fatalf("append export api event %d: %v", i, err)
		}
	}

	// When
	status, body := requestText(t, server.handler, http.MethodGet, "/api/events/export?format=text&source=export-api&limit=600", "")

	// Then
	lines := strings.Count(strings.TrimSpace(body), "\n") + 1
	if status != http.StatusOK {
		t.Fatalf("export status = %d, want %d", status, http.StatusOK)
	}
	if lines != 600 {
		t.Fatalf("export line count = %d, want 600", lines)
	}
	writeAPIEvidence(t, "events-export-limit.json", map[string]any{
		"status":    status,
		"format":    "text",
		"lineCount": lines,
		"limit":     600,
	})
}
