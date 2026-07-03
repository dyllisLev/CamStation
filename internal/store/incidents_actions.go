package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (d *DB) AcknowledgeIncident(ctx context.Context, id int64) (Incident, error) {
	now := time.Now().UTC()
	return d.setIncidentStatus(ctx, id, IncidentStatusAcknowledged, &now, nil, nil)
}

func (d *DB) SnoozeIncident(ctx context.Context, id int64, until time.Time) (Incident, error) {
	if until.IsZero() {
		return Incident{}, fmt.Errorf("snooze until is required: %w", ErrValidation)
	}
	return d.setIncidentStatus(ctx, id, IncidentStatusSnoozed, nil, &until, nil)
}

func (d *DB) ResolveIncident(ctx context.Context, id int64) (Incident, error) {
	now := time.Now().UTC()
	return d.setIncidentStatus(ctx, id, IncidentStatusResolved, nil, nil, &now)
}

func (d *DB) DeleteIncident(ctx context.Context, id int64) (Incident, error) {
	incident, err := d.GetIncident(ctx, id)
	if err != nil {
		return Incident{}, err
	}
	if incident.Status != IncidentStatusResolved {
		return Incident{}, ErrIncidentConflict
	}
	res, err := d.db.ExecContext(ctx, `DELETE FROM incidents WHERE id = ?`, id)
	if err != nil {
		return Incident{}, fmt.Errorf("delete incident: %w", err)
	}
	if err := requireChanged(res); err != nil {
		return Incident{}, ErrIncidentNotFound
	}
	return incident, nil
}

func (d *DB) createIncidentFromEvent(ctx context.Context, event Event) error {
	if event.Level != "error" && event.Level != "critical" {
		return nil
	}
	severity := "high"
	if event.Level == "critical" {
		severity = "critical"
	}
	autoKey := "event:" + event.Source + ":" + event.Message
	_, err := d.insertIncident(ctx, IncidentCreate{
		Title:       event.Message,
		Description: "auto-created error event",
		Severity:    severity,
		Source:      event.Source,
		Details:     map[string]any{"eventLevel": event.Level},
	}, IncidentStatusOpen, autoKey, time.Now().UTC())
	if err == nil || errors.Is(err, ErrIncidentConflict) {
		return nil
	}
	return err
}

func (d *DB) insertIncident(ctx context.Context, create IncidentCreate, status string, autoKey string, now time.Time) (Incident, error) {
	create.Title = strings.TrimSpace(create.Title)
	create.Source = strings.TrimSpace(create.Source)
	severity, err := parseIncidentSeverity(create.Severity)
	if err != nil {
		return Incident{}, err
	}
	create.Severity = severity
	if status == "" {
		status = IncidentStatusOpen
	}
	if err := validateIncident(create.Title, create.Severity, status, create.Source); err != nil {
		return Incident{}, err
	}
	details, err := json.Marshal(redactDetails(create.Details))
	if err != nil {
		return Incident{}, fmt.Errorf("encode incident details: %w", err)
	}
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO incidents(created_at, updated_at, source, severity, status, title, description, details_json, auto_key)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), create.Source, create.Severity, status,
		create.Title, strings.TrimSpace(create.Description), string(details), autoKey,
	)
	if err != nil {
		if autoKey != "" && strings.Contains(err.Error(), "UNIQUE") {
			return Incident{}, ErrIncidentConflict
		}
		return Incident{}, fmt.Errorf("create incident: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Incident{}, fmt.Errorf("created incident id: %w", err)
	}
	return d.GetIncident(ctx, id)
}

func (d *DB) saveIncident(ctx context.Context, incident Incident) error {
	details, err := json.Marshal(redactDetails(incident.Details))
	if err != nil {
		return fmt.Errorf("encode incident details: %w", err)
	}
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`UPDATE incidents
		 SET updated_at = ?, source = ?, severity = ?, status = ?, title = ?, description = ?, details_json = ?,
		     acknowledged_at = ?, snoozed_until = ?, resolved_at = ?
		 WHERE id = ?`,
		now.Format(time.RFC3339Nano), incident.Source, incident.Severity, incident.Status, incident.Title,
		incident.Description, string(details), optionalTimeText(incident.AcknowledgedAt),
		optionalTimeText(incident.SnoozedUntil), optionalTimeText(incident.ResolvedAt), incident.ID,
	)
	if err != nil {
		return fmt.Errorf("update incident: %w", err)
	}
	return requireIncidentChanged(res)
}

func (d *DB) setIncidentStatus(ctx context.Context, id int64, status string, ackedAt *time.Time, snoozedUntil *time.Time, resolvedAt *time.Time) (Incident, error) {
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		`UPDATE incidents
		 SET status = ?, updated_at = ?, acknowledged_at = COALESCE(?, acknowledged_at),
		     snoozed_until = ?, resolved_at = ?
		 WHERE id = ?`,
		status, now.Format(time.RFC3339Nano), optionalTimeText(ackedAt), optionalTimeText(snoozedUntil),
		optionalTimeText(resolvedAt), id,
	)
	if err != nil {
		return Incident{}, fmt.Errorf("set incident status: %w", err)
	}
	if err := requireIncidentChanged(res); err != nil {
		return Incident{}, err
	}
	return d.GetIncident(ctx, id)
}

func requireIncidentChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("incident rows affected: %w", err)
	}
	if changed == 0 {
		return ErrIncidentNotFound
	}
	return nil
}
