package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"camstation/internal/store"
)

func (d routeDeps) handleIncidentCreate(w http.ResponseWriter, r *http.Request) {
	var req store.IncidentCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	incident, err := d.db.CreateIncident(r.Context(), req)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, incident)
}

func (d routeDeps) handleIncidentList(w http.ResponseWriter, r *http.Request) {
	limit, err := optionalPositiveInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	incidents, err := d.db.ListIncidents(r.Context(), store.IncidentQuery{
		Status:   strings.TrimSpace(r.URL.Query().Get("status")),
		Severity: strings.TrimSpace(r.URL.Query().Get("severity")),
		Source:   strings.TrimSpace(r.URL.Query().Get("source")),
		Limit:    limit,
	})
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": incidents})
}

func (d routeDeps) handleIncidentDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathIncidentID(w, r)
	if !ok {
		return
	}
	incident, err := d.db.GetIncident(r.Context(), id)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (d routeDeps) handleIncidentPatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathIncidentID(w, r)
	if !ok {
		return
	}
	var req store.IncidentUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	incident, err := d.db.UpdateIncident(r.Context(), id, req)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (d routeDeps) handleIncidentAck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathIncidentID(w, r)
	if !ok {
		return
	}
	incident, err := d.db.AcknowledgeIncident(r.Context(), id)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (d routeDeps) handleIncidentSnooze(w http.ResponseWriter, r *http.Request) {
	id, ok := pathIncidentID(w, r)
	if !ok {
		return
	}
	req := struct {
		Until string `json:"until"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(req.Until))
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("until must be RFC3339: %w", err))
		return
	}
	incident, err := d.db.SnoozeIncident(r.Context(), id, until)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (d routeDeps) handleIncidentResolve(w http.ResponseWriter, r *http.Request) {
	id, ok := pathIncidentID(w, r)
	if !ok {
		return
	}
	incident, err := d.db.ResolveIncident(r.Context(), id)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, incident)
}

func (d routeDeps) handleIncidentDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathIncidentID(w, r)
	if !ok {
		return
	}
	incident, err := d.db.DeleteIncident(r.Context(), id)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "incident": incident})
}
