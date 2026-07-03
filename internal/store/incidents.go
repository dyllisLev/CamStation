package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	IncidentStatusOpen         = "open"
	IncidentStatusAcknowledged = "acknowledged"
	IncidentStatusSnoozed      = "snoozed"
	IncidentStatusResolved     = "resolved"
)

var (
	ErrIncidentNotFound = errors.New("incident not found")
	ErrIncidentConflict = errors.New("incident state conflict")
)

type Incident struct {
	ID             int64          `json:"id"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	Source         string         `json:"source"`
	Severity       string         `json:"severity"`
	Status         string         `json:"status"`
	Title          string         `json:"title"`
	Description    string         `json:"description,omitempty"`
	Details        map[string]any `json:"details,omitempty"`
	AcknowledgedAt *time.Time     `json:"acknowledgedAt,omitempty"`
	SnoozedUntil   *time.Time     `json:"snoozedUntil,omitempty"`
	ResolvedAt     *time.Time     `json:"resolvedAt,omitempty"`
	AutoKey        string         `json:"-"`
}

type IncidentCreate struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Severity    string         `json:"severity"`
	Status      string         `json:"status"`
	Source      string         `json:"source"`
	Details     map[string]any `json:"details"`
}

type IncidentUpdate struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Severity    string         `json:"severity"`
	Status      string         `json:"status"`
	Source      string         `json:"source"`
	Details     map[string]any `json:"details"`
}

type IncidentQuery struct {
	Status   string
	Severity string
	Source   string
	Limit    int
}

func (d *DB) CreateIncident(ctx context.Context, create IncidentCreate) (Incident, error) {
	now := time.Now().UTC()
	status, err := parseCreateIncidentStatus(create.Status)
	if err != nil {
		return Incident{}, err
	}
	return d.insertIncident(ctx, create, status, "", now)
}

func (d *DB) ListIncidents(ctx context.Context, query IncidentQuery) ([]Incident, error) {
	query.Limit = normalizeIncidentLimit(query.Limit)
	where, args := incidentWhere(query)
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, created_at, updated_at, source, severity, status, title, description, details_json,
		        acknowledged_at, snoozed_until, resolved_at, auto_key
		 FROM incidents `+where+` ORDER BY id DESC LIMIT ?`,
		append(args, query.Limit)...,
	)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()
	incidents := make([]Incident, 0)
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, redactIncident(incident))
	}
	return incidents, rows.Err()
}

func (d *DB) GetIncident(ctx context.Context, id int64) (Incident, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT id, created_at, updated_at, source, severity, status, title, description, details_json,
		        acknowledged_at, snoozed_until, resolved_at, auto_key
		 FROM incidents WHERE id = ?`,
		id,
	)
	incident, err := scanIncident(row)
	if err != nil {
		return Incident{}, err
	}
	return redactIncident(incident), nil
}

func (d *DB) UpdateIncident(ctx context.Context, id int64, update IncidentUpdate) (Incident, error) {
	current, err := d.GetIncident(ctx, id)
	if err != nil {
		return Incident{}, err
	}
	if update.Title != "" {
		current.Title = strings.TrimSpace(update.Title)
	}
	if update.Description != "" {
		current.Description = strings.TrimSpace(update.Description)
	}
	if update.Severity != "" {
		severity, err := parseIncidentSeverity(update.Severity)
		if err != nil {
			return Incident{}, err
		}
		current.Severity = severity
	}
	if update.Source != "" {
		current.Source = strings.TrimSpace(update.Source)
	}
	if update.Details != nil {
		current.Details = redactDetails(update.Details)
	}
	if update.Status != "" {
		status, err := parsePatchIncidentStatus(update.Status)
		if err != nil {
			return Incident{}, err
		}
		current.Status = status
		if current.Status == IncidentStatusOpen {
			current.AcknowledgedAt = nil
			current.SnoozedUntil = nil
			current.ResolvedAt = nil
		}
	}
	if err := validateIncident(current.Title, current.Severity, current.Status, current.Source); err != nil {
		return Incident{}, err
	}
	if err := d.saveIncident(ctx, current); err != nil {
		return Incident{}, err
	}
	return d.GetIncident(ctx, id)
}

func incidentWhere(query IncidentQuery) (string, []any) {
	clauses := make([]string, 0)
	args := make([]any, 0)
	if query.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, strings.TrimSpace(query.Status))
	}
	if query.Severity != "" {
		clauses = append(clauses, "severity = ?")
		args = append(args, strings.TrimSpace(query.Severity))
	}
	if query.Source != "" {
		clauses = append(clauses, "source = ?")
		args = append(args, strings.TrimSpace(query.Source))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func validateIncident(title string, severity string, status string, source string) error {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(source) == "" {
		return fmt.Errorf("incident title and source are required: %w", ErrValidation)
	}
	if !validIncidentSeverity(severity) || !validIncidentStatus(status) {
		return fmt.Errorf("invalid incident severity or status: %w", ErrValidation)
	}
	return nil
}

func parseIncidentSeverity(severity string) (string, error) {
	severity = strings.TrimSpace(severity)
	if severity == "" {
		return "medium", nil
	}
	if !validIncidentSeverity(severity) {
		return "", fmt.Errorf("invalid incident severity: %w", ErrValidation)
	}
	return severity, nil
}

func parseIncidentStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return IncidentStatusOpen, nil
	}
	if !validIncidentStatus(status) {
		return "", fmt.Errorf("invalid incident status: %w", ErrValidation)
	}
	return status, nil
}

func parseCreateIncidentStatus(status string) (string, error) {
	parsed, err := parseIncidentStatus(status)
	if err != nil {
		return "", err
	}
	if parsed != IncidentStatusOpen {
		return "", fmt.Errorf("incident create status must be open: %w", ErrValidation)
	}
	return parsed, nil
}

func parsePatchIncidentStatus(status string) (string, error) {
	parsed, err := parseIncidentStatus(status)
	if err != nil {
		return "", err
	}
	if parsed != IncidentStatusOpen {
		return "", fmt.Errorf("incident status changes use action routes: %w", ErrValidation)
	}
	return parsed, nil
}

func validIncidentSeverity(severity string) bool {
	switch severity {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func validIncidentStatus(status string) bool {
	switch status {
	case IncidentStatusOpen, IncidentStatusAcknowledged, IncidentStatusSnoozed, IncidentStatusResolved:
		return true
	default:
		return false
	}
}

func normalizeIncidentLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}
