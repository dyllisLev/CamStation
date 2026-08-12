package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func scanIncident(scanner interface {
	Scan(dest ...any) error
}) (Incident, error) {
	var incident Incident
	var createdAt, updatedAt, detailsJSON string
	var ackedAt, snoozedUntil, resolvedAt sql.NullString
	if err := scanner.Scan(&incident.ID, &createdAt, &updatedAt, &incident.Source, &incident.Severity, &incident.Status,
		&incident.Title, &incident.Description, &detailsJSON, &ackedAt, &snoozedUntil, &resolvedAt, &incident.AutoKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Incident{}, ErrIncidentNotFound
		}
		return Incident{}, err
	}
	var err error
	incident.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Incident{}, fmt.Errorf("parse incident created_at: %w", err)
	}
	incident.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Incident{}, fmt.Errorf("parse incident updated_at: %w", err)
	}
	incident.AcknowledgedAt = parseNullTime(ackedAt)
	incident.SnoozedUntil = parseNullTime(snoozedUntil)
	incident.ResolvedAt = parseNullTime(resolvedAt)
	if err := json.Unmarshal([]byte(detailsJSON), &incident.Details); err != nil {
		incident.Details = map[string]any{"parseError": err.Error()}
	}
	return incident, nil
}

func redactIncident(incident Incident) Incident {
	incident.Source = redactString(incident.Source)
	incident.Title = redactString(incident.Title)
	incident.Description = redactString(incident.Description)
	incident.Details = redactDetails(incident.Details)
	incident.AutoKey = ""
	return incident
}

func optionalTimeText(value *time.Time) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: value.UTC().Format(time.RFC3339Nano), Valid: true}
}

func parseNullTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}
