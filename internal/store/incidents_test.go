package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIncidents_CRUDActionsAndFiltersResolvedDeleteOnly(t *testing.T) {
	t.Parallel()

	// Given
	db := newEventsIncidentTestDB(t)
	secret := syntheticEventSecret()
	created, err := db.CreateIncident(t.Context(), IncidentCreate{
		Title:       "manual camera check",
		Description: "operator created test incident",
		Severity:    "high",
		Source:      "manual",
		Details:     map[string]any{"snapshot": secret},
	})
	if err != nil {
		t.Fatalf("create incident: %v", err)
	}

	// When
	listed, listErr := db.ListIncidents(t.Context(), IncidentQuery{Status: "open", Severity: "high", Source: "manual"})
	acked, ackErr := db.AcknowledgeIncident(t.Context(), created.ID)
	snoozed, snoozeErr := db.SnoozeIncident(t.Context(), created.ID, time.Date(2026, 7, 2, 5, 0, 0, 0, time.UTC))
	resolved, resolveErr := db.ResolveIncident(t.Context(), created.ID)
	deleted, deleteErr := db.DeleteIncident(t.Context(), created.ID)

	// Then
	if listErr != nil || len(listed) != 1 {
		t.Fatalf("listed incidents = %#v, err=%v", listed, listErr)
	}
	assertStoreJSONDoesNotContain(t, listed, secret)
	if ackErr != nil || acked.Status != IncidentStatusAcknowledged || acked.AcknowledgedAt == nil {
		t.Fatalf("acked incident = %#v, err=%v", acked, ackErr)
	}
	if snoozeErr != nil || snoozed.Status != IncidentStatusSnoozed || snoozed.SnoozedUntil == nil {
		t.Fatalf("snoozed incident = %#v, err=%v", snoozed, snoozeErr)
	}
	if resolveErr != nil || resolved.Status != IncidentStatusResolved || resolved.ResolvedAt == nil {
		t.Fatalf("resolved incident = %#v, err=%v", resolved, resolveErr)
	}
	if deleteErr != nil || deleted.ID != created.ID {
		t.Fatalf("deleted incident = %#v, err=%v", deleted, deleteErr)
	}
	if _, err := db.GetIncident(t.Context(), created.ID); !errors.Is(err, ErrIncidentNotFound) {
		t.Fatalf("get deleted incident error = %v, want ErrIncidentNotFound", err)
	}
}

func TestIncidents_UpdateReopensAndRejectsDeletingOpenIncident(t *testing.T) {
	t.Parallel()

	// Given
	db := newEventsIncidentTestDB(t)
	created, err := db.CreateIncident(t.Context(), IncidentCreate{
		Title:    strings.TrimSpace("needs review"),
		Severity: "medium",
		Source:   "manual",
	})
	if err != nil {
		t.Fatalf("create incident: %v", err)
	}

	// When
	_, deleteOpenErr := db.DeleteIncident(t.Context(), created.ID)
	updated, updateErr := db.UpdateIncident(t.Context(), created.ID, IncidentUpdate{
		Title:    "reviewed camera",
		Severity: "low",
		Status:   IncidentStatusOpen,
	})

	// Then
	if !errors.Is(deleteOpenErr, ErrIncidentConflict) {
		t.Fatalf("delete open error = %v, want ErrIncidentConflict", deleteOpenErr)
	}
	if updateErr != nil {
		t.Fatalf("update incident: %v", updateErr)
	}
	if updated.Title != "reviewed camera" || updated.Severity != "low" || updated.Status != IncidentStatusOpen {
		t.Fatalf("updated incident = %#v", updated)
	}
}

func TestIncidents_RejectsInvalidSeverityAndPatchStatusWithoutPersisting(t *testing.T) {
	t.Parallel()

	// Given
	db := newEventsIncidentTestDB(t)
	_, createErr := db.CreateIncident(t.Context(), IncidentCreate{
		Title:    "invalid severity",
		Severity: "urgent",
		Source:   "manual",
	})
	valid, err := db.CreateIncident(t.Context(), IncidentCreate{
		Title:    "valid incident",
		Severity: "high",
		Source:   "manual",
	})
	if err != nil {
		t.Fatalf("create valid incident: %v", err)
	}

	// When
	_, patchSeverityErr := db.UpdateIncident(t.Context(), valid.ID, IncidentUpdate{Severity: "urgent"})
	_, patchResolvedErr := db.UpdateIncident(t.Context(), valid.ID, IncidentUpdate{Status: IncidentStatusResolved})
	detail, detailErr := db.GetIncident(t.Context(), valid.ID)
	listed, listErr := db.ListIncidents(t.Context(), IncidentQuery{})

	// Then
	if !errors.Is(createErr, ErrValidation) {
		t.Fatalf("invalid create error = %v, want ErrValidation", createErr)
	}
	if !errors.Is(patchSeverityErr, ErrValidation) {
		t.Fatalf("invalid patch severity error = %v, want ErrValidation", patchSeverityErr)
	}
	if !errors.Is(patchResolvedErr, ErrValidation) {
		t.Fatalf("invalid patch resolved error = %v, want ErrValidation", patchResolvedErr)
	}
	if detailErr != nil || listErr != nil {
		t.Fatalf("read incidents after rejected updates: detail=%v list=%v", detailErr, listErr)
	}
	if detail.Severity != "high" || detail.Status != IncidentStatusOpen {
		t.Fatalf("incident changed after rejected updates: %#v", detail)
	}
	if len(listed) != 1 {
		t.Fatalf("incident count = %d, want one valid incident", len(listed))
	}
}

func TestIncidents_AutomaticIncidentPublicJSONOmitsAutoKeyAndRedactsEventContent(t *testing.T) {
	t.Parallel()

	// Given
	db := newEventsIncidentTestDB(t)
	source := syntheticEventSecret()
	message := "camera failed through " + testDiscordWebhookURL()
	if err := db.AppendEvent(t.Context(), Event{
		Source:  source,
		Level:   "error",
		Message: message,
	}); err != nil {
		t.Fatalf("append qualifying event: %v", err)
	}

	// When
	listed, listErr := db.ListIncidents(t.Context(), IncidentQuery{Status: IncidentStatusOpen})
	detail, detailErr := db.GetIncident(t.Context(), listed[0].ID)
	resolved, resolveErr := db.ResolveIncident(t.Context(), listed[0].ID)
	deleted, deleteErr := db.DeleteIncident(t.Context(), resolved.ID)

	// Then
	if listErr != nil || detailErr != nil || resolveErr != nil || deleteErr != nil {
		t.Fatalf("incident lifecycle errors: list=%v detail=%v resolve=%v delete=%v", listErr, detailErr, resolveErr, deleteErr)
	}
	assertStoreJSONDoesNotContain(t, listed, source)
	assertStoreJSONDoesNotContain(t, listed, message)
	assertStoreJSONDoesNotContain(t, detail, source)
	assertStoreJSONDoesNotContain(t, detail, message)
	assertStoreJSONDoesNotContain(t, deleted, source)
	assertStoreJSONDoesNotContain(t, deleted, message)
	assertIncidentJSONOmitsAutoKey(t, listed)
	assertIncidentJSONOmitsAutoKey(t, detail)
	assertIncidentJSONOmitsAutoKey(t, deleted)
}

func assertIncidentJSONOmitsAutoKey(t *testing.T, value any) {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal incident value: %v", err)
	}
	if strings.Contains(string(encoded), "autoKey") {
		t.Fatalf("public incident JSON exposed autoKey")
	}
}
