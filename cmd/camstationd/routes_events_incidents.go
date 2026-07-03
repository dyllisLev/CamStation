package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"camstation/internal/store"
)

func (d routeDeps) registerEventIncidentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/events", d.handleEventsList)
	mux.HandleFunc("GET /api/events/export", d.handleEventsExport)
	mux.HandleFunc("DELETE /api/events", d.handleEventsPrune)
	mux.HandleFunc("POST /api/incidents", d.handleIncidentCreate)
	mux.HandleFunc("GET /api/incidents", d.handleIncidentList)
	mux.HandleFunc("GET /api/incidents/{id}", d.handleIncidentDetail)
	mux.HandleFunc("PATCH /api/incidents/{id}", d.handleIncidentPatch)
	mux.HandleFunc("POST /api/incidents/{id}/ack", d.handleIncidentAck)
	mux.HandleFunc("POST /api/incidents/{id}/snooze", d.handleIncidentSnooze)
	mux.HandleFunc("POST /api/incidents/{id}/resolve", d.handleIncidentResolve)
	mux.HandleFunc("DELETE /api/incidents/{id}", d.handleIncidentDelete)
}

func (d routeDeps) handleEventsList(w http.ResponseWriter, r *http.Request) {
	query, err := eventQueryFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	page, err := d.db.QueryEvents(r.Context(), query)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	if r.URL.RawQuery == "" {
		writeJSON(w, http.StatusOK, page.Events)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (d routeDeps) handleEventsExport(w http.ResponseWriter, r *http.Request) {
	query, err := eventQueryFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	events, err := d.db.ExportEvents(r.Context(), query)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, event := range events {
			fmt.Fprintf(w, "%s %s %s %s\n", event.CreatedAt.Format(time.RFC3339), event.Level, event.Source, event.Message)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (d routeDeps) handleEventsPrune(w http.ResponseWriter, r *http.Request) {
	prune, err := eventPruneFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := d.db.PruneEvents(r.Context(), prune)
	if err != nil {
		writeStoreEventIncidentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func eventQueryFromRequest(r *http.Request) (store.EventQuery, error) {
	query := r.URL.Query()
	limit, err := optionalPositiveInt(query.Get("limit"))
	if err != nil {
		return store.EventQuery{}, err
	}
	from, err := optionalRFC3339(query.Get("from"))
	if err != nil {
		return store.EventQuery{}, fmt.Errorf("from must be RFC3339: %w", err)
	}
	to, err := optionalRFC3339(query.Get("to"))
	if err != nil {
		return store.EventQuery{}, fmt.Errorf("to must be RFC3339: %w", err)
	}
	return store.EventQuery{
		Level:  strings.TrimSpace(query.Get("level")),
		Source: strings.TrimSpace(query.Get("source")),
		Search: strings.TrimSpace(query.Get("search")),
		From:   from,
		To:     to,
		Cursor: strings.TrimSpace(query.Get("cursor")),
		Limit:  limit,
	}, nil
}

func eventPruneFromRequest(r *http.Request) (store.EventPrune, error) {
	query := r.URL.Query()
	limit, err := optionalPositiveInt(query.Get("limit"))
	if err != nil {
		return store.EventPrune{}, err
	}
	if limit == 0 {
		limit = 100
	}
	before, err := optionalRFC3339(query.Get("before"))
	if err != nil {
		return store.EventPrune{}, fmt.Errorf("before must be RFC3339: %w", err)
	}
	return store.EventPrune{
		Confirm: query.Get("confirm") == "true",
		Before:  before,
		Level:   strings.TrimSpace(query.Get("level")),
		Source:  strings.TrimSpace(query.Get("source")),
		Search:  strings.TrimSpace(query.Get("search")),
		Limit:   limit,
	}, nil
}

func pathIncidentID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, store.ErrValidation)
		return 0, false
	}
	return id, true
}

func optionalPositiveInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("limit must be positive: %w", store.ErrValidation)
	}
	return parsed, nil
}

func optionalRFC3339(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}

func writeStoreEventIncidentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrValidation):
		writeError(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrIncidentConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, store.ErrIncidentNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}
